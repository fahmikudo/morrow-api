package calendar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"morrow-api/internal/config"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/settings"
	"morrow-api/internal/platform/google"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/secretbox"
	"morrow-api/internal/platform/timezone"
	"morrow-api/internal/platform/wiretime"
)

const stateTTL = 10 * time.Minute

// Service owns the OAuth connection and two-way event sync.
type Service struct {
	db           *gorm.DB
	cfg          *config.Config
	log          *slog.Logger
	repo         *Repository
	events       *event.Repository
	profiles     *profile.Repository
	integrations *settings.Repository
	google       *google.Client
	box          *secretbox.Box
	locks        sync.Map
}

// NewService wires the calendar integration.
func NewService(
	db *gorm.DB,
	cfg *config.Config,
	log *slog.Logger,
	events *event.Repository,
	profiles *profile.Repository,
	integrations *settings.Repository,
	client *google.Client,
	box *secretbox.Box,
) *Service {
	return &Service{
		db:           db,
		cfg:          cfg,
		log:          log,
		repo:         NewRepository(db),
		events:       events,
		profiles:     profiles,
		integrations: integrations,
		google:       client,
		box:          box,
	}
}

// Configured reports whether OAuth and token encryption are available.
func (s *Service) Configured() bool {
	return s.google != nil && s.google.Configured() && s.box != nil && s.box.Enabled()
}

// Start creates a one-time OAuth state and returns the consent URL.
func (s *Service) Start(ctx context.Context, userID string) (string, error) {
	if !s.Configured() {
		return "", httpx.BadRequest("Google OAuth is not configured")
	}

	connection, err := s.integrations.Integration(ctx, nil, userID, IntegrationID)
	if err != nil {
		return "", httpx.Internal(err, s.log)
	}
	if connection == nil || !connection.Enabled {
		return "", httpx.BadRequest("Integration is disabled")
	}

	state, err := randomToken(32)
	if err != nil {
		return "", httpx.Internal(err, s.log)
	}
	verifier, err := randomToken(32)
	if err != nil {
		return "", httpx.Internal(err, s.log)
	}
	sealedVerifier, err := s.box.Seal(verifier)
	if err != nil {
		return "", httpx.Internal(err, s.log)
	}

	_ = s.repo.PurgeExpiredStates(ctx)
	if err := s.repo.CreateState(ctx, OAuthStateRow{
		StateHash:       hashToken(state),
		UserID:          userID,
		CodeVerifierEnc: sealedVerifier,
		ExpiresAt:       wiretime.Now().Time().Add(stateTTL),
	}); err != nil {
		return "", httpx.Internal(err, s.log)
	}

	challenge := sha256.Sum256([]byte(verifier))
	return s.google.AuthURL(state, base64.RawURLEncoding.EncodeToString(challenge[:])), nil
}

// Callback exchanges the code, stores tokens, and marks the integration
// connected. The caller triggers the first sync.
func (s *Service) Callback(ctx context.Context, code, state string) (string, string, error) {
	if !s.Configured() {
		return "", "", httpx.BadRequest("Google OAuth is not configured")
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		return "", "", httpx.BadRequest("Google authorization was cancelled")
	}

	stored, err := s.repo.ConsumeState(ctx, hashToken(strings.TrimSpace(state)))
	if err != nil {
		return "", "", httpx.Internal(err, s.log)
	}
	if stored == nil || stored.ExpiresAt.Before(wiretime.Now().Time()) {
		return "", "", httpx.BadRequest("OAuth state is invalid or expired")
	}

	verifier, err := s.box.Open(stored.CodeVerifierEnc)
	if err != nil {
		return "", "", httpx.Internal(err, s.log)
	}

	token, err := s.google.Exchange(ctx, strings.TrimSpace(code), verifier)
	if err != nil {
		s.log.Warn("google token exchange failed", "error", err)
		return "", "", httpx.BadRequest("Google authorization failed")
	}

	refreshToken := token.RefreshToken
	if refreshToken == "" {
		existing, err := s.repo.Credential(ctx, stored.UserID)
		if err != nil {
			return "", "", httpx.Internal(err, s.log)
		}
		if existing != nil && existing.RefreshTokenEnc != nil {
			refreshToken, err = s.box.Open(*existing.RefreshTokenEnc)
			if err != nil {
				return "", "", httpx.Internal(err, s.log)
			}
		}
	}
	if refreshToken == "" {
		return "", "", httpx.BadRequest("Google did not return a refresh token; connect again")
	}

	accessEnc, err := s.box.Seal(token.AccessToken)
	if err != nil {
		return "", "", httpx.Internal(err, s.log)
	}
	refreshEnc, err := s.box.Seal(refreshToken)
	if err != nil {
		return "", "", httpx.Internal(err, s.log)
	}

	now := wiretime.Now().Time()
	expiry := token.Expiry
	if expiry.IsZero() {
		expiry = now.Add(time.Hour)
	}

	credential := CredentialRow{
		UserID:          stored.UserID,
		IntegrationID:   IntegrationID,
		AccessTokenEnc:  accessEnc,
		RefreshTokenEnc: &refreshEnc,
		TokenExpiry:     &expiry,
		Scope:           stringPtr(token.Scope),
		UpdatedAt:       now,
	}
	if err := s.repo.SaveCredential(ctx, credential); err != nil {
		return "", "", httpx.Internal(err, s.log)
	}

	email := emailFromIDToken(token.IDToken)
	connection, err := s.integrations.Integration(ctx, nil, stored.UserID, IntegrationID)
	if err != nil {
		return "", "", httpx.Internal(err, s.log)
	}
	if connection == nil {
		return "", "", httpx.Internal(errors.New("Google integration row is missing"), s.log)
	}

	connection.Enabled = true
	connection.Connected = true
	connection.AccountEmail = stringPtr(email)
	connection.CalendarID = stringPtr(DefaultCalendarID)
	connection.LastError = nil
	connection.LastSyncAt = nil
	if err := s.integrations.SetIntegration(ctx, s.db, stored.UserID, IntegrationID, *connection); err != nil {
		return "", "", httpx.Internal(err, s.log)
	}

	return email, stored.UserID, nil
}

// Connection returns the current settings row.
func (s *Service) Connection(ctx context.Context, userID string) (*settings.IntegrationConnection, error) {
	connection, err := s.integrations.Integration(ctx, nil, userID, IntegrationID)
	if err != nil {
		return nil, httpx.Internal(err, s.log)
	}
	if connection == nil {
		return nil, httpx.NotFound("Integration not found")
	}

	return connection, nil
}

// Calendars lists the account's readable calendars for the picker.
func (s *Service) Calendars(ctx context.Context, userID string) ([]CalendarItem, error) {
	connection, err := s.Connection(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !connection.Connected {
		return nil, httpx.BadRequest("Google Calendar is not connected")
	}

	credential, err := s.repo.Credential(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(err, s.log)
	}
	if credential == nil {
		return nil, httpx.BadRequest("Google Calendar is not connected")
	}

	access, _, err := s.accessToken(ctx, credential)
	if err != nil {
		s.recordError(ctx, userID, err)
		return nil, httpx.Internal(err, s.log)
	}

	items, err := s.google.Calendars(ctx, access)
	if err != nil {
		s.recordError(ctx, userID, err)
		return nil, httpx.Internal(err, s.log)
	}

	calendars := make([]CalendarItem, 0, len(items))
	for _, item := range items {
		calendars = append(calendars, CalendarItem{
			ID:         item.ID,
			Summary:    item.Summary,
			Primary:    item.Primary,
			AccessRole: item.AccessRole,
		})
	}

	return calendars, nil
}

// SelectCalendar switches the synced calendar; old imported events become
// local so nothing is deleted.
func (s *Service) SelectCalendar(
	ctx context.Context,
	userID, calendarID string,
) (*settings.IntegrationConnection, error) {
	unlock := s.lockUser(userID)
	defer unlock()

	calendarID = strings.TrimSpace(calendarID)
	if calendarID == "" {
		return nil, httpx.BadRequest("Calendar is required")
	}

	connection, err := s.Connection(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !connection.Connected {
		return nil, httpx.BadRequest("Google Calendar is not connected")
	}

	calendars, err := s.Calendars(ctx, userID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, item := range calendars {
		if item.ID == calendarID {
			found = true
			break
		}
	}
	if !found {
		return nil, httpx.BadRequest("Calendar not found")
	}

	// Keep links for calendars that are no longer selected. The next sync only
	// reconciles the selected calendar, so old imported events stay untouched.
	credential, err := s.repo.Credential(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(err, s.log)
	}
	if credential != nil {
		credential.SyncToken = nil
		credential.LastSyncAt = nil
		credential.UpdatedAt = wiretime.Now().Time()
		if err := s.repo.SaveCredential(ctx, *credential); err != nil {
			return nil, httpx.Internal(err, s.log)
		}
	}

	connection.CalendarID = &calendarID
	connection.LastSyncAt = nil
	connection.LastError = nil
	if err := s.integrations.SetIntegration(ctx, s.db, userID, IntegrationID, *connection); err != nil {
		return nil, httpx.Internal(err, s.log)
	}

	return connection, nil
}

// Disconnect revokes the token, drops links, and keeps imported events local.
func (s *Service) Disconnect(ctx context.Context, userID string) error {
	unlock := s.lockUser(userID)
	defer unlock()

	credential, err := s.repo.Credential(ctx, userID)
	if err != nil {
		return httpx.Internal(err, s.log)
	}

	if credential != nil {
		token := ""
		if credential.RefreshTokenEnc != nil {
			token, _ = s.box.Open(*credential.RefreshTokenEnc)
		}
		if token == "" {
			token, _ = s.box.Open(credential.AccessTokenEnc)
		}
		if token != "" {
			if err := s.google.Revoke(ctx, token); err != nil {
				s.log.Warn("google token revoke failed", "user_id", userID, "error", err)
			}
		}
	}

	links, err := s.repo.Links(ctx, userID)
	if err != nil {
		return httpx.Internal(err, s.log)
	}
	eventIDs := make([]string, 0, len(links))
	for _, link := range links {
		if link.EventID != nil {
			eventIDs = append(eventIDs, *link.EventID)
		}
	}
	if len(eventIDs) > 0 {
		if err := s.db.WithContext(ctx).Model(&event.Row{}).
			Where("user_id = ? and id in ?", userID, eventIDs).
			Update("source", "local").Error; err != nil {
			return httpx.Internal(err, s.log)
		}
	}

	if err := s.repo.DeleteLinks(ctx, userID); err != nil {
		return httpx.Internal(err, s.log)
	}
	if err := s.repo.DeleteCredential(ctx, userID); err != nil {
		return httpx.Internal(err, s.log)
	}

	connection, err := s.Connection(ctx, userID)
	if err != nil {
		return err
	}
	connection.Connected = false
	connection.AccountEmail = nil
	connection.CalendarID = nil
	connection.LastError = nil
	connection.LastSyncAt = nil
	if err := s.integrations.SetIntegration(ctx, s.db, userID, IntegrationID, *connection); err != nil {
		return httpx.Internal(err, s.log)
	}

	return nil
}

// SyncUser runs one pull + push pass for the user.
func (s *Service) SyncUser(ctx context.Context, userID string) error {
	unlock := s.lockUser(userID)
	defer unlock()

	credential, err := s.repo.Credential(ctx, userID)
	if err != nil {
		return httpx.Internal(err, s.log)
	}
	if credential == nil {
		return nil
	}

	connection, err := s.Connection(ctx, userID)
	if err != nil {
		return err
	}
	if !connection.Enabled || !connection.Connected {
		return nil
	}

	calendarID := DefaultCalendarID
	if connection.CalendarID != nil && *connection.CalendarID != "" {
		calendarID = *connection.CalendarID
	}

	access, credential, err := s.accessToken(ctx, credential)
	if err != nil {
		s.recordError(ctx, userID, err)
		return httpx.Internal(err, s.log)
	}

	if err := s.sync(ctx, userID, calendarID, credential, access); err != nil {
		s.recordError(ctx, userID, err)
		return err
	}

	now := wiretime.Now()
	connection.LastSyncAt = &now
	connection.LastError = nil
	if err := s.integrations.SetIntegration(ctx, s.db, userID, IntegrationID, *connection); err != nil {
		return httpx.Internal(err, s.log)
	}

	return nil
}

// Run syncs every connected user immediately and then on a ticker.
func (s *Service) Run(ctx context.Context) {
	tick := time.Duration(s.cfg.CalendarSyncSeconds) * time.Second
	if tick <= 0 {
		tick = 5 * time.Minute
	}

	s.log.Info("google calendar sync started", "tick_seconds", int(tick.Seconds()))
	s.SyncAll(ctx)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SyncAll(ctx)
		}
	}
}

// SyncAll runs one pass for every connected account.
func (s *Service) SyncAll(ctx context.Context) {
	if !s.Configured() {
		return
	}

	userIDs, err := s.repo.CredentialUserIDs(ctx)
	if err != nil {
		s.log.Warn("google calendar sync list failed", "error", err)
		return
	}

	for _, userID := range userIDs {
		if ctx.Err() != nil {
			return
		}
		if err := s.SyncUser(ctx, userID); err != nil {
			s.log.Warn("google calendar sync failed", "user_id", userID, "error", err)
		}
	}
}

// Push mirrors one local event to Google. It is called after a local write and
// never makes the local write fail.
func (s *Service) Push(ctx context.Context, userID string, current event.Event) error {
	unlock := s.lockUser(userID)
	defer unlock()

	return s.pushEvent(ctx, userID, current)
}

func (s *Service) pushEvent(ctx context.Context, userID string, current event.Event) error {
	credential, connection, access, err := s.pushContext(ctx, userID)
	if err != nil || credential == nil || connection == nil {
		return err
	}
	if !connection.Enabled || !connection.Connected {
		return nil
	}

	location := s.location(ctx, userID)
	payload := toGooglePayload(current, location)

	link, err := s.repo.LinkByEvent(ctx, userID, current.ID)
	if err != nil {
		return err
	}

	calendarID := DefaultCalendarID
	if link != nil {
		calendarID = link.CalendarID
	} else if connection.CalendarID != nil && *connection.CalendarID != "" {
		calendarID = *connection.CalendarID
	}

	var external google.Event
	if link == nil {
		external, err = s.google.InsertEvent(ctx, access, calendarID, payload)
	} else {
		external, err = s.google.UpdateEvent(ctx, access, calendarID, link.ExternalID, payload)
		if google.IsNotFound(err) {
			external, err = s.google.InsertEvent(ctx, access, calendarID, payload)
			if err == nil {
				link = nil
			}
		}
	}
	if err != nil {
		s.recordError(ctx, userID, err)
		return err
	}

	updated := parseGoogleTime(external.Updated)
	if link == nil {
		link = &LinkRow{
			ID:            ids.New("glink"),
			EventID:       &current.ID,
			UserID:        userID,
			IntegrationID: IntegrationID,
			CalendarID:    calendarID,
			ExternalID:    external.ID,
		}
		link.ExternalETag = stringPtr(external.Etag)
		link.ExternalUpdatedAt = &updated
		return s.repo.InsertLink(ctx, *link)
	}

	link.CalendarID = calendarID
	link.ExternalID = external.ID
	link.ExternalETag = stringPtr(external.Etag)
	link.ExternalUpdatedAt = &updated
	return s.repo.SaveLink(ctx, *link)
}

// Delete mirrors a local delete to Google. The caller still deletes the local
// row; a failed push leaves a link tombstone for the next sync.
func (s *Service) Delete(ctx context.Context, userID, eventID string) error {
	unlock := s.lockUser(userID)
	defer unlock()

	credential, connection, access, err := s.pushContext(ctx, userID)
	if err != nil || credential == nil || connection == nil {
		return err
	}
	if !connection.Enabled || !connection.Connected {
		return nil
	}

	link, err := s.repo.LinkByEvent(ctx, userID, eventID)
	if err != nil || link == nil {
		return err
	}

	if err := s.google.DeleteEvent(ctx, access, link.CalendarID, link.ExternalID); err != nil {
		s.recordError(ctx, userID, err)
		return err
	}

	return s.repo.DeleteLink(ctx, link.ID)
}

func (s *Service) pushContext(
	ctx context.Context,
	userID string,
) (*CredentialRow, *settings.IntegrationConnection, string, error) {
	credential, err := s.repo.Credential(ctx, userID)
	if err != nil {
		return nil, nil, "", err
	}
	if credential == nil {
		return nil, nil, "", nil
	}

	connection, err := s.Connection(ctx, userID)
	if err != nil {
		return nil, nil, "", err
	}
	if !connection.Enabled || !connection.Connected {
		return credential, connection, "", nil
	}

	access, credential, err := s.accessToken(ctx, credential)
	if err != nil {
		s.recordError(ctx, userID, err)
		return credential, connection, "", err
	}

	return credential, connection, access, nil
}

func (s *Service) sync(
	ctx context.Context,
	userID, calendarID string,
	credential *CredentialRow,
	access string,
) error {
	now := wiretime.Now().Time()
	timeMin := now.AddDate(0, 0, -30).UTC().Format(time.RFC3339)
	timeMax := now.AddDate(0, 0, 180).UTC().Format(time.RFC3339)

	syncToken := ""
	if credential.SyncToken != nil {
		syncToken = *credential.SyncToken
	}

	items, nextSyncToken, err := s.google.ListEvents(ctx, access, calendarID, syncToken, timeMin, timeMax)
	if errors.Is(err, google.ErrGone) {
		credential.SyncToken = nil
		items, nextSyncToken, err = s.google.ListEvents(ctx, access, calendarID, "", timeMin, timeMax)
	}
	if err != nil {
		return err
	}

	location := s.location(ctx, userID)
	for _, item := range items {
		if err := s.applyGoogleEvent(ctx, userID, calendarID, item, location); err != nil {
			return err
		}
	}

	if nextSyncToken != "" {
		credential.SyncToken = &nextSyncToken
	}
	credential.LastSyncAt = &now
	credential.UpdatedAt = now
	if err := s.repo.SaveCredential(ctx, *credential); err != nil {
		return err
	}

	return s.reconcile(ctx, userID, calendarID, access, location)
}

func (s *Service) applyGoogleEvent(
	ctx context.Context,
	userID, calendarID string,
	item google.Event,
	location *time.Location,
) error {
	link, err := s.repo.LinkByExternal(ctx, userID, calendarID, item.ID)
	if err != nil {
		return err
	}

	if item.Status == "cancelled" {
		if link == nil {
			return nil
		}
		if link.EventID != nil {
			if err := s.events.Delete(ctx, userID, *link.EventID); err != nil {
				return err
			}
		}
		return s.repo.DeleteLink(ctx, link.ID)
	}

	mapped, ok := fromGoogleEvent(item, location)
	if !ok {
		return nil
	}

	if link == nil {
		mapped.ID = ids.New("e")
		mapped.Source = "google"
		row := event.Row{UserID: userID}
		row.Load(mapped)
		if err := s.events.Insert(ctx, s.db, row); err != nil {
			return err
		}

		updated := parseGoogleTime(item.Updated)
		return s.repo.InsertLink(ctx, LinkRow{
			ID:                ids.New("glink"),
			EventID:           &mapped.ID,
			UserID:            userID,
			IntegrationID:     IntegrationID,
			CalendarID:        calendarID,
			ExternalID:        item.ID,
			ExternalETag:      stringPtr(item.Etag),
			ExternalUpdatedAt: &updated,
		})
	}

	if link.EventID == nil {
		return nil
	}

	current, err := s.events.Get(ctx, userID, *link.EventID)
	if err != nil {
		return err
	}
	if current == nil {
		link.EventID = nil
		return s.repo.SaveLink(ctx, *link)
	}

	externalUpdated := parseGoogleTime(item.Updated)
	if link.ExternalUpdatedAt != nil && current.UpdatedAt.Time().After((*link.ExternalUpdatedAt).Add(time.Second)) {
		// Local wins; the reconcile pass pushes it after the pull.
		return nil
	}

	updated := *current
	updated.Title = mapped.Title
	updated.Date = mapped.Date
	updated.StartTime = mapped.StartTime
	updated.EndTime = mapped.EndTime
	updated.Notes = mapped.Notes
	updated.UpdatedAt = wiretime.Timestamp(externalUpdated)

	row := event.Row{UserID: userID}
	row.Load(updated)
	if err := s.events.Save(ctx, s.db, row); err != nil {
		return err
	}

	link.ExternalETag = stringPtr(item.Etag)
	link.ExternalUpdatedAt = &externalUpdated
	return s.repo.SaveLink(ctx, *link)
}

func (s *Service) reconcile(
	ctx context.Context,
	userID, calendarID, access string,
	location *time.Location,
) error {
	links, err := s.repo.Links(ctx, userID)
	if err != nil {
		return err
	}

	for index := range links {
		link := &links[index]
		if link.CalendarID != calendarID {
			continue
		}
		if link.EventID == nil {
			if err := s.google.DeleteEvent(ctx, access, link.CalendarID, link.ExternalID); err != nil {
				if !google.IsNotFound(err) {
					return err
				}
			}
			if err := s.repo.DeleteLink(ctx, link.ID); err != nil {
				return err
			}
			continue
		}

		current, err := s.events.Get(ctx, userID, *link.EventID)
		if err != nil {
			return err
		}
		if current == nil {
			link.EventID = nil
			if err := s.repo.SaveLink(ctx, *link); err != nil {
				return err
			}
			continue
		}

		if link.ExternalUpdatedAt == nil ||
			current.UpdatedAt.Time().After(link.ExternalUpdatedAt.Add(time.Second)) {
			if err := s.pushLinked(ctx, userID, calendarID, access, *current, link, location); err != nil {
				return err
			}
		}
	}

	localEvents, err := s.events.List(ctx, userID)
	if err != nil {
		return err
	}
	for _, current := range localEvents {
		link, err := s.repo.LinkByEvent(ctx, userID, current.ID)
		if err != nil {
			return err
		}
		if link != nil {
			continue
		}
		if err := s.pushEvent(ctx, userID, current); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) pushLinked(
	ctx context.Context,
	userID, calendarID, access string,
	current event.Event,
	link *LinkRow,
	location *time.Location,
) error {
	external, err := s.google.UpdateEvent(
		ctx, access, calendarID, link.ExternalID, toGooglePayload(current, location),
	)
	if google.IsNotFound(err) {
		external, err = s.google.InsertEvent(ctx, access, calendarID, toGooglePayload(current, location))
	}
	if err != nil {
		return err
	}

	updated := parseGoogleTime(external.Updated)
	link.ExternalID = external.ID
	link.ExternalETag = stringPtr(external.Etag)
	link.ExternalUpdatedAt = &updated
	return s.repo.SaveLink(ctx, *link)
}

func (s *Service) accessToken(
	ctx context.Context,
	credential *CredentialRow,
) (string, *CredentialRow, error) {
	now := wiretime.Now().Time()
	if credential.AccessTokenEnc != "" && credential.TokenExpiry != nil &&
		credential.TokenExpiry.After(now.Add(time.Minute)) {
		access, err := s.box.Open(credential.AccessTokenEnc)
		if err != nil {
			return "", credential, err
		}
		return access, credential, nil
	}

	if credential.RefreshTokenEnc == nil {
		return "", credential, errors.New("Google access token expired and no refresh token is stored")
	}
	refresh, err := s.box.Open(*credential.RefreshTokenEnc)
	if err != nil {
		return "", credential, err
	}

	token, err := s.google.Refresh(ctx, refresh)
	if err != nil {
		return "", credential, err
	}

	accessEnc, err := s.box.Seal(token.AccessToken)
	if err != nil {
		return "", credential, err
	}
	credential.AccessTokenEnc = accessEnc
	if token.RefreshToken != "" {
		refreshEnc, err := s.box.Seal(token.RefreshToken)
		if err != nil {
			return "", credential, err
		}
		credential.RefreshTokenEnc = &refreshEnc
	}
	expiry := token.Expiry
	if expiry.IsZero() {
		expiry = now.Add(time.Hour)
	}
	credential.TokenExpiry = &expiry
	credential.Scope = stringPtr(token.Scope)
	credential.UpdatedAt = now
	if err := s.repo.SaveCredential(ctx, *credential); err != nil {
		return "", credential, err
	}

	return token.AccessToken, credential, nil
}

func (s *Service) location(ctx context.Context, userID string) *time.Location {
	var profileTimezone *string
	if current, err := s.profiles.Get(ctx, userID); err == nil && current != nil {
		profileTimezone = current.Timezone
	}

	return timezone.Resolve(profileTimezone, s.cfg.AppTimezone)
}

func (s *Service) lockUser(userID string) func() {
	value, _ := s.locks.LoadOrStore(userID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func (s *Service) recordError(ctx context.Context, userID string, err error) {
	connection, currentErr := s.Connection(ctx, userID)
	if currentErr != nil || connection == nil {
		return
	}

	message := err.Error()
	connection.LastError = &message
	if saveErr := s.integrations.SetIntegration(ctx, s.db, userID, IntegrationID, *connection); saveErr != nil {
		s.log.Warn("google calendar error state save failed", "user_id", userID, "error", saveErr)
	}
}

func fromGoogleEvent(item google.Event, location *time.Location) (event.Event, bool) {
	title := strings.TrimSpace(item.Summary)
	if title == "" {
		title = "Untitled event"
	}

	notes := strings.TrimSpace(item.Description)
	var notesPtr *string
	if notes != "" {
		if len(notes) > 4000 {
			notes = notes[:4000]
		}
		notesPtr = &notes
	}

	updated := parseGoogleTime(item.Updated)
	mapped := event.Event{
		Title:     title,
		Notes:     notesPtr,
		Source:    "google",
		CreatedAt: wiretime.Timestamp(updated),
		UpdatedAt: wiretime.Timestamp(updated),
	}

	if item.Start.Date != "" {
		start, err := wiretime.ParseDate(item.Start.Date)
		if err != nil {
			return event.Event{}, false
		}
		if item.End.Date != "" {
			end, err := wiretime.ParseDate(item.End.Date)
			if err != nil {
				return event.Event{}, false
			}
			expected := start.Time().AddDate(0, 0, 1).Format("2006-01-02")
			if end.String() != expected {
				return event.Event{}, false
			}
		}

		mapped.Date = start
		return mapped, true
	}

	if item.Start.DateTime == "" || item.End.DateTime == "" {
		return event.Event{}, false
	}

	start, err := time.Parse(time.RFC3339, item.Start.DateTime)
	if err != nil {
		return event.Event{}, false
	}
	end, err := time.Parse(time.RFC3339, item.End.DateTime)
	if err != nil {
		return event.Event{}, false
	}

	zone := location
	if item.Start.TimeZone != "" {
		if parsed, err := time.LoadLocation(item.Start.TimeZone); err == nil {
			zone = parsed
		}
	}

	localStart := start.In(zone)
	localEnd := end.In(zone)
	if localStart.Format("2006-01-02") != localEnd.Format("2006-01-02") {
		return event.Event{}, false
	}

	startClock := localStart.Format("15:04")
	endClock := localEnd.Format("15:04")
	parsedDate, err := wiretime.ParseDate(localStart.Format("2006-01-02"))
	if err != nil {
		return event.Event{}, false
	}

	mapped.Date = parsedDate
	mapped.StartTime = &startClock
	mapped.EndTime = &endClock
	return mapped, true
}

func toGooglePayload(current event.Event, location *time.Location) google.EventPayload {
	payload := google.EventPayload{
		Summary: current.Title,
	}
	if current.Notes != nil {
		payload.Description = *current.Notes
	}

	date := current.Date.String()
	if current.StartTime == nil && current.EndTime == nil {
		payload.Start = google.EventDateTime{Date: date}
		payload.End = google.EventDateTime{
			Date: parseDateOnly(date).AddDate(0, 0, 1).Format("2006-01-02"),
		}
		return payload
	}

	startClock := "00:00"
	if current.StartTime != nil {
		startClock = *current.StartTime
	}
	endClock := ""
	if current.EndTime != nil {
		endClock = *current.EndTime
	}

	if endClock == "" {
		endClock = startClock
	}

	start, startErr := time.ParseInLocation("2006-01-02 15:04", date+" "+startClock, location)
	end, endErr := time.ParseInLocation("2006-01-02 15:04", date+" "+endClock, location)
	if startErr != nil {
		start = time.Date(parseDateOnly(date).Year(), parseDateOnly(date).Month(), parseDateOnly(date).Day(), 0, 0, 0, 0, location)
	}
	if endErr != nil || !end.After(start) {
		end = start.Add(time.Hour)
	}

	payload.Start = google.EventDateTime{DateTime: start.Format(time.RFC3339), TimeZone: location.String()}
	payload.End = google.EventDateTime{DateTime: end.Format(time.RFC3339), TimeZone: location.String()}
	return payload
}

func parseDateOnly(value string) time.Time {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Now()
	}
	return parsed
}

func parseGoogleTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return wiretime.Now().Time()
	}
	return parsed.UTC()
}

func emailFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Email)
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

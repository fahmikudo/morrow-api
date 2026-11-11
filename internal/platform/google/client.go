// Package google wraps the Google OAuth and Calendar v3 endpoints used by the
// calendar integration.
package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"morrow-api/internal/config"
)

// Scope requests read/write access to events, read access to the calendar
// list (the picker calls CalendarList.list), plus the account email.
const Scope = "openid email " +
	"https://www.googleapis.com/auth/calendar.events " +
	"https://www.googleapis.com/auth/calendar.calendarlist.readonly"

// ErrGone means the incremental sync token is no longer valid.
var ErrGone = errors.New("google: sync token expired")

// StatusError carries a non-2xx Google response.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("google: HTTP %d: %s", e.Code, e.Message)
}

// IsNotFound reports a deleted/missing Google resource.
func IsNotFound(err error) bool {
	var status *StatusError
	if errors.As(err, &status) {
		return status.Code == http.StatusNotFound || status.Code == http.StatusGone
	}
	return errors.Is(err, ErrGone)
}

// Token is the token endpoint response.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int64     `json:"expires_in"`
	Scope        string    `json:"scope"`
	TokenType    string    `json:"token_type"`
	IDToken      string    `json:"id_token"`
	Expiry       time.Time `json:"-"`
}

// Calendar is one entry from calendarList.
type Calendar struct {
	ID         string `json:"id"`
	Summary    string `json:"summary"`
	Primary    bool   `json:"primary"`
	AccessRole string `json:"accessRole"`
}

// EventDateTime is Google's date or dateTime pair.
type EventDateTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

// Event is one Google Calendar event.
type Event struct {
	ID               string        `json:"id"`
	Status           string        `json:"status"`
	Summary          string        `json:"summary"`
	Description      string        `json:"description"`
	Updated          string        `json:"updated"`
	Etag             string        `json:"etag"`
	RecurringEventID string        `json:"recurringEventId"`
	Start            EventDateTime `json:"start"`
	End              EventDateTime `json:"end"`
}

// EventPayload is the writable subset the app owns.
type EventPayload struct {
	Summary     string        `json:"summary"`
	Description string        `json:"description,omitempty"`
	Start       EventDateTime `json:"start"`
	End         EventDateTime `json:"end"`
}

// Client talks to Google with a shared HTTP client.
type Client struct {
	http         *http.Client
	clientID     string
	clientSecret string
	redirectURL  string
	authorizeURL string
	tokenURL     string
	apiBase      string
	revokeURL    string
}

// New builds the client from configuration.
func New(cfg *config.Config, client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	return &Client{
		http:         client,
		clientID:     cfg.GoogleClientID,
		clientSecret: cfg.GoogleClientSecret,
		redirectURL:  cfg.GoogleRedirectURL,
		authorizeURL: cfg.GoogleAuthorizeURL,
		tokenURL:     cfg.GoogleTokenURL,
		apiBase:      strings.TrimRight(cfg.GoogleAPIBaseURL, "/"),
		revokeURL:    cfg.GoogleRevokeURL,
	}
}

// Configured reports whether OAuth credentials are present.
func (c *Client) Configured() bool {
	return c != nil && c.clientID != "" && c.clientSecret != "" && c.redirectURL != ""
}

// AuthURL builds the consent URL.
func (c *Client) AuthURL(state, codeChallenge string) string {
	values := url.Values{
		"client_id":             {c.clientID},
		"redirect_uri":          {c.redirectURL},
		"response_type":         {"code"},
		"scope":                 {Scope},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}

	return c.authorizeURL + "?" + values.Encode()
}

// Exchange trades an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code, codeVerifier string) (Token, error) {
	values := url.Values{
		"code":          {code},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"redirect_uri":  {c.redirectURL},
		"grant_type":    {"authorization_code"},
		"code_verifier": {codeVerifier},
	}

	return c.token(ctx, values)
}

// Refresh exchanges a refresh token for a new access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	values := url.Values{
		"refresh_token": {refreshToken},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"refresh_token"},
	}

	return c.token(ctx, values)
}

func (c *Client) token(ctx context.Context, values url.Values) (Token, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.tokenURL, strings.NewReader(values.Encode()),
	)
	if err != nil {
		return Token{}, err
	}
	request.Header.Set("content-type", "application/x-www-form-urlencoded")

	response, err := c.http.Do(request)
	if err != nil {
		return Token{}, err
	}
	defer closeBody(response)

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Token{}, statusError(response)
	}

	var token Token
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return Token{}, err
	}
	if token.AccessToken == "" {
		return Token{}, errors.New("google: token response is missing access_token")
	}
	if token.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}

	return token, nil
}

// Revoke invalidates a token at Google; a missing token is not fatal.
func (c *Client) Revoke(ctx context.Context, token string) error {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.revokeURL, strings.NewReader(url.Values{"token": {token}}.Encode()),
	)
	if err != nil {
		return err
	}
	request.Header.Set("content-type", "application/x-www-form-urlencoded")

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer closeBody(response)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if response.StatusCode == http.StatusBadRequest {
		return nil
	}

	return statusError(response)
}

// Calendars lists the calendars the account can read.
func (c *Client) Calendars(ctx context.Context, accessToken string) ([]Calendar, error) {
	var payload struct {
		Items []Calendar `json:"items"`
	}
	if err := c.get(ctx, accessToken, "/users/me/calendarList?minAccessRole=reader", &payload); err != nil {
		return nil, err
	}

	return payload.Items, nil
}

// ListEvents returns every page for one calendar. The final sync token is
// empty until Google has a stable page.
func (c *Client) ListEvents(
	ctx context.Context,
	accessToken, calendarID, syncToken, timeMin, timeMax string,
) ([]Event, string, error) {
	events := make([]Event, 0)
	pageToken := ""
	nextSyncToken := ""

	for {
		values := url.Values{"singleEvents": {"true"}, "showDeleted": {"true"}, "maxResults": {"2500"}}
		if syncToken != "" {
			values.Set("syncToken", syncToken)
		} else {
			values.Set("timeMin", timeMin)
			values.Set("timeMax", timeMax)
		}
		if pageToken != "" {
			values.Set("pageToken", pageToken)
		}

		var payload struct {
			Items         []Event `json:"items"`
			NextPageToken string  `json:"nextPageToken"`
			NextSyncToken string  `json:"nextSyncToken"`
		}
		path := "/calendars/" + url.PathEscape(calendarID) + "/events?" + values.Encode()
		if err := c.get(ctx, accessToken, path, &payload); err != nil {
			return nil, "", err
		}

		events = append(events, payload.Items...)
		nextSyncToken = payload.NextSyncToken
		if payload.NextPageToken == "" {
			break
		}
		pageToken = payload.NextPageToken
	}

	return events, nextSyncToken, nil
}

// InsertEvent creates an event and returns Google's copy.
func (c *Client) InsertEvent(
	ctx context.Context,
	accessToken, calendarID string,
	payload EventPayload,
) (Event, error) {
	var event Event
	if err := c.sendJSON(ctx, http.MethodPost, accessToken,
		"/calendars/"+url.PathEscape(calendarID)+"/events", payload, &event); err != nil {
		return Event{}, err
	}

	return event, nil
}

// UpdateEvent overwrites an event; last local write wins.
func (c *Client) UpdateEvent(
	ctx context.Context,
	accessToken, calendarID, eventID string,
	payload EventPayload,
) (Event, error) {
	var event Event
	if err := c.sendJSON(ctx, http.MethodPut, accessToken,
		"/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID), payload, &event); err != nil {
		return Event{}, err
	}

	return event, nil
}

// DeleteEvent removes an event; a missing event is already deleted.
func (c *Client) DeleteEvent(ctx context.Context, accessToken, calendarID, eventID string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.apiBase+"/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID), nil)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "Bearer "+accessToken)

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer closeBody(response)
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusError(response)
	}

	return nil
}

func (c *Client) get(ctx context.Context, accessToken, path string, dst any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "Bearer "+accessToken)
	request.Header.Set("accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer closeBody(response)
	if response.StatusCode == http.StatusGone {
		return ErrGone
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusError(response)
	}

	return json.NewDecoder(response.Body).Decode(dst)
}

func (c *Client) sendJSON(
	ctx context.Context,
	method, accessToken, path string,
	payload, dst any,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "Bearer "+accessToken)
	request.Header.Set("content-type", "application/json")
	request.Header.Set("accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer closeBody(response)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusError(response)
	}

	return json.NewDecoder(response.Body).Decode(dst)
}

func statusError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = strconv.Itoa(response.StatusCode)
	}

	return &StatusError{Code: response.StatusCode, Message: message}
}

func closeBody(response *http.Response) {
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}

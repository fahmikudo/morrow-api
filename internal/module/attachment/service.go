package attachment

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"mime"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/ids"
	"morrow-api/internal/platform/objectstore"
	"morrow-api/internal/platform/wiretime"
)

const (
	// MaxBytes is the upload limit for PDF and Office documents.
	MaxBytes int64 = 25 << 20
	// MaxImageBytes is the stricter upload limit for images.
	MaxImageBytes int64 = 10 << 20

	uploadTTL  = 15 * time.Minute
	pendingTTL = 24 * time.Hour
)

type fileKind struct {
	contentType string
	maxBytes    int64
}

var allowedFiles = map[string]fileKind{
	".pdf":  {contentType: "application/pdf", maxBytes: MaxBytes},
	".png":  {contentType: "image/png", maxBytes: MaxImageBytes},
	".jpg":  {contentType: "image/jpeg", maxBytes: MaxImageBytes},
	".jpeg": {contentType: "image/jpeg", maxBytes: MaxImageBytes},
	".webp": {contentType: "image/webp", maxBytes: MaxImageBytes},
	".gif":  {contentType: "image/gif", maxBytes: MaxImageBytes},
	".docx": {contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", maxBytes: MaxBytes},
	".xlsx": {contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", maxBytes: MaxBytes},
	".pptx": {contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation", maxBytes: MaxBytes},
	".doc":  {contentType: "application/msword", maxBytes: MaxBytes},
	".xls":  {contentType: "application/vnd.ms-excel", maxBytes: MaxBytes},
	".ppt":  {contentType: "application/vnd.ms-powerpoint", maxBytes: MaxBytes},
	".odt":  {contentType: "application/vnd.oasis.opendocument.text", maxBytes: MaxBytes},
	".ods":  {contentType: "application/vnd.oasis.opendocument.spreadsheet", maxBytes: MaxBytes},
	".odp":  {contentType: "application/vnd.oasis.opendocument.presentation", maxBytes: MaxBytes},
}

// Service applies attachment ownership, type, and storage rules.
type Service struct {
	db     *gorm.DB
	log    *slog.Logger
	repo   *Repository
	store  objectstore.Store
	urlTTL time.Duration
}

// NewService wires the attachment service.
func NewService(
	db *gorm.DB,
	log *slog.Logger,
	repo *Repository,
	store objectstore.Store,
	urlTTL time.Duration,
) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if urlTTL <= 0 {
		urlTTL = 15 * time.Minute
	}
	if urlTTL > time.Hour {
		urlTTL = time.Hour
	}

	return &Service{db: db, log: log, repo: repo, store: store, urlTTL: urlTTL}
}

// Config reports whether file storage is available and what the UI may upload.
func (s *Service) Config() Config {
	extensions := make([]string, 0, len(allowedFiles))
	for extension := range allowedFiles {
		extensions = append(extensions, extension)
	}
	sort.Strings(extensions)

	return Config{
		Enabled:       s.enabled(),
		MaxBytes:      MaxBytes,
		ImageMaxBytes: MaxImageBytes,
		Extensions:    extensions,
	}
}

func (s *Service) enabled() bool {
	return s.store != nil && s.store.Configured()
}

// List returns ready attachments for one owned entity.
func (s *Service) List(
	ctx context.Context,
	userID, entityType, entityID string,
) ([]Attachment, error) {
	if err := s.requireEntity(ctx, userID, entityType, entityID); err != nil {
		return nil, err
	}
	s.cleanupStale(ctx, userID)

	rows, err := s.repo.ListReady(ctx, userID, entityType, entityID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	attachments := make([]Attachment, 0, len(rows))
	for _, row := range rows {
		attachments = append(attachments, row.ToWire())
	}
	return attachments, nil
}

// Prepare validates the request and returns a signed direct-upload URL.
func (s *Service) Prepare(ctx context.Context, userID string, input Prepare) (*UploadTicket, error) {
	if !s.enabled() {
		return nil, httpx.Unavailable("File storage is not configured")
	}
	if err := s.requireEntity(ctx, userID, input.EntityType, input.EntityID); err != nil {
		return nil, err
	}

	filename, extension, kind, err := s.validateFile(input.Filename, input.Size)
	if err != nil {
		return nil, err
	}

	s.cleanupStale(ctx, userID)

	now := wiretime.Now().Time()
	attachmentID := ids.New("att")
	storageKey := fmt.Sprintf("attachments/%s/%s%s", userID, attachmentID, extension)
	row := Row{
		ID:          attachmentID,
		UserID:      userID,
		EntityType:  input.EntityType,
		EntityID:    input.EntityID,
		Filename:    filename,
		ContentType: kind.contentType,
		ByteSize:    input.Size,
		StorageKey:  storageKey,
		Status:      "pending",
		CreatedAt:   now,
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		return nil, httpx.Map(err, s.log)
	}

	presigned, err := s.store.PresignPut(ctx, storageKey, kind.contentType, uploadTTL)
	if err != nil {
		_ = s.repo.Delete(ctx, userID, attachmentID)
		return nil, httpx.Map(err, s.log)
	}

	return &UploadTicket{
		Attachment: row.ToWire(),
		UploadURL:  presigned.URL,
		Method:     presigned.Method,
		Headers:    presigned.Headers,
		ExpiresAt:  wiretime.Timestamp(now.Add(uploadTTL)),
	}, nil
}

// Complete verifies the uploaded object and marks the attachment ready.
func (s *Service) Complete(ctx context.Context, userID, id string) (*Attachment, error) {
	if !s.enabled() {
		return nil, httpx.Unavailable("File storage is not configured")
	}

	row, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if row == nil {
		return nil, httpx.NotFound("Attachment not found")
	}
	if row.Status == "ready" {
		attachment := row.ToWire()
		return &attachment, nil
	}

	object, err := s.store.Head(ctx, row.StorageKey)
	if err != nil {
		if err == objectstore.ErrNotFound {
			return nil, httpx.BadRequest("Upload was not completed")
		}
		return nil, httpx.Map(err, s.log)
	}

	kind := allowedFiles[strings.ToLower(path.Ext(row.Filename))]
	if object.Size <= 0 || object.Size > kind.maxBytes {
		s.discard(ctx, row)
		return nil, httpx.BadRequest("File exceeds the size limit")
	}
	if object.ContentType != "" && object.ContentType != kind.contentType {
		s.discard(ctx, row)
		return nil, httpx.BadRequest("File content type does not match its extension")
	}

	prefix, err := s.store.Sniff(ctx, row.StorageKey, 512)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if !matchesMagic(prefix, kind.contentType) {
		s.discard(ctx, row)
		return nil, httpx.BadRequest("File content does not match its type")
	}

	changed, err := s.repo.MarkReady(ctx, userID, id, object.Size)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if !changed {
		current, err := s.repo.Get(ctx, userID, id)
		if err != nil {
			return nil, httpx.Map(err, s.log)
		}
		if current == nil || current.Status != "ready" {
			return nil, httpx.BadRequest("Upload is no longer pending")
		}
		row = current
	} else {
		row.Status = "ready"
		row.ByteSize = object.Size
	}

	attachment := row.ToWire()
	return &attachment, nil
}

// URL returns a short-lived signed download or inline preview URL.
func (s *Service) URL(ctx context.Context, userID, id string, inline bool) (*URLResponse, error) {
	if !s.enabled() {
		return nil, httpx.Unavailable("File storage is not configured")
	}

	row, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	if row == nil || row.Status != "ready" {
		return nil, httpx.NotFound("Attachment not found")
	}

	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	value := mime.FormatMediaType(disposition, map[string]string{"filename": row.Filename})
	url, err := s.store.PresignGet(ctx, row.StorageKey, row.ContentType, value, s.urlTTL)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	return &URLResponse{
		URL:       url,
		ExpiresAt: wiretime.Timestamp(wiretime.Now().Time().Add(s.urlTTL)),
	}, nil
}

// Delete removes the stored object and its metadata.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	if !s.enabled() {
		return httpx.Unavailable("File storage is not configured")
	}

	row, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return httpx.Map(err, s.log)
	}
	if row == nil {
		return httpx.NotFound("Attachment not found")
	}
	if err := s.store.Delete(ctx, row.StorageKey); err != nil {
		return httpx.Map(err, s.log)
	}
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return httpx.Map(err, s.log)
	}
	return nil
}

// DeleteForEntity removes all attachments when a project, task, or note is deleted.
// Object cleanup is best-effort so the local delete never fails on storage errors.
func (s *Service) DeleteForEntity(ctx context.Context, userID, entityType, entityID string) {
	rows, err := s.repo.ListForEntity(ctx, userID, entityType, entityID)
	if err != nil {
		s.log.Warn("attachment cleanup lookup failed", "entity_type", entityType, "entity_id", entityID, "error", err)
		return
	}
	for _, row := range rows {
		if s.store != nil && s.store.Configured() {
			if err := s.store.Delete(ctx, row.StorageKey); err != nil {
				s.log.Warn("attachment object cleanup failed", "attachment_id", row.ID, "error", err)
			}
		}
	}
	if err := s.repo.DeleteByEntity(ctx, userID, entityType, entityID); err != nil {
		s.log.Warn("attachment metadata cleanup failed", "entity_type", entityType, "entity_id", entityID, "error", err)
	}
}

func (s *Service) requireEntity(ctx context.Context, userID, entityType, entityID string) error {
	if entityType != EntityProject && entityType != EntityTask && entityType != EntityNote {
		return httpx.BadRequest("Entity type must be project, task, or note")
	}
	if strings.TrimSpace(entityID) == "" {
		return httpx.BadRequest("Entity id is required")
	}

	exists, err := s.repo.EntityExists(ctx, userID, entityType, entityID)
	if err != nil {
		return httpx.Map(err, s.log)
	}
	if !exists {
		return httpx.NotFound("Attachment target not found")
	}
	return nil
}

func (s *Service) validateFile(rawName string, size int64) (string, string, fileKind, error) {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return "", "", fileKind{}, httpx.BadRequest("Filename is required")
	}
	if len(name) > 200 || path.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return "", "", fileKind{}, httpx.BadRequest("Filename is invalid")
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", "", fileKind{}, httpx.BadRequest("Filename is invalid")
		}
	}

	extension := strings.ToLower(path.Ext(name))
	kind, ok := allowedFiles[extension]
	if !ok {
		return "", "", fileKind{}, httpx.BadRequest("Unsupported file type")
	}
	if size <= 0 {
		return "", "", fileKind{}, httpx.BadRequest("File size is required")
	}
	if size > kind.maxBytes {
		return "", "", fileKind{}, httpx.BadRequest("File exceeds the size limit")
	}

	return name, extension, kind, nil
}

func (s *Service) cleanupStale(ctx context.Context, userID string) {
	rows, err := s.repo.StalePending(ctx, userID, wiretime.Now().Time().Add(-pendingTTL), 25)
	if err != nil {
		s.log.Warn("stale attachment lookup failed", "user_id", userID, "error", err)
		return
	}
	for _, row := range rows {
		if s.store != nil && s.store.Configured() {
			if err := s.store.Delete(ctx, row.StorageKey); err != nil {
				s.log.Warn("stale attachment delete failed", "attachment_id", row.ID, "error", err)
			}
		}
		if err := s.repo.Delete(ctx, userID, row.ID); err != nil {
			s.log.Warn("stale attachment row delete failed", "attachment_id", row.ID, "error", err)
		}
	}
}

func (s *Service) discard(ctx context.Context, row *Row) {
	if s.store != nil && s.store.Configured() {
		_ = s.store.Delete(ctx, row.StorageKey)
	}
	_ = s.repo.Delete(ctx, row.UserID, row.ID)
}

func matchesMagic(prefix []byte, contentType string) bool {
	switch contentType {
	case "application/pdf":
		return bytes.HasPrefix(prefix, []byte("%PDF-"))
	case "image/png":
		return bytes.HasPrefix(prefix, []byte("\x89PNG\r\n\x1a\n"))
	case "image/jpeg":
		return bytes.HasPrefix(prefix, []byte("\xff\xd8\xff"))
	case "image/webp":
		return len(prefix) >= 12 && string(prefix[0:4]) == "RIFF" && string(prefix[8:12]) == "WEBP"
	case "image/gif":
		return bytes.HasPrefix(prefix, []byte("GIF87a")) || bytes.HasPrefix(prefix, []byte("GIF89a"))
	case "application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint":
		return bytes.HasPrefix(prefix, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"))
	default:
		// OOXML and OpenDocument are ZIP containers.
		return bytes.HasPrefix(prefix, []byte("PK\x03\x04"))
	}
}

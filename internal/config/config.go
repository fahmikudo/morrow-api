// Package config loads runtime settings from the environment, with an optional
// `.env` file as the fallback source. Exported environment variables always win.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/spf13/viper"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	DatabaseURL     string
	BindAddr        string
	CORSOrigins     []string
	TokenTTLDays    int
	DeepSeekBaseURL string
	// AppTimezone is the fallback for users without a profile timezone.
	// Backend timestamps remain UTC.
	AppTimezone     string
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	// ReminderTickSeconds is how often the background reminder scheduler runs.
	ReminderTickSeconds      int
	GoogleClientID           string
	GoogleClientSecret       string
	GoogleRedirectURL        string
	GoogleAuthorizeURL       string
	GoogleTokenURL           string
	GoogleAPIBaseURL         string
	GoogleRevokeURL          string
	AppEncryptionKey         string
	CalendarSyncSeconds      int
	PepyEnabled              bool
	PepyAllowedEmails        []string
	PepyBaseURL              string
	PepyUsername             string
	PepyPassword             string
	PepyUserID               int64
	PepyProjectID            int64
	PepyLoginIntervalSeconds int
	PepySyncSeconds          int
	S3Endpoint               string
	S3Region                 string
	S3Bucket                 string
	S3AccessKeyID            string
	S3SecretAccessKey        string
	S3ForcePathStyle         bool
	S3AutoCreateBucket       bool
	AttachmentURLTTLSeconds  int
	LogLevel                 string
	LogFile                  string
	LogMaxSizeMB             int
	LogMaxBackups            int
	LogMaxAgeDays            int
	LogCompress              bool
}

// ErrMissingDatabaseURL is returned when DATABASE_URL is unset or blank.
var ErrMissingDatabaseURL = errors.New(
	"DATABASE_URL is not set. Copy .env.example to .env and point it at a running PostgreSQL")

// Load reads `.env` (when present) and the process environment.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("dotenv")

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("read .env: %w", err)
		}
	}

	v.SetDefault("BIND_ADDR", "127.0.0.1:8080")
	v.SetDefault("CORS_ORIGIN", "http://localhost:5173")
	v.SetDefault("TOKEN_TTL_DAYS", 30)
	v.SetDefault("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1")
	v.SetDefault("APP_TIMEZONE", "UTC")
	v.SetDefault("VAPID_PUBLIC_KEY", "")
	v.SetDefault("VAPID_PRIVATE_KEY", "")
	v.SetDefault("VAPID_SUBJECT", "admin@example.com")
	v.SetDefault("REMINDER_TICK_SECONDS", 60)
	v.SetDefault("GOOGLE_CLIENT_ID", "")
	v.SetDefault("GOOGLE_CLIENT_SECRET", "")
	v.SetDefault("GOOGLE_REDIRECT_URL", "http://127.0.0.1:8080/api/v1/integrations/google-calendar/oauth/callback")
	v.SetDefault("GOOGLE_AUTHORIZE_URL", "https://accounts.google.com/o/oauth2/v2/auth")
	v.SetDefault("GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token")
	v.SetDefault("GOOGLE_API_BASE_URL", "https://www.googleapis.com/calendar/v3")
	v.SetDefault("GOOGLE_REVOKE_URL", "https://oauth2.googleapis.com/revoke")
	v.SetDefault("APP_ENCRYPTION_KEY", "")
	v.SetDefault("CALENDAR_SYNC_SECONDS", 300)
	v.SetDefault("PEPY_ENABLED", false)
	v.SetDefault("PEPY_ALLOWED_EMAILS", "")
	v.SetDefault("PEPY_BASE_URL", "https://softwareseniapi.pepy.app/api/v1")
	v.SetDefault("PEPY_USERNAME", "")
	v.SetDefault("PEPY_PASSWORD", "")
	v.SetDefault("PEPY_USER_ID", 820)
	v.SetDefault("PEPY_PROJECT_ID", 5674)
	v.SetDefault("PEPY_LOGIN_INTERVAL_SECONDS", 259200)
	v.SetDefault("PEPY_SYNC_SECONDS", 300)
	v.SetDefault("S3_ENDPOINT", "")
	v.SetDefault("S3_REGION", "auto")
	v.SetDefault("S3_BUCKET", "second-brain")
	v.SetDefault("S3_ACCESS_KEY_ID", "")
	v.SetDefault("S3_SECRET_ACCESS_KEY", "")
	v.SetDefault("S3_FORCE_PATH_STYLE", false)
	v.SetDefault("S3_AUTO_CREATE_BUCKET", false)
	v.SetDefault("ATTACHMENT_URL_TTL_SECONDS", 900)
	v.SetDefault("LOG_LEVEL", "info")
	v.SetDefault("LOG_FILE", "")
	v.SetDefault("LOG_MAX_SIZE_MB", 100)
	v.SetDefault("LOG_MAX_BACKUPS", 3)
	v.SetDefault("LOG_MAX_AGE_DAYS", 14)
	v.SetDefault("LOG_COMPRESS", false)
	v.AutomaticEnv()

	cfg := &Config{
		DatabaseURL:              strings.TrimSpace(v.GetString("DATABASE_URL")),
		BindAddr:                 v.GetString("BIND_ADDR"),
		CORSOrigins:              splitOrigins(v.GetString("CORS_ORIGIN")),
		TokenTTLDays:             v.GetInt("TOKEN_TTL_DAYS"),
		DeepSeekBaseURL:          strings.TrimRight(v.GetString("DEEPSEEK_BASE_URL"), "/"),
		AppTimezone:              strings.TrimSpace(v.GetString("APP_TIMEZONE")),
		VAPIDPublicKey:           strings.TrimSpace(v.GetString("VAPID_PUBLIC_KEY")),
		VAPIDPrivateKey:          strings.TrimSpace(v.GetString("VAPID_PRIVATE_KEY")),
		VAPIDSubject:             strings.TrimSpace(v.GetString("VAPID_SUBJECT")),
		ReminderTickSeconds:      v.GetInt("REMINDER_TICK_SECONDS"),
		GoogleClientID:           strings.TrimSpace(v.GetString("GOOGLE_CLIENT_ID")),
		GoogleClientSecret:       strings.TrimSpace(v.GetString("GOOGLE_CLIENT_SECRET")),
		GoogleRedirectURL:        strings.TrimSpace(v.GetString("GOOGLE_REDIRECT_URL")),
		GoogleAuthorizeURL:       strings.TrimSpace(v.GetString("GOOGLE_AUTHORIZE_URL")),
		GoogleTokenURL:           strings.TrimSpace(v.GetString("GOOGLE_TOKEN_URL")),
		GoogleAPIBaseURL:         strings.TrimRight(v.GetString("GOOGLE_API_BASE_URL"), "/"),
		GoogleRevokeURL:          strings.TrimSpace(v.GetString("GOOGLE_REVOKE_URL")),
		AppEncryptionKey:         strings.TrimSpace(v.GetString("APP_ENCRYPTION_KEY")),
		CalendarSyncSeconds:      v.GetInt("CALENDAR_SYNC_SECONDS"),
		PepyEnabled:              v.GetBool("PEPY_ENABLED"),
		PepyAllowedEmails:        splitOrigins(v.GetString("PEPY_ALLOWED_EMAILS")),
		PepyBaseURL:              strings.TrimRight(v.GetString("PEPY_BASE_URL"), "/"),
		PepyUsername:             strings.TrimSpace(v.GetString("PEPY_USERNAME")),
		PepyPassword:             v.GetString("PEPY_PASSWORD"),
		PepyUserID:               v.GetInt64("PEPY_USER_ID"),
		PepyProjectID:            v.GetInt64("PEPY_PROJECT_ID"),
		PepyLoginIntervalSeconds: v.GetInt("PEPY_LOGIN_INTERVAL_SECONDS"),
		PepySyncSeconds:          v.GetInt("PEPY_SYNC_SECONDS"),
		S3Endpoint:               strings.TrimRight(v.GetString("S3_ENDPOINT"), "/"),
		S3Region:                 strings.TrimSpace(v.GetString("S3_REGION")),
		S3Bucket:                 strings.TrimSpace(v.GetString("S3_BUCKET")),
		S3AccessKeyID:            strings.TrimSpace(v.GetString("S3_ACCESS_KEY_ID")),
		S3SecretAccessKey:        v.GetString("S3_SECRET_ACCESS_KEY"),
		S3ForcePathStyle:         v.GetBool("S3_FORCE_PATH_STYLE"),
		S3AutoCreateBucket:       v.GetBool("S3_AUTO_CREATE_BUCKET"),
		AttachmentURLTTLSeconds:  v.GetInt("ATTACHMENT_URL_TTL_SECONDS"),
		LogLevel:                 v.GetString("LOG_LEVEL"),
		LogFile:                  v.GetString("LOG_FILE"),
		LogMaxSizeMB:             v.GetInt("LOG_MAX_SIZE_MB"),
		LogMaxBackups:            v.GetInt("LOG_MAX_BACKUPS"),
		LogMaxAgeDays:            v.GetInt("LOG_MAX_AGE_DAYS"),
		LogCompress:              v.GetBool("LOG_COMPRESS"),
	}

	if cfg.DatabaseURL == "" {
		return nil, ErrMissingDatabaseURL
	}

	return cfg, nil
}

// splitOrigins turns the comma-separated CORS_ORIGIN value into a list.
func splitOrigins(raw string) []string {
	origins := make([]string, 0, 2)
	for _, origin := range strings.Split(raw, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

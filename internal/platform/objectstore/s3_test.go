package objectstore

import (
	"context"
	"net/url"
	"testing"
	"time"

	"morrow-api/internal/config"
)

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		bucket  string
		want    string
		wantErr bool
	}{
		{name: "host without scheme", raw: "account.r2.cloudflarestorage.com", bucket: "photos", want: "https://account.r2.cloudflarestorage.com"},
		{name: "trailing slash", raw: "https://account.r2.cloudflarestorage.com/", bucket: "photos", want: "https://account.r2.cloudflarestorage.com"},
		{name: "bucket path is stripped", raw: "https://account.r2.cloudflarestorage.com/photos", bucket: "photos", want: "https://account.r2.cloudflarestorage.com"},
		{name: "http is kept for local storage", raw: "http://localhost:9000", bucket: "photos", want: "http://localhost:9000"},
		{name: "empty", raw: "  ", bucket: "photos", wantErr: true},
		{name: "different path is rejected", raw: "https://media.example.com/bucket", bucket: "photos", wantErr: true},
		{name: "unsupported scheme", raw: "ftp://account.r2.cloudflarestorage.com", bucket: "photos", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeEndpoint(tc.raw, tc.bucket)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeEndpoint(%q) = %q, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeEndpoint(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("normalizeEndpoint(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestPresignPutSignsAgainstS3Endpoint(t *testing.T) {
	store, err := NewS3(context.Background(), &config.Config{
		S3Endpoint:        "https://account.r2.cloudflarestorage.com/second-brain",
		S3Region:          "auto",
		S3Bucket:          "second-brain",
		S3AccessKeyID:     "test-key",
		S3SecretAccessKey: "test-secret",
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	presigned, err := store.PresignPut(
		context.Background(), "attachments/u/a.png", "image/png", 15*time.Minute,
	)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}

	parsed, err := url.Parse(presigned.URL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "second-brain.account.r2.cloudflarestorage.com" {
		t.Errorf("presigned URL host = %s://%s, want https://second-brain.account.r2.cloudflarestorage.com", parsed.Scheme, parsed.Host)
	}
	if presigned.Method != "PUT" {
		t.Errorf("method = %q, want PUT", presigned.Method)
	}

	getURL, err := store.PresignGet(
		context.Background(), "attachments/u/a.png", "image/png", "inline", 15*time.Minute,
	)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	parsed, err = url.Parse(getURL)
	if err != nil {
		t.Fatalf("parse presigned GET: %v", err)
	}
	if parsed.Host != "second-brain.account.r2.cloudflarestorage.com" {
		t.Errorf("presigned GET host = %s, want the S3 API endpoint", parsed.Host)
	}
}

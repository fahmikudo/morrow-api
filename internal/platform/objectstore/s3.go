// Package objectstore wraps S3-compatible object storage for attachments.
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"morrow-api/internal/config"
)

// ErrNotFound means the object does not exist.
var ErrNotFound = errors.New("object not found")

// Object is the metadata stored alongside an upload.
type Object struct {
	Size        int64
	ContentType string
}

// Store is the object-storage slice attachments need.
type Store interface {
	Configured() bool
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (Presigned, error)
	PresignGet(ctx context.Context, key, contentType, disposition string, ttl time.Duration) (string, error)
	Head(ctx context.Context, key string) (Object, error)
	Sniff(ctx context.Context, key string, length int64) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

// Presigned is a browser-facing upload request.
type Presigned struct {
	URL     string
	Method  string
	Headers http.Header
}

// S3 talks to RustFS in development and Cloudflare R2 in production.
type S3 struct {
	ops     *s3.Client
	presign *s3.PresignClient
	bucket  string
}

// NewS3 builds the S3 store. It returns nil when the configuration is incomplete.
func NewS3(ctx context.Context, cfg *config.Config) (*S3, error) {
	if cfg == nil || cfg.S3Endpoint == "" || cfg.S3Bucket == "" ||
		cfg.S3AccessKeyID == "" || cfg.S3SecretAccessKey == "" {
		return nil, nil
	}

	region := cfg.S3Region
	if region == "" {
		region = "us-east-1"
	}

	base := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3SecretAccessKey, ""),
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}

	ops := newClient(base, cfg.S3Endpoint, cfg.S3ForcePathStyle)

	publicEndpoint := cfg.S3PublicEndpoint
	if publicEndpoint == "" {
		publicEndpoint = cfg.S3Endpoint
	}
	public := newClient(base, publicEndpoint, cfg.S3ForcePathStyle)

	store := &S3{
		ops:     ops,
		presign: s3.NewPresignClient(public),
		bucket:  cfg.S3Bucket,
	}
	if cfg.S3AutoCreateBucket {
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := store.ensureBucket(waitCtx); err != nil {
			return nil, err
		}
	}

	return store, nil
}

func newClient(cfg aws.Config, endpoint string, pathStyle bool) *s3.Client {
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = pathStyle
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
}

func (s *S3) ensureBucket(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		_, err := s.ops.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
		if err == nil {
			return nil
		}

		var apiErr smithy.APIError
		if errors.As(err, &apiErr) &&
			(apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchBucket") {
			_, createErr := s.ops.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(s.bucket),
			})
			if createErr == nil {
				return nil
			}
			err = createErr
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("storage not ready: %w", err)
		case <-ticker.C:
		}
	}
}

// Configured reports whether the store has an endpoint and credentials.
func (s *S3) Configured() bool {
	return s != nil && s.ops != nil
}

// PresignPut returns a direct browser upload request.
func (s *S3) PresignPut(
	ctx context.Context,
	key, contentType string,
	ttl time.Duration,
) (Presigned, error) {
	request, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return Presigned{}, err
	}

	return Presigned{URL: request.URL, Method: request.Method, Headers: request.SignedHeader}, nil
}

// PresignGet returns a signed browser download or inline preview URL.
func (s *S3) PresignGet(
	ctx context.Context,
	key, contentType, disposition string,
	ttl time.Duration,
) (string, error) {
	request, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(s.bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String(disposition),
		ResponseContentType:        aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}

	return request.URL, nil
}

// Head returns object metadata.
func (s *S3) Head(ctx context.Context, key string) (Object, error) {
	output, err := s.ops.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return Object{}, ErrNotFound
		}
		return Object{}, err
	}

	object := Object{}
	if output.ContentLength != nil {
		object.Size = *output.ContentLength
	}
	if output.ContentType != nil {
		object.ContentType = *output.ContentType
	}
	return object, nil
}

// Delete removes an object. A missing object is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.ops.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchKey") {
			return nil
		}
		return err
	}
	return nil
}

// Sniff reads the prefix of an uploaded object for magic-byte validation.
func (s *S3) Sniff(ctx context.Context, key string, length int64) ([]byte, error) {
	output, err := s.ops.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=0-%d", length-1)),
	})
	if err != nil {
		return nil, err
	}
	defer output.Body.Close()

	return io.ReadAll(io.LimitReader(output.Body, length))
}

var _ Store = (*S3)(nil)

package reminder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/SherClockHolmes/webpush-go"

	"morrow-api/internal/config"
)

// Subscription is one browser push endpoint and its encryption keys.
type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Payload is the JSON body the service worker reads.
type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

// Sender delivers one push payload to one device and reports whether Web Push
// is configured at all.
type Sender interface {
	Enabled() bool
	Send(ctx context.Context, sub Subscription, payload Payload) error
}

// ErrSubscriptionGone marks an endpoint the push service no longer accepts.
var ErrSubscriptionGone = errors.New("push subscription gone")

// PushSender signs and delivers Web Push messages with the configured VAPID key
// pair. With either key missing it stays disabled and never sends.
type PushSender struct {
	client     webpush.HTTPClient
	publicKey  string
	privateKey string
	subject    string
}

// NewPushSender builds the sender from the runtime configuration.
func NewPushSender(cfg *config.Config, client *http.Client) *PushSender {
	return &PushSender{
		client:     client,
		publicKey:  cfg.VAPIDPublicKey,
		privateKey: cfg.VAPIDPrivateKey,
		subject:    cfg.VAPIDSubject,
	}
}

// Enabled reports whether a full VAPID key pair is configured.
func (s *PushSender) Enabled() bool {
	if s == nil {
		return false
	}

	return s.publicKey != "" && s.privateKey != ""
}

// Send encrypts the payload with the subscription keys and posts it to the
// browser's push service.
func (s *PushSender) Send(ctx context.Context, sub Subscription, payload Payload) error {
	message, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	options := &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             3600,
	}
	if s.client != nil {
		options.HTTPClient = s.client
	}

	response, err := webpush.SendNotificationWithContext(ctx, message, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}, options)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return ErrSubscriptionGone
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("push service status %d", response.StatusCode)
	}

	return nil
}

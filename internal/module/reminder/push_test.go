package reminder

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/SherClockHolmes/webpush-go"

	"morrow-api/internal/config"
)

type fakePushClient struct {
	status   int
	requests []*http.Request
	bodies   [][]byte
}

func (c *fakePushClient) Do(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	c.requests = append(c.requests, request)
	c.bodies = append(c.bodies, body)

	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

func pushFixture(t *testing.T) (*PushSender, Subscription) {
	t.Helper()

	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("generate VAPID keys: %v", err)
	}

	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatalf("read auth bytes: %v", err)
	}

	sender := NewPushSender(&config.Config{
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
		VAPIDSubject:    "admin@example.com",
	}, nil)

	return sender, Subscription{
		Endpoint: "https://push.example.com/subscriptions/1",
		P256dh:   publicKey,
		Auth:     base64.RawURLEncoding.EncodeToString(auth),
	}
}

func TestPushSenderReportsEnabled(t *testing.T) {
	sender, _ := pushFixture(t)
	if !sender.Enabled() {
		t.Error("Enabled() = false with a full key pair")
	}

	disabled := NewPushSender(&config.Config{}, nil)
	if disabled.Enabled() {
		t.Error("Enabled() = true without keys")
	}
}

func TestPushSenderMapsGoneSubscription(t *testing.T) {
	sender, subscription := pushFixture(t)
	client := &fakePushClient{status: http.StatusGone}
	sender.client = client

	err := sender.Send(context.Background(), subscription, Payload{Title: "Task due today", Body: "SB-1 · Ship it"})
	if !errors.Is(err, ErrSubscriptionGone) {
		t.Fatalf("Send error = %v, want ErrSubscriptionGone", err)
	}

	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.requests))
	}
	if got := client.requests[0].Header.Get("Content-Encoding"); got != "aes128gcm" {
		t.Errorf("Content-Encoding = %q, want aes128gcm", got)
	}
	if len(client.bodies[0]) == 0 {
		t.Error("request body is empty")
	}
}

func TestPushSenderAcceptsCreated(t *testing.T) {
	sender, subscription := pushFixture(t)
	client := &fakePushClient{status: http.StatusCreated}
	sender.client = client

	if err := sender.Send(context.Background(), subscription, Payload{Title: "Habit reminder"}); err != nil {
		t.Fatalf("Send error = %v, want nil", err)
	}
}

func TestPushSenderRejectsOtherStatuses(t *testing.T) {
	sender, subscription := pushFixture(t)
	sender.client = &fakePushClient{status: http.StatusInternalServerError}

	err := sender.Send(context.Background(), subscription, Payload{Title: "Task due today"})
	if err == nil || errors.Is(err, ErrSubscriptionGone) {
		t.Fatalf("Send error = %v, want a plain status error", err)
	}
}

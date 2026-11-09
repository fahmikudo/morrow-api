package secretbox

import (
	"encoding/base64"
	"testing"
)

func TestBoxRoundTripSupportsLongKeys(t *testing.T) {
	for name, raw := range map[string][]byte{
		"32 bytes": []byte("0123456789abcdef0123456789abcdef"),
		"48 bytes": []byte("0123456789abcdef0123456789abcdef0123456789abcdef"),
	} {
		t.Run(name, func(t *testing.T) {
			box, err := New(base64.StdEncoding.EncodeToString(raw))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if !box.Enabled() {
				t.Fatal("box is not enabled")
			}

			sealed, err := box.Seal("refresh-token")
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			opened, err := box.Open(sealed)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if opened != "refresh-token" {
				t.Errorf("opened = %q, want refresh-token", opened)
			}
		})
	}
}

func TestBoxDisabledWithoutKey(t *testing.T) {
	box, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if box.Enabled() {
		t.Fatal("box should be disabled without a key")
	}
}

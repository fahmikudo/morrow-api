package timezone

import (
	"testing"
	"time"
)

func TestResolvePrecedence(t *testing.T) {
	profile := "Europe/London"

	if got := Resolve(&profile, "Asia/Jakarta"); got.String() != profile {
		t.Errorf("profile timezone = %s, want %s", got, profile)
	}
	if got := Resolve(nil, "Asia/Jakarta"); got.String() != "Asia/Jakarta" {
		t.Errorf("fallback timezone = %s, want Asia/Jakarta", got)
	}
	if got := Resolve(nil, "Not/AZone"); got != time.UTC {
		t.Errorf("invalid fallback = %s, want UTC", got)
	}

	empty := ""
	if got := Resolve(&empty, "Asia/Jakarta"); got.String() != "Asia/Jakarta" {
		t.Errorf("empty profile = %s, want fallback Asia/Jakarta", got)
	}
}

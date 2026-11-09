package wiretime

import "testing"

func TestClockRoundTrips(t *testing.T) {
	t.Run("scan drops seconds", func(t *testing.T) {
		var clock Clock
		if err := clock.Scan("09:15:00"); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if clock.String() != "09:15" {
			t.Errorf("clock = %q, want 09:15", clock)
		}
	})

	t.Run("scan keeps minute precision", func(t *testing.T) {
		var clock Clock
		if err := clock.Scan([]byte("23:59:00.000000")); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if clock.String() != "23:59" {
			t.Errorf("clock = %q, want 23:59", clock)
		}
	})

	t.Run("scan rejects invalid values", func(t *testing.T) {
		var clock Clock
		if err := clock.Scan("24:00:00"); err == nil {
			t.Fatal("scan accepted 24:00:00")
		}
	})

	t.Run("value renders HH:MM", func(t *testing.T) {
		value, err := Clock("07:05").Value()
		if err != nil {
			t.Fatalf("value: %v", err)
		}
		if value != "07:05" {
			t.Errorf("value = %v, want 07:05", value)
		}
	})

	t.Run("converters preserve nil", func(t *testing.T) {
		if ClockText(nil) != nil || ClockValue(nil) != nil {
			t.Error("nil clock conversion should stay nil")
		}
	})
}

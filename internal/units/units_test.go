package units

import (
	"testing"
	"time"
)

func TestBytes(t *testing.T) {
	for _, c := range []struct {
		n      uint64
		fr, en string
	}{
		{0, "0 o", "0 B"}, {1023, "1023 o", "1023 B"}, {1536, "1,50 Ko", "1.50 KB"},
		{38 << 30, "38,0 Go", "38.0 GB"}, {500 << 30, "500 Go", "500 GB"},
	} {
		if got := Bytes(c.n, "fr"); got != c.fr {
			t.Errorf("fr %d: %q", c.n, got)
		}
		if got := Bytes(c.n, "en"); got != c.en {
			t.Errorf("en %d: %q", c.n, got)
		}
	}
}

func TestCountAndParse(t *testing.T) {
	if got := Count(1234567, "en"); got != "1,234,567" {
		t.Error(got)
	}
	if got := Count(999, "fr"); got != "999" {
		t.Error(got)
	}
	if d, err := ParseAge("30d"); err != nil || d != 30*24*time.Hour {
		t.Error(d, err)
	}
	if d, err := ParseAge("2w"); err != nil || d != 14*24*time.Hour {
		t.Error(d, err)
	}
	if _, err := ParseAge("abc"); err == nil {
		t.Error("expected error")
	}
	for s, want := range map[string]uint64{"10M": 10 << 20, "1.5G": 3 << 29, "500k": 500 << 10, "123": 123, "2Go": 2 << 30, "1MiB": 1 << 20} {
		if got, err := ParseSize(s); err != nil || got != want {
			t.Errorf("%s: %d %v", s, got, err)
		}
	}
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	if got := FileTimeToTime(TimeToFileTime(now)); !got.Equal(now) {
		t.Error(got)
	}
}

package widgetapi

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPairingCodeIsOneTimeAndExpires(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	pairing := NewPairing(strings.Repeat("t", 64), "https://calendar.example", 10*time.Minute)
	pairing.now = func() time.Time { return now }

	link, expires, err := pairing.CreatePairingURL(42)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	code := parsed.Query().Get("code")
	server, token, err := pairing.Redeem(code)
	if err != nil || server != "https://calendar.example" || len(token) != 64 || !expires.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("server=%q token_len=%d expires=%s err=%v", server, len(token), expires, err)
	}
	if _, _, err = pairing.Redeem(code); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("second redemption err=%v", err)
	}

	link, _, err = pairing.CreatePairingURL(42)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ = url.Parse(link)
	now = now.Add(11 * time.Minute)
	if _, _, err = pairing.Redeem(parsed.Query().Get("code")); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("expired redemption err=%v", err)
	}
}

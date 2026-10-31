package config

import (
	"strings"
	"testing"
)

func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("ADMIN_TELEGRAM_USER_ID", "214428515")
	for _, k := range []string{"CALENDAR_ANYA_ICAL_URL", "CALENDAR_LESHA_ICAL_URL", "CALENDAR_SASHA_ICAL_URL", "CALENDAR_NASTYA_ICAL_URL"} {
		t.Setenv(k, "https://calendar.example/secret.ics")
	}
	t.Setenv("TIMEZONE", "Europe/Moscow")
	t.Setenv("DATABASE_PATH", t.TempDir()+"/bot.db")
}
func TestLoad(t *testing.T) {
	validEnv(t)
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if c.AdminID != 214428515 || len(c.Calendars) != 4 {
		t.Fatalf("%+v", c)
	}
}
func TestRejectUsernameAndHTTP(t *testing.T) {
	validEnv(t)
	t.Setenv("ADMIN_TELEGRAM_USER_ID", "eusatenko")
	if _, e := Load(); e == nil {
		t.Fatal("username accepted")
	}
	validEnv(t)
	t.Setenv("CALENDAR_ANYA_ICAL_URL", "http://calendar.example/a.ics")
	if _, e := Load(); e == nil {
		t.Fatal("http accepted")
	}
}
func TestRejectInvalidDurationsAndTimezone(t *testing.T) {
	validEnv(t)
	t.Setenv("ICAL_CACHE_TTL", "0s")
	if _, e := Load(); e == nil {
		t.Fatal("zero ttl accepted")
	}
	validEnv(t)
	t.Setenv("TIMEZONE", "Mars/Olympus")
	if _, e := Load(); e == nil {
		t.Fatal("timezone accepted")
	}
}

func TestGoogleEditingRequiresCompleteConfiguration(t *testing.T) {
	validEnv(t)
	t.Setenv("CALENDAR_EDITING_ENABLED", "true")
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "client")
	if _, e := Load(); e == nil {
		t.Fatal("partial Google editing configuration accepted")
	}

	validEnv(t)
	t.Setenv("CALENDAR_EDITING_ENABLED", "true")
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "client")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("GOOGLE_OAUTH_REFRESH_TOKEN", "refresh")
	for _, key := range []string{"ANYA", "LESHA", "SASHA", "NASTYA"} {
		t.Setenv("GOOGLE_CALENDAR_"+key+"_ID", strings.ToLower(key)+"@example.com")
	}
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if !c.GoogleEditing.Enabled {
		t.Fatal("Google editing not enabled")
	}
}

func TestGoogleCredentialsStayDisabledWithoutFeatureFlag(t *testing.T) {
	validEnv(t)
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "client")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.GoogleEditing.Enabled {
		t.Fatal("editing enabled without feature flag")
	}
}

func TestWidgetAPIRequiresLongToken(t *testing.T) {
	validEnv(t)
	t.Setenv("WIDGET_API_ENABLED", "true")
	t.Setenv("WIDGET_API_TOKEN", "short")
	if _, err := Load(); err == nil {
		t.Fatal("short widget API token accepted")
	}

	validEnv(t)
	t.Setenv("WIDGET_API_ENABLED", "true")
	t.Setenv("WIDGET_API_TOKEN", strings.Repeat("a", 32))
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.WidgetAPI.Enabled || c.WidgetAPI.ListenAddress != ":8080" || c.WidgetAPI.PublicBaseURL != "https://usatenko-bf.com" || c.WidgetAPI.PairTTL.String() != "10m0s" {
		t.Fatalf("%+v", c.WidgetAPI)
	}
}

func TestWidgetAPIRejectsInsecurePublicURL(t *testing.T) {
	validEnv(t)
	t.Setenv("WIDGET_API_ENABLED", "true")
	t.Setenv("WIDGET_API_TOKEN", strings.Repeat("a", 32))
	t.Setenv("WIDGET_PUBLIC_BASE_URL", "http://example.com")
	if _, err := Load(); err == nil {
		t.Fatal("insecure widget public URL accepted")
	}
}

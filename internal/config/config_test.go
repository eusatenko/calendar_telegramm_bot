package config

import "testing"

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

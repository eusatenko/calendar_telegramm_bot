package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Calendar struct {
	Key, Name, URL, GoogleID string
}

type GoogleEditing struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	RefreshToken string
}

type Config struct {
	BotToken      string
	AdminID       int64
	Timezone      *time.Location
	TimezoneName  string
	DatabasePath  string
	CacheTTL      time.Duration
	HTTPTimeout   time.Duration
	InviteTTL     time.Duration
	Calendars     []Calendar
	GoogleEditing GoogleEditing
}

func Load() (Config, error) {
	admin, err := strconv.ParseInt(os.Getenv("ADMIN_TELEGRAM_USER_ID"), 10, 64)
	if err != nil || admin <= 0 {
		return Config{}, fmt.Errorf("ADMIN_TELEGRAM_USER_ID должен быть положительным числовым Telegram ID")
	}
	tzName := value("TIMEZONE", "Europe/Moscow")
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return Config{}, fmt.Errorf("некорректный TIMEZONE: %w", err)
	}
	c := Config{
		BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"), AdminID: admin,
		Timezone: loc, TimezoneName: tzName, DatabasePath: value("DATABASE_PATH", "/app/data/bot.db"),
	}
	if c.BotToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN не задан")
	}
	if c.CacheTTL, err = duration("ICAL_CACHE_TTL", 2*time.Minute); err != nil {
		return Config{}, err
	}
	if c.HTTPTimeout, err = duration("ICAL_HTTP_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if c.InviteTTL, err = duration("INVITE_TTL", 24*time.Hour); err != nil {
		return Config{}, err
	}
	c.Calendars = []Calendar{
		{"anya", "Аня", os.Getenv("CALENDAR_ANYA_ICAL_URL"), os.Getenv("GOOGLE_CALENDAR_ANYA_ID")},
		{"lesha", "Лёша", os.Getenv("CALENDAR_LESHA_ICAL_URL"), os.Getenv("GOOGLE_CALENDAR_LESHA_ID")},
		{"sasha", "Саша", os.Getenv("CALENDAR_SASHA_ICAL_URL"), os.Getenv("GOOGLE_CALENDAR_SASHA_ID")},
		{"nastya", "Настя", os.Getenv("CALENDAR_NASTYA_ICAL_URL"), os.Getenv("GOOGLE_CALENDAR_NASTYA_ID")},
	}
	for _, cal := range c.Calendars {
		u, parseErr := url.Parse(cal.URL)
		if cal.URL == "" || parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return Config{}, fmt.Errorf("CALENDAR_%s_ICAL_URL должен быть валидным HTTPS URL", cal.Key)
		}
	}
	c.GoogleEditing = GoogleEditing{
		ClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		RefreshToken: os.Getenv("GOOGLE_OAUTH_REFRESH_TOKEN"),
	}
	editingEnabled := false
	if raw := os.Getenv("CALENDAR_EDITING_ENABLED"); raw != "" {
		editingEnabled, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("CALENDAR_EDITING_ENABLED должен быть true или false")
		}
	}
	editingValues := []string{c.GoogleEditing.ClientID, c.GoogleEditing.ClientSecret, c.GoogleEditing.RefreshToken}
	configured := 0
	for _, v := range editingValues {
		if v != "" {
			configured++
		}
	}
	for _, cal := range c.Calendars {
		if cal.GoogleID != "" {
			configured++
		}
	}
	if editingEnabled {
		if configured != len(editingValues)+len(c.Calendars) {
			return Config{}, fmt.Errorf("для редактирования нужно задать все GOOGLE_OAUTH_* и GOOGLE_*_CALENDAR_ID")
		}
		c.GoogleEditing.Enabled = true
	}
	return c, nil
}

func value(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func duration(k string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(k)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s должен быть положительной duration: %q", k, v)
	}
	return d, nil
}

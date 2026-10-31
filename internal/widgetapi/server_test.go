package widgetapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
)

type fakeSource struct{ events []calendar.Event }

func (f fakeSource) Events(time.Time, time.Time) ([]calendar.Event, bool, error) {
	return f.events, false, nil
}

func TestPairingEndpointsArePublicAndOneTime(t *testing.T) {
	pairing := NewPairing(strings.Repeat("t", 64), "https://calendar.example", 10*time.Minute)
	link, _, err := pairing.CreatePairingURL(42)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(link)
	code := parsed.Query().Get("code")
	s := &Server{token: strings.Repeat("x", 32), loc: time.UTC, log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: time.Now, pairing: pairing}
	h := s.handler()

	setup := httptest.NewRecorder()
	h.ServeHTTP(setup, httptest.NewRequest(http.MethodGet, "/widget/setup?code="+url.QueryEscape(code), nil))
	if setup.Code != http.StatusOK || !strings.Contains(setup.Body.String(), "familycalendar://setup") {
		t.Fatalf("setup status=%d body=%s", setup.Code, setup.Body.String())
	}

	body, _ := json.Marshal(map[string]string{"code": code})
	exchange := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/widget/pair", bytes.NewReader(body)))
		return recorder
	}
	first := exchange()
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"server_url":"https://calendar.example"`) {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	if second := exchange(); second.Code != http.StatusGone {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestScheduleRequiresTokenAndReturnsMergedRows(t *testing.T) {
	loc := time.FixedZone("MSK", 3*60*60)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, loc)
	start := time.Date(2026, 10, 31, 9, 0, 0, 0, loc)
	people := []Person{
		{Key: "anya", Name: "Аня", Source: fakeSource{[]calendar.Event{{Summary: "Аня — Бассейн", Start: start, End: start.Add(45 * time.Minute)}}}},
		{Key: "lesha", Name: "Лёша", Source: fakeSource{[]calendar.Event{{Summary: "Лёша — Бассейн", Start: start, End: start.Add(45 * time.Minute)}}}},
	}
	s := &Server{token: strings.Repeat("x", 32), people: people, loc: loc, log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: func() time.Time { return now }}
	h := s.handler()

	unauthorized := httptest.NewRecorder()
	h.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/widget/schedule", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/widget/schedule?day=today&mode=chronological", nil)
	req.Header.Set("Authorization", "Bearer "+s.token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var got response
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Date != "2026-10-31" || len(got.Rows) != 1 || !strings.Contains(got.Rows[0].Text, "Аня, 🟣 Лёша — Бассейн") {
		t.Fatalf("%+v", got)
	}
}

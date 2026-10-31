package widgetapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
)

type fakeSource struct{ events []calendar.Event }

func (f fakeSource) Events(time.Time, time.Time) ([]calendar.Event, bool, error) {
	return f.events, false, nil
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

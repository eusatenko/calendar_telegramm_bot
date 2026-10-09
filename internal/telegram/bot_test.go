package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
	"github.com/eusatenko/calendar_telegramm_bot/internal/schedule"
	"github.com/eusatenko/calendar_telegramm_bot/internal/storage"
)

type captured struct {
	path string
	body map[string]any
}

func botFixture(t *testing.T) (*Bot, *storage.Store, *[]captured) {
	t.Helper()
	var mu sync.Mutex
	calls := []captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		calls = append(calls, captured{r.URL.Path, body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	t.Cleanup(srv.Close)
	store, e := storage.Open(filepath.Join(t.TempDir(), "bot.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	client := NewClient("test")
	client.base = srv.URL + "/"
	bot := NewBot(client, store, nil, time.UTC, time.Hour, "family_bot", slog.New(slog.NewTextHandler(io.Discard, nil)))
	return bot, store, &calls
}
func TestUnauthorizedCallbackCannotReceiveSchedule(t *testing.T) {
	bot, _, calls := botFixture(t)
	q := CallbackQuery{ID: "q", From: User{ID: 99}, Message: Message{MessageID: 7, Chat: Chat{ID: 99}}, Data: "all:today"}
	if e := bot.handleCallback(context.Background(), q); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, c := range *calls {
		if strings.HasSuffix(c.path, "editMessageText") && c.body["text"] == "Доступ не предоставлен." {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls=%+v", *calls)
	}
}
func TestMalformedAndAdminCallbackDeniedForUser(t *testing.T) {
	bot, store, calls := botFixture(t)
	store.BootstrapAdmin(1)
	store.AddUser(1, 2)
	for _, data := range []string{"person:unknown:today", "admin:users"} {
		q := CallbackQuery{ID: "q", From: User{ID: 2}, Message: Message{MessageID: 7, Chat: Chat{ID: 2}}, Data: data}
		if e := bot.handleCallback(context.Background(), q); e != nil {
			t.Fatal(e)
		}
	}
	texts := []string{}
	for _, c := range *calls {
		if v, ok := c.body["text"].(string); ok {
			texts = append(texts, v)
		}
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "Некорректная команда.") || !strings.Contains(joined, "Недостаточно прав.") {
		t.Fatalf("%s", joined)
	}
}
func TestMenusHideAdminForUser(t *testing.T) {
	normal, _ := json.Marshal(mainMenu(false))
	admin, _ := json.Marshal(mainMenu(true))
	if strings.Contains(string(normal), "Управление доступом") {
		t.Fatal("admin button visible")
	}
	if !strings.Contains(string(admin), "Управление доступом") {
		t.Fatal("admin button missing")
	}
}

func TestEditingButtonIsFeatureGated(t *testing.T) {
	disabled, _ := json.Marshal(adminMenu(false))
	enabled, _ := json.Marshal(adminMenu(true))
	if strings.Contains(string(disabled), "Изменить расписание") {
		t.Fatal("editing button visible while disabled")
	}
	if !strings.Contains(string(enabled), "Изменить расписание") {
		t.Fatal("editing button missing while enabled")
	}
}

type fixedSource struct{ events []calendar.Event }

func (s fixedSource) Events(time.Time, time.Time) ([]calendar.Event, bool, error) {
	return s.events, false, nil
}

type recordingEditor struct{ calls int }

func (e *recordingEditor) Apply(context.Context, schedule.Request) ([]schedule.CopyResult, error) {
	e.calls++
	return []schedule.CopyResult{{Key: "anya", Name: "Аня"}}, nil
}

func TestFindMatchingCopiesUsesExactTitleAndInterval(t *testing.T) {
	bot, _, _ := botFixture(t)
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	selected := calendar.Event{UID: "a", Summary: "Шахматы", Start: start, End: start.Add(time.Hour), Recurring: true, OriginalStart: start}
	bot.people = map[string]Person{
		"anya":  {Key: "anya", Name: "Аня", Source: fixedSource{[]calendar.Event{selected}}},
		"lesha": {Key: "lesha", Name: "Лёша", Source: fixedSource{[]calendar.Event{{UID: "b", Summary: "Шахматы", Start: start, End: start.Add(time.Hour), Recurring: true, OriginalStart: start}}}},
		"sasha": {Key: "sasha", Name: "Саша", Source: fixedSource{[]calendar.Event{{UID: "c", Summary: "Шахматы", Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), Recurring: true}}}},
	}
	bot.order = []string{"anya", "lesha", "sasha"}
	match, err := bot.findMatchingCopies(editSession{personKey: "anya", event: selected})
	if err != nil {
		t.Fatal(err)
	}
	if len(match.copies) != 2 || match.copies[0].ICalUID != "a" || match.copies[1].ICalUID != "b" || !match.exactTitle {
		t.Fatalf("match=%+v", match)
	}
}

func TestFindMatchingCopiesFallsBackToSameInterval(t *testing.T) {
	bot, _, _ := botFixture(t)
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	selected := calendar.Event{UID: "a", Summary: "Шахматы — Аня", Start: start, End: start.Add(time.Hour), Recurring: true, OriginalStart: start}
	bot.people = map[string]Person{
		"anya":  {Key: "anya", Name: "Аня", Source: fixedSource{[]calendar.Event{selected}}},
		"lesha": {Key: "lesha", Name: "Лёша", Source: fixedSource{[]calendar.Event{{UID: "b", Summary: "Шахматы — Лёша", Start: start, End: start.Add(time.Hour), Recurring: true, OriginalStart: start}}}},
		"sasha": {Key: "sasha", Name: "Саша", Source: fixedSource{[]calendar.Event{{UID: "c", Summary: "Другое время", Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), Recurring: true}}}},
	}
	bot.order = []string{"anya", "lesha", "sasha"}

	match, err := bot.findMatchingCopies(editSession{personKey: "anya", event: selected})
	if err != nil {
		t.Fatal(err)
	}
	if len(match.copies) != 2 || match.copies[1].ICalUID != "b" || match.exactTitle {
		t.Fatalf("match=%+v", match)
	}
	if match.events[1].Summary != "Шахматы — Лёша" {
		t.Fatalf("events=%+v", match.events)
	}
}

func TestFindMatchingCopiesRejectsAmbiguousInterval(t *testing.T) {
	bot, _, _ := botFixture(t)
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	selected := calendar.Event{UID: "a", Summary: "Занятие", Start: start, End: start.Add(time.Hour), Recurring: true}
	sameTime := func(uid, summary string) calendar.Event {
		return calendar.Event{UID: uid, Summary: summary, Start: start, End: start.Add(time.Hour), Recurring: true}
	}
	bot.people = map[string]Person{
		"anya":  {Key: "anya", Name: "Аня", Source: fixedSource{[]calendar.Event{selected}}},
		"lesha": {Key: "lesha", Name: "Лёша", Source: fixedSource{[]calendar.Event{sameTime("b", "Первое"), sameTime("c", "Второе")}}},
	}
	bot.order = []string{"anya", "lesha"}

	_, err := bot.findMatchingCopies(editSession{personKey: "anya", event: selected})
	if !errors.Is(err, errAmbiguousCopyMatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestConfirmLinksSingleEventAndAppliesEdit(t *testing.T) {
	bot, store, _ := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	editor := &recordingEditor{}
	bot.EnableScheduleEditing(editor)
	title := "Новое название"
	token := "test-token"
	bot.editSessions[token] = editSession{
		expires:     time.Now().Add(time.Minute),
		actor:       1,
		personKey:   "anya",
		candidates:  []storage.EventCopy{{CalendarKey: "anya", ICalUID: "uid-a"}},
		targetsText: "• Аня (только выбранное событие)",
		needsLink:   true,
		pending:     &schedule.Request{Actor: 1, SourceKey: "anya", SourceICalUID: "uid-a", Summary: &title},
	}
	q := CallbackQuery{ID: "q", From: User{ID: 1}, Message: Message{MessageID: 7, Chat: Chat{ID: 1}}}
	if err := bot.scheduleEdit(context.Background(), q, []string{"edit", "confirm", token}); err != nil {
		t.Fatal(err)
	}
	linked, err := store.EventGroup("anya", "uid-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 || editor.calls != 1 {
		t.Fatalf("linked=%+v editor_calls=%d", linked, editor.calls)
	}
}

func TestParseTimeRangeSupportsOvernight(t *testing.T) {
	start, end, err := parseTimeRange("23:30–01:00", time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if start.Format("2006-01-02 15:04") != "2026-10-12 23:30" || end.Format("2006-01-02 15:04") != "2026-10-13 01:00" {
		t.Fatalf("%v - %v", start, end)
	}
}

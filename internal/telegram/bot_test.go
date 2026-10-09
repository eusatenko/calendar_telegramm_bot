package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	disabled, _ := json.Marshal(mainMenu(false, false))
	enabled, _ := json.Marshal(mainMenu(false, true))
	adminEnabled := mainMenu(true, true)
	if strings.Contains(string(disabled), "Изменить расписание") {
		t.Fatal("editing button visible while disabled")
	}
	if !strings.Contains(string(enabled), "Изменить расписание") {
		t.Fatal("editing button missing for authorized user while enabled")
	}
	if !strings.Contains(string(enabled), "Добавить событие") {
		t.Fatal("create button missing for authorized user")
	}
	if len(adminEnabled.InlineKeyboard) < 4 || len(adminEnabled.InlineKeyboard[3]) != 2 || adminEnabled.InlineKeyboard[3][0].CallbackData != "edit:menu" || adminEnabled.InlineKeyboard[3][1].CallbackData != "create:menu" {
		t.Fatalf("calendar actions are not adjacent: %+v", adminEnabled.InlineKeyboard)
	}
	admin, _ := json.Marshal(adminMenu())
	if strings.Contains(string(admin), "Изменить расписание") || strings.Contains(string(admin), "Добавить событие") {
		t.Fatal("admin menu buttons are incorrect")
	}
}

func TestAuthorizedUserCanOpenCreation(t *testing.T) {
	bot, store, calls := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	if err := store.AddUser(1, 2); err != nil {
		t.Fatal(err)
	}
	bot.EnableScheduleEditing(&recordingEditor{})
	q := CallbackQuery{ID: "q", From: User{ID: 2}, Message: Message{MessageID: 7, Chat: Chat{ID: 2}}, Data: "create:menu"}
	if err := bot.handleCallback(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range *calls {
		if call.body["text"] == "Кому добавить событие? Выберите один или несколько календарей." {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls=%+v", *calls)
	}
}

func TestAuthorizedUserCanOpenEditing(t *testing.T) {
	bot, store, calls := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	if err := store.AddUser(1, 2); err != nil {
		t.Fatal(err)
	}
	bot.EnableScheduleEditing(&recordingEditor{})
	q := CallbackQuery{ID: "q", From: User{ID: 2}, Message: Message{MessageID: 7, Chat: Chat{ID: 2}}, Data: "edit:menu"}
	if err := bot.handleCallback(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range *calls {
		if call.body["text"] == "Чьё расписание открыть?" {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls=%+v", *calls)
	}
}

type fixedSource struct{ events []calendar.Event }

func (s fixedSource) Events(time.Time, time.Time) ([]calendar.Event, bool, error) {
	return s.events, false, nil
}

type recordingEditor struct {
	calls       int
	lastRequest schedule.Request
}

func (e *recordingEditor) Apply(_ context.Context, request schedule.Request) ([]schedule.CopyResult, error) {
	e.calls++
	e.lastRequest = request
	return []schedule.CopyResult{{Key: "anya", Name: "Аня"}}, nil
}
func (e *recordingEditor) Create(context.Context, schedule.CreateRequest) ([]schedule.CopyResult, error) {
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

func TestSelectedCreateKeysPreserveCalendarOrder(t *testing.T) {
	session := createSession{targets: map[string]bool{"sasha": true, "anya": true}}
	got := selectedCreateKeys(session, []string{"anya", "lesha", "sasha", "nastya"})
	if strings.Join(got, ",") != "anya,sasha" {
		t.Fatalf("keys=%v", got)
	}
}

func TestParseCustomDateUsesConfiguredTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseDate("10.10.2026", loc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format(time.RFC3339) != "2026-10-10T00:00:00+03:00" {
		t.Fatalf("date=%s", got.Format(time.RFC3339))
	}
}

func TestEditTargetCheckboxesContainOnlyMatchedCalendars(t *testing.T) {
	session := editSession{
		candidates:      []storage.EventCopy{{CalendarKey: "anya", ICalUID: "a"}, {CalendarKey: "lesha", ICalUID: "b"}},
		selectedKeys:    []string{"anya"},
		candidateLabels: map[string]string{"anya": "Аня: Танцы", "lesha": "Лёша: Танцы"},
	}
	encoded, err := json.Marshal(editTargetsMarkup("token", session, []string{"anya", "lesha", "sasha"}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, "☑ Аня: Танцы") || !strings.Contains(text, "☐ Лёша: Танцы") || strings.Contains(text, "sasha") {
		t.Fatalf("markup=%s", text)
	}
}

func TestRecurringLocationScopeIsAccepted(t *testing.T) {
	bot, _, calls := botFixture(t)
	token := "location-token"
	bot.editSessions[token] = editSession{expires: time.Now().Add(time.Minute), actor: 1, event: calendar.Event{Recurring: true}}
	q := CallbackQuery{ID: "q", From: User{ID: 1}, Message: Message{MessageID: 7, Chat: Chat{ID: 1}}}
	if err := bot.scheduleEdit(context.Background(), q, []string{"edit", "scope", token, "location", "series"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range *calls {
		if strings.Contains(fmt.Sprint(call.body["text"]), "новое место") {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls=%+v", *calls)
	}
}

func TestDeleteRequiresExplicitConfirmationAndDoesNotLinkCandidates(t *testing.T) {
	bot, store, calls := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	editor := &recordingEditor{}
	bot.EnableScheduleEditing(editor)
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	token := "delete-token"
	bot.editSessions[token] = editSession{
		expires:      time.Now().Add(time.Minute),
		actor:        1,
		personKey:    "anya",
		event:        calendar.Event{UID: "uid-a", Summary: "Танцы", Start: start, End: start.Add(time.Hour), OriginalStart: start},
		candidates:   []storage.EventCopy{{CalendarKey: "anya", ICalUID: "uid-a"}},
		selectedKeys: []string{"anya"},
		targetsText:  "• Аня: Танцы",
		needsLink:    true,
	}
	q := CallbackQuery{ID: "q", From: User{ID: 1}, Message: Message{MessageID: 7, Chat: Chat{ID: 1}}}
	if err := bot.scheduleEdit(context.Background(), q, []string{"edit", "delete", token}); err != nil {
		t.Fatal(err)
	}
	if editor.calls != 0 {
		t.Fatalf("delete executed before confirmation: calls=%d", editor.calls)
	}
	previewFound := false
	for _, call := range *calls {
		if strings.Contains(fmt.Sprint(call.body["text"]), "Подтвердите удаление") {
			previewFound = true
		}
	}
	if !previewFound {
		t.Fatalf("confirmation preview missing: calls=%+v", *calls)
	}
	if err := bot.scheduleEdit(context.Background(), q, []string{"edit", "confirm", token}); err != nil {
		t.Fatal(err)
	}
	if editor.calls != 1 || !editor.lastRequest.Delete || editor.lastRequest.Scope != "occurrence" || !editor.lastRequest.UnlinkOnDelete {
		t.Fatalf("calls=%d request=%+v", editor.calls, editor.lastRequest)
	}
	linked, err := store.EventGroup("anya", "uid-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 0 {
		t.Fatalf("delete unexpectedly linked candidates: %+v", linked)
	}
}

func TestRecurringDeleteOffersOccurrenceAndSeries(t *testing.T) {
	bot, _, calls := botFixture(t)
	token := "recurring-delete-token"
	bot.editSessions[token] = editSession{expires: time.Now().Add(time.Minute), actor: 1, event: calendar.Event{Recurring: true}, selectedKeys: []string{"anya"}}
	q := CallbackQuery{ID: "q", From: User{ID: 1}, Message: Message{MessageID: 7, Chat: Chat{ID: 1}}}
	if err := bot.scheduleEdit(context.Background(), q, []string{"edit", "delete", token}); err != nil {
		t.Fatal(err)
	}
	var rendered []string
	for _, call := range *calls {
		rendered = append(rendered, fmt.Sprint(call.body))
	}
	text := strings.Join(rendered, "\n")
	if !strings.Contains(text, "Только этот экземпляр") || !strings.Contains(text, "Всю серию") {
		t.Fatalf("scope choices missing: %s", text)
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

func TestAdminConfiguresNotificationGroupWithCommand(t *testing.T) {
	bot, store, calls := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	message := Message{From: User{ID: 1}, Chat: Chat{ID: -100123, Type: "supergroup", Title: "Семья"}, Text: "/notifications_here@family_bot"}
	if err := bot.handleMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	chatID, configured, err := store.NotificationChat()
	if err != nil || !configured || chatID != message.Chat.ID {
		t.Fatalf("chat_id=%d configured=%v err=%v", chatID, configured, err)
	}
	if len(*calls) == 0 || !strings.Contains(fmt.Sprint((*calls)[len(*calls)-1].body["text"]), "Уведомления") {
		t.Fatalf("calls=%+v", *calls)
	}
}

func TestNormalUserCannotConfigureNotificationGroup(t *testing.T) {
	bot, store, _ := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	if err := store.AddUser(1, 2); err != nil {
		t.Fatal(err)
	}
	message := Message{From: User{ID: 2}, Chat: Chat{ID: -100123, Type: "supergroup"}, Text: "/notifications_here"}
	if err := bot.handleMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if _, configured, err := store.NotificationChat(); err != nil || configured {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
}

func TestNotificationCommandRejectsPrivateChat(t *testing.T) {
	bot, store, _ := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	message := Message{From: User{ID: 1}, Chat: Chat{ID: 1, Type: "private"}, Text: "/notifications_here"}
	if err := bot.handleMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if _, configured, err := store.NotificationChat(); err != nil || configured {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
}

func TestCreateNotificationIsSentToConfiguredGroup(t *testing.T) {
	bot, store, calls := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNotificationChat(1, -100123); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	session := createSession{summary: "Танцы", location: "Школа", start: start, end: start.Add(time.Hour)}
	results := []schedule.CopyResult{{Key: "anya", Name: "Аня"}, {Key: "lesha", Name: "Лёша", Err: errors.New("temporary")}}
	if err := bot.notifyCreate(context.Background(), User{ID: 2, FirstName: "Евгений"}, session, results); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls=%+v", *calls)
	}
	body := (*calls)[0].body
	text := fmt.Sprint(body["text"])
	chatID, ok := body["chat_id"].(float64)
	if !ok || int64(chatID) != -100123 || !strings.Contains(text, "Событие добавлено") || !strings.Contains(text, "Аня ✅") || !strings.Contains(text, "Лёша ❌") || !strings.Contains(text, "Изменил: Евгений") {
		t.Fatalf("body=%+v", body)
	}
}

func TestAllFailedMutationDoesNotNotifyFamily(t *testing.T) {
	bot, store, calls := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNotificationChat(1, -100123); err != nil {
		t.Fatal(err)
	}
	results := []schedule.CopyResult{{Key: "anya", Name: "Аня", Err: errors.New("temporary")}}
	if err := bot.notifyEdit(context.Background(), User{ID: 1}, editSession{}, schedule.Request{}, results); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected notification: %+v", *calls)
	}
}

func TestBotCommandIgnoresAnotherBotMention(t *testing.T) {
	if got := botCommand("/notifications_here@another_bot", "family_bot"); got != "" {
		t.Fatalf("command=%q", got)
	}
}

func TestEmptyMarkupIsSerializedAsTelegramArray(t *testing.T) {
	bot, store, calls := botFixture(t)
	if err := store.BootstrapAdmin(1); err != nil {
		t.Fatal(err)
	}
	message := Message{From: User{ID: 1}, Chat: Chat{ID: -100123, Type: "supergroup"}, Text: "/notifications_here"}
	if err := bot.handleMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls=%+v", *calls)
	}
	replyMarkup, ok := (*calls)[0].body["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("reply_markup=%#v", (*calls)[0].body["reply_markup"])
	}
	keyboard, ok := replyMarkup["inline_keyboard"].([]any)
	if !ok || len(keyboard) != 0 {
		t.Fatalf("inline_keyboard=%#v", replyMarkup["inline_keyboard"])
	}
}

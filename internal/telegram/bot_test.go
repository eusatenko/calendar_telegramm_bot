package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

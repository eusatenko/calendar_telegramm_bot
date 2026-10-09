package storage

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "bot.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestBootstrapAuthorizationAndFinalAdmin(t *testing.T) {
	s := openTest(t)
	if e := s.BootstrapAdmin(1); e != nil {
		t.Fatal(e)
	}
	ok, admin, e := s.Authorized(1)
	if e != nil || !ok || !admin {
		t.Fatalf("%v %v %v", ok, admin, e)
	}
	if e = s.SetActive(1, 1, false); !errors.Is(e, ErrLastAdmin) {
		t.Fatalf("got %v", e)
	}
	if e = s.BootstrapAdmin(1); e != nil {
		t.Fatal(e)
	}
	users, _ := s.ListUsers()
	if len(users) != 1 {
		t.Fatalf("duplicates: %d", len(users))
	}
}
func TestAddReactivateAndAdminGuard(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	if e := s.AddUser(2, 3); e == nil {
		t.Fatal("non-admin allowed")
	}
	if e := s.AddUser(1, 2); e != nil {
		t.Fatal(e)
	}
	if e := s.SetActive(1, 2, false); e != nil {
		t.Fatal(e)
	}
	if e := s.AddUser(1, 2); e != nil {
		t.Fatal(e)
	}
	u, _ := s.GetUser(2)
	if !u.Active || u.Role != "user" {
		t.Fatalf("%+v", u)
	}
}
func TestInviteOneTimeHashedAndConcurrent(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	token, e := s.CreateInvite(1, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	var stored string
	if e = s.db.QueryRow(`SELECT token_hash FROM invites`).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if stored == token || stored != hashToken(token) {
		t.Fatal("token not hashed")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	for _, id := range []int64{2, 3} {
		go func(id int64) { defer wg.Done(); errs <- s.RedeemInvite(token, id, "", "") }(id)
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("success=%d", success)
	}
}
func TestExpiredInvite(t *testing.T) {
	s := openTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.BootstrapAdmin(1)
	token, e := s.CreateInvite(1, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	s.now = func() time.Time { return now.Add(2 * time.Minute) }
	if e = s.RedeemInvite(token, 2, "", ""); !errors.Is(e, ErrInviteInvalid) {
		t.Fatalf("%v", e)
	}
}

func TestDatabasePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestLinkAndLoadEventCopies(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	copies := []EventCopy{{CalendarKey: "anya", ICalUID: "uid-a"}, {CalendarKey: "lesha", ICalUID: "uid-b"}}
	if err := s.LinkEventCopies(1, copies); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventGroup("anya", "uid-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].CalendarKey != "anya" || got[1].ICalUID != "uid-b" {
		t.Fatalf("copies=%+v", got)
	}
	if err = s.LinkEventCopies(1, copies); err == nil {
		t.Fatal("duplicate event was linked twice")
	}
	if err = s.LinkEventCopies(2, []EventCopy{{CalendarKey: "sasha", ICalUID: "x"}, {CalendarKey: "nastya", ICalUID: "y"}}); err == nil {
		t.Fatal("unauthorized user linked events")
	}
}

func TestActiveUserCanLinkAndAuditEventEdit(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	if err := s.AddUser(1, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkEventCopies(2, []EventCopy{{CalendarKey: "anya", ICalUID: "user-event"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordEventEdit(2, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordEventCreate(2, 1, 0); err != nil {
		t.Fatal(err)
	}
}

func TestLinkAndLoadSingleEvent(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	if err := s.LinkEventCopies(1, []EventCopy{{CalendarKey: "anya", ICalUID: "uid-a"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventGroup("anya", "uid-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CalendarKey != "anya" || got[0].ICalUID != "uid-a" {
		t.Fatalf("copies=%+v", got)
	}
}

func TestActiveUserCanUnlinkDeletedSeriesCopyAndAuditDeletion(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	if err := s.AddUser(1, 2); err != nil {
		t.Fatal(err)
	}
	copies := []EventCopy{{CalendarKey: "anya", ICalUID: "uid-a"}, {CalendarKey: "lesha", ICalUID: "uid-b"}}
	if err := s.LinkEventCopies(2, copies); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkEventCopies(2, copies[1:]); err != nil {
		t.Fatal(err)
	}
	remaining, err := s.EventGroup("anya", "uid-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0] != copies[0] {
		t.Fatalf("remaining=%+v", remaining)
	}
	if err = s.RecordEventDelete(2, 1, 0); err != nil {
		t.Fatal(err)
	}
	var action, metadata string
	if err = s.db.QueryRow(`SELECT action,metadata FROM audit_log WHERE action='EVENT_DELETED'`).Scan(&action, &metadata); err != nil {
		t.Fatal(err)
	}
	if action != "EVENT_DELETED" || metadata != `{"copy_count":1,"failure_count":0}` {
		t.Fatalf("action=%q metadata=%q", action, metadata)
	}
}

func TestUnauthorizedUserCannotUnlinkEventCopy(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	copy := EventCopy{CalendarKey: "anya", ICalUID: "uid-a"}
	if err := s.LinkEventCopies(1, []EventCopy{copy}); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkEventCopies(99, []EventCopy{copy}); err == nil {
		t.Fatal("unauthorized user unlinked an event")
	}
	remaining, err := s.EventGroup(copy.CalendarKey, copy.ICalUID)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("remaining=%+v err=%v", remaining, err)
	}
}

func TestNotificationChatCanOnlyBeManagedByAdmin(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	if err := s.AddUser(1, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNotificationChat(2, -100123); err == nil {
		t.Fatal("non-admin configured notification chat")
	}
	if err := s.SetNotificationChat(1, -100123); err != nil {
		t.Fatal(err)
	}
	chatID, configured, err := s.NotificationChat()
	if err != nil || !configured || chatID != -100123 {
		t.Fatalf("chat_id=%d configured=%v err=%v", chatID, configured, err)
	}
	if err = s.ClearNotificationChat(2); err == nil {
		t.Fatal("non-admin cleared notification chat")
	}
	if err = s.ClearNotificationChat(1); err != nil {
		t.Fatal(err)
	}
	if _, configured, err = s.NotificationChat(); err != nil || configured {
		t.Fatalf("configured=%v err=%v", configured, err)
	}
}

func TestNotificationChatRejectsPrivateChatID(t *testing.T) {
	s := openTest(t)
	s.BootstrapAdmin(1)
	if err := s.SetNotificationChat(1, 123); err == nil {
		t.Fatal("private chat was accepted as notification group")
	}
}

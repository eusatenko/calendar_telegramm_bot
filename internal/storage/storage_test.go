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

package cache

import (
	"errors"
	calendarical "github.com/eusatenko/calendar_telegramm_bot/internal/calendar/ical"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testCalendar(t *testing.T) *calendarical.Calendar {
	t.Helper()
	c, e := calendarical.Parse(strings.NewReader("BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:x\nDTSTART:20260907T100000Z\nDTEND:20260907T110000Z\nSUMMARY:X\nEND:VEVENT\nEND:VCALENDAR\n"), time.UTC)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestHitExpiredStaleAndFirstFailure(t *testing.T) {
	cal := testCalendar(t)
	now := time.Now()
	var calls atomic.Int32
	fail := false
	s := New(time.Minute, func() (*calendarical.Calendar, error) {
		calls.Add(1)
		if fail {
			return nil, errors.New("down")
		}
		return cal, nil
	})
	s.now = func() time.Time { return now }
	from := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	if _, stale, e := s.Events(from, to); e != nil || stale {
		t.Fatal(e)
	}
	if _, _, e := s.Events(from, to); e != nil || calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	now = now.Add(2 * time.Minute)
	fail = true
	if _, stale, e := s.Events(from, to); e != nil || !stale {
		t.Fatalf("stale=%v err=%v", stale, e)
	}
	first := New(time.Minute, func() (*calendarical.Calendar, error) { return nil, errors.New("down") })
	if _, _, e := first.Events(from, to); e == nil {
		t.Fatal("expected failure")
	}
}
func TestConcurrentRefreshCoalesced(t *testing.T) {
	cal := testCalendar(t)
	var calls atomic.Int32
	gate := make(chan struct{})
	s := New(time.Minute, func() (*calendarical.Calendar, error) { calls.Add(1); <-gate; return cal, nil })
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _ = s.Events(time.Now(), time.Now().Add(time.Hour)) }()
	}
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(gate)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

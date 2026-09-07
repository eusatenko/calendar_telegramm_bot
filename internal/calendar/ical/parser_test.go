package ical

import (
	"strings"
	"testing"
	"time"
)

func expandFixture(t *testing.T, body, from, to string) []string {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Moscow")
	f, _ := time.ParseInLocation("2006-01-02", from, loc)
	e, _ := time.ParseInLocation("2006-01-02", to, loc)
	events, err := ParseAndExpand(strings.NewReader("BEGIN:VCALENDAR\n"+body+"END:VCALENDAR\n"), f, e, loc)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(events))
	for i, v := range events {
		out[i] = v.Start.In(loc).Format("2006-01-02 15:04") + "|" + v.End.In(loc).Format("2006-01-02 15:04") + "|" + v.Summary
	}
	return out
}
func TestSimpleUTCAndFloatingAndTZID(t *testing.T) {
	body := `BEGIN:VEVENT
UID:a
DTSTART:20260907T130000Z
DTEND:20260907T140000Z
SUMMARY:UTC
END:VEVENT
BEGIN:VEVENT
UID:b
DTSTART:20260907T180000
DTEND:20260907T190000
SUMMARY:Floating
END:VEVENT
BEGIN:VEVENT
UID:c
DTSTART;TZID=Europe/Moscow:20260907T200000
DTEND;TZID=Europe/Moscow:20260907T210000
SUMMARY:TZID
END:VEVENT
`
	got := expandFixture(t, body, "2026-09-07", "2026-09-08")
	if len(got) != 3 || got[0] != "2026-09-07 16:00|2026-09-07 17:00|UTC" || got[2] != "2026-09-07 20:00|2026-09-07 21:00|TZID" {
		t.Fatalf("%v", got)
	}
}
func TestAllDayMultiDayAndTimedOverlap(t *testing.T) {
	body := `BEGIN:VEVENT
UID:a
DTSTART;VALUE=DATE:20260907
DTEND;VALUE=DATE:20260909
SUMMARY:Каникулы
END:VEVENT
BEGIN:VEVENT
UID:b
DTSTART:20260906T233000Z
DTEND:20260907T220000Z
SUMMARY:Ночная смена
END:VEVENT
`
	got := expandFixture(t, body, "2026-09-08", "2026-09-09")
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if got[0] != "2026-09-07 00:00|2026-09-09 00:00|Каникулы" {
		t.Fatalf("all-day order/interval: %v", got)
	}
}
func TestRecurrenceExdateRdateOverride(t *testing.T) {
	body := `BEGIN:VEVENT
UID:r
DTSTART;TZID=Europe/Moscow:20260907T100000
DTEND;TZID=Europe/Moscow:20260907T110000
RRULE:FREQ=DAILY;COUNT=4
EXDATE;TZID=Europe/Moscow:20260908T100000
RDATE;TZID=Europe/Moscow:20260912T100000
SUMMARY:Оригинал
END:VEVENT
BEGIN:VEVENT
UID:r
RECURRENCE-ID;TZID=Europe/Moscow:20260909T100000
DTSTART;TZID=Europe/Moscow:20260909T140000
DTEND;TZID=Europe/Moscow:20260909T150000
SUMMARY:Перенос
END:VEVENT
`
	got := expandFixture(t, body, "2026-09-07", "2026-09-14")
	want := []string{"2026-09-07 10:00|2026-09-07 11:00|Оригинал", "2026-09-09 14:00|2026-09-09 15:00|Перенос", "2026-09-10 10:00|2026-09-10 11:00|Оригинал", "2026-09-12 10:00|2026-09-12 11:00|Оригинал"}
	if strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("got %v", got)
	}
}
func TestWeeklyByDayUntilAndCancelledOverride(t *testing.T) {
	body := `BEGIN:VEVENT
UID:w
DTSTART;TZID=Europe/Moscow:20260907T100000
DTEND;TZID=Europe/Moscow:20260907T110000
RRULE:FREQ=WEEKLY;BYDAY=MO,WE;UNTIL=20260916T070000Z
SUMMARY:Спорт
END:VEVENT
BEGIN:VEVENT
UID:w
RECURRENCE-ID;TZID=Europe/Moscow:20260909T100000
DTSTART;TZID=Europe/Moscow:20260909T100000
DTEND;TZID=Europe/Moscow:20260909T110000
STATUS:CANCELLED
SUMMARY:Спорт
END:VEVENT
`
	got := expandFixture(t, body, "2026-09-07", "2026-09-18")
	if len(got) != 3 {
		t.Fatalf("expected 3, got %v", got)
	}
}
func TestMalformedEvent(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	_, err := ParseAndExpand(strings.NewReader("BEGIN:VEVENT\nUID:x\nEND:VEVENT\n"), time.Now(), time.Now().Add(time.Hour), loc)
	if err == nil {
		t.Fatal("expected error")
	}
}

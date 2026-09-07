package formatter

import (
	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
	"strings"
	"testing"
	"time"
)

func TestDayAllDayFirstRussian(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	d := time.Date(2026, 9, 7, 0, 0, 0, 0, loc)
	events := []calendar.Event{{Summary: "Шахматы", Start: d.Add(18 * time.Hour), End: d.Add(19 * time.Hour)}, {Summary: "Каникулы", Start: d, End: d.AddDate(0, 0, 1), AllDay: true}}
	got := Day("Аня", d, events, false)
	want := "Аня — понедельник, 7 сентября\n\nВесь день — Каникулы\n18:00–19:00 Шахматы"
	if got != want {
		t.Fatalf("%q", got)
	}
}
func TestEmptyAndStale(t *testing.T) {
	d := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	got := Day("Аня", d, nil, true)
	if !strings.Contains(got, "Нет занятий") || !strings.Contains(got, "могут быть неактуальны") {
		t.Fatal(got)
	}
}
func TestSplit(t *testing.T) {
	parts := Split(strings.Repeat("я", 25), 10)
	if len(parts) != 3 {
		t.Fatalf("%d %#v", len(parts), parts)
	}
	for _, p := range parts {
		if len([]rune(p)) > 10 {
			t.Fatal("too long")
		}
	}
}

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

func TestCombinedMergesMatchingEventsAndOmitsConsumedSection(t *testing.T) {
	d := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	event := func(summary string, hour, minute, duration int) calendar.Event {
		start := d.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
		return calendar.Event{Summary: summary, Start: start, End: start.Add(time.Duration(duration) * time.Minute)}
	}
	got := Combined("Завтра — расписание всех", d, []PersonEvents{
		{Name: "Аня", Events: []calendar.Event{event("Аня — Бассейн", 9, 0, 45), event("Аня — Современный", 11, 50, 60), event("Аня — Репертуар", 13, 20, 60)}},
		{Name: "Лёша", Events: []calendar.Event{event("Лёша — Бассейн", 9, 0, 45), event("Лёша — Современный", 13, 0, 60), event("Лёша — Репертуар", 14, 30, 60)}},
		{Name: "Саша", Events: []calendar.Event{event("Саша — Бассейн", 9, 0, 45), event("Саша — Современный", 13, 0, 60), event("Саша — Репертуар", 14, 30, 60)}},
		{Name: "Настя", Events: []calendar.Event{event("Настя — спираль", 10, 0, 60)}},
	})
	want := "Завтра — расписание всех\n\n09:00–09:45 Аня, Лёша, Саша — Бассейн\n\nАня\n11:50–12:50 Аня — Современный\n13:20–14:20 Аня — Репертуар\n\nЛёша, Саша\n13:00–14:00 Лёша, Саша — Современный\n14:30–15:30 Лёша, Саша — Репертуар\n\nНастя\n10:00–11:00 Настя — спираль"
	if got != want {
		t.Fatalf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestCombinedDoesNotMergeSameTitleAtDifferentTimes(t *testing.T) {
	d := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	got := Combined("Расписание", d, []PersonEvents{
		{Name: "Аня", Events: []calendar.Event{{Summary: "Аня — Танцы", Start: d.Add(10 * time.Hour), End: d.Add(11 * time.Hour)}}},
		{Name: "Лёша", Events: []calendar.Event{{Summary: "Лёша — Танцы", Start: d.Add(11 * time.Hour), End: d.Add(12 * time.Hour)}}},
	})
	if strings.Contains(got, "Аня, Лёша") {
		t.Fatalf("different times were merged:\n%s", got)
	}
}

func TestCombinedKeepsEmptyCalendars(t *testing.T) {
	d := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	got := Combined("Расписание", d, []PersonEvents{{Name: "Аня"}})
	if !strings.Contains(got, "Аня\nНет занятий") {
		t.Fatal(got)
	}
}

func TestCombinedKeepsPersonalHeaderForMixedSection(t *testing.T) {
	d := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	sharedStart := d.Add(10 * time.Hour)
	got := Combined("Расписание", d, []PersonEvents{
		{Name: "Лёша", Events: []calendar.Event{{Summary: "Лёша — Танцы", Start: sharedStart, End: sharedStart.Add(time.Hour)}, {Summary: "Лёша — Шахматы", Start: d.Add(12 * time.Hour), End: d.Add(13 * time.Hour)}}},
		{Name: "Саша", Events: []calendar.Event{{Summary: "Саша — Танцы", Start: sharedStart, End: sharedStart.Add(time.Hour)}}},
	})
	if strings.Contains(got, "\n\nЛёша, Саша\n") || !strings.Contains(got, "\n\nЛёша\n") {
		t.Fatal(got)
	}
}

func TestCombinedChronologicalMergesAndSortsAllEvents(t *testing.T) {
	d := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	event := func(summary string, hour, minute, duration int) calendar.Event {
		start := d.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
		return calendar.Event{Summary: summary, Start: start, End: start.Add(time.Duration(duration) * time.Minute)}
	}
	got := CombinedChronological("Завтра — расписание всех", d, []PersonEvents{
		{Name: "Аня", Events: []calendar.Event{event("Аня — Бассейн", 9, 0, 45), event("Аня — Современный", 11, 50, 60)}},
		{Name: "Лёша", Events: []calendar.Event{event("Лёша — Бассейн", 9, 0, 45), event("Лёша — Репертуар", 14, 30, 60)}},
		{Name: "Настя", Events: []calendar.Event{event("Настя — спираль", 10, 0, 60)}},
	})
	want := "Завтра — расписание всех\n\n09:00–09:45 Аня, Лёша — Бассейн\n10:00–11:00 Настя — спираль\n11:50–12:50 Аня — Современный\n14:30–15:30 Лёша — Репертуар"
	if got != want {
		t.Fatalf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestCombinedChronologicalKeepsCalendarWarnings(t *testing.T) {
	d := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	got := CombinedChronological("Расписание", d, []PersonEvents{{Name: "Аня", Error: true}, {Name: "Лёша", Stale: true}})
	if !strings.Contains(got, "Нет занятий") || !strings.Contains(got, "Аня: не удалось") || !strings.Contains(got, "Лёша: данные могут") {
		t.Fatal(got)
	}
}

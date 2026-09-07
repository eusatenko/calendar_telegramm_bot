package formatter

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
)

var weekdays = []string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}
var weekdaysTitle = []string{"Воскресенье", "Понедельник", "Вторник", "Среда", "Четверг", "Пятница", "Суббота"}
var months = []string{"", "января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

func Day(person string, dayStart time.Time, events []calendar.Event, stale bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s, %d %s\n\n", person, weekdays[dayStart.Weekday()], dayStart.Day(), months[dayStart.Month()])
	b.WriteString(dayLines(dayStart, events))
	if stale {
		b.WriteString("\n\n⚠️ Данные могут быть неактуальны: не удалось обновить календарь.")
	}
	return b.String()
}

func Week(person string, start time.Time, events []calendar.Event, stale bool) string {
	end := start.AddDate(0, 0, 6)
	var b strings.Builder
	if start.Month() == end.Month() {
		fmt.Fprintf(&b, "%s — %d–%d %s", person, start.Day(), end.Day(), months[end.Month()])
	} else {
		fmt.Fprintf(&b, "%s — %d %s – %d %s", person, start.Day(), months[start.Month()], end.Day(), months[end.Month()])
	}
	for i := 0; i < 7; i++ {
		day := start.AddDate(0, 0, i)
		fmt.Fprintf(&b, "\n\n%s\n%s", weekdaysTitle[day.Weekday()], dayLines(day, events))
	}
	if stale {
		b.WriteString("\n\n⚠️ Данные могут быть неактуальны: не удалось обновить календарь.")
	}
	return b.String()
}

func Combined(title string, start time.Time, people []PersonEvents) string {
	var b strings.Builder
	b.WriteString(title)
	for _, p := range people {
		fmt.Fprintf(&b, "\n\n%s\n%s", p.Name, dayLines(start, p.Events))
		if p.Error {
			b.WriteString("\n⚠️ Не удалось получить расписание.")
		} else if p.Stale {
			b.WriteString("\n⚠️ Данные могут быть неактуальны.")
		}
	}
	return b.String()
}

type PersonEvents struct {
	Name         string
	Events       []calendar.Event
	Stale, Error bool
}

func dayLines(day time.Time, events []calendar.Event) string {
	end := day.AddDate(0, 0, 1)
	var list []calendar.Event
	for _, e := range events {
		if calendar.Overlaps(e, day, end) {
			list = append(list, e)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].AllDay != list[j].AllDay {
			return list[i].AllDay
		}
		return list[i].Start.Before(list[j].Start)
	})
	if len(list) == 0 {
		return "Нет занятий"
	}
	lines := make([]string, 0, len(list))
	for _, e := range list {
		if e.AllDay {
			lines = append(lines, "Весь день — "+safeSummary(e.Summary))
			continue
		}
		s := e.Start.In(day.Location())
		en := e.End.In(day.Location())
		lines = append(lines, fmt.Sprintf("%s–%s %s", s.Format("15:04"), en.Format("15:04"), safeSummary(e.Summary)))
	}
	return strings.Join(lines, "\n")
}
func safeSummary(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Без названия"
	}
	return strings.TrimSpace(s)
}

func Split(text string, limit int) []string {
	if len([]rune(text)) <= limit {
		return []string{text}
	}
	paragraphs := strings.Split(text, "\n\n")
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, p := range paragraphs {
		r := []rune(p)
		if len(r) > limit {
			flush()
			for len(r) > limit {
				out = append(out, string(r[:limit]))
				r = r[limit:]
			}
			if len(r) > 0 {
				cur.WriteString(string(r))
			}
			continue
		}
		sep := 0
		if cur.Len() > 0 {
			sep = 2
		}
		if len([]rune(cur.String()))+sep+len(r) > limit {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
	}
	flush()
	return out
}

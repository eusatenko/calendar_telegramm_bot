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
	fmt.Fprintf(&b, "%s — %s, %d %s\n\n", personLabel(person), weekdays[dayStart.Weekday()], dayStart.Day(), months[dayStart.Month()])
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
		fmt.Fprintf(&b, "%s — %d–%d %s", personLabel(person), start.Day(), end.Day(), months[end.Month()])
	} else {
		fmt.Fprintf(&b, "%s — %d %s – %d %s", personLabel(person), start.Day(), months[start.Month()], end.Day(), months[end.Month()])
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
	groups, eventCounts := combinedGroups(start, people)
	var b strings.Builder
	b.WriteString(title)
	for _, group := range sortedCombinedGroups(groups, func(group combinedGroup) bool { return len(group.items) >= 3 }) {
		b.WriteString("\n\n" + combinedLine(start, group))
	}
	for personIndex, p := range people {
		sectionGroups := sortedCombinedGroups(groups, func(group combinedGroup) bool {
			return len(group.items) < 3 && group.anchor() == personIndex
		})
		if len(sectionGroups) == 0 && eventCounts[personIndex] > 0 && !p.Stale && !p.Error {
			continue
		}
		b.WriteString("\n\n" + combinedSectionName(p.Name, sectionGroups))
		if len(sectionGroups) == 0 && eventCounts[personIndex] == 0 {
			b.WriteString("\nНет занятий")
		} else {
			for _, group := range sectionGroups {
				b.WriteString("\n" + combinedLine(start, group))
			}
		}
		if p.Error {
			b.WriteString("\n⚠️ Не удалось получить расписание.")
		} else if p.Stale {
			b.WriteString("\n⚠️ Данные могут быть неактуальны.")
		}
	}
	return b.String()
}

func CombinedChronological(title string, start time.Time, people []PersonEvents) string {
	groups, _ := combinedGroups(start, people)
	groups = sortedCombinedGroups(groups, func(combinedGroup) bool { return true })
	var b strings.Builder
	b.WriteString(title)
	if len(groups) == 0 {
		b.WriteString("\n\nНет занятий")
	} else {
		for index, group := range groups {
			separator := "\n"
			if index == 0 {
				separator = "\n\n"
			}
			b.WriteString(separator + combinedLine(start, group))
		}
	}
	for _, person := range people {
		if person.Error {
			b.WriteString("\n⚠️ " + personLabel(person.Name) + ": не удалось получить расписание.")
		} else if person.Stale {
			b.WriteString("\n⚠️ " + personLabel(person.Name) + ": данные могут быть неактуальны.")
		}
	}
	return b.String()
}

type PersonEvents struct {
	Name         string
	Events       []calendar.Event
	Stale, Error bool
}

type combinedItem struct {
	personIndex int
	personName  string
	event       calendar.Event
	title       string
}

type combinedGroup struct {
	items []combinedItem
}

func (g combinedGroup) anchor() int {
	anchor := g.items[0].personIndex
	for _, item := range g.items[1:] {
		if item.personIndex < anchor {
			anchor = item.personIndex
		}
	}
	return anchor
}

func combinedGroups(day time.Time, people []PersonEvents) ([]combinedGroup, []int) {
	var groups []combinedGroup
	byKey := map[string][]int{}
	eventCounts := make([]int, len(people))
	for personIndex, person := range people {
		for _, event := range eventsForDay(day, person.Events) {
			eventCounts[personIndex]++
			title := combinedTitle(person.Name, event.Summary)
			key := combinedKey(event, title)
			groupIndex := -1
			for _, candidate := range byKey[key] {
				if !groupHasPerson(groups[candidate], personIndex) {
					groupIndex = candidate
					break
				}
			}
			if groupIndex < 0 {
				groups = append(groups, combinedGroup{})
				groupIndex = len(groups) - 1
				byKey[key] = append(byKey[key], groupIndex)
			}
			groups[groupIndex].items = append(groups[groupIndex].items, combinedItem{personIndex: personIndex, personName: person.Name, event: event, title: title})
		}
	}
	return groups, eventCounts
}

func groupHasPerson(group combinedGroup, personIndex int) bool {
	for _, item := range group.items {
		if item.personIndex == personIndex {
			return true
		}
	}
	return false
}

func sortedCombinedGroups(groups []combinedGroup, include func(combinedGroup) bool) []combinedGroup {
	var selected []combinedGroup
	for _, group := range groups {
		if include(group) {
			selected = append(selected, group)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		a, b := selected[i].items[0].event, selected[j].items[0].event
		if a.AllDay != b.AllDay {
			return a.AllDay
		}
		return a.Start.Before(b.Start)
	})
	return selected
}

func combinedLine(day time.Time, group combinedGroup) string {
	event := group.items[0].event
	title := addPersonMarker(group.items[0].personName, safeSummary(event.Summary))
	if len(group.items) > 1 {
		names := make([]string, 0, len(group.items))
		for _, item := range group.items {
			names = append(names, personLabel(item.personName))
		}
		title = strings.Join(names, ", ") + " — " + safeSummary(group.items[0].title)
	}
	if event.AllDay {
		return "Весь день — " + title
	}
	return fmt.Sprintf("%s–%s %s", event.Start.In(day.Location()).Format("15:04"), event.End.In(day.Location()).Format("15:04"), title)
}

func combinedSectionName(defaultName string, groups []combinedGroup) string {
	if len(groups) == 0 || len(groups[0].items) < 2 {
		return personLabel(defaultName)
	}
	want := combinedParticipantKey(groups[0])
	for _, group := range groups[1:] {
		if len(group.items) < 2 || combinedParticipantKey(group) != want {
			return personLabel(defaultName)
		}
	}
	names := make([]string, 0, len(groups[0].items))
	for _, item := range groups[0].items {
		names = append(names, personLabel(item.personName))
	}
	return strings.Join(names, ", ")
}

func personLabel(name string) string {
	marker := personMarker(name)
	if marker == "" {
		return name
	}
	return marker + " " + name
}

func addPersonMarker(personName, title string) string {
	marker := personMarker(personName)
	if marker == "" {
		return title
	}
	return marker + " " + title
}

func personMarker(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "аня":
		return "🟠"
	case "лёша", "леша":
		return "🟣"
	case "настя":
		return "⚪"
	case "саша":
		return "🔴"
	default:
		return ""
	}
}

func combinedParticipantKey(group combinedGroup) string {
	parts := make([]string, 0, len(group.items))
	for _, item := range group.items {
		parts = append(parts, fmt.Sprint(item.personIndex))
	}
	return strings.Join(parts, ",")
}

func combinedTitle(personName, summary string) string {
	summary = strings.TrimSpace(summary)
	for _, separator := range []string{"—", "–", "-", ":"} {
		prefix := personName + " " + separator
		if len(summary) >= len(prefix) && strings.EqualFold(summary[:len(prefix)], prefix) {
			return strings.TrimSpace(summary[len(prefix):])
		}
	}
	return summary
}

func combinedKey(event calendar.Event, title string) string {
	return fmt.Sprintf("%t|%d|%d|%s", event.AllDay, event.Start.UnixNano(), event.End.UnixNano(), strings.ToLower(strings.Join(strings.Fields(title), " ")))
}

func dayLines(day time.Time, events []calendar.Event) string {
	list := eventsForDay(day, events)
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

func eventsForDay(day time.Time, events []calendar.Event) []calendar.Event {
	end := day.AddDate(0, 0, 1)
	var list []calendar.Event
	for _, event := range events {
		if calendar.Overlaps(event, day, end) {
			list = append(list, event)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].AllDay != list[j].AllDay {
			return list[i].AllDay
		}
		return list[i].Start.Before(list[j].Start)
	})
	return list
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

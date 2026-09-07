package ical

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
	"github.com/teambition/rrule-go"
)

type property struct {
	value  string
	params map[string]string
}
type rawEvent struct {
	uid, summary, rule, status string
	start, end                 property
	rdates, exdates            []property
	recurrenceID               *property
}

type Calendar struct {
	events []rawEvent
	loc    *time.Location
}

func Parse(r io.Reader, defaultLoc *time.Location) (*Calendar, error) {
	events, err := parse(r)
	if err != nil {
		return nil, err
	}
	return &Calendar{events: events, loc: defaultLoc}, nil
}

func (c *Calendar) Events(rangeStart, rangeEnd time.Time) ([]calendar.Event, error) {
	return expand(c.events, rangeStart, rangeEnd, c.loc)
}

func ParseAndExpand(r io.Reader, rangeStart, rangeEnd time.Time, defaultLoc *time.Location) ([]calendar.Event, error) {
	raw, err := parse(r)
	if err != nil {
		return nil, err
	}
	return expand(raw, rangeStart, rangeEnd, defaultLoc)
}

func expand(raw []rawEvent, rangeStart, rangeEnd time.Time, defaultLoc *time.Location) ([]calendar.Event, error) {
	groups := map[string][]rawEvent{}
	for _, e := range raw {
		groups[e.uid] = append(groups[e.uid], e)
	}
	var out []calendar.Event
	for uid, group := range groups {
		var masters []rawEvent
		overrides := map[int64]rawEvent{}
		for _, e := range group {
			if e.recurrenceID == nil {
				masters = append(masters, e)
				continue
			}
			t, _, parseErr := parseTime(*e.recurrenceID, defaultLoc)
			if parseErr != nil {
				return nil, fmt.Errorf("UID %s RECURRENCE-ID: %w", uid, parseErr)
			}
			overrides[t.UnixNano()] = e
		}
		usedOverrides := map[int64]bool{}
		for _, master := range masters {
			events, parseErr := expandMaster(master, overrides, usedOverrides, rangeStart, rangeEnd, defaultLoc)
			if parseErr != nil {
				return nil, fmt.Errorf("UID %s: %w", uid, parseErr)
			}
			out = append(out, events...)
		}
		for key, override := range overrides {
			if usedOverrides[key] || strings.EqualFold(override.status, "CANCELLED") {
				continue
			}
			e, parseErr := materialize(override, defaultLoc)
			if parseErr != nil {
				return nil, fmt.Errorf("UID %s override: %w", uid, parseErr)
			}
			if calendar.Overlaps(e, rangeStart, rangeEnd) {
				out = append(out, e)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AllDay != out[j].AllDay {
			return out[i].AllDay
		}
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].Summary < out[j].Summary
	})
	return out, nil
}

func expandMaster(raw rawEvent, overrides map[int64]rawEvent, used map[int64]bool, from, to time.Time, loc *time.Location) ([]calendar.Event, error) {
	if strings.EqualFold(raw.status, "CANCELLED") {
		return nil, nil
	}
	base, err := materialize(raw, loc)
	if err != nil {
		return nil, err
	}
	duration := base.End.Sub(base.Start)
	starts := []time.Time{base.Start}
	if raw.rule != "" {
		opt, parseErr := rrule.StrToROptionInLocation(raw.rule, base.Start.Location())
		if parseErr != nil {
			return nil, fmt.Errorf("RRULE: %w", parseErr)
		}
		opt.Dtstart = base.Start
		r, parseErr := rrule.NewRRule(*opt)
		if parseErr != nil {
			return nil, fmt.Errorf("RRULE: %w", parseErr)
		}
		starts = r.Between(from.Add(-duration), to, true)
	}
	for _, p := range raw.rdates {
		ts, _, e := parseTimeList(p, loc)
		if e != nil {
			return nil, e
		}
		starts = append(starts, ts...)
	}
	excluded := map[int64]bool{}
	for _, p := range raw.exdates {
		ts, _, e := parseTimeList(p, loc)
		if e != nil {
			return nil, e
		}
		for _, t := range ts {
			excluded[t.UnixNano()] = true
		}
	}
	seen := map[int64]bool{}
	var out []calendar.Event
	for _, start := range starts {
		key := start.UnixNano()
		if seen[key] || excluded[key] {
			continue
		}
		seen[key] = true
		if override, ok := overrides[key]; ok {
			used[key] = true
			if strings.EqualFold(override.status, "CANCELLED") {
				continue
			}
			e, eErr := materialize(override, loc)
			if eErr != nil {
				return nil, eErr
			}
			if calendar.Overlaps(e, from, to) {
				out = append(out, e)
			}
			continue
		}
		e := base
		e.Start = start
		e.End = start.Add(duration)
		if calendar.Overlaps(e, from, to) {
			out = append(out, e)
		}
	}
	return out, nil
}

func materialize(raw rawEvent, loc *time.Location) (calendar.Event, error) {
	start, allDay, err := parseTime(raw.start, loc)
	if err != nil {
		return calendar.Event{}, err
	}
	end, endAllDay, err := parseTime(raw.end, loc)
	if raw.end.value == "" {
		if allDay {
			end = start.AddDate(0, 0, 1)
		} else {
			end = start.Add(time.Nanosecond)
		}
		endAllDay = allDay
		err = nil
	}
	if err != nil {
		return calendar.Event{}, err
	}
	if allDay != endAllDay || !end.After(start) {
		return calendar.Event{}, fmt.Errorf("некорректный интервал события")
	}
	return calendar.Event{UID: raw.uid, Summary: unescape(raw.summary), Start: start, End: end, AllDay: allDay}, nil
}

func parse(r io.Reader) ([]rawEvent, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var lines []string
	for s.Scan() {
		line := strings.TrimSuffix(s.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
		} else {
			lines = append(lines, line)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	var out []rawEvent
	seenCalendar, endedCalendar := false, false
	var cur *rawEvent
	for _, line := range lines {
		switch strings.ToUpper(line) {
		case "BEGIN:VCALENDAR":
			seenCalendar = true
			continue
		case "END:VCALENDAR":
			endedCalendar = true
			continue
		case "BEGIN:VEVENT":
			cur = &rawEvent{}
			continue
		case "END:VEVENT":
			if cur != nil {
				if cur.uid == "" || cur.start.value == "" {
					return nil, fmt.Errorf("VEVENT без UID/DTSTART")
				}
				out = append(out, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			continue
		}
		name, p, ok := parseProperty(line)
		if !ok {
			continue
		}
		switch name {
		case "UID":
			cur.uid = p.value
		case "SUMMARY":
			cur.summary = p.value
		case "STATUS":
			cur.status = p.value
		case "DTSTART":
			cur.start = p
		case "DTEND":
			cur.end = p
		case "RRULE":
			cur.rule = p.value
		case "RDATE":
			cur.rdates = append(cur.rdates, p)
		case "EXDATE":
			cur.exdates = append(cur.exdates, p)
		case "RECURRENCE-ID":
			x := p
			cur.recurrenceID = &x
		}
	}
	if !seenCalendar || !endedCalendar || cur != nil {
		return nil, fmt.Errorf("некорректная структура VCALENDAR")
	}
	return out, nil
}

func parseProperty(line string) (string, property, bool) {
	i := strings.IndexByte(line, ':')
	if i < 1 {
		return "", property{}, false
	}
	left, value := line[:i], line[i+1:]
	parts := strings.Split(left, ";")
	p := property{value: value, params: map[string]string{}}
	for _, x := range parts[1:] {
		kv := strings.SplitN(x, "=", 2)
		if len(kv) == 2 {
			p.params[strings.ToUpper(kv[0])] = strings.Trim(kv[1], `"`)
		}
	}
	return strings.ToUpper(parts[0]), p, true
}

func parseTimeList(p property, loc *time.Location) ([]time.Time, bool, error) {
	var out []time.Time
	all := false
	for _, v := range strings.Split(p.value, ",") {
		q := p
		q.value = v
		t, a, e := parseTime(q, loc)
		if e != nil {
			return nil, false, e
		}
		all = a
		out = append(out, t)
	}
	return out, all, nil
}

func parseTime(p property, defaultLoc *time.Location) (time.Time, bool, error) {
	if strings.EqualFold(p.params["VALUE"], "DATE") || (len(p.value) == 8 && !strings.Contains(p.value, "T")) {
		t, e := time.ParseInLocation("20060102", p.value, defaultLoc)
		return t, true, e
	}
	loc := defaultLoc
	if tz := p.params["TZID"]; tz != "" {
		loaded, e := time.LoadLocation(tz)
		if e != nil {
			return time.Time{}, false, fmt.Errorf("неизвестный TZID %q", tz)
		}
		loc = loaded
	}
	if strings.HasSuffix(p.value, "Z") {
		t, e := time.Parse("20060102T150405Z", p.value)
		return t, false, e
	}
	for _, layout := range []string{"20060102T150405", "20060102T1504"} {
		if t, e := time.ParseInLocation(layout, p.value, loc); e == nil {
			return t, false, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("некорректная дата %q", p.value)
}

func unescape(s string) string {
	r := strings.NewReplacer(`\n`, `\n`, `\N`, `\n`, `\,`, `,`, `\;`, `;`, `\\`, `\`)
	return r.Replace(s)
}

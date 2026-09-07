package cache

import (
	"sync"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
	calendarical "github.com/eusatenko/calendar_telegramm_bot/internal/calendar/ical"
)

type Loader func() (*calendarical.Calendar, error)

type Source struct {
	mu         sync.Mutex
	cond       *sync.Cond
	cal        *calendarical.Calendar
	fetchedAt  time.Time
	refreshing bool
	generation uint64
	lastErr    error
	ttl        time.Duration
	load       Loader
	now        func() time.Time
}

func New(ttl time.Duration, load Loader) *Source {
	s := &Source{ttl: ttl, load: load, now: time.Now}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *Source) Events(from, to time.Time) ([]calendar.Event, bool, error) {
	s.mu.Lock()
	initialGeneration := s.generation
	for {
		if s.cal != nil && s.now().Sub(s.fetchedAt) < s.ttl {
			cal := s.cal
			s.mu.Unlock()
			e, err := cal.Events(from, to)
			return e, false, err
		}
		if !s.refreshing {
			s.refreshing = true
			break
		}
		s.cond.Wait()
		if s.generation != initialGeneration && s.lastErr != nil && s.cal != nil {
			cal := s.cal
			s.mu.Unlock()
			events, err := cal.Events(from, to)
			return events, true, err
		}
	}
	old := s.cal
	s.mu.Unlock()
	fresh, err := s.load()
	s.mu.Lock()
	s.refreshing = false
	s.generation++
	s.lastErr = err
	if err == nil {
		s.cal = fresh
		s.fetchedAt = s.now()
	}
	cal := s.cal
	s.cond.Broadcast()
	s.mu.Unlock()
	if err != nil && old == nil {
		return nil, false, err
	}
	events, expandErr := cal.Events(from, to)
	if expandErr != nil {
		return nil, err != nil, expandErr
	}
	return events, err != nil, nil
}

package calendar

import "time"

type Event struct {
	UID, Summary string
	Start, End   time.Time
	AllDay       bool
}

type Source interface {
	Events(start, end time.Time) ([]Event, bool, error)
}

func Overlaps(e Event, start, end time.Time) bool { return e.Start.Before(end) && e.End.After(start) }

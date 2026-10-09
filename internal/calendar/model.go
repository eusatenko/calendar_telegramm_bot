package calendar

import "time"

type Event struct {
	UID, Summary string
	Start, End   time.Time
	AllDay       bool
	// Recurring marks an expanded occurrence of a recurring event.
	Recurring bool
	// OriginalStart is the occurrence identity used by calendar APIs. It stays
	// unchanged when a single occurrence has been moved.
	OriginalStart time.Time
}

type Source interface {
	Events(start, end time.Time) ([]Event, bool, error)
}

func Overlaps(e Event, start, end time.Time) bool { return e.Start.Before(end) && e.End.After(start) }

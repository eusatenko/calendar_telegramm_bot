package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar/googleapi"
	"github.com/eusatenko/calendar_telegramm_bot/internal/storage"
)

type fakeRepository struct {
	copies   []storage.EventCopy
	recorded [3]int
}

func (f *fakeRepository) EventGroup(string, string) ([]storage.EventCopy, error) {
	return f.copies, nil
}
func (f *fakeRepository) RecordEventEdit(actor int64, copies, failures int) error {
	f.recorded = [3]int{int(actor), copies, failures}
	return nil
}

type fakeWriter struct {
	failCalendar string
	calls        []string
}

func (f *fakeWriter) Apply(_ context.Context, calendarID string, _ googleapi.Edit) error {
	f.calls = append(f.calls, calendarID)
	if calendarID == f.failCalendar {
		return errors.New("temporary")
	}
	return nil
}

func TestCoordinatorUpdatesEveryLinkedCopyAndKeepsPartialResult(t *testing.T) {
	repository := &fakeRepository{copies: []storage.EventCopy{{CalendarKey: "anya", ICalUID: "a"}, {CalendarKey: "lesha", ICalUID: "b"}}}
	writer := &fakeWriter{failCalendar: "cal-b"}
	invalidated := 0
	coordinator := NewCoordinator(repository, writer, []Target{
		{Key: "anya", Name: "Аня", CalendarID: "cal-a", Invalidate: func() { invalidated++ }},
		{Key: "lesha", Name: "Лёша", CalendarID: "cal-b", Invalidate: func() { invalidated++ }},
	})
	results, err := coordinator.Apply(context.Background(), Request{Actor: 7, SourceKey: "anya", SourceICalUID: "a", Scope: googleapi.ScopeSeries, OriginalStart: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Err != nil || results[1].Err == nil {
		t.Fatalf("results=%+v", results)
	}
	if invalidated != 1 || repository.recorded != [3]int{7, 2, 1} {
		t.Fatalf("invalidated=%d recorded=%v", invalidated, repository.recorded)
	}
}

func TestCoordinatorRejectsUnlinkedEvent(t *testing.T) {
	coordinator := NewCoordinator(&fakeRepository{}, &fakeWriter{}, nil)
	_, err := coordinator.Apply(context.Background(), Request{SourceKey: "anya", SourceICalUID: "a"})
	if !errors.Is(err, ErrEventNotLinked) {
		t.Fatalf("err=%v", err)
	}
}

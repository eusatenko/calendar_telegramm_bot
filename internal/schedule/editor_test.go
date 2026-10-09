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
func (f *fakeRepository) LinkEventCopies(_ int64, copies []storage.EventCopy) error {
	f.copies = append([]storage.EventCopy(nil), copies...)
	return nil
}
func (f *fakeRepository) RecordEventEdit(actor int64, copies, failures int) error {
	f.recorded = [3]int{int(actor), copies, failures}
	return nil
}
func (f *fakeRepository) RecordEventCreate(actor int64, copies, failures int) error {
	f.recorded = [3]int{int(actor), copies, failures}
	return nil
}
func (f *fakeRepository) UnlinkEventCopies(_ int64, copies []storage.EventCopy) error {
	removed := map[string]bool{}
	for _, copy := range copies {
		removed[copy.CalendarKey+"\x00"+copy.ICalUID] = true
	}
	kept := f.copies[:0]
	for _, copy := range f.copies {
		if !removed[copy.CalendarKey+"\x00"+copy.ICalUID] {
			kept = append(kept, copy)
		}
	}
	f.copies = kept
	return nil
}
func (f *fakeRepository) RecordEventDelete(actor int64, copies, failures int) error {
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
func (f *fakeWriter) Create(_ context.Context, calendarID string, _ googleapi.Create) (string, error) {
	f.calls = append(f.calls, calendarID)
	if calendarID == f.failCalendar {
		return "", errors.New("temporary")
	}
	return "uid-" + calendarID, nil
}
func (f *fakeWriter) Delete(_ context.Context, calendarID string, _ googleapi.Delete) error {
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

func TestCoordinatorUpdatesOnlySelectedCopy(t *testing.T) {
	repository := &fakeRepository{copies: []storage.EventCopy{{CalendarKey: "anya", ICalUID: "a"}, {CalendarKey: "lesha", ICalUID: "b"}}}
	writer := &fakeWriter{}
	coordinator := NewCoordinator(repository, writer, []Target{{Key: "anya", Name: "Аня", CalendarID: "cal-a"}, {Key: "lesha", Name: "Лёша", CalendarID: "cal-b"}})
	title := "Новое"
	results, err := coordinator.Apply(context.Background(), Request{Actor: 7, SourceKey: "lesha", SourceICalUID: "b", Scope: googleapi.ScopeOccurrence, Summary: &title, TargetKeys: []string{"lesha"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(writer.calls) != 1 || writer.calls[0] != "cal-b" {
		t.Fatalf("results=%+v calls=%+v", results, writer.calls)
	}
}

func TestCoordinatorCreatesAndLinksSuccessfulCopies(t *testing.T) {
	repository := &fakeRepository{}
	writer := &fakeWriter{failCalendar: "cal-b"}
	coordinator := NewCoordinator(repository, writer, []Target{{Key: "anya", Name: "Аня", CalendarID: "cal-a"}, {Key: "lesha", Name: "Лёша", CalendarID: "cal-b"}})
	start := time.Date(2026, 10, 10, 13, 0, 0, 0, time.UTC)
	results, err := coordinator.Create(context.Background(), CreateRequest{Actor: 7, TargetKeys: []string{"anya", "lesha"}, Summary: "Занятие", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Err != nil || results[1].Err == nil {
		t.Fatalf("results=%+v", results)
	}
	if len(repository.copies) != 1 || repository.copies[0].CalendarKey != "anya" {
		t.Fatalf("linked=%+v", repository.copies)
	}
}

func TestCoordinatorDeletesSelectedSeriesAndUnlinksIt(t *testing.T) {
	repository := &fakeRepository{copies: []storage.EventCopy{{CalendarKey: "anya", ICalUID: "a"}, {CalendarKey: "lesha", ICalUID: "b"}}}
	writer := &fakeWriter{}
	coordinator := NewCoordinator(repository, writer, []Target{{Key: "anya", Name: "Аня", CalendarID: "cal-a"}, {Key: "lesha", Name: "Лёша", CalendarID: "cal-b"}})
	results, err := coordinator.Apply(context.Background(), Request{Actor: 7, Scope: googleapi.ScopeSeries, Delete: true, UnlinkOnDelete: true, DirectCopies: append([]storage.EventCopy(nil), repository.copies...), TargetKeys: []string{"lesha"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(writer.calls) != 1 || writer.calls[0] != "cal-b" {
		t.Fatalf("results=%+v calls=%+v", results, writer.calls)
	}
	if len(repository.copies) != 1 || repository.copies[0].CalendarKey != "anya" {
		t.Fatalf("remaining=%+v", repository.copies)
	}
}

func TestCoordinatorKeepsLinkAfterDeletingOccurrence(t *testing.T) {
	copies := []storage.EventCopy{{CalendarKey: "anya", ICalUID: "a"}}
	repository := &fakeRepository{copies: append([]storage.EventCopy(nil), copies...)}
	coordinator := NewCoordinator(repository, &fakeWriter{}, []Target{{Key: "anya", Name: "Аня", CalendarID: "cal-a"}})
	_, err := coordinator.Apply(context.Background(), Request{Actor: 7, Scope: googleapi.ScopeOccurrence, Delete: true, DirectCopies: copies, TargetKeys: []string{"anya"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.copies) != 1 {
		t.Fatalf("occurrence deletion removed series link: %+v", repository.copies)
	}
}

func TestCoordinatorUnlinksOnlySuccessfullyDeletedSeriesCopies(t *testing.T) {
	repository := &fakeRepository{copies: []storage.EventCopy{{CalendarKey: "anya", ICalUID: "a"}, {CalendarKey: "lesha", ICalUID: "b"}}}
	writer := &fakeWriter{failCalendar: "cal-b"}
	coordinator := NewCoordinator(repository, writer, []Target{{Key: "anya", Name: "Аня", CalendarID: "cal-a"}, {Key: "lesha", Name: "Лёша", CalendarID: "cal-b"}})
	results, err := coordinator.Apply(context.Background(), Request{Actor: 7, Scope: googleapi.ScopeSeries, Delete: true, UnlinkOnDelete: true, DirectCopies: append([]storage.EventCopy(nil), repository.copies...)})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Err != nil || results[1].Err == nil {
		t.Fatalf("results=%+v", results)
	}
	if len(repository.copies) != 1 || repository.copies[0].CalendarKey != "lesha" {
		t.Fatalf("remaining=%+v", repository.copies)
	}
	if repository.recorded != [3]int{7, 2, 1} {
		t.Fatalf("recorded=%v", repository.recorded)
	}
}

package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar/googleapi"
	"github.com/eusatenko/calendar_telegramm_bot/internal/storage"
)

var ErrEventNotLinked = errors.New("копии события ещё не связаны")

type Writer interface {
	Apply(ctx context.Context, calendarID string, edit googleapi.Edit) error
}

type Repository interface {
	EventGroup(calendarKey, iCalUID string) ([]storage.EventCopy, error)
	RecordEventEdit(actor int64, copyCount, failureCount int) error
}

type Target struct {
	Key, Name, CalendarID string
	Invalidate            func()
}

type Request struct {
	Actor         int64
	SourceKey     string
	SourceICalUID string
	OriginalStart time.Time
	Scope         googleapi.Scope
	Summary       *string
	Start, End    *time.Time
}

type CopyResult struct {
	Key, Name string
	Err       error
}

type Coordinator struct {
	repository Repository
	writer     Writer
	targets    map[string]Target
}

func NewCoordinator(repository Repository, writer Writer, targets []Target) *Coordinator {
	targetMap := make(map[string]Target, len(targets))
	for _, target := range targets {
		targetMap[target.Key] = target
	}
	return &Coordinator{repository: repository, writer: writer, targets: targetMap}
}

func (c *Coordinator) Apply(ctx context.Context, request Request) ([]CopyResult, error) {
	copies, err := c.repository.EventGroup(request.SourceKey, request.SourceICalUID)
	if err != nil {
		return nil, err
	}
	if len(copies) == 0 {
		return nil, ErrEventNotLinked
	}
	results := make([]CopyResult, 0, len(copies))
	failures := 0
	for _, copy := range copies {
		target, ok := c.targets[copy.CalendarKey]
		if !ok {
			results = append(results, CopyResult{Key: copy.CalendarKey, Name: copy.CalendarKey, Err: errors.New("календарь не настроен")})
			failures++
			continue
		}
		edit := googleapi.Edit{
			ICalUID: copy.ICalUID, OriginalStart: request.OriginalStart,
			Scope: request.Scope, Summary: request.Summary, Start: request.Start, End: request.End,
		}
		applyErr := c.writer.Apply(ctx, target.CalendarID, edit)
		if applyErr != nil {
			failures++
		} else if target.Invalidate != nil {
			target.Invalidate()
		}
		results = append(results, CopyResult{Key: target.Key, Name: target.Name, Err: applyErr})
	}
	if err := c.repository.RecordEventEdit(request.Actor, len(copies), failures); err != nil {
		return results, fmt.Errorf("журнал изменения: %w", err)
	}
	return results, nil
}

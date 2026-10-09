package schedule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar/googleapi"
	"github.com/eusatenko/calendar_telegramm_bot/internal/storage"
)

var ErrEventNotLinked = errors.New("копии события ещё не связаны")

type Writer interface {
	Apply(ctx context.Context, calendarID string, edit googleapi.Edit) error
	Create(ctx context.Context, calendarID string, event googleapi.Create) (string, error)
}

type Repository interface {
	EventGroup(calendarKey, iCalUID string) ([]storage.EventCopy, error)
	LinkEventCopies(actor int64, copies []storage.EventCopy) error
	RecordEventEdit(actor int64, copyCount, failureCount int) error
	RecordEventCreate(actor int64, copyCount, failureCount int) error
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
	Location      *string
	Start, End    *time.Time
	TargetKeys    []string
}

type CreateRequest struct {
	Actor       int64
	TargetKeys  []string
	Summary     string
	Location    string
	Start, End  time.Time
	RepeatUntil *time.Time
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
	wanted := keySet(request.TargetKeys)
	failures := 0
	for _, copy := range copies {
		if len(wanted) > 0 && !wanted[copy.CalendarKey] {
			continue
		}
		target, ok := c.targets[copy.CalendarKey]
		if !ok {
			results = append(results, CopyResult{Key: copy.CalendarKey, Name: copy.CalendarKey, Err: errors.New("календарь не настроен")})
			failures++
			continue
		}
		edit := googleapi.Edit{
			ICalUID: copy.ICalUID, OriginalStart: request.OriginalStart,
			Scope: request.Scope, Summary: request.Summary, Location: request.Location, Start: request.Start, End: request.End,
		}
		applyErr := c.writer.Apply(ctx, target.CalendarID, edit)
		if applyErr != nil {
			failures++
		} else if target.Invalidate != nil {
			target.Invalidate()
		}
		results = append(results, CopyResult{Key: target.Key, Name: target.Name, Err: applyErr})
	}
	if err := c.repository.RecordEventEdit(request.Actor, len(results), failures); err != nil {
		return results, fmt.Errorf("журнал изменения: %w", err)
	}
	return results, nil
}

func (c *Coordinator) Create(ctx context.Context, request CreateRequest) ([]CopyResult, error) {
	if len(request.TargetKeys) == 0 || strings.TrimSpace(request.Summary) == "" || !request.End.After(request.Start) {
		return nil, errors.New("некорректный запрос создания")
	}
	seen := map[string]bool{}
	results := make([]CopyResult, 0, len(request.TargetKeys))
	created := make([]storage.EventCopy, 0, len(request.TargetKeys))
	failures := 0
	for _, key := range request.TargetKeys {
		if seen[key] {
			continue
		}
		seen[key] = true
		target, ok := c.targets[key]
		if !ok {
			results = append(results, CopyResult{Key: key, Name: key, Err: errors.New("календарь не настроен")})
			failures++
			continue
		}
		uid, err := c.writer.Create(ctx, target.CalendarID, googleapi.Create{Summary: request.Summary, Location: request.Location, Start: request.Start, End: request.End, RepeatUntil: request.RepeatUntil})
		if err != nil {
			failures++
		} else {
			created = append(created, storage.EventCopy{CalendarKey: key, ICalUID: uid})
			if target.Invalidate != nil {
				target.Invalidate()
			}
		}
		results = append(results, CopyResult{Key: key, Name: target.Name, Err: err})
	}
	if len(created) > 0 {
		if err := c.repository.LinkEventCopies(request.Actor, created); err != nil {
			return results, fmt.Errorf("связь созданных событий: %w", err)
		}
	}
	if err := c.repository.RecordEventCreate(request.Actor, len(results), failures); err != nil {
		return results, fmt.Errorf("журнал создания: %w", err)
	}
	return results, nil
}

func keySet(keys []string) map[string]bool {
	result := make(map[string]bool, len(keys))
	for _, key := range keys {
		result[key] = true
	}
	return result
}

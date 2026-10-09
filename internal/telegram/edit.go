package telegram

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar/googleapi"
	"github.com/eusatenko/calendar_telegramm_bot/internal/schedule"
	"github.com/eusatenko/calendar_telegramm_bot/internal/storage"
)

func (b *Bot) scheduleEdit(ctx context.Context, q CallbackQuery, parts []string) error {
	if len(parts) < 2 {
		return b.invalid(ctx, q)
	}
	switch parts[1] {
	case "menu":
		if len(parts) != 2 {
			return b.invalid(ctx, q)
		}
		markup := Markup{}
		for _, key := range b.order {
			person := b.people[key]
			markup.InlineKeyboard = append(markup.InlineKeyboard, []Button{{Text: person.Name, CallbackData: "edit:person:" + key}})
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, []Button{{Text: "Назад", CallbackData: "admin:menu"}})
		return b.edit(ctx, q, "Чьё расписание открыть?", markup)
	case "person":
		if len(parts) != 3 {
			return b.invalid(ctx, q)
		}
		person, ok := b.people[parts[2]]
		if !ok {
			return b.invalid(ctx, q)
		}
		markup := Markup{InlineKeyboard: [][]Button{
			{{Text: "Сегодня", CallbackData: "edit:day:" + person.Key + ":0"}, {Text: "Завтра", CallbackData: "edit:day:" + person.Key + ":1"}},
			{{Text: "Назад", CallbackData: "edit:menu"}},
		}}
		return b.edit(ctx, q, person.Name+": выберите дату", markup)
	case "day":
		return b.editDay(ctx, q, parts)
	case "event":
		if len(parts) != 3 {
			return b.invalid(ctx, q)
		}
		return b.editEventCard(ctx, q, parts[2])
	case "link":
		if len(parts) != 3 {
			return b.invalid(ctx, q)
		}
		session, ok := b.editSession(q.From.ID, parts[2])
		if !ok || len(session.candidates) < 2 {
			return b.edit(ctx, q, "Сеанс устарел. Выберите событие заново.", editBackMenu())
		}
		if err := b.store.LinkEventCopies(q.From.ID, session.candidates); err != nil {
			return err
		}
		return b.showEditActions(ctx, q, parts[2], session)
	case "field":
		if len(parts) != 4 || (parts[3] != "title" && parts[3] != "time") {
			return b.invalid(ctx, q)
		}
		session, ok := b.editSession(q.From.ID, parts[2])
		if !ok {
			return b.edit(ctx, q, "Сеанс устарел. Выберите событие заново.", editBackMenu())
		}
		if session.event.Recurring {
			markup := Markup{InlineKeyboard: [][]Button{
				{{Text: "Только это", CallbackData: "edit:scope:" + parts[2] + ":" + parts[3] + ":occurrence"}},
				{{Text: "Вся серия", CallbackData: "edit:scope:" + parts[2] + ":" + parts[3] + ":series"}},
				{{Text: "Назад", CallbackData: "edit:event:" + parts[2]}},
			}}
			return b.edit(ctx, q, "Какую часть повторяющегося события изменить?", markup)
		}
		return b.promptEditInput(ctx, q, parts[2], parts[3], googleapi.ScopeOccurrence)
	case "scope":
		if len(parts) != 5 || (parts[3] != "title" && parts[3] != "time") || (parts[4] != string(googleapi.ScopeOccurrence) && parts[4] != string(googleapi.ScopeSeries)) {
			return b.invalid(ctx, q)
		}
		if _, ok := b.editSession(q.From.ID, parts[2]); !ok {
			return b.edit(ctx, q, "Сеанс устарел. Выберите событие заново.", editBackMenu())
		}
		return b.promptEditInput(ctx, q, parts[2], parts[3], googleapi.Scope(parts[4]))
	case "confirm":
		if len(parts) != 3 {
			return b.invalid(ctx, q)
		}
		session, ok := b.editSession(q.From.ID, parts[2])
		if !ok || session.pending == nil {
			return b.edit(ctx, q, "Сеанс устарел. Выберите событие заново.", editBackMenu())
		}
		results, err := b.editor.Apply(ctx, *session.pending)
		if err != nil {
			if errors.Is(err, schedule.ErrEventNotLinked) {
				return b.edit(ctx, q, "Копии события не связаны. Выберите событие заново.", editBackMenu())
			}
			return err
		}
		session.pending = nil
		b.saveEditSession(parts[2], session)
		return b.edit(ctx, q, b.formatEditResults(q.From.ID, results), editBackMenu())
	default:
		return b.invalid(ctx, q)
	}
}

func (b *Bot) editDay(ctx context.Context, q CallbackQuery, parts []string) error {
	if len(parts) != 4 {
		return b.invalid(ctx, q)
	}
	person, ok := b.people[parts[2]]
	if !ok {
		return b.invalid(ctx, q)
	}
	offset, err := strconv.Atoi(parts[3])
	if err != nil || (offset != 0 && offset != 1) {
		return b.invalid(ctx, q)
	}
	from, to := calendar.DayRange(time.Now(), b.loc, offset)
	events, _, err := person.Source.Events(from, to)
	if err != nil {
		return b.edit(ctx, q, "Не удалось загрузить расписание.", editBackMenu())
	}
	markup := Markup{}
	for _, event := range events {
		token, err := b.newEditSession(q.From.ID, person.Key, event)
		if err != nil {
			return err
		}
		label := event.Summary
		if !event.AllDay {
			label = event.Start.In(b.loc).Format("15:04") + " " + label
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, []Button{{Text: truncateRunes(label, 48), CallbackData: "edit:event:" + token}})
	}
	markup.InlineKeyboard = append(markup.InlineKeyboard, []Button{{Text: "Назад", CallbackData: "edit:person:" + person.Key}})
	if len(events) == 0 {
		return b.edit(ctx, q, "На эту дату событий нет.", markup)
	}
	return b.edit(ctx, q, "Выберите событие", markup)
}

func (b *Bot) editEventCard(ctx context.Context, q CallbackQuery, token string) error {
	session, ok := b.editSession(q.From.ID, token)
	if !ok {
		return b.edit(ctx, q, "Сеанс устарел. Выберите событие заново.", editBackMenu())
	}
	linked, err := b.store.EventGroup(session.personKey, session.event.UID)
	if err != nil {
		return err
	}
	if len(linked) > 0 {
		return b.showEditActions(ctx, q, token, session)
	}
	candidates, err := b.findMatchingCopies(session)
	if err != nil {
		return err
	}
	session.candidates = candidates
	b.saveEditSession(token, session)
	if len(candidates) < 2 {
		return b.edit(ctx, q, "Связанные копии не найдены. Для первого изменения копии должны иметь одинаковые название и время.", editBackMenu())
	}
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, b.people[candidate.CalendarKey].Name)
	}
	text := eventDescription(session.event, b.loc) + "\n\nНайдены одинаковые копии: " + strings.Join(names, ", ") + ".\nПроверьте список перед связыванием."
	markup := Markup{InlineKeyboard: [][]Button{{{Text: "Связать эти копии", CallbackData: "edit:link:" + token}}, {{Text: "Отмена", CallbackData: "edit:menu"}}}}
	return b.edit(ctx, q, text, markup)
}

func (b *Bot) showEditActions(ctx context.Context, q CallbackQuery, token string, session editSession) error {
	rows := [][]Button{}
	if !session.event.AllDay {
		rows = append(rows, []Button{{Text: "Изменить время", CallbackData: "edit:field:" + token + ":time"}})
	}
	rows = append(rows,
		[]Button{{Text: "Изменить название", CallbackData: "edit:field:" + token + ":title"}},
		[]Button{{Text: "Назад", CallbackData: "edit:menu"}},
	)
	markup := Markup{InlineKeyboard: rows}
	return b.edit(ctx, q, eventDescription(session.event, b.loc)+"\n\nЧто изменить?", markup)
}

func (b *Bot) promptEditInput(ctx context.Context, q CallbackQuery, token, field string, scope googleapi.Scope) error {
	b.mu.Lock()
	b.awaiting[q.From.ID] = inputState{expires: time.Now().Add(5 * time.Minute), kind: "edit_" + field, token: token, scope: scope}
	b.mu.Unlock()
	text := "Отправьте новое название."
	if field == "time" {
		text = "Отправьте новое время в формате 18:00-19:30."
	}
	return b.edit(ctx, q, text+"\n\nОжидание действует 5 минут.", cancelMenu())
}

func (b *Bot) handleEditInput(ctx context.Context, message Message, state inputState) error {
	session, ok := b.editSession(message.From.ID, state.token)
	if !ok {
		b.clearAwaiting(message.From.ID)
		return b.client.Send(ctx, message.Chat.ID, "Сеанс устарел. Выберите событие заново.", editBackMenu())
	}
	request := schedule.Request{Actor: message.From.ID, SourceKey: session.personKey, SourceICalUID: session.event.UID, OriginalStart: session.event.OriginalStart, Scope: state.scope}
	switch state.kind {
	case "edit_title":
		title := strings.TrimSpace(message.Text)
		if title == "" || len([]rune(title)) > 300 {
			return b.client.Send(ctx, message.Chat.ID, "Название должно содержать от 1 до 300 символов. Отправьте /cancel для отмены.", cancelMenu())
		}
		request.Summary = &title
	case "edit_time":
		start, end, err := parseTimeRange(message.Text, session.event.Start, b.loc)
		if err != nil {
			return b.client.Send(ctx, message.Chat.ID, "Неверный формат. Пример: 18:00-19:30. Отправьте /cancel для отмены.", cancelMenu())
		}
		request.Start, request.End = &start, &end
	default:
		return errors.New("неизвестное состояние ввода")
	}
	b.clearAwaiting(message.From.ID)
	session.pending = &request
	b.saveEditSession(state.token, session)
	preview := "Проверьте изменение:\n\n" + eventDescription(session.event, b.loc)
	if request.Summary != nil {
		preview += "\nНовое название: " + *request.Summary
	} else if request.Start != nil && request.End != nil {
		preview += "\nНовое время: " + request.Start.In(b.loc).Format("15:04") + "–" + request.End.In(b.loc).Format("15:04")
	}
	if request.Scope == googleapi.ScopeSeries {
		preview += "\nОбласть: вся серия"
	} else {
		preview += "\nОбласть: только это событие"
	}
	markup := Markup{InlineKeyboard: [][]Button{{{Text: "Подтвердить", CallbackData: "edit:confirm:" + state.token}}, {{Text: "Отмена", CallbackData: "edit:event:" + state.token}}}}
	return b.client.Send(ctx, message.Chat.ID, preview, markup)
}

func (b *Bot) formatEditResults(actor int64, results []schedule.CopyResult) string {
	var output strings.Builder
	output.WriteString("Изменение завершено:")
	for _, result := range results {
		if result.Err == nil {
			fmt.Fprintf(&output, "\n%s ✅", result.Name)
		} else {
			fmt.Fprintf(&output, "\n%s ❌", result.Name)
			b.log.Error("calendar copy update failed", "telegram_user_id", actor, "calendar_name", result.Key, "error", result.Err)
		}
	}
	return output.String()
}

func (b *Bot) findMatchingCopies(session editSession) ([]storage.EventCopy, error) {
	dayStart := time.Date(session.event.Start.In(b.loc).Year(), session.event.Start.In(b.loc).Month(), session.event.Start.In(b.loc).Day(), 0, 0, 0, 0, b.loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	var copies []storage.EventCopy
	for _, key := range b.order {
		person := b.people[key]
		events, _, err := person.Source.Events(dayStart, dayEnd)
		if err != nil {
			return nil, err
		}
		var matches []calendar.Event
		for _, event := range events {
			if sameCopy(session.event, event) {
				matches = append(matches, event)
			}
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("найдено несколько одинаковых событий в календаре %s", key)
		}
		if len(matches) == 1 {
			copies = append(copies, storage.EventCopy{CalendarKey: key, ICalUID: matches[0].UID})
		}
	}
	return copies, nil
}

func sameCopy(a, b calendar.Event) bool {
	return strings.TrimSpace(a.Summary) == strings.TrimSpace(b.Summary) && a.Start.Equal(b.Start) && a.End.Equal(b.End) && a.AllDay == b.AllDay && a.Recurring == b.Recurring
}

func (b *Bot) newEditSession(actor int64, personKey string, event calendar.Event) (string, error) {
	random := make([]byte, 9)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	b.mu.Lock()
	for key, session := range b.editSessions {
		if time.Now().After(session.expires) {
			delete(b.editSessions, key)
		}
	}
	b.editSessions[token] = editSession{expires: time.Now().Add(10 * time.Minute), actor: actor, personKey: personKey, event: event}
	b.mu.Unlock()
	return token, nil
}

func (b *Bot) saveEditSession(token string, session editSession) {
	b.mu.Lock()
	b.editSessions[token] = session
	b.mu.Unlock()
}

func (b *Bot) editSession(actor int64, token string) (editSession, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	session, ok := b.editSessions[token]
	if !ok || session.actor != actor || time.Now().After(session.expires) {
		delete(b.editSessions, token)
		return editSession{}, false
	}
	return session, true
}

func parseTimeRange(value string, eventStart time.Time, loc *time.Location) (time.Time, time.Time, error) {
	parts := strings.Split(strings.TrimSpace(strings.ReplaceAll(value, "–", "-")), "-")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, errors.New("неверный формат")
	}
	startClock, err := time.Parse("15:04", strings.TrimSpace(parts[0]))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endClock, err := time.Parse("15:04", strings.TrimSpace(parts[1]))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	date := eventStart.In(loc)
	start := time.Date(date.Year(), date.Month(), date.Day(), startClock.Hour(), startClock.Minute(), 0, 0, loc)
	end := time.Date(date.Year(), date.Month(), date.Day(), endClock.Hour(), endClock.Minute(), 0, 0, loc)
	if !end.After(start) {
		end = end.AddDate(0, 0, 1)
	}
	return start, end, nil
}

func eventDescription(event calendar.Event, loc *time.Location) string {
	if event.AllDay {
		return event.Summary + "\n" + event.Start.In(loc).Format("02.01.2006") + ", весь день"
	}
	return event.Summary + "\n" + event.Start.In(loc).Format("02.01.2006 15:04") + "–" + event.End.In(loc).Format("15:04")
}

func editBackMenu() Markup {
	return Markup{InlineKeyboard: [][]Button{{{Text: "К выбору события", CallbackData: "edit:menu"}}, {{Text: "Главное меню", CallbackData: "main"}}}}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit-1]) + "…"
}

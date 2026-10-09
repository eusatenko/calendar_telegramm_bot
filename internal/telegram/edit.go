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

var errAmbiguousCopyMatch = errors.New("неоднозначное сопоставление копий")

type copyMatch struct {
	copies     []storage.EventCopy
	events     []calendar.Event
	exactTitle bool
}

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
			{{Text: "Указать дату", CallbackData: "edit:date:" + person.Key}},
			{{Text: "Назад", CallbackData: "edit:menu"}},
		}}
		return b.edit(ctx, q, person.Name+": выберите дату", markup)
	case "date":
		if len(parts) != 3 {
			return b.invalid(ctx, q)
		}
		if _, ok := b.people[parts[2]]; !ok {
			return b.invalid(ctx, q)
		}
		b.mu.Lock()
		b.awaiting[q.From.ID] = inputState{expires: time.Now().Add(5 * time.Minute), kind: "edit_date", token: parts[2]}
		b.mu.Unlock()
		return b.edit(ctx, q, "Отправьте дату в формате ДД.ММ.ГГГГ.", editCancelMenu())
	case "day":
		return b.editDay(ctx, q, parts)
	case "event":
		if len(parts) != 3 {
			return b.invalid(ctx, q)
		}
		return b.editEventCard(ctx, q, parts[2])
	case "targets":
		if len(parts) != 4 || (parts[3] != "one" && parts[3] != "all") {
			return b.invalid(ctx, q)
		}
		session, ok := b.editSession(q.From.ID, parts[2])
		if !ok {
			return b.edit(ctx, q, "Сеанс устарел. Выберите событие заново.", editBackMenu())
		}
		if parts[3] == "one" {
			session.selectedKeys = []string{session.personKey}
			session.targetsText = "• " + b.people[session.personKey].Name + " (только выбранное событие)"
		} else {
			session.selectedKeys = copyKeys(session.candidates)
		}
		b.saveEditSession(parts[2], session)
		return b.showEditActions(ctx, q, parts[2], session)
	case "field":
		if len(parts) != 4 || (parts[3] != "title" && parts[3] != "time" && parts[3] != "location") {
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
		if session.needsLink {
			if err := b.store.LinkEventCopies(q.From.ID, session.candidates); err != nil {
				return err
			}
			session.needsLink = false
			b.saveEditSession(parts[2], session)
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
	markup, text, err := b.editDayContent(q.From.ID, person.Key, from, to)
	if err != nil {
		return b.edit(ctx, q, "Не удалось загрузить расписание.", editBackMenu())
	}
	return b.edit(ctx, q, text, markup)
}

func (b *Bot) sendEditDay(ctx context.Context, chatID, actor int64, personKey string, date time.Time) error {
	markup, text, err := b.editDayContent(actor, personKey, date, date.AddDate(0, 0, 1))
	if err != nil {
		return b.client.Send(ctx, chatID, "Не удалось загрузить расписание.", editBackMenu())
	}
	return b.client.Send(ctx, chatID, text, markup)
}

func (b *Bot) editDayContent(actor int64, personKey string, from, to time.Time) (Markup, string, error) {
	person, ok := b.people[personKey]
	if !ok {
		return Markup{}, "", errors.New("неизвестный календарь")
	}
	events, _, err := person.Source.Events(from, to)
	if err != nil {
		return Markup{}, "", err
	}
	markup := Markup{}
	for _, event := range events {
		token, err := b.newEditSession(actor, person.Key, event)
		if err != nil {
			return Markup{}, "", err
		}
		label := event.Summary
		if !event.AllDay {
			label = event.Start.In(b.loc).Format("15:04") + " " + label
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, []Button{{Text: truncateRunes(label, 48), CallbackData: "edit:event:" + token}})
	}
	markup.InlineKeyboard = append(markup.InlineKeyboard, []Button{{Text: "Назад", CallbackData: "edit:person:" + person.Key}})
	if len(events) == 0 {
		return markup, "На эту дату событий нет.", nil
	}
	return markup, "Выберите событие", nil
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
		session.candidates = linked
		session.targetsText = b.linkedTargetNames(linked)
		b.saveEditSession(token, session)
		if len(linked) > 1 {
			markup := Markup{InlineKeyboard: [][]Button{
				{{Text: "Только у " + b.people[session.personKey].Name, CallbackData: "edit:targets:" + token + ":one"}},
				{{Text: "Во всех связанных", CallbackData: "edit:targets:" + token + ":all"}},
				{{Text: "Отмена", CallbackData: "edit:menu"}},
			}}
			return b.edit(ctx, q, eventDescription(session.event, b.loc)+"\n\nСвязанные календари:\n"+session.targetsText+"\n\nГде изменить событие?", markup)
		}
		session.selectedKeys = []string{session.personKey}
		b.saveEditSession(token, session)
		return b.showEditActions(ctx, q, token, session)
	}
	match, err := b.findMatchingCopies(session)
	if err != nil {
		if errors.Is(err, errAmbiguousCopyMatch) {
			return b.edit(ctx, q, "Нельзя однозначно найти копии: в одном календаре есть несколько событий на это же время.", editBackMenu())
		}
		return err
	}
	session.candidates = match.copies
	session.needsLink = true
	session.targetsText = b.matchTargetsText(match)
	b.saveEditSession(token, session)
	if len(match.copies) > 1 {
		markup := Markup{InlineKeyboard: [][]Button{
			{{Text: "Только у " + b.people[session.personKey].Name, CallbackData: "edit:targets:" + token + ":one"}},
			{{Text: "Во всех найденных", CallbackData: "edit:targets:" + token + ":all"}},
			{{Text: "Отмена", CallbackData: "edit:menu"}},
		}}
		return b.edit(ctx, q, eventDescription(session.event, b.loc)+"\n\nНайдены возможные копии:\n"+session.targetsText+"\n\nГде изменить событие?", markup)
	}
	session.selectedKeys = []string{session.personKey}
	b.saveEditSession(token, session)
	return b.showEditActions(ctx, q, token, session)
}

func (b *Bot) showEditActions(ctx context.Context, q CallbackQuery, token string, session editSession) error {
	rows := [][]Button{}
	if !session.event.AllDay {
		rows = append(rows, []Button{{Text: "Изменить время", CallbackData: "edit:field:" + token + ":time"}})
	}
	rows = append(rows,
		[]Button{{Text: "Изменить название", CallbackData: "edit:field:" + token + ":title"}},
		[]Button{{Text: "Изменить место", CallbackData: "edit:field:" + token + ":location"}},
		[]Button{{Text: "Назад", CallbackData: "edit:menu"}},
	)
	markup := Markup{InlineKeyboard: rows}
	text := eventDescription(session.event, b.loc) + "\n\nБудет применено к:\n" + session.targetsText + "\n\nЧто изменить?"
	return b.edit(ctx, q, text, markup)
}

func (b *Bot) promptEditInput(ctx context.Context, q CallbackQuery, token, field string, scope googleapi.Scope) error {
	b.mu.Lock()
	b.awaiting[q.From.ID] = inputState{expires: time.Now().Add(5 * time.Minute), kind: "edit_" + field, token: token, scope: scope}
	b.mu.Unlock()
	text := "Отправьте новое название."
	if field == "time" {
		text = "Отправьте новое время в формате 18:00-19:30."
	} else if field == "location" {
		text = "Отправьте новое место события. Чтобы удалить место, отправьте один дефис: -"
	}
	return b.edit(ctx, q, text+"\n\nОжидание действует 5 минут.", editCancelMenu())
}

func (b *Bot) handleEditInput(ctx context.Context, message Message, state inputState) error {
	if state.kind == "edit_date" {
		date, err := time.ParseInLocation("02.01.2006", strings.TrimSpace(message.Text), b.loc)
		if err != nil {
			return b.client.Send(ctx, message.Chat.ID, "Неверная дата. Пример: 25.10.2026.", editCancelMenu())
		}
		b.clearAwaiting(message.From.ID)
		return b.sendEditDay(ctx, message.Chat.ID, message.From.ID, state.token, date)
	}
	session, ok := b.editSession(message.From.ID, state.token)
	if !ok {
		b.clearAwaiting(message.From.ID)
		return b.client.Send(ctx, message.Chat.ID, "Сеанс устарел. Выберите событие заново.", editBackMenu())
	}
	request := schedule.Request{Actor: message.From.ID, SourceKey: session.personKey, SourceICalUID: session.event.UID, OriginalStart: session.event.OriginalStart, Scope: state.scope, TargetKeys: session.selectedKeys}
	switch state.kind {
	case "edit_title":
		title := strings.TrimSpace(message.Text)
		if title == "" || len([]rune(title)) > 300 {
			return b.client.Send(ctx, message.Chat.ID, "Название должно содержать от 1 до 300 символов. Отправьте /cancel для отмены.", editCancelMenu())
		}
		request.Summary = &title
	case "edit_time":
		start, end, err := parseTimeRange(message.Text, session.event.Start, b.loc)
		if err != nil {
			return b.client.Send(ctx, message.Chat.ID, "Неверный формат. Пример: 18:00-19:30. Отправьте /cancel для отмены.", editCancelMenu())
		}
		request.Start, request.End = &start, &end
	case "edit_location":
		location := strings.TrimSpace(message.Text)
		if location == "-" {
			location = ""
		}
		if len([]rune(location)) > 500 {
			return b.client.Send(ctx, message.Chat.ID, "Место должно содержать не более 500 символов.", editCancelMenu())
		}
		request.Location = &location
	default:
		return errors.New("неизвестное состояние ввода")
	}
	b.clearAwaiting(message.From.ID)
	session.pending = &request
	b.saveEditSession(state.token, session)
	preview := "Проверьте изменение:\n\n" + eventDescription(session.event, b.loc)
	if request.Summary != nil {
		preview += "\nНовое название: " + *request.Summary
	} else if request.Location != nil {
		if *request.Location == "" {
			preview += "\nНовое место: удалить"
		} else {
			preview += "\nНовое место: " + *request.Location
		}
	} else if request.Start != nil && request.End != nil {
		preview += "\nНовое время: " + request.Start.In(b.loc).Format("15:04") + "–" + request.End.In(b.loc).Format("15:04")
	}
	if request.Scope == googleapi.ScopeSeries {
		preview += "\nОбласть: вся серия"
	} else {
		preview += "\nОбласть: только это событие"
	}
	preview += "\nПрименить к:\n" + session.targetsText
	markup := Markup{InlineKeyboard: [][]Button{{{Text: "Подтвердить", CallbackData: "edit:confirm:" + state.token}}, {{Text: "Отмена", CallbackData: "edit:event:" + state.token}}}}
	return b.client.Send(ctx, message.Chat.ID, preview, markup)
}

func (b *Bot) formatEditResults(actor int64, results []schedule.CopyResult) string {
	return b.formatCopyResults(actor, "Изменение завершено:", results)
}

func (b *Bot) formatCopyResults(actor int64, heading string, results []schedule.CopyResult) string {
	var output strings.Builder
	output.WriteString(heading)
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

func (b *Bot) findMatchingCopies(session editSession) (copyMatch, error) {
	dayStart := time.Date(session.event.Start.In(b.loc).Year(), session.event.Start.In(b.loc).Month(), session.event.Start.In(b.loc).Day(), 0, 0, 0, 0, b.loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	exact := copyMatch{exactTitle: true}
	byInterval := copyMatch{}
	for _, key := range b.order {
		if key == session.personKey {
			copy := storage.EventCopy{CalendarKey: key, ICalUID: session.event.UID}
			exact.copies = append(exact.copies, copy)
			exact.events = append(exact.events, session.event)
			byInterval.copies = append(byInterval.copies, copy)
			byInterval.events = append(byInterval.events, session.event)
			continue
		}
		person := b.people[key]
		events, _, err := person.Source.Events(dayStart, dayEnd)
		if err != nil {
			return copyMatch{}, err
		}
		var intervalMatches []calendar.Event
		for _, event := range events {
			if sameInterval(session.event, event) {
				intervalMatches = append(intervalMatches, event)
			}
		}
		if len(intervalMatches) > 1 {
			return copyMatch{}, fmt.Errorf("%w: календарь %s", errAmbiguousCopyMatch, key)
		}
		if len(intervalMatches) == 1 {
			event := intervalMatches[0]
			copy := storage.EventCopy{CalendarKey: key, ICalUID: event.UID}
			byInterval.copies = append(byInterval.copies, copy)
			byInterval.events = append(byInterval.events, event)
			if sameTitle(session.event.Summary, event.Summary) {
				exact.copies = append(exact.copies, copy)
				exact.events = append(exact.events, event)
			}
		}
	}
	if len(exact.copies) >= 2 {
		return exact, nil
	}
	return byInterval, nil
}

func (b *Bot) matchTargetsText(match copyMatch) string {
	lines := make([]string, 0, len(match.copies))
	for i, candidate := range match.copies {
		lines = append(lines, "• "+b.people[candidate.CalendarKey].Name+": "+match.events[i].Summary)
	}
	if len(lines) == 1 {
		lines[0] += " (только выбранное событие)"
	} else if !match.exactTitle {
		lines = append(lines, "Названия различаются — проверьте список перед подтверждением.")
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) linkedTargetNames(copies []storage.EventCopy) string {
	lines := make([]string, 0, len(copies))
	for _, copy := range copies {
		name := copy.CalendarKey
		if person, ok := b.people[copy.CalendarKey]; ok {
			name = person.Name
		}
		lines = append(lines, "• "+name)
	}
	if len(lines) == 1 {
		lines[0] += " (только выбранное событие)"
	}
	return strings.Join(lines, "\n")
}

func sameCopy(a, b calendar.Event) bool {
	return sameTitle(a.Summary, b.Summary) && sameInterval(a, b)
}

func sameTitle(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

func sameInterval(a, b calendar.Event) bool {
	return a.Start.Equal(b.Start) && a.End.Equal(b.End) && a.AllDay == b.AllDay && a.Recurring == b.Recurring
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
	location := ""
	if strings.TrimSpace(event.Location) != "" {
		location = "\nМесто: " + strings.TrimSpace(event.Location)
	}
	if event.AllDay {
		return event.Summary + "\n" + event.Start.In(loc).Format("02.01.2006") + ", весь день" + location
	}
	return event.Summary + "\n" + event.Start.In(loc).Format("02.01.2006 15:04") + "–" + event.End.In(loc).Format("15:04") + location
}

func copyKeys(copies []storage.EventCopy) []string {
	keys := make([]string, 0, len(copies))
	for _, copy := range copies {
		keys = append(keys, copy.CalendarKey)
	}
	return keys
}

func editBackMenu() Markup {
	return Markup{InlineKeyboard: [][]Button{{{Text: "К выбору события", CallbackData: "edit:menu"}}, {{Text: "Главное меню", CallbackData: "main"}}}}
}

func editCancelMenu() Markup {
	return Markup{InlineKeyboard: [][]Button{{{Text: "Отмена", CallbackData: "edit:menu"}}}}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit-1]) + "…"
}

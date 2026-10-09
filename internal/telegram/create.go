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

	"github.com/eusatenko/calendar_telegramm_bot/internal/schedule"
)

func (b *Bot) scheduleCreate(ctx context.Context, q CallbackQuery, parts []string) error {
	if len(parts) < 2 {
		return b.invalid(ctx, q)
	}
	switch parts[1] {
	case "menu":
		token, err := b.newCreateSession(q.From.ID)
		if err != nil {
			return err
		}
		return b.showCreateTargets(ctx, q, token)
	case "target":
		if len(parts) != 4 {
			return b.invalid(ctx, q)
		}
		session, ok := b.createSession(q.From.ID, parts[2])
		if !ok {
			return b.edit(ctx, q, "Сеанс устарел.", mainMenuButton())
		}
		if _, ok = b.people[parts[3]]; !ok {
			return b.invalid(ctx, q)
		}
		session.targets[parts[3]] = !session.targets[parts[3]]
		b.saveCreateSession(parts[2], session)
		return b.showCreateTargets(ctx, q, parts[2])
	case "targets_done":
		if len(parts) != 3 {
			return b.invalid(ctx, q)
		}
		session, ok := b.createSession(q.From.ID, parts[2])
		if !ok {
			return b.edit(ctx, q, "Сеанс устарел.", mainMenuButton())
		}
		if len(selectedCreateKeys(session, b.order)) == 0 {
			return b.edit(ctx, q, "Выберите хотя бы один календарь.", createTargetsMarkup(parts[2], session, b))
		}
		return b.edit(ctx, q, "Когда добавить событие?", Markup{InlineKeyboard: [][]Button{
			{{Text: "Сегодня", CallbackData: "create:date:" + parts[2] + ":0"}, {Text: "Завтра", CallbackData: "create:date:" + parts[2] + ":1"}},
			{{Text: "Указать дату", CallbackData: "create:date:" + parts[2] + ":custom"}},
			{{Text: "Отмена", CallbackData: "main"}},
		}})
	case "date":
		if len(parts) != 4 {
			return b.invalid(ctx, q)
		}
		session, ok := b.createSession(q.From.ID, parts[2])
		if !ok {
			return b.edit(ctx, q, "Сеанс устарел.", mainMenuButton())
		}
		if parts[3] == "custom" {
			b.setAwaiting(q.From.ID, inputState{expires: time.Now().Add(5 * time.Minute), kind: "create_date", token: parts[2]})
			return b.edit(ctx, q, "Отправьте дату в формате ДД.ММ.ГГГГ.", createCancelMenu())
		}
		offset, err := strconv.Atoi(parts[3])
		if err != nil || (offset != 0 && offset != 1) {
			return b.invalid(ctx, q)
		}
		now := time.Now().In(b.loc).AddDate(0, 0, offset)
		session.date = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, b.loc)
		b.saveCreateSession(parts[2], session)
		return b.promptCreateTitleEdit(ctx, q, parts[2])
	case "type":
		if len(parts) != 4 || (parts[3] != "once" && parts[3] != "weekly") {
			return b.invalid(ctx, q)
		}
		session, ok := b.createSession(q.From.ID, parts[2])
		if !ok || session.summary == "" || session.start.IsZero() {
			return b.edit(ctx, q, "Сеанс устарел.", mainMenuButton())
		}
		if parts[3] == "weekly" {
			b.setAwaiting(q.From.ID, inputState{expires: time.Now().Add(5 * time.Minute), kind: "create_until", token: parts[2]})
			return b.edit(ctx, q, "Отправьте дату последнего повторения в формате ДД.ММ.ГГГГ.", createCancelMenu())
		}
		session.repeatUntil = nil
		b.saveCreateSession(parts[2], session)
		return b.showCreatePreview(ctx, q, parts[2], session)
	case "confirm":
		if len(parts) != 3 {
			return b.invalid(ctx, q)
		}
		session, ok := b.createSession(q.From.ID, parts[2])
		if !ok || session.summary == "" || session.start.IsZero() {
			return b.edit(ctx, q, "Сеанс устарел.", mainMenuButton())
		}
		results, err := b.editor.Create(ctx, schedule.CreateRequest{Actor: q.From.ID, TargetKeys: selectedCreateKeys(session, b.order), Summary: session.summary, Location: session.location, Start: session.start, End: session.end, RepeatUntil: session.repeatUntil})
		if err != nil {
			return err
		}
		notificationErr := b.notifyCreate(ctx, q.From, session, results)
		b.deleteCreateSession(parts[2])
		text := b.formatCopyResults(q.From.ID, "Создание завершено:", results)
		if notificationErr != nil {
			text += "\n⚠️ Не удалось отправить уведомление в семейную группу."
		}
		return b.edit(ctx, q, text, mainMenuButton())
	default:
		return b.invalid(ctx, q)
	}
}

func (b *Bot) handleCreateInput(ctx context.Context, message Message, state inputState) error {
	session, ok := b.createSession(message.From.ID, state.token)
	if !ok {
		b.clearAwaiting(message.From.ID)
		return b.client.Send(ctx, message.Chat.ID, "Сеанс устарел.", mainMenuButton())
	}
	switch state.kind {
	case "create_date":
		date, err := parseDate(message.Text, b.loc)
		if err != nil {
			return b.client.Send(ctx, message.Chat.ID, "Неверная дата. Пример: 25.10.2026.", createCancelMenu())
		}
		session.date = date
		b.saveCreateSession(state.token, session)
		b.setAwaiting(message.From.ID, inputState{expires: time.Now().Add(5 * time.Minute), kind: "create_title", token: state.token})
		return b.client.Send(ctx, message.Chat.ID, "Отправьте название события.", createCancelMenu())
	case "create_title":
		title := strings.TrimSpace(message.Text)
		if title == "" || len([]rune(title)) > 300 {
			return b.client.Send(ctx, message.Chat.ID, "Название должно содержать от 1 до 300 символов.", createCancelMenu())
		}
		session.summary = title
		b.saveCreateSession(state.token, session)
		b.setAwaiting(message.From.ID, inputState{expires: time.Now().Add(5 * time.Minute), kind: "create_time", token: state.token})
		return b.client.Send(ctx, message.Chat.ID, "Отправьте время в формате 18:00-19:30.", createCancelMenu())
	case "create_time":
		start, end, err := parseTimeRange(message.Text, session.date, b.loc)
		if err != nil {
			return b.client.Send(ctx, message.Chat.ID, "Неверное время. Пример: 18:00-19:30.", createCancelMenu())
		}
		session.start, session.end = start, end
		b.saveCreateSession(state.token, session)
		b.setAwaiting(message.From.ID, inputState{expires: time.Now().Add(5 * time.Minute), kind: "create_location", token: state.token})
		return b.client.Send(ctx, message.Chat.ID, "Отправьте место события или один дефис (-), чтобы пропустить.", createCancelMenu())
	case "create_location":
		location := strings.TrimSpace(message.Text)
		if location == "-" {
			location = ""
		}
		if len([]rune(location)) > 500 {
			return b.client.Send(ctx, message.Chat.ID, "Место должно содержать не более 500 символов.", createCancelMenu())
		}
		session.location = location
		b.saveCreateSession(state.token, session)
		b.clearAwaiting(message.From.ID)
		return b.client.Send(ctx, message.Chat.ID, "Тип события?", createTypeMarkup(state.token))
	case "create_until":
		until, err := parseDate(message.Text, b.loc)
		if err != nil || until.Before(time.Date(session.start.Year(), session.start.Month(), session.start.Day(), 0, 0, 0, 0, b.loc)) {
			return b.client.Send(ctx, message.Chat.ID, "Дата окончания должна быть не раньше даты события.", createCancelMenu())
		}
		session.repeatUntil = &until
		b.saveCreateSession(state.token, session)
		b.clearAwaiting(message.From.ID)
		return b.client.Send(ctx, message.Chat.ID, createPreviewText(session, b), createConfirmMarkup(state.token))
	default:
		return errors.New("неизвестное состояние создания")
	}
}

func (b *Bot) showCreateTargets(ctx context.Context, q CallbackQuery, token string) error {
	session, ok := b.createSession(q.From.ID, token)
	if !ok {
		return b.edit(ctx, q, "Сеанс устарел.", mainMenuButton())
	}
	return b.edit(ctx, q, "Кому добавить событие? Выберите один или несколько календарей.", createTargetsMarkup(token, session, b))
}

func createTargetsMarkup(token string, session createSession, b *Bot) Markup {
	rows := [][]Button{}
	for _, key := range b.order {
		prefix := "☐ "
		if session.targets[key] {
			prefix = "☑ "
		}
		rows = append(rows, []Button{{Text: prefix + b.people[key].Name, CallbackData: "create:target:" + token + ":" + key}})
	}
	rows = append(rows, []Button{{Text: "Продолжить", CallbackData: "create:targets_done:" + token}}, []Button{{Text: "Отмена", CallbackData: "main"}})
	return Markup{InlineKeyboard: rows}
}

func (b *Bot) promptCreateTitleEdit(ctx context.Context, q CallbackQuery, token string) error {
	b.setAwaiting(q.From.ID, inputState{expires: time.Now().Add(5 * time.Minute), kind: "create_title", token: token})
	return b.edit(ctx, q, "Отправьте название события.", createCancelMenu())
}

func (b *Bot) showCreatePreview(ctx context.Context, q CallbackQuery, token string, session createSession) error {
	return b.edit(ctx, q, createPreviewText(session, b), createConfirmMarkup(token))
}

func createPreviewText(session createSession, b *Bot) string {
	repeat := "Разовое"
	if session.repeatUntil != nil {
		repeat = "Еженедельно до " + session.repeatUntil.Format("02.01.2006")
	}
	location := ""
	if session.location != "" {
		location = "\nМесто: " + session.location
	}
	return fmt.Sprintf("Проверьте новое событие:\n\n%s\n%s–%s%s\n%s\nКалендари: %s", session.summary, session.start.Format("02.01.2006 15:04"), session.end.Format("15:04"), location, repeat, strings.Join(createTargetNames(session, b), ", "))
}

func createTypeMarkup(token string) Markup {
	return Markup{InlineKeyboard: [][]Button{
		{{Text: "Разовое", CallbackData: "create:type:" + token + ":once"}, {Text: "Еженедельно", CallbackData: "create:type:" + token + ":weekly"}},
		{{Text: "Отмена", CallbackData: "main"}},
	}}
}

func createConfirmMarkup(token string) Markup {
	return Markup{InlineKeyboard: [][]Button{{{Text: "Подтвердить", CallbackData: "create:confirm:" + token}}, {{Text: "Отмена", CallbackData: "main"}}}}
}

func createCancelMenu() Markup {
	return Markup{InlineKeyboard: [][]Button{{{Text: "Отмена", CallbackData: "main"}}}}
}

func (b *Bot) newCreateSession(actor int64) (string, error) {
	random := make([]byte, 9)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.createSessions[token] = createSession{expires: time.Now().Add(15 * time.Minute), actor: actor, targets: map[string]bool{}}
	return token, nil
}

func (b *Bot) createSession(actor int64, token string) (createSession, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	session, ok := b.createSessions[token]
	if !ok || session.actor != actor || time.Now().After(session.expires) {
		delete(b.createSessions, token)
		return createSession{}, false
	}
	return session, true
}

func (b *Bot) saveCreateSession(token string, session createSession) {
	b.mu.Lock()
	b.createSessions[token] = session
	b.mu.Unlock()
}

func (b *Bot) deleteCreateSession(token string) {
	b.mu.Lock()
	delete(b.createSessions, token)
	b.mu.Unlock()
}

func (b *Bot) setAwaiting(actor int64, state inputState) {
	b.mu.Lock()
	b.awaiting[actor] = state
	b.mu.Unlock()
}

func selectedCreateKeys(session createSession, order []string) []string {
	var keys []string
	for _, key := range order {
		if session.targets[key] {
			keys = append(keys, key)
		}
	}
	return keys
}

func createTargetNames(session createSession, b *Bot) []string {
	keys := selectedCreateKeys(session, b.order)
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, b.people[key].Name)
	}
	return names
}

func parseDate(value string, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation("02.01.2006", strings.TrimSpace(value), loc)
}

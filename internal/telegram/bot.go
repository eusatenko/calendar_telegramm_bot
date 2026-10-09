package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar/googleapi"
	"github.com/eusatenko/calendar_telegramm_bot/internal/formatter"
	"github.com/eusatenko/calendar_telegramm_bot/internal/schedule"
	"github.com/eusatenko/calendar_telegramm_bot/internal/storage"
)

type Person struct {
	Key, Name string
	Source    calendar.Source
}
type inputState struct {
	expires     time.Time
	kind, token string
	scope       googleapi.Scope
}
type editSession struct {
	expires         time.Time
	actor           int64
	personKey       string
	event           calendar.Event
	candidates      []storage.EventCopy
	selectedKeys    []string
	candidateLabels map[string]string
	targetsText     string
	targetsNote     string
	needsLink       bool
	pending         *schedule.Request
}
type createSession struct {
	expires     time.Time
	actor       int64
	targets     map[string]bool
	date        time.Time
	summary     string
	location    string
	start, end  time.Time
	repeatUntil *time.Time
}
type ScheduleEditor interface {
	Apply(context.Context, schedule.Request) ([]schedule.CopyResult, error)
	Create(context.Context, schedule.CreateRequest) ([]schedule.CopyResult, error)
}
type Bot struct {
	client         *Client
	store          *storage.Store
	people         map[string]Person
	order          []string
	loc            *time.Location
	inviteTTL      time.Duration
	username       string
	log            *slog.Logger
	mu             sync.Mutex
	awaiting       map[int64]inputState
	editor         ScheduleEditor
	editSessions   map[string]editSession
	createSessions map[string]createSession
}

func NewBot(client *Client, store *storage.Store, people []Person, loc *time.Location, inviteTTL time.Duration, username string, log *slog.Logger) *Bot {
	m := map[string]Person{}
	var order []string
	for _, p := range people {
		m[p.Key] = p
		order = append(order, p.Key)
	}
	return &Bot{client: client, store: store, people: m, order: order, loc: loc, inviteTTL: inviteTTL, username: username, log: log, awaiting: map[int64]inputState{}, editSessions: map[string]editSession{}, createSessions: map[string]createSession{}}
}

func (b *Bot) EnableScheduleEditing(editor ScheduleEditor) { b.editor = editor }

func (b *Bot) Run(ctx context.Context) error {
	offset := 0
	for {
		updates, err := b.client.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.Error("telegram polling failed", "error", err)
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if err = b.handle(ctx, u); err != nil {
				b.log.Error("update handling failed", "error", err)
			}
		}
	}
}
func (b *Bot) handle(ctx context.Context, u Update) error {
	if u.Message != nil {
		return b.handleMessage(ctx, *u.Message)
	}
	if u.Callback != nil {
		return b.handleCallback(ctx, *u.Callback)
	}
	return nil
}

func (b *Bot) handleMessage(ctx context.Context, m Message) error {
	if strings.HasPrefix(m.Text, "/start invite_") {
		fields := strings.Fields(m.Text)
		if len(fields) != 2 {
			return b.client.Send(ctx, m.Chat.ID, "Приглашение недействительно.", Markup{})
		}
		token := strings.TrimPrefix(fields[1], "invite_")
		if err := b.store.RedeemInvite(token, m.From.ID, m.From.Username, m.From.FirstName); err != nil {
			return b.client.Send(ctx, m.Chat.ID, "Приглашение недействительно, просрочено или уже использовано.", Markup{})
		}
		return b.client.Send(ctx, m.Chat.ID, "Доступ предоставлен.", mainMenu(false, b.editor != nil))
	}
	authorized, admin, err := b.store.Authorized(m.From.ID)
	if err != nil {
		return err
	}
	if !authorized {
		return b.client.Send(ctx, m.Chat.ID, "Доступ не предоставлен.", Markup{})
	}
	_ = b.store.Touch(m.From.ID, m.From.Username, m.From.FirstName)
	if command := botCommand(m.Text, b.username); command == "notifications_here" {
		if !admin {
			return b.client.Send(ctx, m.Chat.ID, "Недостаточно прав.", Markup{})
		}
		if m.Chat.Type != "group" && m.Chat.Type != "supergroup" {
			return b.client.Send(ctx, m.Chat.ID, "Эту команду нужно отправить в семейной группе.", Markup{})
		}
		if err = b.store.SetNotificationChat(m.From.ID, m.Chat.ID); err != nil {
			return err
		}
		return b.client.Send(ctx, m.Chat.ID, "✅ Уведомления об изменениях расписания будут приходить в эту группу.", Markup{})
	} else if command == "notifications_off" {
		if !admin {
			return b.client.Send(ctx, m.Chat.ID, "Недостаточно прав.", Markup{})
		}
		if err = b.store.ClearNotificationChat(m.From.ID); err != nil {
			return err
		}
		return b.client.Send(ctx, m.Chat.ID, "Уведомления отключены.", Markup{})
	}
	if m.Text == "/cancel" {
		b.clearAwaiting(m.From.ID)
		if admin {
			return b.client.Send(ctx, m.Chat.ID, "Действие отменено.", adminMenu())
		}
		return b.client.Send(ctx, m.Chat.ID, "Действие отменено.", mainMenu(false, b.editor != nil))
	}
	if state, ok := b.awaitingState(m.From.ID); ok {
		if strings.HasPrefix(state.kind, "edit_") {
			return b.handleEditInput(ctx, m, state)
		}
		if !admin {
			return b.client.Send(ctx, m.Chat.ID, "Недостаточно прав.", mainMenu(false, b.editor != nil))
		}
		if strings.HasPrefix(state.kind, "create_") {
			return b.handleCreateInput(ctx, m, state)
		}
		id, e := strconv.ParseInt(strings.TrimSpace(m.Text), 10, 64)
		if e != nil || id <= 0 {
			return b.client.Send(ctx, m.Chat.ID, "Нужен положительный числовой Telegram ID. Отправьте /cancel для отмены.", cancelMenu())
		}
		if e = b.store.RequireAdmin(ctx, m.From.ID); e != nil {
			return b.client.Send(ctx, m.Chat.ID, "Недостаточно прав.", mainMenu(false))
		}
		if e = b.store.AddUser(m.From.ID, id); e != nil {
			return e
		}
		b.clearAwaiting(m.From.ID)
		return b.client.Send(ctx, m.Chat.ID, fmt.Sprintf("Пользователь %d добавлен или активирован.", id), adminMenu())
	}
	return b.client.Send(ctx, m.Chat.ID, "Семейное расписание", mainMenu(admin, b.editor != nil))
}
func (b *Bot) handleCallback(ctx context.Context, q CallbackQuery) error {
	defer func() { _ = b.client.Answer(context.Background(), q.ID, "") }()
	authorized, admin, err := b.store.Authorized(q.From.ID)
	if err != nil {
		return err
	}
	if !authorized {
		return b.client.Edit(ctx, q.Message.Chat.ID, q.Message.MessageID, "Доступ не предоставлен.", Markup{})
	}
	parts := strings.Split(q.Data, ":")
	switch parts[0] {
	case "main":
		if len(parts) != 1 {
			return b.invalid(ctx, q)
		}
		return b.edit(ctx, q, "Семейное расписание", mainMenu(admin, b.editor != nil))
	case "person":
		if len(parts) == 2 {
			if _, ok := b.people[parts[1]]; !ok {
				return b.invalid(ctx, q)
			}
			return b.edit(ctx, q, "Выберите период", personMenu(parts[1]))
		}
		if len(parts) == 3 {
			return b.showPerson(ctx, q, parts[1], parts[2])
		}
		return b.invalid(ctx, q)
	case "all":
		if len(parts) != 2 || (parts[1] != "today" && parts[1] != "tomorrow") {
			return b.invalid(ctx, q)
		}
		return b.showAll(ctx, q, parts[1])
	case "admin":
		if !admin {
			return b.denied(ctx, q)
		}
		if err = b.store.RequireAdmin(ctx, q.From.ID); err != nil {
			return b.denied(ctx, q)
		}
		return b.admin(ctx, q, parts)
	case "edit":
		if b.editor == nil {
			return b.denied(ctx, q)
		}
		return b.scheduleEdit(ctx, q, parts)
	case "create":
		if b.editor == nil {
			return b.denied(ctx, q)
		}
		return b.scheduleCreate(ctx, q, parts)
	default:
		return b.invalid(ctx, q)
	}
}
func (b *Bot) showPerson(ctx context.Context, q CallbackQuery, key, period string) error {
	p, ok := b.people[key]
	if !ok {
		return b.invalid(ctx, q)
	}
	now := time.Now()
	var from, to time.Time
	switch period {
	case "today":
		from, to = calendar.DayRange(now, b.loc, 0)
	case "tomorrow":
		from, to = calendar.DayRange(now, b.loc, 1)
	case "week":
		from, to = calendar.WeekRange(now, b.loc)
	default:
		return b.invalid(ctx, q)
	}
	events, stale, err := p.Source.Events(from, to)
	if err != nil {
		b.log.Error("calendar query failed", "telegram_user_id", q.From.ID, "calendar_name", key, "requested_period", period, "error", err)
		return b.edit(ctx, q, "Не удалось получить расписание. Попробуйте ещё раз позже.", personMenu(key))
	}
	text := formatter.Day(p.Name, from, events, stale)
	if period == "week" {
		text = formatter.Week(p.Name, from, events, stale)
	}
	return b.editSplit(ctx, q, text, personMenu(key))
}
func (b *Bot) showAll(ctx context.Context, q CallbackQuery, period string) error {
	offset := 0
	if period == "tomorrow" {
		offset = 1
	}
	from, to := calendar.DayRange(time.Now(), b.loc, offset)
	items := make([]formatter.PersonEvents, 0, len(b.order))
	for _, k := range b.order {
		p := b.people[k]
		e, s, err := p.Source.Events(from, to)
		items = append(items, formatter.PersonEvents{Name: p.Name, Events: e, Stale: s, Error: err != nil})
		if err != nil {
			b.log.Error("combined calendar query failed", "calendar_name", k, "error", err)
		}
	}
	title := "Сегодня — расписание всех"
	if offset == 1 {
		title = "Завтра — расписание всех"
	}
	return b.editSplit(ctx, q, formatter.Combined(title, from, items), mainMenuButton())
}
func (b *Bot) admin(ctx context.Context, q CallbackQuery, p []string) error {
	if len(p) < 2 {
		return b.invalid(ctx, q)
	}
	switch p[1] {
	case "menu":
		b.clearAwaiting(q.From.ID)
		return b.edit(ctx, q, "Управление доступом", adminMenu())
	case "users":
		users, e := b.store.ListUsers()
		if e != nil {
			return e
		}
		var sb strings.Builder
		sb.WriteString("Пользователи")
		keys := Markup{}
		for _, u := range users {
			status := "активен"
			action := "off"
			label := "Отключить"
			if !u.Active {
				status = "неактивен"
				action = "on"
				label = "Включить"
			}
			fmt.Fprintf(&sb, "\n\n%d", u.ID)
			if u.Username != "" {
				fmt.Fprintf(&sb, " @%s", u.Username)
			}
			if u.FirstName != "" {
				fmt.Fprintf(&sb, " — %s", u.FirstName)
			}
			fmt.Fprintf(&sb, "\n%s, %s", u.Role, status)
			keys.InlineKeyboard = append(keys.InlineKeyboard, []Button{{Text: label + " " + strconv.FormatInt(u.ID, 10), CallbackData: "admin:toggle:" + strconv.FormatInt(u.ID, 10) + ":" + action}})
		}
		keys.InlineKeyboard = append(keys.InlineKeyboard, []Button{{Text: "Назад", CallbackData: "admin:menu"}})
		return b.editSplit(ctx, q, sb.String(), keys)
	case "add":
		b.mu.Lock()
		b.awaiting[q.From.ID] = inputState{expires: time.Now().Add(5 * time.Minute), kind: "add_user"}
		b.mu.Unlock()
		return b.edit(ctx, q, "Отправьте числовой Telegram ID пользователя. Ожидание действует 5 минут.", cancelMenu())
	case "invite":
		token, e := b.store.CreateInvite(q.From.ID, b.inviteTTL)
		if e != nil {
			return e
		}
		link := fmt.Sprintf("https://t.me/%s?start=invite_%s", b.username, token)
		return b.edit(ctx, q, "Одноразовое приглашение действительно "+b.inviteTTL.String()+":\n\n"+link, adminMenu())
	case "toggle":
		if len(p) != 4 {
			return b.invalid(ctx, q)
		}
		id, e := strconv.ParseInt(p[2], 10, 64)
		if e != nil || id <= 0 || (p[3] != "on" && p[3] != "off") {
			return b.invalid(ctx, q)
		}
		e = b.store.SetActive(q.From.ID, id, p[3] == "on")
		if errors.Is(e, storage.ErrLastAdmin) {
			return b.edit(ctx, q, "Нельзя отключить последнего активного администратора.", adminMenu())
		}
		if e != nil {
			return e
		}
		return b.edit(ctx, q, "Статус пользователя обновлён.", adminMenu())
	default:
		return b.invalid(ctx, q)
	}
}

func (b *Bot) edit(ctx context.Context, q CallbackQuery, text string, m Markup) error {
	if err := b.client.Edit(ctx, q.Message.Chat.ID, q.Message.MessageID, text, m); err != nil {
		return b.client.Send(ctx, q.Message.Chat.ID, text, m)
	}
	return nil
}
func (b *Bot) editSplit(ctx context.Context, q CallbackQuery, text string, m Markup) error {
	parts := formatter.Split(text, 3900)
	if err := b.edit(ctx, q, parts[0], m); err != nil {
		return err
	}
	for _, p := range parts[1:] {
		if err := b.client.Send(ctx, q.Message.Chat.ID, p, m); err != nil {
			return err
		}
	}
	return nil
}
func (b *Bot) invalid(ctx context.Context, q CallbackQuery) error {
	return b.edit(ctx, q, "Некорректная команда.", mainMenuButton())
}
func (b *Bot) denied(ctx context.Context, q CallbackQuery) error {
	return b.edit(ctx, q, "Недостаточно прав.", mainMenuButton())
}
func (b *Bot) awaitingState(id int64) (inputState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.awaiting[id]
	if ok && time.Now().After(s.expires) {
		delete(b.awaiting, id)
		return inputState{}, false
	}
	return s, ok
}
func (b *Bot) clearAwaiting(id int64) { b.mu.Lock(); delete(b.awaiting, id); b.mu.Unlock() }
func botCommand(text, username string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return ""
	}
	command := strings.TrimPrefix(fields[0], "/")
	parts := strings.SplitN(command, "@", 2)
	if len(parts) == 2 && !strings.EqualFold(parts[1], username) {
		return ""
	}
	return strings.ToLower(parts[0])
}
func mainMenu(admin bool, editing ...bool) Markup {
	m := Markup{InlineKeyboard: [][]Button{{{Text: "Сегодня всех", CallbackData: "all:today"}, {Text: "Завтра всех", CallbackData: "all:tomorrow"}}, {{Text: "Аня", CallbackData: "person:anya"}, {Text: "Лёша", CallbackData: "person:lesha"}}, {{Text: "Саша", CallbackData: "person:sasha"}, {Text: "Настя", CallbackData: "person:nastya"}}}}
	if len(editing) > 0 && editing[0] {
		calendarActions := []Button{{Text: "Изменить расписание", CallbackData: "edit:menu"}, {Text: "Добавить событие", CallbackData: "create:menu"}}
		m.InlineKeyboard = append(m.InlineKeyboard, calendarActions)
	}
	if admin {
		m.InlineKeyboard = append(m.InlineKeyboard, []Button{{Text: "Управление доступом", CallbackData: "admin:menu"}})
	}
	return m
}
func mainMenuButton() Markup {
	return Markup{InlineKeyboard: [][]Button{{{Text: "Главное меню", CallbackData: "main"}}}}
}
func personMenu(k string) Markup {
	return Markup{InlineKeyboard: [][]Button{{{Text: "Сегодня", CallbackData: "person:" + k + ":today"}, {Text: "Завтра", CallbackData: "person:" + k + ":tomorrow"}}, {{Text: "Неделя", CallbackData: "person:" + k + ":week"}}, {{Text: "Другой человек", CallbackData: "main"}, {Text: "Главное меню", CallbackData: "main"}}}}
}
func adminMenu() Markup {
	rows := [][]Button{{{Text: "Пользователи", CallbackData: "admin:users"}}, {{Text: "Добавить по ID", CallbackData: "admin:add"}}, {{Text: "Создать приглашение", CallbackData: "admin:invite"}}}
	rows = append(rows, []Button{{Text: "Назад", CallbackData: "main"}})
	return Markup{InlineKeyboard: rows}
}
func cancelMenu() Markup {
	return Markup{InlineKeyboard: [][]Button{{{Text: "Отмена", CallbackData: "admin:menu"}}}}
}

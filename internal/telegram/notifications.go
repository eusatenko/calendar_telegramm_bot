package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar/googleapi"
	"github.com/eusatenko/calendar_telegramm_bot/internal/schedule"
)

func (b *Bot) notifyCreate(ctx context.Context, actor User, session createSession, results []schedule.CopyResult) error {
	if !hasSuccessfulCopy(results) {
		return nil
	}
	var text strings.Builder
	text.WriteString("📅 Событие добавлено\n")
	text.WriteString(session.summary)
	fmt.Fprintf(&text, "\n%s–%s", session.start.In(b.loc).Format("02.01.2006 15:04"), session.end.In(b.loc).Format("15:04"))
	if session.location != "" {
		text.WriteString("\nМесто: " + session.location)
	}
	if session.repeatUntil != nil {
		text.WriteString("\nПовтор: еженедельно до " + session.repeatUntil.In(b.loc).Format("02.01.2006"))
	}
	appendNotificationResults(&text, results)
	text.WriteString("\nИзменил: " + actorName(actor))
	return b.sendNotification(ctx, text.String())
}

func (b *Bot) notifyEdit(ctx context.Context, actor User, session editSession, request schedule.Request, results []schedule.CopyResult) error {
	if !hasSuccessfulCopy(results) {
		return nil
	}
	var text strings.Builder
	if request.Delete {
		text.WriteString("🗑 Событие удалено\n")
	} else {
		text.WriteString("✏️ Событие изменено\n")
	}
	text.WriteString(eventDescription(session.event, b.loc))
	if request.Delete {
		if session.event.Recurring && request.Scope == googleapi.ScopeSeries {
			text.WriteString("\nУдалена вся повторяющаяся серия")
		} else if session.event.Recurring {
			text.WriteString("\nУдалён только этот экземпляр")
		}
	} else if request.Summary != nil {
		text.WriteString("\nНовое название: " + *request.Summary)
	} else if request.Location != nil {
		location := *request.Location
		if location == "" {
			location = "удалено"
		}
		text.WriteString("\nНовое место: " + location)
	} else if request.Start != nil && request.End != nil {
		fmt.Fprintf(&text, "\nНовое время: %s–%s", request.Start.In(b.loc).Format("15:04"), request.End.In(b.loc).Format("15:04"))
	}
	if session.event.Recurring && !request.Delete {
		if request.Scope == googleapi.ScopeSeries {
			text.WriteString("\nОбласть: вся серия")
		} else {
			text.WriteString("\nОбласть: только этот экземпляр")
		}
	}
	appendNotificationResults(&text, results)
	text.WriteString("\nИзменил: " + actorName(actor))
	return b.sendNotification(ctx, text.String())
}

func (b *Bot) sendNotification(ctx context.Context, text string) error {
	chatID, configured, err := b.store.NotificationChat()
	if err != nil || !configured {
		return err
	}
	if err = b.client.Send(ctx, chatID, text, Markup{}); err != nil {
		b.log.Error("calendar notification failed", "chat_id", chatID, "error", err)
		return err
	}
	return nil
}

func hasSuccessfulCopy(results []schedule.CopyResult) bool {
	for _, result := range results {
		if result.Err == nil {
			return true
		}
	}
	return false
}

func appendNotificationResults(text *strings.Builder, results []schedule.CopyResult) {
	text.WriteString("\nКалендари:")
	for _, result := range results {
		status := " ✅"
		if result.Err != nil {
			status = " ❌"
		}
		text.WriteString("\n• " + result.Name + status)
	}
}

func actorName(actor User) string {
	if strings.TrimSpace(actor.FirstName) != "" {
		return strings.TrimSpace(actor.FirstName)
	}
	if actor.Username != "" {
		return "@" + actor.Username
	}
	return strconv.FormatInt(actor.ID, 10)
}

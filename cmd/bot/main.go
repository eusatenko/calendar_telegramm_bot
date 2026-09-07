package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar/cache"
	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar/fetcher"
	calendarical "github.com/eusatenko/calendar_telegramm_bot/internal/calendar/ical"
	"github.com/eusatenko/calendar_telegramm_bot/internal/config"
	"github.com/eusatenko/calendar_telegramm_bot/internal/storage"
	"github.com/eusatenko/calendar_telegramm_bot/internal/telegram"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		log.Error("configuration error", "error", err)
		os.Exit(1)
	}
	store, err := storage.Open(cfg.DatabasePath)
	if err != nil {
		log.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if err = store.BootstrapAdmin(cfg.AdminID); err != nil {
		log.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}
	f := fetcher.New(cfg.HTTPTimeout)
	people := make([]telegram.Person, 0, len(cfg.Calendars))
	for _, cc := range cfg.Calendars {
		c := cc
		src := cache.New(cfg.CacheTTL, func() (*calendarical.Calendar, error) {
			started := time.Now()
			cal, e := f.Fetch(c.URL, cfg.Timezone)
			if e != nil {
				log.Error("calendar refresh failed", "calendar_name", c.Key, "duration", time.Since(started), "error", e)
			} else {
				log.Info("calendar refreshed", "calendar_name", c.Key, "duration", time.Since(started))
			}
			return cal, e
		})
		people = append(people, telegram.Person{Key: c.Key, Name: c.Name, Source: src})
	}
	client := telegram.NewClient(cfg.BotToken)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	username, err := client.Username(ctx)
	if err != nil {
		log.Error("telegram getMe failed", "error", err)
		os.Exit(1)
	}
	log.Info("bot started", "timezone", cfg.TimezoneName)
	if err = telegram.NewBot(client, store, people, cfg.Timezone, cfg.InviteTTL, username, log).Run(ctx); err != nil && ctx.Err() == nil {
		log.Error("bot stopped", "error", err)
		os.Exit(1)
	}
}

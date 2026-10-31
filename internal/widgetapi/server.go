package widgetapi

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/eusatenko/calendar_telegramm_bot/internal/calendar"
	"github.com/eusatenko/calendar_telegramm_bot/internal/formatter"
)

type Person struct {
	Key, Name string
	Source    calendar.Source
}

type Server struct {
	token  string
	people []Person
	loc    *time.Location
	log    *slog.Logger
	now    func() time.Time
}

type response struct {
	Date        string                  `json:"date"`
	Title       string                  `json:"title"`
	Mode        string                  `json:"mode"`
	GeneratedAt time.Time               `json:"generated_at"`
	Rows        []formatter.ScheduleRow `json:"rows"`
}

func New(token string, people []Person, loc *time.Location, log *slog.Logger) http.Handler {
	s := &Server{token: token, people: people, loc: loc, log: log, now: time.Now}
	return s.handler()
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/widget/schedule", s.schedule)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	dayName := r.URL.Query().Get("day")
	offset := 0
	title := "Сегодня — расписание всех"
	if dayName == "tomorrow" {
		offset = 1
		title = "Завтра — расписание всех"
	} else if dayName != "" && dayName != "today" {
		http.Error(w, "day must be today or tomorrow", http.StatusBadRequest)
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "chronological"
	}
	if mode != "chronological" && mode != "people" {
		http.Error(w, "mode must be chronological or people", http.StatusBadRequest)
		return
	}
	from, to := calendar.DayRange(s.now(), s.loc, offset)
	items := make([]formatter.PersonEvents, len(s.people))
	var queries sync.WaitGroup
	for index, person := range s.people {
		queries.Add(1)
		go func() {
			defer queries.Done()
			events, stale, err := person.Source.Events(from, to)
			items[index] = formatter.PersonEvents{Name: person.Name, Events: events, Stale: stale, Error: err != nil}
			if err != nil {
				s.log.Error("widget calendar query failed", "calendar_name", person.Key, "error", err)
			}
		}()
	}
	queries.Wait()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response{
		Date: from.Format("2006-01-02"), Title: title, Mode: mode,
		GeneratedAt: s.now().UTC(), Rows: formatter.CombinedRows(from, items, mode),
	})
}

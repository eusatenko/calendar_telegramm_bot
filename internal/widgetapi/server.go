package widgetapi

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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
	token   string
	people  []Person
	loc     *time.Location
	log     *slog.Logger
	now     func() time.Time
	pairing *Pairing
}

type response struct {
	Date        string                  `json:"date"`
	Title       string                  `json:"title"`
	Mode        string                  `json:"mode"`
	GeneratedAt time.Time               `json:"generated_at"`
	Rows        []formatter.ScheduleRow `json:"rows"`
}

func New(token string, people []Person, loc *time.Location, log *slog.Logger, pairing *Pairing) http.Handler {
	s := &Server{token: token, people: people, loc: loc, log: log, now: time.Now, pairing: pairing}
	return s.handler()
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/widget/schedule", s.schedule)
	mux.HandleFunc("POST /api/widget/pair", s.pair)
	mux.HandleFunc("GET /widget/setup", s.setup)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/api/widget/pair" || r.URL.Path == "/widget/setup" {
			mux.ServeHTTP(w, r)
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		http.NotFound(w, r)
		return
	}
	var request struct {
		Code string `json:"code"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.Code) == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	serverURL, token, err := s.pairing.Redeem(request.Code)
	if err != nil {
		http.Error(w, "pairing code is invalid, expired, or already used", http.StatusGone)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"server_url": serverURL, "token": token})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		http.NotFound(w, r)
		return
	}
	code := r.URL.Query().Get("code")
	decodedCode, err := url.QueryUnescape(code)
	if err != nil {
		http.Error(w, "invalid pairing link", http.StatusBadRequest)
		return
	}
	if decoded, decodeErr := base64.RawURLEncoding.DecodeString(decodedCode); decodeErr != nil || len(decoded) != 32 {
		http.Error(w, "invalid pairing link", http.StatusBadRequest)
		return
	}
	deepLink := "familycalendar://setup?code=" + url.QueryEscape(decodedCode) + "&server=" + url.QueryEscape(s.pairing.publicURL)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = io.WriteString(w, "<!doctype html><html lang=\"ru\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Family Calendar</title><style>body{font:18px sans-serif;max-width:520px;margin:48px auto;padding:24px;color:#27222f}a{display:block;padding:16px;text-align:center;background:#6750a4;color:white;border-radius:14px;text-decoration:none}</style><h1>Family Calendar</h1><p>Нажмите кнопку, чтобы подключить виджет. Ссылка одноразовая.</p><a href=\""+html.EscapeString(deepLink)+"\">Открыть приложение</a></html>")
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

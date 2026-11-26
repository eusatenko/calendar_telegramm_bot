package widgetapi

import (
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed miniapp/*
var miniFiles embed.FS

type accessLookup func(int64) (bool, bool, error)

func NewMiniApp(botID string, access accessLookup, people []Person, loc *time.Location, log *slog.Logger) http.Handler {
	// Telegram production Ed25519 key: core.telegram.org/bots/webapps#validating-data-for-third-party-use
	key, _ := hex.DecodeString("e7bf03a2fa4602af4580703d88dda5bb59f32ed8b02a56c187fe7d34caed242d")
	s := &Server{people: people, loc: loc, log: log, now: time.Now}
	return s.miniHandler(botID, key, access)
}

func miniUser(raw, botID string, key ed25519.PublicKey, now time.Time) (int64, bool) {
	if raw == "" || len(raw) > 16384 {
		return 0, false
	}
	fields, err := url.ParseQuery(raw)
	if err != nil {
		return 0, false
	}
	var lines []string
	for k, v := range fields {
		if len(v) != 1 || strings.ContainsAny(k, "=\r\n") || strings.ContainsAny(v[0], "\r\n") {
			return 0, false
		}
		if k != "signature" && k != "hash" {
			lines = append(lines, k+"="+v[0])
		}
	}
	sort.Strings(lines)
	signature, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(fields.Get("signature"), "="))
	if err != nil || !ed25519.Verify(key, []byte(botID+":WebAppData\n"+strings.Join(lines, "\n")), signature) {
		return 0, false
	}
	issued, err := strconv.ParseInt(fields.Get("auth_date"), 10, 64)
	if err != nil || issued < now.Add(-time.Hour).Unix() || issued > now.Add(time.Minute).Unix() {
		return 0, false
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal([]byte(fields.Get("user")), &user) != nil || user.ID <= 0 {
		return 0, false
	}
	return user.ID, true
}

func (s *Server) miniHandler(botID string, key ed25519.PublicKey, access accessLookup) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /calendar/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, miniFiles, "miniapp/index.html")
	})
	for _, name := range []string{"app.js", "style.css"} {
		mux.HandleFunc("GET /calendar/"+name, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, miniFiles, "miniapp/"+name)
		})
	}
	mux.HandleFunc("GET /calendar/schedule", func(w http.ResponseWriter, r *http.Request) {
		raw, schemeOK := strings.CutPrefix(r.Header.Get("Authorization"), "tma ")
		if !schemeOK {
			http.Error(w, "unauthorized", 401)
			return
		}
		id, ok := miniUser(raw, botID, key, s.now())
		if !ok {
			http.Error(w, "unauthorized", 401)
			return
		}
		active, _, err := access(id)
		if err != nil {
			http.Error(w, "access unavailable", 503)
			return
		}
		if !active {
			http.Error(w, "forbidden", 403)
			return
		}
		q := r.URL.Query()
		date := q.Get("date")
		if date == "" {
			date = s.now().In(s.loc).Format(time.DateOnly)
		}
		from, err := time.ParseInLocation(time.DateOnly, date, s.loc)
		if err != nil {
			http.Error(w, "invalid date", 400)
			return
		}
		mode := q.Get("mode")
		if mode == "" {
			mode = "chronological"
		}
		if mode != "people" && mode != "chronological" {
			http.Error(w, "invalid mode", 400)
			return
		}
		people := s.people
		if filter := q.Get("person"); filter != "" {
			people = nil
			for _, p := range s.people {
				if p.Key == filter {
					people = []Person{p}
					break
				}
			}
			if len(people) == 0 {
				http.Error(w, "invalid person", 400)
				return
			}
		}
		s.writeSchedule(w, from, from.AddDate(0, 0, 1), date, mode, people)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' https://telegram.org; style-src 'self' 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'")
		mux.ServeHTTP(w, r)
	})
}

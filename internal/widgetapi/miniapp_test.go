package widgetapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMiniAppSignedSchedule(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 25, 22, 0, 0, 0, time.UTC)
	s := &Server{loc: time.FixedZone("MSK", 10800), now: func() time.Time { return now }, log: slog.Default(), people: []Person{{"anya", "Аня", fakeSource{}}, {"lesha", "Лёша", fakeSource{}}}}
	active := true
	h := s.miniHandler("123", pub, func(id int64) (bool, bool, error) { return active && id == 42, false, nil })
	sign := func(at time.Time) string {
		stamp := strconv.FormatInt(at.Unix(), 10)
		user := `{"id":42}`
		sig := ed25519.Sign(priv, []byte("123:WebAppData\nauth_date="+stamp+"\nuser="+user))
		return url.Values{"user": {user}, "auth_date": {stamp}, "signature": {base64.RawURLEncoding.EncodeToString(sig)}}.Encode()
	}
	valid := sign(now)
	get := func(path, auth string, want int) string {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s status %d want %d", path, w.Code, want)
		}
		return w.Body.String()
	}
	for _, file := range []string{"", "app.js", "style.css"} {
		get("/calendar/"+file, "", 200)
	}
	const path = "/calendar/schedule"
	for _, auth := range []string{"", "Bearer " + valid, "tma " + sign(now.Add(-2*time.Hour)), "tma " + strings.Replace(valid, "42", "43", 1), "tma " + valid + "&user=42"} {
		get(path, auth, 401)
	}
	var out response
	if err = json.Unmarshal([]byte(get(path, "tma "+valid, 200)), &out); err != nil || out.Date != "2026-11-26" {
		t.Fatalf("default day: %+v %v", out, err)
	}
	result := get(path+"?date=2026-11-28&mode=people&person=anya", "tma "+valid, 200)
	if !strings.Contains(result, "2026-11-28") || !strings.Contains(result, "Аня") || strings.Contains(result, "Лёша") {
		t.Fatal(result)
	}
	for _, query := range []string{"date=2026-02-30", "mode=bad", "person=unknown"} {
		get(path+"?"+query, "tma "+valid, 400)
	}
	active = false
	get(path, "tma "+valid, 403)
}

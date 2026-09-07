package fetcher

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const validICS = "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:x\nDTSTART:20260907T100000Z\nDTEND:20260907T110000Z\nSUMMARY:X\nEND:VEVENT\nEND:VCALENDAR\n"

func TestFetchHTTPSStatusAndUserAgent(t *testing.T) {
	seenUA := ""
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA = r.Header.Get("User-Agent")
		fmt.Fprint(w, validICS)
	}))
	defer srv.Close()
	f := New(time.Second)
	f.client = srv.Client()
	cal, e := f.Fetch(srv.URL, time.UTC)
	if e != nil {
		t.Fatal(e)
	}
	events, e := cal.Events(time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC))
	if e != nil || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), e)
	}
	if seenUA == "" {
		t.Fatal("missing user agent")
	}
}
func TestFetchRejectsHTTPAndNon2xx(t *testing.T) {
	f := New(time.Second)
	if _, e := f.Fetch("http://example.com/a.ics", time.UTC); e == nil {
		t.Fatal("http accepted")
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 503) }))
	defer srv.Close()
	f.client = srv.Client()
	if _, e := f.Fetch(srv.URL, time.UTC); e == nil || !strings.Contains(e.Error(), "503") {
		t.Fatalf("%v", e)
	}
}
func TestFetchRejectsMalformedAndTooLarge(t *testing.T) {
	for _, tc := range []struct{ name, body string }{{"malformed", "not ical"}, {"large", strings.Repeat("x", maxICSSize+1)}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			f := New(5 * time.Second)
			f.client = srv.Client()
			if _, e := f.Fetch(srv.URL, time.UTC); e == nil {
				t.Fatal("expected error")
			}
		})
	}
}

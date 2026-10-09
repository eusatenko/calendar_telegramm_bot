package googleapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := New(Credentials{ClientID: "id", ClientSecret: "secret", RefreshToken: "refresh"}, time.Second)
	client.apiBase = server.URL
	client.tokenURL = server.URL + "/token"
	return client
}

func TestApplySingleRecurringOccurrence(t *testing.T) {
	var patchedPath, ifMatch string
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"access","expires_in":3600}`)
		case strings.HasSuffix(r.URL.Path, "/events"):
			_, _ = io.WriteString(w, `{"items":[{"id":"parent","etag":"p","iCalUID":"uid","recurrence":["RRULE:FREQ=WEEKLY"]}]}`)
		case strings.HasSuffix(r.URL.Path, "/instances"):
			_, _ = io.WriteString(w, `{"items":[{"id":"instance","etag":"instance-tag","recurringEventId":"parent","originalStartTime":{"dateTime":"2026-10-12T18:00:00+03:00"}}]}`)
		case r.Method == http.MethodPatch:
			patchedPath, ifMatch = r.URL.Path, r.Header.Get("If-Match")
			_, _ = io.WriteString(w, `{}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	})
	original := time.Date(2026, 10, 12, 18, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	title := "Новое название"
	if err := client.Apply(context.Background(), "family@example.com", Edit{ICalUID: "uid", OriginalStart: original, Scope: ScopeOccurrence, Summary: &title}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(patchedPath, "/events/instance") || ifMatch != "instance-tag" {
		t.Fatalf("path=%s etag=%s", patchedPath, ifMatch)
	}
}

func TestApplySeriesTimePreservesParentDate(t *testing.T) {
	var patch map[string]apiDateTime
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"access","expires_in":3600}`)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"items":[{"id":"parent","etag":"p","iCalUID":"uid","start":{"dateTime":"2026-01-05T10:00:00+03:00"},"end":{"dateTime":"2026-01-05T11:00:00+03:00"},"recurrence":["RRULE:FREQ=WEEKLY"]}]}`)
		case r.Method == http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{}`)
		}
	})
	loc := time.FixedZone("MSK", 3*60*60)
	start := time.Date(2026, 10, 12, 18, 30, 0, 0, loc)
	end := start.Add(90 * time.Minute)
	if err := client.Apply(context.Background(), "calendar", Edit{ICalUID: "uid", Scope: ScopeSeries, Start: &start, End: &end}); err != nil {
		t.Fatal(err)
	}
	if patch["start"].DateTime != "2026-01-05T18:30:00+03:00" || patch["end"].DateTime != "2026-01-05T20:00:00+03:00" {
		t.Fatalf("patch=%+v", patch)
	}
}

func TestApplyLocation(t *testing.T) {
	var patch map[string]any
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/token":
			_, _ = io.WriteString(w, `{"access_token":"access","expires_in":3600}`)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"items":[{"id":"event","etag":"tag","iCalUID":"uid"}]}`)
		case r.Method == http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{}`)
		}
	})
	location := "Большой зал"
	if err := client.Apply(context.Background(), "calendar", Edit{ICalUID: "uid", Scope: ScopeSeries, Location: &location}); err != nil {
		t.Fatal(err)
	}
	if patch["location"] != location {
		t.Fatalf("patch=%+v", patch)
	}
}

func TestCreateWeeklyEvent(t *testing.T) {
	var body map[string]any
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"access","expires_in":3600}`)
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/events") {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"iCalUID":"created@example.com"}`)
	})
	loc := time.FixedZone("MSK", 3*60*60)
	start := time.Date(2026, 10, 10, 13, 0, 0, 0, loc)
	end := start.Add(time.Hour)
	until := time.Date(2026, 12, 31, 0, 0, 0, 0, loc)
	uid, err := client.Create(context.Background(), "calendar", Create{Summary: "Современный", Location: "Большой зал", Start: start, End: end, RepeatUntil: &until})
	if err != nil {
		t.Fatal(err)
	}
	if uid != "created@example.com" || body["summary"] != "Современный" || body["location"] != "Большой зал" {
		t.Fatalf("uid=%q body=%+v", uid, body)
	}
	recurrence, ok := body["recurrence"].([]any)
	if !ok || len(recurrence) != 1 || recurrence[0] != "RRULE:FREQ=WEEKLY;UNTIL=20261231T205959Z" {
		t.Fatalf("recurrence=%+v", body["recurrence"])
	}
	if _, exists := body["id"]; exists {
		t.Fatalf("output-only fields must not be sent: %+v", body)
	}
}

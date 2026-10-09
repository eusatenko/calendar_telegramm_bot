package googleapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrEventNotFound = errors.New("связанное событие не найдено")

type Scope string

const (
	ScopeOccurrence Scope = "occurrence"
	ScopeSeries     Scope = "series"
)

type Credentials struct {
	ClientID, ClientSecret, RefreshToken string
}

type Edit struct {
	ICalUID       string
	OriginalStart time.Time
	Scope         Scope
	Summary       *string
	Location      *string
	Start, End    *time.Time
}

type Create struct {
	Summary     string
	Start, End  time.Time
	RepeatUntil *time.Time
}

type Client struct {
	http              *http.Client
	credentials       Credentials
	apiBase, tokenURL string

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

type apiDateTime struct {
	Date     string `json:"date,omitempty"`
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type apiEvent struct {
	ID               string      `json:"id"`
	ETag             string      `json:"etag"`
	ICalUID          string      `json:"iCalUID"`
	Summary          string      `json:"summary"`
	Location         string      `json:"location,omitempty"`
	RecurringEventID string      `json:"recurringEventId"`
	OriginalStart    apiDateTime `json:"originalStartTime"`
	Start            apiDateTime `json:"start"`
	End              apiDateTime `json:"end"`
	Recurrence       []string    `json:"recurrence"`
}

func New(credentials Credentials, timeout time.Duration) *Client {
	return &Client{
		http:        &http.Client{Timeout: timeout},
		credentials: credentials,
		apiBase:     "https://www.googleapis.com/calendar/v3",
		tokenURL:    "https://oauth2.googleapis.com/token",
	}
}

func (c *Client) Apply(ctx context.Context, calendarID string, edit Edit) error {
	if edit.ICalUID == "" || (edit.Scope != ScopeOccurrence && edit.Scope != ScopeSeries) {
		return errors.New("некорректный запрос изменения")
	}
	parent, err := c.findParent(ctx, calendarID, edit.ICalUID)
	if err != nil {
		return err
	}
	target := parent
	if edit.Scope == ScopeOccurrence && len(parent.Recurrence) > 0 {
		if edit.OriginalStart.IsZero() {
			return errors.New("не задано исходное время экземпля")
		}
		target, err = c.findInstance(ctx, calendarID, parent.ID, edit.OriginalStart)
		if err != nil {
			return err
		}
	}

	patch := map[string]any{}
	if edit.Summary != nil {
		patch["summary"] = strings.TrimSpace(*edit.Summary)
	}
	if edit.Location != nil {
		patch["location"] = strings.TrimSpace(*edit.Location)
	}
	if edit.Start != nil || edit.End != nil {
		if edit.Start == nil || edit.End == nil || !edit.End.After(*edit.Start) {
			return errors.New("некорректный интервал события")
		}
		start, end := *edit.Start, *edit.End
		if edit.Scope == ScopeSeries && len(parent.Recurrence) > 0 {
			parentStart, parseErr := parseDateTime(parent.Start)
			if parseErr != nil {
				return parseErr
			}
			duration := end.Sub(start)
			loc := start.Location()
			start = time.Date(parentStart.In(loc).Year(), parentStart.In(loc).Month(), parentStart.In(loc).Day(), start.In(loc).Hour(), start.In(loc).Minute(), start.In(loc).Second(), 0, loc)
			end = start.Add(duration)
		}
		patch["start"] = apiDateTime{DateTime: start.Format(time.RFC3339), TimeZone: start.Location().String()}
		patch["end"] = apiDateTime{DateTime: end.Format(time.RFC3339), TimeZone: end.Location().String()}
	}
	if len(patch) == 0 {
		return errors.New("изменения не заданы")
	}
	return c.patchEvent(ctx, calendarID, target.ID, target.ETag, patch)
}

func (c *Client) Create(ctx context.Context, calendarID string, event Create) (string, error) {
	if strings.TrimSpace(event.Summary) == "" || !event.End.After(event.Start) {
		return "", errors.New("некорректное новое событие")
	}
	body := map[string]any{
		"summary": strings.TrimSpace(event.Summary),
		"start":   apiDateTime{DateTime: event.Start.Format(time.RFC3339), TimeZone: event.Start.Location().String()},
		"end":     apiDateTime{DateTime: event.End.Format(time.RFC3339), TimeZone: event.End.Location().String()},
	}
	if event.RepeatUntil != nil {
		until := time.Date(event.RepeatUntil.Year(), event.RepeatUntil.Month(), event.RepeatUntil.Day(), 23, 59, 59, 0, event.RepeatUntil.Location()).UTC()
		body["recurrence"] = []string{"RRULE:FREQ=WEEKLY;UNTIL=" + until.Format("20060102T150405Z")}
	}
	var created apiEvent
	path := "/calendars/" + url.PathEscape(calendarID) + "/events"
	if err := c.doJSON(ctx, http.MethodPost, path, body, nil, &created); err != nil {
		return "", err
	}
	if created.ICalUID == "" {
		return "", errors.New("Google Calendar API: созданное событие без iCalUID")
	}
	return created.ICalUID, nil
}

func (c *Client) findParent(ctx context.Context, calendarID, iCalUID string) (apiEvent, error) {
	query := url.Values{"iCalUID": {iCalUID}, "showDeleted": {"false"}, "maxResults": {"50"}}
	var response struct {
		Items []apiEvent `json:"items"`
	}
	path := "/calendars/" + url.PathEscape(calendarID) + "/events?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &response); err != nil {
		return apiEvent{}, err
	}
	for _, event := range response.Items {
		if event.RecurringEventID == "" {
			return event, nil
		}
	}
	return apiEvent{}, ErrEventNotFound
}

func (c *Client) findInstance(ctx context.Context, calendarID, parentID string, originalStart time.Time) (apiEvent, error) {
	query := url.Values{
		"showDeleted": {"false"},
		"timeMin":     {originalStart.Add(-time.Hour).Format(time.RFC3339)},
		"timeMax":     {originalStart.Add(time.Hour).Format(time.RFC3339)},
		"maxResults":  {"10"},
	}
	var response struct {
		Items []apiEvent `json:"items"`
	}
	path := "/calendars/" + url.PathEscape(calendarID) + "/events/" + url.PathEscape(parentID) + "/instances?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &response); err != nil {
		return apiEvent{}, err
	}
	for _, event := range response.Items {
		candidate, err := parseDateTime(event.OriginalStart)
		if err == nil && candidate.Equal(originalStart) {
			return event, nil
		}
	}
	return apiEvent{}, ErrEventNotFound
}

func (c *Client) patchEvent(ctx context.Context, calendarID, eventID, etag string, patch map[string]any) error {
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	path := "/calendars/" + url.PathEscape(calendarID) + "/events/" + url.PathEscape(eventID)
	return c.doJSON(ctx, http.MethodPatch, path, patch, headers, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, headers map[string]string, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.token(ctx, attempt > 0)
		if err != nil {
			return err
		}
		var reader io.Reader
		if body != nil {
			encoded, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				return marshalErr
			}
			reader = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			return ErrEventNotFound
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("Google Calendar API: HTTP %d", resp.StatusCode)
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("Google Calendar API: авторизация отклонена")
}

func (c *Client) token(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.accessToken != "" && time.Until(c.tokenExpiry) > time.Minute {
		return c.accessToken, nil
	}
	values := url.Values{
		"client_id":     {c.credentials.ClientID},
		"client_secret": {c.credentials.ClientSecret},
		"refresh_token": {c.credentials.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var envelope struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Google OAuth: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return "", err
	}
	if envelope.AccessToken == "" {
		return "", errors.New("Google OAuth: пустой access token")
	}
	c.accessToken = envelope.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(envelope.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

func parseDateTime(value apiDateTime) (time.Time, error) {
	if value.DateTime != "" {
		return time.Parse(time.RFC3339, value.DateTime)
	}
	if value.Date != "" {
		return time.Parse("2006-01-02", value.Date)
	}
	return time.Time{}, errors.New("Google Calendar API: событие без даты")
}

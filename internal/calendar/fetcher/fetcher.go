package fetcher

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	calendarical "github.com/eusatenko/calendar_telegramm_bot/internal/calendar/ical"
)

const maxICSSize = 10 << 20

type Fetcher struct {
	client    *http.Client
	userAgent string
}

func New(timeout time.Duration) *Fetcher {
	return &Fetcher{client: &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect на незащищённый URL запрещён")
		}
		if len(via) >= 5 {
			return fmt.Errorf("слишком много redirect")
		}
		return nil
	}}, userAgent: "family-calendar-bot/1.0"}
}

func (f *Fetcher) Fetch(rawURL string, loc *time.Location) (*calendarical.Calendar, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("недопустимый URL календаря")
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("создание запроса: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("загрузка iCal: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("сервер iCal вернул HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, maxICSSize+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("чтение iCal: %w", err)
	}
	if len(b) > maxICSSize {
		return nil, fmt.Errorf("ответ iCal превышает лимит %d байт", maxICSSize)
	}
	cal, err := calendarical.Parse(bytes.NewReader(b), loc)
	if err != nil {
		return nil, fmt.Errorf("разбор iCal: %w", err)
	}
	return cal, nil
}

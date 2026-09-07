package calendar

import (
	"testing"
	"time"
)

func TestWeekRangeSundayAndBoundaries(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	tests := []struct{ now, want string }{{"2026-09-13T20:00:00+03:00", "2026-09-07"}, {"2027-01-01T12:00:00+03:00", "2026-12-28"}, {"2028-02-29T12:00:00+03:00", "2028-02-28"}}
	for _, tt := range tests {
		now, _ := time.Parse(time.RFC3339, tt.now)
		start, end := WeekRange(now, loc)
		if got := start.Format("2006-01-02"); got != tt.want {
			t.Errorf("%s: got %s want %s", tt.now, got, tt.want)
		}
		if end.Sub(start) != 7*24*time.Hour {
			t.Errorf("week duration=%v", end.Sub(start))
		}
	}
}
func TestDayRangeMidnight(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	now, _ := time.Parse(time.RFC3339, "2026-12-31T23:59:00+03:00")
	start, end := DayRange(now, loc, 1)
	if start.Format("2006-01-02 15:04") != "2027-01-01 00:00" || end.Format("2006-01-02 15:04") != "2027-01-02 00:00" {
		t.Fatalf("got %v..%v", start, end)
	}
}

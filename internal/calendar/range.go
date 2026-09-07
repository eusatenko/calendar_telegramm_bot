package calendar

import "time"

func DayRange(now time.Time, loc *time.Location, offset int) (time.Time, time.Time) {
	n := now.In(loc).AddDate(0, 0, offset)
	start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 0, 1)
}

func WeekRange(now time.Time, loc *time.Location) (time.Time, time.Time) {
	n := now.In(loc)
	days := (int(n.Weekday()) + 6) % 7
	start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -days)
	return start, start.AddDate(0, 0, 7)
}

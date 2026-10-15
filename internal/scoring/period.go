package scoring

import "time"

type PeriodType string

const (
	PeriodWeek    PeriodType = "WEEK"
	PeriodMonth   PeriodType = "MONTH"
	PeriodQuarter PeriodType = "QUARTER"
)

func ValidPeriodType(t string) bool {
	switch PeriodType(t) {
	case PeriodWeek, PeriodMonth, PeriodQuarter:
		return true
	}
	return false
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// PeriodBounds returns the first and last day (inclusive) of the period that
// contains day.
func PeriodBounds(t PeriodType, day time.Time) (time.Time, time.Time) {
	d := dateOnly(day)
	switch t {
	case PeriodWeek:
		offset := (int(d.Weekday()) + 6) % 7 // Monday = 0
		start := d.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 6)
	case PeriodQuarter:
		month := time.Month((int(d.Month())-1)/3*3 + 1)
		start := time.Date(d.Year(), month, 1, 0, 0, 0, 0, d.Location())
		return start, start.AddDate(0, 3, -1)
	default:
		start := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location())
		return start, start.AddDate(0, 1, -1)
	}
}

// PreviousStart returns the start of the period before the one starting at start.
func PreviousStart(t PeriodType, start time.Time) time.Time {
	prev, _ := PeriodBounds(t, start.AddDate(0, 0, -1))
	return prev
}

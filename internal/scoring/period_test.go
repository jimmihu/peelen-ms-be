package scoring

import (
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestPeriodBounds(t *testing.T) {
	cases := []struct {
		t          PeriodType
		in         time.Time
		start, end time.Time
	}{
		{PeriodWeek, day(2026, 10, 15), day(2026, 10, 12), day(2026, 10, 18)}, // Thursday -> Mon..Sun
		{PeriodWeek, day(2026, 10, 18), day(2026, 10, 12), day(2026, 10, 18)}, // Sunday
		{PeriodMonth, day(2026, 2, 10), day(2026, 2, 1), day(2026, 2, 28)},
		{PeriodQuarter, day(2026, 11, 5), day(2026, 10, 1), day(2026, 12, 31)},
	}
	for _, c := range cases {
		start, end := PeriodBounds(c.t, c.in)
		if !start.Equal(c.start) || !end.Equal(c.end) {
			t.Errorf("%s %s: got %s..%s", c.t, c.in.Format("2006-01-02"), start.Format("2006-01-02"), end.Format("2006-01-02"))
		}
	}
	if got := PreviousStart(PeriodQuarter, day(2026, 1, 1)); !got.Equal(day(2025, 10, 1)) {
		t.Errorf("previous quarter = %s", got)
	}
	if got := PreviousStart(PeriodWeek, day(2026, 10, 12)); !got.Equal(day(2026, 10, 5)) {
		t.Errorf("previous week = %s", got)
	}
}

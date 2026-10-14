package scoring

import "testing"

func okPeriod(score float64, dims ...float64) Result {
	r := Result{Status: StatusOK, Score: score}
	for _, d := range dims {
		r.Items = append(r.Items, ItemResult{Score: d})
	}
	return r
}

func TestLevelUsesGates(t *testing.T) {
	cases := []struct {
		name    string
		history []Result
		want    int
	}{
		{"business doc: three strong periods", []Result{okPeriod(88, 80, 90), okPeriod(90, 80, 92), okPeriod(86, 78, 90)}, 4},
		{"business doc: one weak dimension drops to 3", []Result{okPeriod(88, 80, 90), okPeriod(90, 80, 92), okPeriod(86, 78, 90), okPeriod(86, 70, 95)}, 3},
		{"single good period reaches 2 only", []Result{okPeriod(99, 99, 99)}, 2},
		{"below level 2", []Result{okPeriod(55, 60, 50)}, 1},
		{"no data breaks the streak", []Result{okPeriod(90, 90), {Status: StatusNoData}, okPeriod(90, 90)}, 2},
		{"empty history", nil, 1},
	}
	for _, c := range cases {
		if got := Level(c.history, DefaultLevels); got != c.want {
			t.Errorf("%s: level = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestValidateLevels(t *testing.T) {
	if err := ValidateLevels(DefaultLevels); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	bad := [][]LevelRule{
		{{Level: 2, MinScore: 60, MinDimension: 50, ConsecutivePeriods: 1}}, // missing 3..5
		{{2, 60, 50, 1}, {3, 55, 65, 2}, {4, 85, 75, 3}, {5, 95, 85, 3}},    // score decreases
		{{2, 60, 50, 0}, {3, 75, 65, 2}, {4, 85, 75, 3}, {5, 95, 85, 3}},    // zero periods
		{{2, 60, 50, 1}, {3, 75, 65, 2}, {4, 85, 75, 3}, {5, 101, 85, 3}},   // > 100
	}
	for i, levels := range bad {
		if ValidateLevels(levels) == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestEvaluateMaturityPeriod(t *testing.T) {
	c := Counts{Tasks: 10, Reported: 10, Completed: 10, OnTime: 9, LateUpload: 2, LateMinutesSum: 60, LateMinutesCount: 10}
	got := EvaluateMaturityPeriod(c, []Item{
		{Code: "ON_TIME_RATE", Weight: 50},     // 90
		{Code: "LATE_UPLOAD_RATE", Weight: 25}, // 100-20 = 80
		{Code: "AVG_LATE_MINUTES", Weight: 25}, // 100-0.5*6 = 97
	})
	if got.Status != StatusOK || !near(got.Score, 89.25) {
		t.Fatalf("got %+v", got)
	}
	if EvaluateMaturityPeriod(Counts{}, nil).Status != StatusNoData {
		t.Fatal("empty counts should be NO_DATA")
	}
}

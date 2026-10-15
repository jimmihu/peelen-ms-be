package scoring

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestActual(t *testing.T) {
	c := Counts{Tasks: 10, Reported: 9, Completed: 8, OnTime: 6, LateUpload: 3, EvidenceComplete: 7,
		K3Findings: 4, K3FindingsWithNotes: 3, LateMinutesSum: 40, LateMinutesCount: 8,
		DailyOnTimeRates: []float64{50, 70}}
	cases := map[string]float64{
		"COMPLETION_RATE": 80, "ON_TIME_RATE": 60, "AVG_LATE_MINUTES": 5,
		"LATE_UPLOAD_RATE": 33.33, "EVIDENCE_COMPLETE_RATE": 87.5, "K3_FINDING_NOTES_RATE": 75,
		"CONSISTENCY": 80, // stddev(50,70)=10 -> 100-2*10
	}
	for code, want := range cases {
		got, ok := Actual(code, c)
		if !ok || !near(got, want) {
			t.Errorf("%s = %v (ok=%v), want %v", code, got, ok, want)
		}
	}
}

func TestActualNotApplicable(t *testing.T) {
	c := Counts{Tasks: 5, Reported: 0, Completed: 0, DailyOnTimeRates: []float64{100}}
	for _, code := range []string{"AVG_LATE_MINUTES", "LATE_UPLOAD_RATE", "EVIDENCE_COMPLETE_RATE", "K3_FINDING_NOTES_RATE", "CONSISTENCY"} {
		if _, ok := Actual(code, c); ok {
			t.Errorf("%s should be N/A", code)
		}
	}
	if _, ok := Actual("UNKNOWN", c); ok {
		t.Error("unknown code should be N/A")
	}
}

func TestEvaluateSLA(t *testing.T) {
	// Business doc example: completion 97/95, on-time 85/90, evidence 99/98.
	c := Counts{Tasks: 100, Reported: 100, Completed: 97, OnTime: 85, EvidenceComplete: 96}
	got := EvaluateSLA(c, []Item{
		{Code: "COMPLETION_RATE", Weight: 40, Target: 95},
		{Code: "ON_TIME_RATE", Weight: 40, Target: 90},
		{Code: "EVIDENCE_COMPLETE_RATE", Weight: 20, Target: 98},
	})
	if got.Status != StatusOK || got.Compliant || !near(got.Score, 97.78) {
		t.Fatalf("got %+v", got)
	}
	if got.Items[1].Met || !got.Items[0].Met || !got.Items[2].Met {
		t.Fatalf("met flags wrong: %+v", got.Items)
	}
}

func TestEvaluateSLALowerBetterAndNA(t *testing.T) {
	c := Counts{Tasks: 10, Reported: 10, Completed: 10, OnTime: 10, LateMinutesSum: 100, LateMinutesCount: 10}
	got := EvaluateSLA(c, []Item{
		{Code: "AVG_LATE_MINUTES", Weight: 50, Target: 5},        // actual 10 -> score 50
		{Code: "K3_FINDING_NOTES_RATE", Weight: 50, Target: 100}, // N/A, weight dropped
	})
	if !near(got.Score, 50) || got.Compliant || !got.Items[1].NA {
		t.Fatalf("got %+v", got)
	}
	zero := EvaluateSLA(Counts{Tasks: 1, Completed: 1, LateMinutesCount: 1}, []Item{{Code: "AVG_LATE_MINUTES", Weight: 100, Target: 0}})
	if !near(zero.Score, 100) || !zero.Compliant {
		t.Fatalf("zero target with zero late should pass: %+v", zero)
	}
}

func TestEvaluateSLANoData(t *testing.T) {
	got := EvaluateSLA(Counts{}, []Item{{Code: "COMPLETION_RATE", Weight: 100, Target: 95}})
	if got.Status != StatusNoData {
		t.Fatalf("status = %s, want NO_DATA", got.Status)
	}
}

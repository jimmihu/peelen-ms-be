package scoring

import "math"

const (
	StatusOK     = "OK"
	StatusNoData = "NO_DATA"
)

// Counts aggregates task_day_facts for one scope and period.
type Counts struct {
	Tasks, Reported, Completed, OnTime, LateUpload, EvidenceComplete int
	K3Findings, K3FindingsWithNotes                                  int
	LateMinutesSum, LateMinutesCount                                 int
	DailyOnTimeRates                                                 []float64
}

func percent(part, whole int) (float64, bool) {
	if whole == 0 {
		return 0, false
	}
	return float64(part) / float64(whole) * 100, true
}

// Actual returns the indicator's raw value; ok is false when it is N/A
// (its denominator is zero) or the code is unknown.
func Actual(code string, c Counts) (float64, bool) {
	switch code {
	case "COMPLETION_RATE":
		return percent(c.Completed, c.Tasks)
	case "ON_TIME_RATE":
		return percent(c.OnTime, c.Tasks)
	case "AVG_LATE_MINUTES":
		if c.LateMinutesCount == 0 {
			return 0, false
		}
		return float64(c.LateMinutesSum) / float64(c.LateMinutesCount), true
	case "LATE_UPLOAD_RATE":
		return percent(c.LateUpload, c.Reported)
	case "EVIDENCE_COMPLETE_RATE":
		return percent(c.EvidenceComplete, c.Completed)
	case "K3_FINDING_NOTES_RATE":
		return percent(c.K3FindingsWithNotes, c.K3Findings)
	case "CONSISTENCY":
		if len(c.DailyOnTimeRates) < 2 {
			return 0, false
		}
		return math.Max(0, 100-2*stddev(c.DailyOnTimeRates)), true
	default:
		return 0, false
	}
}

func stddev(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var squares float64
	for _, v := range values {
		squares += (v - mean) * (v - mean)
	}
	return math.Sqrt(squares / float64(len(values)))
}

type Item struct {
	Code   string
	Weight float64
	Target float64
}

type ItemResult struct {
	Code   string  `json:"code"`
	Actual float64 `json:"actual"`
	Target float64 `json:"target"`
	Score  float64 `json:"score"`
	Weight float64 `json:"weight"`
	Met    bool    `json:"met"`
	NA     bool    `json:"is_na"`
}

type Result struct {
	Status    string       `json:"status"`
	Score     float64      `json:"score"`
	Compliant bool         `json:"compliant"`
	Level     int          `json:"level,omitempty"`
	TaskCount int          `json:"task_count"`
	Items     []ItemResult `json:"items"`
}

// targetScore scores an actual value against its target on 0..100.
func targetScore(direction Direction, actual, target float64) (score float64, met bool) {
	if direction == LowerBetter {
		if actual <= target {
			return 100, true
		}
		return target / actual * 100, false
	}
	if target <= 0 || actual >= target {
		return 100, true
	}
	return actual / target * 100, false
}

// weighted averages item scores over non-N/A items, renormalizing weights.
func weighted(items []ItemResult) float64 {
	var total, weights float64
	for _, item := range items {
		if !item.NA {
			total += item.Score * item.Weight
			weights += item.Weight
		}
	}
	if weights == 0 {
		return 0
	}
	return total / weights
}

// EvaluateSLA scores counts against an SLA rule's items.
func EvaluateSLA(c Counts, items []Item) Result {
	if c.Tasks == 0 {
		return Result{Status: StatusNoData}
	}
	result := Result{Status: StatusOK, Compliant: true, TaskCount: c.Tasks}
	for _, item := range items {
		indicator, _ := Lookup(item.Code)
		actual, ok := Actual(item.Code, c)
		r := ItemResult{Code: item.Code, Target: item.Target, Weight: item.Weight, Actual: actual, NA: !ok}
		if ok {
			r.Score, r.Met = targetScore(indicator.Direction, actual, item.Target)
			if !r.Met {
				result.Compliant = false
			}
		}
		result.Items = append(result.Items, r)
	}
	result.Score = weighted(result.Items)
	return result
}

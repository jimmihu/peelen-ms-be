package scoring

import (
	"fmt"
	"math"
)

type LevelRule struct {
	Level              int     `json:"level"`
	MinScore           float64 `json:"min_score"`
	MinDimension       float64 `json:"min_dimension"`
	ConsecutivePeriods int     `json:"consecutive_periods"`
}

// DefaultLevels are copied into a maturity rule created without levels.
var DefaultLevels = []LevelRule{
	{Level: 2, MinScore: 60, MinDimension: 50, ConsecutivePeriods: 1},
	{Level: 3, MinScore: 75, MinDimension: 65, ConsecutivePeriods: 2},
	{Level: 4, MinScore: 85, MinDimension: 75, ConsecutivePeriods: 3},
	{Level: 5, MinScore: 95, MinDimension: 85, ConsecutivePeriods: 3},
}

// ValidateLevels requires levels 2..5 exactly once, values in range, and each
// column non-decreasing as the level rises.
func ValidateLevels(levels []LevelRule) error {
	if len(levels) != 4 {
		return fmt.Errorf("levels 2 to 5 are required")
	}
	byLevel := map[int]LevelRule{}
	for _, l := range levels {
		if l.Level < 2 || l.Level > 5 {
			return fmt.Errorf("level %d is out of range 2-5", l.Level)
		}
		if l.MinScore < 0 || l.MinScore > 100 || l.MinDimension < 0 || l.MinDimension > 100 || l.ConsecutivePeriods < 1 || l.ConsecutivePeriods > 12 {
			return fmt.Errorf("level %d has out-of-range values", l.Level)
		}
		byLevel[l.Level] = l
	}
	if len(byLevel) != 4 {
		return fmt.Errorf("each level 2 to 5 must appear once")
	}
	for level := 3; level <= 5; level++ {
		prev, cur := byLevel[level-1], byLevel[level]
		if cur.MinScore < prev.MinScore || cur.MinDimension < prev.MinDimension || cur.ConsecutivePeriods < prev.ConsecutivePeriods {
			return fmt.Errorf("level %d requirements must be at least those of level %d", level, level-1)
		}
	}
	return nil
}

func MaxConsecutive(levels []LevelRule) int {
	max := 1
	for _, l := range levels {
		if l.ConsecutivePeriods > max {
			max = l.ConsecutivePeriods
		}
	}
	return max
}

// DimensionScore maps a dimension's actual value to 0..100 with a fixed curve.
func DimensionScore(code string, actual float64) float64 {
	switch code {
	case "LATE_UPLOAD_RATE":
		return math.Max(0, 100-actual)
	case "AVG_LATE_MINUTES":
		return math.Max(0, 100-0.5*actual)
	default:
		return math.Min(100, math.Max(0, actual))
	}
}

// EvaluateMaturityPeriod scores one period's dimensions (item targets unused).
func EvaluateMaturityPeriod(c Counts, items []Item) Result {
	if c.Tasks == 0 {
		return Result{Status: StatusNoData}
	}
	result := Result{Status: StatusOK, TaskCount: c.Tasks}
	for _, item := range items {
		actual, ok := Actual(item.Code, c)
		r := ItemResult{Code: item.Code, Weight: item.Weight, Actual: actual, NA: !ok}
		if ok {
			r.Score = DimensionScore(item.Code, actual)
		}
		result.Items = append(result.Items, r)
	}
	result.Score = weighted(result.Items)
	return result
}

func meets(period Result, rule LevelRule) bool {
	if period.Status != StatusOK || period.Score < rule.MinScore {
		return false
	}
	for _, item := range period.Items {
		if !item.NA && item.Score < rule.MinDimension {
			return false
		}
	}
	return true
}

// Level returns the highest level whose gate holds for its required number of
// most recent periods in history (oldest first). Level 1 always holds.
func Level(history []Result, levels []LevelRule) int {
	best := 1
	for _, rule := range levels {
		n := rule.ConsecutivePeriods
		if rule.Level <= best || len(history) < n {
			continue
		}
		ok := true
		for _, period := range history[len(history)-n:] {
			if !meets(period, rule) {
				ok = false
				break
			}
		}
		if ok {
			best = rule.Level
		}
	}
	return best
}

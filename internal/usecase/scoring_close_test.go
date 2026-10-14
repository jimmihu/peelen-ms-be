package usecase

import (
	"context"
	"testing"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/scoring"
)

type closeRepoFake struct {
	evalRepoFake
	lastFinal time.Time
	versions  []entity.ScoreRule
	saved     []entity.ScoreResult
}

func (f *closeRepoFake) LastFinalDate(context.Context) (time.Time, error) { return f.lastFinal, nil }
func (f *closeRepoFake) RuleKeys(context.Context) ([]entity.RuleKey, error) {
	return []entity.RuleKey{{Kind: "SLA", VendorID: testVendor}}, nil
}
func (f *closeRepoFake) RuleVersions(context.Context, entity.RuleKey) ([]entity.ScoreRule, error) {
	return f.versions, nil
}
func (f *closeRepoFake) OverrideSitesAt(context.Context, string, string, time.Time) ([]string, error) {
	return nil, nil
}
func (f *closeRepoFake) ResultExists(_ context.Context, _, scope, _ string, _ *string, _ string, start time.Time) (bool, error) {
	for _, s := range f.saved {
		if s.ScopeType == scope && s.PeriodStart.Equal(start) {
			return true, nil
		}
	}
	return false, nil
}
func (f *closeRepoFake) SaveResult(_ context.Context, r entity.ScoreResult) error {
	f.saved = append(f.saved, r)
	return nil
}

func TestClosePeriodsScoresFromRuleCreation(t *testing.T) {
	loc := time.UTC
	repo := &closeRepoFake{
		evalRepoFake: evalRepoFake{counts: map[string][]entity.ScopeCounts{
			entity.ScopeVendor: {{VendorID: testVendor, Counts: scoring.Counts{Tasks: 10, Completed: 10, Reported: 10}}},
		}},
		lastFinal: time.Date(2026, 10, 14, 0, 0, 0, 0, loc),
		versions: []entity.ScoreRule{{ID: "v1", Kind: "SLA", VendorID: testVendor, PeriodType: "MONTH", Version: 1,
			CreatedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, loc), // mid-August
			Items:     []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}}},
	}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl)
	n, err := u.ClosePeriods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// August (rule created mid-month, scored in full) and September; October not finished.
	var vendorStarts []string
	for _, r := range repo.saved {
		if r.ScopeType == entity.ScopeVendor {
			vendorStarts = append(vendorStarts, r.PeriodStart.Format("2006-01-02"))
		}
	}
	if n != 2 || len(vendorStarts) != 2 || vendorStarts[0] != "2026-08-01" || vendorStarts[1] != "2026-09-01" {
		t.Fatalf("closed %d periods, vendor starts %v; want 2026-08-01 and 2026-09-01", n, vendorStarts)
	}
	// Running again freezes nothing new.
	if n, _ := u.ClosePeriods(context.Background()); n != 0 {
		t.Fatalf("second run closed %d periods, want 0", n)
	}
}

func TestClosePeriodsComparesDatesInPlantZone(t *testing.T) {
	// The DB returns the last final date at UTC midnight. In a zone behind UTC
	// that instant is still the previous local day, so a period ending on that
	// date would be skipped unless the date is read in the plant zone.
	west := time.FixedZone("UTC-5", -5*3600)
	repo := &closeRepoFake{
		evalRepoFake: evalRepoFake{counts: map[string][]entity.ScopeCounts{
			entity.ScopeVendor: {{VendorID: testVendor, Counts: scoring.Counts{Tasks: 1, Completed: 1, Reported: 1}}},
		}},
		lastFinal: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
		versions: []entity.ScoreRule{{ID: "v1", Kind: "SLA", VendorID: testVendor, PeriodType: "MONTH", Version: 1,
			CreatedAt: time.Date(2026, 9, 2, 0, 0, 0, 0, west),
			Items:     []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}}},
	}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl).WithLocation(west)
	if n, err := u.ClosePeriods(context.Background()); err != nil || n != 1 {
		t.Fatalf("closed %d periods (err %v), want September closed", n, err)
	}
}

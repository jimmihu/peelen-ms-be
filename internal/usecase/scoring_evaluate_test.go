package usecase

import (
	"context"
	"testing"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/scoring"
)

type evalRepoFake struct {
	scoringRepoFake
	counts map[string][]entity.ScopeCounts // by scope
	rules  []entity.ScoreRule
	sites  []string
}

func (f *evalRepoFake) AggregateFacts(_ context.Context, q entity.FactQuery) ([]entity.ScopeCounts, error) {
	return f.counts[q.Scope], nil
}
func (f *evalRepoFake) ListRules(context.Context, entity.RuleFilter) ([]entity.ScoreRule, error) {
	return f.rules, nil
}
func (f *evalRepoFake) VendorSites(context.Context, string) ([]string, error) { return f.sites, nil }
func (f *evalRepoFake) VendorIDsForSites(context.Context, []string) ([]string, error) {
	return []string{testVendor}, nil
}

func strPtr(s string) *string { return &s }

func TestEvaluateWorkerAtTwoSites(t *testing.T) {
	worker := "40000000-0000-4000-8000-000000000001"
	full := scoring.Counts{Tasks: 10, Completed: 10, OnTime: 10, Reported: 10}
	half := scoring.Counts{Tasks: 10, Completed: 5, OnTime: 5, Reported: 10}
	repo := &evalRepoFake{counts: map[string][]entity.ScopeCounts{
		entity.ScopeWorker: {
			{VendorID: testVendor, SiteID: strPtr(testSite), WorkerID: &worker, Counts: full},
			{VendorID: testVendor, SiteID: strPtr(otherSite), WorkerID: &worker, Counts: half},
		},
	}}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl)
	rule := entity.ScoreRule{ID: "r", Kind: "SLA", VendorID: testVendor, PeriodType: "MONTH",
		Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	results, err := u.evaluate(context.Background(), rule, entity.ScopeWorker, testVendor, nil, nil, start, start.AddDate(0, 1, -1))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || *results[0].SiteID == *results[1].SiteID {
		t.Fatalf("want one result per site, got %+v", results)
	}
	if !results[0].Compliant || results[1].Compliant {
		t.Fatalf("compliance per site wrong: %v, %v", results[0].Compliant, results[1].Compliant)
	}
}

func TestEvaluateVendorNoData(t *testing.T) {
	repo := &evalRepoFake{counts: map[string][]entity.ScopeCounts{}}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl)
	rule := entity.ScoreRule{ID: "r", Kind: "SLA", VendorID: testVendor, PeriodType: "MONTH",
		Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	results, err := u.evaluate(context.Background(), rule, entity.ScopeVendor, testVendor, nil, nil, start, start.AddDate(0, 1, -1))
	if err != nil || len(results) != 1 || results[0].Status != scoring.StatusNoData {
		t.Fatalf("results = %+v, err %v", results, err)
	}
}

func TestEvaluateMaturityUsesHistory(t *testing.T) {
	strong := scoring.Counts{Tasks: 10, Completed: 10, OnTime: 10, Reported: 10}
	repo := &evalRepoFake{counts: map[string][]entity.ScopeCounts{
		entity.ScopeVendor: {{VendorID: testVendor, Counts: strong}},
	}}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl)
	rule := entity.ScoreRule{ID: "r", Kind: "MATURITY", VendorID: testVendor, PeriodType: "MONTH",
		Items:  []entity.RuleItem{{Code: "ON_TIME_RATE", Weight: 100}},
		Levels: scoring.DefaultLevels}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	results, err := u.evaluate(context.Background(), rule, entity.ScopeVendor, testVendor, nil, nil, start, start.AddDate(0, 1, -1))
	if err != nil || len(results) != 1 || results[0].Level != 5 {
		t.Fatalf("three perfect periods should reach level 5: %+v, err %v", results, err)
	}
}

func TestListResultsVisibility(t *testing.T) {
	repo := &evalRepoFake{}
	u := NewScoringUsecase(repo)
	worker := &entity.User{ID: "40000000-0000-4000-8000-000000000001", Roles: []string{"WORKER"}}
	if _, _, err := u.ListResults(context.Background(), worker, entity.ResultFilter{Kind: "SLA", Scope: entity.ScopeVendor}); err != ErrForbidden {
		t.Fatalf("worker reading vendor results: %v", err)
	}
}

func (f *evalRepoFake) ListResults(_ context.Context, rf entity.ResultFilter) ([]entity.ScoreResult, int, error) {
	return nil, 0, nil
}

type ruleFilterSpy struct {
	evalRepoFake
	filter entity.RuleFilter
}

func (f *ruleFilterSpy) ListRules(_ context.Context, rf entity.RuleFilter) ([]entity.ScoreRule, error) {
	f.filter = rf
	return nil, nil
}

func TestLiveResultsLoadsOnlyVisibleVendorsRules(t *testing.T) {
	cases := []struct {
		name  string
		actor *entity.User
		scope string
		want  []string
	}{
		{"worker", &entity.User{ID: "w", Roles: []string{"WORKER"}, Vendors: []string{testVendor}}, entity.ScopeWorker, []string{testVendor}},
		{"site admin", &entity.User{ID: "a", Roles: []string{"ADMIN"}, Sites: []string{testSite}}, entity.ScopeSite, []string{testVendor}},
	}
	for _, c := range cases {
		repo := &ruleFilterSpy{}
		if _, err := NewScoringUsecase(repo).LiveResults(context.Background(), c.actor, "SLA", c.scope, ""); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if repo.filter.AllVendors || len(repo.filter.VendorIDs) != 1 || repo.filter.VendorIDs[0] != c.want[0] {
			t.Errorf("%s: rule filter = %+v, want only vendors %v", c.name, repo.filter, c.want)
		}
	}
}

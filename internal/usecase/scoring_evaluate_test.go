package usecase

import (
	"context"
	"errors"
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
	results, err := u.evaluate(context.Background(), rule, entity.FactQuery{Scope: entity.ScopeWorker, VendorID: testVendor, From: start, To: start.AddDate(0, 1, -1)})
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
	results, err := u.evaluate(context.Background(), rule, entity.FactQuery{Scope: entity.ScopeVendor, VendorID: testVendor, From: start, To: start.AddDate(0, 1, -1)})
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
	results, err := u.evaluate(context.Background(), rule, entity.FactQuery{Scope: entity.ScopeVendor, VendorID: testVendor, From: start, To: start.AddDate(0, 1, -1)})
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
		if _, err := NewScoringUsecase(repo).LiveResults(context.Background(), c.actor, entity.LiveFilter{Kind: "SLA", Scope: c.scope}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if repo.filter.AllVendors || len(repo.filter.VendorIDs) != 1 || repo.filter.VendorIDs[0] != c.want[0] {
			t.Errorf("%s: rule filter = %+v, want only vendors %v", c.name, repo.filter, c.want)
		}
	}
}

type liveQuerySpy struct {
	evalRepoFake
	queries []entity.FactQuery
}

func (f *liveQuerySpy) AggregateFacts(_ context.Context, q entity.FactQuery) ([]entity.ScopeCounts, error) {
	f.queries = append(f.queries, q)
	return nil, nil
}

func liveSLARule(site *string) entity.ScoreRule {
	return entity.ScoreRule{ID: "r", Kind: "SLA", VendorID: testVendor, SiteID: site, PeriodType: "MONTH",
		Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}}
}

func TestLiveResultsFiltersByWorker(t *testing.T) {
	worker := "40000000-0000-4000-8000-000000000001"
	repo := &liveQuerySpy{evalRepoFake: evalRepoFake{rules: []entity.ScoreRule{liveSLARule(nil)}, sites: []string{testSite}}}
	superAdmin := &entity.User{ID: "sa", Roles: []string{"SUPER_ADMIN"}}
	if _, err := NewScoringUsecase(repo).LiveResults(context.Background(), superAdmin,
		entity.LiveFilter{Kind: "SLA", Scope: entity.ScopeWorker, WorkerID: worker}); err != nil {
		t.Fatal(err)
	}
	if len(repo.queries) == 0 || repo.queries[0].WorkerID != worker {
		t.Fatalf("fact queries = %+v, want worker filter %s", repo.queries, worker)
	}
}

func TestLiveResultsFiltersBySite(t *testing.T) {
	site := otherSite
	repo := &liveQuerySpy{evalRepoFake: evalRepoFake{
		rules: []entity.ScoreRule{liveSLARule(nil), liveSLARule(&site)},
		sites: []string{testSite, otherSite},
	}}
	superAdmin := &entity.User{ID: "sa", Roles: []string{"SUPER_ADMIN"}}
	// testSite has no override: only the vendor default is evaluated, for that site only.
	if _, err := NewScoringUsecase(repo).LiveResults(context.Background(), superAdmin,
		entity.LiveFilter{Kind: "SLA", Scope: entity.ScopeSite, SiteID: testSite}); err != nil {
		t.Fatal(err)
	}
	if len(repo.queries) != 1 || len(repo.queries[0].SiteIDs) != 1 || repo.queries[0].SiteIDs[0] != testSite {
		t.Fatalf("fact queries = %+v, want one query for %s", repo.queries, testSite)
	}
	// otherSite has an override: only the override is evaluated.
	repo.queries = nil
	if _, err := NewScoringUsecase(repo).LiveResults(context.Background(), superAdmin,
		entity.LiveFilter{Kind: "SLA", Scope: entity.ScopeSite, SiteID: otherSite}); err != nil {
		t.Fatal(err)
	}
	if len(repo.queries) != 1 || repo.queries[0].SiteIDs[0] != otherSite {
		t.Fatalf("fact queries = %+v, want only the override for %s", repo.queries, otherSite)
	}
	// A site the vendor does not work at evaluates nothing.
	repo.queries = nil
	if _, err := NewScoringUsecase(repo).LiveResults(context.Background(), superAdmin,
		entity.LiveFilter{Kind: "SLA", Scope: entity.ScopeSite, SiteID: "20000000-0000-4000-8000-000000000009"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.queries) != 0 {
		t.Fatalf("unrelated site still evaluated: %+v", repo.queries)
	}
}

func TestLiveResultsRejectsFiltersOutsideScope(t *testing.T) {
	superAdmin := &entity.User{ID: "sa", Roles: []string{"SUPER_ADMIN"}}
	cases := []entity.LiveFilter{
		{Kind: "SLA", Scope: entity.ScopeVendor, WorkerID: "40000000-0000-4000-8000-000000000001"},
		{Kind: "SLA", Scope: entity.ScopeVendor, SiteID: testSite},
		{Kind: "SLA", Scope: entity.ScopeSite, WorkerID: "40000000-0000-4000-8000-000000000001"},
		{Kind: "SLA", Scope: entity.ScopeWorker, WorkerID: "not-a-uuid"},
	}
	for _, f := range cases {
		if _, err := NewScoringUsecase(&liveQuerySpy{}).LiveResults(context.Background(), superAdmin, f); !errors.Is(err, ErrInvalidOperationalData) {
			t.Errorf("%+v: err = %v, want invalid data", f, err)
		}
	}
}

type namedResultsFake struct {
	evalRepoFake
	history []entity.ScoreResult
	lookups int
}

func (f *namedResultsFake) ListResults(context.Context, entity.ResultFilter) ([]entity.ScoreResult, int, error) {
	return f.history, len(f.history), nil
}

func (f *namedResultsFake) LookupNames(_ context.Context, vendorIDs, siteIDs, userIDs []string) (entity.NameLookup, error) {
	f.lookups++
	return entity.NameLookup{
		Vendors: map[string]string{testVendor: "PT Mitra Teknik Abadi"},
		Sites:   map[string]string{testSite: "PLTU Paiton"},
		Users:   map[string]string{"40000000-0000-4000-8000-000000000001": "Andi Saputra"},
	}, nil
}

func TestResultsCarryVendorSiteAndWorkerNames(t *testing.T) {
	worker := "40000000-0000-4000-8000-000000000001"
	repo := &namedResultsFake{history: []entity.ScoreResult{
		{Kind: "SLA", ScopeType: entity.ScopeWorker, VendorID: testVendor, SiteID: strPtr(testSite), WorkerID: &worker},
	}}
	superAdmin := &entity.User{ID: "sa", Roles: []string{"SUPER_ADMIN"}}
	results, _, err := NewScoringUsecase(repo).ListResults(context.Background(), superAdmin,
		entity.ResultFilter{Kind: "SLA", Scope: entity.ScopeWorker})
	if err != nil {
		t.Fatal(err)
	}
	r := results[0]
	if r.VendorName != "PT Mitra Teknik Abadi" || r.SiteName == nil || *r.SiteName != "PLTU Paiton" ||
		r.WorkerName == nil || *r.WorkerName != "Andi Saputra" {
		t.Fatalf("names not attached: %+v", r)
	}

	// Live results get names too, looked up once for the whole response.
	live := &namedResultsFake{evalRepoFake: evalRepoFake{
		rules:  []entity.ScoreRule{liveSLARule(nil)},
		counts: map[string][]entity.ScopeCounts{entity.ScopeVendor: {{VendorID: testVendor, Counts: scoring.Counts{Tasks: 1, Completed: 1, Reported: 1}}}},
	}}
	liveResults, err := NewScoringUsecase(live).LiveResults(context.Background(), superAdmin, entity.LiveFilter{Kind: "SLA", Scope: entity.ScopeVendor})
	if err != nil {
		t.Fatal(err)
	}
	if len(liveResults) != 1 || liveResults[0].VendorName != "PT Mitra Teknik Abadi" || live.lookups != 1 {
		t.Fatalf("live names: %+v (lookups %d)", liveResults, live.lookups)
	}
}

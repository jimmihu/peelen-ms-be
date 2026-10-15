package usecase

import (
	"context"
	"errors"
	"testing"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
	"peelen-ms-be/internal/scoring"
)

const (
	testVendor = "10000000-0000-4000-8000-000000000001"
	testSite   = "20000000-0000-4000-8000-000000000001"
	otherSite  = "20000000-0000-4000-8000-000000000002"
)

type scoringRepoFake struct {
	repository.ScoringRepository
	created *entity.ScoreRuleInput
	rule    *entity.ScoreRule
	linked  bool
}

func (f *scoringRepoFake) CreateRuleVersion(_ context.Context, in entity.ScoreRuleInput, _ string) (*entity.ScoreRule, error) {
	f.created = &in
	return &entity.ScoreRule{ID: "rule", Kind: in.Kind, VendorID: in.VendorID, SiteID: in.SiteID}, nil
}
func (f *scoringRepoFake) VendorSiteLinked(context.Context, string, string) (bool, error) {
	return f.linked, nil
}
func (f *scoringRepoFake) GetRule(context.Context, string) (*entity.ScoreRule, error) {
	if f.rule == nil {
		return nil, repository.ErrNotFound
	}
	return f.rule, nil
}
func (f *scoringRepoFake) DeactivateRule(context.Context, string) error { return nil }
func (f *scoringRepoFake) LookupNames(context.Context, []string, []string, []string) (entity.NameLookup, error) {
	return entity.NameLookup{}, nil
}

func target(v float64) *float64 { return &v }

func slaInput(site *string, items ...entity.RuleItem) entity.ScoreRuleInput {
	return entity.ScoreRuleInput{Kind: "SLA", VendorID: testVendor, SiteID: site, PeriodType: "MONTH", Items: items}
}

func TestValidateRuleInput(t *testing.T) {
	ok := slaInput(nil,
		entity.RuleItem{Code: "COMPLETION_RATE", Weight: 33.33, Target: target(95)},
		entity.RuleItem{Code: "ON_TIME_RATE", Weight: 33.33, Target: target(90)},
		entity.RuleItem{Code: "EVIDENCE_COMPLETE_RATE", Weight: 33.34, Target: target(98)},
	)
	if err := validateRuleInput(&ok); err != nil {
		t.Fatalf("33.33+33.33+33.34 rejected: %v", err)
	}
	bad := map[string]entity.ScoreRuleInput{
		"weights not 100":   slaInput(nil, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 90, Target: target(95)}),
		"unknown code":      slaInput(nil, entity.RuleItem{Code: "SPEED", Weight: 100, Target: target(1)}),
		"duplicate code":    slaInput(nil, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 50, Target: target(95)}, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 50, Target: target(95)}),
		"missing target":    slaInput(nil, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 100}),
		"percent over 100":  slaInput(nil, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 100, Target: target(101)}),
		"negative minutes":  slaInput(nil, entity.RuleItem{Code: "AVG_LATE_MINUTES", Weight: 100, Target: target(-1)}),
		"maturity-only SLA": slaInput(nil, entity.RuleItem{Code: "CONSISTENCY", Weight: 100, Target: target(50)}),
		"bad period":        {Kind: "SLA", VendorID: testVendor, PeriodType: "DAY", Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}},
		"bad kind":          {Kind: "KPI", VendorID: testVendor, PeriodType: "MONTH", Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}},
	}
	for name, in := range bad {
		in := in
		if err := validateRuleInput(&in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	maturity := entity.ScoreRuleInput{Kind: "MATURITY", VendorID: testVendor, PeriodType: "QUARTER",
		Items: []entity.RuleItem{{Code: "CONSISTENCY", Weight: 100}}}
	if err := validateRuleInput(&maturity); err != nil {
		t.Fatal(err)
	}
	if len(maturity.Levels) != 4 {
		t.Fatalf("default levels not applied: %+v", maturity.Levels)
	}
}

func TestCreateRuleAccess(t *testing.T) {
	item := entity.RuleItem{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}
	site := testSite
	other := otherSite
	superAdmin := &entity.User{ID: "sa", Roles: []string{"SUPER_ADMIN"}}
	admin := &entity.User{ID: "admin", Roles: []string{"ADMIN"}, Sites: []string{testSite}}
	cases := []struct {
		name   string
		actor  *entity.User
		input  entity.ScoreRuleInput
		linked bool
		want   error
	}{
		{"super admin default", superAdmin, slaInput(nil, item), true, nil},
		{"admin default forbidden", admin, slaInput(nil, item), true, ErrForbidden},
		{"admin own site override", admin, slaInput(&site, item), true, nil},
		{"admin other site forbidden", admin, slaInput(&other, item), true, ErrForbidden},
		{"site not linked to vendor", superAdmin, slaInput(&site, item), false, ErrInvalidOperationalData},
		{"worker forbidden", &entity.User{Roles: []string{"WORKER"}}, slaInput(nil, item), true, ErrForbidden},
	}
	for _, c := range cases {
		repo := &scoringRepoFake{linked: c.linked}
		_, err := NewScoringUsecase(repo).CreateRule(context.Background(), c.actor, c.input)
		if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestDeleteRuleAccess(t *testing.T) {
	site := testSite
	admin := &entity.User{ID: "admin", Roles: []string{"ADMIN"}, Sites: []string{testSite}}
	repo := &scoringRepoFake{rule: &entity.ScoreRule{ID: "r", VendorID: testVendor}}
	if err := NewScoringUsecase(repo).DeleteRule(context.Background(), admin, "30000000-0000-4000-8000-000000000001"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin deleting vendor default: %v", err)
	}
	repo.rule.SiteID = &site
	if err := NewScoringUsecase(repo).DeleteRule(context.Background(), admin, "30000000-0000-4000-8000-000000000001"); err != nil {
		t.Fatalf("admin deleting own override: %v", err)
	}
}

var _ = scoring.DefaultLevels // keeps the scoring import used by later tests in this package

package usecase

import (
	"context"
	"fmt"
	"math"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
	"peelen-ms-be/internal/scoring"
)

type ScoringUsecase interface {
	Catalog() []scoring.Indicator
	CreateRule(ctx context.Context, actor *entity.User, input entity.ScoreRuleInput) (*entity.ScoreRule, error)
	GetRule(ctx context.Context, actor *entity.User, id string) (*entity.ScoreRule, error)
	ListRules(ctx context.Context, actor *entity.User, f entity.RuleFilter) ([]entity.ScoreRule, error)
	DeleteRule(ctx context.Context, actor *entity.User, id string) error
}

type ScoringUsecaseImpl struct {
	repository repository.ScoringRepository
}

func NewScoringUsecase(repo repository.ScoringRepository) ScoringUsecase {
	return &ScoringUsecaseImpl{repository: repo}
}

func (u *ScoringUsecaseImpl) Catalog() []scoring.Indicator { return scoring.Catalog }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidOperationalData}, args...)...)
}

// validateRuleInput checks the rule shape and fills default maturity levels.
func validateRuleInput(in *entity.ScoreRuleInput) error {
	if in.Kind != entity.RuleKindSLA && in.Kind != entity.RuleKindMaturity {
		return invalid("kind must be SLA or MATURITY")
	}
	if !scopeUUIDPattern.MatchString(in.VendorID) || in.SiteID != nil && !scopeUUIDPattern.MatchString(*in.SiteID) {
		return invalid("vendor_id and site_id must be UUIDs")
	}
	if !scoring.ValidPeriodType(in.PeriodType) {
		return invalid("period_type must be WEEK, MONTH, or QUARTER")
	}
	if len(in.Items) == 0 {
		return invalid("at least one item is required")
	}
	seen := map[string]bool{}
	var total float64
	for i, item := range in.Items {
		indicator, ok := scoring.Lookup(item.Code)
		if !ok || seen[item.Code] {
			return invalid("unknown or duplicate code %q", item.Code)
		}
		seen[item.Code] = true
		if in.Kind == entity.RuleKindSLA && !indicator.SLA || in.Kind == entity.RuleKindMaturity && !indicator.Maturity {
			return invalid("%s cannot be used in a %s rule", item.Code, in.Kind)
		}
		if item.Weight <= 0 || item.Weight > 100 {
			return invalid("weight of %s must be between 0 and 100", item.Code)
		}
		total += item.Weight
		if in.Kind == entity.RuleKindSLA {
			if item.Target == nil || *item.Target < 0 || indicator.Unit == scoring.UnitPercent && *item.Target > 100 {
				return invalid("target of %s is required and must fit its unit", item.Code)
			}
		} else {
			in.Items[i].Target = nil // maturity dimensions have no target
		}
	}
	if math.Abs(total-100) > 0.01 {
		return invalid("weights must total 100, got %.2f", total)
	}
	if in.Kind == entity.RuleKindSLA {
		in.Levels = nil
		return nil
	}
	if len(in.Levels) == 0 {
		in.Levels = append([]scoring.LevelRule(nil), scoring.DefaultLevels...)
	}
	if err := scoring.ValidateLevels(in.Levels); err != nil {
		return invalid("%v", err)
	}
	return nil
}

// canWriteRule: SUPER_ADMIN writes anything; ADMIN only overrides for its own sites.
func canWriteRule(actor *entity.User, siteID *string) bool {
	if actor == nil {
		return false
	}
	if hasRole(actor, "SUPER_ADMIN") {
		return true
	}
	return siteID != nil && hasRole(actor, "ADMIN") && containsString(actor.Sites, *siteID)
}

func (u *ScoringUsecaseImpl) CreateRule(ctx context.Context, actor *entity.User, input entity.ScoreRuleInput) (*entity.ScoreRule, error) {
	if !canWriteRule(actor, input.SiteID) {
		return nil, ErrForbidden
	}
	if err := validateRuleInput(&input); err != nil {
		return nil, err
	}
	if input.SiteID != nil {
		linked, err := u.repository.VendorSiteLinked(ctx, input.VendorID, *input.SiteID)
		if err != nil {
			return nil, err
		}
		if !linked {
			return nil, invalid("site is not linked to the vendor")
		}
	}
	return u.repository.CreateRuleVersion(ctx, input, actor.ID)
}

func (u *ScoringUsecaseImpl) DeleteRule(ctx context.Context, actor *entity.User, id string) error {
	if !scopeUUIDPattern.MatchString(id) {
		return ErrNotFound
	}
	rule, err := u.repository.GetRule(ctx, id)
	if err != nil {
		return err
	}
	if !canWriteRule(actor, rule.SiteID) {
		return ErrForbidden
	}
	return u.repository.DeactivateRule(ctx, id)
}

// Rule reads: SUPER_ADMIN all; ADMIN rules of vendors linked to its sites.
func (u *ScoringUsecaseImpl) GetRule(ctx context.Context, actor *entity.User, id string) (*entity.ScoreRule, error) {
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	if actor == nil || !hasRole(actor, "SUPER_ADMIN", "ADMIN") {
		return nil, ErrForbidden
	}
	rule, err := u.repository.GetRule(ctx, id)
	if err != nil {
		return nil, err
	}
	if !hasRole(actor, "SUPER_ADMIN") {
		visible, err := u.adminVendorIDs(ctx, actor)
		if err != nil {
			return nil, err
		}
		if !containsString(visible, rule.VendorID) {
			return nil, ErrNotFound
		}
	}
	return rule, nil
}

func (u *ScoringUsecaseImpl) ListRules(ctx context.Context, actor *entity.User, f entity.RuleFilter) ([]entity.ScoreRule, error) {
	if actor == nil || !hasRole(actor, "SUPER_ADMIN", "ADMIN") {
		return nil, ErrForbidden
	}
	f.AllVendors = hasRole(actor, "SUPER_ADMIN")
	if !f.AllVendors {
		visible, err := u.adminVendorIDs(ctx, actor)
		if err != nil {
			return nil, err
		}
		f.VendorIDs = visible
	}
	return u.repository.ListRules(ctx, f)
}

func (u *ScoringUsecaseImpl) adminVendorIDs(ctx context.Context, actor *entity.User) ([]string, error) {
	return u.repository.VendorIDsForSites(ctx, actor.Sites)
}

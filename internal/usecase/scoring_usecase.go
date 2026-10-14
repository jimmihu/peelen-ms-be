package usecase

import (
	"context"
	"fmt"
	"math"
	"time"

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
	LiveResults(ctx context.Context, actor *entity.User, kind, scope, vendorID string) ([]entity.ScoreResult, error)
	ListResults(ctx context.Context, actor *entity.User, f entity.ResultFilter) ([]entity.ScoreResult, int, error)
	FinalizeFacts(ctx context.Context) (int64, error)
	ClosePeriods(ctx context.Context) (int, error)
	RunJobCycle(ctx context.Context) error
}

type ScoringUsecaseImpl struct {
	repository repository.ScoringRepository
	location   *time.Location
	now        func() time.Time
}

func NewScoringUsecase(repo repository.ScoringRepository) ScoringUsecase {
	return &ScoringUsecaseImpl{repository: repo, location: time.UTC, now: time.Now}
}

// WithLocation sets the plant time zone used for period boundaries.
func (u *ScoringUsecaseImpl) WithLocation(location *time.Location) *ScoringUsecaseImpl {
	if location != nil {
		u.location = location
	}
	return u
}

func (u *ScoringUsecaseImpl) Clock() time.Time { return u.now().In(u.location) }

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

func ruleItems(rule entity.ScoreRule) []scoring.Item {
	items := make([]scoring.Item, 0, len(rule.Items))
	for _, item := range rule.Items {
		si := scoring.Item{Code: item.Code, Weight: item.Weight}
		if item.Target != nil {
			si.Target = *item.Target
		}
		items = append(items, si)
	}
	return items
}

func groupKey(c entity.ScopeCounts) string {
	key := c.VendorID
	if c.SiteID != nil {
		key += "|" + *c.SiteID
	}
	if c.WorkerID != nil {
		key += "|" + *c.WorkerID
	}
	return key
}

// evaluate scores one rule for one scope over [start, end]. Maturity rules also
// score the previous periods needed by the level gates.
func (u *ScoringUsecaseImpl) evaluate(ctx context.Context, rule entity.ScoreRule, scope, vendorID string,
	siteIDs, excludeSiteIDs []string, start, end time.Time) ([]entity.ScoreResult, error) {
	query := entity.FactQuery{Scope: scope, VendorID: vendorID, SiteIDs: siteIDs, ExcludeSiteIDs: excludeSiteIDs, From: start, To: end}
	current, err := u.repository.AggregateFacts(ctx, query)
	if err != nil {
		return nil, err
	}
	items := ruleItems(rule)
	base := func(c entity.ScopeCounts) entity.ScoreResult {
		return entity.ScoreResult{Kind: rule.Kind, RuleID: rule.ID, ScopeType: scope, VendorID: c.VendorID,
			SiteID: c.SiteID, WorkerID: c.WorkerID, PeriodType: rule.PeriodType, PeriodStart: start, PeriodEnd: end}
	}
	// Groups without facts: a vendor with no tasks, or a vendor site with no tasks.
	if scope == entity.ScopeVendor && len(current) == 0 {
		current = []entity.ScopeCounts{{VendorID: vendorID}}
	}
	if scope == entity.ScopeSite {
		sites := siteIDs
		if len(sites) == 0 {
			if sites, err = u.repository.VendorSites(ctx, vendorID); err != nil {
				return nil, err
			}
		}
		present := map[string]bool{}
		for _, c := range current {
			present[*c.SiteID] = true
		}
		for _, site := range sites {
			if !present[site] && !containsString(excludeSiteIDs, site) {
				site := site
				current = append(current, entity.ScopeCounts{VendorID: vendorID, SiteID: &site})
			}
		}
	}

	results := make([]entity.ScoreResult, 0, len(current))
	if rule.Kind == entity.RuleKindSLA {
		for _, c := range current {
			res := base(c)
			res.Result = scoring.EvaluateSLA(c.Counts, items)
			results = append(results, res)
		}
		return results, nil
	}

	// Maturity: history[group] holds oldest..newest period results.
	periods := scoring.MaxConsecutive(rule.Levels)
	history := map[string][]scoring.Result{}
	pt := scoring.PeriodType(rule.PeriodType)
	prevStart := start
	for i := 1; i < periods; i++ {
		prevStart = scoring.PreviousStart(pt, prevStart)
		_, prevEnd := scoring.PeriodBounds(pt, prevStart)
		past, err := u.repository.AggregateFacts(ctx, entity.FactQuery{Scope: scope, VendorID: vendorID,
			SiteIDs: siteIDs, ExcludeSiteIDs: excludeSiteIDs, From: prevStart, To: prevEnd})
		if err != nil {
			return nil, err
		}
		byKey := map[string]scoring.Counts{}
		for _, c := range past {
			byKey[groupKey(c)] = c.Counts
		}
		for _, c := range current {
			key := groupKey(c)
			history[key] = append([]scoring.Result{scoring.EvaluateMaturityPeriod(byKey[key], items)}, history[key]...)
		}
	}
	for _, c := range current {
		res := base(c)
		res.Result = scoring.EvaluateMaturityPeriod(c.Counts, items)
		res.Level = scoring.Level(append(history[groupKey(c)], res.Result), rule.Levels)
		results = append(results, res)
	}
	return results, nil
}

// visibility resolves what the actor may read.
func (u *ScoringUsecaseImpl) visibility(ctx context.Context, actor *entity.User, scope string) (entity.ResultFilter, error) {
	var f entity.ResultFilter
	switch {
	case actor == nil:
		return f, ErrForbidden
	case hasRole(actor, "SUPER_ADMIN"):
		f.AllVendors = true
	case hasRole(actor, "ADMIN"):
		vendors, err := u.repository.VendorIDsForSites(ctx, actor.Sites)
		if err != nil {
			return f, err
		}
		if scope == entity.ScopeVendor {
			f.VendorIDs = vendors
		} else {
			f.SiteIDs = actor.Sites
		}
	case hasRole(actor, "ADMIN_WORKER"):
		f.VendorIDs = actor.Vendors
	case hasRole(actor, "WORKER"):
		if scope != entity.ScopeWorker {
			return f, ErrForbidden
		}
		f.OnlyWorkerID = actor.ID
		f.AllVendors = true
	default:
		return f, ErrForbidden
	}
	return f, nil
}

func validScope(scope string) bool {
	return scope == entity.ScopeVendor || scope == entity.ScopeSite || scope == entity.ScopeWorker
}

func (u *ScoringUsecaseImpl) ListResults(ctx context.Context, actor *entity.User, f entity.ResultFilter) ([]entity.ScoreResult, int, error) {
	if f.Kind != entity.RuleKindSLA && f.Kind != entity.RuleKindMaturity || !validScope(f.Scope) {
		return nil, 0, ErrInvalidOperationalData
	}
	vis, err := u.visibility(ctx, actor, f.Scope)
	if err != nil {
		return nil, 0, err
	}
	f.AllVendors, f.VendorIDs, f.SiteIDs, f.OnlyWorkerID = vis.AllVendors, vis.VendorIDs, vis.SiteIDs, vis.OnlyWorkerID
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	if f.To.IsZero() {
		f.To = u.Clock().AddDate(0, 0, 1)
	}
	if f.From.IsZero() {
		f.From = f.To.AddDate(-1, 0, 0)
	}
	return u.repository.ListResults(ctx, f)
}

// LiveResults scores the running period of every active rule the actor can see.
func (u *ScoringUsecaseImpl) LiveResults(ctx context.Context, actor *entity.User, kind, scope, vendorID string) ([]entity.ScoreResult, error) {
	if kind != entity.RuleKindSLA && kind != entity.RuleKindMaturity || !validScope(scope) {
		return nil, ErrInvalidOperationalData
	}
	vis, err := u.visibility(ctx, actor, scope)
	if err != nil {
		return nil, err
	}
	rules, err := u.repository.ListRules(ctx, entity.RuleFilter{Kind: kind, VendorID: vendorID, ActiveOnly: true,
		AllVendors: vis.AllVendors || len(vis.SiteIDs) > 0, VendorIDs: vis.VendorIDs})
	if err != nil {
		return nil, err
	}
	today := u.Clock()
	var results []entity.ScoreResult
	defaults := map[string]entity.ScoreRule{}
	overrides := map[string][]entity.ScoreRule{}
	for _, rule := range rules {
		if rule.SiteID == nil {
			defaults[rule.VendorID] = rule
		} else {
			overrides[rule.VendorID] = append(overrides[rule.VendorID], rule)
		}
	}
	for vendor, rule := range defaults {
		start, end := scoring.PeriodBounds(scoring.PeriodType(rule.PeriodType), today)
		var exclude []string
		for _, o := range overrides[vendor] {
			exclude = append(exclude, *o.SiteID)
		}
		var res []entity.ScoreResult
		if scope == entity.ScopeVendor {
			res, err = u.evaluate(ctx, rule, scope, vendor, nil, nil, start, end)
		} else {
			res, err = u.evaluate(ctx, rule, scope, vendor, nil, exclude, start, end)
		}
		if err != nil {
			return nil, err
		}
		results = append(results, res...)
	}
	if scope != entity.ScopeVendor {
		for vendor, list := range overrides {
			for _, rule := range list {
				start, end := scoring.PeriodBounds(scoring.PeriodType(rule.PeriodType), today)
				res, err := u.evaluate(ctx, rule, scope, vendor, []string{*rule.SiteID}, nil, start, end)
				if err != nil {
					return nil, err
				}
				results = append(results, res...)
			}
		}
	}
	// Vendors in view without any rule.
	if scope == entity.ScopeVendor && vendorID != "" {
		if _, ok := defaults[vendorID]; !ok {
			results = append(results, entity.ScoreResult{Kind: kind, ScopeType: scope, VendorID: vendorID,
				Result: scoring.Result{Status: entity.ResultStatusNoRule}})
		}
	}
	filtered := results[:0]
	for _, r := range results {
		r.Live = true
		if visibleResult(vis, r) {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func visibleResult(vis entity.ResultFilter, r entity.ScoreResult) bool {
	if vis.OnlyWorkerID != "" {
		return r.WorkerID != nil && *r.WorkerID == vis.OnlyWorkerID
	}
	if vis.AllVendors {
		return true
	}
	if containsString(vis.VendorIDs, r.VendorID) {
		return true
	}
	return r.SiteID != nil && containsString(vis.SiteIDs, *r.SiteID)
}

func (u *ScoringUsecaseImpl) FinalizeFacts(ctx context.Context) (int64, error) {
	return u.repository.FinalizeFacts(ctx)
}

// activeAt reports whether a rule version is in force at instant at.
func activeAt(rule entity.ScoreRule, at time.Time) bool {
	return !rule.CreatedAt.After(at) && (rule.SupersededAt == nil || rule.SupersededAt.After(at))
}

// ClosePeriods freezes every finished, fully final period of every rule key,
// scored with the version active when the period ended.
func (u *ScoringUsecaseImpl) ClosePeriods(ctx context.Context) (int, error) {
	lastFinal, err := u.repository.LastFinalDate(ctx)
	if err != nil {
		return 0, err
	}
	// The database returns a date at UTC midnight; compare it as a plant-local date.
	lastFinal = time.Date(lastFinal.Year(), lastFinal.Month(), lastFinal.Day(), 0, 0, 0, 0, u.location)
	keys, err := u.repository.RuleKeys(ctx)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, key := range keys {
		versions, err := u.repository.RuleVersions(ctx, key)
		if err != nil {
			return closed, err
		}
		for _, version := range versions {
			pt := scoring.PeriodType(version.PeriodType)
			start, end := scoring.PeriodBounds(pt, version.CreatedAt.In(u.location))
			for !end.After(lastFinal) {
				periodEndAt := end.AddDate(0, 0, 1) // 00:00 after the last day
				if activeAt(version, periodEndAt) {
					done, err := u.closePeriod(ctx, version, start, end, periodEndAt)
					if err != nil {
						return closed, err
					}
					if done {
						closed++
					}
				}
				start = end.AddDate(0, 0, 1)
				_, end = scoring.PeriodBounds(pt, start)
			}
		}
	}
	return closed, nil
}

// closePeriod freezes one rule version for one period; false if already frozen.
func (u *ScoringUsecaseImpl) closePeriod(ctx context.Context, rule entity.ScoreRule, start, end, endAt time.Time) (bool, error) {
	anchor := entity.ScopeVendor
	if rule.SiteID != nil {
		anchor = entity.ScopeSite
	}
	exists, err := u.repository.ResultExists(ctx, rule.Kind, anchor, rule.VendorID, rule.SiteID, rule.PeriodType, start)
	if err != nil || exists {
		return false, err
	}
	var batches [][]entity.ScoreResult
	if rule.SiteID == nil {
		overrides, err := u.repository.OverrideSitesAt(ctx, rule.Kind, rule.VendorID, endAt)
		if err != nil {
			return false, err
		}
		for _, scope := range []string{entity.ScopeVendor, entity.ScopeSite, entity.ScopeWorker} {
			var exclude []string
			if scope != entity.ScopeVendor {
				exclude = overrides
			}
			res, err := u.evaluate(ctx, rule, scope, rule.VendorID, nil, exclude, start, end)
			if err != nil {
				return false, err
			}
			batches = append(batches, res)
		}
	} else {
		for _, scope := range []string{entity.ScopeSite, entity.ScopeWorker} {
			res, err := u.evaluate(ctx, rule, scope, rule.VendorID, []string{*rule.SiteID}, nil, start, end)
			if err != nil {
				return false, err
			}
			batches = append(batches, res)
		}
	}
	// Save the anchor scope last so a crash mid-way is retried on the next run.
	for i := len(batches) - 1; i >= 0; i-- {
		for _, res := range batches[i] {
			if err := u.repository.SaveResult(ctx, res); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

// RunJobCycle runs one finalize + close pass if this instance holds the job lock.
func (u *ScoringUsecaseImpl) RunJobCycle(ctx context.Context) error {
	_, err := u.repository.WithJobLock(ctx, func(ctx context.Context) error {
		if _, err := u.FinalizeFacts(ctx); err != nil {
			return fmt.Errorf("finalize facts: %w", err)
		}
		if _, err := u.ClosePeriods(ctx); err != nil {
			return fmt.Errorf("close periods: %w", err)
		}
		return nil
	})
	return err
}

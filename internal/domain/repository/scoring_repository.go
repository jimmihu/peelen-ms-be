package repository

import (
	"context"
	"time"

	"peelen-ms-be/internal/domain/entity"
)

type ScoringRepository interface {
	FinalizeFacts(ctx context.Context) (int64, error)
	LastFinalDate(ctx context.Context) (time.Time, error)
	AggregateFacts(ctx context.Context, q entity.FactQuery) ([]entity.ScopeCounts, error)
	CreateRuleVersion(ctx context.Context, input entity.ScoreRuleInput, createdBy string) (*entity.ScoreRule, error)
	GetRule(ctx context.Context, id string) (*entity.ScoreRule, error)
	ListRules(ctx context.Context, f entity.RuleFilter) ([]entity.ScoreRule, error)
	DeactivateRule(ctx context.Context, id string) error
	VendorSiteLinked(ctx context.Context, vendorID, siteID string) (bool, error)
	VendorIDsForSites(ctx context.Context, siteIDs []string) ([]string, error)
}

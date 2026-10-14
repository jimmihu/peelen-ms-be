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
}

package repository

import (
	"context"

	"peelen-ms-be/internal/domain/entity"
)

type DashboardRepository interface {
	GetDashboard(
		ctx context.Context,
		access entity.OperationalAccess,
		filter entity.DashboardFilter,
	) (*entity.DashboardResponse, error)
}

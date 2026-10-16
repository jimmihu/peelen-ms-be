
package repository

import (
	"context"

	"peelen-ms-be/internal/domain/entity"
)

type DashboardRepository interface {
	GetDashboardSummary(
		ctx context.Context,
		access entity.OperationalAccess,
		filter entity.DashboardFilter,
	) (*entity.DashboardSummaryResponse, error)

	GetDashboardDetail(
		ctx context.Context,
		access entity.OperationalAccess,
		filter entity.DashboardFilter,
		detailType string,
	) (*entity.DashboardDetailResponse, error)
}

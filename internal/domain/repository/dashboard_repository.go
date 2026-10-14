package repository

import (
	"context"

	"peelen-ms-be/internal/domain/entity"
)

type DashboardRepository interface {
	GetDashboard(
		context.Context,
		entity.OperationalAccess,
		entity.DashboardFilter,
	) (*entity.DashboardResponse, error)
}

package usecase

import (
	"context"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type DashboardUsecase interface {
	GetDashboard(
		context.Context,
		*entity.User,
		string,
		string,
		string,
	) (*entity.DashboardResponse, error)
}

type DashboardUsecaseImpl struct {
	repository repository.DashboardRepository
}

func NewDashboardUsecase(repo repository.DashboardRepository) DashboardUsecase {
	return &DashboardUsecaseImpl{
		repository: repo,
	}
}

func (u *DashboardUsecaseImpl) GetDashboard(
	ctx context.Context,
	actor *entity.User,
	siteID string,
	vendorID string,
	date string,
) (*entity.DashboardResponse, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}

	if !hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER", "WORKER") {
		return nil, ErrForbidden
	}

	if siteID != "" && !scopeUUIDPattern.MatchString(siteID) {
		return nil, ErrInvalidOperationalData
	}

	if vendorID != "" && !scopeUUIDPattern.MatchString(vendorID) {
		return nil, ErrInvalidOperationalData
	}

	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil || parsedDate.Format("2006-01-02") != date {
		return nil, ErrInvalidOperationalData
	}

	filter := entity.DashboardFilter{
		UserID:   actor.ID,
		IsWorker: hasRole(actor, "WORKER"),
		SiteID:   siteID,
		VendorID: vendorID,
		Date:     date,
	}

	access := operationalAccess(actor)

	return u.repository.GetDashboard(ctx, access, filter)
}

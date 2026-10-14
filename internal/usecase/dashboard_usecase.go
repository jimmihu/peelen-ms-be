package usecase

import (
	"context"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type DashboardUsecase interface {
	GetDashboard(context.Context, *entity.User, string, string, string) (*entity.DashboardResponse, error)
}

type DashboardUsecaseImpl struct {
	repository repository.DashboardRepository
}

func NewDashboardUsecase(repo repository.DashboardRepository) DashboardUsecase {
	return &DashboardUsecaseImpl{repository: repo}
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

	if siteID != "" && !scopeUUIDPattern.MatchString(siteID) {
		return nil, ErrInvalidOperationalData
	}

	if vendorID != "" && !scopeUUIDPattern.MatchString(vendorID) {
		return nil, ErrInvalidOperationalData
	}

	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, ErrInvalidOperationalData
	}

	if !hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER") {
		return nil, ErrForbidden
	}

	filter := entity.DashboardFilter{
		SiteID:   siteID,
		VendorID: vendorID,
		Date:     date,
	}

	return u.repository.GetDashboard(ctx, operationalAccess(actor), filter)
}

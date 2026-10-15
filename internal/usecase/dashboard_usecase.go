package usecase

import (
	"context"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type DashboardUsecase interface {
	GetDashboard(
		ctx context.Context,
		actor *entity.User,
		siteID string,
		vendorID string,
		date string,
	) (*entity.DashboardSummaryResponse, error)

	GetDashboardDetail(
		ctx context.Context,
		actor *entity.User,
		siteID string,
		vendorID string,
		date string,
		detailType string,
	) (*entity.DashboardDetailResponse, error)
}

type DashboardUsecaseImpl struct {
	repository repository.DashboardRepository
}

func NewDashboardUsecase(
	repo repository.DashboardRepository,
) DashboardUsecase {
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
) (*entity.DashboardSummaryResponse, error) {
	if err := validateDashboardFilter(actor, siteID, vendorID, date); err != nil {
		return nil, err
	}

	filter := buildDashboardFilter(actor, siteID, vendorID, date)

	return u.repository.GetDashboardSummary(
		ctx,
		operationalAccess(actor),
		filter,
	)
}

func (u *DashboardUsecaseImpl) GetDashboardDetail(
	ctx context.Context,
	actor *entity.User,
	siteID string,
	vendorID string,
	date string,
	detailType string,
) (*entity.DashboardDetailResponse, error) {
	switch detailType {
	case "vendor_progress", "inventory_reports", "safety_reports":
	default:
		return nil, ErrInvalidOperationalData
	}

	if err := validateDashboardFilter(actor, siteID, vendorID, date); err != nil {
		return nil, err
	}

	filter := buildDashboardFilter(actor, siteID, vendorID, date)

	return u.repository.GetDashboardDetail(
		ctx,
		operationalAccess(actor),
		filter,
		detailType,
	)
}

func buildDashboardFilter(
	actor *entity.User,
	siteID string,
	vendorID string,
	date string,
) entity.DashboardFilter {
	return entity.DashboardFilter{
		UserID: actor.ID,
		IsWorker: hasRole(actor, "WORKER") &&
			!hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER"),
		SiteID:   siteID,
		VendorID: vendorID,
		Date:     date,
	}
}

func validateDashboardFilter(
	actor *entity.User,
	siteID string,
	vendorID string,
	date string,
) error {
	if err := validateWorkflowActor(actor); err != nil {
		return err
	}

	if !hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER", "WORKER") {
		return ErrForbidden
	}

	if siteID != "" && !scopeUUIDPattern.MatchString(siteID) {
		return ErrInvalidOperationalData
	}

	if vendorID != "" && !scopeUUIDPattern.MatchString(vendorID) {
		return ErrInvalidOperationalData
	}

	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil || parsedDate.Format("2006-01-02") != date {
		return ErrInvalidOperationalData
	}

	return nil
}

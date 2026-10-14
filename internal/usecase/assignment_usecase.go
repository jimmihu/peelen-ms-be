package usecase

import (
	"context"
	"strings"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type AssignmentUsecase interface {
	ListAssignments(context.Context, *entity.User, entity.AssignmentFilter) ([]entity.Assignment, int, error)
	GetAssignment(context.Context, *entity.User, string) (*entity.Assignment, error)
	CreateAssignment(context.Context, *entity.User, entity.AssignmentInput) (*entity.Assignment, error)
	UpdateAssignment(context.Context, *entity.User, string, entity.AssignmentInput) (*entity.Assignment, error)
	DeleteAssignment(context.Context, *entity.User, string) error
}

type AssignmentUsecaseImpl struct {
	repository repository.AssignmentRepository
}

func NewAssignmentUsecase(repo repository.AssignmentRepository) AssignmentUsecase {
	return &AssignmentUsecaseImpl{repository: repo}
}

func (u *AssignmentUsecaseImpl) ListAssignments(ctx context.Context, actor *entity.User, filter entity.AssignmentFilter) ([]entity.Assignment, int, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, 0, err
	}
	if err := validateAssignmentFilter(&filter); err != nil {
		return nil, 0, err
	}
	if !hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER") {
		filter.UserID = actor.ID
	}
	return u.repository.ListAssignments(ctx, operationalAccess(actor), filter)
}

func (u *AssignmentUsecaseImpl) GetAssignment(ctx context.Context, actor *entity.User, id string) (*entity.Assignment, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	return u.repository.GetAssignment(ctx, operationalAccess(actor), id)
}

func (u *AssignmentUsecaseImpl) CreateAssignment(ctx context.Context, actor *entity.User, input entity.AssignmentInput) (*entity.Assignment, error) {
	if actor == nil || !canManageAssignments(actor) {
		return nil, ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(input.PipelineItemID) || !scopeUUIDPattern.MatchString(input.UserID) ||
		!validShift(input.Shift) {
		return nil, ErrInvalidOperationalData
	}
	input.Shift = strings.ToLower(strings.TrimSpace(input.Shift))
	return u.repository.CreateAssignment(ctx, operationalAccess(actor), input)
}

// UpdateAssignment only toggles is_active: pipeline reports reference the
// (pipeline item, user, shift) key, so changing it means a new assignment.
func (u *AssignmentUsecaseImpl) UpdateAssignment(ctx context.Context, actor *entity.User, id string, input entity.AssignmentInput) (*entity.Assignment, error) {
	if actor == nil || !canManageAssignments(actor) {
		return nil, ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	if input.IsActive == nil {
		return nil, ErrInvalidOperationalData
	}
	return u.repository.SetAssignmentActive(ctx, operationalAccess(actor), id, *input.IsActive)
}

func (u *AssignmentUsecaseImpl) DeleteAssignment(ctx context.Context, actor *entity.User, id string) error {
	if actor == nil || !canManageAssignments(actor) {
		return ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) {
		return ErrNotFound
	}
	_, err := u.repository.SetAssignmentActive(ctx, operationalAccess(actor), id, false)
	return err
}

// canManageAssignments: only Admin Worker assigns Workers of their own vendor;
// Super Admin and Admin read assignments for monitoring.
func canManageAssignments(actor *entity.User) bool {
	return hasRole(actor, "ADMIN_WORKER")
}

func validateAssignmentFilter(filter *entity.AssignmentFilter) error {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return ErrInvalidOperationalData
	}
	for _, id := range []string{filter.VendorID, filter.SiteID, filter.PipelineID, filter.PipelineItemID, filter.UserID} {
		if id != "" && !scopeUUIDPattern.MatchString(id) {
			return ErrInvalidOperationalData
		}
	}
	if filter.Shift != "" {
		if !validShift(filter.Shift) {
			return ErrInvalidOperationalData
		}
		filter.Shift = strings.ToLower(strings.TrimSpace(filter.Shift))
	}
	return nil
}

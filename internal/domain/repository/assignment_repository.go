package repository

import (
	"context"

	"peelen-ms-be/internal/domain/entity"
)

type AssignmentRepository interface {
	ListAssignments(context.Context, entity.OperationalAccess, entity.AssignmentFilter) ([]entity.Assignment, int, error)
	GetAssignment(context.Context, entity.OperationalAccess, string) (*entity.Assignment, error)
	CreateAssignment(context.Context, entity.OperationalAccess, entity.AssignmentInput) (*entity.Assignment, error)
	SetAssignmentActive(context.Context, entity.OperationalAccess, string, bool) (*entity.Assignment, error)
}

package repository

import (
	"context"

	"peelen-ms-be/internal/domain/entity"
)

type OperationalRepository interface {
	ListOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, filter entity.OperationalFilter) ([]entity.OperationalRecord, int, error)
	GetOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, id string) (*entity.OperationalRecord, error)
	CreateOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, input entity.OperationalInput, ipAddress string) (*entity.OperationalRecord, error)
	UpdateOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, id string, input entity.OperationalInput, ipAddress string) (*entity.OperationalRecord, error)
	DeleteOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, id, ipAddress string) error
}

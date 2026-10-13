package repository

import (
	"context"
	"errors"
	"time"

	"peelen-ms-be/internal/domain/entity"
)

var (
	ErrConflict = errors.New("resource already exists")
	ErrNotFound = errors.New("resource not found")
)

type AdminRepository interface {
	ListManagedUsers(ctx context.Context, vendorID string) ([]*entity.User, error)
	GetManagedUser(ctx context.Context, userID, vendorID string) (*entity.User, error)
	CreateManagedUser(ctx context.Context, user *entity.User, roles []string, siteIDs, vendorIDs []string, assignedBy, ipAddress string) error
	UpdateManagedUser(ctx context.Context, user *entity.User, actorID string, ipAddress string) error
	SetUserRoles(ctx context.Context, userID string, roles []string, assignedBy, ipAddress string) error
	SetUserScopes(ctx context.Context, userID string, siteIDs, vendorIDs []string, actorID, ipAddress string) error
	SetUserAuthorization(ctx context.Context, userID string, roles, siteIDs, vendorIDs []string, actorID, ipAddress string) error
	ResetUserPassword(ctx context.Context, userID, passwordHash, actorID, ipAddress string) error
	RevokeManagedUserSessions(ctx context.Context, userID, actorID, ipAddress string) error
	ListRoles(ctx context.Context) ([]map[string]any, error)
	ListAuditLogs(ctx context.Context, userID, action, entityName string, from, to *time.Time, limit int) ([]entity.AuditLog, error)
}

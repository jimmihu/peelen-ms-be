package repository

import (
	"context"

	"peelen-ms-be/internal/domain/entity"
)

// UserRepository defines the interface for user data operations
type UserRepository interface {
	// Create creates a new user
	Create(ctx context.Context, user *entity.User) error

	// GetByID retrieves a user by ID
	GetByID(ctx context.Context, id string) (*entity.User, error)

	// GetByUsername retrieves a user by username
	GetByUsername(ctx context.Context, username string) (*entity.User, error)

	// GetByEmail retrieves a user by email
	GetByEmail(ctx context.Context, email string) (*entity.User, error)

	// Update updates an existing user
	Update(ctx context.Context, user *entity.User) error

	// Delete deletes a user by ID
	Delete(ctx context.Context, id string) error

	// RecordFailedLogin increments the failure counter and writes an audit event.
	RecordFailedLogin(ctx context.Context, username, ipAddress string) error

	// RecordSuccessfulLogin clears login failures, records the login time and writes an audit event.
	RecordSuccessfulLogin(ctx context.Context, id, ipAddress string) error

	// LoadAuthorization loads current roles and scopes for a user.
	LoadAuthorization(ctx context.Context, user *entity.User) error

	// ChangePassword stores a new password, forces active sessions to close, and audits the change.
	ChangePassword(ctx context.Context, id, passwordHash, ipAddress string) error
}

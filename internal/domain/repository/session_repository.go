package repository

import (
	"context"

	"peelen-ms-be/internal/domain/entity"
)

// SessionRepository defines the interface for session data operations
type SessionRepository interface {
	// Create creates a new session
	Create(ctx context.Context, session *entity.Session) error

	// CreateSessionWithRefresh creates a login session and its refresh token atomically.
	CreateSessionWithRefresh(ctx context.Context, session *entity.Session, refresh *entity.RefreshToken) error

	// GetByTokenHash retrieves a session by its token hash
	GetByTokenHash(ctx context.Context, tokenHash string) (*entity.Session, error)

	// GetByUserID retrieves all sessions for a user
	GetByUserID(ctx context.Context, userID string) ([]*entity.Session, error)

	// RevokeByTokenHash revokes a session by its token hash
	RevokeByTokenHash(ctx context.Context, tokenHash, ipAddress string) error

	// RevokeAllByUserID revokes all sessions for a user
	RevokeAllByUserID(ctx context.Context, userID string) error

	// Touch updates the last-active timestamp for a session.
	Touch(ctx context.Context, tokenHash string) error

	// CreateRefreshToken stores a hashed refresh token associated with a login session.
	CreateRefreshToken(ctx context.Context, token *entity.RefreshToken) error

	// GetRefreshTokenByHash retrieves a refresh token by its hash.
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*entity.RefreshToken, error)

	// RotateRefreshToken atomically revokes and replaces a refresh token.
	RotateRefreshToken(ctx context.Context, oldHash string, replacement *entity.RefreshToken) (userID string, reused bool, err error)

	// RevokeRefreshToken revokes a refresh token by hash.
	RevokeRefreshToken(ctx context.Context, tokenHash, ipAddress string) error

	// RevokeAllForUser revokes every access and refresh session token for a user.
	RevokeAllForUser(ctx context.Context, userID, ipAddress string) error

	// DeleteExpired deletes all expired sessions
	DeleteExpired(ctx context.Context) error
}

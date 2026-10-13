package postgres

import (
	"context"
	"database/sql"
	"errors"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

// SessionRepositoryImpl implements repository.SessionRepository using PostgreSQL
type SessionRepositoryImpl struct {
	db *sql.DB
}

// NewSessionRepository creates a new SessionRepositoryImpl
func NewSessionRepository(db *sql.DB) repository.SessionRepository {
	return &SessionRepositoryImpl{db: db}
}

// Create creates a new session
func (r *SessionRepositoryImpl) Create(ctx context.Context, session *entity.Session) error {
	query := `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::inet)
		RETURNING created_at
	`
	return r.db.QueryRowContext(ctx, query, session.ID, session.UserID, session.TokenHash, session.ExpiresAt, session.UserAgent, session.IPAddress).
		Scan(&session.CreatedAt)
}

// CreateSessionWithRefresh creates a login session and its refresh token atomically.
func (r *SessionRepositoryImpl) CreateSessionWithRefresh(ctx context.Context, session *entity.Session, refresh *entity.RefreshToken) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::inet)
		RETURNING created_at
	`, session.ID, session.UserID, session.TokenHash, session.ExpiresAt, session.UserAgent, session.IPAddress).Scan(&session.CreatedAt); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO refresh_tokens (user_id, session_id, token_hash, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::inet)
		RETURNING id
	`, refresh.UserID, refresh.SessionID, refresh.TokenHash, refresh.ExpiresAt, refresh.UserAgent, refresh.IPAddress).Scan(&refresh.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateRefreshToken stores a hashed refresh token associated with a login session.
func (r *SessionRepositoryImpl) CreateRefreshToken(ctx context.Context, token *entity.RefreshToken) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO refresh_tokens (user_id, session_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, token.UserID, token.SessionID, token.TokenHash, token.ExpiresAt).Scan(&token.ID)
}

// GetRefreshTokenByHash retrieves a refresh token by its hash.
func (r *SessionRepositoryImpl) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*entity.RefreshToken, error) {
	token := &entity.RefreshToken{}
	var revokedAt sql.NullTime
	var sessionID sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, session_id, token_hash, expires_at, revoked_at
		FROM refresh_tokens WHERE token_hash = $1
	`, tokenHash).Scan(&token.ID, &token.UserID, &sessionID, &token.TokenHash, &token.ExpiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if revokedAt.Valid {
		token.RevokedAt = &revokedAt.Time
	}
	if sessionID.Valid {
		token.SessionID = sessionID.String
	}
	return token, nil
}

// RotateRefreshToken atomically revokes and replaces a refresh token.
func (r *SessionRepositoryImpl) RotateRefreshToken(ctx context.Context, oldHash string, replacement *entity.RefreshToken) (string, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()

	var userID, oldSessionID string
	err = tx.QueryRowContext(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		RETURNING user_id, session_id
	`, oldHash).Scan(&userID, &oldSessionID)
	if errors.Is(err, sql.ErrNoRows) {
		var revoked bool
		err = tx.QueryRowContext(ctx,
			`SELECT user_id, session_id, revoked_at IS NOT NULL FROM refresh_tokens WHERE token_hash = $1`,
			oldHash,
		).Scan(&userID, &oldSessionID, &revoked)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		if revoked {
			if _, err := tx.ExecContext(ctx,
				`UPDATE refresh_tokens SET revoked_at = COALESCE(revoked_at, now()) WHERE user_id = $1`, userID,
			); err != nil {
				return "", false, err
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE sessions SET revoked_at = COALESCE(revoked_at, now()) WHERE user_id = $1`, userID,
			); err != nil {
				return "", false, err
			}
			if err := tx.Commit(); err != nil {
				return "", false, err
			}
			return userID, true, nil
		}
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, oldSessionID,
	); err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::inet)
	`, replacement.SessionID, userID, replacement.AccessTokenHash, replacement.AccessExpiresAt, replacement.UserAgent, replacement.IPAddress); err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO refresh_tokens (user_id, session_id, token_hash, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::inet)
	`, userID, replacement.SessionID, replacement.TokenHash, replacement.ExpiresAt, replacement.UserAgent, replacement.IPAddress); err != nil {
		return "", false, err
	}
	if err := insertAudit(ctx, tx, userID, "UPDATE", "refresh_tokens", "", nil, map[string]any{"rotated": true}, replacement.IPAddress); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return userID, false, nil
}

// RevokeRefreshToken revokes a refresh token and its access session.
func (r *SessionRepositoryImpl) RevokeRefreshToken(ctx context.Context, tokenHash, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID, sessionID string
	err = tx.QueryRowContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
		RETURNING user_id, session_id
	`, tokenHash).Scan(&userID, &sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("refresh token not found or already revoked")
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (user_id, action, entity, entity_id, ip_address)
		VALUES ($1, 'LOGOUT', 'sessions', $2, NULLIF($3, '')::inet)
	`, userID, sessionID, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeAllForUser revokes all access and refresh tokens for a user.
func (r *SessionRepositoryImpl) RevokeAllForUser(ctx context.Context, userID, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = COALESCE(revoked_at, now()) WHERE user_id = $1`, userID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = COALESCE(revoked_at, now()) WHERE user_id = $1`, userID,
	); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, userID, "LOGOUT", "sessions", "", nil, map[string]any{"all_sessions_revoked": true}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

// GetByTokenHash retrieves a session by its token hash
func (r *SessionRepositoryImpl) GetByTokenHash(ctx context.Context, tokenHash string) (*entity.Session, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, revoked_at, last_active_at, created_at
		FROM sessions
		WHERE token_hash = $1
	`
	session := &entity.Session{}
	var revokedAt, lastActiveAt sql.NullTime
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt,
		&revokedAt, &lastActiveAt, &session.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if revokedAt.Valid {
		session.RevokedAt = &revokedAt.Time
	}
	if lastActiveAt.Valid {
		session.LastActiveAt = &lastActiveAt.Time
	}
	return session, nil
}

// GetByUserID retrieves all sessions for a user
func (r *SessionRepositoryImpl) GetByUserID(ctx context.Context, userID string) ([]*entity.Session, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, revoked_at, last_active_at, created_at
		FROM sessions
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*entity.Session
	for rows.Next() {
		session := &entity.Session{}
		var revokedAt, lastActiveAt sql.NullTime
		if err := rows.Scan(
			&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt,
			&revokedAt, &lastActiveAt, &session.CreatedAt,
		); err != nil {
			return nil, err
		}
		if revokedAt.Valid {
			session.RevokedAt = &revokedAt.Time
		}
		if lastActiveAt.Valid {
			session.LastActiveAt = &lastActiveAt.Time
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

// RevokeByTokenHash revokes a session by its token hash
func (r *SessionRepositoryImpl) RevokeByTokenHash(ctx context.Context, tokenHash, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var sessionID, userID string
	err = tx.QueryRowContext(ctx, `
		UPDATE sessions
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
		RETURNING id, user_id
	`, tokenHash).Scan(&sessionID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("session not found or already revoked")
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (user_id, action, entity, entity_id, ip_address)
		VALUES ($1, 'LOGOUT', 'sessions', $2, NULLIF($3, '')::inet)
	`, userID, sessionID, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeAllByUserID revokes all sessions for a user
func (r *SessionRepositoryImpl) RevokeAllByUserID(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	return err
}

// Touch updates the last-active timestamp for an active session.
func (r *SessionRepositoryImpl) Touch(ctx context.Context, tokenHash string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE sessions
		SET last_active_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
	`, tokenHash)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return errors.New("session not found or inactive")
	}
	return nil
}

// DeleteExpired deletes expired or revoked sessions
func (r *SessionRepositoryImpl) DeleteExpired(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < now() OR revoked_at IS NOT NULL`)
	return err
}

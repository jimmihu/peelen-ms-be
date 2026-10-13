package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

// UserRepositoryImpl implements repository.UserRepository using PostgreSQL
type UserRepositoryImpl struct {
	db *sql.DB
}

type rowScanner interface {
	Scan(dest ...any) error
}

const userColumns = `
	id, username, password_hash, full_name, email, phone,
	must_change_password, failed_login_count, locked_until, is_active,
	last_login_at, created_at, updated_at
`

// NewUserRepository creates a new UserRepositoryImpl
func NewUserRepository(db *sql.DB) repository.UserRepository {
	return &UserRepositoryImpl{db: db}
}

// Create creates a new user
func (r *UserRepositoryImpl) Create(ctx context.Context, user *entity.User) error {
	query := `
		INSERT INTO users (username, password_hash, full_name, email, phone)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + userColumns

	created, err := scanUser(r.db.QueryRowContext(
		ctx, query, user.Username, user.PasswordHash, user.FullName, user.Email, user.Phone,
	))
	if err != nil {
		return err
	}
	*user = *created
	return nil
}

// GetByID retrieves a user by ID
func (r *UserRepositoryImpl) GetByID(ctx context.Context, id string) (*entity.User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id,
	))
}

// GetByUsername retrieves a user by username
func (r *UserRepositoryImpl) GetByUsername(ctx context.Context, username string) (*entity.User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE username = $1`, strings.ToLower(username),
	))
}

// GetByEmail retrieves a user by email
func (r *UserRepositoryImpl) GetByEmail(ctx context.Context, email string) (*entity.User, error) {
	if email == "" {
		return nil, nil
	}
	return scanUser(r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = $1`, email,
	))
}

// Update updates mutable user attributes. Username is immutable in the database.
func (r *UserRepositoryImpl) Update(ctx context.Context, user *entity.User) error {
	query := `
		UPDATE users
		SET full_name = $1, email = $2, phone = $3, password_hash = $4,
		    must_change_password = $5, is_active = $6
		WHERE id = $7
		RETURNING updated_at
	`
	return r.db.QueryRowContext(ctx, query,
		user.FullName, user.Email, user.Phone, user.PasswordHash,
		user.MustChangePassword, user.IsActive, user.ID,
	).Scan(&user.UpdatedAt)
}

// Delete deactivates a user and revokes their sessions.
func (r *UserRepositoryImpl) Delete(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE users SET is_active = FALSE WHERE id = $1`, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordFailedLogin increments the failure counter and writes an audit event.
func (r *UserRepositoryImpl) RecordFailedLogin(ctx context.Context, username, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID string
	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET failed_login_count = failed_login_count + 1,
		    locked_until = CASE
		        WHEN failed_login_count + 1 >= 5
		             AND (locked_until IS NULL OR locked_until <= now())
		        THEN now() + interval '15 minutes'
		        ELSE locked_until
		    END
		WHERE username = $1 AND is_active = TRUE
		RETURNING id
	`, username).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username = $1`, username).Scan(&userID)
		if errors.Is(err, sql.ErrNoRows) {
			userID = ""
		} else if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	var auditUserID any
	if userID != "" {
		auditUserID = userID
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (user_id, action, entity, ip_address) VALUES ($1, 'LOGIN_FAILED', 'users', NULLIF($2, '')::inet)`,
		auditUserID, ipAddress,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordSuccessfulLogin clears login failures, records the login time and writes an audit event.
func (r *UserRepositoryImpl) RecordSuccessfulLogin(ctx context.Context, id, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE users
		SET failed_login_count = 0, locked_until = NULL, last_login_at = now()
		WHERE id = $1 AND is_active = TRUE
	`, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_logs (user_id, action, entity, entity_id, ip_address) VALUES ($1, 'LOGIN', 'users', $1, NULLIF($2, '')::inet)`,
		id, ipAddress,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// LoadAuthorization loads current roles and scopes for a user.
func (r *UserRepositoryImpl) LoadAuthorization(ctx context.Context, user *entity.User) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.code, NULL::uuid AS site_id, NULL::uuid AS vendor_id
		FROM user_roles ur JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id = $1
		UNION ALL
		SELECT NULL, us.site_id, us.vendor_id
		FROM user_scopes us
		WHERE us.user_id = $1
	`, user.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	user.Roles = []string{}
	user.Sites = []string{}
	user.Vendors = []string{}
	for rows.Next() {
		var role, siteID, vendorID sql.NullString
		if err := rows.Scan(&role, &siteID, &vendorID); err != nil {
			return err
		}
		if role.Valid {
			user.Roles = append(user.Roles, role.String)
		}
		if siteID.Valid {
			user.Sites = append(user.Sites, siteID.String)
		}
		if vendorID.Valid {
			user.Vendors = append(user.Vendors, vendorID.String)
		}
	}
	return rows.Err()
}

// ChangePassword stores a new password, revokes active sessions and audits the change.
func (r *UserRepositoryImpl) ChangePassword(ctx context.Context, id, passwordHash, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := userAuditSnapshot(ctx, tx, id)
	if err != nil {
		return err
	}
	if before == nil {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET password_hash = $1, must_change_password = FALSE WHERE id = $2
	`, passwordHash, id); err != nil {
		return err
	}
	if err := revokeAllTokensTx(ctx, tx, id); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, id, "UPDATE", "users", id, before, map[string]any{
		"password_changed": true, "must_change_password": false,
	}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

func scanUser(row rowScanner) (*entity.User, error) {
	user := &entity.User{}
	var fullName, email, phone sql.NullString
	var lockedUntil, lastLoginAt sql.NullTime
	err := row.Scan(
		&user.ID, &user.Username, &user.PasswordHash, &fullName, &email, &phone,
		&user.MustChangePassword, &user.FailedLoginCount, &lockedUntil, &user.IsActive,
		&lastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	user.FullName = nullableString(fullName)
	user.Email = nullableString(email)
	user.Phone = nullableString(phone)
	if lockedUntil.Valid {
		user.LockedUntil = &lockedUntil.Time
	}
	if lastLoginAt.Valid {
		user.LastLoginAt = &lastLoginAt.Time
	}
	return user, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

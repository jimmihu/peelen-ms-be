package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"

	"github.com/lib/pq"
)

type AdminRepositoryImpl struct {
	db *sql.DB
}

func NewAdminRepository(db *sql.DB) repository.AdminRepository {
	return &AdminRepositoryImpl{db: db}
}

// managedUserFilter is the SQL condition on users for a manager's scope; args
// continue after the given ones.
func managedUserFilter(scope entity.ManagedUserScope, args []any) (string, []any) {
	var conditions []string
	if scope.Role != "" {
		args = append(args, scope.Role)
		// Only accounts whose roles are exactly this one role.
		conditions = append(conditions, fmt.Sprintf(`EXISTS (SELECT 1 FROM user_roles ur JOIN roles ro ON ro.id = ur.role_id
			WHERE ur.user_id = users.id AND ro.code = $%[1]d)
			AND NOT EXISTS (SELECT 1 FROM user_roles ur JOIN roles ro ON ro.id = ur.role_id
			WHERE ur.user_id = users.id AND ro.code <> $%[1]d)`, len(args)))
	}
	if scope.VendorID != "" {
		args = append(args, scope.VendorID)
		conditions = append(conditions, fmt.Sprintf(
			`EXISTS (SELECT 1 FROM user_scopes us WHERE us.user_id = users.id AND us.vendor_id = $%d)`, len(args)))
	}
	if scope.SiteIDs != nil {
		args = append(args, pq.Array(scope.SiteIDs))
		conditions = append(conditions, fmt.Sprintf(`EXISTS (SELECT 1 FROM user_scopes us
			JOIN vendor_sites vs ON vs.vendor_id = us.vendor_id
			WHERE us.user_id = users.id AND vs.site_id = ANY($%d::uuid[]))`, len(args)))
	}
	if len(conditions) == 0 {
		return "true", args
	}
	return strings.Join(conditions, " AND "), args
}

func (r *AdminRepositoryImpl) ListManagedUsers(ctx context.Context, scope entity.ManagedUserScope) ([]*entity.User, error) {
	where, args := managedUserFilter(scope, nil)
	query := `SELECT ` + userColumns + ` FROM users WHERE ` + where + ` ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []*entity.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		if err := r.loadAuthorization(ctx, user); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (r *AdminRepositoryImpl) GetManagedUser(ctx context.Context, userID string, scope entity.ManagedUserScope) (*entity.User, error) {
	where, args := managedUserFilter(scope, []any{userID})
	query := `SELECT ` + userColumns + ` FROM users WHERE id = $1 AND ` + where
	user, err := scanUser(r.db.QueryRowContext(ctx, query, args...))
	if err != nil || user == nil {
		return user, err
	}
	if err := r.loadAuthorization(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// VendorOperatesAtSites reports whether the vendor works at one of the sites.
func (r *AdminRepositoryImpl) VendorOperatesAtSites(ctx context.Context, vendorID string, siteIDs []string) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM vendor_sites WHERE vendor_id = $1::uuid AND site_id = ANY($2::uuid[]))`,
		vendorID, pq.Array(siteIDs)).Scan(&ok)
	return ok, err
}

func (r *AdminRepositoryImpl) CreateManagedUser(ctx context.Context, user *entity.User, roles, siteIDs, vendorIDs []string, assignedBy, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users (username, password_hash, full_name, email, phone, must_change_password)
		VALUES ($1, $2, $3, $4, $5, TRUE)
		RETURNING id, is_active, must_change_password, failed_login_count, created_at, updated_at
	`, user.Username, user.PasswordHash, user.FullName, user.Email, user.Phone).Scan(
		&user.ID, &user.IsActive, &user.MustChangePassword, &user.FailedLoginCount, &user.CreatedAt, &user.UpdatedAt,
	); err != nil {
		return translateDBError(err)
	}
	for _, role := range roles {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_roles (user_id, role_id, assigned_by)
			SELECT $1, id, NULLIF($3, '')::uuid FROM roles WHERE code = $2
		`, user.ID, role, assignedBy); err != nil {
			return err
		}
	}
	for _, siteID := range siteIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_scopes (user_id, site_id) VALUES ($1, $2)`, user.ID, siteID); err != nil {
			return err
		}
	}
	for _, vendorID := range vendorIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_scopes (user_id, vendor_id) VALUES ($1, $2)`, user.ID, vendorID); err != nil {
			return err
		}
	}
	if err := insertAudit(ctx, tx, assignedBy, "CREATE", "users", user.ID, nil, map[string]any{
		"username": user.Username, "roles": roles, "site_ids": siteIDs, "vendor_ids": vendorIDs,
	}, ipAddress); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.loadAuthorization(ctx, user)
}

func (r *AdminRepositoryImpl) UpdateManagedUser(ctx context.Context, user *entity.User, actorID, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := userAuditSnapshot(ctx, tx, user.ID)
	if err != nil {
		return err
	}
	if before == nil {
		return sql.ErrNoRows
	}
	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET full_name = $1, email = $2, phone = $3, is_active = $4
		WHERE id = $5
		RETURNING updated_at
	`, user.FullName, user.Email, user.Phone, user.IsActive, user.ID).Scan(&user.UpdatedAt)
	if err != nil {
		return translateDBError(err)
	}
	if !user.IsActive {
		if err := revokeAllTokensTx(ctx, tx, user.ID); err != nil {
			return err
		}
	}
	if err := insertAudit(ctx, tx, actorID, "UPDATE", "users", user.ID, before, map[string]any{
		"full_name": user.FullName, "email": user.Email, "phone": user.Phone, "is_active": user.IsActive,
	}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AdminRepositoryImpl) SetUserRoles(ctx context.Context, userID string, roles []string, assignedBy, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := roleCodesTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, role := range roles {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO user_roles (user_id, role_id, assigned_by)
			SELECT $1, id, $3 FROM roles WHERE code = $2
		`, userID, role, assignedBy)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("unknown role %q", role)
		}
	}
	if err := revokeAllTokensTx(ctx, tx, userID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, assignedBy, "UPDATE", "user_roles", userID, map[string]any{"roles": before}, map[string]any{"roles": roles}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AdminRepositoryImpl) SetUserScopes(ctx context.Context, userID string, siteIDs, vendorIDs []string, actorID, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := scopeSnapshotTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_scopes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, siteID := range siteIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_scopes (user_id, site_id) VALUES ($1, $2)`, userID, siteID); err != nil {
			return err
		}
	}
	for _, vendorID := range vendorIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_scopes (user_id, vendor_id) VALUES ($1, $2)`, userID, vendorID); err != nil {
			return err
		}
	}
	if err := insertAudit(ctx, tx, actorID, "UPDATE", "user_scopes", userID, before, map[string]any{"site_ids": siteIDs, "vendor_ids": vendorIDs}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AdminRepositoryImpl) SetUserAuthorization(ctx context.Context, userID string, roles, siteIDs, vendorIDs []string, actorID, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	beforeRoles, err := roleCodesTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	beforeScopes, err := scopeSnapshotTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	if _, err := userAuditSnapshot(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, role := range roles {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO user_roles (user_id, role_id, assigned_by)
			SELECT $1, id, NULLIF($3, '')::uuid FROM roles WHERE code = $2
		`, userID, role, actorID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("unknown role %q", role)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_scopes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, siteID := range siteIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_scopes (user_id, site_id) VALUES ($1, $2)`, userID, siteID); err != nil {
			return err
		}
	}
	for _, vendorID := range vendorIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_scopes (user_id, vendor_id) VALUES ($1, $2)`, userID, vendorID); err != nil {
			return err
		}
	}
	if err := revokeAllTokensTx(ctx, tx, userID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actorID, "UPDATE", "user_roles", userID, map[string]any{"roles": beforeRoles}, map[string]any{"roles": roles}, ipAddress); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actorID, "UPDATE", "user_scopes", userID, beforeScopes, map[string]any{"site_ids": siteIDs, "vendor_ids": vendorIDs}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AdminRepositoryImpl) ResetUserPassword(ctx context.Context, userID, passwordHash, actorID, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := userAuditSnapshot(ctx, tx, userID)
	if err != nil {
		return err
	}
	if before == nil {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET password_hash = $1, must_change_password = TRUE WHERE id = $2
	`, passwordHash, userID); err != nil {
		return err
	}
	if err := revokeAllTokensTx(ctx, tx, userID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actorID, "UPDATE", "users", userID, before, map[string]any{"password_reset": true, "must_change_password": true}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AdminRepositoryImpl) RevokeManagedUserSessions(ctx context.Context, userID, actorID, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := revokeAllTokensTx(ctx, tx, userID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actorID, "UPDATE", "sessions", userID, nil, map[string]any{"revoked": true}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *AdminRepositoryImpl) ListRoles(ctx context.Context) ([]map[string]any, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, code, name FROM roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := []map[string]any{}
	for rows.Next() {
		var id int16
		var code, name string
		if err := rows.Scan(&id, &code, &name); err != nil {
			return nil, err
		}
		roles = append(roles, map[string]any{"id": id, "code": code, "name": name})
	}
	return roles, rows.Err()
}

func (r *AdminRepositoryImpl) ListAuditLogs(ctx context.Context, userID, action, entityName string, from, to *time.Time, limit int) ([]entity.AuditLog, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, action, entity, entity_id, before_data, after_data, host(ip_address), created_at
		FROM audit_logs
		WHERE ($1 = '' OR user_id = NULLIF($1, '')::uuid)
		  AND ($2 = '' OR action = $2)
		  AND ($3 = '' OR entity = $3)
		  AND ($4::timestamptz IS NULL OR created_at >= $4)
		  AND ($5::timestamptz IS NULL OR created_at < $5)
		ORDER BY created_at DESC LIMIT $6
	`, userID, action, entityName, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := []entity.AuditLog{}
	for rows.Next() {
		var item entity.AuditLog
		var userIDValue, entityValue, entityIDValue, ipValue sql.NullString
		var before, after []byte
		if err := rows.Scan(&item.ID, &userIDValue, &item.Action, &entityValue, &entityIDValue, &before, &after, &ipValue, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.UserID = nullStringPtr(userIDValue)
		item.Entity = nullStringPtr(entityValue)
		item.EntityID = nullStringPtr(entityIDValue)
		item.IPAddress = nullStringPtr(ipValue)
		item.BeforeData, item.AfterData = before, after
		logs = append(logs, item)
	}
	return logs, rows.Err()
}

func (r *AdminRepositoryImpl) loadAuthorization(ctx context.Context, user *entity.User) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.code FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = $1
	`, user.ID)
	if err != nil {
		return err
	}
	user.Roles = []string{}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			rows.Close()
			return err
		}
		user.Roles = append(user.Roles, role)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	scopeRows, err := r.db.QueryContext(ctx, `
		SELECT us.site_id::text, s.code, s.name, us.vendor_id::text, v.code, v.name
		FROM user_scopes us
		LEFT JOIN sites s ON s.id = us.site_id
		LEFT JOIN vendors v ON v.id = us.vendor_id
		WHERE us.user_id = $1
		ORDER BY s.code NULLS LAST, v.code NULLS LAST
	`, user.ID)
	if err != nil {
		return err
	}
	defer scopeRows.Close()
	user.Sites, user.Vendors, user.Scopes = []string{}, []string{}, []entity.UserScope{}
	for scopeRows.Next() {
		var siteID, siteCode, siteName, vendorID, vendorCode, vendorName sql.NullString
		if err := scopeRows.Scan(&siteID, &siteCode, &siteName, &vendorID, &vendorCode, &vendorName); err != nil {
			return err
		}
		if siteID.Valid {
			user.Sites = append(user.Sites, siteID.String)
			user.Scopes = append(user.Scopes, entity.UserScope{Type: "SITE", ID: siteID.String, Code: siteCode.String, Name: siteName.String})
		}
		if vendorID.Valid {
			user.Vendors = append(user.Vendors, vendorID.String)
			user.Scopes = append(user.Scopes, entity.UserScope{Type: "VENDOR", ID: vendorID.String, Code: vendorCode.String, Name: vendorName.String})
		}
	}
	return scopeRows.Err()
}

func insertAudit(ctx context.Context, tx *sql.Tx, actorID, action, entityName, entityID string, before, after any, ipAddress ...string) error {
	var beforeJSON, afterJSON []byte
	var err error
	if before != nil {
		beforeJSON, err = json.Marshal(before)
		if err != nil {
			return err
		}
	}
	if after != nil {
		afterJSON, err = json.Marshal(after)
		if err != nil {
			return err
		}
	}
	var beforeValue, afterValue any
	if beforeJSON != nil {
		beforeValue = string(beforeJSON)
	}
	if afterJSON != nil {
		afterValue = string(afterJSON)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_logs (user_id, action, entity, entity_id, before_data, after_data, ip_address)
		VALUES (NULLIF($1, '')::uuid, $2, $3, NULLIF($4, '')::uuid, $5, $6, NULLIF($7, '')::inet)
	`, actorID, action, entityName, entityID, beforeValue, afterValue, firstString(ipAddress))
	return err
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func translateDBError(err error) error {
	var pgError *pq.Error
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return repository.ErrConflict
	}
	return err
}

func revokeAllTokensTx(ctx context.Context, tx *sql.Tx, userID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at = COALESCE(revoked_at, now()) WHERE user_id = $1`, userID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = COALESCE(revoked_at, now()) WHERE user_id = $1`, userID)
	return err
}

func userAuditSnapshot(ctx context.Context, tx *sql.Tx, userID string) (map[string]any, error) {
	var username string
	var fullName, email, phone sql.NullString
	var active, mustChange bool
	err := tx.QueryRowContext(ctx, `
		SELECT username, full_name, email, phone, is_active, must_change_password FROM users WHERE id = $1
	`, userID).Scan(&username, &fullName, &email, &phone, &active, &mustChange)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"username": username, "full_name": nullStringPtr(fullName), "email": nullStringPtr(email),
		"phone": nullStringPtr(phone), "is_active": active, "must_change_password": mustChange,
	}, nil
}

func roleCodesTx(ctx context.Context, tx *sql.Tx, userID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.code FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = $1 ORDER BY r.code`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scopeSnapshotTx(ctx context.Context, tx *sql.Tx, userID string) (map[string][]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT site_id, vendor_id FROM user_scopes WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]string{"site_ids": {}, "vendor_ids": {}}
	for rows.Next() {
		var siteID, vendorID sql.NullString
		if err := rows.Scan(&siteID, &vendorID); err != nil {
			return nil, err
		}
		if siteID.Valid {
			result["site_ids"] = append(result["site_ids"], siteID.String)
		}
		if vendorID.Valid {
			result["vendor_ids"] = append(result["vendor_ids"], vendorID.String)
		}
	}
	return result, rows.Err()
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

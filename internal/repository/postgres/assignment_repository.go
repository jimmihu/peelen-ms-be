package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type AssignmentRepositoryImpl struct {
	db *sql.DB
}

func NewAssignmentRepository(db *sql.DB) repository.AssignmentRepository {
	return &AssignmentRepositoryImpl{db: db}
}

const assignmentFrom = `pipeline_item_assignments a
	JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
	JOIN pipelines p ON p.id = pi.pipeline_id
	JOIN items i ON i.id = pi.item_id
	JOIN users u ON u.id = a.user_id`

func (r *AssignmentRepositoryImpl) ListAssignments(ctx context.Context, access entity.OperationalAccess, filter entity.AssignmentFilter) ([]entity.Assignment, int, error) {
	where, args := assignmentScope(access)
	addFilter := func(expr, value string) {
		if value != "" {
			args = append(args, value)
			where = append(where, fmt.Sprintf(expr, len(args)))
		}
	}
	addFilter("p.vendor_id = $%d", filter.VendorID)
	addFilter("EXISTS (SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = $%d)", filter.SiteID)
	addFilter("p.id = $%d", filter.PipelineID)
	addFilter("pi.id = $%d", filter.PipelineItemID)
	addFilter("a.user_id = $%d", filter.UserID)
	if filter.IsActive != nil {
		args = append(args, *filter.IsActive)
		where = append(where, fmt.Sprintf("a.is_active = $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM "+assignmentFrom+" WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.Limit, (filter.Page-1)*filter.Limit)
	result, err := r.queryAssignments(ctx, whereSQL+fmt.Sprintf(
		" ORDER BY p.name, pi.sort_order, u.username LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args)
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

func (r *AssignmentRepositoryImpl) GetAssignment(ctx context.Context, access entity.OperationalAccess, id string) (*entity.Assignment, error) {
	where, args := assignmentScope(access)
	args = append(args, id)
	result, err := r.queryAssignments(ctx, strings.Join(where, " AND ")+fmt.Sprintf(" AND a.id = $%d", len(args)), args)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, repository.ErrNotFound
	}
	return &result[0], nil
}

// CreateAssignment inserts a new assignment or reactivates an inactive one with
// the same (pipeline item, user). The target must be an active WORKER
// scoped to the pipeline's vendor, and the pipeline must be in the actor's scope.
func (r *AssignmentRepositoryImpl) CreateAssignment(ctx context.Context, access entity.OperationalAccess, input entity.AssignmentInput) (*entity.Assignment, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO pipeline_item_assignments (pipeline_item_id, user_id, assigned_by)
		SELECT pi.id, u.id, $3::uuid
		FROM pipeline_items pi
		JOIN pipelines p ON p.id = pi.pipeline_id
		JOIN items i ON i.id = pi.item_id
		JOIN users u ON u.id = $2::uuid
		WHERE pi.id = $1::uuid AND pi.status = 'ACTIVE' AND p.status = 'ACTIVE' AND i.status = 'ACTIVE'
			AND u.is_active AND `+assignableWorkerSQL+` AND `+assignmentVendorScopeSQL(4)+`
		ON CONFLICT (pipeline_item_id, user_id) DO UPDATE
			SET is_active = true, assigned_by = EXCLUDED.assigned_by, updated_at = now()
			WHERE NOT pipeline_item_assignments.is_active
		RETURNING id::text
	`, input.PipelineItemID, input.UserID, access.UserID,
		workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, r.explainMissingInsert(ctx, access, input)
	}
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	return r.GetAssignment(ctx, access, id)
}

// explainMissingInsert separates "already actively assigned" (409) from
// invalid or out-of-scope targets (404) after an insert affected no rows.
func (r *AssignmentRepositoryImpl) explainMissingInsert(ctx context.Context, access entity.OperationalAccess, input entity.AssignmentInput) error {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pipeline_item_assignments a
			JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
			JOIN pipelines p ON p.id = pi.pipeline_id
			WHERE a.pipeline_item_id = $1::uuid AND a.user_id = $2::uuid AND a.is_active
				AND `+assignmentVendorScopeSQL(3)+`
		)
	`, input.PipelineItemID, input.UserID,
		workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return repository.ErrConflict
	}
	return repository.ErrNotFound
}

func (r *AssignmentRepositoryImpl) SetAssignmentActive(ctx context.Context, access entity.OperationalAccess, id string, active bool) (*entity.Assignment, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE pipeline_item_assignments a SET is_active = $2, updated_at = now()
		FROM pipeline_items pi
		JOIN pipelines p ON p.id = pi.pipeline_id
		CROSS JOIN users u
		WHERE a.id = $1::uuid AND pi.id = a.pipeline_item_id AND u.id = a.user_id
			AND `+assignmentVendorScopeSQL(3)+`
			AND (NOT $2::boolean OR (u.is_active AND `+assignableWorkerSQL+`))
	`, id, active, workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	if err := workflowRequireAffected(result); err != nil {
		return nil, err
	}
	return r.GetAssignment(ctx, access, id)
}

func (r *AssignmentRepositoryImpl) queryAssignments(ctx context.Context, whereSQL string, args []any) ([]entity.Assignment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id::text, pi.id::text, p.id::text, p.name, i.id::text, i.name, p.vendor_id::text,
			u.id::text, u.username, u.full_name, a.is_active, a.assigned_by::text, a.created_at, a.updated_at
		FROM `+assignmentFrom+` WHERE `+whereSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]entity.Assignment, 0)
	for rows.Next() {
		var assignment entity.Assignment
		var assignedBy sql.NullString
		if err := rows.Scan(&assignment.ID, &assignment.PipelineItemID, &assignment.PipelineID, &assignment.PipelineName,
			&assignment.ItemID, &assignment.ItemName, &assignment.VendorID, &assignment.UserID, &assignment.Username, &assignment.UserFullName,
			&assignment.IsActive, &assignedBy, &assignment.CreatedAt, &assignment.UpdatedAt); err != nil {
			return nil, err
		}
		assignment.AssignedBy = nullStringPtr(assignedBy)
		result = append(result, assignment)
	}
	return result, rows.Err()
}

// assignmentScope limits rows to the actor's vendors/sites; a plain Worker only
// sees their own assignments.
func assignmentScope(access entity.OperationalAccess) ([]string, []any) {
	where := []string{assignmentVendorScopeSQL(1)}
	args := []any{workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)}
	if hasWorkflowRole(access.Roles, "WORKER") && !hasWorkflowRole(access.Roles, "ADMIN_WORKER", "ADMIN", "SUPER_ADMIN") {
		args = append(args, access.UserID)
		where = append(where, fmt.Sprintf("a.user_id = $%d", len(args)))
	}
	return where, args
}

// assignmentVendorScopeSQL expects the super-admin flag, vendor IDs and site IDs
// as three consecutive parameters starting at first, and an alias p for pipelines.
func assignmentVendorScopeSQL(first int) string {
	return fmt.Sprintf(`($%d::boolean OR p.vendor_id = ANY($%d::uuid[]) OR EXISTS (
		SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = ANY($%d::uuid[])
	))`, first, first+1, first+2)
}

// assignableWorkerSQL requires user alias u to hold the WORKER role and a scope
// on the vendor that owns pipeline alias p.
const assignableWorkerSQL = `EXISTS (
		SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id = u.id AND r.code = 'WORKER'
	) AND EXISTS (
		SELECT 1 FROM user_scopes us WHERE us.user_id = u.id AND us.vendor_id = p.vendor_id
	)`

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/lib/pq"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type OperationalRepositoryImpl struct {
	db *sql.DB
}

func NewOperationalRepository(db *sql.DB) repository.OperationalRepository {
	return &OperationalRepositoryImpl{db: db}
}

func (r *OperationalRepositoryImpl) ListOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, filter entity.OperationalFilter) ([]entity.OperationalRecord, int, error) {
	from, selectSQL, scopeSQL, args, err := operationalQuery(resource, access)
	if err != nil {
		return nil, 0, err
	}
	where := []string{scopeSQL}
	next := len(args) + 1
	if filter.VendorID != "" {
		if !uuidString(filter.VendorID) {
			return nil, 0, entity.ErrInvalidID
		}
		switch resource {
		case entity.ResourceInventory, entity.ResourcePolicy, entity.ResourcePipeline:
			where = append(where, fmt.Sprintf("t.vendor_id = $%d", next))
		case entity.ResourcePipelineItem:
			where = append(where, fmt.Sprintf("p.vendor_id = $%d", next))
		case entity.ResourceItem:
			where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM pipeline_items pi JOIN pipelines px ON px.id = pi.pipeline_id WHERE pi.item_id = t.id AND px.vendor_id = $%d)", next))
		}
		args = append(args, filter.VendorID)
		next++
	}
	if filter.PipelineID != "" {
		if !uuidString(filter.PipelineID) || resource != entity.ResourcePipelineItem {
			return nil, 0, entity.ErrInvalidID
		}
		where = append(where, fmt.Sprintf("t.pipeline_id = $%d", next))
		args = append(args, filter.PipelineID)
		next++
	}
	if filter.SiteID != "" {
		if !uuidString(filter.SiteID) {
			return nil, 0, entity.ErrInvalidID
		}
		switch resource {
		case entity.ResourcePipeline:
			where = append(where, fmt.Sprintf("t.site_id = $%d", next))
		case entity.ResourceInventory, entity.ResourcePolicy:
			where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = t.vendor_id AND vs.site_id = $%d)", next))
		case entity.ResourcePipelineItem:
			where = append(where, fmt.Sprintf("p.site_id = $%d", next))
		case entity.ResourceItem:
			where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM pipeline_items pi JOIN pipelines px ON px.id = pi.pipeline_id JOIN vendor_sites vs ON vs.vendor_id = px.vendor_id WHERE pi.item_id = t.id AND vs.site_id = $%d)", next))
		}
		args = append(args, filter.SiteID)
		next++
	}
	if filter.Status != "" {
		if !validOperationalStatus(filter.Status) {
			return nil, 0, entity.ErrInvalidOperationalStatus
		}
		where = append(where, fmt.Sprintf("t.status = $%d", next))
		args = append(args, filter.Status)
		next++
	}
	if filter.Condition != "" {
		if resource != entity.ResourceInventory || !validInventoryCondition(filter.Condition) {
			return nil, 0, entity.ErrInvalidOperationalStatus
		}
		where = append(where, fmt.Sprintf("t.status_condition = $%d", next))
		args = append(args, filter.Condition)
		next++
	}
	if filter.Query != "" {
		searchColumns := operationalSearchColumns(resource)
		where = append(where, fmt.Sprintf("concat_ws(' ', %s) ILIKE $%d", searchColumns, next))
		args = append(args, "%"+filter.Query+"%")
		next++
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM "+from+" WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.Limit, (filter.Page-1)*filter.Limit)
	rows, err := r.db.QueryContext(ctx,
		"SELECT "+selectSQL+" FROM "+from+" WHERE "+whereSQL+
			fmt.Sprintf(" ORDER BY t.created_at DESC LIMIT $%d OFFSET $%d", next, next+1),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	records := make([]entity.OperationalRecord, 0)
	for rows.Next() {
		record, err := scanOperationalRecord(rows)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func (r *OperationalRepositoryImpl) GetOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, id string) (*entity.OperationalRecord, error) {
	from, selectSQL, scopeSQL, args, err := operationalQuery(resource, access)
	if err != nil {
		return nil, err
	}
	args = append(args, id)
	row := r.db.QueryRowContext(ctx, "SELECT "+selectSQL+" FROM "+from+
		fmt.Sprintf(" WHERE %s AND t.id = $%d", scopeSQL, len(args)), args...)
	record, err := scanOperationalRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *OperationalRepositoryImpl) CreateOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, input entity.OperationalInput, ipAddress string) (*entity.OperationalRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id string
	switch resource {
	case entity.ResourceInventory:
		err = tx.QueryRowContext(ctx, `
			INSERT INTO inventories (vendor_id, item, jenis_item, qty, img, status_condition, created_by)
			SELECT $4, $5, $6, COALESCE($7, 0), $8, COALESCE($9, 'GOOD'), $10
			FROM vendors v
			WHERE v.id = $4 AND `+scopePredicate("v.id", 1)+`
			RETURNING id::text
		`, []any{
			hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs),
			*input.VendorID, strings.TrimSpace(*input.Item), input.ItemType, input.Quantity, input.Image, input.Condition, access.UserID,
		}...).Scan(&id)
	case entity.ResourcePolicy:
		err = tx.QueryRowContext(ctx, `
			INSERT INTO policies (vendor_id, name, file_url, created_by)
			SELECT $4, $5, $6, $7
			FROM vendors v
			WHERE v.id = $4 AND `+scopePredicate("v.id", 1)+`
			RETURNING id::text
		`, hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs),
			*input.VendorID, strings.TrimSpace(*input.Name), input.FileURL, access.UserID).Scan(&id)
	case entity.ResourcePipeline:
		// The site must be one the vendor works at, and within the caller's scope.
		err = tx.QueryRowContext(ctx, `
			INSERT INTO pipelines (vendor_id, site_id, name, description, created_by)
			SELECT v.id, vs.site_id, $5, $6, $7
			FROM vendors v
			JOIN vendor_sites vs ON vs.vendor_id = v.id AND vs.site_id = $8
			WHERE v.id = $4 AND `+scopePredicate("v.id", 1)+`
				-- Site-scoped admins may only use their own sites.
				AND ($1::boolean OR v.id = ANY($2::uuid[]) OR vs.site_id = ANY($3::uuid[]))
			RETURNING id::text
		`, hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs),
			*input.VendorID, strings.TrimSpace(*input.Name), input.Description, access.UserID, *input.SiteID).Scan(&id)
	case entity.ResourceItem:
		err = tx.QueryRowContext(ctx, `
			INSERT INTO items (name, description, created_by)
			VALUES ($1, $2, $3)
			RETURNING id::text
		`, strings.TrimSpace(*input.Name), input.Description, access.UserID).Scan(&id)
	case entity.ResourcePipelineItem:
		err = tx.QueryRowContext(ctx, `
			INSERT INTO pipeline_items (pipeline_id, item_id, sort_order, created_by)
			SELECT p.id, $2, COALESCE($3, 0), $4
			FROM pipelines p
			WHERE p.id = $1 AND `+scopePredicate("p.vendor_id", 5)+` AND `+pipelineWriteScope("p", 5)+`
			RETURNING id::text
		`, *input.PipelineID, *input.ItemID, input.SortOrder, access.UserID,
			hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)).Scan(&id)
	default:
		return nil, entity.ErrInvalidOperationalResource
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	record, err := getOperationalTx(ctx, tx, access, resource, id)
	if err != nil {
		return nil, err
	}
	if err := insertAudit(ctx, tx, access.UserID, "CREATE", string(resource), id, nil, record, ipAddress); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return record, nil
}

func (r *OperationalRepositoryImpl) UpdateOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, id string, input entity.OperationalInput, ipAddress string) (*entity.OperationalRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	before, err := getOperationalTx(ctx, tx, access, resource, id)
	if err != nil {
		return nil, err
	}
	var result sql.Result
	switch resource {
	case entity.ResourceInventory:
		result, err = tx.ExecContext(ctx, `
			UPDATE inventories t SET
				vendor_id = COALESCE($1, t.vendor_id), item = COALESCE($2, t.item),
				jenis_item = COALESCE($3, t.jenis_item), qty = COALESCE($4, t.qty),
				img = COALESCE($5, t.img), status_condition = COALESCE($6, t.status_condition),
				status = COALESCE($7, t.status)
			WHERE t.id = $8 AND `+scopePredicate("t.vendor_id", 9), input.VendorID, input.Item, input.ItemType, input.Quantity, input.Image, input.Condition, input.Status, id,
			hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	case entity.ResourcePolicy:
		result, err = tx.ExecContext(ctx, `
			UPDATE policies t SET vendor_id = COALESCE($1, t.vendor_id),
				name = COALESCE($2, t.name), file_url = COALESCE($3, t.file_url),
				status = COALESCE($4, t.status)
			WHERE t.id = $5 AND `+scopePredicate("t.vendor_id", 6), input.VendorID, input.Name, input.FileURL, input.Status, id,
			hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	case entity.ResourcePipeline:
		result, err = tx.ExecContext(ctx, `
			UPDATE pipelines t SET vendor_id = COALESCE($1, t.vendor_id),
				name = COALESCE($2, t.name), description = COALESCE($3, t.description),
				status = COALESCE($4, t.status)
			WHERE t.id = $5 AND `+scopePredicate("t.vendor_id", 6)+` AND `+pipelineWriteScope("t", 6), input.VendorID, input.Name, input.Description, input.Status, id,
			hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	case entity.ResourceItem:
		result, err = tx.ExecContext(ctx, `
			UPDATE items t SET name = COALESCE($1, t.name),
				description = COALESCE($2, t.description), status = COALESCE($3, t.status)
			WHERE t.id = $4 AND `+itemScopePredicate(5), input.Name, input.Description, input.Status, id,
			hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs), access.UserID, hasOperationalRole(access.Roles, "WORKER"))
	case entity.ResourcePipelineItem:
		result, err = tx.ExecContext(ctx, `
			UPDATE pipeline_items t SET pipeline_id = COALESCE($1, t.pipeline_id),
				item_id = COALESCE($2, t.item_id), sort_order = COALESCE($3, t.sort_order),
				status = COALESCE($4, t.status)
			FROM pipelines p
			WHERE t.id = $5 AND p.id = t.pipeline_id AND `+scopePredicate("p.vendor_id", 6)+` AND `+pipelineWriteScope("p", 6),
			input.PipelineID, input.ItemID, input.SortOrder, input.Status, id,
			hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	default:
		return nil, entity.ErrInvalidOperationalResource
	}
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, repository.ErrNotFound
	}
	after, err := getOperationalTx(ctx, tx, access, resource, id)
	if err != nil {
		return nil, err
	}
	if err := insertAudit(ctx, tx, access.UserID, "UPDATE", string(resource), id, before, after, ipAddress); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return after, nil
}

func (r *OperationalRepositoryImpl) DeleteOperational(ctx context.Context, access entity.OperationalAccess, resource entity.OperationalResource, id, ipAddress string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return translateOperationalDBError(err)
	}
	defer tx.Rollback()
	before, err := getOperationalTx(ctx, tx, access, resource, id)
	if err != nil {
		return err
	}
	var result sql.Result
	switch resource {
	case entity.ResourceInventory:
		result, err = tx.ExecContext(ctx, `UPDATE inventories t SET status = 'INACTIVE' WHERE t.id = $1 AND `+scopePredicate("t.vendor_id", 2), id, hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	case entity.ResourcePolicy:
		result, err = tx.ExecContext(ctx, `UPDATE policies t SET status = 'INACTIVE' WHERE t.id = $1 AND `+scopePredicate("t.vendor_id", 2), id, hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	case entity.ResourcePipeline:
		result, err = tx.ExecContext(ctx, `UPDATE pipelines t SET status = 'INACTIVE' WHERE t.id = $1 AND `+scopePredicate("t.vendor_id", 2)+` AND `+pipelineWriteScope("t", 2), id, hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	case entity.ResourceItem:
		result, err = tx.ExecContext(ctx, `UPDATE items t SET status = 'INACTIVE' WHERE t.id = $1 AND `+itemScopePredicate(2), id, hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs), access.UserID, hasOperationalRole(access.Roles, "WORKER"))
	case entity.ResourcePipelineItem:
		result, err = tx.ExecContext(ctx, `UPDATE pipeline_items t SET status = 'INACTIVE' FROM pipelines p WHERE t.id = $1 AND p.id = t.pipeline_id AND `+scopePredicate("p.vendor_id", 2)+` AND `+pipelineWriteScope("p", 2), id, hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	default:
		return entity.ErrInvalidOperationalResource
	}
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return repository.ErrNotFound
	}
	if err := insertAudit(ctx, tx, access.UserID, "DELETE", string(resource), id, before, map[string]any{"status": "INACTIVE"}, ipAddress); err != nil {
		return err
	}
	return tx.Commit()
}

func operationalQuery(resource entity.OperationalResource, access entity.OperationalAccess) (from, selectSQL, scope string, args []any, err error) {
	args = []any{hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)}
	switch resource {
	case entity.ResourceInventory:
		from = "inventories t"
		selectSQL = operationalSelect("t.id", "t.vendor_id", "NULL::uuid", "NULL::text", "NULL::text", "t.item", "t.jenis_item", "t.qty", "t.img", "t.status_condition", "NULL::text", "NULL::text", "NULL::text", "NULL::integer", "t.status", "t.created_by", "t.created_at", "t.updated_at")
		scope = scopePredicate("t.vendor_id", 1)
	case entity.ResourcePolicy:
		from = "policies t"
		selectSQL = operationalSelect("t.id", "t.vendor_id", "NULL::uuid", "t.name", "NULL::text", "NULL::text", "NULL::text", "NULL::integer", "NULL::text", "NULL::text", "t.file_url", "NULL::text", "NULL::text", "NULL::integer", "t.status", "t.created_by", "t.created_at", "t.updated_at")
		scope = scopePredicate("t.vendor_id", 1)
	case entity.ResourcePipeline:
		from = "pipelines t"
		selectSQL = operationalSelect("t.id", "t.vendor_id", "t.site_id", "t.name", "t.description", "NULL::text", "NULL::text", "NULL::integer", "NULL::text", "NULL::text", "NULL::text", "NULL::text", "NULL::text", "NULL::integer", "t.status", "t.created_by", "t.created_at", "t.updated_at")
		scope = scopePredicate("t.vendor_id", 1) + ` AND (NOT $5::boolean OR EXISTS (
			SELECT 1 FROM pipeline_items pi JOIN pipeline_item_assignments a ON a.pipeline_item_id = pi.id
			JOIN items i ON i.id = pi.item_id
			WHERE pi.pipeline_id = t.id AND pi.status = 'ACTIVE' AND i.status = 'ACTIVE'
				AND t.status = 'ACTIVE' AND a.user_id = $4 AND a.is_active
		))`
		args = append(args, access.UserID, hasOperationalRole(access.Roles, "WORKER"))
	case entity.ResourceItem:
		from = "items t"
		selectSQL = operationalSelect("t.id", "NULL::uuid", "NULL::uuid", "t.name", "t.description", "NULL::text", "NULL::text", "NULL::integer", "NULL::text", "NULL::text", "NULL::text", "NULL::uuid", "t.id", "NULL::integer", "t.status", "t.created_by", "t.created_at", "t.updated_at")
		scope = itemScopePredicate(1)
		args = append(args, access.UserID, hasOperationalRole(access.Roles, "WORKER"))
	case entity.ResourcePipelineItem:
		from = "pipeline_items t JOIN pipelines p ON p.id = t.pipeline_id JOIN items i ON i.id = t.item_id"
		selectSQL = operationalSelect("t.id", "p.vendor_id", "p.site_id", "i.name", "i.description", "NULL::text", "NULL::text", "NULL::integer", "NULL::text", "NULL::text", "NULL::text", "t.pipeline_id", "t.item_id", "t.sort_order", "t.status", "t.created_by", "t.created_at", "t.updated_at")
		scope = scopePredicate("p.vendor_id", 1) + ` AND (NOT $5::boolean OR (
			t.status = 'ACTIVE' AND p.status = 'ACTIVE' AND i.status = 'ACTIVE' AND EXISTS (
				SELECT 1 FROM pipeline_item_assignments a
				WHERE a.pipeline_item_id = t.id AND a.user_id = $4 AND a.is_active
			)
		))`
		args = append(args, access.UserID, hasOperationalRole(access.Roles, "WORKER"))
	default:
		err = entity.ErrInvalidOperationalResource
	}
	return
}

func operationalSelect(columns ...string) string {
	return strings.Join(columns, ", ")
}

func scopePredicate(vendorExpr string, start int) string {
	return fmt.Sprintf(`($%d::boolean OR %s = ANY($%d::uuid[]) OR EXISTS (
		SELECT 1 FROM vendor_sites vs
		WHERE vs.vendor_id = %s AND vs.site_id = ANY($%d::uuid[])
	))`, start, vendorExpr, start+1, vendorExpr, start+2)
}

// pipelineWriteScope narrows writes on a pipeline (or its items) to the
// caller's sites for site-scoped admins; vendor-scoped and super admins pass.
// It expects the same three parameters as scopePredicate starting at start.
func pipelineWriteScope(alias string, start int) string {
	return fmt.Sprintf(`($%d::boolean OR %[2]s.vendor_id = ANY($%d::uuid[]) OR %[2]s.site_id = ANY($%d::uuid[]))`,
		start, alias, start+1, start+2)
}

func itemScopePredicate(start int) string {
	return fmt.Sprintf(`($%d::boolean OR EXISTS (
		SELECT 1 FROM pipeline_items pi JOIN pipelines p ON p.id = pi.pipeline_id
		WHERE pi.item_id = t.id AND (
			p.vendor_id = ANY($%d::uuid[]) OR EXISTS (
				SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = ANY($%d::uuid[])
			)
		) AND (NOT $%d::boolean OR (
			pi.status = 'ACTIVE' AND p.status = 'ACTIVE' AND t.status = 'ACTIVE' AND EXISTS (
			SELECT 1 FROM pipeline_item_assignments a
			WHERE a.pipeline_item_id = pi.id AND a.user_id = $%d AND a.is_active
			)
		))
	))`, start, start+1, start+2, start+4, start+3)
}

func getOperationalTx(ctx context.Context, tx *sql.Tx, access entity.OperationalAccess, resource entity.OperationalResource, id string) (*entity.OperationalRecord, error) {
	from, selectSQL, scope, args, err := operationalQuery(resource, access)
	if err != nil {
		return nil, err
	}
	args = append(args, id)
	record, err := scanOperationalRecord(tx.QueryRowContext(ctx,
		"SELECT "+selectSQL+" FROM "+from+fmt.Sprintf(" WHERE %s AND t.id = $%d", scope, len(args)), args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func scanOperationalRecord(row interface{ Scan(...any) error }) (entity.OperationalRecord, error) {
	var record entity.OperationalRecord
	var vendorID, siteID, name, description, item, itemType, image, condition, fileURL, pipelineID, itemID, status, createdBy sql.NullString
	var quantity, sortOrder sql.NullInt64
	err := row.Scan(
		&record.ID, &vendorID, &siteID, &name, &description, &item, &itemType,
		&quantity, &image, &condition, &fileURL, &pipelineID, &itemID,
		&sortOrder, &status, &createdBy, &record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		return record, err
	}
	record.VendorID = operationalStringPtr(vendorID)
	record.SiteID = operationalStringPtr(siteID)
	record.Name = name.String
	record.Description = operationalStringPtr(description)
	record.Item = item.String
	record.ItemType = operationalStringPtr(itemType)
	record.Image = operationalStringPtr(image)
	record.Condition = condition.String
	record.FileURL = operationalStringPtr(fileURL)
	record.PipelineID = operationalStringPtr(pipelineID)
	record.ItemID = operationalStringPtr(itemID)
	record.Status = status.String
	record.CreatedBy = operationalStringPtr(createdBy)
	if quantity.Valid {
		value := int(quantity.Int64)
		record.Quantity = &value
	}
	if sortOrder.Valid {
		value := int(sortOrder.Int64)
		record.SortOrder = &value
	}
	return record, nil
}

func operationalStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func operationalSearchColumns(resource entity.OperationalResource) string {
	switch resource {
	case entity.ResourceInventory:
		return "t.item, t.jenis_item"
	case entity.ResourcePolicy, entity.ResourcePipeline:
		return "t.name, t.description"
	case entity.ResourceItem:
		return "t.name, t.description"
	case entity.ResourcePipelineItem:
		return "i.name, i.description"
	default:
		return "t.id::text"
	}
}

func hasOperationalRole(roles []string, expected string) bool {
	for _, role := range roles {
		if role == expected {
			return true
		}
	}
	return false
}

func validOperationalStatus(value string) bool {
	return value == "ACTIVE" || value == "INACTIVE"
}

func validInventoryCondition(value string) bool {
	switch value {
	case "GOOD", "DAMAGED", "MAINTENANCE", "LOST":
		return true
	default:
		return false
	}
}

func translateOperationalDBError(err error) error {
	var pgError *pq.Error
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "23503":
			return repository.ErrNotFound
		case "23514", "23502", "22001":
			return entity.ErrInvalidOperationalData
		}
	}
	return translateDBError(err)
}

func uuidString(value string) bool {
	return operationalUUIDPattern.MatchString(value)
}

var operationalUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

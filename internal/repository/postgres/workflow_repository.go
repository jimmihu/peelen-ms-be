package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type WorkflowRepositoryImpl struct {
	db *sql.DB
}

func NewWorkflowRepository(db *sql.DB) repository.WorkflowRepository {
	return &WorkflowRepositoryImpl{db: db}
}

func (r *WorkflowRepositoryImpl) ListSites(ctx context.Context, access entity.OperationalAccess) ([]entity.Site, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id::text, s.code, s.name, s.site_type, s.timezone, s.status, s.created_at, s.updated_at
		FROM sites s
		WHERE ($1::boolean OR s.id = ANY($2::uuid[]) OR EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.site_id = s.id AND vs.vendor_id = ANY($3::uuid[])
		))
		ORDER BY s.code
	`, workflowSuperAdmin(access), pq.Array(access.SiteIDs), pq.Array(access.VendorIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sites := make([]entity.Site, 0)
	for rows.Next() {
		var site entity.Site
		var siteType sql.NullString
		if err := rows.Scan(&site.ID, &site.Code, &site.Name, &siteType, &site.Timezone, &site.Status, &site.CreatedAt, &site.UpdatedAt); err != nil {
			return nil, err
		}
		site.SiteType = nullStringPtr(siteType)
		sites = append(sites, site)
	}
	return sites, rows.Err()
}

func (r *WorkflowRepositoryImpl) GetSite(ctx context.Context, access entity.OperationalAccess, id string) (*entity.Site, error) {
	var site entity.Site
	var siteType sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id::text, s.code, s.name, s.site_type, s.timezone, s.status, s.created_at, s.updated_at
		FROM sites s
		WHERE s.id = $1 AND ($2::boolean OR s.id = ANY($3::uuid[]) OR EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.site_id = s.id AND vs.vendor_id = ANY($4::uuid[])
		))
	`, id, workflowSuperAdmin(access), pq.Array(access.SiteIDs), pq.Array(access.VendorIDs)).
		Scan(&site.ID, &site.Code, &site.Name, &siteType, &site.Timezone, &site.Status, &site.CreatedAt, &site.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	site.SiteType = nullStringPtr(siteType)
	return &site, nil
}

func (r *WorkflowRepositoryImpl) CreateSite(ctx context.Context, site entity.Site) (*entity.Site, error) {
	var result entity.Site
	var siteType sql.NullString
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO sites (code, name, site_type, status, timezone)
		VALUES ($1, $2, $3, COALESCE(NULLIF($4, ''), 'ACTIVE'), COALESCE(NULLIF($5, ''), 'Asia/Jakarta'))
		RETURNING id::text, code, name, site_type, timezone, status, created_at, updated_at
	`, site.Code, site.Name, site.SiteType, site.Status, site.Timezone).
		Scan(&result.ID, &result.Code, &result.Name, &siteType, &result.Timezone, &result.Status, &result.CreatedAt, &result.UpdatedAt)
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	result.SiteType = nullStringPtr(siteType)
	return &result, nil
}

func (r *WorkflowRepositoryImpl) UpdateSite(ctx context.Context, id string, site entity.Site) (*entity.Site, error) {
	var result entity.Site
	var siteType sql.NullString
	err := r.db.QueryRowContext(ctx, `
		UPDATE sites SET code = COALESCE(NULLIF($2, ''), code),
			name = COALESCE(NULLIF($3, ''), name),
			site_type = COALESCE($4, site_type),
			status = COALESCE(NULLIF($5, ''), status),
			timezone = COALESCE(NULLIF($6, ''), timezone)
		WHERE id = $1
		RETURNING id::text, code, name, site_type, timezone, status, created_at, updated_at
	`, id, site.Code, site.Name, site.SiteType, site.Status, site.Timezone).
		Scan(&result.ID, &result.Code, &result.Name, &siteType, &result.Timezone, &result.Status, &result.CreatedAt, &result.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	result.SiteType = nullStringPtr(siteType)
	return &result, nil
}

func (r *WorkflowRepositoryImpl) DeleteSite(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE sites SET status = 'INACTIVE' WHERE id = $1`, id)
	if err != nil {
		return translateOperationalDBError(err)
	}
	return workflowRequireAffected(result)
}

func (r *WorkflowRepositoryImpl) ListVendors(ctx context.Context, access entity.OperationalAccess, filter entity.VendorFilter) ([]entity.Vendor, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT v.id::text, v.code, v.name, v.status,
			ARRAY(SELECT vs.site_id::text FROM vendor_sites vs WHERE vs.vendor_id = v.id ORDER BY vs.site_id)::text,
			v.created_at, v.updated_at
		FROM vendors v
		WHERE ($1::boolean OR v.id = ANY($2::uuid[]) OR EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = v.id AND vs.site_id = ANY($3::uuid[])
		))
		AND ($4 = '' OR EXISTS (SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = v.id AND vs.site_id = NULLIF($4, '')::uuid))
		AND ($5 = '' OR v.status = $5)
		AND ($6 = '' OR concat_ws(' ', v.code, v.name) ILIKE '%' || $6 || '%')
		ORDER BY v.code
	`, workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs),
		filter.SiteID, filter.Status, filter.Query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	vendors := make([]entity.Vendor, 0)
	for rows.Next() {
		var vendor entity.Vendor
		var siteIDs pq.StringArray
		if err := rows.Scan(&vendor.ID, &vendor.Code, &vendor.Name, &vendor.Status, &siteIDs, &vendor.CreatedAt, &vendor.UpdatedAt); err != nil {
			return nil, err
		}
		vendor.SiteIDs = []string(siteIDs)
		vendors = append(vendors, vendor)
	}
	return vendors, rows.Err()
}

func (r *WorkflowRepositoryImpl) GetVendor(ctx context.Context, access entity.OperationalAccess, id string) (*entity.Vendor, error) {
	var vendor entity.Vendor
	var siteIDs pq.StringArray
	err := r.db.QueryRowContext(ctx, `
		SELECT v.id::text, v.code, v.name, v.status,
			ARRAY(SELECT vs.site_id::text FROM vendor_sites vs WHERE vs.vendor_id = v.id ORDER BY vs.site_id)::text,
			v.created_at, v.updated_at
		FROM vendors v
		WHERE v.id = $1 AND ($2::boolean OR v.id = ANY($3::uuid[]) OR EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = v.id AND vs.site_id = ANY($4::uuid[])
		))
	`, id, workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)).
		Scan(&vendor.ID, &vendor.Code, &vendor.Name, &vendor.Status, &siteIDs, &vendor.CreatedAt, &vendor.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	vendor.SiteIDs = []string(siteIDs)
	return &vendor, nil
}

func (r *WorkflowRepositoryImpl) CreateVendor(ctx context.Context, vendor entity.Vendor, siteIDs []string) (*entity.Vendor, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO vendors (code, name, status)
		VALUES ($1, $2, COALESCE(NULLIF($3, ''), 'ACTIVE'))
		RETURNING id::text
	`, vendor.Code, vendor.Name, vendor.Status).Scan(&id); err != nil {
		return nil, translateOperationalDBError(err)
	}
	if err := replaceVendorSites(ctx, tx, id, siteIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetVendor(ctx, superAdminAccess(), id)
}

func (r *WorkflowRepositoryImpl) UpdateVendor(ctx context.Context, id string, vendor entity.Vendor, siteIDs []string) (*entity.Vendor, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE vendors SET code = COALESCE(NULLIF($2, ''), code),
			name = COALESCE(NULLIF($3, ''), name),
			status = COALESCE(NULLIF($4, ''), status)
		WHERE id = $1
	`, id, vendor.Code, vendor.Name, vendor.Status)
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	if err := workflowRequireAffected(result); err != nil {
		return nil, err
	}
	if siteIDs != nil {
		if err := replaceVendorSites(ctx, tx, id, siteIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetVendor(ctx, superAdminAccess(), id)
}

func (r *WorkflowRepositoryImpl) DeleteVendor(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE vendors SET status = 'INACTIVE' WHERE id = $1`, id)
	if err != nil {
		return translateOperationalDBError(err)
	}
	return workflowRequireAffected(result)
}

func replaceVendorSites(ctx context.Context, tx *sql.Tx, vendorID string, siteIDs []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM vendor_sites WHERE vendor_id = $1`, vendorID); err != nil {
		return err
	}
	for _, siteID := range siteIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO vendor_sites (vendor_id, site_id) VALUES ($1, $2)`, vendorID, siteID); err != nil {
			return translateOperationalDBError(err)
		}
	}
	// A site still used by active pipelines cannot be removed from the vendor.
	var orphaned bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pipelines p
			WHERE p.vendor_id = $1 AND p.status = 'ACTIVE'
				AND NOT EXISTS (SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = p.site_id)
		)
	`, vendorID).Scan(&orphaned); err != nil {
		return err
	}
	if orphaned {
		return repository.ErrConflict
	}
	return nil
}

func (r *WorkflowRepositoryImpl) ListShifts(ctx context.Context) ([]entity.Shift, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT shift, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
		FROM shifts
		ORDER BY CASE shift WHEN 'DAY' THEN 1 WHEN 'EVENING' THEN 2 ELSE 3 END
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shifts := make([]entity.Shift, 0, 3)
	for rows.Next() {
		var shift entity.Shift
		if err := rows.Scan(&shift.Shift, &shift.StartTime, &shift.EndTime); err != nil {
			return nil, err
		}
		shifts = append(shifts, shift)
	}
	return shifts, rows.Err()
}

func (r *WorkflowRepositoryImpl) GetShift(ctx context.Context, name string) (*entity.Shift, error) {
	var shift entity.Shift
	err := r.db.QueryRowContext(ctx, `
		SELECT shift, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
		FROM shifts WHERE shift = $1
	`, name).Scan(&shift.Shift, &shift.StartTime, &shift.EndTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &shift, nil
}

func (r *WorkflowRepositoryImpl) CreateShift(ctx context.Context, shift entity.Shift) (*entity.Shift, error) {
	var result entity.Shift
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO shifts (shift, start_time, end_time)
		VALUES ($1, $2::time, $3::time)
		RETURNING shift, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
	`, shift.Shift, shift.StartTime, shift.EndTime).Scan(&result.Shift, &result.StartTime, &result.EndTime)
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	return &result, nil
}

func (r *WorkflowRepositoryImpl) UpdateShift(ctx context.Context, name string, shift entity.Shift) (*entity.Shift, error) {
	var result entity.Shift
	err := r.db.QueryRowContext(ctx, `
		UPDATE shifts SET start_time = $2::time, end_time = $3::time
		WHERE shift = $1
		RETURNING shift, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
	`, name, shift.StartTime, shift.EndTime).Scan(&result.Shift, &result.StartTime, &result.EndTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	return &result, nil
}

func (r *WorkflowRepositoryImpl) DeleteShift(ctx context.Context, name string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM shifts WHERE shift = $1`, name)
	if err != nil {
		var pgError *pq.Error
		if errors.As(err, &pgError) && pgError.Code == "23503" {
			return repository.ErrConflict
		}
		return translateOperationalDBError(err)
	}
	return workflowRequireAffected(result)
}

// nullDate sends a zero time as SQL NULL.
func nullDate(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (r *WorkflowRepositoryImpl) ListMyTasks(ctx context.Context, access entity.OperationalAccess, reportDate time.Time) ([]entity.MyTask, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id::text, pi.id::text, p.id::text, p.name, i.id::text, i.name,
			p.vendor_id::text, st.id::text, st.name, st.timezone,
			pr.shift, to_char(sh.start_time, 'HH24:MI'), to_char(sh.end_time, 'HH24:MI'),
			to_char(day.d, 'YYYY-MM-DD'), pr.id::text, pr.status, pr.status_revisi
		FROM pipeline_item_assignments a
		JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
		JOIN pipelines p ON p.id = pi.pipeline_id
		JOIN sites st ON st.id = p.site_id
		-- No date asked: "today" at the task's site.
		CROSS JOIN LATERAL (SELECT coalesce($2::date, (now() AT TIME ZONE st.timezone)::date) AS d) day
		JOIN items i ON i.id = pi.item_id
		LEFT JOIN pipeline_reports pr ON pr.pipeline_item_assignment_id = a.id AND pr.report_date = day.d
		LEFT JOIN shifts sh ON sh.shift = pr.shift
		WHERE a.user_id = $1 AND a.is_active AND pi.status = 'ACTIVE' AND p.status = 'ACTIVE' AND i.status = 'ACTIVE'
		ORDER BY p.name, pi.sort_order, i.name, pr.shift
	`, access.UserID, nullDate(reportDate))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]entity.MyTask, 0)
	for rows.Next() {
		var task entity.MyTask
		var shift, shiftStart, shiftEnd, reportID, reportStatus, statusRevisi sql.NullString
		if err := rows.Scan(&task.AssignmentID, &task.PipelineItemID, &task.PipelineID, &task.PipelineName,
			&task.ItemID, &task.ItemName, &task.VendorID, &task.SiteID, &task.SiteName, &task.SiteTimezone,
			&shift, &shiftStart, &shiftEnd, &task.ReportDate, &reportID, &reportStatus, &statusRevisi); err != nil {
			return nil, err
		}
		task.Shift = nullStringPtr(shift)
		task.ShiftStart = nullStringPtr(shiftStart)
		task.ShiftEnd = nullStringPtr(shiftEnd)
		task.ReportID = nullStringPtr(reportID)
		task.ReportStatus = nullStringPtr(reportStatus)
		task.StatusRevisi = nullStringPtr(statusRevisi)
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (r *WorkflowRepositoryImpl) ListPipelineReports(ctx context.Context, access entity.OperationalAccess, filter entity.ReportFilter) ([]entity.PipelineReport, int, error) {
	where, args := pipelineReportScope(access)
	// A pipeline belongs to one site: filter on it, not on every site of the vendor.
	siteID := filter.SiteID
	filter.SiteID = ""
	appendReportFilters(&where, &args, filter, "p.vendor_id", "a.user_id", "pr.status", "pr.report_date")
	if siteID != "" {
		args = append(args, siteID)
		where = append(where, fmt.Sprintf("p.site_id = $%d::uuid", len(args)))
	}
	switch filter.NoteCategory {
	case "":
	case "ANY":
		where = append(where, "d.note_category IS NOT NULL")
	default:
		args = append(args, filter.NoteCategory)
		where = append(where, fmt.Sprintf("d.note_category = $%d", len(args)))
	}
	return queryPipelineReports(ctx, r.db, strings.Join(where, " AND "), args, filter.Page, filter.Limit)
}

func (r *WorkflowRepositoryImpl) GetPipelineReport(ctx context.Context, access entity.OperationalAccess, id string) (*entity.PipelineReport, error) {
	where, args := pipelineReportScope(access)
	args = append(args, id)
	reports, _, err := queryPipelineReports(ctx, r.db, strings.Join(where, " AND ")+fmt.Sprintf(" AND pr.id = $%d", len(args)), args, 1, 1)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, repository.ErrNotFound
	}
	return &reports[0], nil
}

// Report timing, relative to the report's shift (alias sh) on dateExpr, in the
// site's time zone tzExpr (an IANA name): the shift ends at its end time (next
// day when it crosses midnight), and a Worker may still upload until the same
// shift starts the next day. Both return instants (timestamptz).
func reportShiftEndSQL(dateExpr, tzExpr string) string {
	return fmt.Sprintf(`(((%s + sh.end_time) + CASE WHEN sh.end_time <= sh.start_time THEN interval '1 day' ELSE interval '0' END) AT TIME ZONE %s)`, dateExpr, tzExpr)
}

func reportUploadCloseSQL(dateExpr, tzExpr string) string {
	return fmt.Sprintf(`((%s + sh.start_time + interval '1 day') AT TIME ZONE %s)`, dateExpr, tzExpr)
}

// siteTimezoneOfReportSQL is the time zone of the site where report $1 is worked.
const siteTimezoneOfReportSQL = `(SELECT st.timezone FROM pipeline_reports r
	JOIN pipeline_item_assignments ra ON ra.id = r.pipeline_item_assignment_id
	JOIN pipeline_items rpi ON rpi.id = ra.pipeline_item_id
	JOIN pipelines rp ON rp.id = rpi.pipeline_id
	JOIN sites st ON st.id = rp.site_id
	WHERE r.id = $1)`

// checkReportWorkTimes rejects open/closed times (from photo EXIF or the
// client) earlier than an hour before the shift starts or later than now, so
// an old photo cannot stand in for today's work.
func checkReportWorkTimes(ctx context.Context, tx *sql.Tx, reportID string) error {
	var valid bool
	if err := tx.QueryRowContext(ctx, `
		SELECT coalesce(bool_and(t IS NULL OR (
			t >= ((pr.report_date + sh.start_time) - interval '1 hour') AT TIME ZONE st.timezone
			AND t <= now() + interval '5 minutes'
		)), true)
		FROM pipeline_reports pr
		JOIN shifts sh ON sh.shift = pr.shift
		JOIN pipeline_item_assignments a ON a.id = pr.pipeline_item_assignment_id
		JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
		JOIN pipelines p ON p.id = pi.pipeline_id
		JOIN sites st ON st.id = p.site_id
		JOIN pipeline_report_details d ON d.pipeline_report_id = pr.id
		CROSS JOIN LATERAL (VALUES (d.open_date), (d.closed_date)) AS times(t)
		WHERE pr.id = $1
	`, reportID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: photo/work time must be between 1 hour before the shift starts and now", entity.ErrInvalidOperationalData)
	}
	return nil
}

func (r *WorkflowRepositoryImpl) CreatePipelineReport(ctx context.Context, access entity.OperationalAccess, input entity.PipelineReportInput) (*entity.PipelineReport, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// The site's local clock decides "today" and the running shift.
	var timezone, localToday string
	var currentShift sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT st.timezone, to_char((now() AT TIME ZONE st.timezone)::date, 'YYYY-MM-DD'),
			(SELECT sh.shift FROM shifts sh
			 CROSS JOIN LATERAL (SELECT (now() AT TIME ZONE st.timezone)::time AS t) local
			 WHERE CASE WHEN sh.start_time < sh.end_time
			            THEN local.t >= sh.start_time AND local.t < sh.end_time
			            ELSE local.t >= sh.start_time OR local.t < sh.end_time END
			 ORDER BY sh.start_time LIMIT 1)
		FROM pipeline_items pi
		JOIN pipelines p ON p.id = pi.pipeline_id
		JOIN sites st ON st.id = p.site_id
		WHERE pi.id = $1
	`, input.PipelineItemID).Scan(&timezone, &localToday, &currentShift); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	reportDate := input.ReportDate
	if reportDate == "" {
		reportDate = localToday
	}
	shift := input.Shift
	if shift == "" {
		// Only the running shift of today can be inferred; any other day needs it explicitly.
		if reportDate != localToday || !currentShift.Valid {
			return nil, fmt.Errorf("%w: shift is required for a report date other than today at the site", entity.ErrInvalidOperationalData)
		}
		shift = currentShift.String
	}
	var locked bool
	if err := tx.QueryRowContext(ctx, `SELECT now() >= `+reportUploadCloseSQL("$2::date", "$3")+` FROM shifts sh WHERE sh.shift = $1`,
		shift, reportDate, timezone).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	if locked {
		return nil, entity.ErrReportUploadClosed
	}
	status := input.Status
	if status == "" {
		status = "IN_PROGRESS"
	}
	var reportID, revision string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO pipeline_reports (pipeline_item_assignment_id, shift, report_date, status, status_revisi)
		SELECT a.id, sh.shift, $4::date, $5,
			CASE WHEN now() > `+reportShiftEndSQL("$4::date", "st.timezone")+` THEN 'SUBMITTED' ELSE 'NONE' END
		FROM pipeline_item_assignments a
		JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
		JOIN pipelines p ON p.id = pi.pipeline_id
		JOIN sites st ON st.id = p.site_id
		JOIN shifts sh ON sh.shift = $3
		WHERE pi.id = $1 AND a.user_id = $2 AND a.is_active
			AND pi.status = 'ACTIVE' AND p.status = 'ACTIVE'
		RETURNING id::text, status_revisi
	`, input.PipelineItemID, access.UserID, shift, reportDate, status).Scan(&reportID, &revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, translateOperationalDBError(err)
	}
	if err := writePipelineReportDetails(ctx, tx, reportID, input); err != nil {
		return nil, err
	}
	if err := checkReportWorkTimes(ctx, tx, reportID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pipeline_report_history (pipeline_report_id, actor_id, new_status, new_revision, reason)
		VALUES ($1, $2, $3, $4, $5)
	`, reportID, access.UserID, status, revision, input.Reason); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, translateOperationalDBError(err)
	}
	return r.GetPipelineReport(ctx, access, reportID)
}

// UpdatePipelineReport is Worker-only: the assigned Worker fills in details and
// completes the report. Changes after the shift ends mark status_revisi
// SUBMITTED (late upload, e.g. no signal on site); no manager review applies.
func (r *WorkflowRepositoryImpl) UpdatePipelineReport(ctx context.Context, access entity.OperationalAccess, id string, input entity.PipelineReportInput) (*entity.PipelineReport, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var previousStatus, previousRevision string
	var afterShift, locked bool
	if err := tx.QueryRowContext(ctx, `
		SELECT pr.status, pr.status_revisi,
			now() > `+reportShiftEndSQL("pr.report_date", "st.timezone")+`,
			now() >= `+reportUploadCloseSQL("pr.report_date", "st.timezone")+`
		FROM pipeline_reports pr
		JOIN pipeline_item_assignments a ON a.id = pr.pipeline_item_assignment_id
		JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
		JOIN pipelines p ON p.id = pi.pipeline_id
		JOIN sites st ON st.id = p.site_id
		JOIN shifts sh ON sh.shift = pr.shift
		WHERE pr.id = $1 AND a.user_id = $2 AND a.is_active
		FOR UPDATE OF pr
	`, id, access.UserID).Scan(&previousStatus, &previousRevision, &afterShift, &locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	if locked {
		return nil, entity.ErrReportUploadClosed
	}
	status := input.Status
	if status == "" {
		status = previousStatus
	}
	if previousStatus == "COMPLETED" && status != "COMPLETED" {
		return nil, fmt.Errorf("%w: a completed report cannot be reopened", entity.ErrInvalidOperationalData)
	}
	revision := previousRevision
	if afterShift {
		revision = "SUBMITTED"
	}
	if err := writePipelineReportDetails(ctx, tx, id, input); err != nil {
		return nil, err
	}
	if err := checkReportWorkTimes(ctx, tx, id); err != nil {
		return nil, err
	}
	if status == "COMPLETED" {
		var complete bool
		if err := tx.QueryRowContext(ctx, `
			SELECT evidence_after IS NOT NULL AND closed_date IS NOT NULL
			FROM pipeline_report_details WHERE pipeline_report_id = $1
		`, id).Scan(&complete); err != nil {
			return nil, err
		}
		if !complete {
			return nil, fmt.Errorf("%w: evidence_after and closed_date are required to complete a report", entity.ErrInvalidOperationalData)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pipeline_reports SET status = $2, status_revisi = $3, updated_at = now()
		WHERE id = $1
	`, id, status, revision); err != nil {
		return nil, translateOperationalDBError(err)
	}
	if previousStatus != status || previousRevision != revision {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pipeline_report_history
				(pipeline_report_id, actor_id, previous_status, new_status, previous_revision, new_revision, reason)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, id, access.UserID, previousStatus, status, previousRevision, revision, input.Reason); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPipelineReport(ctx, access, id)
}

// RevisePipelineReport archives the current details of the Worker's completed
// report as version revision_count, then writes the revision's new photos and
// times (notes and category are kept unless given). The report stays
// COMPLETED; a revision after the shift ends marks it SUBMITTED (late upload),
// like any change after the shift.
func (r *WorkflowRepositoryImpl) RevisePipelineReport(ctx context.Context, access entity.OperationalAccess, id string, input entity.PipelineReportInput) (*entity.PipelineReport, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status, previousRevision string
	var revisionCount int
	var afterShift, locked bool
	if err := tx.QueryRowContext(ctx, `
		SELECT pr.status, pr.status_revisi, pr.revision_count,
			now() > `+reportShiftEndSQL("pr.report_date", "st.timezone")+`,
			now() >= `+reportUploadCloseSQL("pr.report_date", "st.timezone")+`
		FROM pipeline_reports pr
		JOIN pipeline_item_assignments a ON a.id = pr.pipeline_item_assignment_id
		JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
		JOIN pipelines p ON p.id = pi.pipeline_id
		JOIN sites st ON st.id = p.site_id
		JOIN shifts sh ON sh.shift = pr.shift
		WHERE pr.id = $1 AND a.user_id = $2 AND a.is_active
		FOR UPDATE OF pr
	`, id, access.UserID).Scan(&status, &previousRevision, &revisionCount, &afterShift, &locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	if locked {
		return nil, entity.ErrReportUploadClosed
	}
	if status != "COMPLETED" {
		return nil, fmt.Errorf("%w: only a completed report can be revised", entity.ErrInvalidOperationalData)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pipeline_report_revisions
			(pipeline_report_id, revision_no, evidence_before, evidence_after, open_date, closed_date,
			 note_category, notes, revised_by, reason)
		SELECT d.pipeline_report_id, $2, d.evidence_before, d.evidence_after, d.open_date, d.closed_date,
			d.note_category, d.notes, $3, $4
		FROM pipeline_report_details d WHERE d.pipeline_report_id = $1
	`, id, revisionCount, access.UserID, input.Reason); err != nil {
		return nil, translateOperationalDBError(err)
	}
	// The revision's times come only from its new photos (or the client).
	if _, err := tx.ExecContext(ctx, `
		UPDATE pipeline_report_details SET open_date = NULL, closed_date = NULL WHERE pipeline_report_id = $1
	`, id); err != nil {
		return nil, err
	}
	if err := writePipelineReportDetails(ctx, tx, id, input); err != nil {
		return nil, err
	}
	if err := checkReportWorkTimes(ctx, tx, id); err != nil {
		return nil, err
	}
	revision := previousRevision
	if afterShift {
		revision = "SUBMITTED"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pipeline_reports SET revision_count = revision_count + 1, status_revisi = $2, updated_at = now()
		WHERE id = $1
	`, id, revision); err != nil {
		return nil, translateOperationalDBError(err)
	}
	reason := fmt.Sprintf("Revisi ke-%d", revisionCount+1)
	if input.Reason != nil && strings.TrimSpace(*input.Reason) != "" {
		reason += ": " + strings.TrimSpace(*input.Reason)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pipeline_report_history
			(pipeline_report_id, actor_id, previous_status, new_status, previous_revision, new_revision, reason)
		VALUES ($1, $2, $3, $3, $4, $5, $6)
	`, id, access.UserID, status, previousRevision, revision, reason); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetPipelineReport(ctx, access, id)
}

// ListPipelineReportRevisions returns the archived versions of a report, oldest first.
func (r *WorkflowRepositoryImpl) ListPipelineReportRevisions(ctx context.Context, reportID string) ([]entity.PipelineReportRevision, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT rv.id::text, rv.revision_no, rv.evidence_before, rv.evidence_after, rv.open_date, rv.closed_date,
			rv.note_category, rv.notes, rv.revised_by::text, coalesce(u.full_name, u.username), rv.reason, rv.created_at
		FROM pipeline_report_revisions rv
		LEFT JOIN users u ON u.id = rv.revised_by
		WHERE rv.pipeline_report_id = $1
		ORDER BY rv.revision_no`, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	revisions := make([]entity.PipelineReportRevision, 0)
	for rows.Next() {
		var rv entity.PipelineReportRevision
		var before, after, category, notes, revisedBy, revisedByName, reason sql.NullString
		var openDate, closedDate sql.NullTime
		if err := rows.Scan(&rv.ID, &rv.RevisionNo, &before, &after, &openDate, &closedDate,
			&category, &notes, &revisedBy, &revisedByName, &reason, &rv.CreatedAt); err != nil {
			return nil, err
		}
		rv.EvidenceBefore, rv.EvidenceAfter = nullStringPtr(before), nullStringPtr(after)
		rv.OpenDate, rv.ClosedDate = nullTimePtr(openDate), nullTimePtr(closedDate)
		rv.NoteCategory, rv.Notes = nullStringPtr(category), nullStringPtr(notes)
		rv.RevisedBy, rv.RevisedByName, rv.Reason = nullStringPtr(revisedBy), nullStringPtr(revisedByName), nullStringPtr(reason)
		revisions = append(revisions, rv)
	}
	return revisions, rows.Err()
}

func writePipelineReportDetails(ctx context.Context, tx *sql.Tx, reportID string, input entity.PipelineReportInput) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO pipeline_report_details
			(pipeline_report_id, evidence_before, evidence_after, open_date, closed_date, note_category, notes)
		VALUES ($1, $2, $3,
			-- Times without an offset (photo EXIF) are wall-clock times at the site.
			COALESCE($4::timestamptz, $8::timestamp AT TIME ZONE `+siteTimezoneOfReportSQL+`),
			COALESCE($5::timestamptz, $9::timestamp AT TIME ZONE `+siteTimezoneOfReportSQL+`),
			$6, $7)
		ON CONFLICT (pipeline_report_id) DO UPDATE SET
			evidence_before = COALESCE(EXCLUDED.evidence_before, pipeline_report_details.evidence_before),
			evidence_after = COALESCE(EXCLUDED.evidence_after, pipeline_report_details.evidence_after),
			open_date = COALESCE(EXCLUDED.open_date, pipeline_report_details.open_date),
			closed_date = COALESCE(EXCLUDED.closed_date, pipeline_report_details.closed_date),
			note_category = COALESCE(EXCLUDED.note_category, pipeline_report_details.note_category),
			notes = COALESCE(EXCLUDED.notes, pipeline_report_details.notes),
			updated_at = now()
	`, reportID, input.EvidenceBefore, input.EvidenceAfter, input.OpenDate, input.ClosedDate, input.NoteCategory, input.Notes,
		input.OpenDateLocal, input.ClosedDateLocal)
	return err
}

func (r *WorkflowRepositoryImpl) ListInventoryReports(ctx context.Context, access entity.OperationalAccess, filter entity.ReportFilter) ([]entity.InventoryReport, int, error) {
	where := []string{`($1::boolean OR ir.vendor_id = ANY($2::uuid[]) OR EXISTS (
		SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = ir.vendor_id AND vs.site_id = ANY($3::uuid[])
	))`}
	args := []any{workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)}
	if hasWorkflowRole(access.Roles, "WORKER") && !hasWorkflowRole(access.Roles, "ADMIN_WORKER", "ADMIN", "SUPER_ADMIN") {
		where = append(where, fmt.Sprintf("ir.user_id = $%d", len(args)+1))
		args = append(args, access.UserID)
	}
	appendReportFilters(&where, &args, filter, "ir.vendor_id", "ir.user_id", "ir.status", "ir.created_at")
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM inventory_reports ir JOIN users u ON u.id = ir.user_id WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.Limit, (filter.Page-1)*filter.Limit)
	rows, err := r.db.QueryContext(ctx, `
		SELECT ir.id::text, ir.vendor_id::text, ir.inventory_id::text, ir.user_id::text, u.username,
			ir.report_type, ir.evidence, ir.description, ir.status, ir.review_reason,
			ir.reviewed_by::text, ir.created_at, ir.updated_at
		FROM inventory_reports ir JOIN users u ON u.id = ir.user_id
		WHERE `+whereSQL+fmt.Sprintf(" ORDER BY ir.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]entity.InventoryReport, 0)
	for rows.Next() {
		report, err := scanInventoryReport(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, report)
	}
	return result, total, rows.Err()
}

func (r *WorkflowRepositoryImpl) CreateInventoryReport(ctx context.Context, access entity.OperationalAccess, input entity.InventoryReportInput) (*entity.InventoryReport, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO inventory_reports (vendor_id, inventory_id, user_id, report_type, evidence, description)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE ($7::boolean OR $1 = ANY($8::uuid[]) OR EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = $1 AND vs.site_id = ANY($9::uuid[])
		))
		RETURNING id::text
	`, input.VendorID, input.InventoryID, access.UserID, input.ReportType, input.Evidence, input.Description,
		workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	return r.getInventoryReport(ctx, access, id)
}

func (r *WorkflowRepositoryImpl) ReviewInventoryReport(ctx context.Context, access entity.OperationalAccess, id, status string, reason *string) (*entity.InventoryReport, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE inventory_reports ir SET status = $2, review_reason = $3, reviewed_by = $4, updated_at = now()
		WHERE ir.id = $1 AND ($5::boolean OR ir.vendor_id = ANY($6::uuid[]) OR EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = ir.vendor_id AND vs.site_id = ANY($7::uuid[])
		))
	`, id, status, reason, access.UserID, workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs))
	if err != nil {
		return nil, translateOperationalDBError(err)
	}
	if err := workflowRequireAffected(result); err != nil {
		return nil, err
	}
	return r.getInventoryReport(ctx, access, id)
}

func (r *WorkflowRepositoryImpl) getInventoryReport(ctx context.Context, access entity.OperationalAccess, id string) (*entity.InventoryReport, error) {
	where := `ir.id = $1 AND ($2::boolean OR ir.vendor_id = ANY($3::uuid[]) OR EXISTS (
		SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = ir.vendor_id AND vs.site_id = ANY($4::uuid[])
	))`
	if hasWorkflowRole(access.Roles, "WORKER") && !hasWorkflowRole(access.Roles, "ADMIN_WORKER", "ADMIN", "SUPER_ADMIN") {
		where += " AND ir.user_id = $5"
	}
	args := []any{id, workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)}
	if strings.Contains(where, "ir.user_id = $5") {
		args = append(args, access.UserID)
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT ir.id::text, ir.vendor_id::text, ir.inventory_id::text, ir.user_id::text, u.username,
			ir.report_type, ir.evidence, ir.description, ir.status, ir.review_reason,
			ir.reviewed_by::text, ir.created_at, ir.updated_at
		FROM inventory_reports ir JOIN users u ON u.id = ir.user_id WHERE `+where, args...)
	report, err := scanInventoryReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &report, nil
}

func (r *WorkflowRepositoryImpl) GetInventoryReport(ctx context.Context, access entity.OperationalAccess, id string) (*entity.InventoryReport, error) {
	return r.getInventoryReport(ctx, access, id)
}

func (r *WorkflowRepositoryImpl) ReportDashboard(ctx context.Context, access entity.OperationalAccess, date time.Time, siteID, vendorID string) (map[string]any, error) {
	var pipelineTotal, pipelineInProgress, pipelineCompleted, pipelineLateUpload int
	pipelineScope := `($1::boolean OR p.vendor_id = ANY($2::uuid[]) OR EXISTS (
		SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = ANY($3::uuid[])
	))`
	pipelineQuery := `SELECT count(*),
		count(*) FILTER (WHERE pr.status = 'IN_PROGRESS'),
		count(*) FILTER (WHERE pr.status = 'COMPLETED'),
		count(*) FILTER (WHERE pr.status_revisi = 'SUBMITTED')
		FROM pipeline_reports pr
		JOIN pipeline_item_assignments a ON a.id = pr.pipeline_item_assignment_id
		JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
		JOIN pipelines p ON p.id = pi.pipeline_id
		WHERE pr.report_date = $4::date AND ` + pipelineScope
	args := []any{workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs), date}
	if siteID != "" {
		pipelineQuery += ` AND EXISTS (SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = $5)`
		args = append(args, siteID)
	}
	if vendorID != "" {
		pipelineQuery += fmt.Sprintf(" AND p.vendor_id = $%d", len(args)+1)
		args = append(args, vendorID)
	}
	if hasWorkflowRole(access.Roles, "WORKER") && !hasWorkflowRole(access.Roles, "ADMIN_WORKER", "ADMIN", "SUPER_ADMIN") {
		pipelineQuery += fmt.Sprintf(" AND a.user_id = $%d", len(args)+1)
		args = append(args, access.UserID)
	}
	if err := r.db.QueryRowContext(ctx, pipelineQuery, args...).Scan(&pipelineTotal, &pipelineInProgress, &pipelineCompleted, &pipelineLateUpload); err != nil {
		return nil, err
	}

	var inventoryTotal, inventorySubmitted, inventoryInReview, inventoryResolved int
	inventoryQuery := `SELECT count(*),
		count(*) FILTER (WHERE ir.status = 'SUBMITTED'),
		count(*) FILTER (WHERE ir.status = 'IN_REVIEW'),
		count(*) FILTER (WHERE ir.status = 'RESOLVED')
		FROM inventory_reports ir
		WHERE ir.created_at::date = $4::date AND ($1::boolean OR ir.vendor_id = ANY($2::uuid[]) OR EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = ir.vendor_id AND vs.site_id = ANY($3::uuid[])
		))`
	inventoryArgs := []any{workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs), date}
	if siteID != "" {
		inventoryQuery += ` AND EXISTS (SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = ir.vendor_id AND vs.site_id = $5)`
		inventoryArgs = append(inventoryArgs, siteID)
	}
	if vendorID != "" {
		inventoryQuery += fmt.Sprintf(" AND ir.vendor_id = $%d", len(inventoryArgs)+1)
		inventoryArgs = append(inventoryArgs, vendorID)
	}
	if hasWorkflowRole(access.Roles, "WORKER") && !hasWorkflowRole(access.Roles, "ADMIN_WORKER", "ADMIN", "SUPER_ADMIN") {
		inventoryQuery += fmt.Sprintf(" AND ir.user_id = $%d", len(inventoryArgs)+1)
		inventoryArgs = append(inventoryArgs, access.UserID)
	}
	if err := r.db.QueryRowContext(ctx, inventoryQuery, inventoryArgs...).Scan(&inventoryTotal, &inventorySubmitted, &inventoryInReview, &inventoryResolved); err != nil {
		return nil, err
	}
	return map[string]any{
		"date": date.Format("2006-01-02"),
		"pipeline_reports": map[string]int{
			"total": pipelineTotal, "in_progress": pipelineInProgress, "completed": pipelineCompleted,
			"late_upload": pipelineLateUpload,
		},
		"inventory_reports": map[string]int{
			"total": inventoryTotal, "submitted": inventorySubmitted, "in_review": inventoryInReview, "resolved": inventoryResolved,
		},
	}, nil
}

func (r *WorkflowRepositoryImpl) DailyReport(ctx context.Context, access entity.OperationalAccess, date time.Time, siteID, vendorID string) ([]map[string]any, error) {
	query := `SELECT p.vendor_id::text, v.name, p.id::text, p.name,
		count(DISTINCT pi.id) AS task_count,
		count(DISTINCT pr.id) AS report_count,
		count(DISTINCT pr.id) FILTER (WHERE pr.status = 'COMPLETED') AS completed_count,
		count(DISTINCT pr.id) FILTER (WHERE pr.status = 'IN_PROGRESS') AS in_progress_count,
		count(DISTINCT pr.id) FILTER (WHERE pr.status_revisi = 'SUBMITTED') AS late_upload_count
		FROM pipelines p JOIN vendors v ON v.id = p.vendor_id
		JOIN pipeline_items pi ON pi.pipeline_id = p.id AND pi.status = 'ACTIVE'
		LEFT JOIN pipeline_item_assignments a ON a.pipeline_item_id = pi.id AND a.is_active
		LEFT JOIN pipeline_reports pr ON pr.pipeline_item_assignment_id = a.id AND pr.report_date = $4::date
		WHERE p.status = 'ACTIVE' AND ($1::boolean OR p.vendor_id = ANY($2::uuid[]) OR EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = ANY($3::uuid[])
		))`
	args := []any{workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs), date}
	if siteID != "" {
		query += ` AND EXISTS (SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = $5)`
		args = append(args, siteID)
	}
	if vendorID != "" {
		query += fmt.Sprintf(" AND p.vendor_id = $%d", len(args)+1)
		args = append(args, vendorID)
	}
	if hasWorkflowRole(access.Roles, "WORKER") && !hasWorkflowRole(access.Roles, "ADMIN_WORKER", "ADMIN", "SUPER_ADMIN") {
		query += fmt.Sprintf(" AND a.user_id = $%d", len(args)+1)
		args = append(args, access.UserID)
	}
	query += ` GROUP BY p.vendor_id, v.name, p.id, p.name ORDER BY v.name, p.name`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var vendorID, vendorName, pipelineID, pipelineName string
		var tasks, reports, completed, inProgress, lateUpload int
		if err := rows.Scan(&vendorID, &vendorName, &pipelineID, &pipelineName, &tasks, &reports, &completed, &inProgress, &lateUpload); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"vendor_id": vendorID, "vendor_name": vendorName, "pipeline_id": pipelineID, "pipeline_name": pipelineName,
			"task_count": tasks, "report_count": reports, "completed_count": completed,
			"in_progress_count": inProgress, "late_upload_count": lateUpload,
		})
	}
	return result, rows.Err()
}

func queryPipelineReports(ctx context.Context, db *sql.DB, whereSQL string, args []any, page, limit int) ([]entity.PipelineReport, int, error) {
	from := `pipeline_reports pr
		JOIN pipeline_item_assignments a ON a.id = pr.pipeline_item_assignment_id
		JOIN users u ON u.id = a.user_id
		JOIN pipeline_items pi ON pi.id = a.pipeline_item_id
		JOIN pipelines p ON p.id = pi.pipeline_id
		JOIN sites rs ON rs.id = p.site_id
		JOIN items i ON i.id = pi.item_id
		LEFT JOIN pipeline_report_details d ON d.pipeline_report_id = pr.id`
	var total int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+from+" WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := db.QueryContext(ctx, `
		SELECT pr.id::text, a.id::text, pi.id::text, p.id::text, i.id::text, i.name, p.vendor_id::text,
			u.id::text, u.username, rs.id::text, rs.name, rs.timezone, pr.shift, to_char(pr.report_date, 'YYYY-MM-DD'),
			pr.status, pr.status_revisi, pr.revision_count, d.evidence_before, d.evidence_after,
			d.open_date, d.closed_date, d.note_category, d.notes, pr.created_at, pr.updated_at
		FROM `+from+" WHERE "+whereSQL+
		fmt.Sprintf(" ORDER BY pr.report_date DESC, pr.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]entity.PipelineReport, 0)
	for rows.Next() {
		report, err := scanPipelineReport(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, report)
	}
	return result, total, rows.Err()
}

func scanPipelineReport(row interface{ Scan(...any) error }) (entity.PipelineReport, error) {
	var report entity.PipelineReport
	var evidenceBefore, evidenceAfter, noteCategory, notes sql.NullString
	var openDate, closedDate sql.NullTime
	err := row.Scan(&report.ID, &report.PipelineItemAssignmentID, &report.PipelineItemID, &report.PipelineID, &report.ItemID,
		&report.ItemName, &report.VendorID, &report.UserID, &report.Username,
		&report.SiteID, &report.SiteName, &report.SiteTimezone, &report.Shift,
		&report.ReportDate, &report.Status, &report.StatusRevisi, &report.RevisionCount, &evidenceBefore, &evidenceAfter,
		&openDate, &closedDate, &noteCategory, &notes, &report.CreatedAt, &report.UpdatedAt)
	if err != nil {
		return entity.PipelineReport{}, err
	}
	report.EvidenceBefore = nullStringPtr(evidenceBefore)
	report.EvidenceAfter = nullStringPtr(evidenceAfter)
	report.NoteCategory = nullStringPtr(noteCategory)
	report.Notes = nullStringPtr(notes)
	report.OpenDate = nullTimePtr(openDate)
	report.ClosedDate = nullTimePtr(closedDate)
	return report, nil
}

func scanInventoryReport(row interface{ Scan(...any) error }) (entity.InventoryReport, error) {
	var report entity.InventoryReport
	var inventoryID, evidence, reviewReason, reviewedBy sql.NullString
	err := row.Scan(&report.ID, &report.VendorID, &inventoryID, &report.UserID, &report.Username,
		&report.ReportType, &evidence, &report.Description, &report.Status, &reviewReason,
		&reviewedBy, &report.CreatedAt, &report.UpdatedAt)
	if err != nil {
		return entity.InventoryReport{}, err
	}
	report.InventoryID = nullStringPtr(inventoryID)
	report.Evidence = nullStringPtr(evidence)
	report.ReviewReason = nullStringPtr(reviewReason)
	report.ReviewedBy = nullStringPtr(reviewedBy)
	return report, nil
}

func pipelineReportScope(access entity.OperationalAccess) ([]string, []any) {
	where := []string{`($1::boolean OR p.vendor_id = ANY($2::uuid[]) OR EXISTS (
		SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = p.vendor_id AND vs.site_id = ANY($3::uuid[])
	))`}
	args := []any{workflowSuperAdmin(access), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs)}
	if hasWorkflowRole(access.Roles, "WORKER") && !hasWorkflowRole(access.Roles, "ADMIN_WORKER", "ADMIN", "SUPER_ADMIN") {
		where = append(where, fmt.Sprintf("a.user_id = $%d", len(args)+1))
		args = append(args, access.UserID)
	}
	return where, args
}

func appendReportFilters(where *[]string, args *[]any, filter entity.ReportFilter, vendorExpr, userExpr, statusExpr, dateExpr string) {
	if filter.VendorID != "" {
		*where = append(*where, fmt.Sprintf("%s = $%d", vendorExpr, len(*args)+1))
		*args = append(*args, filter.VendorID)
	}
	if filter.SiteID != "" {
		*where = append(*where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = %s AND vs.site_id = $%d
		)`, vendorExpr, len(*args)+1))
		*args = append(*args, filter.SiteID)
	}
	if filter.UserID != "" {
		*where = append(*where, fmt.Sprintf("%s = $%d", userExpr, len(*args)+1))
		*args = append(*args, filter.UserID)
	}
	if filter.Status != "" {
		*where = append(*where, fmt.Sprintf("%s = $%d", statusExpr, len(*args)+1))
		*args = append(*args, filter.Status)
	}
	if filter.From != "" {
		*where = append(*where, fmt.Sprintf("%s >= $%d::date", dateExpr, len(*args)+1))
		*args = append(*args, filter.From)
	}
	if filter.To != "" {
		*where = append(*where, fmt.Sprintf("%s < ($%d::date + interval '1 day')", dateExpr, len(*args)+1))
		*args = append(*args, filter.To)
	}
}

func workflowSuperAdmin(access entity.OperationalAccess) bool {
	return hasWorkflowRole(access.Roles, "SUPER_ADMIN")
}

func superAdminAccess() entity.OperationalAccess {
	return entity.OperationalAccess{Roles: []string{"SUPER_ADMIN"}}
}

func hasWorkflowRole(roles []string, expected ...string) bool {
	for _, role := range roles {
		for _, value := range expected {
			if role == value {
				return true
			}
		}
	}
	return false
}

func workflowRequireAffected(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type DashboardRepositoryImpl struct {
	db *sql.DB
}

func NewDashboardRepository(db *sql.DB) repository.DashboardRepository {
	return &DashboardRepositoryImpl{db: db}
}

func (r *DashboardRepositoryImpl) GetDashboard(
	ctx context.Context,
	access entity.OperationalAccess,
	filter entity.DashboardFilter,
) (*entity.DashboardResponse, error) {
	result := &entity.DashboardResponse{
		Filter:          filter,
		VendorProgress:  make([]entity.DashboardVendorProgress, 0),
		InventoryReport: make([]entity.DashboardInventoryReport, 0),
		SafetyReports:   make([]entity.DashboardSafetyReport, 0),
	}

	args := []any{
		workflowSuperAdmin(access),
		pq.Array(access.VendorIDs),
		pq.Array(access.SiteIDs),
		filter.SiteID,
		filter.VendorID,
		filter.Date,
	}

	fmt.Printf(
		"[Dashboard] site_id=%v vendor_id=%v date=%v super_admin=%v vendor_ids=%v site_ids=%v\n",
		filter.SiteID,
		filter.VendorID,
		filter.Date,
		workflowSuperAdmin(access),
		access.VendorIDs,
		access.SiteIDs,
	)

	if err := r.getTaskSummary(ctx, args, &result.TaskSummary); err != nil {
		return nil, dashboardQueryError("task summary", err)
	}

	vendors, err := r.getVendorProgress(ctx, args)
	if err != nil {
		return nil, dashboardQueryError("vendor progress", err)
	}
	result.VendorProgress = vendors

	inventory, err := r.getInventoryReports(ctx, args)
	if err != nil {
		return nil, dashboardQueryError("inventory reports", err)
	}
	result.InventoryReport = inventory

	safety, err := r.getSafetyReports(ctx, args)
	if err != nil {
		return nil, dashboardQueryError("safety reports", err)
	}
	result.SafetyReports = safety

	fmt.Printf(
		"[Dashboard] result task_total=%d task_in_progress=%d task_completed=%d vendors=%d inventory_reports=%d safety_reports=%d\n",
		result.TaskSummary.TotalTask,
		result.TaskSummary.InProgress,
		result.TaskSummary.Completed,
		len(result.VendorProgress),
		len(result.InventoryReport),
		len(result.SafetyReports),
	)

	return result, nil
}

func dashboardPipelineScope() string {
	return `(
		$1::boolean
		OR p.vendor_id = ANY($2::uuid[])
		OR EXISTS (
			SELECT 1
			FROM vendor_sites vs_scope
			WHERE vs_scope.vendor_id = p.vendor_id
			  AND vs_scope.site_id = ANY($3::uuid[])
		)
	)`
}

func dashboardInventoryScope() string {
	return `(
		$1::boolean
		OR ir.vendor_id = ANY($2::uuid[])
		OR EXISTS (
			SELECT 1
			FROM vendor_sites vs_scope
			WHERE vs_scope.vendor_id = ir.vendor_id
			  AND vs_scope.site_id = ANY($3::uuid[])
		)
	)`
}

func dashboardSiteFilter(alias string) string {
	return fmt.Sprintf(`(
		NULLIF($4, '') IS NULL
		OR EXISTS (
			SELECT 1
			FROM vendor_sites vs_filter
			WHERE vs_filter.vendor_id = %s.vendor_id
			  AND vs_filter.site_id = NULLIF($4, '')::uuid
		)
	)`, alias)
}

func dashboardVendorFilter(alias string) string {
	return fmt.Sprintf(`(
		NULLIF($5, '') IS NULL
		OR %s.vendor_id = NULLIF($5, '')::uuid
	)`, alias)
}

func dashboardDateRange(alias, column string) string {
	return fmt.Sprintf(
		"%s.%s >= ($6::date::timestamp AT TIME ZONE 'Asia/Jakarta') AND %s.%s < (($6::date + 1)::timestamp AT TIME ZONE 'Asia/Jakarta')",
		alias, column, alias, column,
	)
}

func (r *DashboardRepositoryImpl) getTaskSummary(
	ctx context.Context,
	args []any,
	result *entity.DashboardTaskSummary,
) error {
	query := `
		SELECT
			COUNT(*) AS total_task,
			COUNT(*) FILTER (
				WHERE pr.status = 'IN_PROGRESS'
			) AS in_progress,
			COUNT(*) FILTER (
				WHERE pr.status = 'COMPLETED'
			) AS completed
		FROM pipeline_reports pr
		JOIN pipeline_item_assignments pia
			ON pia.id = pr.pipeline_item_assignment_id
		JOIN pipeline_items pi
			ON pi.id = pia.pipeline_item_id
		JOIN pipelines p
			ON p.id = pi.pipeline_id
		WHERE pr.report_date = $6::date
		  AND pia.is_active = TRUE
		  AND ` + dashboardPipelineScope() + `
		  AND ` + dashboardSiteFilter("p") + `
		  AND ` + dashboardVendorFilter("p") + `
	`

	return r.db.QueryRowContext(ctx, query, args...).Scan(
		&result.TotalTask,
		&result.InProgress,
		&result.Completed,
	)
}

func (r *DashboardRepositoryImpl) getVendorProgress(
	ctx context.Context,
	args []any,
) ([]entity.DashboardVendorProgress, error) {
	query := `
		SELECT
			v.id::text,
			v.name,
			COUNT(*) AS total_task,
			COUNT(*) FILTER (
				WHERE pr.status = 'COMPLETED'
			) AS completed,
			COUNT(*) FILTER (
				WHERE pr.status = 'IN_PROGRESS'
			) AS in_progress,
			COALESCE(
				ROUND(
					100.0 * COUNT(*) FILTER (
						WHERE pr.status = 'COMPLETED'
					) / NULLIF(COUNT(*), 0),
					0
				),
				0
			) AS progress_percentage
		FROM pipeline_reports pr
		JOIN pipeline_item_assignments pia
			ON pia.id = pr.pipeline_item_assignment_id
		JOIN pipeline_items pi
			ON pi.id = pia.pipeline_item_id
		JOIN pipelines p
			ON p.id = pi.pipeline_id
		JOIN vendors v
			ON v.id = p.vendor_id
		WHERE pr.report_date = $6::date
		  AND pia.is_active = TRUE
		  AND ` + dashboardPipelineScope() + `
		  AND ` + dashboardSiteFilter("p") + `
		  AND ` + dashboardVendorFilter("p") + `
		GROUP BY v.id, v.name
		ORDER BY v.name
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]entity.DashboardVendorProgress, 0)

	for rows.Next() {
		var item entity.DashboardVendorProgress

		if err := rows.Scan(
			&item.VendorID,
			&item.VendorName,
			&item.TotalTask,
			&item.Completed,
			&item.InProgress,
			&item.ProgressPercentage,
		); err != nil {
			return nil, err
		}

		result = append(result, item)
	}

	return result, rows.Err()
}

func (r *DashboardRepositoryImpl) getInventoryReports(
	ctx context.Context,
	args []any,
) ([]entity.DashboardInventoryReport, error) {
	query := `
		SELECT
			ir.id::text,
			v.name,
			u.full_name,
			inv.item,
			ir.description,
			inv.status_condition,
			ir.evidence,
			ir.report_type,
			ir.status,
			ir.created_at
		FROM inventory_reports ir
		LEFT JOIN inventories inv
			ON inv.id = ir.inventory_id
		   AND inv.vendor_id = ir.vendor_id
		JOIN vendors v
			ON v.id = ir.vendor_id
		JOIN users u
			ON u.id = ir.user_id
		WHERE ` + dashboardDateRange("ir", "created_at") + `
		  AND ` + dashboardInventoryScope() + `
		  AND ` + dashboardSiteFilter("ir") + `
		  AND ` + dashboardVendorFilter("ir") + `
		ORDER BY ir.created_at DESC
		LIMIT 10
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]entity.DashboardInventoryReport, 0)

	for rows.Next() {
		var item entity.DashboardInventoryReport
		var inventoryItem, description, statusCondition, evidence sql.NullString

		if err := rows.Scan(
			&item.InventoryReportID,
			&item.VendorName,
			&item.FullName,
			&inventoryItem,
			&description,
			&statusCondition,
			&evidence,
			&item.ReportType,
			&item.ReportStatus,
			&item.ReportDate,
		); err != nil {
			return nil, err
		}

		item.Item = nullStringPtr(inventoryItem)
		item.Description = nullStringPtr(description)
		item.StatusCondition = nullStringPtr(statusCondition)
		item.Evidence = nullStringPtr(evidence)

		result = append(result, item)
	}

	return result, rows.Err()
}

func (r *DashboardRepositoryImpl) getSafetyReports(
	ctx context.Context,
	args []any,
) ([]entity.DashboardSafetyReport, error) {
	query := `
		SELECT
			s.name,
			v.name,
			u.full_name,
			p.name,
			i.name,
			d.note_category,
			d.notes,
			d.open_date,
			d.closed_date,
			pr.shift,
			pr.report_date
		FROM pipeline_report_details d
		JOIN pipeline_reports pr
			ON pr.id = d.pipeline_report_id
		JOIN pipeline_item_assignments pia
			ON pia.id = pr.pipeline_item_assignment_id
		JOIN pipeline_items pi
			ON pi.id = pia.pipeline_item_id
		JOIN pipelines p
			ON p.id = pi.pipeline_id
		JOIN items i
			ON i.id = pi.item_id
		JOIN vendors v
			ON v.id = p.vendor_id
		JOIN vendor_sites vs
			ON vs.vendor_id = v.id
		JOIN sites s
			ON s.id = vs.site_id
		JOIN users u
			ON u.id = pia.user_id
		WHERE (
			NULLIF($4, '') IS NULL
			OR vs.site_id = NULLIF($4, '')::uuid
		)
		  AND (
			NULLIF($5, '') IS NULL
			OR v.id = NULLIF($5, '')::uuid
		  )
		  AND ` + dashboardDateRange("d", "created_at") + `
		  AND ` + dashboardPipelineScope() + `
		  AND d.note_category IS NOT NULL
		  AND TRIM(d.note_category) <> ''
		  AND TRIM(d.note_category) <> '-'
		ORDER BY d.open_date DESC NULLS LAST
		LIMIT 10
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]entity.DashboardSafetyReport, 0)

	for rows.Next() {
		var item entity.DashboardSafetyReport
		var noteCategory, notes sql.NullString
		var openDate, closedDate sql.NullTime

		if err := rows.Scan(
			&item.SiteName,
			&item.VendorName,
			&item.WorkerName,
			&item.CategoryName,
			&item.AreaName,
			&noteCategory,
			&notes,
			&openDate,
			&closedDate,
			&item.Shift,
			&item.ReportDate,
		); err != nil {
			return nil, err
		}

		item.NoteCategory = nullStringPtr(noteCategory)
		item.Notes = nullStringPtr(notes)
		item.OpenDate = nullTimePtr(openDate)
		item.ClosedDate = nullTimePtr(closedDate)

		result = append(result, item)
	}

	return result, rows.Err()
}

var _ repository.DashboardRepository = (*DashboardRepositoryImpl)(nil)

func dashboardQueryError(section string, err error) error {
	return fmt.Errorf("dashboard %s: %w", section, err)
}

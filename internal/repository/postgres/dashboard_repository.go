
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

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

func (r *DashboardRepositoryImpl) GetDashboardSummary(
	ctx context.Context,
	access entity.OperationalAccess,
	filter entity.DashboardFilter,
) (*entity.DashboardSummaryResponse, error) {
	if filter.Date == "" {
		filter.Date = time.Now().In(time.FixedZone("WIB", 7*60*60)).Format("2006-01-02")
	}

	result := &entity.DashboardSummaryResponse{
		Filter:          filter,
		VendorProgress:  make([]entity.DashboardVendorProgressSummary, 0),
		InventoryReport: make([]entity.DashboardInventoryReport, 0),
		SafetyReports:   make([]entity.DashboardSafetyReport, 0),
	}

	args := dashboardArgs(access, filter)

	if err := r.getTaskSummary(ctx, args, &result.TaskSummary); err != nil {
		return nil, dashboardQueryError("task summary", err)
	}

	vendors, err := r.getVendorProgress(ctx, args)
	if err != nil {
		return nil, dashboardQueryError("vendor progress", err)
	}

	for _, vendor := range vendors {
		result.VendorProgress = append(result.VendorProgress, entity.DashboardVendorProgressSummary{
			SiteID:             vendor.SiteID,
			SiteName:           vendor.SiteName,
			VendorID:           vendor.VendorID,
			VendorName:         vendor.VendorName,
			TotalTask:          vendor.TotalTask,
			Completed:          vendor.Completed,
			InProgress:         vendor.InProgress,
			NotStarted:         vendor.NotStarted,
			ProgressPercentage: vendor.ProgressPercentage,
		})
	}

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

	return result, nil
}

func (r *DashboardRepositoryImpl) GetDashboardDetail(
	ctx context.Context,
	access entity.OperationalAccess,
	filter entity.DashboardFilter,
	detailType string,
) (*entity.DashboardDetailResponse, error) {
	if filter.Date == "" {
		filter.Date = time.Now().In(time.FixedZone("WIB", 7*60*60)).Format("2006-01-02")
	}

	args := dashboardArgs(access, filter)
	result := &entity.DashboardDetailResponse{
		Type: detailType,
	}

	switch detailType {
	case "vendor_progress":
		vendors, err := r.getVendorProgress(ctx, args)
		if err != nil {
			return nil, dashboardQueryError("vendor progress", err)
		}
		result.Data = entity.DashboardVendorProgressResponse{
			VendorProgress: vendors,
		}

	case "inventory_reports":
		data, err := r.getInventoryReports(ctx, args)
		if err != nil {
			return nil, dashboardQueryError("inventory reports", err)
		}
		result.Data = data

	case "safety_reports":
		data, err := r.getSafetyReports(ctx, args)
		if err != nil {
			return nil, dashboardQueryError("safety reports", err)
		}
		result.Data = data

	default:
		return nil, fmt.Errorf("unsupported dashboard detail type: %s", detailType)
	}

	return result, nil
}

// Parameter SQL:
// $1  super admin
// $2  vendor IDs in actor scope
// $3  site IDs in actor scope
// $4  requested site ID
// $5  requested vendor ID
// $6  dashboard date
// $7  IsWorker from filter
// $8  authenticated actor user ID
// $9  ADMIN role
// $10 ADMIN_WORKER role
// $11 WORKER role
func dashboardArgs(
	access entity.OperationalAccess,
	filter entity.DashboardFilter,
) []any {
	return []any{
		workflowSuperAdmin(access),
		pq.Array(access.VendorIDs),
		pq.Array(access.SiteIDs),
		filter.SiteID,
		filter.VendorID,
		filter.Date,
		filter.IsWorker,
		filter.UserID,
		hasDashboardRole(access, "ADMIN"),
		hasDashboardRole(access, "ADMIN_WORKER"),
		hasDashboardRole(access, "WORKER"),
	}
}

func hasDashboardRole(access entity.OperationalAccess, role string) bool {
	for _, currentRole := range access.Roles {
		if currentRole == role {
			return true
		}
	}
	return false
}

// Scope pipeline:
// SUPER_ADMIN: semua data.
// ADMIN: site dalam scope dan relasi vendor-site valid.
// ADMIN_WORKER: vendor dalam scope dan semua site yang terhubung.
// WORKER: vendor dalam scope; identitas pekerja dibatasi oleh dashboardWorkerFilter.
func dashboardPipelineScope() string {
	return `(
		$1::boolean
		OR (
			EXISTS (
				SELECT 1
				FROM public.vendor_sites vs_scope
				WHERE vs_scope.vendor_id = p.vendor_id
				  AND vs_scope.site_id = p.site_id
			)
			AND (
				(
					$9::boolean
					AND cardinality($3::uuid[]) > 0
					AND p.site_id = ANY($3::uuid[])
				)
				OR (
					$10::boolean
					AND cardinality($2::uuid[]) > 0
					AND p.vendor_id = ANY($2::uuid[])
				)
				OR (
					$11::boolean
					AND cardinality($2::uuid[]) > 0
					AND p.vendor_id = ANY($2::uuid[])
				)
			)
		)
	)`
}

// Mempersempit site dari request tanpa menghapus batas scope role.
func dashboardPipelineSiteFilter() string {
	return `(
		NULLIF($4, '') IS NULL
		OR p.site_id = NULLIF($4, '')::uuid
	)`
}

// Inventory reports memiliki vendor_id, bukan site_id langsung.
// Batas site menggunakan relasi vendor_sites.
func dashboardInventoryScope() string {
	return `(
		$1::boolean
		OR (
			(
				(
					$9::boolean
					AND cardinality($3::uuid[]) > 0
					AND EXISTS (
						SELECT 1
						FROM public.vendor_sites vs_scope
						WHERE vs_scope.vendor_id = ir.vendor_id
						  AND vs_scope.site_id = ANY($3::uuid[])
					)
				)
				OR (
					$10::boolean
					AND cardinality($2::uuid[]) > 0
					AND ir.vendor_id = ANY($2::uuid[])
				)
				OR (
					$11::boolean
					AND cardinality($2::uuid[]) > 0
					AND ir.vendor_id = ANY($2::uuid[])
				)
			)
		)
	)`
}

// Digunakan untuk filter site pada inventory reports.
func dashboardSiteFilter(alias string) string {
	return fmt.Sprintf(`(
		NULLIF($4, '') IS NULL
		OR EXISTS (
			SELECT 1
			FROM public.vendor_sites vs_filter
			WHERE vs_filter.vendor_id = %s.vendor_id
			  AND vs_filter.site_id = NULLIF($4, '')::uuid
			  AND (
				NOT $9::boolean
				OR vs_filter.site_id = ANY($3::uuid[])
			  )
		)
	)`, alias)
}

// Filter request tidak dapat memperluas scope role karena
// tetap dikombinasikan dengan dashboardPipelineScope/dashboardInventoryScope.
func dashboardVendorFilter(alias string) string {
	return fmt.Sprintf(`(
		NULLIF($5, '') IS NULL
		OR %s.vendor_id = NULLIF($5, '')::uuid
	)`, alias)
}

// WORKER selalu dibatasi ke user login.
// $11 memastikan pembatasan tetap aktif berdasarkan role,
// sekalipun filter.IsWorker tidak sengaja bernilai false.
func dashboardWorkerFilter(alias string) string {
	return fmt.Sprintf(`(
		NOT ($7::boolean OR $11::boolean)
		OR %s.user_id = NULLIF($8, '')::uuid
	)`, alias)
}

func dashboardDateRange(alias, column string) string {
	return fmt.Sprintf(`(
		NULLIF($6, '') IS NOT NULL
		AND %s.%s >= (
			NULLIF($6, '')::date::timestamp
			AT TIME ZONE 'Asia/Jakarta'
		)
		AND %s.%s < (
			(NULLIF($6, '')::date + 1)::timestamp
			AT TIME ZONE 'Asia/Jakarta'
		)
	)`, alias, column, alias, column)
}

func (r *DashboardRepositoryImpl) getTaskSummary(
	ctx context.Context,
	args []any,
	result *entity.DashboardTaskSummary,
) error {
	query := `
		SELECT
			COUNT(DISTINCT pia.id) AS total_task,
			COUNT(DISTINCT pia.id) FILTER (
				WHERE pr.status = 'IN_PROGRESS'
			) AS in_progress,
			COUNT(DISTINCT pia.id) FILTER (
				WHERE pr.status = 'COMPLETED'
			) AS completed,
			COUNT(DISTINCT pia.id) FILTER (
				WHERE pr.id IS NULL
			) AS not_started
		FROM pipeline_item_assignments pia
		JOIN pipeline_items pi
			ON pi.id = pia.pipeline_item_id
		JOIN pipelines p
			ON p.id = pi.pipeline_id
		LEFT JOIN pipeline_reports pr
			ON pr.pipeline_item_assignment_id = pia.id
		   AND pr.report_date = $6::date
		WHERE pia.is_active = TRUE
		  AND ` + dashboardPipelineScope() + `
		  AND ` + dashboardPipelineSiteFilter() + `
		  AND ` + dashboardVendorFilter("p") + `
		  AND ` + dashboardWorkerFilter("pia")

	return r.db.QueryRowContext(ctx, query, args...).Scan(
		&result.TotalTask,
		&result.InProgress,
		&result.Completed,
		&result.NotStarted,
	)
}

func (r *DashboardRepositoryImpl) getVendorProgress(
	ctx context.Context,
	args []any,
) ([]entity.DashboardVendorProgress, error) {
	query := `
		WITH filtered_assignment AS (
			SELECT
				pia.id AS assignment_id,
				pia.pipeline_item_id,
				pia.user_id,
				u.full_name AS worker_name,
				p.id AS area_id,
				p.name AS area_name,
				p.site_id,
				s.name AS site_name,
				p.vendor_id,
				v.name AS vendor_name,
				pi.item_id,
				i.name AS point_name,
				pi.sort_order
			FROM pipeline_item_assignments pia
			JOIN pipeline_items pi ON pi.id = pia.pipeline_item_id
			JOIN pipelines p ON p.id = pi.pipeline_id
			JOIN sites s ON s.id = p.site_id
			JOIN items i ON i.id = pi.item_id
			JOIN vendors v ON v.id = p.vendor_id
			LEFT JOIN users u ON u.id = pia.user_id
			WHERE pia.is_active = TRUE
			  AND pi.status = 'ACTIVE'
			  AND p.status = 'ACTIVE'
			  AND i.status = 'ACTIVE'
			  AND ` + dashboardPipelineScope() + `
			  AND ` + dashboardPipelineSiteFilter() + `
			  AND ` + dashboardVendorFilter("p") + `
			  AND ` + dashboardWorkerFilter("pia") + `
		),
		point_base AS (
			SELECT DISTINCT
				site_id, site_name, vendor_id, vendor_name,
				area_id, area_name, pipeline_item_id, item_id,
				point_name, sort_order
			FROM filtered_assignment
		),
		assignment_summary AS (
			SELECT
				site_id, vendor_id, area_id, pipeline_item_id,
				COUNT(DISTINCT user_id) AS total_worker
			FROM filtered_assignment
			GROUP BY site_id, vendor_id, area_id, pipeline_item_id
		),
		pending_detail AS (
			SELECT
				fa.site_id, fa.vendor_id, fa.area_id, fa.pipeline_item_id,
				JSONB_AGG(
					JSONB_BUILD_OBJECT(
						'assignment_id', fa.assignment_id,
						'worker_id', fa.user_id,
						'worker_name', fa.worker_name,
						'status', 'PENDING'
					)
					ORDER BY fa.worker_name, fa.assignment_id
				) AS task_detail
			FROM filtered_assignment fa
			WHERE NOT EXISTS (
				SELECT 1
				FROM pipeline_reports pr
				WHERE pr.pipeline_item_assignment_id = fa.assignment_id
				  AND pr.report_date = NULLIF($6, '')::date
			)
			GROUP BY fa.site_id, fa.vendor_id, fa.area_id, fa.pipeline_item_id
		),
		report_count AS (
			SELECT
				fa.site_id, fa.vendor_id, fa.area_id, fa.pipeline_item_id,
				COUNT(DISTINCT pr.id) AS total_report
			FROM filtered_assignment fa
			JOIN pipeline_reports pr
				ON pr.pipeline_item_assignment_id = fa.assignment_id
			   AND pr.report_date = NULLIF($6, '')::date
			GROUP BY fa.site_id, fa.vendor_id, fa.area_id, fa.pipeline_item_id
		),
		report_rows AS (
			SELECT
				fa.site_id, fa.vendor_id, fa.area_id, fa.pipeline_item_id,
				fa.assignment_id, fa.user_id, fa.worker_name,
				pr.id AS report_id, pr.shift, pr.status AS task_status,
				prd.open_date, prd.closed_date AS close_date,
				COALESCE(NULLIF(prd.note_category, ''), '-') AS category_note,
				prd.notes
			FROM filtered_assignment fa
			JOIN pipeline_reports pr
				ON pr.pipeline_item_assignment_id = fa.assignment_id
			   AND pr.report_date = NULLIF($6, '')::date
			LEFT JOIN pipeline_report_details prd
				ON prd.pipeline_report_id = pr.id
		),
		shift_detail AS (
			SELECT
				rr.site_id, rr.vendor_id, rr.area_id,
				rr.pipeline_item_id, rr.shift,
				JSONB_AGG(
					JSONB_BUILD_OBJECT(
						'assignment_id', rr.assignment_id,
						'worker_id', rr.user_id,
						'worker_name', rr.worker_name,
						'status', rr.task_status,
						'open_date', rr.open_date,
						'close_date', rr.close_date,
						'category_note', rr.category_note,
						'notes', rr.notes
					)
					ORDER BY rr.worker_name, rr.report_id
				) AS task_detail
			FROM report_rows rr
			GROUP BY
				rr.site_id, rr.vendor_id, rr.area_id,
				rr.pipeline_item_id, rr.shift
		),
		point_json AS (
			SELECT
				pb.site_id, pb.site_name, pb.vendor_id, pb.vendor_name,
				pb.area_id, pb.area_name, pb.pipeline_item_id,
				pb.point_name, pb.sort_order,
				COALESCE(ass.total_worker, 0) AS total_worker,
				COALESCE(rc.total_report, 0) AS total_report,
				JSONB_BUILD_OBJECT(
					'pipeline_item_id', pb.pipeline_item_id,
					'nama_point', pb.point_name,
					'total_worker', COALESCE(ass.total_worker, 0),
					'total_report', COALESCE(rc.total_report, 0),
					'pending_detail', COALESCE(pd.task_detail, '[]'::jsonb),
					'shift_detail', COALESCE(
						(
							SELECT JSONB_OBJECT_AGG(
								sd.shift,
								JSONB_BUILD_OBJECT('task_detail', sd.task_detail)
							)
							FROM shift_detail sd
							WHERE sd.site_id = pb.site_id
							  AND sd.vendor_id = pb.vendor_id
							  AND sd.area_id = pb.area_id
							  AND sd.pipeline_item_id = pb.pipeline_item_id
						),
						'{}'::jsonb
					)
				) AS point_data
			FROM point_base pb
			LEFT JOIN assignment_summary ass
				ON ass.site_id = pb.site_id
			   AND ass.vendor_id = pb.vendor_id
			   AND ass.area_id = pb.area_id
			   AND ass.pipeline_item_id = pb.pipeline_item_id
			LEFT JOIN report_count rc
				ON rc.site_id = pb.site_id
			   AND rc.vendor_id = pb.vendor_id
			   AND rc.area_id = pb.area_id
			   AND rc.pipeline_item_id = pb.pipeline_item_id
			LEFT JOIN pending_detail pd
				ON pd.site_id = pb.site_id
			   AND pd.vendor_id = pb.vendor_id
			   AND pd.area_id = pb.area_id
			   AND pd.pipeline_item_id = pb.pipeline_item_id
		),
		area_json AS (
			SELECT
				pj.site_id, pj.site_name, pj.vendor_id, pj.vendor_name,
				pj.area_id, pj.area_name,
				COUNT(*) AS total_point_per_area,
				(
					SELECT COUNT(DISTINCT fa.user_id)
					FROM filtered_assignment fa
					WHERE fa.site_id = pj.site_id
					  AND fa.vendor_id = pj.vendor_id
					  AND fa.area_id = pj.area_id
				) AS total_worker_per_area,
				SUM(pj.total_report) AS total_report_per_area,
				JSONB_AGG(
					pj.point_data
					ORDER BY pj.sort_order, pj.point_name
				) AS point_detail
			FROM point_json pj
			GROUP BY
				pj.site_id, pj.site_name, pj.vendor_id, pj.vendor_name,
				pj.area_id, pj.area_name
		),
		vendor_detail AS (
			SELECT
				aj.site_id, aj.vendor_id,
				JSONB_BUILD_OBJECT(
					'total_area', COUNT(*),
					'total_point', COALESCE(SUM(aj.total_point_per_area), 0),
					'total_worker', (
						SELECT COUNT(DISTINCT fa.user_id)
						FROM filtered_assignment fa
						WHERE fa.site_id = aj.site_id
						  AND fa.vendor_id = aj.vendor_id
					),
					'total_report', COALESCE(SUM(aj.total_report_per_area), 0),
					'area_detail', JSONB_AGG(
						JSONB_BUILD_OBJECT(
							'area_id', aj.area_id,
							'nama_area', aj.area_name,
							'total_point_per_area', aj.total_point_per_area,
							'total_worker_per_area', aj.total_worker_per_area,
							'total_report_per_area', aj.total_report_per_area,
							'point_detail', aj.point_detail
						)
						ORDER BY aj.area_name
					)
				) AS detail
			FROM area_json aj
			GROUP BY aj.site_id, aj.vendor_id
		),
		vendor_summary AS (
			SELECT
				fa.site_id, fa.site_name, fa.vendor_id, fa.vendor_name,
				COUNT(DISTINCT fa.assignment_id) AS total_task,
				COUNT(DISTINCT fa.assignment_id) FILTER (
					WHERE EXISTS (
						SELECT 1
						FROM pipeline_reports pr
						WHERE pr.pipeline_item_assignment_id = fa.assignment_id
						  AND pr.report_date = NULLIF($6, '')::date
						  AND pr.status = 'COMPLETED'
					)
				) AS completed,
				COUNT(DISTINCT fa.assignment_id) FILTER (
					WHERE EXISTS (
						SELECT 1
						FROM pipeline_reports pr
						WHERE pr.pipeline_item_assignment_id = fa.assignment_id
						  AND pr.report_date = NULLIF($6, '')::date
						  AND pr.status = 'IN_PROGRESS'
					)
				) AS in_progress,
				COUNT(DISTINCT fa.assignment_id) FILTER (
					WHERE NOT EXISTS (
						SELECT 1
						FROM pipeline_reports pr
						WHERE pr.pipeline_item_assignment_id = fa.assignment_id
						  AND pr.report_date = NULLIF($6, '')::date
					)
				) AS not_started,
				COALESCE(
					ROUND(
						100.0 * COUNT(DISTINCT fa.assignment_id) FILTER (
							WHERE EXISTS (
								SELECT 1
								FROM pipeline_reports pr
								WHERE pr.pipeline_item_assignment_id = fa.assignment_id
								  AND pr.report_date = NULLIF($6, '')::date
								  AND pr.status = 'COMPLETED'
							)
						) / NULLIF(COUNT(DISTINCT fa.assignment_id), 0),
						0
					),
					0
				) AS progress_percentage
			FROM filtered_assignment fa
			GROUP BY
				fa.site_id, fa.site_name, fa.vendor_id, fa.vendor_name
		)
		SELECT
			vs.site_id::text,
			vs.site_name,
			vs.vendor_id::text,
			vs.vendor_name,
			vs.total_task,
			vs.completed,
			vs.in_progress,
			vs.not_started,
			vs.progress_percentage,
			COALESCE(vd.detail, '{}'::jsonb) AS vendor_progress_detail
		FROM vendor_summary vs
		LEFT JOIN vendor_detail vd
			ON vd.site_id = vs.site_id
		   AND vd.vendor_id = vs.vendor_id
		ORDER BY vs.site_name, vs.vendor_name
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]entity.DashboardVendorProgress, 0)

	for rows.Next() {
		var item entity.DashboardVendorProgress
		var detail []byte

		if err := rows.Scan(
			&item.SiteID,
			&item.SiteName,
			&item.VendorID,
			&item.VendorName,
			&item.TotalTask,
			&item.Completed,
			&item.InProgress,
			&item.NotStarted,
			&item.ProgressPercentage,
			&detail,
		); err != nil {
			return nil, err
		}

		item.VendorProgressDetail = append(json.RawMessage(nil), detail...)
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
		  AND ` + dashboardWorkerFilter("ir") + `
		ORDER BY ir.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]entity.DashboardInventoryReport, 0)

	for rows.Next() {
		var item entity.DashboardInventoryReport
		var inventoryItem sql.NullString
		var description sql.NullString
		var statusCondition sql.NullString
		var evidence sql.NullString

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
		SELECT DISTINCT
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
		JOIN sites s
			ON s.id = p.site_id
		JOIN users u
			ON u.id = pia.user_id
		WHERE ` + dashboardPipelineSiteFilter() + `
		  AND ` + dashboardDateRange("d", "created_at") + `
		  AND ` + dashboardPipelineScope() + `
		  AND ` + dashboardVendorFilter("p") + `
		  AND ` + dashboardWorkerFilter("pia") + `
		  AND d.note_category IS NOT NULL
		  AND TRIM(d.note_category) <> ''
		  AND TRIM(d.note_category) <> '-'
		ORDER BY d.open_date DESC NULLS LAST
	`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]entity.DashboardSafetyReport, 0)

	for rows.Next() {
		var item entity.DashboardSafetyReport
		var noteCategory sql.NullString
		var notes sql.NullString
		var openDate sql.NullTime
		var closedDate sql.NullTime

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

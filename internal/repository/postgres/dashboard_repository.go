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
		result.VendorProgress = append(
			result.VendorProgress,
			entity.DashboardVendorProgressSummary{
				SiteID:             vendor.SiteID,
				SiteName:           vendor.SiteName,
				VendorID:           vendor.VendorID,
				VendorName:         vendor.VendorName,
				TotalTask:          vendor.TotalTask,
				TotalArea:          vendor.TotalArea,
				TotalPoint:         vendor.TotalPoint,
				TotalWorker:        vendor.TotalWorker,
				TotalReport:        vendor.TotalReport,
				Completed:          vendor.Completed,
				InProgress:         vendor.InProgress,
				NotStarted:         vendor.NotStarted,
				ProgressPercentage: vendor.ProgressPercentage,
				StatusCheckPoint: dashboardCheckPointStatusFromDetail(
					vendor.VendorProgressDetail,
				),
			},
		)
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

// SUPER_ADMIN: semua data.
// ADMIN: site dalam scope dan relasi vendor-site valid.
// ADMIN_WORKER: vendor dalam scope dan seluruh site yang terhubung.
// WORKER: vendor dalam scope; assignment/laporan dibatasi ke user login.
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

func dashboardPipelineSiteFilter() string {
	return `(
		NULLIF($4, '') IS NULL
		OR p.site_id = NULLIF($4, '')::uuid
	)`
}

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

func dashboardVendorFilter(alias string) string {
	return fmt.Sprintf(`(
		NULLIF($5, '') IS NULL
		OR %s.vendor_id = NULLIF($5, '')::uuid
	)`, alias)
}

// WORKER selalu dibatasi ke user login.
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

// dashboardCheckPointFromDetail mengambil check_point dari JSON
// vendor_progress_detail agar statusnya dapat digunakan oleh response.
func dashboardCheckPointFromDetail(
	detail json.RawMessage,
) *entity.DashboardVendorCheckPoint {
	if len(detail) == 0 {
		return nil
	}

	var wrapper struct {
		CheckPoint *entity.DashboardVendorCheckPoint `json:"check_point"`
	}

	if err := json.Unmarshal(detail, &wrapper); err != nil {
		return nil
	}

	return wrapper.CheckPoint
}

// dashboardCheckPointStatusFromDetail mengambil status check_point
// untuk field status_check_point pada response vendor_progress.
func dashboardCheckPointStatusFromDetail(
	detail json.RawMessage,
) string {
	checkPoint := dashboardCheckPointFromDetail(detail)
	if checkPoint == nil {
		return "INCOMPLETE"
	}

	if checkPoint.Status == "COMPLETE" {
		return "COMPLETE"
	}

	return "INCOMPLETE"
}

// getTaskSummary menghitung target dari pipeline_items, bukan dari assignment.
// Status laporan dihitung dari assignment aktif dan tanggal dashboard.
func (r *DashboardRepositoryImpl) getTaskSummary(
	ctx context.Context,
	args []any,
	result *entity.DashboardTaskSummary,
) error {
	query := `
		WITH filtered_target AS (
			SELECT
				pi.id AS pipeline_item_id,
				p.id AS area_id
			FROM pipeline_items pi
			JOIN pipelines p ON p.id = pi.pipeline_id
			JOIN items i ON i.id = pi.item_id
			WHERE pi.status = 'ACTIVE'
			  AND p.status = 'ACTIVE'
			  AND i.status = 'ACTIVE'
			  AND ` + dashboardPipelineScope() + `
			  AND ` + dashboardPipelineSiteFilter() + `
			  AND ` + dashboardVendorFilter("p") + `
		),
		filtered_assignment AS (
			SELECT
				pia.id AS assignment_id
			FROM pipeline_item_assignments pia
			JOIN filtered_target ft
				ON ft.pipeline_item_id = pia.pipeline_item_id
			WHERE pia.is_active = TRUE
			  AND ` + dashboardWorkerFilter("pia") + `
		),
		target_summary AS (
			SELECT
				COUNT(DISTINCT area_id) AS total_area,
				COUNT(*) AS total_point
			FROM filtered_target
		),
		assignment_summary AS (
			SELECT
				COUNT(DISTINCT fa.assignment_id) AS total_task,
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
					WHERE EXISTS (
						SELECT 1
						FROM pipeline_reports pr
						WHERE pr.pipeline_item_assignment_id = fa.assignment_id
						  AND pr.report_date = NULLIF($6, '')::date
						  AND pr.status = 'COMPLETED'
					)
				) AS completed,
				COUNT(DISTINCT fa.assignment_id) FILTER (
					WHERE NOT EXISTS (
						SELECT 1
						FROM pipeline_reports pr
						WHERE pr.pipeline_item_assignment_id = fa.assignment_id
						  AND pr.report_date = NULLIF($6, '')::date
					)
				) AS not_started
			FROM filtered_assignment fa
		)
		SELECT
			ts.total_point,
			asum.total_task,
			ts.total_area,
			asum.in_progress,
			asum.completed,
			asum.not_started
		FROM target_summary ts
		CROSS JOIN assignment_summary asum
	`

	return r.db.QueryRowContext(ctx, query, args...).Scan(
		&result.TotalPoint,
		&result.TotalTask,
		&result.TotalArea,
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
		WITH filtered_target AS (
			SELECT
				pi.id AS pipeline_item_id,
				pi.item_id,
				pi.sort_order,
				p.id AS area_id,
				p.name AS area_name,
				p.site_id,
				s.name AS site_name,
				p.vendor_id,
				v.name AS vendor_name,
				i.name AS point_name
			FROM pipeline_items pi
			JOIN pipelines p ON p.id = pi.pipeline_id
			JOIN sites s ON s.id = p.site_id
			JOIN items i ON i.id = pi.item_id
			JOIN vendors v ON v.id = p.vendor_id
			WHERE pi.status = 'ACTIVE'
			  AND p.status = 'ACTIVE'
			  AND i.status = 'ACTIVE'
			  AND ` + dashboardPipelineScope() + `
			  AND ` + dashboardPipelineSiteFilter() + `
			  AND ` + dashboardVendorFilter("p") + `
		),
		-- Assignment aktif yang terlihat oleh akun login.
		-- Untuk WORKER, hanya assignment milik user tersebut.
		filtered_assignment AS (
			SELECT
				pia.id AS assignment_id,
				pia.pipeline_item_id,
				pia.user_id,
				u.full_name AS worker_name,
				ft.area_id,
				ft.area_name,
				ft.site_id,
				ft.site_name,
				ft.vendor_id,
				ft.vendor_name,
				ft.item_id,
				ft.point_name,
				ft.sort_order
			FROM pipeline_item_assignments pia
			JOIN filtered_target ft
				ON ft.pipeline_item_id = pia.pipeline_item_id
			LEFT JOIN users u ON u.id = pia.user_id
			WHERE pia.is_active = TRUE
			  AND ` + dashboardWorkerFilter("pia") + `
		),
		-- Check Point membaca semua target dan assignment aktif,
		-- bukan hanya assignment yang terlihat oleh WORKER.
		point_assignment_status AS (
			SELECT
				ft.site_id,
				ft.vendor_id,
				ft.pipeline_item_id,
				EXISTS (
					SELECT 1
					FROM pipeline_item_assignments pia
					WHERE pia.pipeline_item_id = ft.pipeline_item_id
					  AND pia.is_active = TRUE
				) AS is_assigned
			FROM filtered_target ft
		),
		check_point_summary AS (
			SELECT
				ft.site_id,
				ft.vendor_id,
				COUNT(DISTINCT ft.area_id) AS total_area,
				COUNT(*) AS total_point,
				COUNT(*) FILTER (
					WHERE pas.is_assigned
				) AS total_assigned_point,
				COALESCE(
					JSONB_AGG(
						JSONB_BUILD_OBJECT(
							'pipeline_item_id', ft.pipeline_item_id,
							'item_id', ft.item_id,
							'nama_point', ft.point_name,
							'area_id', ft.area_id,
							'nama_area', ft.area_name
						)
						ORDER BY ft.area_name, ft.sort_order, ft.point_name
					) FILTER (WHERE NOT pas.is_assigned),
					'[]'::jsonb
				) AS unassigned_point_detail
			FROM filtered_target ft
			JOIN point_assignment_status pas
				ON pas.site_id = ft.site_id
			   AND pas.vendor_id = ft.vendor_id
			   AND pas.pipeline_item_id = ft.pipeline_item_id
			GROUP BY ft.site_id, ft.vendor_id
		),
		-- Hanya point yang memiliki assignment aktif yang dimasukkan
		-- ke area_detail.point_detail.
		-- Untuk WORKER, point harus memiliki assignment milik user login.
		point_base AS (
			SELECT
				ft.site_id,
				ft.site_name,
				ft.vendor_id,
				ft.vendor_name,
				ft.area_id,
				ft.area_name,
				ft.pipeline_item_id,
				ft.item_id,
				ft.point_name,
				ft.sort_order
			FROM filtered_target ft
			WHERE EXISTS (
				SELECT 1
				FROM pipeline_item_assignments pia
				WHERE pia.pipeline_item_id = ft.pipeline_item_id
				  AND pia.is_active = TRUE
				  AND (
					NOT ($7::boolean OR $11::boolean)
					OR pia.user_id = NULLIF($8, '')::uuid
				  )
			)
		),
		assignment_summary AS (
			SELECT
				fa.site_id,
				fa.vendor_id,
				fa.area_id,
				fa.pipeline_item_id,
				COUNT(DISTINCT fa.user_id) AS total_worker
			FROM filtered_assignment fa
			GROUP BY
				fa.site_id,
				fa.vendor_id,
				fa.area_id,
				fa.pipeline_item_id
		),
		pending_detail AS (
			SELECT
				fa.site_id,
				fa.vendor_id,
				fa.area_id,
				fa.pipeline_item_id,
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
			GROUP BY
				fa.site_id,
				fa.vendor_id,
				fa.area_id,
				fa.pipeline_item_id
		),
		report_count AS (
			SELECT
				fa.site_id,
				fa.vendor_id,
				fa.area_id,
				fa.pipeline_item_id,
				COUNT(DISTINCT pr.id) AS total_report
			FROM filtered_assignment fa
			JOIN pipeline_reports pr
				ON pr.pipeline_item_assignment_id = fa.assignment_id
			   AND pr.report_date = NULLIF($6, '')::date
			GROUP BY
				fa.site_id,
				fa.vendor_id,
				fa.area_id,
				fa.pipeline_item_id
		),
		report_rows AS (
			SELECT
				fa.site_id,
				fa.vendor_id,
				fa.area_id,
				fa.pipeline_item_id,
				fa.assignment_id,
				fa.user_id,
				fa.worker_name,
				pr.id AS report_id,
				pr.shift,
				pr.status AS task_status,
				prd.open_date,
				prd.closed_date AS close_date,
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
				rr.site_id,
				rr.vendor_id,
				rr.area_id,
				rr.pipeline_item_id,
				rr.shift,
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
				rr.site_id,
				rr.vendor_id,
				rr.area_id,
				rr.pipeline_item_id,
				rr.shift
		),
		point_json AS (
			SELECT
				pb.site_id,
				pb.site_name,
				pb.vendor_id,
				pb.vendor_name,
				pb.area_id,
				pb.area_name,
				pb.pipeline_item_id,
				pb.point_name,
				pb.sort_order,
				COALESCE(ass.total_worker, 0) AS total_worker,
				COALESCE(rc.total_report, 0) AS total_report,
				JSONB_BUILD_OBJECT(
					'pipeline_item_id', pb.pipeline_item_id,
					'item_id', pb.item_id,
					'nama_point', pb.point_name,
					'total_worker', COALESCE(ass.total_worker, 0),
					'total_report', COALESCE(rc.total_report, 0),
					'pending_detail', COALESCE(pd.task_detail, '[]'::jsonb),
					'shift_detail', COALESCE(
						(
							SELECT JSONB_OBJECT_AGG(
								sd.shift,
								JSONB_BUILD_OBJECT(
									'task_detail', sd.task_detail
								)
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
				pj.site_id,
				pj.site_name,
				pj.vendor_id,
				pj.vendor_name,
				pj.area_id,
				pj.area_name,
				-- Hanya menghitung point yang memiliki assignment,
				-- sesuai dengan isi point_detail.
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
				pj.site_id,
				pj.site_name,
				pj.vendor_id,
				pj.vendor_name,
				pj.area_id,
				pj.area_name
		),
		vendor_detail AS (
			SELECT
				aj.site_id,
				aj.vendor_id,
				(
					SELECT COUNT(DISTINCT fa.user_id)
					FROM filtered_assignment fa
					WHERE fa.site_id = aj.site_id
					  AND fa.vendor_id = aj.vendor_id
				) AS total_worker,
				COALESCE(SUM(aj.total_report_per_area), 0) AS total_report,
				JSONB_BUILD_OBJECT(
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
					),
					'check_point',
						CASE
							-- Check Point global tidak ditampilkan ke WORKER.
							WHEN $7::boolean OR $11::boolean THEN NULL
							ELSE JSONB_BUILD_OBJECT(
								'status',
									CASE
										WHEN GREATEST(
											COALESCE(cps.total_point, 0)
											- COALESCE(cps.total_assigned_point, 0),
											0
										) = 0
										THEN 'COMPLETE'
										ELSE 'INCOMPLETE'
									END,
								'total_area',
									COALESCE(cps.total_area, 0),
								'total_point',
									COALESCE(cps.total_assigned_point, 0),
								'total_unassignment',
									GREATEST(
										COALESCE(cps.total_point, 0)
										- COALESCE(cps.total_assigned_point, 0),
										0
									),
								'unassigned_point_detail',
									COALESCE(
										cps.unassigned_point_detail,
										'[]'::jsonb
									)
							)
						END
				) AS detail
			FROM area_json aj
			LEFT JOIN check_point_summary cps
				ON cps.site_id = aj.site_id
			   AND cps.vendor_id = aj.vendor_id
			GROUP BY
				aj.site_id,
				aj.vendor_id,
				cps.total_area,
				cps.total_point,
				cps.total_assigned_point,
				cps.unassigned_point_detail
		),
		assignment_status AS (
			SELECT
				fa.site_id,
				fa.site_name,
				fa.vendor_id,
				fa.vendor_name,
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
				) AS not_started
			FROM filtered_assignment fa
			GROUP BY
				fa.site_id,
				fa.site_name,
				fa.vendor_id,
				fa.vendor_name
		),
		assignment_total AS (
			SELECT
				ft.site_id,
				ft.vendor_id,
				COUNT(pia.id) AS total_task
			FROM filtered_target ft
			JOIN pipeline_item_assignments pia
				ON pia.pipeline_item_id = ft.pipeline_item_id
			   AND pia.is_active = TRUE
			WHERE ` + dashboardWorkerFilter("pia") + `
			GROUP BY ft.site_id, ft.vendor_id
		),
		target_summary AS (
			SELECT
				ft.site_id,
				ft.site_name,
				ft.vendor_id,
				ft.vendor_name,
				COUNT(DISTINCT ft.area_id) AS total_area,
				COUNT(*) AS total_point
			FROM filtered_target ft
			GROUP BY
				ft.site_id,
				ft.site_name,
				ft.vendor_id,
				ft.vendor_name
		)
		SELECT
			ts.site_id::text,
			ts.site_name,
			ts.vendor_id::text,
			ts.vendor_name,
			COALESCE(at.total_task, 0) AS total_task,
			ts.total_area,
			ts.total_point,
			COALESCE(vd.total_worker, 0) AS total_worker,
			COALESCE(vd.total_report, 0) AS total_report,
			COALESCE(ast.completed, 0) AS completed,
			COALESCE(ast.in_progress, 0) AS in_progress,
			COALESCE(ast.not_started, 0) AS not_started,
			COALESCE(
				ROUND(
					100.0 * COALESCE(ast.completed, 0)
					/ NULLIF(COALESCE(at.total_task, 0), 0),
					2
				),
				0
			) AS progress_percentage,
			COALESCE(vd.detail, '{}'::jsonb) AS vendor_progress_detail
		FROM target_summary ts
		LEFT JOIN assignment_status ast
			ON ast.site_id = ts.site_id
		   AND ast.vendor_id = ts.vendor_id
		LEFT JOIN assignment_total at
			ON at.site_id = ts.site_id
		   AND at.vendor_id = ts.vendor_id
		LEFT JOIN vendor_detail vd
			ON vd.site_id = ts.site_id
		   AND vd.vendor_id = ts.vendor_id
		ORDER BY ts.site_name, ts.vendor_name
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
			&item.TotalArea,
			&item.TotalPoint,
			&item.TotalWorker,
			&item.TotalReport,
			&item.Completed,
			&item.InProgress,
			&item.NotStarted,
			&item.ProgressPercentage,
			&detail,
		); err != nil {
			return nil, err
		}

		item.VendorProgressDetail = append(json.RawMessage(nil), detail...)
		item.StatusCheckPoint = dashboardCheckPointStatusFromDetail(
			item.VendorProgressDetail,
		)

		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, rows.Err()
	}

	return result, nil
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

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
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

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

var _ repository.DashboardRepository = (*DashboardRepositoryImpl)(nil)

func dashboardQueryError(section string, err error) error {
	return fmt.Errorf("dashboard %s: %w", section, err)
}

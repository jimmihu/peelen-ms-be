package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type ScoringRepositoryImpl struct {
	db *sql.DB
}

func NewScoringRepository(db *sql.DB) repository.ScoringRepository {
	return &ScoringRepositoryImpl{db: db}
}

// taskDayFactsSQL builds one fact row per active assignment per day between
// the given date parameters. It is the single definition used both to store
// final facts and to compute live facts, so both always agree.
func taskDayFactsSQL(fromParam, toParam int) string {
	return fmt.Sprintf(`
		SELECT a.id AS assignment_id, d::date AS report_date,
			p.vendor_id, p.site_id, p.id AS pipeline_id, a.user_id AS worker_id,
			coalesce(r.reported, false) AS reported,
			coalesce(r.completed, false) AS completed,
			coalesce(r.on_time, false) AS on_time,
			r.late_minutes,
			coalesce(r.late_upload, false) AS late_upload,
			coalesce(r.evidence_complete, false) AS evidence_complete,
			coalesce(r.k3_finding, false) AS k3_finding,
			coalesce(r.k3_finding_with_notes, false) AS k3_finding_with_notes
		FROM generate_series($%[1]d::date, $%[2]d::date, interval '1 day') AS d
		JOIN pipeline_item_assignments a ON a.is_active AND a.created_at::date <= d::date
		JOIN pipeline_items pi ON pi.id = a.pipeline_item_id AND pi.status = 'ACTIVE'
		JOIN pipelines p ON p.id = pi.pipeline_id AND p.status = 'ACTIVE'
		JOIN items i ON i.id = pi.item_id AND i.status = 'ACTIVE'
		LEFT JOIN LATERAL (
			SELECT bool_or(true) AS reported,
				bool_or(pr.status = 'COMPLETED') AS completed,
				bool_or(pr.status = 'COMPLETED' AND det.closed_date <= se.shift_end) AS on_time,
				min(CASE WHEN pr.status = 'COMPLETED' AND det.closed_date IS NOT NULL
					THEN greatest(0, ceil(extract(epoch FROM det.closed_date - se.shift_end) / 60))::int END) AS late_minutes,
				bool_or(pr.status_revisi = 'SUBMITTED') AS late_upload,
				bool_or(pr.status = 'COMPLETED' AND det.evidence_before IS NOT NULL AND det.evidence_after IS NOT NULL) AS evidence_complete,
				bool_or(det.note_category IS NOT NULL) AS k3_finding,
				bool_or(det.note_category IS NOT NULL AND nullif(btrim(det.notes), '') IS NOT NULL) AS k3_finding_with_notes
			FROM pipeline_reports pr
			JOIN shifts sh ON sh.shift = pr.shift
			LEFT JOIN pipeline_report_details det ON det.pipeline_report_id = pr.id
			CROSS JOIN LATERAL (SELECT (pr.report_date + sh.end_time)
				+ CASE WHEN sh.end_time <= sh.start_time THEN interval '1 day' ELSE interval '0' END AS shift_end) se
			WHERE pr.pipeline_item_assignment_id = a.id AND pr.report_date = d::date
		) r ON true`, fromParam, toParam)
}

// lastFinalDateSQL is the latest date whose upload windows have all closed:
// date + 1 day + the latest shift start must be in the past.
const lastFinalDateSQL = `(localtimestamp - interval '1 day' - (SELECT coalesce(max(start_time), '00:00')::interval FROM shifts))::date`

func (r *ScoringRepositoryImpl) LastFinalDate(ctx context.Context) (time.Time, error) {
	var day time.Time
	err := r.db.QueryRowContext(ctx, `SELECT `+lastFinalDateSQL).Scan(&day)
	return day, err
}

// FinalizeFacts stores facts for every final day not yet stored, starting at
// the earliest assignment (or the day after the latest stored fact).
func (r *ScoringRepositoryImpl) FinalizeFacts(ctx context.Context) (int64, error) {
	var from sql.NullTime
	var to time.Time
	if err := r.db.QueryRowContext(ctx, `
		SELECT coalesce((SELECT max(report_date) + 1 FROM task_day_facts),
			(SELECT min(created_at)::date FROM pipeline_item_assignments)), `+lastFinalDateSQL,
	).Scan(&from, &to); err != nil {
		return 0, err
	}
	if !from.Valid || from.Time.After(to) {
		return 0, nil
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO task_day_facts (assignment_id, report_date, vendor_id, site_id, pipeline_id, worker_id,
			reported, completed, on_time, late_minutes, late_upload, evidence_complete, k3_finding, k3_finding_with_notes)
		`+taskDayFactsSQL(1, 2)+`
		ON CONFLICT (assignment_id, report_date) DO NOTHING
	`, from.Time, to)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// AggregateFacts sums facts per scope group. Stored facts cover dates up to
// the last final date; later dates are computed live with the same SQL.
func (r *ScoringRepositoryImpl) AggregateFacts(ctx context.Context, q entity.FactQuery) ([]entity.ScopeCounts, error) {
	keys := map[string]string{
		entity.ScopeVendor: "vendor_id, NULL::uuid AS site_id, NULL::uuid AS worker_id",
		entity.ScopeSite:   "vendor_id, site_id, NULL::uuid AS worker_id",
		entity.ScopeWorker: "vendor_id, site_id, worker_id",
	}
	groupBy := map[string]string{
		entity.ScopeVendor: "vendor_id",
		entity.ScopeSite:   "vendor_id, site_id",
		entity.ScopeWorker: "vendor_id, site_id, worker_id",
	}
	keySQL, ok := keys[q.Scope]
	if !ok {
		return nil, entity.ErrInvalidOperationalData
	}
	args := []any{q.From, q.To, q.VendorID, pq.Array(q.SiteIDs), pq.Array(q.ExcludeSiteIDs)}
	query := `
		WITH last_final AS (SELECT ` + lastFinalDateSQL + ` AS day),
		facts AS (
			SELECT assignment_id, report_date, vendor_id, site_id, worker_id, reported, completed, on_time,
				late_minutes, late_upload, evidence_complete, k3_finding, k3_finding_with_notes
			FROM task_day_facts, last_final
			WHERE report_date BETWEEN $1::date AND least($2::date, last_final.day)
			UNION ALL
			SELECT live.assignment_id, live.report_date, live.vendor_id, live.site_id, live.worker_id, live.reported,
				live.completed, live.on_time, live.late_minutes, live.late_upload, live.evidence_complete,
				live.k3_finding, live.k3_finding_with_notes
			FROM (` + taskDayFactsSQL(1, 2) + `) live, last_final
			WHERE live.report_date > last_final.day
		),
		scoped AS (
			SELECT * FROM facts
			WHERE vendor_id = $3
				AND (coalesce(cardinality($4::uuid[]), 0) = 0 OR site_id = ANY($4::uuid[]))
				AND NOT coalesce(site_id = ANY($5::uuid[]), false)
		),
		daily AS (
			SELECT ` + groupBy[q.Scope] + `, report_date,
				count(*) AS tasks, count(*) FILTER (WHERE reported) AS reported,
				count(*) FILTER (WHERE completed) AS completed, count(*) FILTER (WHERE on_time) AS on_time,
				count(*) FILTER (WHERE late_upload) AS late_upload,
				count(*) FILTER (WHERE evidence_complete) AS evidence_complete,
				count(*) FILTER (WHERE k3_finding) AS k3, count(*) FILTER (WHERE k3_finding_with_notes) AS k3_notes,
				coalesce(sum(late_minutes), 0) AS late_sum, count(late_minutes) AS late_count
			FROM scoped GROUP BY ` + groupBy[q.Scope] + `, report_date
		)
		SELECT ` + keySQL + `,
			sum(tasks), sum(reported), sum(completed), sum(on_time), sum(late_upload), sum(evidence_complete),
			sum(k3), sum(k3_notes), sum(late_sum), sum(late_count),
			array_agg(100.0 * on_time / tasks ORDER BY report_date)
		FROM daily GROUP BY ` + groupBy[q.Scope]
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []entity.ScopeCounts
	for rows.Next() {
		var sc entity.ScopeCounts
		var siteID, workerID sql.NullString
		var rates pq.Float64Array
		c := &sc.Counts
		if err := rows.Scan(&sc.VendorID, &siteID, &workerID, &c.Tasks, &c.Reported, &c.Completed, &c.OnTime,
			&c.LateUpload, &c.EvidenceComplete, &c.K3Findings, &c.K3FindingsWithNotes,
			&c.LateMinutesSum, &c.LateMinutesCount, &rates); err != nil {
			return nil, err
		}
		sc.SiteID = nullStringPtr(siteID)
		sc.WorkerID = nullStringPtr(workerID)
		c.DailyOnTimeRates = []float64(rates)
		result = append(result, sc)
	}
	return result, rows.Err()
}

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"peelen-ms-be/internal/domain/entity"
)

// scoringFixture creates one vendor, site, pipeline, item, worker, and
// assignment created 10 days ago, and returns their ids.
type scoringFixture struct{ vendorID, siteID, pipelineItemID, workerID, assignmentID string }

func openScoringDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`SET TIME ZONE 'Asia/Jakarta'`); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // keep the session time zone on every query
	return db
}

func newScoringFixture(t *testing.T, db *sql.DB) scoringFixture {
	t.Helper()
	ctx := context.Background()
	code := time.Now().Format("150405.000")
	var f scoringFixture
	var pipelineID, itemID string
	steps := []struct {
		query string
		args  []any
		dest  *string
	}{
		{`INSERT INTO sites (code, name) VALUES ('S' || $1, 'Fact Site') RETURNING id::text`, []any{code}, &f.siteID},
		{`INSERT INTO vendors (code, name) VALUES ('V' || $1, 'Fact Vendor') RETURNING id::text`, []any{code}, &f.vendorID},
		{`INSERT INTO users (username, password_hash) VALUES ('fact.' || $1, 'x') RETURNING id::text`, []any{code}, &f.workerID},
		{`INSERT INTO items (name) VALUES ('Fact Item ' || $1) RETURNING id::text`, []any{code}, &itemID},
	}
	for _, s := range steps {
		if err := db.QueryRowContext(ctx, s.query, s.args...).Scan(s.dest); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, db, `INSERT INTO vendor_sites (vendor_id, site_id) VALUES ($1, $2)`, f.vendorID, f.siteID)
	if err := db.QueryRowContext(ctx, `INSERT INTO pipelines (vendor_id, site_id, name) VALUES ($1, $2, 'Fact Area') RETURNING id::text`, f.vendorID, f.siteID).Scan(&pipelineID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO pipeline_items (pipeline_id, item_id) VALUES ($1, $2) RETURNING id::text`, pipelineID, itemID).Scan(&f.pipelineItemID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO pipeline_item_assignments (pipeline_item_id, user_id, created_at) VALUES ($1, $2, current_date - 10) RETURNING id::text`, f.pipelineItemID, f.workerID).Scan(&f.assignmentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM task_day_facts WHERE assignment_id = $1`,
			`DELETE FROM pipeline_report_details WHERE pipeline_report_id IN (SELECT id FROM pipeline_reports WHERE pipeline_item_assignment_id = $1)`,
			`DELETE FROM pipeline_report_history WHERE pipeline_report_id IN (SELECT id FROM pipeline_reports WHERE pipeline_item_assignment_id = $1)`,
			`DELETE FROM pipeline_reports WHERE pipeline_item_assignment_id = $1`,
			`DELETE FROM pipeline_item_assignments WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, f.assignmentID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM pipeline_items WHERE id = $1`, f.pipelineItemID)
		_, _ = db.ExecContext(ctx, `DELETE FROM pipelines WHERE vendor_id = $1`, f.vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM items WHERE id = $1`, itemID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vendor_sites WHERE vendor_id = $1`, f.vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, f.workerID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vendors WHERE id = $1`, f.vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM sites WHERE id = $1`, f.siteID)
	})
	return f
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

// addReport inserts a report for the fixture on daysAgo with a closed time
// offset from the report date's midnight.
func addReport(t *testing.T, db *sql.DB, f scoringFixture, daysAgo int, shift, status string, closedAfterMidnight *time.Duration) {
	t.Helper()
	var reportID string
	if err := db.QueryRow(`INSERT INTO pipeline_reports (pipeline_item_assignment_id, shift, report_date, status)
		VALUES ($1, $2, current_date - $3::int, $4) RETURNING id::text`, f.assignmentID, shift, daysAgo, status).Scan(&reportID); err != nil {
		t.Fatal(err)
	}
	var closed any
	if closedAfterMidnight != nil {
		closed = closedAfterMidnight.String()
	}
	mustExec(t, db, `INSERT INTO pipeline_report_details (pipeline_report_id, evidence_before, evidence_after, closed_date)
		VALUES ($1, 'b.jpg', 'a.jpg', (current_date - $2::int) + $3::interval)`, reportID, daysAgo, closed)
}

func TestFactsEveningShiftAndMissingReport(t *testing.T) {
	db := openScoringDB(t)
	f := newScoringFixture(t, db)
	repo := NewScoringRepository(db)
	ctx := context.Background()

	onTime := 23*time.Hour + 50*time.Minute // 23:50 same day, EVENING ends 00:00 next day
	late := 24*time.Hour + 10*time.Minute   // 00:10 next day
	addReport(t, db, f, 5, "EVENING", "COMPLETED", &onTime)
	addReport(t, db, f, 4, "EVENING", "COMPLETED", &late)
	// day 3: no report at all

	if _, err := repo.FinalizeFacts(ctx); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		onTime, reported bool
		late             sql.NullInt64
	}
	result, err := db.Query(`SELECT on_time, reported, late_minutes FROM task_day_facts
		WHERE assignment_id = $1 AND report_date BETWEEN current_date - 5 AND current_date - 3 ORDER BY report_date`, f.assignmentID)
	if err != nil {
		t.Fatal(err)
	}
	for result.Next() {
		var r struct {
			onTime, reported bool
			late             sql.NullInt64
		}
		if err := result.Scan(&r.onTime, &r.reported, &r.late); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d fact rows, want 3", len(rows))
	}
	if !rows[0].onTime || rows[0].late.Int64 != 0 {
		t.Errorf("23:50 EVENING close: %+v, want on time", rows[0])
	}
	if rows[1].onTime || rows[1].late.Int64 != 10 {
		t.Errorf("00:10 close: %+v, want 10 minutes late", rows[1])
	}
	if rows[2].reported {
		t.Errorf("day without report: %+v, want reported=false", rows[2])
	}

	// The stored/live boundary is what finalization actually stored, not a fresh
	// clock read, so a period can never be frozen with a day missing.
	through, err := repo.LastFinalDate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var stored time.Time
	if err := db.QueryRow(`SELECT facts_through FROM scoring_job_state`).Scan(&stored); err != nil {
		t.Fatalf("facts_through not recorded: %v", err)
	}
	if !through.Equal(stored) {
		t.Fatalf("LastFinalDate = %s, want stored facts_through %s", through, stored)
	}

	// Idempotent: a second run inserts nothing.
	if n, err := repo.FinalizeFacts(ctx); err != nil || n != 0 {
		t.Fatalf("second finalize inserted %d (err %v), want 0", n, err)
	}

	// Aggregate (vendor scope) covers final facts and live days up to today.
	counts, err := repo.AggregateFacts(ctx, entity.FactQuery{
		Scope: entity.ScopeVendor, VendorID: f.vendorID,
		From: time.Now().AddDate(0, 0, -5), To: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].Counts.Tasks != 6 || counts[0].Counts.OnTime != 1 || counts[0].Counts.Completed != 2 {
		t.Fatalf("aggregate = %+v, want 6 tasks (5 days ago..today), 1 on time, 2 completed", counts)
	}
}

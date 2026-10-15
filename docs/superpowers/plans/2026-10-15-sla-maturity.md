# SLA & Maturity Scoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Super Admin/Admin configure per-vendor (with per-site override) SLA and maturity rules, compute daily task facts, and expose live and frozen SLA scores and maturity levels per vendor, site, and worker.

**Architecture:** A daily fact grain (`task_day_facts`, one row per assignment × date) is produced by one shared SQL query, both for finalization (stored) and for live views (not stored). A pure Go package `internal/scoring` turns aggregated fact counts into indicator scores, SLA scores, maturity scores, and gate-based levels. A background job (15-minute ticker, Postgres advisory lock) finalizes facts and freezes closed periods into snapshot tables; HTTP endpoints manage versioned rules and read live/frozen results.

**Tech Stack:** Go 1.x, Gin, `database/sql` + `lib/pq`, PostgreSQL. Tests: `go test`, repository tests gated on `TEST_POSTGRES_DSN`.

**Spec:** `docs/SLA-Maturity-Dokumen-Teknis.docx` (technical) and `docs/SLA-Maturity-Ringkasan-Bisnis.docx` (business).

**Deviation from spec (implementation detail, flagged to the user):** the spec names separate tables per kind (`sla_rules`, `maturity_rules`, `sla_period_results`, …). This plan stores both kinds in shared tables with a `kind` column (`score_rules`, `score_rule_items`, `score_rule_levels`, `score_period_results`, `score_period_item_results`). Behavior is identical; it halves the repository code.

## Global Constraints

- Rule kinds: `SLA`, `MATURITY`. Period types: `WEEK` (ISO, Monday start), `MONTH`, `QUARTER` (calendar).
- Scope types: `VENDOR`, `SITE`, `WORKER`. Result status: `OK`, `NO_DATA`; live-only status `NO_RULE`.
- Item weights per rule must total exactly 100; each weight > 0.
- Targets: percent indicators 0–100; `AVG_LATE_MINUTES` ≥ 0. Maturity dimensions have no target.
- Default maturity levels: L2 (60, 50, 1), L3 (75, 65, 2), L4 (85, 75, 3), L5 (95, 85, 3) as (min_score, min_dimension, consecutive_periods). Each column must be non-decreasing from L2 to L5.
- A task-day is final at `report_date + 1 day + max(shifts.start_time)` (local time; DB session time zone = `APP_TIMEZONE`).
- A period is scored with the rule version active at the period's end (`period_end + 1 day` at 00:00 local).
- Only `SUPER_ADMIN` writes vendor-default rules; `ADMIN` writes site overrides only for sites in its scope whose vendor is linked to that site.
- Read access: `SUPER_ADMIN` all; `ADMIN` vendors linked to its sites, its sites, workers at its sites; `ADMIN_WORKER` own vendors; `WORKER` own worker results.
- Do not add Go module dependencies (the repo vendors its modules).

## Review Focus

1. **Vendor site list edited after pipelines exist** — removing a site from a vendor that still has active pipelines at that site must be rejected (409), not orphan the pipelines. Test added in Task 1.
2. **Weights like 33.33 + 33.33 + 33.34** — float rounding must still accept a total of 100. Compare with a 0.01 tolerance. Test in Task 6.
3. **Rule created mid-period** — the period containing the rule's creation is scored in full with that rule; periods ending before creation are never scored. Test in Task 9.
4. **Worker with tasks at two sites** — produces two WORKER results (one per site), each with that site's rule. Test in Task 8.
5. **EVENING shift crossing midnight** — a task closed at 23:50 on the report date for a 16:00–00:00 shift is on time; closed at 00:10 next day is 10 minutes late. Test in Task 5.

---

## File Structure

| File | Responsibility |
|---|---|
| `ddl/migrations/006_pipeline_site.sql` | Add `pipelines.site_id` |
| `ddl/migrations/007_scoring.sql` | Rule, fact, and snapshot tables |
| `ddl/init.sql` | Same schema for fresh installs |
| `internal/scoring/catalog.go` | Indicator catalog |
| `internal/scoring/sla.go` | Counts, actual values, indicator & SLA scores |
| `internal/scoring/maturity.go` | Dimension scores, maturity score, gate levels, level validation |
| `internal/scoring/period.go` | Period bounds and stepping |
| `internal/domain/entity/scoring.go` | Rule, result, filter entities |
| `internal/domain/repository/scoring_repository.go` | Repository interface |
| `internal/repository/postgres/scoring_facts.go` | Shared fact SQL, finalization, aggregation |
| `internal/repository/postgres/scoring_repository.go` | Rules CRUD, snapshots, rule lookups, advisory lock |
| `internal/usecase/scoring_usecase.go` | Rule validation, access control, evaluation, live/history, job steps |
| `internal/delivery/http/handler/scoring_handler.go` | HTTP endpoints |
| `internal/jobs/scoring_job.go` | Ticker + lock around job steps |
| `cmd/server/main.go`, `internal/delivery/http/router/router.go` | Wiring |
| `ddl/seed_v3.sql` | 90 days of reports, pipelines with sites, sample rules |
| `readme.md`, `postman/peelen-ms-be.postman_collection.json` | Docs |

---

### Task 1: Pipelines belong to a site

**Files:**
- Create: `ddl/migrations/006_pipeline_site.sql`
- Modify: `ddl/init.sql` (pipelines table), `internal/domain/entity/operational.go` (OperationalRecord, OperationalInput), `internal/usecase/operational_usecase.go` (field sets, validation), `internal/repository/postgres/operational_repository.go` (select columns, scan, create, filters), `internal/repository/postgres/workflow_repository.go:226-240` (vendor sites update guard)
- Test: `internal/usecase/operational_usecase_test.go`, `internal/repository/postgres/operational_repository_test.go`

**Interfaces:**
- Produces: `pipelines.site_id uuid NOT NULL`; `entity.OperationalRecord.SiteID *string` (`json:"site_id,omitempty"`); `entity.OperationalInput.SiteID *string` (`json:"site_id"`); pipeline create requires `site_id`.

- [ ] **Step 1: Write the migration**

```sql
-- ddl/migrations/006_pipeline_site.sql
BEGIN;

-- Each pipeline is a work area of one vendor at one site, so scores can be
-- grouped per site.
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS site_id uuid REFERENCES sites(id) ON DELETE RESTRICT;

-- Vendors with exactly one site: assign it automatically.
UPDATE pipelines p
SET site_id = vs.site_id
FROM (
    SELECT vendor_id, min(site_id::text)::uuid AS site_id
    FROM vendor_sites GROUP BY vendor_id HAVING count(*) = 1
) vs
WHERE p.vendor_id = vs.vendor_id AND p.site_id IS NULL;

DO $$
DECLARE missing text;
BEGIN
    SELECT string_agg(id::text || ' (' || name || ')', ', ') INTO missing
    FROM pipelines WHERE site_id IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'Set pipelines.site_id manually before running this migration: %', missing;
    END IF;
END $$;

ALTER TABLE pipelines ALTER COLUMN site_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_pipelines_site_id ON pipelines(site_id);

COMMIT;
```

- [ ] **Step 2: Mirror it in `ddl/init.sql`**

In `CREATE TABLE IF NOT EXISTS pipelines (...)` add after `vendor_id`:

```sql
    site_id     uuid NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
```

and after the table:

```sql
CREATE INDEX IF NOT EXISTS idx_pipelines_site_id ON pipelines(site_id);
```

- [ ] **Step 3: Write the failing usecase test**

Append to `internal/usecase/operational_usecase_test.go`:

```go
func TestPipelineCreateRequiresSite(t *testing.T) {
	vendor := "10000000-0000-4000-8000-000000000001"
	name := "Boiler Area"
	err := validateOperationalInput(entity.ResourcePipeline, entity.OperationalInput{VendorID: &vendor, Name: &name}, true)
	if err == nil {
		t.Fatal("expected pipeline without site_id to be rejected")
	}
	site := "20000000-0000-4000-8000-000000000001"
	if err := validateOperationalInput(entity.ResourcePipeline, entity.OperationalInput{VendorID: &vendor, Name: &name, SiteID: &site}, true); err != nil {
		t.Fatalf("valid pipeline rejected: %v", err)
	}
	bad := "not-a-uuid"
	if err := validateOperationalInput(entity.ResourcePipeline, entity.OperationalInput{VendorID: &vendor, Name: &name, SiteID: &bad}, true); err == nil {
		t.Fatal("expected invalid site_id to be rejected")
	}
}
```

- [ ] **Step 4: Run it — expect a compile failure (`SiteID` undefined)**

Run: `go test ./internal/usecase/ -run TestPipelineCreateRequiresSite`
Expected: FAIL, `unknown field SiteID`.

- [ ] **Step 5: Add `SiteID` to the entities**

In `internal/domain/entity/operational.go`, `OperationalRecord` add after `VendorID`:

```go
	SiteID      *string   `json:"site_id,omitempty"`
```

and `OperationalInput` add after `VendorID`:

```go
	SiteID      *string `json:"site_id"`
```

- [ ] **Step 6: Allow and validate `site_id` for pipelines**

In `internal/usecase/operational_usecase.go`:

1. `operationalFieldsAllowed`, creating, `ResourcePipeline`: `operationalFieldSet("vendor_id", "site_id", "name", "description")`.
2. `hasOnlyOperationalFields` map: add `"site_id": input.SiteID != nil,`.
3. `inputIsEmpty`: add `&& input.SiteID == nil` to the expression.
4. In `validateOperationalInput`, extend the UUID check:

```go
	if input.VendorID != nil && !scopeUUIDPattern.MatchString(*input.VendorID) ||
		input.SiteID != nil && !scopeUUIDPattern.MatchString(*input.SiteID) ||
		input.PipelineID != nil && !scopeUUIDPattern.MatchString(*input.PipelineID) ||
		input.ItemID != nil && !scopeUUIDPattern.MatchString(*input.ItemID) {
		return ErrInvalidOperationalData
	}
```

5. In the creating switch, split pipeline from policy:

```go
		case entity.ResourcePolicy:
			if !hasText(input.VendorID) || !hasText(input.Name) || !hasText(input.FileURL) {
				return ErrInvalidOperationalData
			}
		case entity.ResourcePipeline:
			if !hasText(input.VendorID) || !hasText(input.SiteID) || !hasText(input.Name) {
				return ErrInvalidOperationalData
			}
```

- [ ] **Step 7: Run the usecase test — expect PASS**

Run: `go test ./internal/usecase/ -run TestPipelineCreateRequiresSite`
Expected: PASS.

- [ ] **Step 8: Select, scan, and create `site_id` in the repository**

In `internal/repository/postgres/operational_repository.go`:

1. Every `operationalSelect(...)` call gets an 18th column, `site_id`, inserted right after the vendor column (2nd argument): `"t.site_id"` for `ResourcePipeline`, `"p.site_id"` for `ResourcePipelineItem`, `"NULL::uuid"` for inventory, policy, and item.
2. `scanOperationalRecord`: declare `siteID` in the `sql.NullString` list, scan it right after `&vendorID`, and set `record.SiteID = operationalStringPtr(siteID)`.
3. Pipeline create — the site must be linked to the vendor:

```go
	case entity.ResourcePipeline:
		err = tx.QueryRowContext(ctx, `
			INSERT INTO pipelines (vendor_id, site_id, name, description, created_by)
			SELECT v.id, vs.site_id, $5, $6, $7
			FROM vendors v
			JOIN vendor_sites vs ON vs.vendor_id = v.id AND vs.site_id = $8
			WHERE v.id = $4 AND `+scopePredicate("v.id", 1)+`
			RETURNING id::text
		`, hasOperationalRole(access.Roles, "SUPER_ADMIN"), pq.Array(access.VendorIDs), pq.Array(access.SiteIDs),
			*input.VendorID, strings.TrimSpace(*input.Name), input.Description, access.UserID, *input.SiteID).Scan(&id)
```

4. `ListOperational` site filter: pipelines filter on their own site now:

```go
		case entity.ResourcePipeline:
			where = append(where, fmt.Sprintf("t.site_id = $%d", next))
		case entity.ResourceInventory, entity.ResourcePolicy:
			where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM vendor_sites vs WHERE vs.vendor_id = t.vendor_id AND vs.site_id = $%d)", next))
		case entity.ResourcePipelineItem:
			where = append(where, fmt.Sprintf("p.site_id = $%d", next))
```

- [ ] **Step 9: Guard vendor site edits (Review Focus 1)**

In `internal/repository/postgres/workflow_repository.go`, in the function that runs `DELETE FROM vendor_sites WHERE vendor_id = $1` and re-inserts sites, add right after the re-insert loop (still inside the transaction):

```go
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
```

- [ ] **Step 10: Repository test for create + guard (DB-gated)**

Append to `internal/repository/postgres/operational_repository_test.go` a test following the existing `TEST_POSTGRES_DSN` pattern in that file (skip when unset). It must:
1. Insert a site, a vendor, and a `vendor_sites` row; create a pipeline through `repo.CreateOperational(ctx, superAdminAccess(), entity.ResourcePipeline, input)` with `SiteID` set; assert `record.SiteID` equals the site.
2. Create a second site not linked to the vendor; assert creating a pipeline with it returns `repository.ErrNotFound`.
3. Call the workflow repository's vendor update with an empty site list; assert `repository.ErrConflict`.
4. Clean up rows in reverse order in `t.Cleanup`.

```go
func TestPipelineSiteLinkAndVendorSiteGuard(t *testing.T) {
	db := openTestDB(t) // helper: same as the TEST_POSTGRES_DSN block used in this file; skip when unset
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var siteID, otherSiteID, vendorID string
	mustScan(t, db.QueryRowContext(ctx, `INSERT INTO sites (code, name) VALUES ($1, 'Test Site') RETURNING id::text`, "TS"+suffix[len(suffix)-6:]), &siteID)
	mustScan(t, db.QueryRowContext(ctx, `INSERT INTO sites (code, name) VALUES ($1, 'Other Site') RETURNING id::text`, "TO"+suffix[len(suffix)-6:]), &otherSiteID)
	mustScan(t, db.QueryRowContext(ctx, `INSERT INTO vendors (code, name) VALUES ($1, 'Test Vendor') RETURNING id::text`, "TV"+suffix[len(suffix)-6:]), &vendorID)
	if _, err := db.ExecContext(ctx, `INSERT INTO vendor_sites (vendor_id, site_id) VALUES ($1, $2)`, vendorID, siteID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM pipelines WHERE vendor_id = $1`, vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vendor_sites WHERE vendor_id = $1`, vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vendors WHERE id = $1`, vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM sites WHERE id IN ($1, $2)`, siteID, otherSiteID)
	})

	repo := NewOperationalRepository(db)
	name := "Boiler Area"
	record, err := repo.CreateOperational(ctx, superAdminAccess(), entity.ResourcePipeline,
		entity.OperationalInput{VendorID: &vendorID, SiteID: &siteID, Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if record.SiteID == nil || *record.SiteID != siteID {
		t.Fatalf("site_id = %v, want %s", record.SiteID, siteID)
	}
	_, err = repo.CreateOperational(ctx, superAdminAccess(), entity.ResourcePipeline,
		entity.OperationalInput{VendorID: &vendorID, SiteID: &otherSiteID, Name: &name})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unlinked site: err = %v, want ErrNotFound", err)
	}
	workflow := NewWorkflowRepository(db)
	if _, err := workflow.UpdateVendor(ctx, vendorID, entity.Vendor{SiteIDs: []string{}}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("removing used site: err = %v, want ErrConflict", err)
	}
}
```

Before writing, open `operational_repository_test.go` and `workflow_repository.go`: use the file's actual DSN/open helper and the actual method name and signature of the vendor-update function that contains the `DELETE FROM vendor_sites` statement (adapt the last call to it). Add `openTestDB`/`mustScan` helpers only if the file has none.

- [ ] **Step 11: Update the seed pipelines insert**

In `ddl/seed_v3.sql`, the `INSERT INTO pipelines` statement creates one pipeline set **per vendor site** (so PT Mitra gets 9 areas at Paiton and 9 at Pacitan):

```sql
INSERT INTO pipelines (vendor_id, site_id, name, created_by)
SELECT v.id, vs.site_id, f.pipeline_name, aw.id
FROM vendors v
JOIN vendor_sites vs ON vs.vendor_id = v.id
CROSS JOIN (SELECT DISTINCT pipeline_name FROM pipeline_fixture) f
JOIN user_scopes us ON us.vendor_id = v.id
JOIN users aw ON aw.id = us.user_id AND aw.username LIKE 'aw.%';
```

Also add `ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS site_id uuid REFERENCES sites(id);` near the other constraint-sync statements at the top of the seed (the truncate already empties the table), and keep the worker-to-pipeline mapping valid: in the `numbered_pipelines` CTE, partition by `p.vendor_id, p.site_id` and add `JOIN vendor_sites` ordering so workers 01–05 take the vendor's first site (ordered by site code) and workers 06–10 the second:

```sql
WITH vendor_site_no AS (
    SELECT vs.vendor_id, vs.site_id,
           row_number() OVER (PARTITION BY vs.vendor_id ORDER BY s.code) AS site_no
    FROM vendor_sites vs JOIN sites s ON s.id = vs.site_id
),
numbered_pipelines AS (
    SELECT p.*, vsn.site_no,
           row_number() OVER (PARTITION BY p.vendor_id, p.site_id ORDER BY p.name) AS pipeline_no
    FROM pipelines p
    JOIN vendor_site_no vsn ON vsn.vendor_id = p.vendor_id AND vsn.site_id = p.site_id
)
INSERT INTO pipeline_item_assignments (pipeline_item_id, user_id, assigned_by, created_at, updated_at)
SELECT pi.id, w.id, np.created_by, current_date - 100, current_date - 100
FROM users w
JOIN user_scopes ws ON ws.user_id = w.id
JOIN numbered_pipelines np ON np.vendor_id = ws.vendor_id
    AND np.site_no = CASE WHEN substring(w.username FROM 10 FOR 2)::int <= 5 THEN 1 ELSE 2 END
    AND np.pipeline_no = (substring(w.username FROM 10 FOR 2)::int - 1) % 5 + 1
JOIN pipeline_items pi ON pi.pipeline_id = np.id
WHERE w.username LIKE 'w.vnd%';
```

(Worker 01–05 at site 1 take areas 1–5; 06–10 at site 2 take areas 1–5. Assignment dates move to 100 days back for Task 10.)

- [ ] **Step 12: Run all tests and vet**

Run: `go vet ./internal/... ./cmd/... && go test ./internal/...`
Expected: all PASS (DB tests SKIP without `TEST_POSTGRES_DSN`).

- [ ] **Step 13: Commit**

```bash
git add ddl/migrations/006_pipeline_site.sql ddl/init.sql ddl/seed_v3.sql internal/domain/entity/operational.go internal/usecase/operational_usecase.go internal/usecase/operational_usecase_test.go internal/repository/postgres/operational_repository.go internal/repository/postgres/operational_repository_test.go internal/repository/postgres/workflow_repository.go
git commit -m "feat: pipelines belong to a vendor site"
```

---

### Task 2: Scoring catalog, counts, and SLA scores (pure Go)

**Files:**
- Create: `internal/scoring/catalog.go`, `internal/scoring/sla.go`
- Test: `internal/scoring/sla_test.go`

**Interfaces:**
- Produces:

```go
type Direction int // HigherBetter, LowerBetter
type Unit string   // UnitPercent "PERCENT", UnitMinutes "MINUTES", UnitScore "SCORE"
type Indicator struct { Code, Name string; Unit Unit; Direction Direction; SLA, Maturity bool }
var Catalog []Indicator
func Lookup(code string) (Indicator, bool)

type Counts struct {
	Tasks, Reported, Completed, OnTime, LateUpload, EvidenceComplete int
	K3Findings, K3FindingsWithNotes int
	LateMinutesSum, LateMinutesCount int
	DailyOnTimeRates []float64 // one value per day that had tasks, 0..100
}
func Actual(code string, c Counts) (value float64, ok bool) // ok=false means N/A

type Item struct { Code string; Weight float64; Target float64 }
type ItemResult struct { Code string; Actual, Target, Score, Weight float64; Met, NA bool }
type Result struct { Status string; Score float64; Compliant bool; TaskCount int; Items []ItemResult }
const StatusOK, StatusNoData = "OK", "NO_DATA"
func EvaluateSLA(c Counts, items []Item) Result
```

- [ ] **Step 1: Write the failing tests**

```go
// internal/scoring/sla_test.go
package scoring

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestActual(t *testing.T) {
	c := Counts{Tasks: 10, Reported: 9, Completed: 8, OnTime: 6, LateUpload: 3, EvidenceComplete: 7,
		K3Findings: 4, K3FindingsWithNotes: 3, LateMinutesSum: 40, LateMinutesCount: 8,
		DailyOnTimeRates: []float64{50, 70}}
	cases := map[string]float64{
		"COMPLETION_RATE": 80, "ON_TIME_RATE": 60, "AVG_LATE_MINUTES": 5,
		"LATE_UPLOAD_RATE": 33.33, "EVIDENCE_COMPLETE_RATE": 87.5, "K3_FINDING_NOTES_RATE": 75,
		"CONSISTENCY": 80, // stddev(50,70)=10 -> 100-2*10
	}
	for code, want := range cases {
		got, ok := Actual(code, c)
		if !ok || !near(got, want) {
			t.Errorf("%s = %v (ok=%v), want %v", code, got, ok, want)
		}
	}
}

func TestActualNotApplicable(t *testing.T) {
	c := Counts{Tasks: 5, Reported: 0, Completed: 0, DailyOnTimeRates: []float64{100}}
	for _, code := range []string{"AVG_LATE_MINUTES", "LATE_UPLOAD_RATE", "EVIDENCE_COMPLETE_RATE", "K3_FINDING_NOTES_RATE", "CONSISTENCY"} {
		if _, ok := Actual(code, c); ok {
			t.Errorf("%s should be N/A", code)
		}
	}
	if _, ok := Actual("UNKNOWN", c); ok {
		t.Error("unknown code should be N/A")
	}
}

func TestEvaluateSLA(t *testing.T) {
	// Business doc example: completion 97/95, on-time 85/90, evidence 99/98.
	c := Counts{Tasks: 100, Reported: 100, Completed: 97, OnTime: 85, EvidenceComplete: 96}
	got := EvaluateSLA(c, []Item{
		{Code: "COMPLETION_RATE", Weight: 40, Target: 95},
		{Code: "ON_TIME_RATE", Weight: 40, Target: 90},
		{Code: "EVIDENCE_COMPLETE_RATE", Weight: 20, Target: 98},
	})
	if got.Status != StatusOK || got.Compliant || !near(got.Score, 97.78) {
		t.Fatalf("got %+v", got)
	}
	if got.Items[1].Met || !got.Items[0].Met || !got.Items[2].Met {
		t.Fatalf("met flags wrong: %+v", got.Items)
	}
}

func TestEvaluateSLALowerBetterAndNA(t *testing.T) {
	c := Counts{Tasks: 10, Reported: 10, Completed: 10, OnTime: 10, LateMinutesSum: 100, LateMinutesCount: 10}
	got := EvaluateSLA(c, []Item{
		{Code: "AVG_LATE_MINUTES", Weight: 50, Target: 5},       // actual 10 -> score 50
		{Code: "K3_FINDING_NOTES_RATE", Weight: 50, Target: 100}, // N/A, weight dropped
	})
	if !near(got.Score, 50) || got.Compliant || !got.Items[1].NA {
		t.Fatalf("got %+v", got)
	}
	zero := EvaluateSLA(Counts{Tasks: 1, Completed: 1, LateMinutesCount: 1}, []Item{{Code: "AVG_LATE_MINUTES", Weight: 100, Target: 0}})
	if !near(zero.Score, 100) || !zero.Compliant {
		t.Fatalf("zero target with zero late should pass: %+v", zero)
	}
}

func TestEvaluateSLANoData(t *testing.T) {
	got := EvaluateSLA(Counts{}, []Item{{Code: "COMPLETION_RATE", Weight: 100, Target: 95}})
	if got.Status != StatusNoData {
		t.Fatalf("status = %s, want NO_DATA", got.Status)
	}
}
```

Note: the business doc shows 97,6 with on-time scored as a rounded 94. The exact value is 40·100 + 40·(85/90·100) + 20·100 = 97.78; the test pins the exact value.

- [ ] **Step 2: Run — expect compile failure**

Run: `go test ./internal/scoring/`
Expected: FAIL (`undefined: Counts`).

- [ ] **Step 3: Implement the catalog**

```go
// internal/scoring/catalog.go
// Package scoring turns task fact counts into SLA scores and maturity levels.
// It has no database or HTTP dependencies.
package scoring

type Direction int

const (
	HigherBetter Direction = iota
	LowerBetter
)

type Unit string

const (
	UnitPercent Unit = "PERCENT"
	UnitMinutes Unit = "MINUTES"
	UnitScore   Unit = "SCORE"
)

type Indicator struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Unit      Unit      `json:"unit"`
	Direction Direction `json:"-"`
	SLA       bool      `json:"sla"`
	Maturity  bool      `json:"maturity"`
}

// Catalog is the fixed list of indicators admins can pick from.
var Catalog = []Indicator{
	{Code: "COMPLETION_RATE", Name: "Tingkat penyelesaian", Unit: UnitPercent, Direction: HigherBetter, SLA: true, Maturity: true},
	{Code: "ON_TIME_RATE", Name: "Ketepatan waktu", Unit: UnitPercent, Direction: HigherBetter, SLA: true, Maturity: true},
	{Code: "AVG_LATE_MINUTES", Name: "Rata-rata keterlambatan", Unit: UnitMinutes, Direction: LowerBetter, SLA: true, Maturity: true},
	{Code: "LATE_UPLOAD_RATE", Name: "Upload susulan", Unit: UnitPercent, Direction: LowerBetter, SLA: true, Maturity: true},
	{Code: "EVIDENCE_COMPLETE_RATE", Name: "Kelengkapan foto", Unit: UnitPercent, Direction: HigherBetter, SLA: true, Maturity: true},
	{Code: "K3_FINDING_NOTES_RATE", Name: "Temuan K3 berpenjelasan", Unit: UnitPercent, Direction: HigherBetter, SLA: true, Maturity: true},
	{Code: "CONSISTENCY", Name: "Konsistensi", Unit: UnitScore, Direction: HigherBetter, SLA: false, Maturity: true},
}

func Lookup(code string) (Indicator, bool) {
	for _, indicator := range Catalog {
		if indicator.Code == code {
			return indicator, true
		}
	}
	return Indicator{}, false
}
```

- [ ] **Step 4: Implement counts, actual values, and SLA evaluation**

```go
// internal/scoring/sla.go
package scoring

import "math"

const (
	StatusOK     = "OK"
	StatusNoData = "NO_DATA"
)

// Counts aggregates task_day_facts for one scope and period.
type Counts struct {
	Tasks, Reported, Completed, OnTime, LateUpload, EvidenceComplete int
	K3Findings, K3FindingsWithNotes                                  int
	LateMinutesSum, LateMinutesCount                                 int
	DailyOnTimeRates                                                 []float64
}

func percent(part, whole int) (float64, bool) {
	if whole == 0 {
		return 0, false
	}
	return float64(part) / float64(whole) * 100, true
}

// Actual returns the indicator's raw value; ok is false when it is N/A
// (its denominator is zero) or the code is unknown.
func Actual(code string, c Counts) (float64, bool) {
	switch code {
	case "COMPLETION_RATE":
		return percent(c.Completed, c.Tasks)
	case "ON_TIME_RATE":
		return percent(c.OnTime, c.Tasks)
	case "AVG_LATE_MINUTES":
		if c.LateMinutesCount == 0 {
			return 0, false
		}
		return float64(c.LateMinutesSum) / float64(c.LateMinutesCount), true
	case "LATE_UPLOAD_RATE":
		return percent(c.LateUpload, c.Reported)
	case "EVIDENCE_COMPLETE_RATE":
		return percent(c.EvidenceComplete, c.Completed)
	case "K3_FINDING_NOTES_RATE":
		return percent(c.K3FindingsWithNotes, c.K3Findings)
	case "CONSISTENCY":
		if len(c.DailyOnTimeRates) < 2 {
			return 0, false
		}
		return math.Max(0, 100-2*stddev(c.DailyOnTimeRates)), true
	default:
		return 0, false
	}
}

func stddev(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var squares float64
	for _, v := range values {
		squares += (v - mean) * (v - mean)
	}
	return math.Sqrt(squares / float64(len(values)))
}

type Item struct {
	Code   string
	Weight float64
	Target float64
}

type ItemResult struct {
	Code   string  `json:"code"`
	Actual float64 `json:"actual"`
	Target float64 `json:"target"`
	Score  float64 `json:"score"`
	Weight float64 `json:"weight"`
	Met    bool    `json:"met"`
	NA     bool    `json:"is_na"`
}

type Result struct {
	Status    string       `json:"status"`
	Score     float64      `json:"score"`
	Compliant bool         `json:"compliant"`
	Level     int          `json:"level,omitempty"`
	TaskCount int          `json:"task_count"`
	Items     []ItemResult `json:"items"`
}

// targetScore scores an actual value against its target on 0..100.
func targetScore(direction Direction, actual, target float64) (score float64, met bool) {
	if direction == LowerBetter {
		if actual <= target {
			return 100, true
		}
		return target / actual * 100, false
	}
	if target <= 0 || actual >= target {
		return 100, true
	}
	return actual / target * 100, false
}

// weighted averages item scores over non-N/A items, renormalizing weights.
func weighted(items []ItemResult) float64 {
	var total, weights float64
	for _, item := range items {
		if !item.NA {
			total += item.Score * item.Weight
			weights += item.Weight
		}
	}
	if weights == 0 {
		return 0
	}
	return total / weights
}

// EvaluateSLA scores counts against an SLA rule's items.
func EvaluateSLA(c Counts, items []Item) Result {
	if c.Tasks == 0 {
		return Result{Status: StatusNoData}
	}
	result := Result{Status: StatusOK, Compliant: true, TaskCount: c.Tasks}
	for _, item := range items {
		indicator, _ := Lookup(item.Code)
		actual, ok := Actual(item.Code, c)
		r := ItemResult{Code: item.Code, Target: item.Target, Weight: item.Weight, Actual: actual, NA: !ok}
		if ok {
			r.Score, r.Met = targetScore(indicator.Direction, actual, item.Target)
			if !r.Met {
				result.Compliant = false
			}
		}
		result.Items = append(result.Items, r)
	}
	result.Score = weighted(result.Items)
	return result
}
```

- [ ] **Step 5: Run — expect PASS**

Run: `go test ./internal/scoring/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/scoring/
git commit -m "feat(scoring): indicator catalog and SLA scoring"
```

---

### Task 3: Maturity scores, gate levels, and periods (pure Go)

**Files:**
- Create: `internal/scoring/maturity.go`, `internal/scoring/period.go`
- Test: `internal/scoring/maturity_test.go`, `internal/scoring/period_test.go`

**Interfaces:**
- Consumes: `Counts`, `Item`, `ItemResult`, `Result`, `Actual`, `Lookup`, `weighted` from Task 2.
- Produces:

```go
type LevelRule struct { Level int `json:"level"`; MinScore float64 `json:"min_score"`; MinDimension float64 `json:"min_dimension"`; ConsecutivePeriods int `json:"consecutive_periods"` }
var DefaultLevels []LevelRule
func ValidateLevels(levels []LevelRule) error
func DimensionScore(code string, actual float64) float64
func EvaluateMaturityPeriod(c Counts, items []Item) Result // Items carry dimension scores; Target unused
func Level(history []Result, levels []LevelRule) int     // history oldest..newest
func MaxConsecutive(levels []LevelRule) int

type PeriodType string // PeriodWeek "WEEK", PeriodMonth "MONTH", PeriodQuarter "QUARTER"
func ValidPeriodType(t string) bool
func PeriodBounds(t PeriodType, day time.Time) (start, end time.Time) // end = last day, inclusive; date-only values in day's location
func PreviousStart(t PeriodType, start time.Time) time.Time
```

- [ ] **Step 1: Write the failing maturity tests**

```go
// internal/scoring/maturity_test.go
package scoring

import "testing"

func okPeriod(score float64, dims ...float64) Result {
	r := Result{Status: StatusOK, Score: score}
	for _, d := range dims {
		r.Items = append(r.Items, ItemResult{Score: d})
	}
	return r
}

func TestLevelUsesGates(t *testing.T) {
	cases := []struct {
		name    string
		history []Result
		want    int
	}{
		{"business doc: three strong periods", []Result{okPeriod(88, 80, 90), okPeriod(90, 80, 92), okPeriod(86, 78, 90)}, 4},
		{"business doc: one weak dimension drops to 3", []Result{okPeriod(88, 80, 90), okPeriod(90, 80, 92), okPeriod(86, 78, 90), okPeriod(86, 70, 95)}, 3},
		{"single good period reaches 2 only", []Result{okPeriod(99, 99, 99)}, 2},
		{"below level 2", []Result{okPeriod(55, 60, 50)}, 1},
		{"no data breaks the streak", []Result{okPeriod(90, 90), {Status: StatusNoData}, okPeriod(90, 90)}, 2},
		{"empty history", nil, 1},
	}
	for _, c := range cases {
		if got := Level(c.history, DefaultLevels); got != c.want {
			t.Errorf("%s: level = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestValidateLevels(t *testing.T) {
	if err := ValidateLevels(DefaultLevels); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	bad := [][]LevelRule{
		{{Level: 2, MinScore: 60, MinDimension: 50, ConsecutivePeriods: 1}}, // missing 3..5
		{{2, 60, 50, 1}, {3, 55, 65, 2}, {4, 85, 75, 3}, {5, 95, 85, 3}},     // score decreases
		{{2, 60, 50, 0}, {3, 75, 65, 2}, {4, 85, 75, 3}, {5, 95, 85, 3}},     // zero periods
		{{2, 60, 50, 1}, {3, 75, 65, 2}, {4, 85, 75, 3}, {5, 101, 85, 3}},    // > 100
	}
	for i, levels := range bad {
		if ValidateLevels(levels) == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestEvaluateMaturityPeriod(t *testing.T) {
	c := Counts{Tasks: 10, Reported: 10, Completed: 10, OnTime: 9, LateUpload: 2, LateMinutesSum: 60, LateMinutesCount: 10}
	got := EvaluateMaturityPeriod(c, []Item{
		{Code: "ON_TIME_RATE", Weight: 50},     // 90
		{Code: "LATE_UPLOAD_RATE", Weight: 25}, // 100-20 = 80
		{Code: "AVG_LATE_MINUTES", Weight: 25}, // 100-0.5*6 = 97
	})
	if got.Status != StatusOK || !near(got.Score, 89.25) {
		t.Fatalf("got %+v", got)
	}
	if EvaluateMaturityPeriod(Counts{}, nil).Status != StatusNoData {
		t.Fatal("empty counts should be NO_DATA")
	}
}
```

- [ ] **Step 2: Write the failing period tests**

```go
// internal/scoring/period_test.go
package scoring

import (
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestPeriodBounds(t *testing.T) {
	cases := []struct {
		t          PeriodType
		in         time.Time
		start, end time.Time
	}{
		{PeriodWeek, day(2026, 10, 15), day(2026, 10, 12), day(2026, 10, 18)}, // Thursday -> Mon..Sun
		{PeriodWeek, day(2026, 10, 18), day(2026, 10, 12), day(2026, 10, 18)}, // Sunday
		{PeriodMonth, day(2026, 2, 10), day(2026, 2, 1), day(2026, 2, 28)},
		{PeriodQuarter, day(2026, 11, 5), day(2026, 10, 1), day(2026, 12, 31)},
	}
	for _, c := range cases {
		start, end := PeriodBounds(c.t, c.in)
		if !start.Equal(c.start) || !end.Equal(c.end) {
			t.Errorf("%s %s: got %s..%s", c.t, c.in.Format("2006-01-02"), start.Format("2006-01-02"), end.Format("2006-01-02"))
		}
	}
	if got := PreviousStart(PeriodQuarter, day(2026, 1, 1)); !got.Equal(day(2025, 10, 1)) {
		t.Errorf("previous quarter = %s", got)
	}
	if got := PreviousStart(PeriodWeek, day(2026, 10, 12)); !got.Equal(day(2026, 10, 5)) {
		t.Errorf("previous week = %s", got)
	}
}
```

- [ ] **Step 3: Run — expect compile failure**

Run: `go test ./internal/scoring/`
Expected: FAIL (`undefined: Level`, `undefined: PeriodBounds`).

- [ ] **Step 4: Implement maturity**

```go
// internal/scoring/maturity.go
package scoring

import (
	"fmt"
	"math"
)

type LevelRule struct {
	Level              int     `json:"level"`
	MinScore           float64 `json:"min_score"`
	MinDimension       float64 `json:"min_dimension"`
	ConsecutivePeriods int     `json:"consecutive_periods"`
}

// DefaultLevels are copied into a maturity rule created without levels.
var DefaultLevels = []LevelRule{
	{Level: 2, MinScore: 60, MinDimension: 50, ConsecutivePeriods: 1},
	{Level: 3, MinScore: 75, MinDimension: 65, ConsecutivePeriods: 2},
	{Level: 4, MinScore: 85, MinDimension: 75, ConsecutivePeriods: 3},
	{Level: 5, MinScore: 95, MinDimension: 85, ConsecutivePeriods: 3},
}

// ValidateLevels requires levels 2..5 exactly once, values in range, and each
// column non-decreasing as the level rises.
func ValidateLevels(levels []LevelRule) error {
	if len(levels) != 4 {
		return fmt.Errorf("levels 2 to 5 are required")
	}
	byLevel := map[int]LevelRule{}
	for _, l := range levels {
		if l.Level < 2 || l.Level > 5 {
			return fmt.Errorf("level %d is out of range 2-5", l.Level)
		}
		if l.MinScore < 0 || l.MinScore > 100 || l.MinDimension < 0 || l.MinDimension > 100 || l.ConsecutivePeriods < 1 || l.ConsecutivePeriods > 12 {
			return fmt.Errorf("level %d has out-of-range values", l.Level)
		}
		byLevel[l.Level] = l
	}
	if len(byLevel) != 4 {
		return fmt.Errorf("each level 2 to 5 must appear once")
	}
	for level := 3; level <= 5; level++ {
		prev, cur := byLevel[level-1], byLevel[level]
		if cur.MinScore < prev.MinScore || cur.MinDimension < prev.MinDimension || cur.ConsecutivePeriods < prev.ConsecutivePeriods {
			return fmt.Errorf("level %d requirements must be at least those of level %d", level, level-1)
		}
	}
	return nil
}

func MaxConsecutive(levels []LevelRule) int {
	max := 1
	for _, l := range levels {
		if l.ConsecutivePeriods > max {
			max = l.ConsecutivePeriods
		}
	}
	return max
}

// DimensionScore maps a dimension's actual value to 0..100 with a fixed curve.
func DimensionScore(code string, actual float64) float64 {
	switch code {
	case "LATE_UPLOAD_RATE":
		return math.Max(0, 100-actual)
	case "AVG_LATE_MINUTES":
		return math.Max(0, 100-0.5*actual)
	default:
		return math.Min(100, math.Max(0, actual))
	}
}

// EvaluateMaturityPeriod scores one period's dimensions (item targets unused).
func EvaluateMaturityPeriod(c Counts, items []Item) Result {
	if c.Tasks == 0 {
		return Result{Status: StatusNoData}
	}
	result := Result{Status: StatusOK, TaskCount: c.Tasks}
	for _, item := range items {
		actual, ok := Actual(item.Code, c)
		r := ItemResult{Code: item.Code, Weight: item.Weight, Actual: actual, NA: !ok}
		if ok {
			r.Score = DimensionScore(item.Code, actual)
		}
		result.Items = append(result.Items, r)
	}
	result.Score = weighted(result.Items)
	return result
}

func meets(period Result, rule LevelRule) bool {
	if period.Status != StatusOK || period.Score < rule.MinScore {
		return false
	}
	for _, item := range period.Items {
		if !item.NA && item.Score < rule.MinDimension {
			return false
		}
	}
	return true
}

// Level returns the highest level whose gate holds for its required number of
// most recent periods in history (oldest first). Level 1 always holds.
func Level(history []Result, levels []LevelRule) int {
	best := 1
	for _, rule := range levels {
		n := rule.ConsecutivePeriods
		if rule.Level <= best || len(history) < n {
			continue
		}
		ok := true
		for _, period := range history[len(history)-n:] {
			if !meets(period, rule) {
				ok = false
				break
			}
		}
		if ok {
			best = rule.Level
		}
	}
	return best
}
```

Note: `Level` iterates every level and keeps the highest passing one, so a level-5 pass with a level-4 failure is impossible only because requirements are non-decreasing (enforced by `ValidateLevels`).

- [ ] **Step 5: Implement periods**

```go
// internal/scoring/period.go
package scoring

import "time"

type PeriodType string

const (
	PeriodWeek    PeriodType = "WEEK"
	PeriodMonth   PeriodType = "MONTH"
	PeriodQuarter PeriodType = "QUARTER"
)

func ValidPeriodType(t string) bool {
	switch PeriodType(t) {
	case PeriodWeek, PeriodMonth, PeriodQuarter:
		return true
	}
	return false
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// PeriodBounds returns the first and last day (inclusive) of the period that
// contains day.
func PeriodBounds(t PeriodType, day time.Time) (time.Time, time.Time) {
	d := dateOnly(day)
	switch t {
	case PeriodWeek:
		offset := (int(d.Weekday()) + 6) % 7 // Monday = 0
		start := d.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 6)
	case PeriodQuarter:
		month := time.Month((int(d.Month())-1)/3*3 + 1)
		start := time.Date(d.Year(), month, 1, 0, 0, 0, 0, d.Location())
		return start, start.AddDate(0, 3, -1)
	default:
		start := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location())
		return start, start.AddDate(0, 1, -1)
	}
}

// PreviousStart returns the start of the period before the one starting at start.
func PreviousStart(t PeriodType, start time.Time) time.Time {
	prev, _ := PeriodBounds(t, start.AddDate(0, 0, -1))
	return prev
}
```

- [ ] **Step 6: Run — expect PASS**

Run: `go test ./internal/scoring/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/scoring/
git commit -m "feat(scoring): maturity gates and period bounds"
```

---

### Task 4: Scoring schema

**Files:**
- Create: `ddl/migrations/007_scoring.sql`
- Modify: `ddl/init.sql` (append the same DDL before the triggers section), `ddl/seed_v3.sql` (truncate list), `Makefile` (`db-migrate` runs all migrations)

**Interfaces:**
- Produces tables `score_rules`, `score_rule_items`, `score_rule_levels`, `task_day_facts`, `score_period_results`, `score_period_item_results` used by Tasks 5–10.

- [ ] **Step 1: Write the migration**

```sql
-- ddl/migrations/007_scoring.sql
BEGIN;

-- Versioned SLA / maturity rules. site_id NULL = vendor default, set = site override.
CREATE TABLE IF NOT EXISTS score_rules (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind          varchar(10) NOT NULL CHECK (kind IN ('SLA', 'MATURITY')),
    vendor_id     uuid NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
    site_id       uuid REFERENCES sites(id) ON DELETE RESTRICT,
    period_type   varchar(10) NOT NULL CHECK (period_type IN ('WEEK', 'MONTH', 'QUARTER')),
    version       int NOT NULL CHECK (version > 0),
    created_by    uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    superseded_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_score_rules_version ON score_rules
    (kind, vendor_id, coalesce(site_id, '00000000-0000-0000-0000-000000000000'::uuid), version);
CREATE UNIQUE INDEX IF NOT EXISTS uq_score_rules_active ON score_rules
    (kind, vendor_id, coalesce(site_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS score_rule_items (
    rule_id uuid NOT NULL REFERENCES score_rules(id) ON DELETE CASCADE,
    code    varchar(40) NOT NULL,
    weight  numeric(5,2) NOT NULL CHECK (weight > 0 AND weight <= 100),
    target  numeric(10,2),
    PRIMARY KEY (rule_id, code)
);

CREATE TABLE IF NOT EXISTS score_rule_levels (
    rule_id             uuid NOT NULL REFERENCES score_rules(id) ON DELETE CASCADE,
    level               smallint NOT NULL CHECK (level BETWEEN 2 AND 5),
    min_score           numeric(5,2) NOT NULL,
    min_dimension       numeric(5,2) NOT NULL,
    consecutive_periods smallint NOT NULL CHECK (consecutive_periods >= 1),
    PRIMARY KEY (rule_id, level)
);

-- One row per task (assignment) per day, written once the day can no longer change.
CREATE TABLE IF NOT EXISTS task_day_facts (
    assignment_id         uuid NOT NULL REFERENCES pipeline_item_assignments(id) ON DELETE RESTRICT,
    report_date           date NOT NULL,
    vendor_id             uuid NOT NULL,
    site_id               uuid NOT NULL,
    pipeline_id           uuid NOT NULL,
    worker_id             uuid NOT NULL,
    reported              boolean NOT NULL,
    completed             boolean NOT NULL,
    on_time               boolean NOT NULL,
    late_minutes          int,
    late_upload           boolean NOT NULL,
    evidence_complete     boolean NOT NULL,
    k3_finding            boolean NOT NULL,
    k3_finding_with_notes boolean NOT NULL,
    finalized_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (assignment_id, report_date)
);
CREATE INDEX IF NOT EXISTS idx_task_day_facts_vendor_date ON task_day_facts(vendor_id, report_date);
CREATE INDEX IF NOT EXISTS idx_task_day_facts_site_date ON task_day_facts(site_id, report_date);
CREATE INDEX IF NOT EXISTS idx_task_day_facts_worker_date ON task_day_facts(worker_id, report_date);

-- Frozen results per closed period.
CREATE TABLE IF NOT EXISTS score_period_results (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         varchar(10) NOT NULL CHECK (kind IN ('SLA', 'MATURITY')),
    rule_id      uuid NOT NULL REFERENCES score_rules(id) ON DELETE RESTRICT,
    scope_type   varchar(10) NOT NULL CHECK (scope_type IN ('VENDOR', 'SITE', 'WORKER')),
    vendor_id    uuid NOT NULL REFERENCES vendors(id) ON DELETE RESTRICT,
    site_id      uuid REFERENCES sites(id) ON DELETE RESTRICT,
    worker_id    uuid REFERENCES users(id) ON DELETE RESTRICT,
    period_type  varchar(10) NOT NULL,
    period_start date NOT NULL,
    period_end   date NOT NULL,
    status       varchar(10) NOT NULL CHECK (status IN ('OK', 'NO_DATA')),
    score        numeric(5,2),
    compliant    boolean,
    level        smallint,
    task_count   int NOT NULL DEFAULT 0,
    computed_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_score_period_results ON score_period_results (
    kind, scope_type, vendor_id,
    coalesce(site_id, '00000000-0000-0000-0000-000000000000'::uuid),
    coalesce(worker_id, '00000000-0000-0000-0000-000000000000'::uuid),
    period_type, period_start);
CREATE INDEX IF NOT EXISTS idx_score_period_results_lookup
    ON score_period_results(kind, scope_type, period_start DESC);

CREATE TABLE IF NOT EXISTS score_period_item_results (
    result_id uuid NOT NULL REFERENCES score_period_results(id) ON DELETE CASCADE,
    code      varchar(40) NOT NULL,
    actual    numeric(12,2),
    target    numeric(10,2),
    score     numeric(5,2),
    weight    numeric(5,2) NOT NULL,
    met       boolean,
    is_na     boolean NOT NULL,
    PRIMARY KEY (result_id, code)
);

COMMIT;
```

- [ ] **Step 2: Mirror in `ddl/init.sql`**

Paste the same statements (without `BEGIN;`/`COMMIT;`) right before the `updated_at` triggers section of `init.sql`.

- [ ] **Step 3: Truncate the new tables in the seed**

In `ddl/seed_v3.sql`, add to the top of the `TRUNCATE TABLE` list:

```sql
    score_period_item_results,
    score_period_results,
    score_rule_levels,
    score_rule_items,
    score_rules,
    task_day_facts,
```

- [ ] **Step 4: `make db-migrate` applies every migration in order**

Replace the `db-migrate` recipe in `Makefile`:

```make
db-migrate:
	set -a; . ./.env; set +a; for f in ddl/migrations/*.sql; do echo "== $$f"; PGSSLMODE="$$DB_SSLMODE" PGPASSWORD="$$DB_PASSWORD" psql -X -w -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 -f "$$f" || exit 1; done
```

(Every migration is idempotent: `IF NOT EXISTS` / `IF EXISTS` / data updates that are no-ops when re-run.)

- [ ] **Step 5: Apply against a dev database and confirm**

Run: `make db-setup && make db-reset`
Expected: both complete without error. If no database is available in the environment, record this step as not run.

- [ ] **Step 6: Commit**

```bash
git add ddl/migrations/007_scoring.sql ddl/init.sql ddl/seed_v3.sql Makefile
git commit -m "feat(db): scoring rules, task day facts, and period snapshots"
```

---

### Task 5: Task-day facts — shared SQL, finalization, aggregation

**Files:**
- Create: `internal/domain/entity/scoring.go`, `internal/domain/repository/scoring_repository.go`, `internal/repository/postgres/scoring_facts.go`
- Test: `internal/repository/postgres/scoring_facts_test.go`

**Interfaces:**
- Consumes: `scoring.Counts` (Task 2), schema (Task 4).
- Produces:

```go
// entity
const (ScopeVendor = "VENDOR"; ScopeSite = "SITE"; ScopeWorker = "WORKER")
type FactQuery struct {
	Scope      string    // VENDOR | SITE | WORKER
	VendorID   string    // required
	SiteIDs    []string  // optional include filter
	ExcludeSiteIDs []string
	From, To   time.Time // inclusive dates
}
type ScopeCounts struct { VendorID string; SiteID, WorkerID *string; Counts scoring.Counts }

// repository.ScoringRepository (fact part)
FinalizeFacts(ctx context.Context) (int64, error)
LastFinalDate(ctx context.Context) (time.Time, error)
AggregateFacts(ctx context.Context, q entity.FactQuery) ([]entity.ScopeCounts, error)
```

`AggregateFacts` reads stored facts for dates ≤ `LastFinalDate` and computes live facts with the same SQL for later dates.

- [ ] **Step 1: Entities and interface (fact part)**

```go
// internal/domain/entity/scoring.go
package entity

import (
	"time"

	"peelen-ms-be/internal/scoring"
)

const (
	ScopeVendor = "VENDOR"
	ScopeSite   = "SITE"
	ScopeWorker = "WORKER"

	RuleKindSLA      = "SLA"
	RuleKindMaturity = "MATURITY"

	ResultStatusNoRule = "NO_RULE"
)

type FactQuery struct {
	Scope          string
	VendorID       string
	SiteIDs        []string
	ExcludeSiteIDs []string
	From, To       time.Time
}

type ScopeCounts struct {
	VendorID string
	SiteID   *string
	WorkerID *string
	Counts   scoring.Counts
}
```

```go
// internal/domain/repository/scoring_repository.go
package repository

import (
	"context"
	"time"

	"peelen-ms-be/internal/domain/entity"
)

type ScoringRepository interface {
	FinalizeFacts(ctx context.Context) (int64, error)
	LastFinalDate(ctx context.Context) (time.Time, error)
	AggregateFacts(ctx context.Context, q entity.FactQuery) ([]entity.ScopeCounts, error)
}
```

(Task 6 and Task 8 extend this interface.)

- [ ] **Step 2: Write the DB-gated failing test (covers Review Focus 5)**

```go
// internal/repository/postgres/scoring_facts_test.go
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
```

- [ ] **Step 3: Run — expect compile failure**

Run: `go test ./internal/repository/postgres/ -run TestFacts`
Expected: FAIL (`undefined: NewScoringRepository`). With `TEST_POSTGRES_DSN` unset the test would skip once it compiles.

- [ ] **Step 4: Implement the shared fact SQL, finalization, and aggregation**

```go
// internal/repository/postgres/scoring_facts.go
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
const lastFinalDateSQL = `(localtimestamp - interval '1 day' - (SELECT coalesce(max(start_time), '00:00') FROM shifts))::date`

func (r *ScoringRepositoryImpl) LastFinalDate(ctx context.Context) (time.Time, error) {
	var day time.Time
	err := r.db.QueryRowContext(ctx, `SELECT `+lastFinalDateSQL).Scan(&day)
	return day, err
}
```

Finalization reads the date range first, then inserts with the shared SQL:

```go
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
```

Then aggregation:

```go
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
```

Notes for the implementer:
- `$1`/`$2` are used twice (stored range and live range); that is valid in PostgreSQL.
- `pq.Array` of a nil slice is sent as SQL `NULL`; the `coalesce` wrappers keep empty include/exclude lists from filtering out every row.
- Aggregation uses `vendor_id::text` etc. implicitly: scan `uuid` columns into `string` works with lib/pq (it returns text). If a scan error appears, cast the key columns with `::text` in `keySQL`.

- [ ] **Step 5: Run — PASS (or SKIP without a DB)**

Run: `go vet ./internal/... && go test ./internal/repository/postgres/ -run TestFacts -v`
Expected: PASS with `TEST_POSTGRES_DSN` set; SKIP otherwise. Run it against a dev DB before committing if one is available.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/entity/scoring.go internal/domain/repository/scoring_repository.go internal/repository/postgres/scoring_facts.go internal/repository/postgres/scoring_facts_test.go
git commit -m "feat(scoring): task day facts with shared final/live SQL"
```

---

### Task 6: Rules — entities, repository, validation, access

**Files:**
- Modify: `internal/domain/entity/scoring.go`, `internal/domain/repository/scoring_repository.go`
- Create: `internal/repository/postgres/scoring_repository.go`, `internal/usecase/scoring_usecase.go`
- Test: `internal/usecase/scoring_usecase_test.go`

**Interfaces:**
- Consumes: `scoring.Lookup`, `scoring.ValidateLevels`, `scoring.DefaultLevels`, `scoring.ValidPeriodType`.
- Produces:

```go
// entity
type RuleItem struct { Code string `json:"code"`; Weight float64 `json:"weight"`; Target *float64 `json:"target,omitempty"` }
type ScoreRule struct {
	ID, Kind, VendorID string; SiteID *string; PeriodType string; Version int
	Items []RuleItem; Levels []scoring.LevelRule
	CreatedBy *string; CreatedAt time.Time; SupersededAt *time.Time
}
type ScoreRuleInput struct { Kind, VendorID string; SiteID *string; PeriodType string; Items []RuleItem; Levels []scoring.LevelRule }
type RuleFilter struct { Kind, VendorID string; SiteID *string; ActiveOnly bool }

// repository.ScoringRepository additions
CreateRuleVersion(ctx, input entity.ScoreRuleInput, createdBy string) (*entity.ScoreRule, error)
GetRule(ctx, id string) (*entity.ScoreRule, error)
ListRules(ctx, f entity.RuleFilter) ([]entity.ScoreRule, error)
DeactivateRule(ctx, id string) error
VendorSiteLinked(ctx, vendorID, siteID string) (bool, error)

// usecase
type ScoringUsecase interface {
	Catalog() []scoring.Indicator
	CreateRule(ctx, actor *entity.User, input entity.ScoreRuleInput) (*entity.ScoreRule, error)
	GetRule(ctx, actor *entity.User, id string) (*entity.ScoreRule, error)
	ListRules(ctx, actor *entity.User, f entity.RuleFilter) ([]entity.ScoreRule, error)
	DeleteRule(ctx, actor *entity.User, id string) error
	// Task 8 adds: LiveResults, ListResults; Task 9 adds: FinalizeFacts, ClosePeriods
}
func NewScoringUsecase(repo repository.ScoringRepository) ScoringUsecase
func validateRuleInput(input *entity.ScoreRuleInput) error
```

- [ ] **Step 1: Write failing usecase tests (covers Review Focus 2)**

```go
// internal/usecase/scoring_usecase_test.go
package usecase

import (
	"context"
	"errors"
	"testing"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
	"peelen-ms-be/internal/scoring"
)

const (
	testVendor = "10000000-0000-4000-8000-000000000001"
	testSite   = "20000000-0000-4000-8000-000000000001"
	otherSite  = "20000000-0000-4000-8000-000000000002"
)

type scoringRepoFake struct {
	repository.ScoringRepository
	created *entity.ScoreRuleInput
	rule    *entity.ScoreRule
	linked  bool
}

func (f *scoringRepoFake) CreateRuleVersion(_ context.Context, in entity.ScoreRuleInput, _ string) (*entity.ScoreRule, error) {
	f.created = &in
	return &entity.ScoreRule{ID: "rule", Kind: in.Kind, VendorID: in.VendorID, SiteID: in.SiteID}, nil
}
func (f *scoringRepoFake) VendorSiteLinked(context.Context, string, string) (bool, error) { return f.linked, nil }
func (f *scoringRepoFake) GetRule(context.Context, string) (*entity.ScoreRule, error) {
	if f.rule == nil {
		return nil, repository.ErrNotFound
	}
	return f.rule, nil
}
func (f *scoringRepoFake) DeactivateRule(context.Context, string) error { return nil }

func target(v float64) *float64 { return &v }

func slaInput(site *string, items ...entity.RuleItem) entity.ScoreRuleInput {
	return entity.ScoreRuleInput{Kind: "SLA", VendorID: testVendor, SiteID: site, PeriodType: "MONTH", Items: items}
}

func TestValidateRuleInput(t *testing.T) {
	ok := slaInput(nil,
		entity.RuleItem{Code: "COMPLETION_RATE", Weight: 33.33, Target: target(95)},
		entity.RuleItem{Code: "ON_TIME_RATE", Weight: 33.33, Target: target(90)},
		entity.RuleItem{Code: "EVIDENCE_COMPLETE_RATE", Weight: 33.34, Target: target(98)},
	)
	if err := validateRuleInput(&ok); err != nil {
		t.Fatalf("33.33+33.33+33.34 rejected: %v", err)
	}
	bad := map[string]entity.ScoreRuleInput{
		"weights not 100":   slaInput(nil, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 90, Target: target(95)}),
		"unknown code":      slaInput(nil, entity.RuleItem{Code: "SPEED", Weight: 100, Target: target(1)}),
		"duplicate code":    slaInput(nil, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 50, Target: target(95)}, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 50, Target: target(95)}),
		"missing target":    slaInput(nil, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 100}),
		"percent over 100":  slaInput(nil, entity.RuleItem{Code: "COMPLETION_RATE", Weight: 100, Target: target(101)}),
		"negative minutes":  slaInput(nil, entity.RuleItem{Code: "AVG_LATE_MINUTES", Weight: 100, Target: target(-1)}),
		"maturity-only SLA": slaInput(nil, entity.RuleItem{Code: "CONSISTENCY", Weight: 100, Target: target(50)}),
		"bad period":        {Kind: "SLA", VendorID: testVendor, PeriodType: "DAY", Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}},
		"bad kind":          {Kind: "KPI", VendorID: testVendor, PeriodType: "MONTH", Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}},
	}
	for name, in := range bad {
		in := in
		if err := validateRuleInput(&in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	maturity := entity.ScoreRuleInput{Kind: "MATURITY", VendorID: testVendor, PeriodType: "QUARTER",
		Items: []entity.RuleItem{{Code: "CONSISTENCY", Weight: 100}}}
	if err := validateRuleInput(&maturity); err != nil {
		t.Fatal(err)
	}
	if len(maturity.Levels) != 4 {
		t.Fatalf("default levels not applied: %+v", maturity.Levels)
	}
}

func TestCreateRuleAccess(t *testing.T) {
	item := entity.RuleItem{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}
	site := testSite
	other := otherSite
	superAdmin := &entity.User{ID: "sa", Roles: []string{"SUPER_ADMIN"}}
	admin := &entity.User{ID: "admin", Roles: []string{"ADMIN"}, Sites: []string{testSite}}
	cases := []struct {
		name   string
		actor  *entity.User
		input  entity.ScoreRuleInput
		linked bool
		want   error
	}{
		{"super admin default", superAdmin, slaInput(nil, item), true, nil},
		{"admin default forbidden", admin, slaInput(nil, item), true, ErrForbidden},
		{"admin own site override", admin, slaInput(&site, item), true, nil},
		{"admin other site forbidden", admin, slaInput(&other, item), true, ErrForbidden},
		{"site not linked to vendor", superAdmin, slaInput(&site, item), false, ErrInvalidOperationalData},
		{"worker forbidden", &entity.User{Roles: []string{"WORKER"}}, slaInput(nil, item), true, ErrForbidden},
	}
	for _, c := range cases {
		repo := &scoringRepoFake{linked: c.linked}
		_, err := NewScoringUsecase(repo).CreateRule(context.Background(), c.actor, c.input)
		if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestDeleteRuleAccess(t *testing.T) {
	site := testSite
	admin := &entity.User{ID: "admin", Roles: []string{"ADMIN"}, Sites: []string{testSite}}
	repo := &scoringRepoFake{rule: &entity.ScoreRule{ID: "r", VendorID: testVendor}}
	if err := NewScoringUsecase(repo).DeleteRule(context.Background(), admin, "30000000-0000-4000-8000-000000000001"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin deleting vendor default: %v", err)
	}
	repo.rule.SiteID = &site
	if err := NewScoringUsecase(repo).DeleteRule(context.Background(), admin, "30000000-0000-4000-8000-000000000001"); err != nil {
		t.Fatalf("admin deleting own override: %v", err)
	}
}

var _ = scoring.DefaultLevels // keeps the scoring import used by later tests in this package
```

- [ ] **Step 2: Run — expect compile failure**

Run: `go test ./internal/usecase/ -run 'TestValidateRuleInput|TestCreateRuleAccess|TestDeleteRuleAccess'`
Expected: FAIL (`undefined: validateRuleInput`).

- [ ] **Step 3: Add rule entities and interface methods**

Append to `internal/domain/entity/scoring.go`:

```go
type RuleItem struct {
	Code   string   `json:"code"`
	Weight float64  `json:"weight"`
	Target *float64 `json:"target,omitempty"`
}

type ScoreRule struct {
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	VendorID     string              `json:"vendor_id"`
	SiteID       *string             `json:"site_id,omitempty"`
	PeriodType   string              `json:"period_type"`
	Version      int                 `json:"version"`
	Items        []RuleItem          `json:"items"`
	Levels       []scoring.LevelRule `json:"levels,omitempty"`
	CreatedBy    *string             `json:"created_by,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	SupersededAt *time.Time          `json:"superseded_at,omitempty"`
}

type ScoreRuleInput struct {
	Kind       string              `json:"-"`
	VendorID   string              `json:"vendor_id"`
	SiteID     *string             `json:"site_id"`
	PeriodType string              `json:"period_type"`
	Items      []RuleItem          `json:"items"`
	Levels     []scoring.LevelRule `json:"levels"`
}

type RuleFilter struct {
	Kind       string
	VendorID   string
	SiteID     *string
	ActiveOnly bool
	// Visibility, set by the usecase from the caller's access.
	AllVendors bool
	VendorIDs  []string
}
```

Add to `ScoringRepository`:

```go
	CreateRuleVersion(ctx context.Context, input entity.ScoreRuleInput, createdBy string) (*entity.ScoreRule, error)
	GetRule(ctx context.Context, id string) (*entity.ScoreRule, error)
	ListRules(ctx context.Context, f entity.RuleFilter) ([]entity.ScoreRule, error)
	DeactivateRule(ctx context.Context, id string) error
	VendorSiteLinked(ctx context.Context, vendorID, siteID string) (bool, error)
```

- [ ] **Step 4: Implement the rule repository**

```go
// internal/repository/postgres/scoring_repository.go
package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
	"peelen-ms-be/internal/scoring"
)

const zeroUUID = "00000000-0000-0000-0000-000000000000"

func (r *ScoringRepositoryImpl) VendorSiteLinked(ctx context.Context, vendorID, siteID string) (bool, error) {
	var linked bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM vendor_sites WHERE vendor_id = $1 AND site_id = $2)`,
		vendorID, siteID).Scan(&linked)
	return linked, err
}

// CreateRuleVersion supersedes the active version (if any) and inserts the next one.
func (r *ScoringRepositoryImpl) CreateRuleVersion(ctx context.Context, in entity.ScoreRuleInput, createdBy string) (*entity.ScoreRule, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize versioning per (kind, vendor, site).
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1 || $2 || coalesce($3::text, '')))`,
		in.Kind, in.VendorID, in.SiteID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE score_rules SET superseded_at = now()
		WHERE kind = $1 AND vendor_id = $2 AND coalesce(site_id, $4::uuid) = coalesce($3::uuid, $4::uuid)
			AND superseded_at IS NULL`, in.Kind, in.VendorID, in.SiteID, zeroUUID); err != nil {
		return nil, translateOperationalDBError(err)
	}
	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by)
		SELECT $1, $2, $3, $4, coalesce(max(version), 0) + 1, $5
		FROM score_rules
		WHERE kind = $1 AND vendor_id = $2 AND coalesce(site_id, $6::uuid) = coalesce($3::uuid, $6::uuid)
		RETURNING id::text`, in.Kind, in.VendorID, in.SiteID, in.PeriodType, createdBy, zeroUUID).Scan(&id); err != nil {
		return nil, translateOperationalDBError(err)
	}
	for _, item := range in.Items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO score_rule_items (rule_id, code, weight, target) VALUES ($1, $2, $3, $4)`,
			id, item.Code, item.Weight, item.Target); err != nil {
			return nil, translateOperationalDBError(err)
		}
	}
	for _, level := range in.Levels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO score_rule_levels (rule_id, level, min_score, min_dimension, consecutive_periods)
			VALUES ($1, $2, $3, $4, $5)`, id, level.Level, level.MinScore, level.MinDimension, level.ConsecutivePeriods); err != nil {
			return nil, translateOperationalDBError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetRule(ctx, id)
}

const ruleColumns = `r.id::text, r.kind, r.vendor_id::text, r.site_id::text, r.period_type, r.version,
	r.created_by::text, r.created_at, r.superseded_at`

func scanRule(row interface{ Scan(...any) error }) (entity.ScoreRule, error) {
	var rule entity.ScoreRule
	var siteID, createdBy sql.NullString
	var superseded sql.NullTime
	err := row.Scan(&rule.ID, &rule.Kind, &rule.VendorID, &siteID, &rule.PeriodType, &rule.Version,
		&createdBy, &rule.CreatedAt, &superseded)
	rule.SiteID = nullStringPtr(siteID)
	rule.CreatedBy = nullStringPtr(createdBy)
	rule.SupersededAt = nullTimePtr(superseded)
	return rule, err
}

func (r *ScoringRepositoryImpl) loadRuleParts(ctx context.Context, rule *entity.ScoreRule) error {
	rows, err := r.db.QueryContext(ctx, `SELECT code, weight, target FROM score_rule_items WHERE rule_id = $1 ORDER BY weight DESC, code`, rule.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item entity.RuleItem
		var target sql.NullFloat64
		if err := rows.Scan(&item.Code, &item.Weight, &target); err != nil {
			return err
		}
		if target.Valid {
			item.Target = &target.Float64
		}
		rule.Items = append(rule.Items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	levels, err := r.db.QueryContext(ctx, `SELECT level, min_score, min_dimension, consecutive_periods
		FROM score_rule_levels WHERE rule_id = $1 ORDER BY level`, rule.ID)
	if err != nil {
		return err
	}
	defer levels.Close()
	for levels.Next() {
		var l scoring.LevelRule
		if err := levels.Scan(&l.Level, &l.MinScore, &l.MinDimension, &l.ConsecutivePeriods); err != nil {
			return err
		}
		rule.Levels = append(rule.Levels, l)
	}
	return levels.Err()
}

func (r *ScoringRepositoryImpl) GetRule(ctx context.Context, id string) (*entity.ScoreRule, error) {
	rule, err := scanRule(r.db.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM score_rules r WHERE r.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := r.loadRuleParts(ctx, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *ScoringRepositoryImpl) ListRules(ctx context.Context, f entity.RuleFilter) ([]entity.ScoreRule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+ruleColumns+` FROM score_rules r
		WHERE ($1 = '' OR r.kind = $1)
			AND ($2 = '' OR r.vendor_id::text = $2)
			AND ($3::uuid IS NULL OR r.site_id = $3::uuid)
			AND (NOT $4 OR r.superseded_at IS NULL)
			AND ($5 OR r.vendor_id = ANY($6::uuid[]))
		ORDER BY r.vendor_id, r.site_id NULLS FIRST, r.kind, r.version DESC`,
		f.Kind, f.VendorID, f.SiteID, f.ActiveOnly, f.AllVendors, pq.Array(f.VendorIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []entity.ScoreRule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range rules {
		if err := r.loadRuleParts(ctx, &rules[i]); err != nil {
			return nil, err
		}
	}
	return rules, nil
}

func (r *ScoringRepositoryImpl) DeactivateRule(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE score_rules SET superseded_at = now() WHERE id = $1 AND superseded_at IS NULL`, id)
	if err != nil {
		return err
	}
	return workflowRequireAffected(result)
}
```

- [ ] **Step 5: Implement rule validation and access in the usecase**

```go
// internal/usecase/scoring_usecase.go
package usecase

import (
	"context"
	"fmt"
	"math"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
	"peelen-ms-be/internal/scoring"
)

type ScoringUsecase interface {
	Catalog() []scoring.Indicator
	CreateRule(ctx context.Context, actor *entity.User, input entity.ScoreRuleInput) (*entity.ScoreRule, error)
	GetRule(ctx context.Context, actor *entity.User, id string) (*entity.ScoreRule, error)
	ListRules(ctx context.Context, actor *entity.User, f entity.RuleFilter) ([]entity.ScoreRule, error)
	DeleteRule(ctx context.Context, actor *entity.User, id string) error
}

type ScoringUsecaseImpl struct {
	repository repository.ScoringRepository
}

func NewScoringUsecase(repo repository.ScoringRepository) ScoringUsecase {
	return &ScoringUsecaseImpl{repository: repo}
}

func (u *ScoringUsecaseImpl) Catalog() []scoring.Indicator { return scoring.Catalog }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidOperationalData}, args...)...)
}

// validateRuleInput checks the rule shape and fills default maturity levels.
func validateRuleInput(in *entity.ScoreRuleInput) error {
	if in.Kind != entity.RuleKindSLA && in.Kind != entity.RuleKindMaturity {
		return invalid("kind must be SLA or MATURITY")
	}
	if !scopeUUIDPattern.MatchString(in.VendorID) || in.SiteID != nil && !scopeUUIDPattern.MatchString(*in.SiteID) {
		return invalid("vendor_id and site_id must be UUIDs")
	}
	if !scoring.ValidPeriodType(in.PeriodType) {
		return invalid("period_type must be WEEK, MONTH, or QUARTER")
	}
	if len(in.Items) == 0 {
		return invalid("at least one item is required")
	}
	seen := map[string]bool{}
	var total float64
	for i, item := range in.Items {
		indicator, ok := scoring.Lookup(item.Code)
		if !ok || seen[item.Code] {
			return invalid("unknown or duplicate code %q", item.Code)
		}
		seen[item.Code] = true
		if in.Kind == entity.RuleKindSLA && !indicator.SLA || in.Kind == entity.RuleKindMaturity && !indicator.Maturity {
			return invalid("%s cannot be used in a %s rule", item.Code, in.Kind)
		}
		if item.Weight <= 0 || item.Weight > 100 {
			return invalid("weight of %s must be between 0 and 100", item.Code)
		}
		total += item.Weight
		if in.Kind == entity.RuleKindSLA {
			if item.Target == nil || *item.Target < 0 || indicator.Unit == scoring.UnitPercent && *item.Target > 100 {
				return invalid("target of %s is required and must fit its unit", item.Code)
			}
		} else {
			in.Items[i].Target = nil // maturity dimensions have no target
		}
	}
	if math.Abs(total-100) > 0.01 {
		return invalid("weights must total 100, got %.2f", total)
	}
	if in.Kind == entity.RuleKindSLA {
		in.Levels = nil
		return nil
	}
	if len(in.Levels) == 0 {
		in.Levels = append([]scoring.LevelRule(nil), scoring.DefaultLevels...)
	}
	if err := scoring.ValidateLevels(in.Levels); err != nil {
		return invalid("%v", err)
	}
	return nil
}

// canWriteRule: SUPER_ADMIN writes anything; ADMIN only overrides for its own sites.
func canWriteRule(actor *entity.User, siteID *string) bool {
	if actor == nil {
		return false
	}
	if hasRole(actor, "SUPER_ADMIN") {
		return true
	}
	return siteID != nil && hasRole(actor, "ADMIN") && containsString(actor.Sites, *siteID)
}

func (u *ScoringUsecaseImpl) CreateRule(ctx context.Context, actor *entity.User, input entity.ScoreRuleInput) (*entity.ScoreRule, error) {
	if !canWriteRule(actor, input.SiteID) {
		return nil, ErrForbidden
	}
	if err := validateRuleInput(&input); err != nil {
		return nil, err
	}
	if input.SiteID != nil {
		linked, err := u.repository.VendorSiteLinked(ctx, input.VendorID, *input.SiteID)
		if err != nil {
			return nil, err
		}
		if !linked {
			return nil, invalid("site is not linked to the vendor")
		}
	}
	return u.repository.CreateRuleVersion(ctx, input, actor.ID)
}

func (u *ScoringUsecaseImpl) DeleteRule(ctx context.Context, actor *entity.User, id string) error {
	if !scopeUUIDPattern.MatchString(id) {
		return ErrNotFound
	}
	rule, err := u.repository.GetRule(ctx, id)
	if err != nil {
		return err
	}
	if !canWriteRule(actor, rule.SiteID) {
		return ErrForbidden
	}
	return u.repository.DeactivateRule(ctx, id)
}

// ruleVisibility limits rule reads: SUPER_ADMIN all; ADMIN vendors linked to its
// sites (resolved by the caller into actor.Vendors at login is not guaranteed,
// so the repository filter uses site links); others their own vendors.
func (u *ScoringUsecaseImpl) GetRule(ctx context.Context, actor *entity.User, id string) (*entity.ScoreRule, error) {
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	if actor == nil || !hasRole(actor, "SUPER_ADMIN", "ADMIN") {
		return nil, ErrForbidden
	}
	rule, err := u.repository.GetRule(ctx, id)
	if err != nil {
		return nil, err
	}
	if !hasRole(actor, "SUPER_ADMIN") {
		visible, err := u.adminVendorIDs(ctx, actor)
		if err != nil {
			return nil, err
		}
		if !containsString(visible, rule.VendorID) {
			return nil, ErrNotFound
		}
	}
	return rule, nil
}

func (u *ScoringUsecaseImpl) ListRules(ctx context.Context, actor *entity.User, f entity.RuleFilter) ([]entity.ScoreRule, error) {
	if actor == nil || !hasRole(actor, "SUPER_ADMIN", "ADMIN") {
		return nil, ErrForbidden
	}
	f.AllVendors = hasRole(actor, "SUPER_ADMIN")
	if !f.AllVendors {
		visible, err := u.adminVendorIDs(ctx, actor)
		if err != nil {
			return nil, err
		}
		f.VendorIDs = visible
	}
	return u.repository.ListRules(ctx, f)
}
```

`adminVendorIDs` needs one more repository method. Add to `ScoringRepository`:

```go
	VendorIDsForSites(ctx context.Context, siteIDs []string) ([]string, error)
```

Implement in `scoring_repository.go`:

```go
func (r *ScoringRepositoryImpl) VendorIDsForSites(ctx context.Context, siteIDs []string) ([]string, error) {
	var ids pq.StringArray
	err := r.db.QueryRowContext(ctx, `SELECT coalesce(array_agg(DISTINCT vendor_id::text), '{}') FROM vendor_sites WHERE site_id = ANY($1::uuid[])`,
		pq.Array(siteIDs)).Scan(&ids)
	return []string(ids), err
}
```

and in the usecase:

```go
func (u *ScoringUsecaseImpl) adminVendorIDs(ctx context.Context, actor *entity.User) ([]string, error) {
	return u.repository.VendorIDsForSites(ctx, actor.Sites)
}
```

Delete the misleading "ruleVisibility" comment above `GetRule` and replace it with: `// Rule reads: SUPER_ADMIN all; ADMIN rules of vendors linked to its sites.`

`containsString` already exists in the usecase package (used by `CreateInventoryReport`); reuse it.

- [ ] **Step 6: Run — expect PASS**

Run: `go vet ./internal/... && go test ./internal/usecase/ ./internal/scoring/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/entity/scoring.go internal/domain/repository/scoring_repository.go internal/repository/postgres/scoring_repository.go internal/usecase/scoring_usecase.go internal/usecase/scoring_usecase_test.go
git commit -m "feat(scoring): versioned SLA and maturity rules with access control"
```

---

### Task 7: Rule lookups and snapshot storage (repository)

**Files:**
- Modify: `internal/domain/entity/scoring.go`, `internal/domain/repository/scoring_repository.go`, `internal/repository/postgres/scoring_repository.go`
- Test: `internal/repository/postgres/scoring_repository_test.go`

**Interfaces:**
- Consumes: `scoring.Result`, `scoring.ItemResult`.
- Produces:

```go
// entity
type ScoreResult struct {
	ID, Kind, RuleID, ScopeType, VendorID string
	SiteID, WorkerID *string
	PeriodType string; PeriodStart, PeriodEnd time.Time
	scoring.Result // Status, Score, Compliant, Level, TaskCount, Items
	Live bool `json:"live"`
}
type ResultFilter struct {
	Kind, Scope string; VendorID, SiteID, WorkerID string
	From, To time.Time; Page, Limit int
	AllVendors bool; VendorIDs, SiteIDs []string; OnlyWorkerID string
}

// repository additions
RuleKeys(ctx) ([]entity.RuleKey, error)                               // distinct (kind, vendor, site) with any version
RuleVersions(ctx, key entity.RuleKey) ([]entity.ScoreRule, error)     // oldest first, with items and levels
OverrideSitesAt(ctx, kind, vendorID string, at time.Time) ([]string, error)
VendorSites(ctx, vendorID string) ([]string, error)
ResultExists(ctx, kind, scope, vendorID string, siteID *string, periodType string, start time.Time) (bool, error)
SaveResult(ctx, r entity.ScoreResult) error                           // ON CONFLICT DO NOTHING
ListResults(ctx, f entity.ResultFilter) ([]entity.ScoreResult, int, error)
WithJobLock(ctx, fn func(context.Context) error) (bool, error)        // pg_try_advisory_lock on a dedicated conn
```

with `type RuleKey struct { Kind, VendorID string; SiteID *string }`.

- [ ] **Step 1: Write the DB-gated failing test**

```go
// internal/repository/postgres/scoring_repository_test.go
package postgres

import (
	"context"
	"testing"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/scoring"
)

func TestRuleVersionsAndSnapshots(t *testing.T) {
	db := openScoringDB(t)
	f := newScoringFixture(t, db)
	repo := NewScoringRepository(db).(*ScoringRepositoryImpl)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM score_period_results WHERE vendor_id = $1`, f.vendorID)
		_, _ = db.Exec(`DELETE FROM score_rules WHERE vendor_id = $1`, f.vendorID)
	})
	target := 95.0
	in := entity.ScoreRuleInput{Kind: "SLA", VendorID: f.vendorID, PeriodType: "MONTH",
		Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: &target}}}
	first, err := repo.CreateRuleVersion(ctx, in, f.workerID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateRuleVersion(ctx, in, f.workerID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("versions = %d, %d", first.Version, second.Version)
	}
	versions, err := repo.RuleVersions(ctx, entity.RuleKey{Kind: "SLA", VendorID: f.vendorID})
	if err != nil || len(versions) != 2 || versions[0].SupersededAt == nil || versions[1].SupersededAt != nil {
		t.Fatalf("versions = %+v, err %v", versions, err)
	}

	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	result := entity.ScoreResult{Kind: "SLA", RuleID: second.ID, ScopeType: entity.ScopeVendor, VendorID: f.vendorID,
		PeriodType: "MONTH", PeriodStart: start, PeriodEnd: start.AddDate(0, 1, -1),
		Result: scoring.Result{Status: "OK", Score: 97.5, Compliant: true, TaskCount: 10,
			Items: []scoring.ItemResult{{Code: "COMPLETION_RATE", Actual: 97.5, Target: 95, Score: 100, Weight: 100, Met: true}}}}
	if err := repo.SaveResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveResult(ctx, result); err != nil { // idempotent
		t.Fatal(err)
	}
	exists, err := repo.ResultExists(ctx, "SLA", entity.ScopeVendor, f.vendorID, nil, "MONTH", start)
	if err != nil || !exists {
		t.Fatalf("exists = %v, err %v", exists, err)
	}
	list, total, err := repo.ListResults(ctx, entity.ResultFilter{Kind: "SLA", Scope: entity.ScopeVendor,
		From: start, To: start.AddDate(0, 1, 0), Page: 1, Limit: 10, AllVendors: true, VendorID: f.vendorID})
	if err != nil || total != 1 || len(list[0].Items) != 1 || list[0].Score != 97.5 {
		t.Fatalf("list = %+v total %d err %v", list, total, err)
	}

	ran, err := repo.WithJobLock(ctx, func(context.Context) error { return nil })
	if err != nil || !ran {
		t.Fatalf("job lock: ran=%v err=%v", ran, err)
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

Run: `go test ./internal/repository/postgres/ -run TestRuleVersionsAndSnapshots`
Expected: FAIL (`undefined: entity.ScoreResult`).

- [ ] **Step 3: Add entities and interface methods**

Append to `internal/domain/entity/scoring.go`:

```go
type RuleKey struct {
	Kind     string
	VendorID string
	SiteID   *string
}

type ScoreResult struct {
	ID          string    `json:"id,omitempty"`
	Kind        string    `json:"kind"`
	RuleID      string    `json:"rule_id,omitempty"`
	ScopeType   string    `json:"scope_type"`
	VendorID    string    `json:"vendor_id"`
	SiteID      *string   `json:"site_id,omitempty"`
	WorkerID    *string   `json:"worker_id,omitempty"`
	PeriodType  string    `json:"period_type,omitempty"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Live        bool      `json:"live"`
	scoring.Result
}

type ResultFilter struct {
	Kind, Scope                 string
	VendorID, SiteID, WorkerID  string
	From, To                    time.Time
	Page, Limit                 int
	// Visibility, set by the usecase.
	AllVendors   bool
	VendorIDs    []string
	SiteIDs      []string
	OnlyWorkerID string
}
```

Add the methods listed in **Interfaces** to `ScoringRepository`.

- [ ] **Step 4: Implement the repository methods**

Append to `internal/repository/postgres/scoring_repository.go`:

```go
func (r *ScoringRepositoryImpl) RuleKeys(ctx context.Context) ([]entity.RuleKey, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT kind, vendor_id::text, site_id::text FROM score_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []entity.RuleKey
	for rows.Next() {
		var key entity.RuleKey
		var siteID sql.NullString
		if err := rows.Scan(&key.Kind, &key.VendorID, &siteID); err != nil {
			return nil, err
		}
		key.SiteID = nullStringPtr(siteID)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (r *ScoringRepositoryImpl) RuleVersions(ctx context.Context, key entity.RuleKey) ([]entity.ScoreRule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM score_rules r
		WHERE r.kind = $1 AND r.vendor_id = $2 AND coalesce(r.site_id, $4::uuid) = coalesce($3::uuid, $4::uuid)
		ORDER BY r.version`, key.Kind, key.VendorID, key.SiteID, zeroUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []entity.ScoreRule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range rules {
		if err := r.loadRuleParts(ctx, &rules[i]); err != nil {
			return nil, err
		}
	}
	return rules, nil
}

// OverrideSitesAt lists sites with a site override of kind active at the instant.
func (r *ScoringRepositoryImpl) OverrideSitesAt(ctx context.Context, kind, vendorID string, at time.Time) ([]string, error) {
	var ids pq.StringArray
	err := r.db.QueryRowContext(ctx, `SELECT coalesce(array_agg(DISTINCT site_id::text), '{}') FROM score_rules
		WHERE kind = $1 AND vendor_id = $2 AND site_id IS NOT NULL
			AND created_at <= $3 AND (superseded_at IS NULL OR superseded_at > $3)`, kind, vendorID, at).Scan(&ids)
	return []string(ids), err
}

func (r *ScoringRepositoryImpl) VendorSites(ctx context.Context, vendorID string) ([]string, error) {
	var ids pq.StringArray
	err := r.db.QueryRowContext(ctx, `SELECT coalesce(array_agg(site_id::text ORDER BY site_id), '{}') FROM vendor_sites WHERE vendor_id = $1`,
		vendorID).Scan(&ids)
	return []string(ids), err
}

func (r *ScoringRepositoryImpl) ResultExists(ctx context.Context, kind, scope, vendorID string, siteID *string, periodType string, start time.Time) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM score_period_results
		WHERE kind = $1 AND scope_type = $2 AND vendor_id = $3 AND coalesce(site_id, $7::uuid) = coalesce($4::uuid, $7::uuid)
			AND worker_id IS NULL AND period_type = $5 AND period_start = $6)`,
		kind, scope, vendorID, siteID, periodType, start, zeroUUID).Scan(&exists)
	return exists, err
}

func (r *ScoringRepositoryImpl) SaveResult(ctx context.Context, res entity.ScoreResult) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var score, compliant, level any
	if res.Status == scoring.StatusOK {
		score = res.Score
		if res.Kind == entity.RuleKindSLA {
			compliant = res.Compliant
		} else {
			level = res.Level
		}
	} else if res.Kind == entity.RuleKindMaturity {
		level = res.Level
	}
	var id string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO score_period_results (kind, rule_id, scope_type, vendor_id, site_id, worker_id, period_type,
			period_start, period_end, status, score, compliant, level, task_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT DO NOTHING
		RETURNING id::text`, res.Kind, res.RuleID, res.ScopeType, res.VendorID, res.SiteID, res.WorkerID, res.PeriodType,
		res.PeriodStart, res.PeriodEnd, res.Status, score, compliant, level, res.TaskCount).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already frozen
	}
	if err != nil {
		return translateOperationalDBError(err)
	}
	for _, item := range res.Items {
		var target, met any
		if res.Kind == entity.RuleKindSLA {
			target, met = item.Target, item.Met
		}
		var actual, itemScore any
		if !item.NA {
			actual, itemScore = item.Actual, item.Score
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO score_period_item_results (result_id, code, actual, target, score, weight, met, is_na)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, item.Code, actual, target, itemScore, item.Weight, met, item.NA); err != nil {
			return translateOperationalDBError(err)
		}
	}
	return tx.Commit()
}

func (r *ScoringRepositoryImpl) ListResults(ctx context.Context, f entity.ResultFilter) ([]entity.ScoreResult, int, error) {
	where := `kind = $1 AND scope_type = $2 AND period_start >= $3 AND period_start < $4
		AND ($5 = '' OR vendor_id::text = $5) AND ($6 = '' OR site_id::text = $6) AND ($7 = '' OR worker_id::text = $7)
		AND ($8 OR vendor_id = ANY($9::uuid[]) OR site_id = ANY($10::uuid[]))
		AND ($11 = '' OR worker_id::text = $11)`
	args := []any{f.Kind, f.Scope, f.From, f.To, f.VendorID, f.SiteID, f.WorkerID,
		f.AllVendors, pq.Array(f.VendorIDs), pq.Array(f.SiteIDs), f.OnlyWorkerID}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM score_period_results WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, kind, rule_id::text, scope_type, vendor_id::text, site_id::text, worker_id::text, period_type,
			period_start, period_end, status, score, compliant, level, task_count
		FROM score_period_results WHERE `+where+`
		ORDER BY period_start DESC, vendor_id, site_id NULLS FIRST, worker_id NULLS FIRST
		LIMIT $12 OFFSET $13`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var results []entity.ScoreResult
	for rows.Next() {
		var res entity.ScoreResult
		var siteID, workerID sql.NullString
		var score sql.NullFloat64
		var compliant sql.NullBool
		var level sql.NullInt64
		if err := rows.Scan(&res.ID, &res.Kind, &res.RuleID, &res.ScopeType, &res.VendorID, &siteID, &workerID,
			&res.PeriodType, &res.PeriodStart, &res.PeriodEnd, &res.Status, &score, &compliant, &level, &res.TaskCount); err != nil {
			return nil, 0, err
		}
		res.SiteID, res.WorkerID = nullStringPtr(siteID), nullStringPtr(workerID)
		res.Score, res.Compliant, res.Level = score.Float64, compliant.Bool, int(level.Int64)
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range results {
		items, err := r.db.QueryContext(ctx, `SELECT code, coalesce(actual, 0), coalesce(target, 0), coalesce(score, 0), weight,
			coalesce(met, false), is_na FROM score_period_item_results WHERE result_id = $1 ORDER BY weight DESC, code`, results[i].ID)
		if err != nil {
			return nil, 0, err
		}
		for items.Next() {
			var it scoring.ItemResult
			if err := items.Scan(&it.Code, &it.Actual, &it.Target, &it.Score, &it.Weight, &it.Met, &it.NA); err != nil {
				items.Close()
				return nil, 0, err
			}
			results[i].Items = append(results[i].Items, it)
		}
		items.Close()
	}
	return results, total, nil
}

// scoringJobLockKey identifies the scoring job's advisory lock.
const scoringJobLockKey = 7_420_015

// WithJobLock runs fn only if this process wins the advisory lock; the lock is
// held on a dedicated connection for fn's duration.
func (r *ScoringRepositoryImpl) WithJobLock(ctx context.Context, fn func(context.Context) error) (bool, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, scoringJobLockKey).Scan(&locked); err != nil {
		return false, err
	}
	if !locked {
		return false, nil
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, scoringJobLockKey)
	return true, fn(ctx)
}
```

Add `"time"` to this file's imports (Task 6 did not need it).

- [ ] **Step 5: Run — PASS (or SKIP without a DB)**

Run: `go vet ./internal/... && go test ./internal/repository/postgres/ -run TestRuleVersionsAndSnapshots -v`
Expected: PASS with `TEST_POSTGRES_DSN`; SKIP otherwise.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/entity/scoring.go internal/domain/repository/scoring_repository.go internal/repository/postgres/scoring_repository.go internal/repository/postgres/scoring_repository_test.go
git commit -m "feat(scoring): rule lookups, period snapshots, and job lock"
```

---

### Task 8: Evaluation, live results, and history (usecase)

**Files:**
- Modify: `internal/usecase/scoring_usecase.go`
- Test: `internal/usecase/scoring_evaluate_test.go`

**Interfaces:**
- Consumes: repository methods from Tasks 5–7; `scoring.EvaluateSLA`, `scoring.EvaluateMaturityPeriod`, `scoring.Level`, `scoring.MaxConsecutive`, `scoring.PeriodBounds`, `scoring.PreviousStart`.
- Produces (added to `ScoringUsecase`):

```go
LiveResults(ctx context.Context, actor *entity.User, kind, scope string, vendorID string) ([]entity.ScoreResult, error)
ListResults(ctx context.Context, actor *entity.User, f entity.ResultFilter) ([]entity.ScoreResult, int, error)
```

and internal helpers used by Task 9:

```go
func (u *ScoringUsecaseImpl) evaluate(ctx context.Context, rule entity.ScoreRule, scope string, vendorID string,
	siteIDs, excludeSiteIDs []string, start, end time.Time) ([]entity.ScoreResult, error)
func (u *ScoringUsecaseImpl) Clock() time.Time // now in the configured location; settable in tests
```

- [ ] **Step 1: Write failing tests (covers Review Focus 4)**

```go
// internal/usecase/scoring_evaluate_test.go
package usecase

import (
	"context"
	"testing"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/scoring"
)

type evalRepoFake struct {
	scoringRepoFake
	counts map[string][]entity.ScopeCounts // by scope
	rules  []entity.ScoreRule
	sites  []string
}

func (f *evalRepoFake) AggregateFacts(_ context.Context, q entity.FactQuery) ([]entity.ScopeCounts, error) {
	return f.counts[q.Scope], nil
}
func (f *evalRepoFake) ListRules(context.Context, entity.RuleFilter) ([]entity.ScoreRule, error) { return f.rules, nil }
func (f *evalRepoFake) VendorSites(context.Context, string) ([]string, error)                   { return f.sites, nil }
func (f *evalRepoFake) VendorIDsForSites(context.Context, []string) ([]string, error) {
	return []string{testVendor}, nil
}

func strPtr(s string) *string { return &s }

func TestEvaluateWorkerAtTwoSites(t *testing.T) {
	worker := "40000000-0000-4000-8000-000000000001"
	full := scoring.Counts{Tasks: 10, Completed: 10, OnTime: 10, Reported: 10}
	half := scoring.Counts{Tasks: 10, Completed: 5, OnTime: 5, Reported: 10}
	repo := &evalRepoFake{counts: map[string][]entity.ScopeCounts{
		entity.ScopeWorker: {
			{VendorID: testVendor, SiteID: strPtr(testSite), WorkerID: &worker, Counts: full},
			{VendorID: testVendor, SiteID: strPtr(otherSite), WorkerID: &worker, Counts: half},
		},
	}}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl)
	rule := entity.ScoreRule{ID: "r", Kind: "SLA", VendorID: testVendor, PeriodType: "MONTH",
		Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	results, err := u.evaluate(context.Background(), rule, entity.ScopeWorker, testVendor, nil, nil, start, start.AddDate(0, 1, -1))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || *results[0].SiteID == *results[1].SiteID {
		t.Fatalf("want one result per site, got %+v", results)
	}
	if !results[0].Compliant || results[1].Compliant {
		t.Fatalf("compliance per site wrong: %v, %v", results[0].Compliant, results[1].Compliant)
	}
}

func TestEvaluateVendorNoData(t *testing.T) {
	repo := &evalRepoFake{counts: map[string][]entity.ScopeCounts{}}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl)
	rule := entity.ScoreRule{ID: "r", Kind: "SLA", VendorID: testVendor, PeriodType: "MONTH",
		Items: []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	results, err := u.evaluate(context.Background(), rule, entity.ScopeVendor, testVendor, nil, nil, start, start.AddDate(0, 1, -1))
	if err != nil || len(results) != 1 || results[0].Status != scoring.StatusNoData {
		t.Fatalf("results = %+v, err %v", results, err)
	}
}

func TestEvaluateMaturityUsesHistory(t *testing.T) {
	strong := scoring.Counts{Tasks: 10, Completed: 10, OnTime: 10, Reported: 10}
	repo := &evalRepoFake{counts: map[string][]entity.ScopeCounts{
		entity.ScopeVendor: {{VendorID: testVendor, Counts: strong}},
	}}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl)
	rule := entity.ScoreRule{ID: "r", Kind: "MATURITY", VendorID: testVendor, PeriodType: "MONTH",
		Items:  []entity.RuleItem{{Code: "ON_TIME_RATE", Weight: 100}},
		Levels: scoring.DefaultLevels}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	results, err := u.evaluate(context.Background(), rule, entity.ScopeVendor, testVendor, nil, nil, start, start.AddDate(0, 1, -1))
	if err != nil || len(results) != 1 || results[0].Level != 5 {
		t.Fatalf("three perfect periods should reach level 5: %+v, err %v", results, err)
	}
}

func TestListResultsVisibility(t *testing.T) {
	repo := &evalRepoFake{}
	u := NewScoringUsecase(repo)
	worker := &entity.User{ID: "40000000-0000-4000-8000-000000000001", Roles: []string{"WORKER"}}
	if _, _, err := u.ListResults(context.Background(), worker, entity.ResultFilter{Kind: "SLA", Scope: entity.ScopeVendor}); err != ErrForbidden {
		t.Fatalf("worker reading vendor results: %v", err)
	}
}
```

The fake's `AggregateFacts` returns the same counts for every period, which is what `TestEvaluateMaturityUsesHistory` relies on.

Also add a `ListResults` stub to `evalRepoFake` that records the filter:

```go
func (f *evalRepoFake) ListResults(_ context.Context, rf entity.ResultFilter) ([]entity.ScoreResult, int, error) {
	return nil, 0, nil
}
```

- [ ] **Step 2: Run — expect compile failure**

Run: `go test ./internal/usecase/ -run 'TestEvaluate|TestListResultsVisibility'`
Expected: FAIL (`u.evaluate undefined`).

- [ ] **Step 3: Implement evaluation, live results, and history**

Add to `ScoringUsecase` interface:

```go
	LiveResults(ctx context.Context, actor *entity.User, kind, scope, vendorID string) ([]entity.ScoreResult, error)
	ListResults(ctx context.Context, actor *entity.User, f entity.ResultFilter) ([]entity.ScoreResult, int, error)
```

Change `ScoringUsecaseImpl` and its constructor:

```go
type ScoringUsecaseImpl struct {
	repository repository.ScoringRepository
	location   *time.Location
	now        func() time.Time
}

func NewScoringUsecase(repo repository.ScoringRepository) ScoringUsecase {
	return &ScoringUsecaseImpl{repository: repo, location: time.UTC, now: time.Now}
}

// WithLocation sets the plant time zone used for period boundaries.
func (u *ScoringUsecaseImpl) WithLocation(location *time.Location) *ScoringUsecaseImpl {
	if location != nil {
		u.location = location
	}
	return u
}

func (u *ScoringUsecaseImpl) Clock() time.Time { return u.now().In(u.location) }
```

Then the evaluation code (add `"time"` to imports):

```go
func ruleItems(rule entity.ScoreRule) []scoring.Item {
	items := make([]scoring.Item, 0, len(rule.Items))
	for _, item := range rule.Items {
		si := scoring.Item{Code: item.Code, Weight: item.Weight}
		if item.Target != nil {
			si.Target = *item.Target
		}
		items = append(items, si)
	}
	return items
}

func groupKey(c entity.ScopeCounts) string {
	key := c.VendorID
	if c.SiteID != nil {
		key += "|" + *c.SiteID
	}
	if c.WorkerID != nil {
		key += "|" + *c.WorkerID
	}
	return key
}

// evaluate scores one rule for one scope over [start, end]. Maturity rules also
// score the previous periods needed by the level gates.
func (u *ScoringUsecaseImpl) evaluate(ctx context.Context, rule entity.ScoreRule, scope, vendorID string,
	siteIDs, excludeSiteIDs []string, start, end time.Time) ([]entity.ScoreResult, error) {
	query := entity.FactQuery{Scope: scope, VendorID: vendorID, SiteIDs: siteIDs, ExcludeSiteIDs: excludeSiteIDs, From: start, To: end}
	current, err := u.repository.AggregateFacts(ctx, query)
	if err != nil {
		return nil, err
	}
	items := ruleItems(rule)
	base := func(c entity.ScopeCounts) entity.ScoreResult {
		return entity.ScoreResult{Kind: rule.Kind, RuleID: rule.ID, ScopeType: scope, VendorID: c.VendorID,
			SiteID: c.SiteID, WorkerID: c.WorkerID, PeriodType: rule.PeriodType, PeriodStart: start, PeriodEnd: end}
	}
	// Groups without facts: a vendor with no tasks, or a vendor site with no tasks.
	if scope == entity.ScopeVendor && len(current) == 0 {
		current = []entity.ScopeCounts{{VendorID: vendorID}}
	}
	if scope == entity.ScopeSite {
		sites := siteIDs
		if len(sites) == 0 {
			if sites, err = u.repository.VendorSites(ctx, vendorID); err != nil {
				return nil, err
			}
		}
		present := map[string]bool{}
		for _, c := range current {
			present[*c.SiteID] = true
		}
		for _, site := range sites {
			if !present[site] && !containsString(excludeSiteIDs, site) {
				site := site
				current = append(current, entity.ScopeCounts{VendorID: vendorID, SiteID: &site})
			}
		}
	}

	results := make([]entity.ScoreResult, 0, len(current))
	if rule.Kind == entity.RuleKindSLA {
		for _, c := range current {
			res := base(c)
			res.Result = scoring.EvaluateSLA(c.Counts, items)
			results = append(results, res)
		}
		return results, nil
	}

	// Maturity: history[group] holds oldest..newest period results.
	periods := scoring.MaxConsecutive(rule.Levels)
	history := map[string][]scoring.Result{}
	pt := scoring.PeriodType(rule.PeriodType)
	prevStart := start
	for i := 1; i < periods; i++ {
		prevStart = scoring.PreviousStart(pt, prevStart)
		_, prevEnd := scoring.PeriodBounds(pt, prevStart)
		past, err := u.repository.AggregateFacts(ctx, entity.FactQuery{Scope: scope, VendorID: vendorID,
			SiteIDs: siteIDs, ExcludeSiteIDs: excludeSiteIDs, From: prevStart, To: prevEnd})
		if err != nil {
			return nil, err
		}
		byKey := map[string]scoring.Counts{}
		for _, c := range past {
			byKey[groupKey(c)] = c.Counts
		}
		for _, c := range current {
			key := groupKey(c)
			history[key] = append([]scoring.Result{scoring.EvaluateMaturityPeriod(byKey[key], items)}, history[key]...)
		}
	}
	for _, c := range current {
		res := base(c)
		res.Result = scoring.EvaluateMaturityPeriod(c.Counts, items)
		res.Level = scoring.Level(append(history[groupKey(c)], res.Result), rule.Levels)
		results = append(results, res)
	}
	return results, nil
}
```

Live results and history with access control:

```go
// visibility resolves what the actor may read.
func (u *ScoringUsecaseImpl) visibility(ctx context.Context, actor *entity.User, scope string) (entity.ResultFilter, error) {
	var f entity.ResultFilter
	switch {
	case actor == nil:
		return f, ErrForbidden
	case hasRole(actor, "SUPER_ADMIN"):
		f.AllVendors = true
	case hasRole(actor, "ADMIN"):
		vendors, err := u.repository.VendorIDsForSites(ctx, actor.Sites)
		if err != nil {
			return f, err
		}
		if scope == entity.ScopeVendor {
			f.VendorIDs = vendors
		} else {
			f.SiteIDs = actor.Sites
		}
	case hasRole(actor, "ADMIN_WORKER"):
		f.VendorIDs = actor.Vendors
	case hasRole(actor, "WORKER"):
		if scope != entity.ScopeWorker {
			return f, ErrForbidden
		}
		f.OnlyWorkerID = actor.ID
		f.AllVendors = true
	default:
		return f, ErrForbidden
	}
	return f, nil
}

func validScope(scope string) bool {
	return scope == entity.ScopeVendor || scope == entity.ScopeSite || scope == entity.ScopeWorker
}

func (u *ScoringUsecaseImpl) ListResults(ctx context.Context, actor *entity.User, f entity.ResultFilter) ([]entity.ScoreResult, int, error) {
	if f.Kind != entity.RuleKindSLA && f.Kind != entity.RuleKindMaturity || !validScope(f.Scope) {
		return nil, 0, ErrInvalidOperationalData
	}
	vis, err := u.visibility(ctx, actor, f.Scope)
	if err != nil {
		return nil, 0, err
	}
	f.AllVendors, f.VendorIDs, f.SiteIDs, f.OnlyWorkerID = vis.AllVendors, vis.VendorIDs, vis.SiteIDs, vis.OnlyWorkerID
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	if f.To.IsZero() {
		f.To = u.Clock().AddDate(0, 0, 1)
	}
	if f.From.IsZero() {
		f.From = f.To.AddDate(-1, 0, 0)
	}
	return u.repository.ListResults(ctx, f)
}

// LiveResults scores the running period of every active rule the actor can see.
func (u *ScoringUsecaseImpl) LiveResults(ctx context.Context, actor *entity.User, kind, scope, vendorID string) ([]entity.ScoreResult, error) {
	if kind != entity.RuleKindSLA && kind != entity.RuleKindMaturity || !validScope(scope) {
		return nil, ErrInvalidOperationalData
	}
	vis, err := u.visibility(ctx, actor, scope)
	if err != nil {
		return nil, err
	}
	rules, err := u.repository.ListRules(ctx, entity.RuleFilter{Kind: kind, VendorID: vendorID, ActiveOnly: true,
		AllVendors: vis.AllVendors || len(vis.SiteIDs) > 0, VendorIDs: vis.VendorIDs})
	if err != nil {
		return nil, err
	}
	today := u.Clock()
	var results []entity.ScoreResult
	defaults := map[string]entity.ScoreRule{}
	overrides := map[string][]entity.ScoreRule{}
	for _, rule := range rules {
		if rule.SiteID == nil {
			defaults[rule.VendorID] = rule
		} else {
			overrides[rule.VendorID] = append(overrides[rule.VendorID], rule)
		}
	}
	for vendor, rule := range defaults {
		start, end := scoring.PeriodBounds(scoring.PeriodType(rule.PeriodType), today)
		var exclude []string
		for _, o := range overrides[vendor] {
			exclude = append(exclude, *o.SiteID)
		}
		var res []entity.ScoreResult
		if scope == entity.ScopeVendor {
			res, err = u.evaluate(ctx, rule, scope, vendor, nil, nil, start, end)
		} else {
			res, err = u.evaluate(ctx, rule, scope, vendor, nil, exclude, start, end)
		}
		if err != nil {
			return nil, err
		}
		results = append(results, res...)
	}
	if scope != entity.ScopeVendor {
		for vendor, list := range overrides {
			for _, rule := range list {
				start, end := scoring.PeriodBounds(scoring.PeriodType(rule.PeriodType), today)
				res, err := u.evaluate(ctx, rule, scope, vendor, []string{*rule.SiteID}, nil, start, end)
				if err != nil {
					return nil, err
				}
				results = append(results, res...)
			}
		}
	}
	// Vendors in view without any rule.
	if scope == entity.ScopeVendor && vendorID != "" {
		if _, ok := defaults[vendorID]; !ok {
			results = append(results, entity.ScoreResult{Kind: kind, ScopeType: scope, VendorID: vendorID,
				Result: scoring.Result{Status: entity.ResultStatusNoRule}})
		}
	}
	filtered := results[:0]
	for _, r := range results {
		r.Live = true
		if visibleResult(vis, r) {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func visibleResult(vis entity.ResultFilter, r entity.ScoreResult) bool {
	if vis.OnlyWorkerID != "" {
		return r.WorkerID != nil && *r.WorkerID == vis.OnlyWorkerID
	}
	if vis.AllVendors {
		return true
	}
	if containsString(vis.VendorIDs, r.VendorID) {
		return true
	}
	return r.SiteID != nil && containsString(vis.SiteIDs, *r.SiteID)
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go vet ./internal/... && go test ./internal/usecase/ ./internal/scoring/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/usecase/scoring_usecase.go internal/usecase/scoring_evaluate_test.go
git commit -m "feat(scoring): evaluate rules per scope with live and frozen results"
```

---

### Task 9: Background job — finalize facts and close periods

**Files:**
- Modify: `internal/usecase/scoring_usecase.go`
- Create: `internal/jobs/scoring_job.go`
- Test: `internal/usecase/scoring_close_test.go`, `internal/jobs/scoring_job_test.go`

**Interfaces:**
- Consumes: `evaluate`, repository `FinalizeFacts`, `LastFinalDate`, `RuleKeys`, `RuleVersions`, `OverrideSitesAt`, `ResultExists`, `SaveResult`, `WithJobLock`.
- Produces:

```go
// ScoringUsecase additions
FinalizeFacts(ctx context.Context) (int64, error)
ClosePeriods(ctx context.Context) (int, error) // number of periods frozen
RunJobCycle(ctx context.Context) error          // lock + FinalizeFacts + ClosePeriods

// jobs
func StartScoringJob(ctx context.Context, u usecase.ScoringUsecase, interval time.Duration)
```

- [ ] **Step 1: Write the failing close-period test (covers Review Focus 3)**

```go
// internal/usecase/scoring_close_test.go
package usecase

import (
	"context"
	"testing"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/scoring"
)

type closeRepoFake struct {
	evalRepoFake
	lastFinal time.Time
	versions  []entity.ScoreRule
	saved     []entity.ScoreResult
}

func (f *closeRepoFake) LastFinalDate(context.Context) (time.Time, error) { return f.lastFinal, nil }
func (f *closeRepoFake) RuleKeys(context.Context) ([]entity.RuleKey, error) {
	return []entity.RuleKey{{Kind: "SLA", VendorID: testVendor}}, nil
}
func (f *closeRepoFake) RuleVersions(context.Context, entity.RuleKey) ([]entity.ScoreRule, error) {
	return f.versions, nil
}
func (f *closeRepoFake) OverrideSitesAt(context.Context, string, string, time.Time) ([]string, error) {
	return nil, nil
}
func (f *closeRepoFake) ResultExists(_ context.Context, _, scope, _ string, _ *string, _ string, start time.Time) (bool, error) {
	for _, s := range f.saved {
		if s.ScopeType == scope && s.PeriodStart.Equal(start) {
			return true, nil
		}
	}
	return false, nil
}
func (f *closeRepoFake) SaveResult(_ context.Context, r entity.ScoreResult) error {
	f.saved = append(f.saved, r)
	return nil
}

func TestClosePeriodsScoresFromRuleCreation(t *testing.T) {
	loc := time.UTC
	repo := &closeRepoFake{
		evalRepoFake: evalRepoFake{counts: map[string][]entity.ScopeCounts{
			entity.ScopeVendor: {{VendorID: testVendor, Counts: scoring.Counts{Tasks: 10, Completed: 10, Reported: 10}}},
		}},
		lastFinal: time.Date(2026, 10, 14, 0, 0, 0, 0, loc),
		versions: []entity.ScoreRule{{ID: "v1", Kind: "SLA", VendorID: testVendor, PeriodType: "MONTH", Version: 1,
			CreatedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, loc), // mid-August
			Items:     []entity.RuleItem{{Code: "COMPLETION_RATE", Weight: 100, Target: target(95)}}}},
	}
	u := NewScoringUsecase(repo).(*ScoringUsecaseImpl)
	n, err := u.ClosePeriods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// August (rule created mid-month, scored in full) and September; October not finished.
	var vendorStarts []string
	for _, r := range repo.saved {
		if r.ScopeType == entity.ScopeVendor {
			vendorStarts = append(vendorStarts, r.PeriodStart.Format("2006-01-02"))
		}
	}
	if n != 2 || len(vendorStarts) != 2 || vendorStarts[0] != "2026-08-01" || vendorStarts[1] != "2026-09-01" {
		t.Fatalf("closed %d periods, vendor starts %v; want 2026-08-01 and 2026-09-01", n, vendorStarts)
	}
	// Running again freezes nothing new.
	if n, _ := u.ClosePeriods(context.Background()); n != 0 {
		t.Fatalf("second run closed %d periods, want 0", n)
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

Run: `go test ./internal/usecase/ -run TestClosePeriods`
Expected: FAIL (`u.ClosePeriods undefined`).

- [ ] **Step 3: Implement finalize, close, and the job cycle**

Add to the `ScoringUsecase` interface:

```go
	FinalizeFacts(ctx context.Context) (int64, error)
	ClosePeriods(ctx context.Context) (int, error)
	RunJobCycle(ctx context.Context) error
```

Implementation:

```go
func (u *ScoringUsecaseImpl) FinalizeFacts(ctx context.Context) (int64, error) {
	return u.repository.FinalizeFacts(ctx)
}

// activeAt reports whether a rule version is in force at instant at.
func activeAt(rule entity.ScoreRule, at time.Time) bool {
	return !rule.CreatedAt.After(at) && (rule.SupersededAt == nil || rule.SupersededAt.After(at))
}

// ClosePeriods freezes every finished, fully final period of every rule key,
// scored with the version active when the period ended.
func (u *ScoringUsecaseImpl) ClosePeriods(ctx context.Context) (int, error) {
	lastFinal, err := u.repository.LastFinalDate(ctx)
	if err != nil {
		return 0, err
	}
	keys, err := u.repository.RuleKeys(ctx)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, key := range keys {
		versions, err := u.repository.RuleVersions(ctx, key)
		if err != nil {
			return closed, err
		}
		for _, version := range versions {
			pt := scoring.PeriodType(version.PeriodType)
			start, end := scoring.PeriodBounds(pt, version.CreatedAt.In(u.location))
			for !end.After(lastFinal) {
				periodEndAt := end.AddDate(0, 0, 1) // 00:00 after the last day
				if activeAt(version, periodEndAt) {
					done, err := u.closePeriod(ctx, version, start, end, periodEndAt)
					if err != nil {
						return closed, err
					}
					if done {
						closed++
					}
				}
				start = end.AddDate(0, 0, 1)
				_, end = scoring.PeriodBounds(pt, start)
			}
		}
	}
	return closed, nil
}

// closePeriod freezes one rule version for one period; false if already frozen.
func (u *ScoringUsecaseImpl) closePeriod(ctx context.Context, rule entity.ScoreRule, start, end, endAt time.Time) (bool, error) {
	anchor := entity.ScopeVendor
	if rule.SiteID != nil {
		anchor = entity.ScopeSite
	}
	exists, err := u.repository.ResultExists(ctx, rule.Kind, anchor, rule.VendorID, rule.SiteID, rule.PeriodType, start)
	if err != nil || exists {
		return false, err
	}
	var batches [][]entity.ScoreResult
	if rule.SiteID == nil {
		overrides, err := u.repository.OverrideSitesAt(ctx, rule.Kind, rule.VendorID, endAt)
		if err != nil {
			return false, err
		}
		for _, scope := range []string{entity.ScopeVendor, entity.ScopeSite, entity.ScopeWorker} {
			var exclude []string
			if scope != entity.ScopeVendor {
				exclude = overrides
			}
			res, err := u.evaluate(ctx, rule, scope, rule.VendorID, nil, exclude, start, end)
			if err != nil {
				return false, err
			}
			batches = append(batches, res)
		}
	} else {
		for _, scope := range []string{entity.ScopeSite, entity.ScopeWorker} {
			res, err := u.evaluate(ctx, rule, scope, rule.VendorID, []string{*rule.SiteID}, nil, start, end)
			if err != nil {
				return false, err
			}
			batches = append(batches, res)
		}
	}
	// Save the anchor scope last so a crash mid-way is retried on the next run.
	for i := len(batches) - 1; i >= 0; i-- {
		for _, res := range batches[i] {
			if err := u.repository.SaveResult(ctx, res); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

// RunJobCycle runs one finalize + close pass if this instance holds the job lock.
func (u *ScoringUsecaseImpl) RunJobCycle(ctx context.Context) error {
	_, err := u.repository.WithJobLock(ctx, func(ctx context.Context) error {
		if _, err := u.FinalizeFacts(ctx); err != nil {
			return fmt.Errorf("finalize facts: %w", err)
		}
		if _, err := u.ClosePeriods(ctx); err != nil {
			return fmt.Errorf("close periods: %w", err)
		}
		return nil
	})
	return err
}
```

`ResultExists` is called with the anchor scope; the fake in Step 1 matches on scope and start. Since the anchor batch (index 0) is saved last, a partial failure leaves the anchor missing and the period is retried; `SaveResult`'s `ON CONFLICT DO NOTHING` makes the retry safe.

- [ ] **Step 4: Implement the job runner and its test**

```go
// internal/jobs/scoring_job.go
package jobs

import (
	"context"
	"log"
	"time"
)

type jobCycle interface {
	RunJobCycle(ctx context.Context) error
}

// StartScoringJob runs one cycle immediately, then every interval, until ctx ends.
func StartScoringJob(ctx context.Context, u jobCycle, interval time.Duration) {
	go func() {
		run := func() {
			runCtx, cancel := context.WithTimeout(ctx, interval)
			defer cancel()
			if err := u.RunJobCycle(runCtx); err != nil {
				log.Printf("scoring job: %v", err)
			}
		}
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
```

```go
// internal/jobs/scoring_job_test.go
package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingCycle struct{ runs atomic.Int32 }

func (c *countingCycle) RunJobCycle(context.Context) error { c.runs.Add(1); return nil }

func TestStartScoringJobRunsImmediatelyAndOnTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cycle := &countingCycle{}
	StartScoringJob(ctx, cycle, 20*time.Millisecond)
	time.Sleep(70 * time.Millisecond)
	cancel()
	if n := cycle.runs.Load(); n < 2 {
		t.Fatalf("runs = %d, want at least 2", n)
	}
}
```

- [ ] **Step 5: Run — expect PASS**

Run: `go vet ./internal/... && go test ./internal/usecase/ ./internal/jobs/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/scoring_usecase.go internal/usecase/scoring_close_test.go internal/jobs/
git commit -m "feat(scoring): background job finalizes facts and freezes closed periods"
```

---

### Task 10: HTTP API, wiring, seed, and docs

**Files:**
- Create: `internal/delivery/http/handler/scoring_handler.go`, `internal/delivery/http/handler/scoring_handler_test.go`
- Modify: `internal/delivery/http/router/router.go`, `internal/delivery/http/router/router_test.go`, `cmd/server/main.go`, `ddl/seed_v3.sql`, `readme.md`, `postman/peelen-ms-be.postman_collection.json`

**Interfaces:**
- Consumes: `usecase.ScoringUsecase` (Tasks 6, 8, 9), `jobs.StartScoringJob`.
- Produces endpoints (all under `/api/v1`, authenticated):

| Method & path | Handler |
|---|---|
| `GET /sla-catalog`, `GET /maturity-catalog` | `Catalog(kind)` |
| `GET /sla-rules`, `GET /maturity-rules` | `ListRules(kind)` — query `vendor_id`, `site_id`, `active` (default true) |
| `GET /sla-rules/history`, `GET /maturity-rules/history` | `ListRules(kind)` with `active=false` |
| `GET /sla-rules/:id`, `GET /maturity-rules/:id` | `GetRule` |
| `POST /sla-rules`, `POST /maturity-rules` | `CreateRule(kind)`; body `vendor_id`, `site_id`, `period_type`, `indicators` (SLA) / `dimensions` (maturity), `levels` |
| `DELETE /sla-rules/:id`, `DELETE /maturity-rules/:id` | `DeleteRule` |
| `GET /sla-scores/live`, `GET /maturity-levels/live` | `Live(kind)`; query `scope`, `vendor_id` |
| `GET /sla-scores`, `GET /maturity-levels` | `History(kind)`; query `scope`, `vendor_id`, `site_id`, `worker_id`, `from`, `to`, `page`, `limit` |

- [ ] **Step 1: Write the failing handler test**

```go
// internal/delivery/http/handler/scoring_handler_test.go
package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/usecase"
)

type scoringUsecaseFake struct {
	usecase.ScoringUsecase
	input entity.ScoreRuleInput
}

func (f *scoringUsecaseFake) CreateRule(_ context.Context, _ *entity.User, in entity.ScoreRuleInput) (*entity.ScoreRule, error) {
	f.input = in
	return &entity.ScoreRule{ID: "r", Kind: in.Kind}, nil
}

func TestCreateMaturityRuleMapsDimensions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &scoringUsecaseFake{}
	h := NewScoringHandler(fake)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/maturity-rules", bytes.NewBufferString(`{
		"vendor_id": "10000000-0000-4000-8000-000000000001", "period_type": "QUARTER",
		"dimensions": [{"code": "ON_TIME_RATE", "weight": 100}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateRule(entity.RuleKindMaturity)(c)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if fake.input.Kind != "MATURITY" || len(fake.input.Items) != 1 || fake.input.Items[0].Code != "ON_TIME_RATE" {
		t.Fatalf("input = %+v", fake.input)
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

Run: `go test ./internal/delivery/http/handler/ -run TestCreateMaturityRule`
Expected: FAIL (`undefined: NewScoringHandler`).

- [ ] **Step 3: Implement the handler**

```go
// internal/delivery/http/handler/scoring_handler.go
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/scoring"
	"peelen-ms-be/internal/usecase"
)

type ScoringHandler struct {
	usecase usecase.ScoringUsecase
}

func NewScoringHandler(u usecase.ScoringUsecase) *ScoringHandler {
	return &ScoringHandler{usecase: u}
}

type ruleRequest struct {
	VendorID   string              `json:"vendor_id"`
	SiteID     *string             `json:"site_id"`
	PeriodType string              `json:"period_type"`
	Indicators []entity.RuleItem   `json:"indicators"`
	Dimensions []entity.RuleItem   `json:"dimensions"`
	Levels     []scoring.LevelRule `json:"levels"`
}

func (h *ScoringHandler) Catalog(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var list []scoring.Indicator
		for _, indicator := range h.usecase.Catalog() {
			if kind == entity.RuleKindSLA && indicator.SLA || kind == entity.RuleKindMaturity && indicator.Maturity {
				list = append(list, indicator)
			}
		}
		body := gin.H{"data": list}
		if kind == entity.RuleKindMaturity {
			body["default_levels"] = scoring.DefaultLevels
		}
		c.JSON(http.StatusOK, body)
	}
}

func (h *ScoringHandler) CreateRule(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ruleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
			return
		}
		items := req.Indicators
		if kind == entity.RuleKindMaturity {
			items = req.Dimensions
		}
		rule, err := h.usecase.CreateRule(c.Request.Context(), currentUser(c), entity.ScoreRuleInput{
			Kind: kind, VendorID: req.VendorID, SiteID: req.SiteID, PeriodType: req.PeriodType, Items: items, Levels: req.Levels,
		})
		if err != nil {
			handleWorkflowError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": rule})
	}
}

func (h *ScoringHandler) ListRules(kind string, history bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := entity.RuleFilter{Kind: kind, VendorID: c.Query("vendor_id"), ActiveOnly: !history}
		if site := c.Query("site_id"); site != "" {
			f.SiteID = &site
		}
		rules, err := h.usecase.ListRules(c.Request.Context(), currentUser(c), f)
		if err != nil {
			handleWorkflowError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rules})
	}
}

func (h *ScoringHandler) GetRule(c *gin.Context) {
	rule, err := h.usecase.GetRule(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rule})
}

func (h *ScoringHandler) DeleteRule(c *gin.Context) {
	if err := h.usecase.DeleteRule(c.Request.Context(), currentUser(c), c.Param("id")); err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ScoringHandler) Live(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		results, err := h.usecase.LiveResults(c.Request.Context(), currentUser(c), kind, c.DefaultQuery("scope", entity.ScopeVendor), c.Query("vendor_id"))
		if err != nil {
			handleWorkflowError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": results})
	}
}

func (h *ScoringHandler) History(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := entity.ResultFilter{Kind: kind, Scope: c.DefaultQuery("scope", entity.ScopeVendor),
			VendorID: c.Query("vendor_id"), SiteID: c.Query("site_id"), WorkerID: c.Query("worker_id")}
		var err error
		for key, dest := range map[string]*time.Time{"from": &f.From, "to": &f.To} {
			if value := c.Query(key); value != "" {
				if *dest, err = time.Parse("2006-01-02", value); err != nil {
					writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", key+" must be YYYY-MM-DD")
					return
				}
			}
		}
		f.Page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
		f.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
		results, total, err := h.usecase.ListResults(c.Request.Context(), currentUser(c), f)
		if err != nil {
			handleWorkflowError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": results, "page": f.Page, "limit": f.Limit, "total": total})
	}
}
```

- [ ] **Step 4: Register routes**

In `internal/delivery/http/router/router.go`, add `ScoringHandler *handler.ScoringHandler` to `Config`, and before `return router`:

```go
	for _, kind := range []struct {
		code, rules, results string
	}{
		{entity.RuleKindSLA, "/sla-rules", "/sla-scores"},
		{entity.RuleKindMaturity, "/maturity-rules", "/maturity-levels"},
	} {
		catalog := "/sla-catalog"
		if kind.code == entity.RuleKindMaturity {
			catalog = "/maturity-catalog"
		}
		operations.GET(catalog, cfg.ScoringHandler.Catalog(kind.code))
		rules := operations.Group(kind.rules)
		rules.GET("", cfg.ScoringHandler.ListRules(kind.code, false))
		rules.GET("/history", cfg.ScoringHandler.ListRules(kind.code, true))
		rules.POST("", cfg.ScoringHandler.CreateRule(kind.code))
		rules.GET("/:id", cfg.ScoringHandler.GetRule)
		rules.DELETE("/:id", cfg.ScoringHandler.DeleteRule)
		operations.GET(kind.results+"/live", cfg.ScoringHandler.Live(kind.code))
		operations.GET(kind.results, cfg.ScoringHandler.History(kind.code))
	}
```

In `router_test.go`, add `ScoringHandler: handler.NewScoringHandler(nil),` to the config literal.

- [ ] **Step 5: Wire the server and the job**

In `cmd/server/main.go` after the assignment handler:

```go
	scoringRepository := postgres.NewScoringRepository(db)
	scoringUsecase := usecase.NewScoringUsecase(scoringRepository).(*usecase.ScoringUsecaseImpl).WithLocation(location)
	scoringHandler := handler.NewScoringHandler(scoringUsecase)
	jobCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	jobs.StartScoringJob(jobCtx, scoringUsecase, 15*time.Minute)
```

add `ScoringHandler: scoringHandler,` to `router.Config`, import `"peelen-ms-be/internal/jobs"`, and call `stopJobs()` in the existing shutdown path before `srv.Shutdown`.

- [ ] **Step 6: Run the full suite**

Run: `gofmt -l internal cmd; go vet ./internal/... ./cmd/... && go test ./internal/...`
Expected: no gofmt output; all PASS.

- [ ] **Step 7: Seed 90 days and sample rules**

In `ddl/seed_v3.sql`:
1. In `report_plan`, change `generate_series(current_date - 6, current_date, interval '1 day')` to `generate_series(current_date - 90, current_date, interval '1 day')`.
2. Give vendors different trends by shifting the bucket per vendor and per age. Replace the `bucket` expression with:

```sql
           (abs(hashtext(a.id::text || d::date::text)::bigint) % 100
             + CASE substring(v.code FROM 5)::int
                   WHEN 1 THEN 0                                        -- stable, strong
                   WHEN 2 THEN (current_date - d::date) / 3             -- improving: older days worse
                   WHEN 3 THEN 30 - (current_date - d::date) / 3        -- declining: recent days worse
                   WHEN 4 THEN 15                                       -- steady, middling
                   ELSE 25                                              -- weakest
               END) % 100 AS bucket
```

and add `JOIN vendors v ON v.id = (SELECT vendor_id FROM pipelines p JOIN pipeline_items pi ON pi.pipeline_id = p.id WHERE pi.id = a.pipeline_item_id)` to the `base` CTE's `FROM`. (`% 100` keeps the bucket in range; higher buckets map to worse scenarios.)
3. Append sample rules before `COMMIT;`, created 120 days ago so past periods are scored:

```sql
-- Sample rules: SLA monthly default per vendor, one stricter override at Paiton,
-- maturity monthly default per vendor (default levels).
INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by, created_at)
SELECT 'SLA', v.id, NULL, 'MONTH', 1, sa.id, now() - interval '120 days'
FROM vendors v CROSS JOIN (SELECT id FROM users WHERE username = 'superadmin') sa;

INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by, created_at)
SELECT 'SLA', v.id, s.id, 'MONTH', 1, ad.id, now() - interval '120 days'
FROM vendors v JOIN sites s ON s.code = 'PAITON'
JOIN users ad ON ad.username = 'admin.paiton'
WHERE v.code = 'VND-001';

INSERT INTO score_rule_items (rule_id, code, weight, target)
SELECT r.id, x.code, x.weight, CASE WHEN r.site_id IS NULL THEN x.target ELSE x.strict END
FROM score_rules r
CROSS JOIN (VALUES
    ('COMPLETION_RATE', 40, 95, 97),
    ('ON_TIME_RATE', 40, 90, 95),
    ('EVIDENCE_COMPLETE_RATE', 20, 98, 99)
) AS x(code, weight, target, strict)
WHERE r.kind = 'SLA';

INSERT INTO score_rules (kind, vendor_id, site_id, period_type, version, created_by, created_at)
SELECT 'MATURITY', v.id, NULL, 'MONTH', 1, sa.id, now() - interval '120 days'
FROM vendors v CROSS JOIN (SELECT id FROM users WHERE username = 'superadmin') sa;

INSERT INTO score_rule_items (rule_id, code, weight)
SELECT r.id, x.code, x.weight
FROM score_rules r
CROSS JOIN (VALUES ('COMPLETION_RATE', 30), ('ON_TIME_RATE', 30), ('EVIDENCE_COMPLETE_RATE', 15),
    ('LATE_UPLOAD_RATE', 10), ('CONSISTENCY', 15)) AS x(code, weight)
WHERE r.kind = 'MATURITY';

INSERT INTO score_rule_levels (rule_id, level, min_score, min_dimension, consecutive_periods)
SELECT r.id, l.level, l.min_score, l.min_dimension, l.periods
FROM score_rules r
CROSS JOIN (VALUES (2, 60, 50, 1), (3, 75, 65, 2), (4, 85, 75, 3), (5, 95, 85, 3))
    AS l(level, min_score, min_dimension, periods)
WHERE r.kind = 'MATURITY';
```

Facts and snapshots are not seeded: the job builds them on first server start.

- [ ] **Step 8: End-to-end check against a dev database**

Run: `make db-reset && make run` (in another terminal), wait ~1 minute, then:

```bash
TOKEN=$(curl -s localhost:8080/api/v1/auth/login -d '{"username":"superadmin","password":"Password123!"}' -H 'Content-Type: application/json' | jq -r .access_token)
curl -s "localhost:8080/api/v1/sla-scores?scope=VENDOR&from=2026-07-01" -H "Authorization: Bearer $TOKEN" | jq '.total, [.data[] | {vendor_id, period_start, score, compliant}]'
curl -s "localhost:8080/api/v1/maturity-levels?scope=VENDOR" -H "Authorization: Bearer $TOKEN" | jq '[.data[] | {vendor_id, period_start, level}]'
curl -s "localhost:8080/api/v1/sla-scores/live?scope=SITE" -H "Authorization: Bearer $TOKEN" | jq '[.data[] | {vendor_id, site_id, score, status}]'
```

Expected: frozen monthly results for July–September per vendor with differing scores; maturity levels differing by vendor; live site results including PAITON scored against the stricter override. (Check the login response field name in `auth_handler.go` and adjust `jq -r .access_token` if it differs.) If no database is available, record this step as not run.

- [ ] **Step 9: Docs**

Add a "SLA & maturity" section to `readme.md` listing the endpoints table from this task, the rule body examples, the job (15 minutes, advisory lock), and that `make db-migrate` now runs all migrations. Add a Postman folder "SLA & Maturity" with one request per endpoint (body examples as in the technical doc §10).

- [ ] **Step 10: Commit**

```bash
git add internal/delivery/http/handler/scoring_handler.go internal/delivery/http/handler/scoring_handler_test.go internal/delivery/http/router/ cmd/server/main.go ddl/seed_v3.sql readme.md postman/peelen-ms-be.postman_collection.json
git commit -m "feat(scoring): SLA and maturity API, job wiring, and demo seed"
```

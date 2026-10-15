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

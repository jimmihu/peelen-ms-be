package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

func TestAssignmentRepositoryScopesAndLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", os.Getpid())
	insertID := func(query string, args ...any) string {
		t.Helper()
		var id string
		if err := db.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	actorID := insertID(`INSERT INTO users (username, password_hash) VALUES ($1, 'test') RETURNING id::text`, "astestactor"+suffix)
	workerID := insertID(`INSERT INTO users (username, password_hash) VALUES ($1, 'test') RETURNING id::text`, "astestworker"+suffix)
	otherWorkerID := insertID(`INSERT INTO users (username, password_hash) VALUES ($1, 'test') RETURNING id::text`, "astestother"+suffix)
	siteID := insertID(`INSERT INTO sites (code, name) VALUES ($1, 'Assignment Test') RETURNING id::text`, "AS"+suffix)
	vendorID := insertID(`INSERT INTO vendors (code, name) VALUES ($1, 'Assignment Vendor') RETURNING id::text`, "AS"+suffix)
	otherVendorID := insertID(`INSERT INTO vendors (code, name) VALUES ($1, 'Other Vendor') RETURNING id::text`, "AO"+suffix)
	itemID := insertID(`INSERT INTO items (name, created_by) VALUES ('Assignment item', $1) RETURNING id::text`, actorID)
	pipelineID := insertID(`INSERT INTO pipelines (vendor_id, name) VALUES ($1, 'Assignment pipeline') RETURNING id::text`, vendorID)
	pipelineItemID := insertID(`INSERT INTO pipeline_items (pipeline_id, item_id) VALUES ($1, $2) RETURNING id::text`, pipelineID, itemID)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM pipeline_item_assignments WHERE pipeline_item_id = $1`, pipelineItemID)
		_, _ = db.ExecContext(ctx, `DELETE FROM pipeline_items WHERE id = $1`, pipelineItemID)
		_, _ = db.ExecContext(ctx, `DELETE FROM pipelines WHERE id = $1`, pipelineID)
		_, _ = db.ExecContext(ctx, `DELETE FROM items WHERE id = $1`, itemID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vendor_sites WHERE site_id = $1`, siteID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vendors WHERE id IN ($1, $2)`, vendorID, otherVendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM sites WHERE id = $1`, siteID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id IN ($1, $2, $3)`, actorID, workerID, otherWorkerID)
	}()
	setup := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO vendor_sites (vendor_id, site_id) VALUES ($1, $2)`, []any{vendorID, siteID}},
		{`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = 'WORKER'`, []any{workerID}},
		{`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = 'WORKER'`, []any{otherWorkerID}},
		{`INSERT INTO user_scopes (user_id, vendor_id) VALUES ($1, $2)`, []any{workerID, vendorID}},
		{`INSERT INTO user_scopes (user_id, vendor_id) VALUES ($1, $2)`, []any{otherWorkerID, otherVendorID}},
	}
	for _, step := range setup {
		if _, err := db.ExecContext(ctx, step.query, step.args...); err != nil {
			t.Fatal(err)
		}
	}

	repo := NewAssignmentRepository(db)
	adminWorker := entity.OperationalAccess{UserID: actorID, Roles: []string{"ADMIN_WORKER"}, VendorIDs: []string{vendorID}}
	siteAdmin := entity.OperationalAccess{UserID: actorID, Roles: []string{"ADMIN"}, SiteIDs: []string{siteID}}
	outsider := entity.OperationalAccess{UserID: actorID, Roles: []string{"ADMIN_WORKER"}, VendorIDs: []string{otherVendorID}}
	input := entity.AssignmentInput{PipelineItemID: pipelineItemID, UserID: workerID, Shift: "pagi"}

	if _, err := repo.CreateAssignment(ctx, outsider, input); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("out-of-scope create error = %v, want ErrNotFound", err)
	}
	created, err := repo.CreateAssignment(ctx, adminWorker, input)
	if err != nil {
		t.Fatalf("create assignment: %v", err)
	}
	if !created.IsActive || created.VendorID != vendorID || created.Username != "astestworker"+suffix ||
		created.AssignedBy == nil || *created.AssignedBy != actorID {
		t.Fatalf("unexpected created assignment: %+v", created)
	}
	if _, err := repo.CreateAssignment(ctx, adminWorker, input); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("duplicate create error = %v, want ErrConflict", err)
	}
	wrongVendorWorker := input
	wrongVendorWorker.UserID = otherWorkerID
	if _, err := repo.CreateAssignment(ctx, adminWorker, wrongVendorWorker); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("worker from another vendor error = %v, want ErrNotFound", err)
	}
	nonWorker := input
	nonWorker.UserID = actorID
	if _, err := repo.CreateAssignment(ctx, adminWorker, nonWorker); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("non-worker user error = %v, want ErrNotFound", err)
	}

	if _, err := repo.GetAssignment(ctx, outsider, created.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("out-of-scope get error = %v, want ErrNotFound", err)
	}
	ownWorker := entity.OperationalAccess{UserID: workerID, Roles: []string{"WORKER"}, VendorIDs: []string{vendorID}}
	if _, err := repo.GetAssignment(ctx, ownWorker, created.ID); err != nil {
		t.Fatalf("worker get own assignment: %v", err)
	}
	sameVendorWorker := entity.OperationalAccess{UserID: otherWorkerID, Roles: []string{"WORKER"}, VendorIDs: []string{vendorID}}
	if _, err := repo.GetAssignment(ctx, sameVendorWorker, created.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("worker get other's assignment error = %v, want ErrNotFound", err)
	}

	list, total, err := repo.ListAssignments(ctx, siteAdmin, entity.AssignmentFilter{PipelineID: pipelineID, Page: 1, Limit: 20})
	if err != nil || total != 1 || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("site admin list = %+v total %d err %v", list, total, err)
	}
	if _, total, err := repo.ListAssignments(ctx, outsider, entity.AssignmentFilter{Page: 1, Limit: 20, PipelineID: pipelineID}); err != nil || total != 0 {
		t.Fatalf("outsider list total %d err %v, want 0", total, err)
	}

	if _, err := repo.SetAssignmentActive(ctx, outsider, created.ID, false); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("out-of-scope deactivate error = %v, want ErrNotFound", err)
	}
	deactivated, err := repo.SetAssignmentActive(ctx, adminWorker, created.ID, false)
	if err != nil || deactivated.IsActive {
		t.Fatalf("deactivate = %+v err %v", deactivated, err)
	}
	inactive := false
	if _, total, err := repo.ListAssignments(ctx, adminWorker, entity.AssignmentFilter{Page: 1, Limit: 20, PipelineID: pipelineID, IsActive: &inactive}); err != nil || total != 1 {
		t.Fatalf("inactive filter total %d err %v, want 1", total, err)
	}
	reactivated, err := repo.CreateAssignment(ctx, adminWorker, input)
	if err != nil || reactivated.ID != created.ID || !reactivated.IsActive {
		t.Fatalf("re-create should reactivate the same assignment: %+v err %v", reactivated, err)
	}
}

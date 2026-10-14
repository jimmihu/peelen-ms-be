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

func TestOperationalRepositoryCRUDAndWorkerScopes(t *testing.T) {
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
	var actorID, workerID, siteID, vendorID string
	if err := db.QueryRowContext(ctx, `INSERT INTO users (username, password_hash) VALUES ($1, 'test') RETURNING id::text`, "optestadmin"+suffix).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO users (username, password_hash) VALUES ($1, 'test') RETURNING id::text`, "optestworker"+suffix).Scan(&workerID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO sites (code, name) VALUES ($1, 'Operations Test') RETURNING id::text`, "OT"+suffix).Scan(&siteID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO vendors (code, name) VALUES ($1, 'Operations Test Vendor') RETURNING id::text`, "OT"+suffix).Scan(&vendorID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM pipeline_item_assignments WHERE user_id IN ($1, $2)`, actorID, workerID)
		_, _ = db.ExecContext(ctx, `DELETE FROM pipeline_items WHERE pipeline_id IN (SELECT id FROM pipelines WHERE vendor_id = $1)`, vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM pipelines WHERE vendor_id = $1`, vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM inventories WHERE vendor_id = $1`, vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM items WHERE created_by = $1`, actorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM vendors WHERE id = $1`, vendorID)
		_, _ = db.ExecContext(ctx, `DELETE FROM sites WHERE id = $1`, siteID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, actorID, workerID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO vendor_sites (vendor_id, site_id) VALUES ($1, $2)`, vendorID, siteID); err != nil {
		t.Fatal(err)
	}

	repo := NewOperationalRepository(db)
	superAdmin := entity.OperationalAccess{UserID: actorID, Roles: []string{"SUPER_ADMIN"}}
	inventory, err := repo.CreateOperational(ctx, superAdmin, entity.ResourceInventory, entity.OperationalInput{
		VendorID: &vendorID, Item: stringPtr("Test PPE"),
	}, "")
	if err != nil {
		t.Fatalf("create inventory: %v", err)
	}
	if inventory.VendorID == nil || *inventory.VendorID != vendorID || inventory.Status != "ACTIVE" {
		t.Fatalf("unexpected inventory record: %+v", inventory)
	}
	qty := 4
	inventory, err = repo.UpdateOperational(ctx, superAdmin, entity.ResourceInventory, inventory.ID, entity.OperationalInput{Quantity: &qty}, "")
	if err != nil {
		t.Fatalf("update inventory: %v", err)
	}
	if inventory.Quantity == nil || *inventory.Quantity != qty {
		t.Fatalf("inventory quantity was not updated: %+v", inventory)
	}

	pipeline, err := repo.CreateOperational(ctx, superAdmin, entity.ResourcePipeline, entity.OperationalInput{
		VendorID: &vendorID, Name: stringPtr("Daily Safety"),
	}, "")
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	adminAccess := entity.OperationalAccess{
		UserID: actorID, Roles: []string{"ADMIN"}, SiteIDs: []string{siteID},
	}
	adminPipelines, total, err := repo.ListOperational(ctx, adminAccess, entity.ResourcePipeline, entity.OperationalFilter{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("list pipelines in admin site scope: %v", err)
	}
	if total != 1 || len(adminPipelines) != 1 || adminPipelines[0].ID != pipeline.ID {
		t.Fatalf("admin should see pipelines through its site-vendor relation: total=%d records=%+v", total, adminPipelines)
	}
	outsideScopeVendorID := "30000000-0000-4000-8000-000000000001"
	if _, err := repo.CreateOperational(ctx, adminAccess, entity.ResourcePipeline, entity.OperationalInput{
		VendorID: &outsideScopeVendorID, Name: stringPtr("Out of scope"),
	}, ""); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected out-of-scope vendor to be hidden, got %v", err)
	}
	item, err := repo.CreateOperational(ctx, superAdmin, entity.ResourceItem, entity.OperationalInput{
		Name: stringPtr("Inspect PPE"),
	}, "")
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	pipelineItem, err := repo.CreateOperational(ctx, superAdmin, entity.ResourcePipelineItem, entity.OperationalInput{
		PipelineID: &pipeline.ID, ItemID: &item.ID,
	}, "")
	if err != nil {
		t.Fatalf("create pipeline item: %v", err)
	}

	workerAccess := entity.OperationalAccess{
		UserID: workerID, Roles: []string{"WORKER"}, VendorIDs: []string{vendorID},
	}
	if _, _, err := repo.ListOperational(ctx, workerAccess, entity.ResourcePipeline, entity.OperationalFilter{}); err != nil {
		t.Fatalf("list assigned pipelines: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pipeline_item_assignments (pipeline_item_id, user_id, shift, assigned_by)
		VALUES ($1, $2, 'pagi', $3)
	`, pipelineItem.ID, workerID, actorID); err != nil {
		t.Fatal(err)
	}
	pipelines, total, err := repo.ListOperational(ctx, workerAccess, entity.ResourcePipeline, entity.OperationalFilter{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("list worker pipelines: %v", err)
	}
	if total != 1 || len(pipelines) != 1 || pipelines[0].ID != pipeline.ID {
		t.Fatalf("worker should see the assigned pipeline only: total=%d records=%+v", total, pipelines)
	}
	pipelineItems, total, err := repo.ListOperational(ctx, workerAccess, entity.ResourcePipelineItem, entity.OperationalFilter{PipelineID: pipeline.ID, Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("list worker pipeline items: %v", err)
	}
	if total != 1 || len(pipelineItems) != 1 || pipelineItems[0].ID != pipelineItem.ID {
		t.Fatalf("worker should see the assigned task only: total=%d records=%+v", total, pipelineItems)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE pipeline_item_assignments SET is_active = FALSE
		WHERE pipeline_item_id = $1 AND user_id = $2
	`, pipelineItem.ID, workerID); err != nil {
		t.Fatal(err)
	}
	pipelines, total, err = repo.ListOperational(ctx, workerAccess, entity.ResourcePipeline, entity.OperationalFilter{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("list pipelines after assignment deactivation: %v", err)
	}
	if total != 0 || len(pipelines) != 0 {
		t.Fatalf("worker should not see pipelines with inactive assignments: total=%d records=%+v", total, pipelines)
	}
	pipelineItems, total, err = repo.ListOperational(ctx, workerAccess, entity.ResourcePipelineItem, entity.OperationalFilter{PipelineID: pipeline.ID, Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("list pipeline items after assignment deactivation: %v", err)
	}
	if total != 0 || len(pipelineItems) != 0 {
		t.Fatalf("worker should not see tasks with inactive assignments: total=%d records=%+v", total, pipelineItems)
	}

	if err := repo.DeleteOperational(ctx, superAdmin, entity.ResourceInventory, inventory.ID, ""); err != nil {
		t.Fatalf("soft-delete inventory: %v", err)
	}
	deleted, err := repo.GetOperational(ctx, superAdmin, entity.ResourceInventory, inventory.ID)
	if err != nil {
		t.Fatalf("get soft-deleted inventory: %v", err)
	}
	if deleted.Status != "INACTIVE" {
		t.Fatalf("expected inactive record after delete, got %s", deleted.Status)
	}
	err = repo.DeleteOperational(ctx, superAdmin, entity.ResourceItem, "00000000-0000-4000-8000-000000000099", "")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected resource-not-found error, got %v", err)
	}
}

func stringPtr(value string) *string {
	return &value
}

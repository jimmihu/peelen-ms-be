package usecase

import (
	"context"
	"testing"

	"peelen-ms-be/internal/domain/entity"
)

type operationalRepositoryFake struct {
	listCalls   int
	createCalls int
	access      entity.OperationalAccess
	filter      entity.OperationalFilter
}

func (r *operationalRepositoryFake) ListOperational(_ context.Context, access entity.OperationalAccess, _ entity.OperationalResource, filter entity.OperationalFilter) ([]entity.OperationalRecord, int, error) {
	r.listCalls++
	r.access = access
	r.filter = filter
	return []entity.OperationalRecord{}, 0, nil
}

func (*operationalRepositoryFake) GetOperational(context.Context, entity.OperationalAccess, entity.OperationalResource, string) (*entity.OperationalRecord, error) {
	return nil, nil
}

func (r *operationalRepositoryFake) CreateOperational(_ context.Context, access entity.OperationalAccess, _ entity.OperationalResource, _ entity.OperationalInput, _ string) (*entity.OperationalRecord, error) {
	r.createCalls++
	r.access = access
	return &entity.OperationalRecord{}, nil
}

func (*operationalRepositoryFake) UpdateOperational(context.Context, entity.OperationalAccess, entity.OperationalResource, string, entity.OperationalInput, string) (*entity.OperationalRecord, error) {
	return &entity.OperationalRecord{}, nil
}

func (*operationalRepositoryFake) DeleteOperational(context.Context, entity.OperationalAccess, entity.OperationalResource, string, string) error {
	return nil
}

func TestOperationalUsecaseEnforcesSprintTwoRoleMatrix(t *testing.T) {
	vendorID := "20000000-0000-4000-8000-000000000001"
	pipelineID := "20000000-0000-4000-8000-000000000002"
	itemID := "20000000-0000-4000-8000-000000000003"
	tests := []struct {
		name     string
		role     string
		resource entity.OperationalResource
		wantErr  bool
	}{
		{"admin worker can manage inventory", "ADMIN_WORKER", entity.ResourceInventory, false},
		{"admin worker can manage policy", "ADMIN_WORKER", entity.ResourcePolicy, false},
		{"admin worker cannot manage pipeline", "ADMIN_WORKER", entity.ResourcePipeline, true},
		{"admin worker cannot manage pipeline items", "ADMIN_WORKER", entity.ResourcePipelineItem, true},
		{"admin cannot mutate global items", "ADMIN", entity.ResourceItem, true},
		{"super admin can manage global items", "SUPER_ADMIN", entity.ResourceItem, false},
		{"worker cannot create inventory", "WORKER", entity.ResourceInventory, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &operationalRepositoryFake{}
			service := NewOperationalUsecase(repo)
			actor := &entity.User{ID: "10000000-0000-4000-8000-000000000001", Roles: []string{test.role}}
			input := entity.OperationalInput{}
			switch test.resource {
			case entity.ResourceInventory:
				input.VendorID = &vendorID
				input.Item = operationalStringPtr("Helmet")
			case entity.ResourcePolicy:
				input.VendorID = &vendorID
				input.Name = operationalStringPtr("Safety SOP")
				input.FileURL = operationalStringPtr("policies/test.pdf")
			case entity.ResourcePipeline:
				input.VendorID = &vendorID
				input.Name = operationalStringPtr("Daily pipeline")
			case entity.ResourcePipelineItem:
				input.PipelineID = &pipelineID
				input.ItemID = &itemID
			case entity.ResourceItem:
				input.Name = operationalStringPtr("Inspect equipment")
			}
			_, err := service.Create(context.Background(), actor, test.resource, input, "")
			if test.wantErr && err != ErrForbidden {
				t.Fatalf("expected forbidden, got %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("create should be allowed, got %v", err)
			}
			if test.wantErr && repo.createCalls != 0 {
				t.Fatal("repository was called for a forbidden write")
			}
		})
	}
}

func TestOperationalUsecaseAppliesListDefaultsAndPassesActorScopes(t *testing.T) {
	repo := &operationalRepositoryFake{}
	service := NewOperationalUsecase(repo)
	actor := &entity.User{
		ID: "10000000-0000-4000-8000-000000000001", Roles: []string{"ADMIN"},
		Sites: []string{"20000000-0000-4000-8000-000000000001"},
	}
	_, _, err := service.List(context.Background(), actor, entity.ResourcePipeline, entity.OperationalFilter{Page: -1, Limit: 1000})
	if err != nil {
		t.Fatalf("list operational records: %v", err)
	}
	if repo.filter.Page != 1 || repo.filter.Limit != 20 {
		t.Fatalf("expected default page and limit, got page=%d limit=%d", repo.filter.Page, repo.filter.Limit)
	}
	if repo.access.UserID != actor.ID || len(repo.access.SiteIDs) != 1 || repo.access.SiteIDs[0] != actor.Sites[0] {
		t.Fatalf("actor's authorization scopes were not passed to repository: %+v", repo.access)
	}
}

func TestOperationalUsecaseRejectsScopeTransferAndInvalidInventoryValues(t *testing.T) {
	repo := &operationalRepositoryFake{}
	service := NewOperationalUsecase(repo)
	actor := &entity.User{ID: "10000000-0000-4000-8000-000000000001", Roles: []string{"SUPER_ADMIN"}}
	id := "20000000-0000-4000-8000-000000000001"
	otherVendor := "20000000-0000-4000-8000-000000000002"
	if _, err := service.Update(context.Background(), actor, entity.ResourceInventory, id, entity.OperationalInput{VendorID: &otherVendor}, ""); err != ErrInvalidOperationalData {
		t.Fatalf("expected vendor transfer to be rejected, got %v", err)
	}
	negative := -1
	if _, err := service.Create(context.Background(), actor, entity.ResourceInventory, entity.OperationalInput{
		VendorID: &otherVendor, Item: operationalStringPtr("Helmet"), Quantity: &negative,
	}, ""); err != ErrInvalidOperationalData {
		t.Fatalf("expected negative inventory quantity to be rejected, got %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatal("repository was called for invalid inventory data")
	}
}

func operationalStringPtr(value string) *string {
	return &value
}

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

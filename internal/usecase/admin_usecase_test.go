package usecase

import (
	"context"
	"testing"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type adminRepoFake struct {
	repository.AdminRepository
	createdRoles   []string
	createdSites   []string
	createdVendors []string
	created        bool
}

func (r *adminRepoFake) CreateManagedUser(_ context.Context, user *entity.User, roles, siteIDs, vendorIDs []string, _, _ string) error {
	r.created = true
	r.createdRoles = roles
	r.createdSites = siteIDs
	r.createdVendors = vendorIDs
	user.ID = "10000000-0000-4000-8000-000000000001"
	user.IsActive = true
	return nil
}

func TestAdminWorkerCanOnlyCreateWorkerInOwnVendor(t *testing.T) {
	repo := &adminRepoFake{}
	service := NewAdminUsecase(repo, 12)
	actor := &entity.User{
		ID:      "10000000-0000-4000-8000-000000000002",
		Roles:   []string{"ADMIN_WORKER"},
		Vendors: []string{"20000000-0000-4000-8000-000000000001"},
	}
	email := "worker@example.com"
	user, temporaryPassword, err := service.CreateUser(context.Background(), actor, CreateUserRequest{
		Username: "worker.1", Email: &email, Roles: []string{"WORKER"},
		VendorIDs: []string{"20000000-0000-4000-8000-000000000099"},
	}, "")
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}
	if !repo.created || user.ID == "" || !user.MustChangePassword {
		t.Fatalf("user was not created with required initial password change: %+v", user)
	}
	if len(repo.createdRoles) != 1 || repo.createdRoles[0] != "WORKER" {
		t.Fatalf("unexpected roles assigned: %v", repo.createdRoles)
	}
	if len(repo.createdVendors) != 1 || repo.createdVendors[0] != actor.Vendors[0] {
		t.Fatalf("worker was not forced into actor's vendor: %v", repo.createdVendors)
	}
	if temporaryPassword == "" {
		t.Fatal("expected a generated temporary password")
	}

	repo.created = false
	_, _, err = service.CreateUser(context.Background(), actor, CreateUserRequest{
		Username: "site.admin", Roles: []string{"ADMIN"},
	}, "")
	if err != ErrForbidden {
		t.Fatalf("expected forbidden for creating ADMIN, got %v", err)
	}
	if repo.created {
		t.Fatal("repository was called for forbidden role assignment")
	}
}

func TestScopeRequirementsForRoles(t *testing.T) {
	site := "20000000-0000-4000-8000-000000000001"
	vendor := "20000000-0000-4000-8000-000000000002"
	cases := []struct {
		name                  string
		roles, sites, vendors []string
		want                  bool
	}{
		{"admin requires site", []string{"ADMIN"}, nil, nil, false},
		{"admin with site", []string{"ADMIN"}, []string{site}, nil, true},
		{"worker requires one vendor", []string{"WORKER"}, nil, []string{vendor}, true},
		{"worker requires exactly one vendor", []string{"WORKER"}, nil, []string{vendor, site}, false},
		{"superadmin needs no scope", []string{"SUPER_ADMIN"}, nil, nil, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := scopeMatchesRoles(test.roles, test.sites, test.vendors); got != test.want {
				t.Fatalf("scopeMatchesRoles() = %v, want %v", got, test.want)
			}
		})
	}
}

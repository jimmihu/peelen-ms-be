package usecase

import (
	"context"
	"errors"
	"testing"

	"peelen-ms-be/internal/domain/entity"
)

const (
	testPipelineItemID = "30000000-0000-4000-8000-000000000001"
	testWorkerID       = "30000000-0000-4000-8000-000000000002"
	testAssignmentID   = "30000000-0000-4000-8000-000000000003"
)

type assignmentRepositoryFake struct {
	createCalls int
	setCalls    int
	listCalls   int
	access      entity.OperationalAccess
	input       entity.AssignmentInput
	filter      entity.AssignmentFilter
	active      *bool
}

func (r *assignmentRepositoryFake) ListAssignments(_ context.Context, access entity.OperationalAccess, filter entity.AssignmentFilter) ([]entity.Assignment, int, error) {
	r.listCalls++
	r.access = access
	r.filter = filter
	return []entity.Assignment{}, 0, nil
}

func (*assignmentRepositoryFake) GetAssignment(context.Context, entity.OperationalAccess, string) (*entity.Assignment, error) {
	return &entity.Assignment{}, nil
}

func (r *assignmentRepositoryFake) CreateAssignment(_ context.Context, access entity.OperationalAccess, input entity.AssignmentInput) (*entity.Assignment, error) {
	r.createCalls++
	r.access = access
	r.input = input
	return &entity.Assignment{}, nil
}

func (r *assignmentRepositoryFake) SetAssignmentActive(_ context.Context, access entity.OperationalAccess, _ string, active bool) (*entity.Assignment, error) {
	r.setCalls++
	r.access = access
	r.active = &active
	return &entity.Assignment{}, nil
}

func validAssignmentInput() entity.AssignmentInput {
	return entity.AssignmentInput{PipelineItemID: testPipelineItemID, UserID: testWorkerID, Shift: "pagi"}
}

func TestCreateAssignmentRoleMatrix(t *testing.T) {
	tests := []struct {
		role    string
		wantErr error
	}{
		{"SUPER_ADMIN", ErrForbidden},
		{"ADMIN", ErrForbidden},
		{"ADMIN_WORKER", nil},
		{"WORKER", ErrForbidden},
	}
	for _, test := range tests {
		t.Run(test.role, func(t *testing.T) {
			repo := &assignmentRepositoryFake{}
			service := NewAssignmentUsecase(repo)
			actor := &entity.User{ID: "actor", Roles: []string{test.role}}
			_, err := service.CreateAssignment(context.Background(), actor, validAssignmentInput())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateAssignment() error = %v, want %v", err, test.wantErr)
			}
			wantCalls := 1
			if test.wantErr != nil {
				wantCalls = 0
			}
			if repo.createCalls != wantCalls {
				t.Fatalf("repository create calls = %d, want %d", repo.createCalls, wantCalls)
			}
		})
	}
}

func TestCreateAssignmentNormalizesShiftAndPassesScope(t *testing.T) {
	repo := &assignmentRepositoryFake{}
	service := NewAssignmentUsecase(repo)
	actor := &entity.User{ID: "actor", Roles: []string{"ADMIN_WORKER"}, Vendors: []string{"vendor-a"}}
	input := validAssignmentInput()
	input.Shift = " MALAM "
	if _, err := service.CreateAssignment(context.Background(), actor, input); err != nil {
		t.Fatal(err)
	}
	if repo.input.Shift != "malam" {
		t.Fatalf("shift = %q, want malam", repo.input.Shift)
	}
	if repo.access.UserID != "actor" || len(repo.access.VendorIDs) != 1 || repo.access.VendorIDs[0] != "vendor-a" {
		t.Fatalf("unexpected access passed to repository: %+v", repo.access)
	}
}

func TestCreateAssignmentRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*entity.AssignmentInput)
	}{
		{"invalid pipeline item", func(in *entity.AssignmentInput) { in.PipelineItemID = "abc" }},
		{"invalid user", func(in *entity.AssignmentInput) { in.UserID = "" }},
		{"invalid shift", func(in *entity.AssignmentInput) { in.Shift = "sore" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &assignmentRepositoryFake{}
			service := NewAssignmentUsecase(repo)
			input := validAssignmentInput()
			test.mutate(&input)
			_, err := service.CreateAssignment(context.Background(), &entity.User{Roles: []string{"ADMIN_WORKER"}}, input)
			if !errors.Is(err, ErrInvalidOperationalData) {
				t.Fatalf("CreateAssignment() error = %v, want ErrInvalidOperationalData", err)
			}
			if repo.createCalls != 0 {
				t.Fatal("repository must not be called for invalid input")
			}
		})
	}
}

func TestUpdateAssignmentChangesActiveFlag(t *testing.T) {
	repo := &assignmentRepositoryFake{}
	service := NewAssignmentUsecase(repo)
	inactive := false
	_, err := service.UpdateAssignment(context.Background(), &entity.User{Roles: []string{"ADMIN_WORKER"}}, testAssignmentID,
		entity.AssignmentInput{IsActive: &inactive})
	if err != nil {
		t.Fatal(err)
	}
	if repo.active == nil || *repo.active {
		t.Fatalf("expected repository to deactivate assignment, got %v", repo.active)
	}
}

func TestUpdateAssignmentRequiresIsActive(t *testing.T) {
	service := NewAssignmentUsecase(&assignmentRepositoryFake{})
	_, err := service.UpdateAssignment(context.Background(), &entity.User{Roles: []string{"ADMIN_WORKER"}}, testAssignmentID,
		entity.AssignmentInput{})
	if !errors.Is(err, ErrInvalidOperationalData) {
		t.Fatalf("UpdateAssignment() error = %v, want ErrInvalidOperationalData", err)
	}
}

func TestOnlyAdminWorkerCanChangeAssignments(t *testing.T) {
	for _, role := range []string{"SUPER_ADMIN", "ADMIN", "WORKER"} {
		t.Run(role, func(t *testing.T) {
			repo := &assignmentRepositoryFake{}
			service := NewAssignmentUsecase(repo)
			actor := &entity.User{ID: testWorkerID, Roles: []string{role}}
			active := true
			if _, err := service.UpdateAssignment(context.Background(), actor, testAssignmentID,
				entity.AssignmentInput{IsActive: &active}); !errors.Is(err, ErrForbidden) {
				t.Fatalf("UpdateAssignment() error = %v, want ErrForbidden", err)
			}
			if err := service.DeleteAssignment(context.Background(), actor, testAssignmentID); !errors.Is(err, ErrForbidden) {
				t.Fatalf("DeleteAssignment() error = %v, want ErrForbidden", err)
			}
			if repo.setCalls != 0 {
				t.Fatal("repository must not be called")
			}
		})
	}
}

func TestDeleteAssignmentDeactivates(t *testing.T) {
	repo := &assignmentRepositoryFake{}
	service := NewAssignmentUsecase(repo)
	if err := service.DeleteAssignment(context.Background(), &entity.User{Roles: []string{"ADMIN_WORKER"}}, testAssignmentID); err != nil {
		t.Fatal(err)
	}
	if repo.active == nil || *repo.active {
		t.Fatalf("expected delete to deactivate assignment, got %v", repo.active)
	}
}

func TestGetAssignmentRejectsMalformedID(t *testing.T) {
	service := NewAssignmentUsecase(&assignmentRepositoryFake{})
	_, err := service.GetAssignment(context.Background(), &entity.User{Roles: []string{"ADMIN"}}, "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAssignment() error = %v, want ErrNotFound", err)
	}
}

func TestListAssignmentsLimitsWorkerToOwnAssignments(t *testing.T) {
	repo := &assignmentRepositoryFake{}
	service := NewAssignmentUsecase(repo)
	worker := &entity.User{ID: testWorkerID, Roles: []string{"WORKER"}}
	_, _, err := service.ListAssignments(context.Background(), worker,
		entity.AssignmentFilter{UserID: "30000000-0000-4000-8000-000000000099"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.filter.UserID != testWorkerID {
		t.Fatalf("worker filter user = %q, want own ID", repo.filter.UserID)
	}
	if repo.filter.Page != 1 || repo.filter.Limit != 20 {
		t.Fatalf("expected default paging, got page %d limit %d", repo.filter.Page, repo.filter.Limit)
	}
}

func TestListAssignmentsKeepsManagerFilters(t *testing.T) {
	for _, role := range []string{"SUPER_ADMIN", "ADMIN", "ADMIN_WORKER"} {
		t.Run(role, func(t *testing.T) {
			repo := &assignmentRepositoryFake{}
			actor := &entity.User{ID: "actor", Roles: []string{role}}
			_, _, err := NewAssignmentUsecase(repo).ListAssignments(context.Background(), actor,
				entity.AssignmentFilter{UserID: testWorkerID})
			if err != nil {
				t.Fatal(err)
			}
			if repo.filter.UserID != testWorkerID {
				t.Fatalf("%s filter user = %q, want requested worker", role, repo.filter.UserID)
			}
		})
	}
}

func TestListAssignmentsValidatesFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter entity.AssignmentFilter
	}{
		{"invalid vendor", entity.AssignmentFilter{VendorID: "x"}},
		{"invalid pipeline", entity.AssignmentFilter{PipelineID: "x"}},
		{"invalid shift", entity.AssignmentFilter{Shift: "sore"}},
		{"limit too large", entity.AssignmentFilter{Limit: 101}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &assignmentRepositoryFake{}
			_, _, err := NewAssignmentUsecase(repo).ListAssignments(context.Background(),
				&entity.User{Roles: []string{"ADMIN"}}, test.filter)
			if !errors.Is(err, ErrInvalidOperationalData) {
				t.Fatalf("ListAssignments() error = %v, want ErrInvalidOperationalData", err)
			}
			if repo.listCalls != 0 {
				t.Fatal("repository must not be called for an invalid filter")
			}
		})
	}
}

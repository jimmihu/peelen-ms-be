package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

var ErrInvalidOperationalData = errors.New("invalid operational data")

type OperationalUsecase interface {
	List(ctx context.Context, actor *entity.User, resource entity.OperationalResource, filter entity.OperationalFilter) ([]entity.OperationalRecord, int, error)
	Get(ctx context.Context, actor *entity.User, resource entity.OperationalResource, id string) (*entity.OperationalRecord, error)
	Create(ctx context.Context, actor *entity.User, resource entity.OperationalResource, input entity.OperationalInput, ipAddress string) (*entity.OperationalRecord, error)
	Update(ctx context.Context, actor *entity.User, resource entity.OperationalResource, id string, input entity.OperationalInput, ipAddress string) (*entity.OperationalRecord, error)
	Delete(ctx context.Context, actor *entity.User, resource entity.OperationalResource, id, ipAddress string) error
}

type OperationalUsecaseImpl struct {
	repository repository.OperationalRepository
}

func NewOperationalUsecase(repo repository.OperationalRepository) OperationalUsecase {
	return &OperationalUsecaseImpl{repository: repo}
}

func (u *OperationalUsecaseImpl) List(ctx context.Context, actor *entity.User, resource entity.OperationalResource, filter entity.OperationalFilter) ([]entity.OperationalRecord, int, error) {
	if err := validateOperationalResource(resource); err != nil {
		return nil, 0, err
	}
	if !hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER", "WORKER") {
		return nil, 0, ErrForbidden
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}
	filter.Query = strings.TrimSpace(filter.Query)
	return u.repository.ListOperational(ctx, operationalAccess(actor), resource, filter)
}

func (u *OperationalUsecaseImpl) Get(ctx context.Context, actor *entity.User, resource entity.OperationalResource, id string) (*entity.OperationalRecord, error) {
	if err := validateOperationalResource(resource); err != nil {
		return nil, err
	}
	if !hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER", "WORKER") {
		return nil, ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	record, err := u.repository.GetOperational(ctx, operationalAccess(actor), resource, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	return record, err
}

func (u *OperationalUsecaseImpl) Create(ctx context.Context, actor *entity.User, resource entity.OperationalResource, input entity.OperationalInput, ipAddress string) (*entity.OperationalRecord, error) {
	if err := validateOperationalResource(resource); err != nil {
		return nil, err
	}
	if !canWriteOperational(actor, resource) {
		return nil, ErrForbidden
	}
	if err := validateOperationalInput(resource, input, true); err != nil {
		return nil, err
	}
	return u.repository.CreateOperational(ctx, operationalAccess(actor), resource, input, ipAddress)
}

func (u *OperationalUsecaseImpl) Update(ctx context.Context, actor *entity.User, resource entity.OperationalResource, id string, input entity.OperationalInput, ipAddress string) (*entity.OperationalRecord, error) {
	if err := validateOperationalResource(resource); err != nil {
		return nil, err
	}
	if !canWriteOperational(actor, resource) {
		return nil, ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	if err := validateOperationalInput(resource, input, false); err != nil {
		return nil, err
	}
	record, err := u.repository.UpdateOperational(ctx, operationalAccess(actor), resource, id, input, ipAddress)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	return record, err
}

func (u *OperationalUsecaseImpl) Delete(ctx context.Context, actor *entity.User, resource entity.OperationalResource, id, ipAddress string) error {
	if err := validateOperationalResource(resource); err != nil {
		return err
	}
	if !canWriteOperational(actor, resource) {
		return ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) {
		return ErrNotFound
	}
	err := u.repository.DeleteOperational(ctx, operationalAccess(actor), resource, id, ipAddress)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func operationalAccess(actor *entity.User) entity.OperationalAccess {
	return entity.OperationalAccess{
		UserID: actor.ID, Roles: actor.Roles, SiteIDs: actor.Sites, VendorIDs: actor.Vendors,
	}
}

func canWriteOperational(actor *entity.User, resource entity.OperationalResource) bool {
	switch resource {
	case entity.ResourceInventory, entity.ResourcePolicy:
		return hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER")
	case entity.ResourceItem:
		return hasRole(actor, "SUPER_ADMIN")
	case entity.ResourcePipeline:
		return hasRole(actor, "SUPER_ADMIN", "ADMIN")
	case entity.ResourcePipelineItem:
		return hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER")
	default:
		return false
	}
}

func validateOperationalResource(resource entity.OperationalResource) error {
	switch resource {
	case entity.ResourceInventory, entity.ResourcePolicy, entity.ResourceItem, entity.ResourcePipeline, entity.ResourcePipelineItem:
		return nil
	default:
		return ErrInvalidOperationalData
	}
}

func validateOperationalInput(resource entity.OperationalResource, input entity.OperationalInput, creating bool) error {
	if !operationalFieldsAllowed(resource, input, creating) {
		return ErrInvalidOperationalData
	}
	if input.Quantity != nil && *input.Quantity < 0 {
		return ErrInvalidOperationalData
	}
	if input.SortOrder != nil && *input.SortOrder < 0 {
		return ErrInvalidOperationalData
	}
	if input.Status != nil && *input.Status != "ACTIVE" && *input.Status != "INACTIVE" {
		return ErrInvalidOperationalData
	}
	if input.VendorID != nil && !scopeUUIDPattern.MatchString(*input.VendorID) ||
		input.PipelineID != nil && !scopeUUIDPattern.MatchString(*input.PipelineID) ||
		input.ItemID != nil && !scopeUUIDPattern.MatchString(*input.ItemID) {
		return ErrInvalidOperationalData
	}
	if creating {
		switch resource {
		case entity.ResourceInventory:
			if !hasText(input.VendorID) || !hasText(input.Item) {
				return ErrInvalidOperationalData
			}
			if input.Condition == nil {
				value := "GOOD"
				input.Condition = &value
			}
		case entity.ResourcePolicy, entity.ResourcePipeline:
			if !hasText(input.VendorID) || !hasText(input.Name) {
				return ErrInvalidOperationalData
			}
			if resource == entity.ResourcePolicy && !hasText(input.FileURL) {
				return ErrInvalidOperationalData
			}
		case entity.ResourceItem:
			if !hasText(input.Name) {
				return ErrInvalidOperationalData
			}
		case entity.ResourcePipelineItem:
			if !hasText(input.PipelineID) || !hasText(input.ItemID) {
				return ErrInvalidOperationalData
			}
		}
	} else if inputIsEmpty(input) {
		return ErrInvalidOperationalData
	}
	if input.Condition != nil {
		switch *input.Condition {
		case "GOOD", "DAMAGED", "MAINTENANCE", "LOST":
		default:
			return fmt.Errorf("%w: unsupported inventory condition", ErrInvalidOperationalData)
		}
	}
	if input.Name != nil && (len(strings.TrimSpace(*input.Name)) == 0 || len(*input.Name) > 150) ||
		input.Item != nil && (len(strings.TrimSpace(*input.Item)) == 0 || len(*input.Item) > 150) ||
		input.ItemType != nil && len(*input.ItemType) > 100 {
		return ErrInvalidOperationalData
	}
	return nil
}

func operationalFieldsAllowed(resource entity.OperationalResource, input entity.OperationalInput, creating bool) bool {
	allowed := map[string]bool{}
	if creating {
		switch resource {
		case entity.ResourceInventory:
			allowed = operationalFieldSet("vendor_id", "item", "jenis_item", "qty", "img", "status_condition")
		case entity.ResourcePolicy:
			allowed = operationalFieldSet("vendor_id", "name", "file_url")
		case entity.ResourcePipeline:
			allowed = operationalFieldSet("vendor_id", "name", "description")
		case entity.ResourceItem:
			allowed = operationalFieldSet("name", "description")
		case entity.ResourcePipelineItem:
			allowed = operationalFieldSet("pipeline_id", "item_id", "sort_order")
		}
	} else {
		switch resource {
		case entity.ResourceInventory:
			allowed = operationalFieldSet("qty", "status_condition", "img", "status")
		case entity.ResourcePolicy:
			allowed = operationalFieldSet("name", "file_url", "status")
		case entity.ResourcePipeline:
			allowed = operationalFieldSet("name", "description", "status")
		case entity.ResourceItem:
			allowed = operationalFieldSet("name", "description", "status")
		case entity.ResourcePipelineItem:
			allowed = operationalFieldSet("sort_order", "status")
		}
	}
	return !inputIsEmpty(input) && hasOnlyOperationalFields(input, allowed)
}

func operationalFieldSet(names ...string) map[string]bool {
	result := make(map[string]bool, len(names))
	for _, name := range names {
		result[name] = true
	}
	return result
}

func hasOnlyOperationalFields(input entity.OperationalInput, allowed map[string]bool) bool {
	fields := map[string]bool{
		"vendor_id": input.VendorID != nil, "name": input.Name != nil, "description": input.Description != nil,
		"item": input.Item != nil, "jenis_item": input.ItemType != nil, "qty": input.Quantity != nil,
		"img": input.Image != nil, "status_condition": input.Condition != nil, "file_url": input.FileURL != nil,
		"pipeline_id": input.PipelineID != nil, "item_id": input.ItemID != nil,
		"sort_order": input.SortOrder != nil, "status": input.Status != nil,
	}
	for name, present := range fields {
		if present && !allowed[name] {
			return false
		}
	}
	return true
}

func hasText(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func inputIsEmpty(input entity.OperationalInput) bool {
	return input.VendorID == nil && input.Name == nil && input.Description == nil &&
		input.Item == nil && input.ItemType == nil && input.Quantity == nil && input.Image == nil &&
		input.Condition == nil && input.FileURL == nil && input.PipelineID == nil &&
		input.ItemID == nil && input.SortOrder == nil && input.Status == nil
}

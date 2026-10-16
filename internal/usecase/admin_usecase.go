package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

var (
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrInvalidScope    = errors.New("scope does not match assigned roles")
	ErrInvalidUserData = errors.New("invalid user profile data")
)

var scopeUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type CreateUserRequest struct {
	Username  string   `json:"username" binding:"required,min=3,max=30"`
	Email     *string  `json:"email" binding:"omitempty,email,max=150"`
	FullName  *string  `json:"full_name" binding:"omitempty,max=150"`
	Phone     *string  `json:"phone" binding:"omitempty,max=30"`
	Password  string   `json:"password" binding:"omitempty,min=8,max=72"`
	Roles     []string `json:"roles" binding:"required,min=1"`
	SiteIDs   []string `json:"site_ids"`
	VendorIDs []string `json:"vendor_ids"`
}

type UpdateUserRequest struct {
	FullName *string `json:"full_name"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"`
	IsActive *bool   `json:"is_active"`
}

type ScopeRequest struct {
	SiteIDs   []string `json:"site_ids"`
	VendorIDs []string `json:"vendor_ids"`
}

type AuthorizationRequest struct {
	Roles     []string `json:"roles"`
	SiteIDs   []string `json:"site_ids"`
	VendorIDs []string `json:"vendor_ids"`
}

type AdminUsecase interface {
	ListUsers(ctx context.Context, actor *entity.User) ([]*entity.User, error)
	GetUser(ctx context.Context, actor *entity.User, targetID string) (*entity.User, error)
	CreateUser(ctx context.Context, actor *entity.User, req CreateUserRequest, ipAddress string) (*entity.User, string, error)
	UpdateUser(ctx context.Context, actor *entity.User, targetID string, req UpdateUserRequest, ipAddress string) (*entity.User, error)
	SetAuthorization(ctx context.Context, actor *entity.User, targetID string, req AuthorizationRequest, updateRoles, updateScopes bool, ipAddress string) error
	ResetPassword(ctx context.Context, actor *entity.User, targetID, password string, ipAddress string) (string, error)
	RevokeSessions(ctx context.Context, actor *entity.User, targetID, ipAddress string) error
	ListRoles(ctx context.Context, actor *entity.User) ([]map[string]any, error)
	ListAuditLogs(ctx context.Context, actor *entity.User, userID, action, entityName string, from, to *time.Time, limit int) ([]entity.AuditLog, error)
}

type AdminUsecaseImpl struct {
	repository repository.AdminRepository
	bcryptCost int
}

func NewAdminUsecase(repo repository.AdminRepository, bcryptCost int) AdminUsecase {
	return &AdminUsecaseImpl{repository: repo, bcryptCost: bcryptCost}
}

// managedScope is the set of accounts the actor may manage: Super Admin all
// accounts, Admin the Admin Workers of vendors at its sites, Admin Worker the
// Workers of its vendor. ok is false for anyone else.
func managedScope(actor *entity.User) (scope entity.ManagedUserScope, ok bool) {
	switch {
	case hasRole(actor, "SUPER_ADMIN"):
		return entity.ManagedUserScope{}, true
	case hasRole(actor, "ADMIN"):
		if len(actor.Sites) == 0 {
			return scope, false
		}
		return entity.ManagedUserScope{Role: "ADMIN_WORKER", SiteIDs: actor.Sites}, true
	case hasRole(actor, "ADMIN_WORKER"):
		if len(actor.Vendors) != 1 {
			return scope, false
		}
		return entity.ManagedUserScope{Role: "WORKER", VendorID: actor.Vendors[0]}, true
	}
	return scope, false
}

func (u *AdminUsecaseImpl) ListUsers(ctx context.Context, actor *entity.User) ([]*entity.User, error) {
	scope, ok := managedScope(actor)
	if !ok {
		return nil, ErrForbidden
	}
	return u.repository.ListManagedUsers(ctx, scope)
}

func (u *AdminUsecaseImpl) GetUser(ctx context.Context, actor *entity.User, targetID string) (*entity.User, error) {
	scope, ok := managedScope(actor)
	if !ok {
		return nil, ErrForbidden
	}
	user, err := u.repository.GetManagedUser(ctx, targetID, scope)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrNotFound
	}
	return user, nil
}

func (u *AdminUsecaseImpl) CreateUser(ctx context.Context, actor *entity.User, req CreateUserRequest, ipAddress string) (*entity.User, string, error) {
	scope, ok := managedScope(actor)
	if !ok {
		return nil, "", ErrForbidden
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if !validUsername(req.Username) {
		return nil, "", ErrUsernamePolicy
	}
	req.Roles = uniqueStrings(req.Roles)
	req.SiteIDs = uniqueStrings(req.SiteIDs)
	req.VendorIDs = uniqueStrings(req.VendorIDs)
	if !validScopeIDs(req.SiteIDs, req.VendorIDs) {
		return nil, "", ErrInvalidScope
	}
	if scope.Role != "" {
		// Managers create only the one role they manage.
		if len(req.Roles) != 1 || req.Roles[0] != scope.Role {
			return nil, "", ErrForbidden
		}
		req.SiteIDs = nil
		if scope.VendorID != "" {
			req.VendorIDs = []string{scope.VendorID}
		} else if len(req.VendorIDs) == 1 {
			// An Admin picks a vendor that operates at one of its sites.
			operates, err := u.repository.VendorOperatesAtSites(ctx, req.VendorIDs[0], scope.SiteIDs)
			if err != nil {
				return nil, "", err
			}
			if !operates {
				return nil, "", ErrInvalidScope
			}
		}
	}
	if !validRoleSet(req.Roles) || !scopeMatchesRoles(req.Roles, req.SiteIDs, req.VendorIDs) {
		return nil, "", ErrInvalidScope
	}
	temporaryPassword := req.Password
	if temporaryPassword == "" {
		var err error
		temporaryPassword, err = generateTemporaryPassword()
		if err != nil {
			return nil, "", err
		}
	}
	if err := validatePassword(temporaryPassword); err != nil {
		return nil, "", err
	}
	req.Email = normalizedOptional(req.Email)
	req.FullName = normalizedOptional(req.FullName)
	req.Phone = normalizedOptional(req.Phone)
	if !validProfile(req.FullName, req.Email, req.Phone) {
		return nil, "", ErrInvalidUserData
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(temporaryPassword), u.bcryptCost)
	if err != nil {
		return nil, "", err
	}
	user := &entity.User{
		Username: req.Username, Email: req.Email, FullName: req.FullName, Phone: req.Phone,
		PasswordHash: string(passwordHash), MustChangePassword: true,
	}
	if err := u.repository.CreateManagedUser(ctx, user, req.Roles, req.SiteIDs, req.VendorIDs, actor.ID, ipAddress); err != nil {
		return nil, "", err
	}
	return user, temporaryPassword, nil
}

func (u *AdminUsecaseImpl) UpdateUser(ctx context.Context, actor *entity.User, targetID string, req UpdateUserRequest, ipAddress string) (*entity.User, error) {
	user, err := u.GetUser(ctx, actor, targetID)
	if err != nil {
		return nil, err
	}
	if req.FullName != nil {
		user.FullName = normalizedOptional(req.FullName)
	}
	if req.Email != nil {
		user.Email = normalizedOptional(req.Email)
	}
	if req.Phone != nil {
		user.Phone = normalizedOptional(req.Phone)
	}
	if (req.FullName != nil || req.Email != nil || req.Phone != nil) &&
		!validProfile(user.FullName, user.Email, user.Phone) {
		return nil, ErrInvalidUserData
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}
	if err := u.repository.UpdateManagedUser(ctx, user, actor.ID, ipAddress); err != nil {
		return nil, err
	}
	return u.GetUser(ctx, actor, targetID)
}

func (u *AdminUsecaseImpl) SetAuthorization(ctx context.Context, actor *entity.User, targetID string, req AuthorizationRequest, updateRoles, updateScopes bool, ipAddress string) error {
	if !hasRole(actor, "SUPER_ADMIN") {
		return ErrForbidden
	}
	current, err := u.repository.GetManagedUser(ctx, targetID, entity.ManagedUserScope{})
	if err != nil || current == nil {
		if err != nil {
			return err
		}
		return ErrNotFound
	}
	roles, siteIDs, vendorIDs := current.Roles, current.Sites, current.Vendors
	if updateRoles {
		roles = uniqueStrings(req.Roles)
	}
	if updateScopes {
		siteIDs = uniqueStrings(req.SiteIDs)
		vendorIDs = uniqueStrings(req.VendorIDs)
	}
	if !validRoleSet(roles) || !validScopeIDs(siteIDs, vendorIDs) || !scopeMatchesRoles(roles, siteIDs, vendorIDs) {
		return ErrInvalidScope
	}
	return u.repository.SetUserAuthorization(ctx, targetID, roles, siteIDs, vendorIDs, actor.ID, ipAddress)
}

func (u *AdminUsecaseImpl) ResetPassword(ctx context.Context, actor *entity.User, targetID, password string, ipAddress string) (string, error) {
	// GetUser limits the target to the actor's managed accounts.
	if _, err := u.GetUser(ctx, actor, targetID); err != nil {
		return "", err
	}
	var err error
	if password == "" {
		password, err = generateTemporaryPassword()
		if err != nil {
			return "", err
		}
	}
	if err := validatePassword(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), u.bcryptCost)
	if err != nil {
		return "", err
	}
	if err := u.repository.ResetUserPassword(ctx, targetID, string(hash), actor.ID, ipAddress); err != nil {
		return "", err
	}
	return password, nil
}

func (u *AdminUsecaseImpl) RevokeSessions(ctx context.Context, actor *entity.User, targetID, ipAddress string) error {
	if !hasRole(actor, "SUPER_ADMIN") {
		return ErrForbidden
	}
	if _, err := u.GetUser(ctx, actor, targetID); err != nil {
		return err
	}
	return u.repository.RevokeManagedUserSessions(ctx, targetID, actor.ID, ipAddress)
}

func (u *AdminUsecaseImpl) ListRoles(ctx context.Context, actor *entity.User) ([]map[string]any, error) {
	if !hasRole(actor, "SUPER_ADMIN") {
		return nil, ErrForbidden
	}
	return u.repository.ListRoles(ctx)
}

func (u *AdminUsecaseImpl) ListAuditLogs(ctx context.Context, actor *entity.User, userID, action, entityName string, from, to *time.Time, limit int) ([]entity.AuditLog, error) {
	if !hasRole(actor, "SUPER_ADMIN") {
		return nil, ErrForbidden
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	return u.repository.ListAuditLogs(ctx, userID, action, entityName, from, to, limit)
}

func hasRole(user *entity.User, roles ...string) bool {
	for _, role := range user.Roles {
		for _, allowed := range roles {
			if role == allowed {
				return true
			}
		}
	}
	return false
}

func validRoleSet(roles []string) bool {
	if len(roles) == 0 {
		return false
	}
	allowed := map[string]bool{"SUPER_ADMIN": true, "ADMIN": true, "ADMIN_WORKER": true, "WORKER": true}
	for _, role := range roles {
		if !allowed[role] {
			return false
		}
	}
	return true
}

func scopeMatchesRoles(roles, siteIDs, vendorIDs []string) bool {
	if len(roles) == 0 {
		return false
	}
	hasAdmin, needsVendor := false, false
	for _, role := range roles {
		switch role {
		case "ADMIN":
			hasAdmin = true
		case "ADMIN_WORKER", "WORKER":
			needsVendor = true
		case "SUPER_ADMIN":
		default:
			return false
		}
	}
	if hasAdmin && len(siteIDs) == 0 {
		return false
	}
	if needsVendor && len(vendorIDs) != 1 {
		return false
	}
	if !hasAdmin && !needsVendor && len(siteIDs)+len(vendorIDs) != 0 {
		return false
	}
	if hasAdmin && !needsVendor && len(vendorIDs) != 0 {
		return false
	}
	return true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validScopeIDs(siteIDs, vendorIDs []string) bool {
	for _, id := range append(append([]string{}, siteIDs...), vendorIDs...) {
		if !scopeUUIDPattern.MatchString(id) {
			return false
		}
	}
	return true
}

func normalizedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func validProfile(fullName, email, phone *string) bool {
	if fullName != nil && len(*fullName) > 150 || phone != nil && len(*phone) > 30 {
		return false
	}
	if email != nil {
		if len(*email) > 150 {
			return false
		}
		address, err := mail.ParseAddress(*email)
		if err != nil || address.Address != *email {
			return false
		}
	}
	return true
}

func generateTemporaryPassword() (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		value, err := generateToken(18)
		if err != nil {
			return "", err
		}
		if err := validatePassword(value); err == nil {
			return value, nil
		}
	}
	return "", fmt.Errorf("unable to generate a temporary password meeting the password policy")
}

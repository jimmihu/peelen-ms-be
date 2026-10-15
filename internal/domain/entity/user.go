package entity

import "time"

// User represents a user in the system
type User struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	Email              *string    `json:"email,omitempty"`
	FullName           *string    `json:"full_name,omitempty"`
	Phone              *string    `json:"phone,omitempty"`
	PasswordHash       string     `json:"-"`
	MustChangePassword bool       `json:"must_change_password"`
	FailedLoginCount   int        `json:"failed_login_count"`
	LockedUntil        *time.Time `json:"locked_until,omitempty"`
	IsActive           bool       `json:"is_active"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Roles              []string   `json:"roles,omitempty"`
	Sites              []string   `json:"site_ids,omitempty"`
	Vendors            []string   `json:"vendor_ids,omitempty"`
	// Scopes repeats Sites and Vendors with display names (user management only).
	Scopes []UserScope `json:"scopes,omitempty"`
}

// UserScope is one site or vendor a user is scoped to.
type UserScope struct {
	Type string `json:"type"` // SITE | VENDOR
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

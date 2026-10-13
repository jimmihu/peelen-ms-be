package entity

import "time"

// Session represents an authentication session
type Session struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	TokenHash    string     `json:"-"`
	ExpiresAt    time.Time  `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UserAgent    string     `json:"-"`
	IPAddress    string     `json:"-"`
}

type RefreshToken struct {
	ID              string
	UserID          string
	SessionID       string
	TokenHash       string
	AccessTokenHash string
	ExpiresAt       time.Time
	AccessExpiresAt time.Time
	RevokedAt       *time.Time
	UserAgent       string
	IPAddress       string
}

// IsExpired checks if the session has expired
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsValid checks if the session is valid (not expired and not revoked)
func (s *Session) IsValid() bool {
	return !s.IsExpired() && s.RevokedAt == nil
}

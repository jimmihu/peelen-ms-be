package entity

import (
	"encoding/json"
	"time"
)

type AuditLog struct {
	ID         int64           `json:"id"`
	UserID     *string         `json:"user_id,omitempty"`
	Action     string          `json:"action"`
	Entity     *string         `json:"entity,omitempty"`
	EntityID   *string         `json:"entity_id,omitempty"`
	BeforeData json.RawMessage `json:"before_data,omitempty"`
	AfterData  json.RawMessage `json:"after_data,omitempty"`
	IPAddress  *string         `json:"ip_address,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

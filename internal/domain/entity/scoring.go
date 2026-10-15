package entity

import (
	"time"

	"peelen-ms-be/internal/scoring"
)

const (
	ScopeVendor = "VENDOR"
	ScopeSite   = "SITE"
	ScopeWorker = "WORKER"

	RuleKindSLA      = "SLA"
	RuleKindMaturity = "MATURITY"

	ResultStatusNoRule = "NO_RULE"
)

type FactQuery struct {
	Scope          string
	VendorID       string
	SiteIDs        []string
	ExcludeSiteIDs []string
	WorkerID       string // optional: one worker only
	From, To       time.Time
}

// LiveFilter selects what a live score request covers.
type LiveFilter struct {
	Kind     string
	Scope    string
	VendorID string
	SiteID   string // SITE and WORKER scopes only
	WorkerID string // WORKER scope only
}

type ScopeCounts struct {
	VendorID string
	SiteID   *string
	WorkerID *string
	Counts   scoring.Counts
}

type RuleItem struct {
	Code   string   `json:"code"`
	Weight float64  `json:"weight"`
	Target *float64 `json:"target,omitempty"`
}

type ScoreRule struct {
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	VendorID     string              `json:"vendor_id"`
	SiteID       *string             `json:"site_id,omitempty"`
	PeriodType   string              `json:"period_type"`
	Version      int                 `json:"version"`
	Items        []RuleItem          `json:"items"`
	Levels       []scoring.LevelRule `json:"levels,omitempty"`
	CreatedBy    *string             `json:"created_by,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	SupersededAt *time.Time          `json:"superseded_at,omitempty"`
}

type ScoreRuleInput struct {
	Kind       string              `json:"-"`
	VendorID   string              `json:"vendor_id"`
	SiteID     *string             `json:"site_id"`
	PeriodType string              `json:"period_type"`
	Items      []RuleItem          `json:"items"`
	Levels     []scoring.LevelRule `json:"levels"`
}

type RuleFilter struct {
	Kind       string
	VendorID   string
	SiteID     *string
	ActiveOnly bool
	// Visibility, set by the usecase from the caller's access.
	AllVendors bool
	VendorIDs  []string
}

type RuleKey struct {
	Kind     string
	VendorID string
	SiteID   *string
}

type ScoreResult struct {
	ID          string    `json:"id,omitempty"`
	Kind        string    `json:"kind"`
	RuleID      string    `json:"rule_id,omitempty"`
	ScopeType   string    `json:"scope_type"`
	VendorID    string    `json:"vendor_id"`
	SiteID      *string   `json:"site_id,omitempty"`
	WorkerID    *string   `json:"worker_id,omitempty"`
	PeriodType  string    `json:"period_type,omitempty"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Live        bool      `json:"live"`
	scoring.Result
}

type ResultFilter struct {
	Kind, Scope                string
	VendorID, SiteID, WorkerID string
	From, To                   time.Time
	Page, Limit                int
	// Visibility, set by the usecase.
	AllVendors   bool
	VendorIDs    []string
	SiteIDs      []string
	OnlyWorkerID string
}

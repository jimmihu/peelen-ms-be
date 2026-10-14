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
	From, To       time.Time
}

type ScopeCounts struct {
	VendorID string
	SiteID   *string
	WorkerID *string
	Counts   scoring.Counts
}

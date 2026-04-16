package approval

import "time"

// Status is the approval state.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// StatusResult is the normalized approval status payload from any client mode.
type StatusResult struct {
	Status      Status
	Reason      string
	BypassScope BypassScope
	BypassTTL   time.Duration
}

// BypassScope controls how bypass rules are matched.
type BypassScope string

const (
	BypassScopeExact      BypassScope = "exact"
	BypassScopeTable      BypassScope = "table"
	BypassScopeOneTime    BypassScope = "one_time"
	BypassScopeToolAndTbl BypassScope = "tool_table"
)

// Request describes one approval request.
type Request struct {
	ID                string
	Fingerprint       string
	Tool              string
	TableName         string
	PayloadJSON       string
	Status            Status
	ExternalApprovalID string
	Reason            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// BypassRule stores auto-pass policy.
type BypassRule struct {
	ID          int64
	Scope       BypassScope
	Fingerprint string
	Tool        string
	TableName   string
	ExpiresAt   time.Time
	Enabled     bool
	CreatedAt   time.Time
}

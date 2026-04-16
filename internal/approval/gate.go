package approval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Decision is authorization result for one tool call.
type Decision string

const (
	DecisionAllow   Decision = "allow"
	DecisionPending Decision = "pending"
	DecisionReject  Decision = "reject"
)

// GateInput contains request info used by approval gate.
type GateInput struct {
	RequestID string
	Tool      string
	TableName string
	Payload   string
}

// GateResult returns gate decision and context fields.
type GateResult struct {
	Decision   Decision `json:"decision"`
	ApprovalID string   `json:"approval_id,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// ApprovalClient is the external async approval system client.
type ApprovalClient interface {
	Submit(ctx context.Context, req SubmitRequest) (string, error)
	GetStatus(ctx context.Context, externalID string) (StatusResult, error)
}

// Gate orchestrates approval enforcement.
type Gate struct {
	store          Store
	client         ApprovalClient
	requestTimeout time.Duration
}

// NewGate creates a gate instance.
func NewGate(store Store, client ApprovalClient, requestTimeout time.Duration) *Gate {
	if requestTimeout <= 0 {
		requestTimeout = 30 * time.Minute
	}
	return &Gate{store: store, client: client, requestTimeout: requestTimeout}
}

// Authorize checks if tool execution is currently allowed.
func (g *Gate) Authorize(ctx context.Context, input GateInput) (GateResult, error) {
	if strings.TrimSpace(input.RequestID) == "" {
		return GateResult{}, fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(input.Tool) == "" {
		return GateResult{}, fmt.Errorf("tool is required")
	}
	if strings.TrimSpace(input.TableName) == "" {
		return GateResult{}, fmt.Errorf("table_name is required")
	}

	fingerprint := BuildFingerprint(input.Tool, input.TableName, input.Payload)
	if ok, err := g.store.MatchBypassRule(ctx, fingerprint, input.Tool, input.TableName); err != nil {
		return GateResult{}, err
	} else if ok {
		return GateResult{Decision: DecisionAllow, Reason: "bypass_rule"}, nil
	}

	if ok, err := g.store.ConsumeOneTimeApproval(ctx, fingerprint, input.Tool, input.TableName); err != nil {
		return GateResult{}, err
	} else if ok {
		return GateResult{Decision: DecisionAllow, Reason: "approved_once"}, nil
	}

	existing, err := g.store.GetRequest(ctx, input.RequestID)
	if err != nil {
		return GateResult{}, err
	}
	if existing != nil {
		switch existing.Status {
		case StatusApproved:
			_ = g.store.CreateOneTimeApproval(ctx, input.RequestID, fingerprint, input.Tool, input.TableName)
			_, _ = g.store.ConsumeOneTimeApproval(ctx, fingerprint, input.Tool, input.TableName)
			return GateResult{Decision: DecisionAllow, Reason: existing.Reason}, nil
		case StatusRejected:
			return GateResult{Decision: DecisionReject, ApprovalID: existing.ID, Reason: existing.Reason}, nil
		default:
			_ = g.RefreshStatus(ctx, input.RequestID)
			existing2, err := g.store.GetRequest(ctx, input.RequestID)
			if err != nil {
				return GateResult{}, err
			}
			if existing2 != nil {
				switch existing2.Status {
				case StatusApproved:
					_ = g.store.CreateOneTimeApproval(ctx, input.RequestID, fingerprint, input.Tool, input.TableName)
					_, _ = g.store.ConsumeOneTimeApproval(ctx, fingerprint, input.Tool, input.TableName)
					return GateResult{Decision: DecisionAllow, Reason: existing2.Reason}, nil
				case StatusRejected:
					return GateResult{Decision: DecisionReject, ApprovalID: existing2.ID, Reason: existing2.Reason}, nil
				}
			}
			return GateResult{Decision: DecisionPending, ApprovalID: existing.ID, Reason: existing.Reason}, nil
		}
	}

	extID, err := g.client.Submit(ctx, SubmitRequest{
		RequestID:   input.RequestID,
		Fingerprint: fingerprint,
		Tool:        input.Tool,
		TableName:   input.TableName,
		Summary:     buildSummary(input.Tool, input.TableName),
	})
	if err != nil {
		return GateResult{}, fmt.Errorf("submit approval error: %w", err)
	}

	err = g.store.CreateRequest(ctx, Request{
		ID:                 input.RequestID,
		Fingerprint:        fingerprint,
		Tool:               input.Tool,
		TableName:          input.TableName,
		PayloadJSON:        input.Payload,
		Status:             StatusPending,
		ExternalApprovalID: extID,
	})
	if err != nil {
		return GateResult{}, err
	}
	return GateResult{Decision: DecisionPending, ApprovalID: input.RequestID}, nil
}

// RefreshStatus polls external approval status and syncs request state.
func (g *Gate) RefreshStatus(ctx context.Context, requestID string) error {
	req, err := g.store.GetRequest(ctx, requestID)
	if err != nil {
		return err
	}
	if req == nil {
		return fmt.Errorf("approval request not found: %s", requestID)
	}
	if req.Status != StatusPending {
		return nil
	}
	if strings.TrimSpace(req.ExternalApprovalID) == "" {
		return fmt.Errorf("external_approval_id is empty for request %s", requestID)
	}

	result, err := g.client.GetStatus(ctx, req.ExternalApprovalID)
	if err != nil {
		return err
	}
	switch result.Status {
	case StatusPending:
		return nil
	case StatusApproved:
		if err := g.store.UpdateRequestStatus(ctx, requestID, StatusApproved, result.Reason); err != nil {
			return err
		}
		if err := g.applyBypassOrOneTime(ctx, req, result.BypassScope, result.BypassTTL); err != nil {
			return err
		}
		return nil
	case StatusRejected:
		return g.store.UpdateRequestStatus(ctx, requestID, StatusRejected, result.Reason)
	default:
		return fmt.Errorf("unknown approval status: %s", result.Status)
	}
}

// ApplyCallbackResult updates local state from webhook callback result.
func (g *Gate) ApplyCallbackResult(ctx context.Context, requestID string, status Status, reason string, bypassScope BypassScope, bypassTTL time.Duration) error {
	req, err := g.store.GetRequest(ctx, requestID)
	if err != nil {
		return err
	}
	if req == nil {
		return fmt.Errorf("approval request not found: %s", requestID)
	}

	switch status {
	case StatusApproved:
		if err := g.store.UpdateRequestStatus(ctx, requestID, StatusApproved, reason); err != nil {
			return err
		}
		if err := g.applyBypassOrOneTime(ctx, req, bypassScope, bypassTTL); err != nil {
			return err
		}
		return nil
	case StatusRejected:
		return g.store.UpdateRequestStatus(ctx, requestID, StatusRejected, reason)
	default:
		return fmt.Errorf("unsupported callback status: %s", status)
	}
}

func (g *Gate) applyBypassOrOneTime(ctx context.Context, req *Request, bypassScope BypassScope, bypassTTL time.Duration) error {
	if bypassScope != "" && bypassTTL > 0 && bypassScope != BypassScopeOneTime {
		rule := BypassRule{
			Scope:       bypassScope,
			Fingerprint: req.Fingerprint,
			Tool:        req.Tool,
			TableName:   req.TableName,
			ExpiresAt:   time.Now().Add(bypassTTL),
			Enabled:     true,
		}
		return g.store.AddBypassRule(ctx, rule)
	}
	return g.store.CreateOneTimeApproval(ctx, req.ID, req.Fingerprint, req.Tool, req.TableName)
}

// BuildFingerprint builds stable fingerprint for one logical query request.
func BuildFingerprint(tool, table, payload string) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(tool)))
	h.Write([]byte("|"))
	h.Write([]byte(strings.TrimSpace(table)))
	h.Write([]byte("|"))
	h.Write([]byte(strings.TrimSpace(payload)))
	return hex.EncodeToString(h.Sum(nil))
}

func buildSummary(tool, table string) string {
	return fmt.Sprintf("tool=%s table=%s", tool, table)
}

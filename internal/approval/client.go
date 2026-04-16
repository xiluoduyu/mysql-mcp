package approval

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ClientConfig contains external approval API settings.
type ClientConfig struct {
	BaseURL            string
	SubmitPath         string
	StatusPathTemplate string
	HTTPClient         *http.Client
}

// SubmitRequest is the payload sent to approval service.
type SubmitRequest struct {
	RequestID   string `json:"request_id"`
	Fingerprint string `json:"fingerprint"`
	Tool        string `json:"tool,omitempty"`
	TableName   string `json:"table_name,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

type submitResponse struct {
	ApprovalID string `json:"approval_id"`
}

type statusResponse struct {
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	BypassScope string `json:"bypass_scope"`
	BypassTTL   string `json:"bypass_ttl"`
}

// Client wraps external async approval APIs.
type Client struct {
	baseURL            string
	submitPath         string
	statusPathTemplate string
	httpClient         *http.Client
}

// NewClient creates a new approval client.
func NewClient(cfg ClientConfig) *Client {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:            strings.TrimRight(cfg.BaseURL, "/"),
		submitPath:         cfg.SubmitPath,
		statusPathTemplate: cfg.StatusPathTemplate,
		httpClient:         hc,
	}
}

// Submit sends a new approval request and returns external approval id.
func (c *Client) Submit(ctx context.Context, req SubmitRequest) (string, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal submit request error: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.submitPath, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create submit request error: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("submit approval request error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return "", fmt.Errorf("submit approval got status %d body=%q", resp.StatusCode, string(body))
	}
	var sr submitResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", fmt.Errorf("decode submit response error: %w", err)
	}
	if sr.ApprovalID == "" {
		return "", fmt.Errorf("submit response missing approval_id")
	}
	return sr.ApprovalID, nil
}

// GetStatus retrieves current approval status by external id.
func (c *Client) GetStatus(ctx context.Context, externalID string) (StatusResult, error) {
	path := strings.ReplaceAll(c.statusPathTemplate, "{id}", externalID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return StatusResult{}, fmt.Errorf("create status request error: %w", err)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return StatusResult{}, fmt.Errorf("get approval status request error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return StatusResult{}, fmt.Errorf("get status got status %d body=%q", resp.StatusCode, string(body))
	}
	var sr statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return StatusResult{}, fmt.Errorf("decode status response error: %w", err)
	}
	status := Status(strings.ToLower(strings.TrimSpace(sr.Status)))
	switch status {
	case StatusPending, StatusApproved, StatusRejected:
		result := StatusResult{
			Status: status,
			Reason: sr.Reason,
		}
		if strings.TrimSpace(sr.BypassScope) != "" {
			result.BypassScope = BypassScope(strings.TrimSpace(sr.BypassScope))
		}
		if strings.TrimSpace(sr.BypassTTL) != "" {
			d, err := time.ParseDuration(sr.BypassTTL)
			if err != nil {
				return StatusResult{}, fmt.Errorf("invalid bypass_ttl %q: %w", sr.BypassTTL, err)
			}
			result.BypassTTL = d
		}
		return result, nil
	default:
		return StatusResult{}, fmt.Errorf("unknown status %q", sr.Status)
	}
}

// VerifyCallbackSignature checks callback HMAC signature.
// Message format: hex(hmac_sha256(secret, timestamp + "." + body)).
func VerifyCallbackSignature(secret, ts, sig string, body []byte, maxSkew time.Duration) bool {
	if secret == "" || ts == "" || sig == "" {
		return false
	}
	tsUnix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	if abs(now-tsUnix) > int64(maxSkew.Seconds()) {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := mac.Sum(nil)
	provided, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(expected, provided) == 1
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

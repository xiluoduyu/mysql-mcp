package approval

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ncruces/zenity"
)

// LocalDesktopClientConfig contains local desktop approval behavior options.
type LocalDesktopClientConfig struct {
	RequestTimeout time.Duration
	Notifier       func(title, message string) error
	Asker          func(title, message string) (approved bool, reason string, err error)
	BypassAsker    func(title, message string, defaultTTL time.Duration) (BypassScope, time.Duration, error)
	DefaultBypassTTL time.Duration
}

type localDecision struct {
	status      Status
	reason      string
	bypassScope BypassScope
	bypassTTL   time.Duration
}

// LocalDesktopClient is an in-process ApprovalClient that asks user on local desktop.
type LocalDesktopClient struct {
	timeout  time.Duration
	notifier func(title, message string) error
	asker    func(title, message string) (bool, string, error)
	bypassAsker func(title, message string, defaultTTL time.Duration) (BypassScope, time.Duration, error)
	defaultBypassTTL time.Duration

	mu        sync.RWMutex
	decisions map[string]localDecision
}

// NewLocalDesktopClient creates local desktop approval client.
func NewLocalDesktopClient(cfg LocalDesktopClientConfig) *LocalDesktopClient {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	notifier := cfg.Notifier
	if notifier == nil {
		notifier = defaultDesktopNotifier
	}
	asker := cfg.Asker
	if asker == nil {
		asker = defaultDesktopAsker
	}
	bypassAsker := cfg.BypassAsker
	if bypassAsker == nil {
		bypassAsker = defaultDesktopBypassAsker
	}
	defaultBypassTTL := cfg.DefaultBypassTTL
	if defaultBypassTTL <= 0 {
		defaultBypassTTL = 30 * time.Minute
	}
	return &LocalDesktopClient{
		timeout:   timeout,
		notifier:  notifier,
		asker:     asker,
		bypassAsker: bypassAsker,
		defaultBypassTTL: defaultBypassTTL,
		decisions: make(map[string]localDecision),
	}
}

// Submit creates a local approval request and triggers desktop prompt asynchronously.
func (c *LocalDesktopClient) Submit(ctx context.Context, req SubmitRequest) (string, error) {
	extID := strings.TrimSpace(req.RequestID)
	if extID == "" {
		extID = "local-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}

	c.mu.Lock()
	c.decisions[extID] = localDecision{status: StatusPending}
	c.mu.Unlock()

	title := "MCP Approval Required"
	message := buildLocalPromptMessage(req)

	log.Printf("[approval][local_desktop] submit request_id=%s tool=%s table=%s", extID, req.Tool, req.TableName)
	go c.askWithTimeout(extID, title, message)
	return extID, nil
}

// GetStatus returns current local approval status.
func (c *LocalDesktopClient) GetStatus(ctx context.Context, externalID string) (StatusResult, error) {
	c.mu.Lock()
	decision, ok := c.decisions[externalID]
	if !ok {
		c.decisions[externalID] = localDecision{status: StatusPending}
		c.mu.Unlock()

		title := "MCP Approval Required"
		message := "Request ID: " + strings.TrimSpace(externalID) + "\nApprove this request?"
		log.Printf("[approval][local_desktop] status-miss request_id=%s, re-prompt", externalID)
		go c.askWithTimeout(externalID, title, message)
		return StatusResult{Status: StatusPending}, nil
	}
	c.mu.Unlock()
	return StatusResult{
		Status:      decision.status,
		Reason:      decision.reason,
		BypassScope: decision.bypassScope,
		BypassTTL:   decision.bypassTTL,
	}, nil
}

func (c *LocalDesktopClient) askWithTimeout(externalID, title, message string) {
	if err := c.notifier(title, message); err != nil {
		log.Printf("[approval][local_desktop] notifier error request_id=%s err=%v", externalID, err)
	}

	type result struct {
		approved bool
		reason   string
		err      error
	}
	ch := make(chan result, 1)
	go func() {
		ok, reason, err := c.asker(title, message)
		ch <- result{approved: ok, reason: strings.TrimSpace(reason), err: err}
	}()

	timer := time.NewTimer(c.timeout)
	defer timer.Stop()

	select {
	case r := <-ch:
		if r.err != nil {
			log.Printf("[approval][local_desktop] prompt error request_id=%s err=%v", externalID, r.err)
			c.setDecision(externalID, localDecision{
				status: StatusRejected,
				reason: "desktop approval error: " + r.err.Error(),
			})
			return
		}
		if r.approved {
			reason := r.reason
			if reason == "" {
				reason = "approved_by_local_desktop"
			}
			scope, ttl := c.askBypassPolicy(externalID)
			c.setDecision(externalID, localDecision{
				status:      StatusApproved,
				reason:      reason,
				bypassScope: scope,
				bypassTTL:   ttl,
			})
			return
		}
		reason := r.reason
		if reason == "" {
			reason = "rejected_by_local_desktop"
		}
		c.setDecision(externalID, localDecision{
			status: StatusRejected,
			reason: reason,
		})
	case <-timer.C:
		c.setDecision(externalID, localDecision{
			status: StatusRejected,
			reason: "approval timeout",
		})
	}
}

func (c *LocalDesktopClient) askBypassPolicy(externalID string) (BypassScope, time.Duration) {
	title := "MCP Approval Policy"
	msg := "Apply auto-approve policy for similar requests?"
	type result struct {
		scope BypassScope
		ttl   time.Duration
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		scope, ttl, err := c.bypassAsker(title, msg, c.defaultBypassTTL)
		ch <- result{scope: scope, ttl: ttl, err: err}
	}()

	wait := c.timeout
	if wait <= 0 || wait > 30*time.Second {
		wait = 30 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	var scope BypassScope
	var ttl time.Duration
	select {
	case r := <-ch:
		if r.err != nil {
			log.Printf("[approval][local_desktop] bypass policy prompt error request_id=%s err=%v", externalID, r.err)
			return BypassScopeOneTime, 0
		}
		scope = r.scope
		ttl = r.ttl
	case <-timer.C:
		log.Printf("[approval][local_desktop] bypass policy prompt timeout request_id=%s", externalID)
		return BypassScopeOneTime, 0
	}

	switch scope {
	case BypassScopeExact, BypassScopeTable, BypassScopeToolAndTbl:
		if ttl > 0 {
			return scope, ttl
		}
	case BypassScopeOneTime, "":
		return BypassScopeOneTime, 0
	}
	return BypassScopeOneTime, 0
}

func (c *LocalDesktopClient) setDecision(externalID string, decision localDecision) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decisions[externalID] = decision
	log.Printf(
		"[approval][local_desktop] decision request_id=%s status=%s reason=%s bypass_scope=%s bypass_ttl=%s",
		externalID,
		decision.status,
		decision.reason,
		decision.bypassScope,
		decision.bypassTTL,
	)
}

func buildLocalPromptMessage(req SubmitRequest) string {
	var b strings.Builder
	b.WriteString("Request ID: ")
	b.WriteString(strings.TrimSpace(req.RequestID))
	if req.Tool != "" || req.TableName != "" {
		b.WriteString("\nTool: ")
		b.WriteString(strings.TrimSpace(req.Tool))
		b.WriteString("\nTable: ")
		b.WriteString(strings.TrimSpace(req.TableName))
	}
	if strings.TrimSpace(req.Summary) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(req.Summary))
	}
	b.WriteString("\nApprove this request?")
	return b.String()
}

func defaultDesktopNotifier(title, message string) error {
	switch runtime.GOOS {
	case "darwin", "linux":
		return zenity.Notify(
			message,
			zenity.Title(title),
		)
	default:
		return nil
	}
}

func defaultDesktopAsker(title, message string) (bool, string, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		err := zenity.Question(
			message,
			zenity.Title(title),
			zenity.OKLabel("Approve"),
			zenity.CancelLabel("Reject"),
		)
		switch {
		case err == nil:
			return true, "Approve", nil
		case errors.Is(err, zenity.ErrCanceled):
			return false, "Reject", nil
		default:
			return false, "", fmt.Errorf("zenity question failed: %w", err)
		}
	default:
		return false, "local desktop prompt unsupported on this OS", nil
	}
}

func defaultDesktopBypassAsker(title, message string, defaultTTL time.Duration) (BypassScope, time.Duration, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		ttlLabel := defaultTTL.String()
		options := []string{
			"Only this request",
			"Same table auto-approve (" + ttlLabel + ")",
			"Same tool+table auto-approve (" + ttlLabel + ")",
			"Exact same request auto-approve (" + ttlLabel + ")",
		}
		selection, err := zenity.List(
			message,
			options,
			zenity.Title(title),
			zenity.OKLabel("Apply"),
			zenity.CancelLabel("Only once"),
		)
		switch {
		case err == nil:
			switch selection {
			case options[1]:
				return BypassScopeTable, defaultTTL, nil
			case options[2]:
				return BypassScopeToolAndTbl, defaultTTL, nil
			case options[3]:
				return BypassScopeExact, defaultTTL, nil
			default:
				return BypassScopeOneTime, 0, nil
			}
		case errors.Is(err, zenity.ErrCanceled):
			return BypassScopeOneTime, 0, nil
		default:
			return BypassScopeOneTime, 0, fmt.Errorf("zenity bypass list failed: %w", err)
		}
	default:
		return BypassScopeOneTime, 0, nil
	}
}

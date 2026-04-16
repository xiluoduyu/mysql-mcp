package approval

import (
	"context"
	"testing"
	"time"
)

type fakeClient struct {
	submitID string
	status   Status
	reason   string
	scope    BypassScope
	ttl      time.Duration
}

func (f fakeClient) Submit(ctx context.Context, req SubmitRequest) (string, error) {
	return f.submitID, nil
}

func (f fakeClient) GetStatus(ctx context.Context, externalID string) (StatusResult, error) {
	return StatusResult{
		Status:      f.status,
		Reason:      f.reason,
		BypassScope: f.scope,
		BypassTTL:   f.ttl,
	}, nil
}

type switchableClient struct {
	submitID string
	status   Status
	reason   string
	scope    BypassScope
	ttl      time.Duration
}

func (s *switchableClient) Submit(ctx context.Context, req SubmitRequest) (string, error) {
	return s.submitID, nil
}

func (s *switchableClient) GetStatus(ctx context.Context, externalID string) (StatusResult, error) {
	return StatusResult{
		Status:      s.status,
		Reason:      s.reason,
		BypassScope: s.scope,
		BypassTTL:   s.ttl,
	}, nil
}

func TestGateDefaultPending(t *testing.T) {
	st, _ := NewSQLiteStore(":memory:")
	t.Cleanup(func() { _ = st.Close() })
	g := NewGate(st, fakeClient{submitID: "ext-1"}, 10*time.Minute)

	res, err := g.Authorize(context.Background(), GateInput{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "users",
		Payload:   `{"table":"users"}`,
	})
	if err != nil {
		t.Fatalf("Authorize err=%v", err)
	}
	if res.Decision != DecisionPending || res.ApprovalID == "" {
		t.Fatalf("res=%+v", res)
	}
}

func TestGateApprovedOneTime(t *testing.T) {
	st, _ := NewSQLiteStore(":memory:")
	t.Cleanup(func() { _ = st.Close() })
	g := NewGate(st, fakeClient{submitID: "ext-1", status: StatusApproved, reason: "ok"}, 10*time.Minute)

	_, _ = g.Authorize(context.Background(), GateInput{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "users",
		Payload:   `{"table":"users"}`,
	})

	if err := g.RefreshStatus(context.Background(), "req-1"); err != nil {
		t.Fatalf("RefreshStatus err=%v", err)
	}

	res, err := g.Authorize(context.Background(), GateInput{
		RequestID: "req-2",
		Tool:      "query_table",
		TableName: "users",
		Payload:   `{"table":"users"}`,
	})
	if err != nil {
		t.Fatalf("Authorize2 err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("decision=%s", res.Decision)
	}
}

func TestGateRefreshApprovedWithBypassPolicy(t *testing.T) {
	st, _ := NewSQLiteStore(":memory:")
	t.Cleanup(func() { _ = st.Close() })
	g := NewGate(st, fakeClient{
		submitID: "ext-1",
		status:   StatusApproved,
		reason:   "ok",
		scope:    BypassScopeTable,
		ttl:      30 * time.Minute,
	}, 10*time.Minute)

	_, _ = g.Authorize(context.Background(), GateInput{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "users",
		Payload:   `{"table":"users","filters":{"id":1}}`,
	})

	if err := g.RefreshStatus(context.Background(), "req-1"); err != nil {
		t.Fatalf("RefreshStatus err=%v", err)
	}

	res, err := g.Authorize(context.Background(), GateInput{
		RequestID: "req-2",
		Tool:      "query_table",
		TableName: "users",
		Payload:   `{"table":"users","filters":{"id":2}}`,
	})
	if err != nil {
		t.Fatalf("Authorize err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("decision=%s", res.Decision)
	}
	if res.Reason != "bypass_rule" {
		t.Fatalf("reason=%q", res.Reason)
	}
}

func TestGateBypassRule(t *testing.T) {
	st, _ := NewSQLiteStore(":memory:")
	t.Cleanup(func() { _ = st.Close() })
	_ = st.AddBypassRule(context.Background(), BypassRule{
		Scope:       BypassScopeTable,
		TableName:   "users",
		Enabled:     true,
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	g := NewGate(st, fakeClient{submitID: "ext-1"}, 10*time.Minute)
	res, err := g.Authorize(context.Background(), GateInput{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "users",
		Payload:   `{"table":"users"}`,
	})
	if err != nil {
		t.Fatalf("Authorize err=%v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("decision=%s", res.Decision)
	}
}

func TestGateRefreshPendingOnRepeatedAuthorize(t *testing.T) {
	st, _ := NewSQLiteStore(":memory:")
	t.Cleanup(func() { _ = st.Close() })
	cli := &switchableClient{
		submitID: "ext-1",
		status:   StatusPending,
	}
	g := NewGate(st, cli, 10*time.Minute)

	res1, err := g.Authorize(context.Background(), GateInput{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "users",
		Payload:   `{"table":"users"}`,
	})
	if err != nil {
		t.Fatalf("Authorize 1 err=%v", err)
	}
	if res1.Decision != DecisionPending {
		t.Fatalf("decision1=%s", res1.Decision)
	}

	cli.status = StatusApproved
	cli.reason = "desktop approved"
	res2, err := g.Authorize(context.Background(), GateInput{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "users",
		Payload:   `{"table":"users"}`,
	})
	if err != nil {
		t.Fatalf("Authorize 2 err=%v", err)
	}
	if res2.Decision != DecisionAllow {
		t.Fatalf("decision2=%s", res2.Decision)
	}
}

package approval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	st, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore err=%v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	r := Request{
		ID:          "req-1",
		Fingerprint: "fp-1",
		Tool:        "query_table",
		TableName:   "users",
		PayloadJSON: `{"x":1}`,
		Status:      StatusPending,
	}
	if err := st.CreateRequest(ctx, r); err != nil {
		t.Fatalf("CreateRequest err=%v", err)
	}

	got, err := st.GetRequest(ctx, "req-1")
	if err != nil {
		t.Fatalf("GetRequest err=%v", err)
	}
	if got == nil || got.Status != StatusPending {
		t.Fatalf("request=%+v", got)
	}

	if err := st.UpdateRequestStatus(ctx, "req-1", StatusApproved, "ok"); err != nil {
		t.Fatalf("UpdateRequestStatus err=%v", err)
	}
	got, _ = st.GetRequest(ctx, "req-1")
	if got.Status != StatusApproved {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestStoreBypassAndConsume(t *testing.T) {
	ctx := context.Background()
	st, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore err=%v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rule := BypassRule{
		Scope:       BypassScopeExact,
		Fingerprint: "fp-1",
		Tool:        "query_table",
		TableName:   "users",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		Enabled:     true,
	}
	if err := st.AddBypassRule(ctx, rule); err != nil {
		t.Fatalf("AddBypassRule err=%v", err)
	}

	matched, err := st.MatchBypassRule(ctx, "fp-1", "query_table", "users")
	if err != nil {
		t.Fatalf("MatchBypassRule err=%v", err)
	}
	if !matched {
		t.Fatal("expected matched=true")
	}

	if err := st.CreateOneTimeApproval(ctx, "req-2", "fp-2", "query_table", "users"); err != nil {
		t.Fatalf("CreateOneTimeApproval err=%v", err)
	}
	ok, err := st.ConsumeOneTimeApproval(ctx, "fp-2", "query_table", "users")
	if err != nil {
		t.Fatalf("ConsumeOneTimeApproval err=%v", err)
	}
	if !ok {
		t.Fatal("expected first consume true")
	}
	ok, err = st.ConsumeOneTimeApproval(ctx, "fp-2", "query_table", "users")
	if err != nil {
		t.Fatalf("ConsumeOneTimeApproval #2 err=%v", err)
	}
	if ok {
		t.Fatal("expected second consume false")
	}
}

func TestNewSQLiteStoreCreatesParentDirectory(t *testing.T) {
	base := t.TempDir()
	dbPath := filepath.Join(base, "nested", "state.db")
	if _, err := os.Stat(filepath.Dir(dbPath)); !os.IsNotExist(err) {
		t.Fatalf("expected parent dir to not exist, err=%v", err)
	}

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore err=%v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db file to exist, err=%v", err)
	}
}

package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/xiluoduyu/mysql-mcp/internal/approval"
	"github.com/xiluoduyu/mysql-mcp/internal/mcpserver"
	"github.com/xiluoduyu/mysql-mcp/internal/mysqlquery"
)

type okApprovalClient struct{}

func (okApprovalClient) Submit(ctx context.Context, req approval.SubmitRequest) (string, error) {
	return "ext-1", nil
}

func (okApprovalClient) GetStatus(ctx context.Context, externalID string) (approval.StatusResult, error) {
	return approval.StatusResult{
		Status: approval.StatusApproved,
		Reason: "ok",
	}, nil
}

func TestApprovalThenBypassCallbackFlow(t *testing.T) {
	store, err := approval.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store err=%v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	gate := approval.NewGate(store, okApprovalClient{}, 10*time.Minute)

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite err=%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, _ = db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);`)
	_, _ = db.Exec(`INSERT INTO users(id,email) VALUES (1,'a@test.com'), (2,'b@test.com');`)

	querySvc := mysqlquery.NewWithSchema(db, 100, map[string][]mysqlquery.ColumnInfo{
		"users": {{Name: "id"}, {Name: "email"}},
	})

	srv := mcpserver.New(mcpserver.Config{
		BearerToken:       "token",
		CallbackSecret:    "secret",
		ApprovalBypassTTL: 30 * time.Minute,
	}, querySvc, gate, store)

	_, _ = gate.Authorize(context.Background(), approval.GateInput{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "default.users",
		Payload:   `{"table":"users","filters":{"id":1}}`,
	})

	payload := map[string]any{
		"request_id":   "req-1",
		"status":       "approved",
		"reason":       "ok",
		"bypass_scope": "table",
	}
	body, _ := json.Marshal(payload)
	tsValue := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte(tsValue))
	mac.Write([]byte("."))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/callbacks/approvals", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", tsValue)
	resp := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("callback status=%d body=%s", resp.Code, resp.Body.String())
	}

	res, err := gate.Authorize(context.Background(), approval.GateInput{
		RequestID: "req-2",
		Tool:      "query_table",
		TableName: "default.users",
		Payload:   `{"table":"users","filters":{"id":2}}`,
	})
	if err != nil {
		t.Fatalf("authorize err=%v", err)
	}
	if res.Decision != approval.DecisionAllow {
		t.Fatalf("decision=%s", res.Decision)
	}

	rows, err := querySvc.QueryRows(context.Background(), mysqlquery.QueryInput{
		Table:   "users",
		Filters: map[string]any{"id": 2},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("query rows err=%v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len=%d", len(rows))
	}
}

package mcpserver

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xiluoduyu/mysql-mcp/internal/approval"
	"github.com/xiluoduyu/mysql-mcp/internal/mysqlquery"
)

type stubApprovalClient struct{}

func (stubApprovalClient) Submit(ctx context.Context, req approval.SubmitRequest) (string, error) {
	return "ext-1", nil
}

func (stubApprovalClient) GetStatus(ctx context.Context, externalID string) (approval.StatusResult, error) {
	return approval.StatusResult{
		Status: approval.StatusApproved,
		Reason: "ok",
	}, nil
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := approval.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store err=%v", err)
	}
	g := approval.NewGate(store, stubApprovalClient{}, 10*time.Minute)
	q := mysqlquery.NewWithSources(
		map[string]*sql.DB{},
		200,
		map[string]map[string][]mysqlquery.ColumnInfo{
			mysqlquery.DefaultSource: {"users": {{Name: "id"}, {Name: "email"}}},
			"audit":                  {"events": {{Name: "id"}, {Name: "action"}}},
		},
	)

	cfg := Config{
		BearerToken:        "token",
		CallbackSecret:     "secret",
		ApprovalBypassTTL:  30 * time.Minute,
		ApprovalPollPeriod: 3 * time.Second,
	}
	return New(cfg, q, g, store)
}

func newTestServerNoDefaultSource(t *testing.T) *Server {
	t.Helper()
	store, err := approval.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store err=%v", err)
	}
	g := approval.NewGate(store, stubApprovalClient{}, 10*time.Minute)
	q := mysqlquery.NewWithSources(
		map[string]*sql.DB{},
		200,
		map[string]map[string][]mysqlquery.ColumnInfo{
			"audit": {"events": {{Name: "id"}, {Name: "action"}}},
		},
	)

	cfg := Config{
		BearerToken:        "token",
		CallbackSecret:     "secret",
		ApprovalBypassTTL:  30 * time.Minute,
		ApprovalPollPeriod: 3 * time.Second,
	}
	return New(cfg, q, g, store)
}

func TestRequireBearer(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", resp.Code)
	}
}

func TestCallbackSignatureAndUpdate(t *testing.T) {
	srv := newTestServer(t)
	h := srv.Handler()

	_, _ = srv.gate.Authorize(context.Background(), approval.GateInput{
		RequestID: "req-1",
		Tool:      "query_table",
		TableName: "default.users",
		Payload:   `{"table":"users"}`,
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
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	res, err := srv.gate.Authorize(context.Background(), approval.GateInput{
		RequestID: "req-2",
		Tool:      "query_table",
		TableName: "default.users",
		Payload:   `{"table":"users"}`,
	})
	if err != nil {
		t.Fatalf("authorize err=%v", err)
	}
	if res.Decision != approval.DecisionAllow {
		t.Fatalf("decision=%s", res.Decision)
	}
}

func TestQueryTableAutoGeneratesRequestID(t *testing.T) {
	srv := newTestServer(t)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"table": "users",
			},
		},
	}

	res, err := srv.handleQueryTable(context.Background(), req)
	if err != nil {
		t.Fatalf("handleQueryTable err=%v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res)
	}

	body, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type=%T", res.StructuredContent)
	}
	requestID, _ := body["request_id"].(string)
	if requestID == "" {
		t.Fatalf("expected generated request_id in response, body=%v", body)
	}
	approvalObj, ok := body["approval"].(map[string]any)
	if !ok {
		t.Fatalf("approval field missing, body=%v", body)
	}
	if fmt.Sprint(approvalObj["decision"]) != string(approval.DecisionPending) {
		t.Fatalf("decision=%v", approvalObj["decision"])
	}
}

func TestQueryTableRejectsRequestIDInFilters(t *testing.T) {
	srv := newTestServer(t)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"table": "users",
				"filters": map[string]any{
					"request_id": "req-from-filters",
				},
			},
		},
	}

	res, err := srv.handleQueryTable(context.Background(), req)
	if err != nil {
		t.Fatalf("handleQueryTable err=%v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error, got: %+v", res)
	}
	if !strings.Contains(fmt.Sprint(res.Content), "request_id must be top-level") {
		t.Fatalf("unexpected error content: %+v", res.Content)
	}
}

func TestQueryTableRejectsTypoRequestIDInFilters(t *testing.T) {
	srv := newTestServer(t)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"table": "users",
				"filters": map[string]any{
					"reuqest_id": "req-from-filters-typo",
				},
			},
		},
	}

	res, err := srv.handleQueryTable(context.Background(), req)
	if err != nil {
		t.Fatalf("handleQueryTable err=%v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error, got: %+v", res)
	}
	if !strings.Contains(fmt.Sprint(res.Content), "request_id must be top-level") {
		t.Fatalf("unexpected error content: %+v", res.Content)
	}
}

func TestQueryTableToolSchemaStrict(t *testing.T) {
	srv := newTestServer(t)

	raw := srv.mcp.HandleMessage(context.Background(), []byte(`{
	  "jsonrpc":"2.0",
	  "id":1,
	  "method":"tools/list",
	  "params":{}
	}`))
	resp, ok := raw.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("response type=%T", raw)
	}
	var list mcp.ListToolsResult
	switch v := resp.Result.(type) {
	case mcp.ListToolsResult:
		list = v
	default:
		resultMap, ok := resp.Result.(map[string]any)
		if !ok {
			t.Fatalf("result type=%T", resp.Result)
		}
		toolsAny, ok := resultMap["tools"].([]any)
		if !ok {
			t.Fatalf("tools type=%T", resultMap["tools"])
		}
		rawTools, _ := json.Marshal(map[string]any{"tools": toolsAny})
		if err := json.Unmarshal(rawTools, &list); err != nil {
			t.Fatalf("unmarshal tools err=%v", err)
		}
	}

	var queryTool *mcp.Tool
	for i := range list.Tools {
		if list.Tools[i].Name == "query_table" {
			queryTool = &list.Tools[i]
			break
		}
	}
	if queryTool == nil {
		t.Fatal("query_table tool not found")
	}
	if len(queryTool.RawInputSchema) == 0 {
		t.Fatal("expected raw input schema for query_table")
	}

	var schema map[string]any
	if err := json.Unmarshal(queryTool.RawInputSchema, &schema); err != nil {
		t.Fatalf("unmarshal raw schema err=%v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("schema type=%v", schema["type"])
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties=%v", schema["additionalProperties"])
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["source"]; !ok {
		t.Fatal("source property missing")
	}
	if _, ok := props["request_id"]; !ok {
		t.Fatal("request_id property missing")
	}
	filtersSchema, _ := props["filters"].(map[string]any)
	notSchema, _ := filtersSchema["not"].(map[string]any)
	if notSchema == nil {
		t.Fatal("filters.not schema missing")
	}
}

func TestListTablesWithSource(t *testing.T) {
	srv := newTestServer(t)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"source": "audit",
			},
		},
	}
	res, err := srv.handleListTables(context.Background(), req)
	if err != nil {
		t.Fatalf("handleListTables err=%v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type=%T", res.StructuredContent)
	}
	if fmt.Sprint(body["source"]) != "audit" {
		t.Fatalf("source=%v", body["source"])
	}
	tablesAny, ok := body["tables"].([]any)
	if !ok {
		if tables2, ok2 := body["tables"].([]string); ok2 {
			if len(tables2) == 0 {
				t.Fatalf("tables=%v", body["tables"])
			}
			return
		}
		t.Fatalf("tables=%v", body["tables"])
	}
	if len(tablesAny) == 0 {
		t.Fatalf("tables=%v", body["tables"])
	}
}

func TestQueryTableRequiresSourceWhenNoDefaultSource(t *testing.T) {
	srv := newTestServerNoDefaultSource(t)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"table": "events",
			},
		},
	}

	res, err := srv.handleQueryTable(context.Background(), req)
	if err != nil {
		t.Fatalf("handleQueryTable err=%v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error, got: %+v", res)
	}
	if !strings.Contains(fmt.Sprint(res.Content), "source is required") {
		t.Fatalf("unexpected error content: %+v", res.Content)
	}
}

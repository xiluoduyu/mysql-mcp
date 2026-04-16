package mcpserver

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/xiluoduyu/mysql-mcp/internal/approval"
	"github.com/xiluoduyu/mysql-mcp/internal/mysqlquery"
)

// Config configures MCP HTTP server and callback behavior.
type Config struct {
	BearerToken        string
	CallbackSecret     string
	ApprovalBypassTTL  time.Duration
	ApprovalPollPeriod time.Duration
}

// Server wires MCP tools, approval gate, and callback endpoints.
type Server struct {
	cfg   Config
	query *mysqlquery.Service
	gate  *approval.Gate
	store approval.Store
	mcp   *server.MCPServer
	http  *server.StreamableHTTPServer
}

// New creates a server instance.
func New(cfg Config, query *mysqlquery.Service, gate *approval.Gate, store approval.Store) *Server {
	mcpServer := server.NewMCPServer(
		"mysql-approval-mcp",
		"0.1.0",
		server.WithRecovery(),
	)
	s := &Server{
		cfg:   cfg,
		query: query,
		gate:  gate,
		store: store,
		mcp:   mcpServer,
		http:  server.NewStreamableHTTPServer(mcpServer, server.WithEndpointPath("/mcp")),
	}
	s.registerTools()
	return s
}

// Handler returns the http handler with auth and callback routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	mux.Handle("/callbacks/approvals", http.HandlerFunc(s.handleApprovalCallback))
	mux.Handle("/mcp", s.requireBearer(s.http))
	return mux
}

// Start starts HTTP server.
func (s *Server) Start(ctx context.Context, addr string) error {
	httpSrv := &http.Server{Addr: addr, Handler: s.Handler()}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) registerTools() {
	listTablesTool := mcp.NewTool(
		"list_tables",
		mcp.WithDescription("List readable MySQL tables."),
		mcp.WithString("source", mcp.Description("Optional MySQL source name, default is 'default' when configured.")),
	)
	s.mcp.AddTool(listTablesTool, s.handleListTables)

	describeTableTool := mcp.NewTool(
		"describe_table",
		mcp.WithDescription("Describe columns of one table."),
		mcp.WithString("source", mcp.Description("Optional MySQL source name, default is 'default' when configured.")),
		mcp.WithString("table", mcp.Required()),
	)
	s.mcp.AddTool(describeTableTool, s.handleDescribeTable)

	queryTableTool := mcp.NewToolWithRawSchema(
		"query_table",
		"Query one table with optional filters/order/limit/offset.",
		json.RawMessage(`{
			"type": "object",
			"additionalProperties": false,
			"properties": {
				"table": {
					"type": "string",
					"description": "Target table name"
				},
				"source": {
					"type": "string",
					"description": "MySQL source name. Optional when default source is configured."
				},
				"filters": {
					"type": "object",
					"description": "Equality filters by column name",
					"additionalProperties": true,
					"not": {
						"anyOf": [
							{"required": ["request_id"]},
							{"required": ["reuqest_id"]}
						]
					}
				},
				"order_by": {
					"type": "string"
				},
				"order": {
					"type": "string",
					"enum": ["asc", "desc"]
				},
				"limit": {
					"type": "number",
					"minimum": 1
				},
				"offset": {
					"type": "number",
					"minimum": 0
				},
				"request_id": {
					"type": "string",
					"description": "Approval request id; top-level only"
				}
			},
			"required": ["table"]
		}`),
	)
	s.mcp.AddTool(queryTableTool, s.handleQueryTable)
}

func (s *Server) handleListTables(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx
	source := strings.TrimSpace(req.GetString("source", ""))
	tables, err := s.query.ListTablesBySource(source)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	sourceName := source
	if sourceName == "" {
		sourceName = mysqlquery.DefaultSource
	}
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"source": sourceName,
		"tables": tables,
	}), nil
}

func (s *Server) handleDescribeTable(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx
	source := strings.TrimSpace(req.GetString("source", ""))
	table, err := req.RequireString("table")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cols, err := s.query.DescribeTableBySource(source, table)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	sourceName := source
	if sourceName == "" {
		sourceName = mysqlquery.DefaultSource
	}
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"source":  sourceName,
		"table":   table,
		"columns": cols,
	}), nil
}

func (s *Server) handleQueryTable(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	source := strings.TrimSpace(req.GetString("source", ""))
	table, err := req.RequireString("table")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	filters := map[string]any{}
	if raw := req.GetArguments()["filters"]; raw != nil {
		if m, ok := raw.(map[string]any); ok {
			filters = m
		} else {
			return mcp.NewToolResultError("filters must be object"), nil
		}
	}
	if _, ok := filters["request_id"]; ok {
		return mcp.NewToolResultError("request_id must be top-level argument, not inside filters"), nil
	}
	if _, ok := filters["reuqest_id"]; ok {
		return mcp.NewToolResultError("request_id must be top-level argument, not inside filters"), nil
	}

	requestID := strings.TrimSpace(req.GetString("request_id", ""))
	if requestID == "" {
		requestID = generateRequestID()
	}

	input := mysqlquery.QueryInput{
		Source:  source,
		Table:   table,
		Filters: filters,
		OrderBy: req.GetString("order_by", ""),
		Order:   req.GetString("order", ""),
		Limit:   int(req.GetFloat("limit", 50)),
		Offset:  int(req.GetFloat("offset", 0)),
	}
	if _, _, err := s.query.BuildQuery(input); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	payloadBytes, _ := json.Marshal(input)

	decision, err := s.gate.Authorize(ctx, approval.GateInput{
		RequestID: requestID,
		Tool:      "query_table",
		TableName: composeScopedTable(source, table),
		Payload:   string(payloadBytes),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if decision.Decision != approval.DecisionAllow {
		return mcp.NewToolResultStructuredOnly(map[string]any{
			"request_id": requestID,
			"approval": map[string]any{
				"decision":    decision.Decision,
				"approval_id": decision.ApprovalID,
				"reason":      decision.Reason,
			},
			"message": "request requires external approval",
		}), nil
	}

	rows, err := s.query.QueryRows(ctx, input)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"request_id": requestID,
		"source":     normalizedSource(source),
		"table":      table,
		"rows":       rows,
		"count":      len(rows),
	}), nil
}

func composeScopedTable(source, table string) string {
	return normalizedSource(source) + "." + strings.TrimSpace(table)
}

func normalizedSource(source string) string {
	v := strings.ToLower(strings.TrimSpace(source))
	if v == "" {
		return mysqlquery.DefaultSource
	}
	return v
}

func generateRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b[:])
}

func (s *Server) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "" || token != s.cfg.BearerToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleApprovalCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body error", http.StatusBadRequest)
		return
	}
	ts := r.Header.Get("X-Timestamp")
	sig := r.Header.Get("X-Signature")
	if !approval.VerifyCallbackSignature(s.cfg.CallbackSecret, ts, sig, body, 5*time.Minute) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var payload struct {
		RequestID   string               `json:"request_id"`
		Status      approval.Status      `json:"status"`
		Reason      string               `json:"reason"`
		BypassScope approval.BypassScope `json:"bypass_scope"`
		BypassTTL   string               `json:"bypass_ttl"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if payload.RequestID == "" {
		http.Error(w, "request_id required", http.StatusBadRequest)
		return
	}

	bypassTTL := s.cfg.ApprovalBypassTTL
	if strings.TrimSpace(payload.BypassTTL) != "" {
		d, err := time.ParseDuration(payload.BypassTTL)
		if err != nil {
			http.Error(w, "invalid bypass_ttl", http.StatusBadRequest)
			return
		}
		bypassTTL = d
	}

	if err := s.gate.ApplyCallbackResult(r.Context(), payload.RequestID, payload.Status, payload.Reason, payload.BypassScope, bypassTTL); err != nil {
		http.Error(w, fmt.Sprintf("apply callback result error: %v", err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// NewMySQLService creates mysql query service with introspected schema.
func NewMySQLService(ctx context.Context, dbs map[string]*sql.DB, maxLimit int, opts ...mysqlquery.Option) (*mysqlquery.Service, error) {
	return mysqlquery.NewFromSources(ctx, maxLimit, dbs, opts...)
}

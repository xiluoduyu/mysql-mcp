package approval

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists approval requests and bypass policies.
type Store interface {
	CreateRequest(ctx context.Context, req Request) error
	GetRequest(ctx context.Context, id string) (*Request, error)
	UpdateRequestStatus(ctx context.Context, id string, status Status, reason string) error
	AddBypassRule(ctx context.Context, rule BypassRule) error
	MatchBypassRule(ctx context.Context, fingerprint, tool, table string) (bool, error)
	CreateOneTimeApproval(ctx context.Context, id, fingerprint, tool, table string) error
	ConsumeOneTimeApproval(ctx context.Context, fingerprint, tool, table string) (bool, error)
	Close() error
}

// SQLiteStore is a SQLite implementation of Store.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite store and initializes schema.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if err := ensureSQLitePathReady(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite error: %w", err)
	}
	st := &SQLiteStore{db: db}
	if err := st.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func ensureSQLitePathReady(path string) error {
	p := strings.TrimSpace(path)
	if p == "" || p == ":memory:" || strings.HasPrefix(p, "file:") {
		return nil
	}
	dir := filepath.Dir(p)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite dir error: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) initSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS approval_requests (
			id TEXT PRIMARY KEY,
			fingerprint TEXT NOT NULL,
			tool TEXT NOT NULL,
			table_name TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			status TEXT NOT NULL,
			external_approval_id TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_approval_fp ON approval_requests(fingerprint);`,
		`CREATE TABLE IF NOT EXISTS bypass_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scope TEXT NOT NULL,
			fingerprint TEXT NOT NULL DEFAULT '',
			tool TEXT NOT NULL DEFAULT '',
			table_name TEXT NOT NULL DEFAULT '',
			expires_at DATETIME NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS one_time_approvals (
			id TEXT PRIMARY KEY,
			fingerprint TEXT NOT NULL,
			tool TEXT NOT NULL,
			table_name TEXT NOT NULL,
			consumed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_one_time_match ON one_time_approvals(fingerprint, tool, table_name, consumed);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init schema error: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) CreateRequest(ctx context.Context, req Request) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO approval_requests(id,fingerprint,tool,table_name,payload_json,status,external_approval_id,reason)
		 VALUES(?,?,?,?,?,?,?,?)`,
		req.ID,
		req.Fingerprint,
		req.Tool,
		req.TableName,
		req.PayloadJSON,
		req.Status,
		req.ExternalApprovalID,
		req.Reason,
	)
	if err != nil {
		return fmt.Errorf("create request error: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRequest(ctx context.Context, id string) (*Request, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id,fingerprint,tool,table_name,payload_json,status,external_approval_id,reason,created_at,updated_at
		 FROM approval_requests WHERE id=?`,
		id,
	)
	var req Request
	if err := row.Scan(
		&req.ID,
		&req.Fingerprint,
		&req.Tool,
		&req.TableName,
		&req.PayloadJSON,
		&req.Status,
		&req.ExternalApprovalID,
		&req.Reason,
		&req.CreatedAt,
		&req.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get request error: %w", err)
	}
	return &req, nil
}

func (s *SQLiteStore) UpdateRequestStatus(ctx context.Context, id string, status Status, reason string) error {
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE approval_requests
		 SET status=?, reason=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		status,
		reason,
		id,
	)
	if err != nil {
		return fmt.Errorf("update request status error: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected error: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("request not found: %s", id)
	}
	return nil
}

func (s *SQLiteStore) AddBypassRule(ctx context.Context, rule BypassRule) error {
	enabled := 0
	if rule.Enabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO bypass_rules(scope,fingerprint,tool,table_name,expires_at,enabled)
		 VALUES(?,?,?,?,?,?)`,
		rule.Scope,
		rule.Fingerprint,
		rule.Tool,
		rule.TableName,
		rule.ExpiresAt,
		enabled,
	)
	if err != nil {
		return fmt.Errorf("add bypass rule error: %w", err)
	}
	return nil
}

func (s *SQLiteStore) MatchBypassRule(ctx context.Context, fingerprint, tool, table string) (bool, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT scope,fingerprint,tool,table_name
		 FROM bypass_rules
		 WHERE enabled=1 AND expires_at > CURRENT_TIMESTAMP`,
	)
	if err != nil {
		return false, fmt.Errorf("query bypass rules error: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var scope BypassScope
		var fp, t, tb string
		if err := rows.Scan(&scope, &fp, &t, &tb); err != nil {
			return false, fmt.Errorf("scan bypass rule error: %w", err)
		}
		if matchRule(scope, fp, t, tb, fingerprint, tool, table) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate bypass rules error: %w", err)
	}
	return false, nil
}

func matchRule(scope BypassScope, fp, tool, table, inputFP, inputTool, inputTable string) bool {
	switch scope {
	case BypassScopeExact:
		return fp == inputFP
	case BypassScopeTable:
		return table != "" && table == inputTable
	case BypassScopeToolAndTbl:
		return tool == inputTool && table == inputTable
	default:
		return false
	}
}

func (s *SQLiteStore) CreateOneTimeApproval(ctx context.Context, id, fingerprint, tool, table string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO one_time_approvals(id,fingerprint,tool,table_name,consumed)
		 VALUES(?,?,?,?,0)`,
		id,
		fingerprint,
		tool,
		table,
	)
	if err != nil {
		if isUniqConstraintErr(err) {
			return nil
		}
		return fmt.Errorf("create one-time approval error: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ConsumeOneTimeApproval(ctx context.Context, fingerprint, tool, table string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx error: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(
		ctx,
		`SELECT id FROM one_time_approvals
		 WHERE fingerprint=? AND tool=? AND table_name=? AND consumed=0
		 ORDER BY created_at ASC LIMIT 1`,
		fingerprint,
		tool,
		table,
	)
	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("scan one-time approval error: %w", err)
	}

	res, err := tx.ExecContext(ctx, `UPDATE one_time_approvals SET consumed=1 WHERE id=?`, id)
	if err != nil {
		return false, fmt.Errorf("update one-time approval error: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected error: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit tx error: %w", err)
	}
	return true, nil
}

// CleanupExpiredRules deletes expired bypass rules.
func (s *SQLiteStore) CleanupExpiredRules(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bypass_rules WHERE expires_at <= ?`, now)
	if err != nil {
		return fmt.Errorf("cleanup expired rules error: %w", err)
	}
	return nil
}

func isUniqConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}

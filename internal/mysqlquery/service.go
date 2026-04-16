package mysqlquery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const defaultMaskPlaceholder = "******"

var defaultMaskKeywords = []string{"password", "token", "secret", "key"}

const DefaultSource = "default"

// ColumnInfo contains table column metadata.
type ColumnInfo struct {
	Name string
}

// QueryInput is query_table tool input.
type QueryInput struct {
	Source  string
	Table   string
	Filters map[string]any
	OrderBy string
	Order   string
	Limit   int
	Offset  int
}

// Option mutates service behavior.
type Option func(*Service)

// WithMasking configures field masking policy.
// - keywords: case-insensitive substrings matched against column names.
// - fields: explicit field names ("password") or table-qualified fields ("users.password").
//
// When keywords is nil, service defaults are retained.
// When keywords is non-nil (including empty), service keywords are replaced.
func WithMasking(keywords, fields, jsonFields []string) Option {
	return func(s *Service) {
		if keywords != nil {
			s.maskKeywords = normalizeKeywords(keywords)
		}
		s.maskFields = normalizeFieldSet(fields)
		s.maskJSONFields = normalizeFieldSet(jsonFields)
	}
}

// Service validates and executes read-only table queries.
type Service struct {
	maxLimit int

	// source -> db connection
	dbBySource map[string]*sql.DB
	// source -> table -> columns
	schemaBySource map[string]map[string][]ColumnInfo

	// Lowercased keyword substrings matched against column names.
	maskKeywords []string
	// Lowercased explicit fields; supports "field" and "table.field".
	maskFields map[string]struct{}
	// Fields where JSON-typed value should be traversed and masked recursively.
	// Supports "field" and "table.field".
	maskJSONFields map[string]struct{}
}

// NewWithSchema constructs a single-source service with preloaded schema.
func NewWithSchema(db *sql.DB, maxLimit int, schema map[string][]ColumnInfo, opts ...Option) *Service {
	return NewWithSources(
		map[string]*sql.DB{DefaultSource: db},
		maxLimit,
		map[string]map[string][]ColumnInfo{DefaultSource: schema},
		opts...,
	)
}

// NewWithSources constructs a multi-source service with preloaded schemas.
func NewWithSources(dbs map[string]*sql.DB, maxLimit int, schemas map[string]map[string][]ColumnInfo, opts ...Option) *Service {
	normalizedSchemas := normalizeSchemas(schemas)
	if len(normalizedSchemas) == 0 {
		normalizedSchemas[DefaultSource] = map[string][]ColumnInfo{}
	}

	normalizedDBs := map[string]*sql.DB{}
	for source, db := range dbs {
		ns := normalizeToken(source)
		if ns == "" {
			continue
		}
		normalizedDBs[ns] = db
	}

	if maxLimit <= 0 {
		maxLimit = 200
	}
	svc := &Service{
		maxLimit:       maxLimit,
		dbBySource:     normalizedDBs,
		schemaBySource: normalizedSchemas,
		maskKeywords:   normalizeKeywords(defaultMaskKeywords),
		maskFields:     map[string]struct{}{},
		maskJSONFields: map[string]struct{}{},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// NewFromMySQL introspects table/column metadata from current schema as one default source.
func NewFromMySQL(ctx context.Context, db *sql.DB, maxLimit int, opts ...Option) (*Service, error) {
	return NewFromSources(ctx, maxLimit, map[string]*sql.DB{DefaultSource: db}, opts...)
}

// NewFromSources introspects table/column metadata for each source.
func NewFromSources(ctx context.Context, maxLimit int, dbs map[string]*sql.DB, opts ...Option) (*Service, error) {
	if len(dbs) == 0 {
		return nil, errors.New("no mysql sources configured")
	}

	normalizedDBs := map[string]*sql.DB{}
	schemas := map[string]map[string][]ColumnInfo{}
	for source, db := range dbs {
		ns := normalizeToken(source)
		if ns == "" {
			return nil, fmt.Errorf("invalid source name: %q", source)
		}
		if db == nil {
			return nil, fmt.Errorf("db is nil for source %s", ns)
		}

		schema, err := introspectSchema(ctx, db)
		if err != nil {
			return nil, fmt.Errorf("introspect source %s error: %w", ns, err)
		}
		if len(schema) == 0 {
			return nil, fmt.Errorf("no tables found in source %s current database", ns)
		}

		normalizedDBs[ns] = db
		schemas[ns] = schema
	}

	return NewWithSources(normalizedDBs, maxLimit, schemas, opts...), nil
}

func introspectSchema(ctx context.Context, db *sql.DB) (map[string][]ColumnInfo, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT TABLE_NAME, COLUMN_NAME
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		ORDER BY TABLE_NAME ASC, ORDINAL_POSITION ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query information_schema error: %w", err)
	}
	defer rows.Close()

	schema := map[string][]ColumnInfo{}
	for rows.Next() {
		var table, col string
		if err := rows.Scan(&table, &col); err != nil {
			return nil, fmt.Errorf("scan information_schema row error: %w", err)
		}
		schema[table] = append(schema[table], ColumnInfo{Name: col})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate information_schema error: %w", err)
	}
	return schema, nil
}

// Sources returns sorted source names.
func (s *Service) Sources() []string {
	sources := make([]string, 0, len(s.schemaBySource))
	for source := range s.schemaBySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

// ListTables returns sorted allowed table names in default source.
func (s *Service) ListTables() []string {
	tables, err := s.ListTablesBySource("")
	if err != nil {
		return nil
	}
	return tables
}

// ListTablesBySource returns sorted allowed table names for selected source.
func (s *Service) ListTablesBySource(source string) ([]string, error) {
	_, schema, err := s.resolveSource(source)
	if err != nil {
		return nil, err
	}
	tables := make([]string, 0, len(schema))
	for t := range schema {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	return tables, nil
}

// DescribeTable returns columns for one table in default source.
func (s *Service) DescribeTable(table string) ([]string, error) {
	return s.DescribeTableBySource("", table)
}

// DescribeTableBySource returns columns for one table in selected source.
func (s *Service) DescribeTableBySource(source, table string) ([]string, error) {
	resolvedSource, schema, err := s.resolveSource(source)
	if err != nil {
		return nil, err
	}
	cols, ok := schema[table]
	if !ok {
		return nil, fmt.Errorf("table not allowed in source %s: %s", resolvedSource, table)
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out, nil
}

// BuildQuery validates input and builds parameterized SQL.
func (s *Service) BuildQuery(input QueryInput) (string, []any, error) {
	resolvedSource, schema, err := s.resolveSource(input.Source)
	if err != nil {
		return "", nil, err
	}

	table := strings.TrimSpace(input.Table)
	if table == "" {
		return "", nil, fmt.Errorf("table is required")
	}
	cols, ok := schema[table]
	if !ok {
		return "", nil, fmt.Errorf("table not allowed in source %s: %s", resolvedSource, table)
	}

	columnSet := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		columnSet[c.Name] = struct{}{}
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > s.maxLimit {
		return "", nil, fmt.Errorf("limit exceeds max_limit %d", s.maxLimit)
	}
	if input.Offset < 0 {
		return "", nil, fmt.Errorf("offset must be >= 0")
	}

	whereKeys := make([]string, 0, len(input.Filters))
	for k := range input.Filters {
		whereKeys = append(whereKeys, k)
	}
	sort.Strings(whereKeys)

	parts := []string{fmt.Sprintf("SELECT * FROM `%s`", table)}
	args := make([]any, 0, len(whereKeys)+2)
	if len(whereKeys) > 0 {
		clauses := make([]string, 0, len(whereKeys))
		for _, k := range whereKeys {
			if _, ok := columnSet[k]; !ok {
				return "", nil, fmt.Errorf("filter column not allowed: %s", k)
			}
			clauses = append(clauses, fmt.Sprintf("`%s` = ?", k))
			args = append(args, input.Filters[k])
		}
		parts = append(parts, "WHERE "+strings.Join(clauses, " AND "))
	}

	if input.OrderBy != "" {
		if _, ok := columnSet[input.OrderBy]; !ok {
			return "", nil, fmt.Errorf("order_by column not allowed: %s", input.OrderBy)
		}
		orderDir := strings.ToUpper(strings.TrimSpace(input.Order))
		if orderDir == "" {
			orderDir = "ASC"
		}
		if orderDir != "ASC" && orderDir != "DESC" {
			return "", nil, fmt.Errorf("order must be ASC or DESC")
		}
		parts = append(parts, fmt.Sprintf("ORDER BY `%s` %s", input.OrderBy, orderDir))
	}

	parts = append(parts, "LIMIT ? OFFSET ?")
	args = append(args, limit, input.Offset)
	return strings.Join(parts, " "), args, nil
}

// QueryRows executes read query and returns rows as generic maps.
func (s *Service) QueryRows(ctx context.Context, input QueryInput) ([]map[string]any, error) {
	source, _, err := s.resolveSource(input.Source)
	if err != nil {
		return nil, err
	}

	db, ok := s.dbBySource[source]
	if !ok || db == nil {
		return nil, fmt.Errorf("db is nil for source %s", source)
	}

	query, args, err := s.BuildQuery(input)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query rows error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns error: %w", err)
	}
	out := make([]map[string]any, 0)
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan row error: %w", err)
		}
		item := make(map[string]any, len(cols))
		for i, c := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				item[c] = string(b)
			} else {
				item[c] = v
			}
		}
		s.maskRowValues(input.Table, item)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows error: %w", err)
	}
	return out, nil
}

func (s *Service) resolveSource(source string) (string, map[string][]ColumnInfo, error) {
	requested := normalizeToken(source)
	if requested == "" {
		if schema, ok := s.schemaBySource[DefaultSource]; ok {
			return DefaultSource, schema, nil
		}
		return "", nil, fmt.Errorf("source is required; available sources: %s", strings.Join(s.Sources(), ","))
	}
	schema, ok := s.schemaBySource[requested]
	if !ok {
		return "", nil, fmt.Errorf("source not allowed: %s", requested)
	}
	return requested, schema, nil
}

func (s *Service) maskRowValues(table string, row map[string]any) {
	for col, v := range row {
		if v == nil {
			continue
		}
		if s.shouldMaskField(table, col) {
			row[col] = defaultMaskPlaceholder
			continue
		}
		if !s.shouldTraverseJSONField(table, col) {
			continue
		}
		masked, ok := s.maskNestedJSONValue(table, v)
		if ok {
			row[col] = masked
		}
	}
}

func (s *Service) shouldMaskField(table, field string) bool {
	col := normalizeToken(field)
	if col == "" {
		return false
	}
	if _, ok := s.maskFields[col]; ok {
		return true
	}

	tbl := normalizeToken(table)
	if tbl != "" {
		if _, ok := s.maskFields[tbl+"."+col]; ok {
			return true
		}
	}

	for _, kw := range s.maskKeywords {
		if strings.Contains(col, kw) {
			return true
		}
	}
	return false
}

func (s *Service) shouldTraverseJSONField(table, field string) bool {
	if len(s.maskJSONFields) == 0 {
		return false
	}
	col := normalizeToken(field)
	if col == "" {
		return false
	}
	if _, ok := s.maskJSONFields[col]; ok {
		return true
	}
	tbl := normalizeToken(table)
	if tbl != "" {
		if _, ok := s.maskJSONFields[tbl+"."+col]; ok {
			return true
		}
	}
	return false
}

func (s *Service) maskNestedJSONValue(table string, v any) (any, bool) {
	switch x := v.(type) {
	case string:
		var root any
		if err := json.Unmarshal([]byte(x), &root); err != nil {
			return v, false
		}
		masked := s.maskAnyJSON(table, root)
		b, err := json.Marshal(masked)
		if err != nil {
			return v, false
		}
		return string(b), true
	case []byte:
		var root any
		if err := json.Unmarshal(x, &root); err != nil {
			return v, false
		}
		masked := s.maskAnyJSON(table, root)
		b, err := json.Marshal(masked)
		if err != nil {
			return v, false
		}
		return string(b), true
	case json.RawMessage:
		var root any
		if err := json.Unmarshal(x, &root); err != nil {
			return v, false
		}
		return s.maskAnyJSON(table, root), true
	case map[string]any, []any:
		return s.maskAnyJSON(table, x), true
	default:
		return v, false
	}
}

func (s *Service) maskAnyJSON(table string, v any) any {
	switch node := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))
		for k, vv := range node {
			if s.shouldMaskField(table, k) {
				out[k] = defaultMaskPlaceholder
				continue
			}
			out[k] = s.maskAnyJSON(table, vv)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, vv := range node {
			out[i] = s.maskAnyJSON(table, vv)
		}
		return out
	default:
		return v
	}
}

func normalizeSchemas(schemas map[string]map[string][]ColumnInfo) map[string]map[string][]ColumnInfo {
	out := make(map[string]map[string][]ColumnInfo, len(schemas))
	for source, schema := range schemas {
		ns := normalizeToken(source)
		if ns == "" {
			continue
		}
		tables := make(map[string][]ColumnInfo, len(schema))
		for t, cols := range schema {
			tables[t] = append([]ColumnInfo(nil), cols...)
		}
		out[ns] = tables
	}
	return out
}

func normalizeFieldSet(fields []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range fields {
		v := normalizeToken(f)
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}

func normalizeKeywords(keywords []string) []string {
	out := make([]string, 0, len(keywords))
	seen := map[string]struct{}{}
	for _, kw := range keywords {
		v := normalizeToken(kw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func normalizeToken(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

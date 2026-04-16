package mysqlquery

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBuildQuerySuccess(t *testing.T) {
	svc := NewWithSchema(nil, 200, map[string][]ColumnInfo{
		"users": {
			{Name: "id"},
			{Name: "email"},
			{Name: "nick_name"},
		},
	})

	q, args, err := svc.BuildQuery(QueryInput{
		Table:   "users",
		Filters: map[string]any{"id": 1, "email": "a@b.com"},
		OrderBy: "id",
		Order:   "desc",
		Limit:   20,
		Offset:  5,
	})
	if err != nil {
		t.Fatalf("BuildQuery err=%v", err)
	}
	expectSQL := "SELECT * FROM `users` WHERE `email` = ? AND `id` = ? ORDER BY `id` DESC LIMIT ? OFFSET ?"
	if q != expectSQL {
		t.Fatalf("sql=%q", q)
	}
	expectArgs := []any{"a@b.com", 1, 20, 5}
	if !reflect.DeepEqual(args, expectArgs) {
		t.Fatalf("args=%#v", args)
	}
}

func TestBuildQueryRejectsUnknownTable(t *testing.T) {
	svc := NewWithSchema(nil, 200, map[string][]ColumnInfo{"users": {{Name: "id"}}})
	_, _, err := svc.BuildQuery(QueryInput{Table: "orders"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildQueryRejectsUnknownColumn(t *testing.T) {
	svc := NewWithSchema(nil, 200, map[string][]ColumnInfo{"users": {{Name: "id"}}})
	_, _, err := svc.BuildQuery(QueryInput{Table: "users", Filters: map[string]any{"email": "x"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildQueryLimitValidation(t *testing.T) {
	svc := NewWithSchema(nil, 100, map[string][]ColumnInfo{"users": {{Name: "id"}}})
	_, _, err := svc.BuildQuery(QueryInput{Table: "users", Limit: 101})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListTablesSorted(t *testing.T) {
	svc := NewWithSchema(nil, 100, map[string][]ColumnInfo{
		"b": {{Name: "id"}},
		"a": {{Name: "id"}},
	})
	got := svc.ListTables()
	expect := []string{"a", "b"}
	if !reflect.DeepEqual(got, expect) {
		t.Fatalf("tables=%v", got)
	}
}

func TestQueryRowsDefaultMaskKeywords(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite err=%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, _ = db.Exec(`CREATE TABLE users (id INTEGER, password TEXT, access_token TEXT, api_key TEXT, nickname TEXT);`)
	_, _ = db.Exec(`INSERT INTO users(id,password,access_token,api_key,nickname) VALUES (1,'p1','t1','k1','nick');`)

	svc := NewWithSchema(db, 200, map[string][]ColumnInfo{
		"users": {
			{Name: "id"},
			{Name: "password"},
			{Name: "access_token"},
			{Name: "api_key"},
			{Name: "nickname"},
		},
	})
	rows, err := svc.QueryRows(context.Background(), QueryInput{Table: "users", Limit: 10})
	if err != nil {
		t.Fatalf("QueryRows err=%v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len=%d", len(rows))
	}
	row := rows[0]
	if row["password"] != "******" {
		t.Fatalf("password=%v", row["password"])
	}
	if row["access_token"] != "******" {
		t.Fatalf("access_token=%v", row["access_token"])
	}
	if row["api_key"] != "******" {
		t.Fatalf("api_key=%v", row["api_key"])
	}
	if row["nickname"] != "nick" {
		t.Fatalf("nickname=%v", row["nickname"])
	}
}

func TestQueryRowsCustomMaskFields(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite err=%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, _ = db.Exec(`CREATE TABLE users (id INTEGER, nickname TEXT, phone TEXT, login_token TEXT);`)
	_, _ = db.Exec(`INSERT INTO users(id,nickname,phone,login_token) VALUES (1,'nick','13800000000','t1');`)

	svc := NewWithSchema(
		db,
		200,
		map[string][]ColumnInfo{
			"users": {
				{Name: "id"},
				{Name: "nickname"},
				{Name: "phone"},
				{Name: "login_token"},
			},
		},
		WithMasking([]string{}, []string{"users.phone"}, nil),
	)

	rows, err := svc.QueryRows(context.Background(), QueryInput{Table: "users", Limit: 10})
	if err != nil {
		t.Fatalf("QueryRows err=%v", err)
	}
	row := rows[0]
	if row["phone"] != "******" {
		t.Fatalf("phone=%v", row["phone"])
	}
	// keywords disabled by empty keyword list
	if row["login_token"] != "t1" {
		t.Fatalf("login_token=%v", row["login_token"])
	}
}

func TestQueryRowsMaskNestedJSONStringField(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite err=%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, _ = db.Exec(`CREATE TABLE users (id INTEGER, profile TEXT);`)
	_, _ = db.Exec(`INSERT INTO users(id,profile) VALUES (1,'{"name":"n1","access_token":"t1","nested":{"password":"p1"}}');`)

	svc := NewWithSchema(
		db,
		200,
		map[string][]ColumnInfo{
			"users": {
				{Name: "id"},
				{Name: "profile"},
			},
		},
		WithMasking(nil, nil, []string{"users.profile"}),
	)

	rows, err := svc.QueryRows(context.Background(), QueryInput{Table: "users", Limit: 10})
	if err != nil {
		t.Fatalf("QueryRows err=%v", err)
	}
	profile, ok := rows[0]["profile"].(string)
	if !ok {
		t.Fatalf("profile type=%T", rows[0]["profile"])
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(profile), &obj); err != nil {
		t.Fatalf("unmarshal profile err=%v", err)
	}
	if obj["access_token"] != "******" {
		t.Fatalf("access_token=%v", obj["access_token"])
	}
	nested, _ := obj["nested"].(map[string]any)
	if nested["password"] != "******" {
		t.Fatalf("nested.password=%v", nested["password"])
	}
}

func TestQueryRowsMaskNestedJSONTypedField(t *testing.T) {
	svc := NewWithSchema(
		nil,
		200,
		map[string][]ColumnInfo{"users": {{Name: "meta"}}},
		WithMasking(nil, nil, []string{"users.meta"}),
	)
	in := map[string]any{
		"meta": map[string]any{
			"refresh_token": "abc",
			"child": map[string]any{
				"secret_value": "s1",
			},
			"ok": "x",
		},
	}
	svc.maskRowValues("users", in)
	meta, _ := in["meta"].(map[string]any)
	if meta["refresh_token"] != "******" {
		t.Fatalf("refresh_token=%v", meta["refresh_token"])
	}
	child, _ := meta["child"].(map[string]any)
	if child["secret_value"] != "******" {
		t.Fatalf("secret_value=%v", child["secret_value"])
	}
	if meta["ok"] != "x" {
		t.Fatalf("ok=%v", meta["ok"])
	}
}

package config

import "testing"

func TestLoadFromEnvSuccess(t *testing.T) {
	env := map[string]string{
		EnvMCPBindAddr:                "127.0.0.1:9090",
		EnvMCPBearerToken:             "abc",
		EnvMySQLDSNs:                  "core=user:pwd@tcp(localhost:3306)/db_core;audit=user:pwd@tcp(localhost:3306)/db_audit",
		EnvApprovalClientMode:         "http",
		EnvApprovalBaseURL:            "http://localhost:8088",
		EnvApprovalSubmitPath:         "/api/submit",
		EnvApprovalStatusPathTemplate: "/api/status/{id}",
		EnvApprovalCallbackSecret:     "secret",
		EnvApprovalPollInterval:       "3s",
		EnvApprovalTimeout:            "30m",
		EnvStateSQLitePath:            ":memory:",
		EnvMaxLimit:                   "200",
	}

	cfg, err := LoadFromEnv(getterFromMap(env))
	if err != nil {
		t.Fatalf("LoadFromEnv error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:9090" {
		t.Fatalf("BindAddr = %q", cfg.BindAddr)
	}
	if cfg.MaxLimit != 200 {
		t.Fatalf("MaxLimit = %d", cfg.MaxLimit)
	}
	if got, want := len(cfg.MySQLDSNs), 2; got != want {
		t.Fatalf("MySQLDSNs len=%d", got)
	}
	if len(cfg.MaskFieldKeywords) != 0 {
		t.Fatalf("MaskFieldKeywords=%v", cfg.MaskFieldKeywords)
	}
	if len(cfg.MaskFields) != 0 {
		t.Fatalf("MaskFields=%v", cfg.MaskFields)
	}
}

func TestLoadFromEnvLocalDesktopMode(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "root:pwd@tcp(localhost:3306)/db",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}

	cfg, err := LoadFromEnv(getterFromMap(env))
	if err != nil {
		t.Fatalf("LoadFromEnv err=%v", err)
	}
	if cfg.ApprovalClientMode != "local_desktop" {
		t.Fatalf("ApprovalClientMode=%q", cfg.ApprovalClientMode)
	}
}

func TestLoadFromEnvInvalidApprovalClientMode(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "root:pwd@tcp(localhost:3306)/db",
		EnvApprovalClientMode:     "bad_mode",
		EnvApprovalCallbackSecret: "secret",
	}
	if _, err := LoadFromEnv(getterFromMap(env)); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnvMissingRequired(t *testing.T) {
	env := map[string]string{}
	_, err := LoadFromEnv(getterFromMap(env))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnvOnlyMySQLDSNs(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "core=user:pwd@tcp(localhost:3306)/db_core",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}
	cfg, err := LoadFromEnv(getterFromMap(env))
	if err != nil {
		t.Fatalf("LoadFromEnv err=%v", err)
	}
	if got, want := len(cfg.MySQLDSNs), 1; got != want {
		t.Fatalf("MySQLDSNs len=%d", got)
	}
}

func TestLoadFromEnvMultiMySQLDSNsSeparatedByNewline(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "core=user:pwd@tcp(localhost:3306)/db_core\naudit=user:pwd@tcp(localhost:3306)/db_audit",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}
	cfg, err := LoadFromEnv(getterFromMap(env))
	if err != nil {
		t.Fatalf("LoadFromEnv err=%v", err)
	}
	if got, want := len(cfg.MySQLDSNs), 2; got != want {
		t.Fatalf("MySQLDSNs len=%d", got)
	}
	if cfg.MySQLDSNs[0].Name != "core" || cfg.MySQLDSNs[1].Name != "audit" {
		t.Fatalf("MySQLDSNs=%v", cfg.MySQLDSNs)
	}
}

func TestLoadFromEnvMultiMySQLDSNsSeparatedBySemicolonAndNewline(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "core=user:pwd@tcp(localhost:3306)/db_core;\naudit=user:pwd@tcp(localhost:3306)/db_audit",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}
	cfg, err := LoadFromEnv(getterFromMap(env))
	if err != nil {
		t.Fatalf("LoadFromEnv err=%v", err)
	}
	if got, want := len(cfg.MySQLDSNs), 2; got != want {
		t.Fatalf("MySQLDSNs len=%d", got)
	}
}

func TestLoadFromEnvMaskFields(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "root:pwd@tcp(localhost:3306)/db",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
		EnvMaskFieldKeywords:      "password, token, secret, key",
		EnvMaskFields:             "api_key,users.phone , users.id",
		EnvMaskJSONFields:         "profile,users.meta",
	}
	cfg, err := LoadFromEnv(getterFromMap(env))
	if err != nil {
		t.Fatalf("LoadFromEnv err=%v", err)
	}
	if got, want := len(cfg.MaskFieldKeywords), 4; got != want {
		t.Fatalf("MaskFieldKeywords len=%d", got)
	}
	if got, want := len(cfg.MaskFields), 3; got != want {
		t.Fatalf("MaskFields len=%d", got)
	}
	if got, want := len(cfg.MaskJSONFields), 2; got != want {
		t.Fatalf("MaskJSONFields len=%d", got)
	}
}

func TestSplitCSVEmptyReturnsNil(t *testing.T) {
	if got := splitCSV(""); got != nil {
		t.Fatalf("splitCSV empty should return nil, got=%v", got)
	}
	if got := splitCSV("   "); got != nil {
		t.Fatalf("splitCSV spaces should return nil, got=%v", got)
	}
}

func TestLoadFromEnvInvalidMySQLDSNs(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "user:pwd@tcp(localhost:3306)/db;bad-format-without-separator",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}
	if _, err := LoadFromEnv(getterFromMap(env)); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnvDuplicateMySQLDSNs(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "core=user:pwd@tcp(localhost:3306)/db_core;core=user:pwd@tcp(localhost:3306)/db_core2",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}
	if _, err := LoadFromEnv(getterFromMap(env)); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnvDuplicateMySQLDSNsWithNewlineSeparator(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "core=user:pwd@tcp(localhost:3306)/db_core\ncore=user:pwd@tcp(localhost:3306)/db_core2",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}
	if _, err := LoadFromEnv(getterFromMap(env)); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnvSingleUnnamedMySQLDSN(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "user:pwd@tcp(localhost:3306)/db?parseTime=true&loc=Local",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}
	cfg, err := LoadFromEnv(getterFromMap(env))
	if err != nil {
		t.Fatalf("LoadFromEnv err=%v", err)
	}
	if got, want := len(cfg.MySQLDSNs), 1; got != want {
		t.Fatalf("MySQLDSNs len=%d", got)
	}
	if cfg.MySQLDSNs[0].Name != DefaultSourceName {
		t.Fatalf("source=%q", cfg.MySQLDSNs[0].Name)
	}
	if cfg.MySQLDSNs[0].DSN != "user:pwd@tcp(localhost:3306)/db?parseTime=true&loc=Local" {
		t.Fatalf("dsn=%q", cfg.MySQLDSNs[0].DSN)
	}
}

func TestLoadFromEnvInvalidSourceName(t *testing.T) {
	env := map[string]string{
		EnvMCPBearerToken:         "abc",
		EnvMySQLDSNs:              "core=user:pwd@tcp(localhost:3306)/db;bad.name=user:pwd@tcp(localhost:3306)/db",
		EnvApprovalClientMode:     "local_desktop",
		EnvApprovalCallbackSecret: "secret",
	}
	if _, err := LoadFromEnv(getterFromMap(env)); err == nil {
		t.Fatal("expected error")
	}
}

func getterFromMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

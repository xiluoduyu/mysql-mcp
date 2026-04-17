package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigInitAndSet(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	buf := &strings.Builder{}

	if err := runConfigInit(context.Background(), cfgPath, buf); err != nil {
		t.Fatalf("runConfigInit err=%v", err)
	}
	if !strings.Contains(buf.String(), "config initialized") {
		t.Fatalf("unexpected output: %q", buf.String())
	}

	buf.Reset()
	if err := runConfigSet(context.Background(), cfgPath, "MCP_BIND_ADDR", "0.0.0.0:9090", buf); err != nil {
		t.Fatalf("runConfigSet err=%v", err)
	}
	if !strings.Contains(buf.String(), "config updated") {
		t.Fatalf("unexpected output: %q", buf.String())
	}

	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config err=%v", err)
	}
	if !strings.Contains(string(b), `MCP_BIND_ADDR = "0.0.0.0:9090"`) {
		t.Fatalf("config content unexpected: %s", string(b))
	}
}

func TestConfigSetUpdatesCompactTomlKey(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	content := strings.Join([]string{
		`MCP_BIND_ADDR="127.0.0.1:9090"`,
		`MCP_BEARER_TOKEN="token"`,
		`[MYSQL_DSNS]`,
		`default="user:pwd@tcp(127.0.0.1:3306)/db"`,
		`APPROVAL_CALLBACK_SECRET="secret"`,
	}, "\n") + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config err=%v", err)
	}

	if err := runConfigSet(context.Background(), cfgPath, "MCP_BIND_ADDR", "0.0.0.0:9090", &strings.Builder{}); err != nil {
		t.Fatalf("runConfigSet err=%v", err)
	}

	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config err=%v", err)
	}
	got := string(b)
	if strings.Count(got, "MCP_BIND_ADDR") != 1 {
		t.Fatalf("expect single MCP_BIND_ADDR entry, got:\n%s", got)
	}
	if !strings.Contains(got, `MCP_BIND_ADDR = "0.0.0.0:9090"`) {
		t.Fatalf("missing updated value, got:\n%s", got)
	}
}

func TestConfigSetUncommentsAndUpdatesKey(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	content := strings.Join([]string{
		`# MCP_BIND_ADDR = "127.0.0.1:9090"`,
		`MCP_BEARER_TOKEN = "token"`,
		`[MYSQL_DSNS]`,
		`default = "user:pwd@tcp(127.0.0.1:3306)/db"`,
		`APPROVAL_CALLBACK_SECRET = "secret"`,
	}, "\n") + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config err=%v", err)
	}

	if err := runConfigSet(context.Background(), cfgPath, "MCP_BIND_ADDR", "0.0.0.0:9090", &strings.Builder{}); err != nil {
		t.Fatalf("runConfigSet err=%v", err)
	}

	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config err=%v", err)
	}
	got := string(b)
	if strings.Count(got, "MCP_BIND_ADDR") != 1 {
		t.Fatalf("expect single MCP_BIND_ADDR entry, got:\n%s", got)
	}
	if strings.Contains(got, `# MCP_BIND_ADDR`) {
		t.Fatalf("expected MCP_BIND_ADDR to be uncommented, got:\n%s", got)
	}
	if !strings.Contains(got, `MCP_BIND_ADDR = "0.0.0.0:9090"`) {
		t.Fatalf("missing updated value, got:\n%s", got)
	}
}

func TestConfigSetRejectsMYSQLDSNS(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	err := runConfigSet(context.Background(), cfgPath, "MYSQL_DSNS", "default=dsn", &strings.Builder{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "managed by [MYSQL_DSNS] table") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigFileIntoEnvDoesNotOverrideExistingEmptyValue(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("MCP_BEARER_TOKEN = \"token-from-file\"\n"), 0o644); err != nil {
		t.Fatalf("write config err=%v", err)
	}

	old, had := os.LookupEnv("MCP_BEARER_TOKEN")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("MCP_BEARER_TOKEN", old)
		} else {
			_ = os.Unsetenv("MCP_BEARER_TOKEN")
		}
	})
	if err := os.Setenv("MCP_BEARER_TOKEN", ""); err != nil {
		t.Fatalf("set env err=%v", err)
	}

	if err := loadConfigFileIntoEnv(cfgPath); err != nil {
		t.Fatalf("loadConfigFileIntoEnv err=%v", err)
	}
	if got := os.Getenv("MCP_BEARER_TOKEN"); got != "" {
		t.Fatalf("env should keep existing empty value, got=%q", got)
	}
}

func TestLoadConfigFileIntoEnvLoadsMYSQLDSNSTable(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	content := strings.Join([]string{
		`MCP_BEARER_TOKEN = "token-from-file"`,
		`[MYSQL_DSNS]`,
		`core = "user:pwd@tcp(127.0.0.1:3306)/core"`,
		`audit = "user:pwd@tcp(127.0.0.1:3306)/audit"`,
	}, "\n") + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config err=%v", err)
	}

	old, had := os.LookupEnv("MYSQL_DSNS")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("MYSQL_DSNS", old)
		} else {
			_ = os.Unsetenv("MYSQL_DSNS")
		}
	})
	_ = os.Unsetenv("MYSQL_DSNS")

	if err := loadConfigFileIntoEnv(cfgPath); err != nil {
		t.Fatalf("loadConfigFileIntoEnv err=%v", err)
	}
	got := os.Getenv("MYSQL_DSNS")
	if got != `core=user:pwd@tcp(127.0.0.1:3306)/core;audit=user:pwd@tcp(127.0.0.1:3306)/audit` {
		t.Fatalf("MYSQL_DSNS=%q", got)
	}
}

func TestLoadConfigFileIntoEnvRejectsLegacyMYSQLDSNSKey(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	content := `MYSQL_DSNS = "user:pwd@tcp(127.0.0.1:3306)/db"` + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config err=%v", err)
	}

	err := loadConfigFileIntoEnv(cfgPath)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "legacy MYSQL_DSNS key is no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigSetRejectsUnknownKey(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	initial := "MCP_BEARER_TOKEN = \"token\"\n"
	if err := os.WriteFile(cfgPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write config err=%v", err)
	}

	err := runConfigSet(context.Background(), cfgPath, "UNKNOWN_KEY", "v", &strings.Builder{})
	if err == nil {
		t.Fatalf("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "invalid config key") {
		t.Fatalf("unexpected error: %v", err)
	}

	b, readErr := os.ReadFile(cfgPath)
	if readErr != nil {
		t.Fatalf("read config err=%v", readErr)
	}
	if string(b) != initial {
		t.Fatalf("config should remain unchanged, got:\n%s", string(b))
	}
}

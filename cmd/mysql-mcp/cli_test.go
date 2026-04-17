package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestBuildCommandDefaults(t *testing.T) {
	home := t.TempDir()
	oldHome, hadHome := os.LookupEnv("HOME")
	t.Cleanup(func() {
		if hadHome {
			_ = os.Setenv("HOME", oldHome)
		} else {
			_ = os.Unsetenv("HOME")
		}
	})
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME err=%v", err)
	}

	cmd := buildCommand(os.Stdout, os.Stderr)
	if cmd.DefaultCommand != "serve" {
		t.Fatalf("DefaultCommand=%q", cmd.DefaultCommand)
	}
	var haveCfg bool
	for _, f := range cmd.Flags {
		switch v := f.(type) {
		case *cli.StringFlag:
			if v.Name == "config" {
				haveCfg = true
				if v.Value != filepath.Join(home, ".mysql-mcp", "config.toml") {
					t.Fatalf("config default=%q", v.Value)
				}
			}
		}
	}
	if !haveCfg {
		t.Fatalf("missing config flag")
	}
}

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
		`MYSQL_DSNS="user:pwd@tcp(127.0.0.1:3306)/db"`,
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
		`MYSQL_DSNS = "user:pwd@tcp(127.0.0.1:3306)/db"`,
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

func TestDefaultConfigFilePathFallbackNoHome(t *testing.T) {
	home := t.TempDir()
	oldHome, hadHome := os.LookupEnv("HOME")
	t.Cleanup(func() {
		if hadHome {
			_ = os.Setenv("HOME", oldHome)
		} else {
			_ = os.Unsetenv("HOME")
		}
	})
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME err=%v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", "/")
	})
	if err := os.Unsetenv("HOME"); err != nil {
		t.Fatalf("unset HOME err=%v", err)
	}

	got := defaultConfigFilePath()
	want := "./config.toml"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

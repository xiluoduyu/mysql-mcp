package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvFileMissingIgnored(t *testing.T) {
	if err := LoadDotEnvFile(filepath.Join(t.TempDir(), "not-exists.env")); err != nil {
		t.Fatalf("LoadDotEnvFile err=%v", err)
	}
}

func TestLoadDotEnvFileSetsValues(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, ".env")
	content := "" +
		"# comment\n" +
		"MCP_BIND_ADDR=127.0.0.1:9999\n" +
		"APPROVAL_SUBMIT_PATH='/api/submit'\n" +
		"APPROVAL_STATUS_PATH_TEMPLATE=\"/api/status/{id}\"\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write file err=%v", err)
	}

	clearEnvKey(t, EnvMCPBindAddr)
	clearEnvKey(t, EnvApprovalSubmitPath)
	clearEnvKey(t, EnvApprovalStatusPathTemplate)

	if err := LoadDotEnvFile(p); err != nil {
		t.Fatalf("LoadDotEnvFile err=%v", err)
	}
	if got := os.Getenv(EnvMCPBindAddr); got != "127.0.0.1:9999" {
		t.Fatalf("env %s=%q", EnvMCPBindAddr, got)
	}
	if got := os.Getenv(EnvApprovalSubmitPath); got != "/api/submit" {
		t.Fatalf("env %s=%q", EnvApprovalSubmitPath, got)
	}
	if got := os.Getenv(EnvApprovalStatusPathTemplate); got != "/api/status/{id}" {
		t.Fatalf("env %s=%q", EnvApprovalStatusPathTemplate, got)
	}
}

func TestLoadDotEnvFileDoesNotOverrideExisting(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, ".env")
	if err := os.WriteFile(p, []byte("MCP_BIND_ADDR=127.0.0.1:9999\n"), 0o644); err != nil {
		t.Fatalf("write file err=%v", err)
	}

	old := os.Getenv(EnvMCPBindAddr)
	t.Cleanup(func() { _ = os.Setenv(EnvMCPBindAddr, old) })
	if err := os.Setenv(EnvMCPBindAddr, "127.0.0.1:8080"); err != nil {
		t.Fatalf("setenv err=%v", err)
	}

	if err := LoadDotEnvFile(p); err != nil {
		t.Fatalf("LoadDotEnvFile err=%v", err)
	}
	if got := os.Getenv(EnvMCPBindAddr); got != "127.0.0.1:8080" {
		t.Fatalf("env %s=%q", EnvMCPBindAddr, got)
	}
}

func TestLoadDotEnvFileInvalidLine(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, ".env")
	if err := os.WriteFile(p, []byte("INVALID_LINE\n"), 0o644); err != nil {
		t.Fatalf("write file err=%v", err)
	}

	if err := LoadDotEnvFile(p); err == nil {
		t.Fatal("expected error")
	}
}

func clearEnvKey(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
	_ = os.Unsetenv(key)
}

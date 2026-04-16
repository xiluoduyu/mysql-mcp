package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCLIOptionsDefaults(t *testing.T) {
	cwd := t.TempDir()
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

	opts, err := parseCLIOptions(nil, cwd)
	if err != nil {
		t.Fatalf("parseCLIOptions err=%v", err)
	}
	if opts.ShowHelp {
		t.Fatalf("ShowHelp=%v", opts.ShowHelp)
	}
	if opts.DotEnvPath != filepath.Join(cwd, ".env.mysql-mcp") {
		t.Fatalf("DotEnvPath=%q", opts.DotEnvPath)
	}
}

func TestParseCLIOptionsEnvFileOverride(t *testing.T) {
	cwd := "/user/xiluo"
	opts, err := parseCLIOptions([]string{"-env-file", "/tmp/custom.env"}, cwd)
	if err != nil {
		t.Fatalf("parseCLIOptions err=%v", err)
	}
	if opts.DotEnvPath != "/tmp/custom.env" {
		t.Fatalf("DotEnvPath=%q", opts.DotEnvPath)
	}
}

func TestParseCLIOptionsHelp(t *testing.T) {
	cwd := "/user/xiluo"
	opts, err := parseCLIOptions([]string{"-h"}, cwd)
	if err != nil {
		t.Fatalf("parseCLIOptions err=%v", err)
	}
	if !opts.ShowHelp {
		t.Fatalf("ShowHelp=%v", opts.ShowHelp)
	}
}

func TestParseCLIOptionsHelpWithDoubleDash(t *testing.T) {
	cwd := "/user/xiluo"
	opts, err := parseCLIOptions([]string{"--", "-h"}, cwd)
	if err != nil {
		t.Fatalf("parseCLIOptions err=%v", err)
	}
	if !opts.ShowHelp {
		t.Fatalf("ShowHelp=%v", opts.ShowHelp)
	}
}

func TestParseCLIOptionsDefaultsUseHomeDotEnvWhenExists(t *testing.T) {
	cwd := t.TempDir()
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

	homeDir := filepath.Join(home, ".mysql-mcp")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home dir err=%v", err)
	}
	homeEnv := filepath.Join(homeDir, ".env")
	if err := os.WriteFile(homeEnv, []byte("A=1\n"), 0o644); err != nil {
		t.Fatalf("write home env err=%v", err)
	}

	opts, err := parseCLIOptions(nil, cwd)
	if err != nil {
		t.Fatalf("parseCLIOptions err=%v", err)
	}
	if opts.DotEnvPath != homeEnv {
		t.Fatalf("DotEnvPath=%q want=%q", opts.DotEnvPath, homeEnv)
	}
}

func TestHelpTextContainsProjectAndTools(t *testing.T) {
	out := helpText("mysql-mcp", "/user/xiluo/.mysql-mcp/.env")
	mustContain := []string{
		"MySQL MCP server",
		"Project",
		"Tools",
		"list_tables",
		"describe_table",
		"query_table",
		"/user/xiluo/.mysql-mcp/.env",
	}
	for _, c := range mustContain {
		if !strings.Contains(out, c) {
			t.Fatalf("help text missing %q", c)
		}
	}
}

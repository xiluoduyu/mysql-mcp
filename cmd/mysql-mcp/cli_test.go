package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestBuildCommandDefaultsFallbackToCwdDotEnv(t *testing.T) {
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

	cmd := buildCommand(cwd, os.Stdout, os.Stderr)
	if cmd.DefaultCommand != "serve" {
		t.Fatalf("DefaultCommand=%q", cmd.DefaultCommand)
	}
	var haveEnv, haveCfg bool
	for _, f := range cmd.Flags {
		switch v := f.(type) {
		case *cli.StringFlag:
			if v.Name == "env-file" {
				haveEnv = true
				if v.Value != filepath.Join(cwd, ".env.mysql-mcp") {
					t.Fatalf("env-file default=%q", v.Value)
				}
			}
			if v.Name == "config" {
				haveCfg = true
				if v.Value != filepath.Join(home, ".mysql-mcp", "config.toml") {
					t.Fatalf("config default=%q", v.Value)
				}
			}
		}
	}
	if !haveEnv || !haveCfg {
		t.Fatalf("missing expected flags: env=%v config=%v", haveEnv, haveCfg)
	}
}

func TestBuildCommandDefaultsUseHomeDotEnvWhenExists(t *testing.T) {
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
		t.Fatalf("mkdir err=%v", err)
	}
	homeEnv := filepath.Join(homeDir, ".env")
	if err := os.WriteFile(homeEnv, []byte("A=1\n"), 0o644); err != nil {
		t.Fatalf("write err=%v", err)
	}

	cmd := buildCommand(cwd, os.Stdout, os.Stderr)
	for _, f := range cmd.Flags {
		if v, ok := f.(*cli.StringFlag); ok && v.Name == "env-file" {
			if v.Value != homeEnv {
				t.Fatalf("env-file=%q want=%q", v.Value, homeEnv)
			}
			return
		}
	}
	t.Fatalf("env-file flag not found")
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

func TestResolveDefaultDotEnvPathFallback(t *testing.T) {
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

	got := resolveDefaultDotEnvPath(cwd)
	want := filepath.Join(cwd, ".env.mysql-mcp")
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestResolveDefaultDotEnvPathUseHomeFile(t *testing.T) {
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
		t.Fatalf("mkdir err=%v", err)
	}
	homeEnv := filepath.Join(homeDir, ".env")
	if err := os.WriteFile(homeEnv, []byte("A=1\n"), 0o644); err != nil {
		t.Fatalf("write err=%v", err)
	}

	got := resolveDefaultDotEnvPath(cwd)
	if got != homeEnv {
		t.Fatalf("got=%q want=%q", got, homeEnv)
	}
}

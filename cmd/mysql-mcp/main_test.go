package main

import (
	"os"
	"path/filepath"
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

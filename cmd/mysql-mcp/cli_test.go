package main

import (
	"strings"
	"testing"
)

func TestParseCLIOptionsDefaults(t *testing.T) {
	cwd := "/user/xiluo"
	opts, err := parseCLIOptions(nil, cwd)
	if err != nil {
		t.Fatalf("parseCLIOptions err=%v", err)
	}
	if opts.ShowHelp {
		t.Fatalf("ShowHelp=%v", opts.ShowHelp)
	}
	if opts.DotEnvPath != "/user/xiluo/.env" {
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

func TestHelpTextContainsProjectAndTools(t *testing.T) {
	out := helpText("mysql-mcp", "/user/xiluo/.env")
	mustContain := []string{
		"MySQL MCP server",
		"Project",
		"Tools",
		"list_tables",
		"describe_table",
		"query_table",
		"/user/xiluo/.env",
	}
	for _, c := range mustContain {
		if !strings.Contains(out, c) {
			t.Fatalf("help text missing %q", c)
		}
	}
}

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xiluoduyu/mysql-mcp/internal/config"
)

type cliOptions struct {
	DotEnvPath string
	ShowHelp   bool
}

func parseCLIOptions(args []string, cwd string) (cliOptions, error) {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	defaultDotEnvPath := config.DefaultDotEnvPath()
	if _, err := os.Stat(defaultDotEnvPath); err != nil && os.IsNotExist(err) {
		defaultDotEnvPath = filepath.Join(cwd, config.LegacyDefaultDotEnvPath)
	}
	opts := cliOptions{
		DotEnvPath: defaultDotEnvPath,
	}

	fs := flag.NewFlagSet("mysql-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.ShowHelp, "h", false, "show help")
	fs.BoolVar(&opts.ShowHelp, "help", false, "show help")
	fs.StringVar(&opts.DotEnvPath, "env-file", defaultDotEnvPath, "path to dotenv file")
	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}
	return opts, nil
}

func helpText(programName string, defaultDotEnvPath string) string {
	return fmt.Sprintf(`%s - MySQL MCP server (read-only query with approval gate)

Project:
  Standalone MySQL MCP server with built-in approval flow and result masking.

Tools:
  list_tables    List accessible tables
  describe_table Describe table schema and fields
  query_table    Query rows by filters (approval required; reuse request_id when pending)

Usage:
  %s [-env-file /path/to/.env]
  %s -h

Flags:
  -env-file string
      Path to .env file.
      Default: %s
  -h, --help
      Show this help message.
`, programName, programName, programName, defaultDotEnvPath)
}

func printHelp(w io.Writer, programName string, defaultDotEnvPath string) {
	_, _ = fmt.Fprint(w, helpText(programName, defaultDotEnvPath))
}

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

type appOptions struct {
	ConfigPath string
}

func buildCommand(stdout io.Writer, stderr io.Writer) *cli.Command {
	defaultConfigPath := defaultConfigFilePath()

	opts := &appOptions{
		ConfigPath: defaultConfigPath,
	}

	cmd := &cli.Command{
		Name:      "mysql-mcp",
		Usage:     "MySQL MCP server (read-only query with approval gate)",
		UsageText: "mysql-mcp <command> [command options]",
		Writer:    stdout,
		ErrWriter: stderr,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Usage:       "Path to config file",
				Value:       defaultConfigPath,
				Destination: &opts.ConfigPath,
			},
		},
		DefaultCommand: "serve",
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Start MCP service",
				Action: func(ctx context.Context, c *cli.Command) error {
					return runServe(ctx, opts)
				},
			},
			{
				Name:  "config",
				Usage: "Manage config file",
				Commands: []*cli.Command{
					{
						Name:  "init",
						Usage: "Create config template file if missing",
						Action: func(ctx context.Context, c *cli.Command) error {
							return runConfigInit(ctx, opts.ConfigPath, c.Root().Writer)
						},
					},
					{
						Name:      "set",
						Usage:     "Set key/value in config file",
						ArgsUsage: "<key> <value>",
						Action: func(ctx context.Context, c *cli.Command) error {
							key := strings.TrimSpace(c.Args().Get(0))
							value := c.Args().Get(1)
							if key == "" || c.Args().Len() < 2 {
								return fmt.Errorf("usage: mysql-mcp config set <key> <value>")
							}
							return runConfigSet(ctx, opts.ConfigPath, key, value, c.Root().Writer)
						},
					},
				},
			},
		},
	}

	return cmd
}

func defaultConfigFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "./config.toml"
	}
	return filepath.Join(home, ".mysql-mcp", "config.toml")
}

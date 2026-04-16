package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiluoduyu/mysql-mcp/internal/config"
)

func loadRuntimeConfig(opts *appOptions, cwd string) (config.Config, error) {
	// Priority:
	// 1) Environment variables already exported by caller.
	// 2) Config file key/value map (for v1 limited keys).
	// 3) Legacy dotenv fallback for compatibility.
	if err := loadConfigFileIntoEnv(opts.ConfigPath); err != nil {
		return config.Config{}, err
	}

	legacyDefault := filepath.Join(cwd, config.LegacyDefaultDotEnvPath)
	if opts.DotEnvPath != "" {
		// Keep backward compatibility in v1.
		_ = config.LoadDotEnvFile(opts.DotEnvPath)
	} else {
		_ = config.LoadDotEnvFile(legacyDefault)
	}

	cfg, err := config.LoadFromEnv(os.Getenv)
	if err != nil {
		return config.Config{}, fmt.Errorf("load config error: %w", err)
	}
	return cfg, nil
}

func runConfigInit(ctx context.Context, configPath string, out io.Writer) error {
	_ = ctx
	p := strings.TrimSpace(configPath)
	if p == "" {
		return fmt.Errorf("config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create config dir error: %w", err)
	}
	if _, err := os.Stat(p); err == nil {
		_, _ = fmt.Fprintf(out, "config already exists: %s\n", p)
		return nil
	}

	content := []byte(defaultConfigTemplate())
	if err := os.WriteFile(p, content, 0o644); err != nil {
		return fmt.Errorf("write config file error: %w", err)
	}
	_, _ = fmt.Fprintf(out, "config initialized: %s\n", p)
	return nil
}

func runConfigSet(ctx context.Context, configPath, key, value string, out io.Writer) error {
	_ = ctx
	p := strings.TrimSpace(configPath)
	if p == "" {
		return fmt.Errorf("config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create config dir error: %w", err)
	}

	lines := []string{}
	if b, err := os.ReadFile(p); err == nil {
		lines = strings.Split(string(b), "\n")
	}

	found := false
	prefix := key + " = "
	for i := range lines {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, prefix) {
			lines[i] = fmt.Sprintf("%s = %q", key, value)
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, fmt.Sprintf("%s = %q", key, value))
	}
	content := strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write config file error: %w", err)
	}

	_, _ = fmt.Fprintf(out, "config updated: %s\n", p)
	return nil
}

func loadConfigFileIntoEnv(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config file error: %w", err)
	}

	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		idx := strings.Index(l, "=")
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(l[:idx])
		v := strings.Trim(strings.TrimSpace(l[idx+1:]), "\"")
		if k == "" {
			continue
		}
		if os.Getenv(k) != "" {
			continue
		}
		_ = os.Setenv(k, v)
	}
	return nil
}

func defaultConfigTemplate() string {
	return strings.TrimSpace(`
# mysql-mcp config (TOML key/value)
# V1: values are loaded into environment variables.

MCP_BIND_ADDR = "127.0.0.1:9090"
MCP_BEARER_TOKEN = "replace-with-strong-token"
MYSQL_DSNS = "user:password@tcp(127.0.0.1:3306)/dbname?parseTime=true&loc=Local"
APPROVAL_CLIENT_MODE = "local_desktop"
APPROVAL_CALLBACK_SECRET = "replace-with-hmac-secret"

# Optional
# APPROVAL_BASE_URL = "http://127.0.0.1:8088"
# APPROVAL_SUBMIT_PATH = "/approvals"
# APPROVAL_STATUS_PATH_TEMPLATE = "/approvals/{id}"
# APPROVAL_POLL_INTERVAL = "3s"
# APPROVAL_TIMEOUT = "30m"
# STATE_SQLITE_PATH = ""
# MAX_LIMIT = "200"
# MASK_FIELD_KEYWORDS = ""
# MASK_FIELDS = ""
# MASK_JSON_FIELDS = ""
`)
}

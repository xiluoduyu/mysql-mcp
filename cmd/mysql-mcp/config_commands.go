package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xiluoduyu/mysql-mcp/internal/config"
)

var allowedConfigKeys = map[string]struct{}{
	config.EnvMCPBindAddr:                {},
	config.EnvMCPBearerToken:             {},
	config.EnvMySQLDSNs:                  {},
	config.EnvApprovalClientMode:         {},
	config.EnvApprovalBaseURL:            {},
	config.EnvApprovalSubmitPath:         {},
	config.EnvApprovalStatusPathTemplate: {},
	config.EnvApprovalCallbackSecret:     {},
	config.EnvApprovalPollInterval:       {},
	config.EnvApprovalTimeout:            {},
	config.EnvStateSQLitePath:            {},
	config.EnvMaxLimit:                   {},
	config.EnvMaskFieldKeywords:          {},
	config.EnvMaskFields:                 {},
	config.EnvMaskJSONFields:             {},
}

// loadRuntimeConfig keeps env-first behavior intentionally:
// process env is the runtime source of truth, config file provides persistent defaults.
// This allows per-project/runtime overrides and avoids forcing secrets to be stored on disk.
func loadRuntimeConfig(opts *appOptions) (config.Config, error) {
	// Priority:
	// 1) Environment variables already exported by caller.
	// 2) Config file key/value map.
	if err := loadConfigFileIntoEnv(opts.ConfigPath); err != nil {
		return config.Config{}, err
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
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("config key is empty")
	}
	if key == config.EnvMySQLDSNs {
		return fmt.Errorf("%s is managed by [%s] table in config.toml; edit that table directly", config.EnvMySQLDSNs, config.EnvMySQLDSNs)
	}
	// Guard against typo/unknown keys: only keys recognized by runtime are writable.
	if _, ok := allowedConfigKeys[key]; !ok {
		return fmt.Errorf("invalid config key %q; allowed keys: %s", key, strings.Join(listAllowedConfigKeys(), ", "))
	}

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
	for i := range lines {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		// Allow commented entries (e.g. "# KEY = ...") so `config set` can
		// uncomment-and-update instead of appending duplicate keys.
		normalized := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		if normalized == "" {
			continue
		}
		idx := strings.Index(normalized, "=")
		if idx <= 0 {
			continue
		}
		existingKey := strings.TrimSpace(normalized[:idx])
		if existingKey != key {
			continue
		}
		lines[i] = fmt.Sprintf("%s = %q", key, value)
		found = true
		break
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

func listAllowedConfigKeys() []string {
	keys := make([]string, 0, len(allowedConfigKeys))
	for k := range allowedConfigKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// loadConfigFileIntoEnv parses config.toml subset:
// - top-level entries: KEY = VALUE
// - [MYSQL_DSNS] table entries: source = dsn
// - blank lines and "#" comments are ignored
// - values keep simple string semantics used by CLI template/set flow
//
// This is intentionally not a full TOML parser.
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
	inMySQLDSNsTable := false
	dsnEntries := make([]string, 0, 4)
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			sectionName := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(l, "["), "]"))
			inMySQLDSNsTable = sectionName == config.EnvMySQLDSNs
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
		if inMySQLDSNsTable {
			if v == "" {
				return fmt.Errorf("[%s] entry %q has empty dsn", config.EnvMySQLDSNs, k)
			}
			dsnEntries = append(dsnEntries, fmt.Sprintf("%s=%s", k, v))
			continue
		}
		if k == config.EnvMySQLDSNs {
			return fmt.Errorf("legacy %s key is no longer supported in config.toml; use [%s] table", config.EnvMySQLDSNs, config.EnvMySQLDSNs)
		}
		// Presence check (not value check): empty string is still an explicit env choice.
		if _, exists := os.LookupEnv(k); exists {
			continue
		}
		_ = os.Setenv(k, v)
	}
	if len(dsnEntries) > 0 {
		// Presence check (not value check): empty string is still an explicit env choice.
		if _, exists := os.LookupEnv(config.EnvMySQLDSNs); !exists {
			_ = os.Setenv(config.EnvMySQLDSNs, strings.Join(dsnEntries, ";"))
		}
	}
	return nil
}

func defaultConfigTemplate() string {
	return strings.TrimSpace(`
# mysql-mcp config (toml-like subset)
# Runtime still uses env-first precedence:
# process env > values loaded from this file.

MCP_BIND_ADDR = "127.0.0.1:9090"
MCP_BEARER_TOKEN = "replace-with-strong-token"
APPROVAL_CLIENT_MODE = "local_desktop"
APPROVAL_CALLBACK_SECRET = "replace-with-hmac-secret"

[MYSQL_DSNS]
default = "user:password@tcp(127.0.0.1:3306)/dbname?parseTime=true&loc=Local"

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

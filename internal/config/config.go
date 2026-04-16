package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config contains runtime settings for the MySQL MCP server.
type Config struct {
	BindAddr                   string
	BearerToken                string
	MySQLDSNs                  []NamedDSN
	MaskFieldKeywords          []string
	MaskFields                 []string
	MaskJSONFields             []string
	ApprovalClientMode         string
	ApprovalBaseURL            string
	ApprovalSubmitPath         string
	ApprovalStatusPathTemplate string
	ApprovalCallbackSecret     string
	ApprovalPollInterval       time.Duration
	ApprovalTimeout            time.Duration
	StateSQLitePath            string
	MaxLimit                   int
}

// NamedDSN maps one source name to a MySQL DSN.
type NamedDSN struct {
	Name string
	DSN  string
}

// LoadFromEnv loads config from environment getter.
func LoadFromEnv(getEnv func(string) string) (Config, error) {
	namedDSNs, err := parseNamedDSNs(getEnv(EnvMySQLDSNs))
	if err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", EnvMySQLDSNs, err)
	}

	cfg := Config{
		BindAddr:                   defaultStr(getEnv(EnvMCPBindAddr), DefaultMCPBindAddr),
		BearerToken:                strings.TrimSpace(getEnv(EnvMCPBearerToken)),
		MySQLDSNs:                  namedDSNs,
		MaskFieldKeywords:          splitCSV(getEnv(EnvMaskFieldKeywords)),
		MaskFields:                 splitCSV(getEnv(EnvMaskFields)),
		MaskJSONFields:             splitCSV(getEnv(EnvMaskJSONFields)),
		ApprovalClientMode:         defaultStr(strings.TrimSpace(getEnv(EnvApprovalClientMode)), DefaultApprovalClientMode),
		ApprovalBaseURL:            strings.TrimSpace(getEnv(EnvApprovalBaseURL)),
		ApprovalSubmitPath:         defaultStr(strings.TrimSpace(getEnv(EnvApprovalSubmitPath)), DefaultApprovalSubmitPath),
		ApprovalStatusPathTemplate: defaultStr(strings.TrimSpace(getEnv(EnvApprovalStatusPathTemplate)), DefaultApprovalStatusPathTemplate),
		ApprovalCallbackSecret:     strings.TrimSpace(getEnv(EnvApprovalCallbackSecret)),
		StateSQLitePath:            defaultStr(strings.TrimSpace(getEnv(EnvStateSQLitePath)), DefaultStateSQLitePath),
		MaxLimit:                   DefaultMaxLimit,
		ApprovalPollInterval:       3 * time.Second,
		ApprovalTimeout:            30 * time.Minute,
	}

	if raw := strings.TrimSpace(getEnv(EnvMaxLimit)); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return Config{}, fmt.Errorf("invalid %s: %q", EnvMaxLimit, raw)
		}
		cfg.MaxLimit = v
	}

	if raw := strings.TrimSpace(getEnv(EnvApprovalPollInterval)); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("invalid %s: %q", EnvApprovalPollInterval, raw)
		}
		cfg.ApprovalPollInterval = d
	}

	if raw := strings.TrimSpace(getEnv(EnvApprovalTimeout)); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("invalid %s: %q", EnvApprovalTimeout, raw)
		}
		cfg.ApprovalTimeout = d
	}

	if cfg.BearerToken == "" {
		return Config{}, fmt.Errorf("%s is required", EnvMCPBearerToken)
	}
	if len(cfg.MySQLDSNs) == 0 {
		return Config{}, fmt.Errorf("%s is required", EnvMySQLDSNs)
	}
	switch cfg.ApprovalClientMode {
	case "http", "local_desktop":
	default:
		return Config{}, fmt.Errorf("invalid %s: %q", EnvApprovalClientMode, cfg.ApprovalClientMode)
	}
	if cfg.ApprovalClientMode == "http" && cfg.ApprovalBaseURL == "" {
		return Config{}, fmt.Errorf("%s is required when %s=http", EnvApprovalBaseURL, EnvApprovalClientMode)
	}
	if cfg.ApprovalCallbackSecret == "" {
		return Config{}, fmt.Errorf("%s is required", EnvApprovalCallbackSecret)
	}

	return cfg, nil
}

func defaultStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func parseNamedDSNs(raw string) ([]NamedDSN, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ";")
	if len(parts) == 1 {
		item := strings.TrimSpace(parts[0])
		if item == "" {
			return nil, nil
		}
		if !looksLikeNamedDSN(item) {
			if eq := strings.Index(item, "="); eq > 0 {
				left := strings.TrimSpace(item[:eq])
				if !looksLikeDSNPrefix(left) {
					return nil, fmt.Errorf("entry %q has invalid source name", item)
				}
			}
			return []NamedDSN{{Name: DefaultSourceName, DSN: item}}, nil
		}
	}
	out := make([]NamedDSN, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		idx := strings.Index(item, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("entry %q should be name=dsn", item)
		}
		rawName := strings.TrimSpace(item[:idx])
		if !isValidSourceName(rawName) {
			return nil, fmt.Errorf("entry %q has invalid source name", item)
		}
		name := normalizeSourceName(rawName)
		dsn := strings.TrimSpace(item[idx+1:])
		if name == "" {
			return nil, fmt.Errorf("entry %q has empty source name", item)
		}
		if dsn == "" {
			return nil, fmt.Errorf("entry %q has empty dsn", item)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate source name %q", name)
		}
		seen[name] = struct{}{}
		out = append(out, NamedDSN{Name: name, DSN: dsn})
	}
	return out, nil
}

const DefaultSourceName = "default"

func looksLikeNamedDSN(item string) bool {
	eq := strings.Index(item, "=")
	if eq <= 0 {
		return false
	}
	name := strings.TrimSpace(item[:eq])
	return isValidSourceName(name)
}

func looksLikeDSNPrefix(v string) bool {
	return strings.ContainsAny(v, ":@/?()")
}

func isValidSourceName(v string) bool {
	name := strings.TrimSpace(v)
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeSourceName(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

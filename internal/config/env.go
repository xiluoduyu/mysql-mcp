package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	EnvMCPBindAddr                = "MCP_BIND_ADDR"
	EnvMCPBearerToken             = "MCP_BEARER_TOKEN"
	EnvMySQLDSNs                  = "MYSQL_DSNS"
	EnvApprovalClientMode         = "APPROVAL_CLIENT_MODE"
	EnvApprovalBaseURL            = "APPROVAL_BASE_URL"
	EnvApprovalSubmitPath         = "APPROVAL_SUBMIT_PATH"
	EnvApprovalStatusPathTemplate = "APPROVAL_STATUS_PATH_TEMPLATE"
	EnvApprovalCallbackSecret     = "APPROVAL_CALLBACK_SECRET"
	EnvApprovalPollInterval       = "APPROVAL_POLL_INTERVAL"
	EnvApprovalTimeout            = "APPROVAL_TIMEOUT"
	EnvStateSQLitePath            = "STATE_SQLITE_PATH"
	EnvMaxLimit                   = "MAX_LIMIT"
	EnvMaskFieldKeywords          = "MASK_FIELD_KEYWORDS"
	EnvMaskFields                 = "MASK_FIELDS"
	EnvMaskJSONFields             = "MASK_JSON_FIELDS"
)

const (
	DefaultMCPBindAddr                = "127.0.0.1:9090"
	DefaultApprovalClientMode         = "local_desktop"
	DefaultApprovalSubmitPath         = "/approvals"
	DefaultApprovalStatusPathTemplate = "/approvals/{id}"
	DefaultStateSQLitePath            = "./data/state.db"
	DefaultDotEnvPath                 = ".env"
	DefaultMaxLimit                   = 200
)

// LoadDotEnvFile loads dotenv entries from file into process env.
// Existing environment variables are not overridden.
func LoadDotEnvFile(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open dotenv file error: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		idx := strings.IndexRune(line, '=')
		if idx <= 0 {
			return fmt.Errorf("invalid dotenv line %d", lineNo)
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if key == "" {
			return fmt.Errorf("invalid dotenv line %d: empty key", lineNo)
		}
		if strings.HasPrefix(val, "\"") {
			startLine := lineNo
			for !hasCompleteDoubleQuotedValue(val) {
				if !scanner.Scan() {
					return fmt.Errorf("invalid dotenv value at line %d: unterminated double-quoted value", startLine)
				}
				lineNo++
				val += "\n" + scanner.Text()
			}
		}

		parsed, err := parseDotEnvValue(val)
		if err != nil {
			return fmt.Errorf("invalid dotenv value at line %d: %w", lineNo, err)
		}
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, parsed); err != nil {
			return fmt.Errorf("set env %s error: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read dotenv file error: %w", err)
	}
	return nil
}

func parseDotEnvValue(v string) (string, error) {
	if strings.HasPrefix(v, "\"") {
		return parseDoubleQuotedDotEnvValue(v)
	}
	if strings.HasPrefix(v, "'") {
		if len(v) < 2 || !strings.HasSuffix(v, "'") {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		return v[1 : len(v)-1], nil
	}
	if strings.ContainsRune(v, '\n') || strings.ContainsRune(v, '\r') {
		return "", fmt.Errorf("unquoted value cannot contain newline")
	}
	return v, nil
}

func hasCompleteDoubleQuotedValue(v string) bool {
	if !strings.HasPrefix(v, "\"") {
		return false
	}
	escaped := false
	for i := 1; i < len(v); i++ {
		ch := v[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return strings.TrimSpace(v[i+1:]) == ""
		}
	}
	return false
}

func parseDoubleQuotedDotEnvValue(v string) (string, error) {
	if !strings.HasPrefix(v, "\"") {
		return "", fmt.Errorf("double-quoted value must start with quote")
	}
	var b strings.Builder
	escaped := false
	for i := 1; i < len(v); i++ {
		ch := v[i]
		if escaped {
			switch ch {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				return "", fmt.Errorf("invalid escape sequence \\%c", ch)
			}
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			if strings.TrimSpace(v[i+1:]) != "" {
				return "", fmt.Errorf("invalid trailing characters after closing quote")
			}
			return b.String(), nil
		}
		b.WriteByte(ch)
	}
	if escaped {
		return "", fmt.Errorf("unterminated escape sequence")
	}
	return "", fmt.Errorf("unterminated double-quoted value")
}

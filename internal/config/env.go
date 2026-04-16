package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
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
	if len(v) >= 2 && strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") {
		s, err := strconv.Unquote(v)
		if err != nil {
			return "", err
		}
		return s, nil
	}
	if len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		return v[1 : len(v)-1], nil
	}
	return v, nil
}

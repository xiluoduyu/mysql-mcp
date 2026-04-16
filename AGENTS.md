# AGENTS.md

This file defines repository-specific guidance for coding agents working in this project.

## Scope

- Applies to the entire repository from this directory downward.
- If another `AGENTS.md` exists in a subdirectory, the nearer one takes precedence for that subtree.

## Project Summary

- Standalone MySQL MCP server with approval gate.
- Core tools:
  - `list_tables`
  - `describe_table`
  - `query_table`
- Approval modes:
  - `APPROVAL_CLIENT_MODE=http`
  - `APPROVAL_CLIENT_MODE=local_desktop`

## Run and Verify

- Start server:
  - `go run ./cmd/mysql-mcp`
- Full tests:
  - `go test ./... -count=1`
- Module-focused tests:
  - `go test ./internal/approval -count=1`
  - `go test ./internal/mcpserver -count=1`
  - `go test ./internal/config -count=1`

## Environment and Config Rules

- Env names are centralized in `internal/config/env.go`.
- Main bootstrap loads `.env` automatically via `config.LoadDotEnvFile(config.DefaultDotEnvPath)`.
- Dotenv behavior:
  - missing `.env` is ignored
  - existing process env values are not overridden
- Do not introduce new scattered env-name string literals if constants already exist in `internal/config`.

## MCP / Approval Contract Rules

- `query_table` uses strict raw input schema.
- `request_id` must be top-level argument only.
- Reject any `request_id` / `reuqest_id` inside `filters`.
- Pending retry must reuse the same `request_id`.
- Approval handling is unified in Gate:
  - callback path: `ApplyCallbackResult`
  - polling path: `RefreshStatus` using `StatusResult`
- Dynamic bypass is expected to work in both approval modes.

## Coding Guidelines for Changes

- Keep behavior-compatible changes minimal and explicit.
- Prefer extending existing modules over introducing new abstractions unless necessary.
- If changing approval flow, update all impacted tests:
  - `internal/approval/*_test.go`
  - stubs in `internal/mcpserver/server_test.go`
  - stubs in `internal/e2e/e2e_test.go`
- If changing MCP tool schema/registration, run `internal/mcpserver` tests.

## Documentation Update Policy

- Keep `README.md` (English) and `README.zh.md` (Chinese) aligned for user-facing behavior.
- If startup/config/approval behavior changes, update both README files in the same change.
- Keep skill docs aligned when changing wrapper scripts:
  - `skills/mysql-mcp/SKILL.md`

## Security / Data Masking

- `query_table` result masking is expected:
  - default keyword-based masking on column names (`password`, `token`, `secret`, `key`)
  - configurable extra/override controls via `MASK_FIELD_KEYWORDS` and `MASK_FIELDS`
- Do not remove or weaken masking behavior without explicit request.

## Safety Constraints

- Do not weaken approval requirements by default.
- Do not bypass table/column whitelist checks.
- Do not relax `query_table` schema strictness without explicit requirement.

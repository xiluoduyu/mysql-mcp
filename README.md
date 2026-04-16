# MySQL MCP Service

Streamable HTTP MCP service for read-only MySQL queries with built-in approval and result masking.

中文版: see [README.zh.md](README.zh.md).

## Why This Project

In daily development and maintenance, teams often need to verify data across multiple data sources.  
This repetitive work is a strong fit for AI automation.  
However, exposing databases directly to AI is risky, and preconfiguring fine-grained ACLs is often costly.  
This project exposes data to LLM agents through a guarded model: `MCP + strict initial approval + dynamic bypass`, balancing safety and operational efficiency.

## Quick Start

```bash
cp .env.example .env
go run ./cmd/mysql-mcp
```

Show help:

```bash
go run ./cmd/mysql-mcp -h
```

Specify `.env` path:

```bash
go run ./cmd/mysql-mcp -env-file /path/to/custom.env
```

Install CLI binary with `go install`:

```bash
# in this repo
go install ./cmd/mysql-mcp

# from module path (outside repo)
go install github.com/xiluoduyu/mysql-mcp/cmd/mysql-mcp@latest
```

Notes:

- `.env` is auto-loaded at startup from the command working directory (for example, running `mysql-mcp` in `/user/xiluo` loads `/user/xiluo/.env` by default).
- You can override dotenv path with `-env-file`.
- Existing process env vars are not overridden by `.env`.
- Dotenv multi-line values:
  - Double-quoted values support both literal newlines and `\n`.
  - Single-quoted or unquoted values do not support cross-line values.

## Endpoints

- `/mcp` (requires `Authorization: Bearer <MCP_BEARER_TOKEN>`)
  - `POST`: primary JSON-RPC request path (tool calls)
  - `GET`: stream/listen channel for server notifications (SSE)
  - `DELETE`: session termination in stateful streamable-http mode
- `GET /healthz` (no auth)
- `POST /callbacks/approvals` (approval callback)

Callback signature headers:

- `X-Timestamp`: unix seconds
- `X-Signature`: `hex(hmac_sha256(secret, timestamp + "." + body))`

## Required Configuration

- `MCP_BEARER_TOKEN`
- `MYSQL_DSNS`
- `APPROVAL_CALLBACK_SECRET`

Defaults:

- `APPROVAL_CLIENT_MODE=local_desktop`
- `MCP_BIND_ADDR=127.0.0.1:9090`
- `MAX_LIMIT=200`

Multi-source configuration:

- Single source (name optional): `MYSQL_DSNS=<dsn>`, auto-mapped to source=`default`.
- Multiple sources: `MYSQL_DSNS` supports both `;` and newline as separators.
  - Example (`;`): `MYSQL_DSNS=core=user:pwd@tcp(127.0.0.1:3306)/core;audit=user:pwd@tcp(127.0.0.1:3306)/audit`
  - Example (newline):
    ```env
    MYSQL_DSNS="core=user:pwd@tcp(127.0.0.1:3306)/core
    audit=user:pwd@tcp(127.0.0.1:3306)/audit"
    ```
  - `name` is used by tool calls as `source`.
  - Source name allows only letters, digits, `_`, `-`.

When `APPROVAL_CLIENT_MODE=http`, set:

- `APPROVAL_BASE_URL`

## Tools

- `list_tables`
- `describe_table`
- `query_table`

`query_table` input rules:

- Required: `table`
- Optional: `source`, `filters`, `order_by`, `order`, `limit`, `offset`, `request_id`
- `request_id` must be top-level only
- `filters` must not include `request_id` or `reuqest_id`

`list_tables` / `describe_table` also support optional `source`.

## Approval Behavior (`query_table`)

- First request may omit `request_id` (server returns one).
- If decision is `pending`, retry with the same payload + same `request_id`.
- If decision is `reject`, stop and surface `reason`.
- If approved, response returns `rows` and `count`.

## Result Masking

By default, query result values are masked as `******` when field names contain:

- `password`
- `token`
- `secret`
- `key`

Customize with:

- `MASK_FIELD_KEYWORDS`: comma-separated keywords
- `MASK_FIELDS`: comma-separated explicit fields (`field` or `table.field`)
- `MASK_JSON_FIELDS`: comma-separated fields to deep-mask nested JSON values
  - supports `field` or `table.field`
  - supports JSON strings and JSON-typed values (object/array/raw json)

Example:

```bash
MASK_FIELD_KEYWORDS=password,token,secret,key,credential
MASK_FIELDS=api_key,users.phone,orders.bank_card
MASK_JSON_FIELDS=users.profile,orders.payload
```

## Extend Approval Mode

Approval entry is unified by `approval.Gate`, and all approval forms (HTTP callback, local desktop prompt, custom modes) implement the same client contract:

```go
type ApprovalClient interface {
	Submit(ctx context.Context, req SubmitRequest) (string, error)
	GetStatus(ctx context.Context, externalID string) (StatusResult, error)
}
```

`StatusResult` can return:

- `Status`: `pending | approved | rejected`
- `Reason`: optional reason text
- `BypassScope` + `BypassTTL`: optional dynamic bypass policy

Typical extension patterns:

- Wrap an external approval system:
  - `Submit` maps MCP request to external API and returns external id.
  - `GetStatus` polls external status and maps it to `StatusResult`.
  - Optional callback endpoint can call `Gate.ApplyCallbackResult(...)` to push decisions.
- Implement a local/in-process approver (like `local_desktop`):
  - Trigger UI/dialog in `Submit`.
  - Cache and expose decisions in `GetStatus`.
  - Return bypass scope/ttl after approval when needed.

Then wire it in `cmd/mysql-mcp/main.go` by assigning your implementation to `approvalClient`.

## Agent Wrapper Skill (Optional)

- `skills/mysql-mcp/SKILL.md`
- `skills/mysql-mcp/scripts/mcp_tools.sh`
- `skills/mysql-mcp/scripts/query_table_with_approval.sh`

## Test

```bash
go test ./... -count=1
```

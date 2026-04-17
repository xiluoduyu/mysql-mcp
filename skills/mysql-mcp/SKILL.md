---
name: mysql-mcp
description: Use when querying this MySQL MCP service and handling approval pending/retry with stable request_id.
---

# MySQL MCP Query Skill

Wrapper around `query_table` calls with approval retry flow, so callers do not need to implement polling manually each time.

## When To Use

- You need to call `query_table`.
- You need to handle approval state transitions (`pending -> approved/rejected`).
- You need stable `request_id` reuse across retries.

## Usage

1. Set environment variables:
   - `MYSQL_MCP_URL`, default `http://127.0.0.1:9090/mcp`
   - `MYSQL_MCP_TOKEN`, required (Bearer token)
   - Server config is recommended to be managed with `mysql-mcp config init/set`; `config set` only accepts runtime-whitelisted keys.
2. Use the unified entry script:

```bash
./skills/mysql-mcp/scripts/mcp_tools.sh list_tables

./skills/mysql-mcp/scripts/mcp_tools.sh list_tables --source audit

./skills/mysql-mcp/scripts/mcp_tools.sh describe_table --source audit --table users

./skills/mysql-mcp/scripts/mcp_tools.sh query_table \
  --source audit \
  --table users \
  --filters '{"id":1}' \
  --limit 20
```

3. `query_table` script behavior:
   - Automatically runs `initialize` and reuses `Mcp-Session-Id` (compatible with stateful streamable-http mode).
   - If `--request-id` is omitted on the first call, the server generates and returns one.
   - On `pending`, retries automatically with the same `request_id`.
   - On `reject`, exits immediately (status code `2`).
   - On success, returns final `rows/count/request_id` JSON.

## Key Constraints

- `request_id` must be a top-level argument and must not be placed in `filters`.
- `filters` must be a JSON object (for example `{"id":1}`).

## Arguments

- `--table` required
- `--source` optional (recommended explicitly in multi-source setups)
- `--filters` optional, default `{}`
- `--order-by` optional
- `--order` optional (`asc|desc`)
- `--limit` optional
- `--offset` optional
- `--request-id` optional
- `--poll-interval` optional, default `2` seconds
- `--max-retries` optional, default `60`

For protocol details, see [references/mcp-contract.md](references/mcp-contract.md).

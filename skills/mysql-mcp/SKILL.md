---
name: mysql-mcp
description: Use when querying this MySQL MCP service and handling approval pending/retry with stable request_id.
---

# MySQL MCP Query Skill

用于封装 `query_table` 的调用与审批重试流程，避免每次手写轮询逻辑。

## 触发场景

- 需要调用 `query_table`
- 需要处理审批 `pending -> approve/reject`
- 需要保证 `request_id` 在重试中复用

## 使用方式

1. 设置环境变量
   - `MYSQL_MCP_URL`，默认 `http://127.0.0.1:9090/mcp`
   - `MYSQL_MCP_TOKEN`，必填（Bearer Token）
2. 推荐统一入口脚本：

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

3. 也可使用专用查询脚本：

```bash
./skills/mysql-mcp/scripts/query_table_with_approval.sh \
  --source audit \
  --table users \
  --filters '{"id":1}' \
  --limit 20
```

4. `query_table` 脚本行为
   - 自动执行 `initialize` 并复用 `Mcp-Session-Id`（兼容 stateful streamable-http）
   - 首次若未传 `--request-id`，由服务端生成并回传
   - 收到 `pending` 时自动复用同一个 `request_id` 重试
   - `reject` 直接退出（返回码 `2`）
   - 成功返回最终 `rows/count/request_id` JSON

## 关键约束

- `request_id` 必须是顶层参数，不能放到 `filters`
- `filters` 必须是 JSON object（例如 `{"id":1}`）

## 参数

- `--table` 必填
- `--source` 可选（多数据源场景建议显式传）
- `--filters` 可选，默认 `{}`
- `--order-by` 可选
- `--order` 可选（`asc|desc`）
- `--limit` 可选
- `--offset` 可选
- `--request-id` 可选
- `--poll-interval` 可选，默认 `2` 秒
- `--max-retries` 可选，默认 `60`

更多协议细节见 [references/mcp-contract.md](references/mcp-contract.md)。

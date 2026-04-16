# MySQL MCP 服务

一个基于 Streamable HTTP 的 MySQL MCP 服务，提供只读查询能力，并内置审批与结果脱敏。

English version: see [README.md](README.md).

## 项目出发点

在日常开发和维护中，经常需要核查多个数据源的数据，这类重复且繁琐的工作很适合交给 AI 自动化执行。  
但直接将数据库开放给 AI 风险较高，而预先配置细粒度 ACL 在很多场景下又成本较高。  
本项目采用“`MCP + 严格初核 + 动态免审`”的方式，对 LLM agent 暴露可控的数据查询能力：先严格审批，再对同类低风险请求按策略临时免审，在效率和安全之间做平衡。

## 快速开始

```bash
go run ./cmd/mysql-mcp config init
go run ./cmd/mysql-mcp config set MCP_BEARER_TOKEN replace-with-strong-token
go run ./cmd/mysql-mcp config set MYSQL_DSNS 'user:password@tcp(127.0.0.1:3306)/dbname?parseTime=true&loc=Local'
go run ./cmd/mysql-mcp config set APPROVAL_CALLBACK_SECRET replace-with-hmac-secret
go run ./cmd/mysql-mcp serve
```

查看帮助：

```bash
go run ./cmd/mysql-mcp -h
```

指定 `.env` 路径：

```bash
go run ./cmd/mysql-mcp serve --env-file /path/to/custom.env
```

也可通过 `go install` 安装命令行：

```bash
# 在仓库内
go install ./cmd/mysql-mcp

# 在仓库外按 module 路径安装
go install github.com/xiluoduyu/mysql-mcp/cmd/mysql-mcp@latest
```

说明：

- `serve` 为默认命令（执行 `mysql-mcp` 等价于 `mysql-mcp serve`）。
- 配置文件默认路径为 `~/.mysql-mcp/config.toml`。
- 可通过 `--config` 指定配置文件路径。
- `--env-file` 在 v1 保留用于兼容，后续可能移除。
- 已存在的系统环境变量不会被文件加载覆盖。
- dotenv 多行值规则（兼容模式）：
  - 双引号值支持真实换行和 `\n`。
  - 单引号值或无引号值不支持跨行。

## 接口

- `/mcp`（需要 `Authorization: Bearer <MCP_BEARER_TOKEN>`）
  - `POST`：主要 JSON-RPC 请求通道（工具调用）
  - `GET`：服务端通知的监听通道（SSE）
  - `DELETE`：在有状态 streamable-http 模式下终止会话
- `GET /healthz`（无需鉴权）
- `POST /callbacks/approvals`（审批回调）

回调签名请求头：

- `X-Timestamp`：unix 秒
- `X-Signature`：`hex(hmac_sha256(secret, timestamp + "." + body))`

## 必填配置

- `MCP_BEARER_TOKEN`
- `MYSQL_DSNS`
- `APPROVAL_CALLBACK_SECRET`

默认值：

- `APPROVAL_CLIENT_MODE=local_desktop`
- `MCP_BIND_ADDR=127.0.0.1:9090`
- `STATE_SQLITE_PATH=~/.mysql-mcp/state.db`（未设置 `STATE_SQLITE_PATH` 时）
- `MAX_LIMIT=200`

多数据源配置：

- 单数据源（可省略 name）：`MYSQL_DSNS=<dsn>`，自动映射为 source=`default`。
- 多数据源：`MYSQL_DSNS` 支持 `;` 或换行分隔。
  - 示例（`;`）：`MYSQL_DSNS=core=user:pwd@tcp(127.0.0.1:3306)/core;audit=user:pwd@tcp(127.0.0.1:3306)/audit`
  - 示例（换行）：
    ```env
    MYSQL_DSNS="core=user:pwd@tcp(127.0.0.1:3306)/core
    audit=user:pwd@tcp(127.0.0.1:3306)/audit"
    ```
  - `name` 用于工具调用时的 `source` 选择。
  - source name 仅允许字母、数字、`_`、`-`。

当 `APPROVAL_CLIENT_MODE=http` 时，必须设置：

- `APPROVAL_BASE_URL`

## 工具

- `list_tables`
- `describe_table`
- `query_table`

`query_table` 入参规则：

- 必填：`table`
- 可选：`source`、`filters`、`order_by`、`order`、`limit`、`offset`、`request_id`
- `request_id` 只能是顶层参数
- `filters` 里不能出现 `request_id` 或 `reuqest_id`

`list_tables` / `describe_table` 也支持可选 `source` 参数。

## 审批行为（query_table）

- 首次可不传 `request_id`（服务端会回传）。
- 返回 `pending` 时，必须使用“相同 payload + 相同 request_id”重试。
- 返回 `reject` 时停止并展示 `reason`。
- 审批通过后返回 `rows` 和 `count`。

## 结果脱敏

默认会对字段名包含以下关键词的值脱敏为 `******`：

- `password`
- `token`
- `secret`
- `key`

可通过以下环境变量自定义：

- `MASK_FIELD_KEYWORDS`：逗号分隔关键词
- `MASK_FIELDS`：逗号分隔显式字段（支持 `field` 或 `table.field`）
- `MASK_JSON_FIELDS`：逗号分隔字段，针对该字段值进行 JSON 深度脱敏
  - 支持 `field` 或 `table.field`
  - 同时支持 JSON 字符串和 JSON 类型值（object/array/raw json）

示例：

```bash
MASK_FIELD_KEYWORDS=password,token,secret,key,credential
MASK_FIELDS=api_key,users.phone,orders.bank_card
MASK_JSON_FIELDS=users.profile,orders.payload
```

## 扩展审批形态

审批入口统一由 `approval.Gate` 管控，不同审批形态（HTTP 回调、本地弹窗、自定义模式）都实现同一套客户端接口：

```go
type ApprovalClient interface {
	Submit(ctx context.Context, req SubmitRequest) (string, error)
	GetStatus(ctx context.Context, externalID string) (StatusResult, error)
}
```

`StatusResult` 可返回：

- `Status`：`pending | approved | rejected`
- `Reason`：可选原因说明
- `BypassScope` + `BypassTTL`：可选动态免审批策略

常见扩展方式：

- 对接外部审批服务：
  - `Submit` 将 MCP 请求映射到外部 API，并返回 external id。
  - `GetStatus` 轮询外部审批状态，并映射为 `StatusResult`。
  - 如有回调，可在回调处理时调用 `Gate.ApplyCallbackResult(...)` 主动落库。
- 实现本地/进程内审批器（类似 `local_desktop`）：
  - 在 `Submit` 触发本地 UI/弹窗。
  - 在 `GetStatus` 返回当前缓存的审批状态。
  - 审批通过后可按需返回 bypass scope/ttl。

最后在 `cmd/mysql-mcp/main.go` 中将 `approvalClient` 替换为你的实现即可。

## Agent 封装脚本（可选）

- `skills/mysql-mcp/SKILL.md`
- `skills/mysql-mcp/scripts/mcp_tools.sh`
- `skills/mysql-mcp/scripts/query_table_with_approval.sh`

封装脚本环境变量：

- `MYSQL_MCP_URL`（默认 `http://127.0.0.1:9090/mcp`）
- `MYSQL_MCP_TOKEN`（必填）

封装脚本行为说明：

- 脚本会自动调用 MCP `initialize`，并在后续 `tools/call` 复用 `Mcp-Session-Id`。
- 在有状态 streamable-http 模式下可避免 `Invalid session ID` 报错。

## 测试

```bash
go test ./... -count=1
```

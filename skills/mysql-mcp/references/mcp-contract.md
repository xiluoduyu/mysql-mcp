# MCP Contract (query_table)

本 skill 依赖的 `query_table` 协议要点：

1. 输入参数（顶层）
   - `table` 必填
   - `source` 可选（当服务未配置 default source 时必填）
   - `filters` 可选 object
   - `order_by` / `order` / `limit` / `offset` 可选
   - `request_id` 可选（顶层）

2. 审批响应（未放行）
   - 返回结构示例：
   - `approval.decision = pending|reject`
   - `request_id` 一定存在（服务端会回传）

```json
{
  "request_id": "req-xxxx",
  "approval": {
    "decision": "pending",
    "approval_id": "req-xxxx",
    "reason": ""
  },
  "message": "request requires external approval"
}
```

3. 查询成功响应（已放行）

```json
{
  "request_id": "req-xxxx",
  "source": "audit",
  "table": "users",
  "rows": [],
  "count": 0
}
```

4. 重试规则
   - `pending` 时复用同一个 `request_id`
   - payload（source/table/filters/order/limit/offset）保持一致
   - 推荐 1-3 秒轮询

5. 错误规则
   - `request_id` 出现在 `filters` 会被拒绝
   - `filters` 非 object 会报错

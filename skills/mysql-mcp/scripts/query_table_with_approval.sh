#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  query_table_with_approval.sh --table <table> [options]

Options:
  --table <name>            Required
  --source <name>           Optional MySQL source
  --filters <json>          Default: {}
  --order-by <col>
  --order <asc|desc>
  --limit <n>
  --offset <n>
  --request-id <id>
  --poll-interval <sec>     Default: 2
  --max-retries <n>         Default: 60
  -h, --help

Env:
  MYSQL_MCP_URL             Default: http://127.0.0.1:9090/mcp
  MYSQL_MCP_TOKEN           Required (Bearer token)
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

TABLE=""
SOURCE=""
FILTERS='{}'
ORDER_BY=""
ORDER=""
LIMIT=""
OFFSET=""
REQUEST_ID=""
POLL_INTERVAL=2
MAX_RETRIES=60

while [[ $# -gt 0 ]]; do
  case "$1" in
    --table) TABLE="${2:-}"; shift 2 ;;
    --source) SOURCE="${2:-}"; shift 2 ;;
    --filters) FILTERS="${2:-}"; shift 2 ;;
    --order-by) ORDER_BY="${2:-}"; shift 2 ;;
    --order) ORDER="${2:-}"; shift 2 ;;
    --limit) LIMIT="${2:-}"; shift 2 ;;
    --offset) OFFSET="${2:-}"; shift 2 ;;
    --request-id) REQUEST_ID="${2:-}"; shift 2 ;;
    --poll-interval) POLL_INTERVAL="${2:-}"; shift 2 ;;
    --max-retries) MAX_RETRIES="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "$TABLE" ]]; then
  echo "--table is required" >&2
  usage
  exit 1
fi

MYSQL_MCP_URL="${MYSQL_MCP_URL:-http://127.0.0.1:9090/mcp}"
MYSQL_MCP_TOKEN="${MYSQL_MCP_TOKEN:-}"
if [[ -z "$MYSQL_MCP_TOKEN" ]]; then
  echo "MYSQL_MCP_TOKEN is required" >&2
  exit 1
fi

require_cmd curl
require_cmd jq

MYSQL_MCP_SESSION_ID=""

if ! echo "$FILTERS" | jq -e 'type == "object" and (has("request_id") | not) and (has("reuqest_id") | not)' >/dev/null; then
  echo "--filters must be a JSON object and must not contain request_id/reuqest_id" >&2
  exit 1
fi

build_args() {
  jq -n \
    --arg table "$TABLE" \
    --arg source "$SOURCE" \
    --argjson filters "$FILTERS" \
    --arg orderBy "$ORDER_BY" \
    --arg order "$ORDER" \
    --arg limit "$LIMIT" \
    --arg offset "$OFFSET" \
    --arg requestId "$REQUEST_ID" '
    {
      table: $table
    }
    + (if $source != "" then {source: $source} else {} end)
    + {
      filters: $filters
    }
    + (if $orderBy != "" then {order_by: $orderBy} else {} end)
    + (if $order != "" then {order: $order} else {} end)
    + (if $limit != "" then {limit: ($limit|tonumber)} else {} end)
    + (if $offset != "" then {offset: ($offset|tonumber)} else {} end)
    + (if $requestId != "" then {request_id: $requestId} else {} end)
  '
}

ensure_session_id() {
  if [[ -n "${MYSQL_MCP_SESSION_ID:-}" ]]; then
    return 0
  fi

  local init_req init_resp
  init_req='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"mysql-mcp-skill","version":"1.0"}}}'
  init_resp="$(curl -sS -i --max-time 15 \
    -H "Authorization: Bearer $MYSQL_MCP_TOKEN" \
    -H "Content-Type: application/json" \
    -d "$init_req" \
    "$MYSQL_MCP_URL")"

  MYSQL_MCP_SESSION_ID="$(echo "$init_resp" | awk -F': ' '/^Mcp-Session-Id:/{print $2}' | tr -d '\r')"
  if [[ -z "$MYSQL_MCP_SESSION_ID" ]]; then
    echo "failed to initialize MCP session (header Mcp-Session-Id missing)" >&2
    echo "$init_resp" | sed -n '1,80p' >&2
    exit 1
  fi
}

rpc_call() {
  local method="$1"
  local params_json="$2"
  local req_json
  req_json="$(jq -n --arg m "$method" --argjson p "$params_json" \
    '{jsonrpc:"2.0",id:1,method:$m,params:$p}')"

  local resp body status
  resp="$(curl -sS -i --max-time 30 \
    -H "Authorization: Bearer $MYSQL_MCP_TOKEN" \
    -H "Mcp-Session-Id: $MYSQL_MCP_SESSION_ID" \
    -H "Content-Type: application/json" \
    -d "$req_json" \
    "$MYSQL_MCP_URL")"
  body="$(echo "$resp" | sed -n '/^\r$/,$p' | sed '1d')"
  status="$(echo "$resp" | awk 'NR==1 {print $2}')"

  if [[ -z "$status" ]]; then
    echo "request failed: missing HTTP status" >&2
    echo "$resp" | sed -n '1,80p' >&2
    exit 1
  fi

  if [[ "$status" -lt 200 || "$status" -ge 300 ]]; then
    echo "request failed: HTTP $status" >&2
    echo "$body" | sed -n '1,80p' >&2
    exit 1
  fi

  if ! echo "$body" | jq -e . >/dev/null 2>&1; then
    echo "request failed: response is not valid JSON" >&2
    echo "$body" | sed -n '1,80p' >&2
    exit 1
  fi

  echo "$body"
}

call_once() {
  local args_json
  args_json="$(build_args)"
  rpc_call "tools/call" "$(jq -n --argjson a "$args_json" '{name:"query_table",arguments:$a}')"
}

extract_content_json() {
  jq -cer '
    if .error then
      {__rpc_error__: .error}
    else
      (
        .result.structuredContent
        // (
          (.result.content // [])
          | map(select(.type=="text") | .text | fromjson? | select(. != null))
          | .[0]
        )
      ) // {}
    end
  '
}

retry=0
ensure_session_id
while true; do
  raw="$(call_once)"
  body="$(echo "$raw" | extract_content_json)"

  if echo "$body" | jq -e 'has("__rpc_error__")' >/dev/null; then
    echo "$body" | jq .
    exit 1
  fi

  new_request_id="$(echo "$body" | jq -r '.request_id // empty')"
  if [[ -n "$new_request_id" ]]; then
    REQUEST_ID="$new_request_id"
  fi

  decision="$(echo "$body" | jq -r '.approval.decision // empty')"
  case "$decision" in
    "")
      echo "$body" | jq .
      exit 0
      ;;
    allow|approved)
      echo "$body" | jq .
      exit 0
      ;;
    pending)
      retry=$((retry + 1))
      if (( retry > MAX_RETRIES )); then
        echo "$body" | jq .
        echo "exceeded max retries: $MAX_RETRIES" >&2
        exit 3
      fi
      sleep "$POLL_INTERVAL"
      ;;
    reject|rejected)
      echo "$body" | jq .
      exit 2
      ;;
    *)
      echo "$body" | jq .
      echo "unknown approval decision: $decision" >&2
      exit 1
      ;;
  esac
done

#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  mcp_tools.sh <command> [options]

Commands:
  list_tables [--source <name>]
  describe_table --table <name> [--source <name>]
  query_table --table <name> [query options]

Query options:
  --source <name>           Optional MySQL source
  --filters <json>          Default: {}
  --order-by <col>
  --order <asc|desc>
  --limit <n>
  --offset <n>
  --request-id <id>
  --poll-interval <sec>     Default: 2
  --max-retries <n>         Default: 60

Global env:
  MCP_URL                   Default: http://127.0.0.1:9090/mcp
  MYSQL_MCP_TOKEN           Required (Bearer token)
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

rpc_call() {
  local tool_name="$1"
  local args_json="$2"
  local req_json
  req_json="$(jq -n --arg name "$tool_name" --argjson a "$args_json" \
    '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:$name,arguments:$a}}')"

  curl -sS "$MCP_URL" \
    -H "Authorization: Bearer $MYSQL_MCP_TOKEN" \
    -H "Content-Type: application/json" \
    -d "$req_json"
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
          | map(select(.type=="text") | (.text as $t | (try fromjson catch {"text": $t})))
          | .[0]
        )
      ) // {}
    end
  '
}

handle_list_tables() {
  local source=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --source) source="${2:-}"; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) echo "unknown argument for list_tables: $1" >&2; usage; exit 1 ;;
    esac
  done
  local args='{}'
  if [[ -n "$source" ]]; then
    args="$(jq -n --arg source "$source" '{source:$source}')"
  fi
  raw="$(rpc_call "list_tables" "$args")"
  body="$(echo "$raw" | extract_content_json)"
  if echo "$body" | jq -e 'has("__rpc_error__")' >/dev/null; then
    echo "$body" | jq .
    exit 1
  fi
  echo "$body" | jq .
}

handle_describe_table() {
  local table=""
  local source=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --table) table="${2:-}"; shift 2 ;;
      --source) source="${2:-}"; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) echo "unknown argument for describe_table: $1" >&2; usage; exit 1 ;;
    esac
  done
  if [[ -z "$table" ]]; then
    echo "--table is required for describe_table" >&2
    exit 1
  fi

  raw="$(rpc_call "describe_table" "$(jq -n --arg table "$table" --arg source "$source" \
    '{table:$table} + (if $source != "" then {source:$source} else {} end)')")"
  body="$(echo "$raw" | extract_content_json)"
  if echo "$body" | jq -e 'has("__rpc_error__")' >/dev/null; then
    echo "$body" | jq .
    exit 1
  fi
  echo "$body" | jq .
}

handle_query_table() {
  local table=""
  local source=""
  local filters='{}'
  local order_by=""
  local order=""
  local limit=""
  local offset=""
  local request_id=""
  local poll_interval=2
  local max_retries=60

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --table) table="${2:-}"; shift 2 ;;
      --source) source="${2:-}"; shift 2 ;;
      --filters) filters="${2:-}"; shift 2 ;;
      --order-by) order_by="${2:-}"; shift 2 ;;
      --order) order="${2:-}"; shift 2 ;;
      --limit) limit="${2:-}"; shift 2 ;;
      --offset) offset="${2:-}"; shift 2 ;;
      --request-id) request_id="${2:-}"; shift 2 ;;
      --poll-interval) poll_interval="${2:-}"; shift 2 ;;
      --max-retries) max_retries="${2:-}"; shift 2 ;;
      -h|--help) usage; exit 0 ;;
      *) echo "unknown argument for query_table: $1" >&2; usage; exit 1 ;;
    esac
  done

  if [[ -z "$table" ]]; then
    echo "--table is required for query_table" >&2
    exit 1
  fi

  if ! echo "$filters" | jq -e \
    'type == "object" and (has("request_id") | not) and (has("reuqest_id") | not)' >/dev/null; then
    echo "--filters must be JSON object and must not include request_id/reuqest_id" >&2
    exit 1
  fi

  local retry=0
  while true; do
    args_json="$(jq -n \
      --arg table "$table" \
      --arg source "$source" \
      --argjson filters "$filters" \
      --arg orderBy "$order_by" \
      --arg order "$order" \
      --arg limit "$limit" \
      --arg offset "$offset" \
      --arg requestId "$request_id" '
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
    ')"

    raw="$(rpc_call "query_table" "$args_json")"
    body="$(echo "$raw" | extract_content_json)"

    if echo "$body" | jq -e 'has("__rpc_error__")' >/dev/null; then
      echo "$body" | jq .
      exit 1
    fi

    new_request_id="$(echo "$body" | jq -r '.request_id // empty')"
    if [[ -n "$new_request_id" ]]; then
      request_id="$new_request_id"
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
        if (( retry > max_retries )); then
          echo "$body" | jq .
          echo "exceeded max retries: $max_retries" >&2
          exit 3
        fi
        sleep "$poll_interval"
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
}

main() {
  if [[ $# -lt 1 ]]; then
    usage
    exit 1
  fi

  case "${1:-}" in
    -h|--help|help)
      usage
      exit 0
      ;;
  esac

  MCP_URL="${MCP_URL:-http://127.0.0.1:9090/mcp}"
  MYSQL_MCP_TOKEN="${MYSQL_MCP_TOKEN:-}"
  if [[ -z "$MYSQL_MCP_TOKEN" ]]; then
    echo "MYSQL_MCP_TOKEN is required" >&2
    exit 1
  fi

  require_cmd curl
  require_cmd jq

  cmd="$1"
  shift
  case "$cmd" in
    list_tables) handle_list_tables "$@" ;;
    describe_table) handle_describe_table "$@" ;;
    query_table) handle_query_table "$@" ;;
    *) echo "unknown command: $cmd" >&2; usage; exit 1 ;;
  esac
}

main "$@"

#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"
REQUESTS="${REQUESTS:-12}"
CONCURRENCY="${CONCURRENCY:-3}"
QUERY="${QUERY:-最近有哪些网络热词？}"
UPLOAD_FILE="${UPLOAD_FILE:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPORT_PATH="${REPO_ROOT}/tmp/bench-report.ndjson"
cd "$REPO_ROOT"

mkdir -p "${REPO_ROOT}/tmp"
: > "$REPORT_PATH"

run_bench() {
  local title="$1"
  shift
  echo
  echo "=== ${title} ==="
  go run ./cmd/loadtest "$@" -save "$REPORT_PATH"
}

run_bench "检索接口 /api/search" \
  -scenario search \
  -base-url "$BASE_URL" \
  -requests "$REQUESTS" \
  -concurrency "$CONCURRENCY" \
  -query "$QUERY"

for mode in rag react rag_agent multi-agent graph_rag graph_multi; do
  run_bench "问答接口 /api/query [${mode}]" \
    -scenario query \
    -base-url "$BASE_URL" \
    -mode "$mode" \
    -requests "$REQUESTS" \
    -concurrency "$CONCURRENCY" \
    -query "$QUERY"
done

if [[ -z "$UPLOAD_FILE" ]]; then
  UPLOAD_FILE="$(find "$REPO_ROOT" -type f \( -name '*.md' -o -name '*.markdown' \) | head -n 1 || true)"
fi

if [[ -n "$UPLOAD_FILE" ]]; then
  run_bench "上传接口 /api/upload [${UPLOAD_FILE}]" \
    -scenario upload \
    -base-url "$BASE_URL" \
    -requests 6 \
    -concurrency 2 \
    -file "$UPLOAD_FILE"
else
  echo
  echo "跳过上传压测：没找到可上传的 Markdown 文件。"
fi

echo
echo "=== 压测总榜 ==="
go run ./cmd/benchreport -input "$REPORT_PATH"
echo
echo "报告已保存：$REPORT_PATH"

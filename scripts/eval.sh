#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"
DATASET="${DATASET:-testdata/agent_eval_cases.json}"
MODES="${MODES:-rag,rag_agent,multi-agent,graph_multi}"
TIMEOUT="${TIMEOUT:-90s}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPORT_PATH="${REPO_ROOT}/tmp/agent-eval-report.json"
cd "$REPO_ROOT"

export GOTELEMETRY="off"
export GOPATH="${REPO_ROOT}/tmp/.gopath"
export GOMODCACHE="${GOPATH}/pkg/mod"
export GOCACHE="${REPO_ROOT}/tmp/.gocache"
mkdir -p "${REPO_ROOT}/tmp"

echo "=== 执行 agent 评测 ==="
go run ./cmd/agenteval \
  -base-url "$BASE_URL" \
  -dataset "$DATASET" \
  -modes "$MODES" \
  -timeout "$TIMEOUT" \
  -save "$REPORT_PATH"

echo
echo "=== 输出评测总榜 ==="
go run ./cmd/agentevalreport -input "$REPORT_PATH"
echo
echo "报告已保存：$REPORT_PATH"
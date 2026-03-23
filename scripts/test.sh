#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

export GOPATH="${REPO_ROOT}/tmp/.gopath"
export GOMODCACHE="${GOPATH}/pkg/mod"
export GOCACHE="${REPO_ROOT}/tmp/.gocache"

mkdir -p "${GOPATH}" "${GOMODCACHE}" "${GOCACHE}"

echo "GOPATH=${GOPATH}"
echo "GOMODCACHE=${GOMODCACHE}"
echo "GOCACHE=${GOCACHE}"

cd "${REPO_ROOT}"
go test ./...
go vet ./...

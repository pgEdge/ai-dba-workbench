#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench - E2E local runner
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# Brings up Postgres in Docker, builds the server with coverage
# instrumentation, builds the client, starts the stack via
# start-stack.sh, runs the Playwright suite, then tears everything
# down. Set E2E_KEEP_STACK=1 to skip teardown for debugging.
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
E2E_DIR="${REPO_ROOT}/e2e"
RUN_DIR="${E2E_DIR}/.runtime"
PG_CONTAINER="ai-dba-e2e-postgres"

export E2E_RUN_DIR="${RUN_DIR}"
export E2E_SERVER_BIN="${REPO_ROOT}/bin/ai-dba-server"
export E2E_COLLECTOR_BIN="${REPO_ROOT}/bin/ai-dba-collector"
export E2E_CLIENT_DIR="${REPO_ROOT}/client"
export E2E_DB_HOST="${E2E_DB_HOST:-127.0.0.1}"
export E2E_DB_PORT="${E2E_DB_PORT:-55432}"
export E2E_DB_NAME="${E2E_DB_NAME:-postgres}"
export E2E_DB_USER="${E2E_DB_USER:-postgres}"
export E2E_DB_PASSWORD="${E2E_DB_PASSWORD:-postgres}"
export E2E_SERVER_PORT="${E2E_SERVER_PORT:-8080}"
export E2E_SERVER_ADDR="${E2E_SERVER_ADDR:-:${E2E_SERVER_PORT}}"
export E2E_PREVIEW_PORT="${E2E_PREVIEW_PORT:-4173}"
export E2E_ADMIN_USERNAME="${E2E_ADMIN_USERNAME:-e2e-admin}"
export E2E_ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD:-e2e-admin-password-please-change}"

mkdir -p "${RUN_DIR}"

cleanup() {
    if [ "${E2E_KEEP_STACK:-0}" = "1" ]; then
        echo ""
        echo "E2E_KEEP_STACK=1; leaving stack running."
        echo "  Server log: ${RUN_DIR}/logs/server.log"
        echo "  Preview log: ${RUN_DIR}/logs/preview.log"
        echo "  Postgres container: ${PG_CONTAINER}"
        echo "  Tear down with: ${E2E_DIR}/scripts/stop-stack.sh && docker rm -f ${PG_CONTAINER}"
        return
    fi
    echo "==> Tearing down stack"
    "${E2E_DIR}/scripts/stop-stack.sh" || true
    docker rm -f "${PG_CONTAINER}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> Starting Postgres container"
docker run --rm -d \
    --name "${PG_CONTAINER}" \
    -e POSTGRES_PASSWORD="${E2E_DB_PASSWORD}" \
    -p "${E2E_DB_PORT}:5432" \
    postgres:16 >/dev/null

echo "==> Building server with coverage"
(
    cd "${REPO_ROOT}/server/src"
    # `-coverpkg=./...` instruments every package in the server module,
    # not just `cmd/mcp-server`. Without it the integration coverage
    # signal is near-zero because handler packages such as internal/api
    # are not in the default instrumentation set.
    go build -cover -covermode=atomic -coverpkg=./... \
        -o "${E2E_SERVER_BIN}" \
        ./cmd/mcp-server
)

echo "==> Building collector (for datastore migrations)"
(
    cd "${REPO_ROOT}/collector/src"
    go build -o "${E2E_COLLECTOR_BIN}" .
)

echo "==> Building client"
(
    cd "${E2E_CLIENT_DIR}"
    npm run build
)

echo "==> Starting stack"
"${E2E_DIR}/scripts/start-stack.sh"

echo "==> Running Playwright suite"
(
    cd "${E2E_DIR}"
    npx playwright test "$@"
)

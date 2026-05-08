#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench - E2E stack starter
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# Starts the server (with Go integration coverage) and `vite preview`
# in the background, after Postgres is reachable and a config has
# been rendered. Writes PIDs to E2E_RUN_DIR for stop-stack.sh.
#
set -euo pipefail

: "${E2E_RUN_DIR:?must be set}"
: "${E2E_SERVER_BIN:?must be set}"
: "${E2E_CLIENT_DIR:?must be set}"
: "${E2E_DB_HOST:?must be set}"
: "${E2E_DB_PORT:?must be set}"
: "${E2E_DB_USER:?must be set}"
: "${E2E_SERVER_PORT:?must be set}"
: "${E2E_PREVIEW_PORT:?must be set}"
: "${E2E_ADMIN_USERNAME:?must be set}"
: "${E2E_ADMIN_PASSWORD:?must be set}"

# GOCOVERDIR is required for `go build -cover` binaries; if absent,
# the binary panics on startup. Default to a runtime subdir so local
# runs do not need to pre-export the variable.
export GOCOVERDIR="${GOCOVERDIR:-${E2E_RUN_DIR}/cov}"
mkdir -p "${E2E_RUN_DIR}" "${GOCOVERDIR}"

LOG_DIR="${E2E_RUN_DIR}/logs"
mkdir -p "${LOG_DIR}"

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> Waiting for Postgres at ${E2E_DB_HOST}:${E2E_DB_PORT}"
"${SCRIPTS_DIR}/wait-for-postgres.sh" \
    "${E2E_DB_HOST}" "${E2E_DB_PORT}" "${E2E_DB_USER}"

echo "==> Rendering server config"
"${SCRIPTS_DIR}/render-config.sh"

echo "==> Starting server (binary: ${E2E_SERVER_BIN})"
"${E2E_SERVER_BIN}" \
    --config="${E2E_RUN_DIR}/ai-dba-server.yaml" \
    > "${LOG_DIR}/server.log" 2>&1 &
SERVER_PID=$!
echo "${SERVER_PID}" > "${E2E_RUN_DIR}/server.pid"
echo "Server PID: ${SERVER_PID}"

echo "==> Waiting for server /health"
"${SCRIPTS_DIR}/wait-for-http.sh" \
    "http://127.0.0.1:${E2E_SERVER_PORT}/health" 30

echo "==> Bootstrapping admin user"
"${SCRIPTS_DIR}/bootstrap-admin.sh"

echo "==> Starting vite preview from ${E2E_CLIENT_DIR}"
(
    cd "${E2E_CLIENT_DIR}"
    E2E_SERVER_URL="http://127.0.0.1:${E2E_SERVER_PORT}" \
    npx vite preview \
        --host 127.0.0.1 \
        --port "${E2E_PREVIEW_PORT}" \
        --strictPort \
        > "${LOG_DIR}/preview.log" 2>&1 &
    echo $! > "${E2E_RUN_DIR}/preview.pid"
)
PREVIEW_PID=$(cat "${E2E_RUN_DIR}/preview.pid")
echo "Preview PID: ${PREVIEW_PID}"

echo "==> Waiting for vite preview"
"${SCRIPTS_DIR}/wait-for-http.sh" \
    "http://127.0.0.1:${E2E_PREVIEW_PORT}/" 30

echo "STACK_READY"

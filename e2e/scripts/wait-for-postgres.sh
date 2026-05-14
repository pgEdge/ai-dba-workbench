#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench - E2E Postgres readiness probe
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# Polls Postgres with pg_isready until ready, with a configurable
# timeout. Used by start-stack.sh and CI.
#
set -euo pipefail

HOST="${1:-127.0.0.1}"
PORT="${2:-5432}"
USER="${3:-postgres}"
TIMEOUT="${E2E_PG_TIMEOUT:-30}"

echo "Waiting up to ${TIMEOUT}s for Postgres on ${HOST}:${PORT}..."

for i in $(seq 1 "${TIMEOUT}"); do
    if pg_isready -h "${HOST}" -p "${PORT}" -U "${USER}" -q; then
        echo "Postgres ready after ${i}s."
        exit 0
    fi
    sleep 1
done

echo "ERROR: Postgres did not become ready within ${TIMEOUT}s." >&2
exit 1

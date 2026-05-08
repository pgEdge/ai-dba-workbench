#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench - E2E HTTP readiness probe
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# Polls a URL until it returns a 2xx/3xx response, with a configurable
# timeout. Used to wait for the server's /health endpoint and the
# vite preview root.
#
set -euo pipefail

URL="${1:?usage: wait-for-http.sh URL [TIMEOUT]}"
TIMEOUT="${2:-30}"

echo "Waiting up to ${TIMEOUT}s for ${URL}..."

for i in $(seq 1 "${TIMEOUT}"); do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "${URL}" || true)
    if [[ "${code}" =~ ^[23][0-9][0-9]$ ]]; then
        echo "URL ${URL} ready after ${i}s (HTTP ${code})."
        exit 0
    fi
    sleep 1
done

echo "ERROR: ${URL} did not become ready within ${TIMEOUT}s (last HTTP code: ${code:-none})." >&2
exit 1

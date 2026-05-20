#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# rpm-preflight.sh — Network connectivity preflight check.
#
# Validates DNS resolution and TCP/HTTP reachability of all supporting
# containers from inside the RPM workbench container. Runs automatically
# at container startup (via rpm-entrypoint.sh) and on demand via
# docker compose exec.
#
# Exit codes:
#   0 — all checks passed
#   1 — one or more checks failed
#

set -uo pipefail

overall=0

# Print a labelled PASS or FAIL line and update the exit code.
check() {
    local label="$1"
    shift
    printf '[preflight] %-52s ' "${label}..."
    if "$@" > /dev/null 2>&1; then
        echo "PASS"
    else
        echo "FAIL"
        overall=1
    fi
}

echo "[preflight] ================================================="
echo "[preflight] RPM Container Network Connectivity Check"
echo "[preflight] ================================================="

# --- DNS resolution ---
check "DNS: postgres"  getent hosts postgres
check "DNS: mailpit"   getent hosts mailpit
check "DNS: wiremock"  getent hosts wiremock

# --- TCP connectivity ---
# Uses bash built-in /dev/tcp — no netcat required.
check "TCP: postgres:5432" \
    timeout 5 bash -c 'cat < /dev/null > /dev/tcp/postgres/5432'

check "TCP: mailpit:1025 (SMTP)" \
    timeout 5 bash -c 'cat < /dev/null > /dev/tcp/mailpit/1025'

# --- HTTP connectivity ---
check "HTTP: mailpit:8025/api/v1/messages" \
    curl -sf --connect-timeout 5 http://mailpit:8025/api/v1/messages

check "HTTP: wiremock:8080/__admin/requests" \
    curl -sf --connect-timeout 5 http://wiremock:8080/__admin/requests

echo "[preflight] ================================================="
if [ "${overall}" -eq 0 ]; then
    echo "[preflight] All connectivity checks PASSED."
else
    echo "[preflight] One or more connectivity checks FAILED."
    echo "[preflight] Verify all services are on e2e-network and healthy."
fi
echo "[preflight] ================================================="

exit "${overall}"

#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench - E2E admin bootstrap
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# Creates the E2E admin user and grants superuser status. Uses the
# existing add-user and set-superuser CLI subcommands. Idempotent on
# re-runs: list-users is consulted first, and missing-only paths are
# taken.
#
set -euo pipefail

: "${E2E_SERVER_BIN:?must be set}"
: "${E2E_RUN_DIR:?must be set}"
: "${E2E_ADMIN_USERNAME:?must be set}"
: "${E2E_ADMIN_PASSWORD:?must be set}"

CONFIG="${E2E_RUN_DIR}/ai-dba-server.yaml"

# list-users prints "username    ..." rows; grep with -w to avoid
# matching substrings.
existing=$("${E2E_SERVER_BIN}" --config="${CONFIG}" -list-users 2>/dev/null \
    | awk 'NR>1 {print $1}' \
    | grep -wx "${E2E_ADMIN_USERNAME}" \
    || true)

if [ -z "${existing}" ]; then
    echo "Creating admin user: ${E2E_ADMIN_USERNAME}"
    "${E2E_SERVER_BIN}" --config="${CONFIG}" \
        -add-user \
        -username "${E2E_ADMIN_USERNAME}" \
        -password "${E2E_ADMIN_PASSWORD}" \
        -user-note "E2E test admin"
else
    echo "Admin user ${E2E_ADMIN_USERNAME} already exists; skipping create."
fi

echo "Granting superuser to ${E2E_ADMIN_USERNAME}"
"${E2E_SERVER_BIN}" --config="${CONFIG}" \
    -set-superuser \
    -username "${E2E_ADMIN_USERNAME}"

echo "Admin bootstrap complete."

#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench - E2E stack stopper
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# Stops the server and vite preview. SIGTERM first so the server
# (built with -cover) flushes coverage; SIGKILL only after a 10s
# grace period.
#
set -euo pipefail

: "${E2E_RUN_DIR:?must be set}"

stop_pid() {
    local label="$1"
    local pid_file="$2"
    if [ ! -f "${pid_file}" ]; then
        return 0
    fi
    local pid
    pid=$(cat "${pid_file}")
    if ! kill -0 "${pid}" 2>/dev/null; then
        rm -f "${pid_file}"
        return 0
    fi
    echo "==> Stopping ${label} (PID ${pid})"
    kill -TERM "${pid}" 2>/dev/null || true
    for _ in $(seq 1 10); do
        if ! kill -0 "${pid}" 2>/dev/null; then
            rm -f "${pid_file}"
            return 0
        fi
        sleep 1
    done
    echo "==> ${label} did not exit on SIGTERM; sending SIGKILL"
    kill -KILL "${pid}" 2>/dev/null || true
    rm -f "${pid_file}"
}

stop_pid "vite preview" "${E2E_RUN_DIR}/preview.pid"
stop_pid "server" "${E2E_RUN_DIR}/server.pid"

echo "STACK_STOPPED"

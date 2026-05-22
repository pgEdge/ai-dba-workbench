#!/usr/bin/env bash
#
# stop-replication.sh
#
# Stops all three replication PostgreSQL instances. Called via
# docker exec by ReplicationHelper.stop() in E2E tests.
#

set -euo pipefail

# ---------------------------------------------------------------------------
# Portable privilege-drop helper.  Tries gosu (Debian/official postgres
# image), su-exec (Alpine), runuser (util-linux), then POSIX su.
# Usage:  _pg <cmd> [args...]
#         PGPASSWORD=secret _pg pg_basebackup ...
# ---------------------------------------------------------------------------
if command -v gosu > /dev/null 2>&1; then
    _pg() { gosu postgres "$@"; }
elif command -v su-exec > /dev/null 2>&1; then
    _pg() { su-exec postgres "$@"; }
elif command -v runuser > /dev/null 2>&1; then
    _pg() { runuser -u postgres -- "$@"; }
else
    _pg() {
        local cmd
        cmd="$(printf '%q ' "$@")"
        if [ -n "${PGPASSWORD:-}" ]; then
            su postgres -s /bin/bash -c "export PGPASSWORD=$(printf '%q' "${PGPASSWORD}"); ${cmd}"
        else
            su postgres -s /bin/bash -c "${cmd}"
        fi
    }
fi

echo "[replication] Stopping standby2 (port 5442)..."
_pg pg_ctl -D /data/repl-standby2 stop -m fast 2>/dev/null || true

echo "[replication] Stopping standby1 (port 5441)..."
_pg pg_ctl -D /data/repl-standby1 stop -m fast 2>/dev/null || true

echo "[replication] Stopping primary (port 5440)..."
_pg pg_ctl -D /data/repl-primary stop -m fast 2>/dev/null || true

echo "[replication] All replication instances stopped."

#!/usr/bin/env bash
#
# stop-replication.sh
#
# Stops all three replication PostgreSQL instances AND removes their data
# directories for a clean-slate, disk-freeing shutdown. Called via docker
# exec by ReplicationHelper.forceStop() at the end of the physical phase, so
# both RAM/CPU (the running instances) and disk (the data dirs) are released
# before the logical phase runs. start-replication.sh re-creates the
# instances from scratch (initdb) on the next physical run.
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

# Remove the data directories to reclaim disk (clean-slate shutdown). Only the
# three replication dirs under /data are removed; the container's default
# PGDATA (the ai_workbench app datastore) is untouched. This is safe because
# the instances above are already stopped, and start-replication.sh re-inits
# these dirs from scratch on the next physical run.
echo "[replication] Removing replication data directories to free disk..."
rm -rf /data/repl-primary /data/repl-standby1 /data/repl-standby2
echo "[replication] Replication data directories removed."

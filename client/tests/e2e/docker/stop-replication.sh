#!/usr/bin/env bash
#
# stop-replication.sh
#
# Stops all three replication PostgreSQL instances. Called via
# docker exec by ReplicationHelper.stop() in E2E tests.
#

set -euo pipefail

echo "[replication] Stopping standby2 (port 5442)..."
gosu postgres pg_ctl -D /data/repl-standby2 stop -m fast 2>/dev/null || true

echo "[replication] Stopping standby1 (port 5441)..."
gosu postgres pg_ctl -D /data/repl-standby1 stop -m fast 2>/dev/null || true

echo "[replication] Stopping primary (port 5440)..."
gosu postgres pg_ctl -D /data/repl-primary stop -m fast 2>/dev/null || true

echo "[replication] All replication instances stopped."

#!/usr/bin/env bash
#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------
#
# Minimal container entrypoint for the RPM-based PostgreSQL 19 image
# (Dockerfile.pg19). rockylinux has no postgres docker-entrypoint, so this
# replicates just what the E2E stack needs: first-run initdb into PGDATA,
# password + host auth, create POSTGRES_DB, then exec `postgres` (as the
# postgres user, PID 1 via setpriv) with the `-c` flags passed by compose.
#
# Absolute tool paths are used throughout: runuser lives in /usr/sbin and the
# postgres binaries in /usr/pgsql-19/bin, neither guaranteed on PATH.
set -Eeuo pipefail

PGBIN="/usr/pgsql-19/bin"
PGDATA="${PGDATA:-/var/lib/pgsql/19/data}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"

# The compose command is `postgres -c ...`; drop the leading program name so
# the remaining args are the `-c` flags appended to our own `postgres` exec.
if [ "${1:-}" = "postgres" ]; then
    shift
fi

# Run a command as the postgres user (runuser passes argv directly — no shell
# re-quoting). Absolute path because /usr/sbin may not be on PATH.
as_pg() { /usr/sbin/runuser -u postgres -- "$@"; }

if [ ! -s "${PGDATA}/PG_VERSION" ]; then
    mkdir -p "${PGDATA}"
    chown -R postgres:postgres "$(dirname "${PGDATA}")" "${PGDATA}"
    chmod 700 "${PGDATA}"

    pwfile="$(mktemp)"
    printf '%s' "${POSTGRES_PASSWORD}" > "${pwfile}"
    chown postgres "${pwfile}"
    as_pg "${PGBIN}/initdb" -D "${PGDATA}" -U "${POSTGRES_USER}" \
        --auth-local=trust --auth-host=scram-sha-256 --pwfile="${pwfile}"
    rm -f "${pwfile}"

    echo "listen_addresses = '*'" >> "${PGDATA}/postgresql.conf"
    # initdb defaults max_connections to 100. In RPM mode this one instance
    # is dual-role — the workbench's ai_workbench metadata store AND a
    # monitored database — so the server/collector/alerter pools plus every
    # monitoring connection share its slots and exhaust them under the full
    # suite ("remaining connection slots are reserved for ... SUPERUSER",
    # SQLSTATE 53300). The pgEdge standard 16-18 images ship a higher ceiling;
    # match that so the store does not run dry.
    echo "max_connections = 300" >> "${PGDATA}/postgresql.conf"
    echo "host all all all scram-sha-256" >> "${PGDATA}/pg_hba.conf"

    # Bring the server up on the unix socket only to create the app database.
    as_pg "${PGBIN}/pg_ctl" -D "${PGDATA}" -o "-c listen_addresses=''" -w start
    if [ "${POSTGRES_DB}" != "postgres" ]; then
        as_pg "${PGBIN}/psql" -v ON_ERROR_STOP=1 -U "${POSTGRES_USER}" \
            -d postgres -c "CREATE DATABASE \"${POSTGRES_DB}\""
    fi
    as_pg "${PGBIN}/pg_ctl" -D "${PGDATA}" -m fast -w stop
fi

# Exec postgres as the postgres user; setpriv execs (no fork) so signals from
# `docker stop` reach postgres directly for a clean shutdown.
exec /usr/bin/setpriv --reuid=postgres --regid=postgres --init-groups \
    "${PGBIN}/postgres" -D "${PGDATA}" "$@"

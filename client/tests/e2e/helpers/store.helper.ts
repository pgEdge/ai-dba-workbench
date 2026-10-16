/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { exec } from 'child_process';
import { promisify } from 'util';

const execAsync = promisify(exec);

/**
 * StoreHelper reads the Workbench metrics store (the `ai_workbench`
 * database) directly, providing the "ground truth" against which the
 * GUI and the REST API responses are compared in the cluster stats
 * verification spec.
 *
 * The E2E package ships no PostgreSQL client, so queries run through
 * `docker exec psql` against the postgres container — the same
 * container-discovery approach used by ReplicationHelper. The main
 * cluster (`ai_workbench`) is served on port 5432 inside that
 * container.
 */
export class StoreHelper {
    private static readonly EXEC_TIMEOUT_MS = 20_000;

    /**
     * Run a read-only SQL statement against the `ai_workbench` store
     * and return the result rows. Each row is an array of column
     * values as strings (psql `-tA` tuples-only, unaligned output with
     * a tab field separator). An empty result yields an empty array.
     */
    async query(sql: string): Promise<string[][]> {
        const id = await this.getContainerId();
        const password = process.env['POSTGRES_PASSWORD'] ?? 'postgres';

        // Single-quote the SQL for the shell and escape any embedded
        // single quotes. The SQL this helper runs is composed from
        // integer connection IDs and hex markers only, never from
        // free-form user input.
        const quotedSql = `'${sql.replace(/'/g, `'\\''`)}'`;

        // Connect over the local Unix socket (the official postgres
        // image trusts local connections) as the postgres superuser.
        // PGPASSWORD is also supplied so the command still succeeds if
        // the deployment requires a password.
        const cmd =
            `docker exec -e PGPASSWORD='${password}' ${id} ` +
            `psql -U postgres -d ai_workbench -tA -F '\t' -c ${quotedSql}`;

        const { stdout } = await execAsync(cmd, {
            timeout: StoreHelper.EXEC_TIMEOUT_MS,
        });

        const trimmed = stdout.replace(/\n+$/, '');
        if (trimmed === '') {
            return [];
        }
        return trimmed.split('\n').map((line) => line.split('\t'));
    }

    /**
     * Convenience wrapper returning the first cell of the first row,
     * or null when the query produced no rows.
     */
    async queryScalar(sql: string): Promise<string | null> {
        const rows = await this.query(sql);
        if (rows.length === 0 || rows[0].length === 0) {
            return null;
        }
        return rows[0][0];
    }

    /**
     * Run a statement against the replication PRIMARY (port 5440,
     * database `ragdb`) as the postgres superuser via `docker exec
     * psql`. Unlike the MCP `query_database` tool — which runs SQL in a
     * read-only transaction and cannot execute DDL — this path can
     * create the per-run marker table the stats spec uses to identify
     * its workload query in pg_stat_statements (a leading SQL comment is
     * stripped by pg_stat_statements, and a column alias does not change
     * the query id, so a distinct table name is the reliable marker).
     * `stdout` is returned trimmed for callers that need it.
     */
    async execPrimary(sql: string): Promise<string> {
        const id = await this.getContainerId();
        const password = process.env['POSTGRES_PASSWORD'] ?? 'postgres';
        const quotedSql = `'${sql.replace(/'/g, `'\\''`)}'`;
        const cmd =
            `docker exec -e PGPASSWORD='${password}' ${id} ` +
            `psql -U postgres -p 5440 -d ragdb -tA -c ${quotedSql}`;
        const { stdout } = await execAsync(cmd, {
            timeout: StoreHelper.EXEC_TIMEOUT_MS,
        });
        return stdout.replace(/\n+$/, '');
    }

    // ---------------------------------------------------------------
    // Private helpers
    // ---------------------------------------------------------------

    /**
     * Resolve the container ID hosting Postgres. In BOTH Docker and RPM
     * modes the `ai_workbench` store and the replication cluster run in
     * the `postgres` compose service — in RPM mode the workbench binaries
     * run in a separate `workbench` container, but the databases do not
     * (docker-compose.rpm.yml defines its own `postgres` service with
     * POSTGRES_DB=ai_workbench, and ReplicationHelper starts the cluster
     * inside that same container). So this always targets `postgres`,
     * independent of INSTALL_MODE.
     */
    private async getContainerId(): Promise<string> {
        const { stdout } = await execAsync(
            'docker ps ' +
            '--filter "label=com.docker.compose.service=postgres" ' +
            '--format "{{.ID}}"',
        );
        const id = stdout.trim().split('\n')[0];
        if (!id) {
            throw new Error(
                'postgres container not found — is the stack running?',
            );
        }
        return id;
    }
}

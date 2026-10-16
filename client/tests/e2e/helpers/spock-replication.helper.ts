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
import * as path from 'path';
import { promisify } from 'util';

const execAsync = promisify(exec);

const DOCKER_DIR = path.resolve(__dirname, '..', 'docker');
const START_SCRIPT = path.join(DOCKER_DIR, 'start-spock-replication.sh');

// Compose service names (also the hostnames resolvable on e2e-network, which
// is how the workbench server/collector reach the nodes).
const NODE_SERVICES = [
    'pg-spock-node1',
    'pg-spock-node2',
    'pg-spock-node3',
] as const;
const INTERNAL_PORT = 5432;
// Host port mappings (docker-compose.yml) for test-side setup/verification.
const HOST_PORTS = [5452, 5453, 5454] as const;
// Quick docker-compose/pg_isready operations (up/rm, port polling) — these
// complete in seconds once the nodes are running, so a tight timeout is the
// right failure signal if one hangs.
const DOCKER_COMMAND_TIMEOUT_MS = 180_000;
// start-spock-replication.sh's OWN internal budget is up to ~660s worst case:
// 120s of pg_isready polling up front, then three sequential 180s stage
// timeouts (subscription health, sentinel-insert propagation, sentinel-delete
// propagation). The external wrapper must give real headroom above that sum,
// not match or undercut it, or it kills the script mid-stage before either
// the success or failure line is ever printed. 15 minutes leaves ~4 minutes
// of margin over the ~11-minute worst case for docker/psql overhead.
const SCRIPT_TIMEOUT_MS = 900_000;

/** Node index accepted by the per-node helpers: 1, 2 or 3. */
export type SpockNodeIndex = 1 | 2 | 3;

// ---------------------------------------------------------------
// SpockReplicationHelper
// ---------------------------------------------------------------

/**
 * Controls the three-node NATIVE Spock (multi-master) replication cluster used
 * by the spock E2E specs. Mirrors LogicalReplicationHelper, but the topology is
 * a full mesh across three dedicated containers rather than a
 * publisher/subscriber pair:
 *
 *   pg-spock-node1  node n1  (host 5452 -> 5432, ragdb)
 *   pg-spock-node2  node n2  (host 5453 -> 5432, ragdb)
 *   pg-spock-node3  node n3  (host 5454 -> 5432, ragdb)
 *
 * Each node subscribes to the other two (six subscriptions total), so a write
 * on any node replicates to the other two. The workbench reaches them over the
 * docker network as pg-spock-node1:5432 / pg-spock-node2:5432 /
 * pg-spock-node3:5432.
 *
 * Call start() in beforeAll() to (idempotently) establish the mesh. stop() is a
 * deliberate no-op so the cluster persists across specs; remove() tears the
 * three containers down at cleanup.
 */
export class SpockReplicationHelper {

    /**
     * Establish the Spock mesh by running start-spock-replication.sh inside the
     * node1 container (which can reach node2/node3 over the e2e network).
     * Idempotent: reattaches to an already-configured cluster. Waits until all
     * three nodes accept connections before returning.
     */
    async start(): Promise<void> {
        // The three node containers are gated behind the `spock` compose
        // profile, so the default global-setup `docker compose up -d`
        // deliberately does NOT start them — they stay down through the other
        // phases to keep the idle footprint minimal. composeUpNodes() enables
        // the profile and brings them up on demand here (a no-op when already
        // running) before configuring replication.
        await this.composeUpNodes();

        const node1 = await this.getContainerId(NODE_SERVICES[0]);
        await execAsync(
            `docker cp ${START_SCRIPT} ${node1}:/tmp/start-spock-replication.sh`,
        );
        const { stdout, stderr } = await execAsync(
            `docker exec -u root -e POSTGRES_PASSWORD=${this.pgPassword()} `
            + `${node1} bash /tmp/start-spock-replication.sh`,
            { timeout: SCRIPT_TIMEOUT_MS },
        );
        if (!/Replication healthy/.test(stdout)) {
            throw new Error(
                '[SpockReplicationHelper] start-spock-replication.sh did not '
                + `report a healthy mesh:\n${stdout}\n${stderr}`,
            );
        }
        if (stderr) {
            console.log(
                '[SpockReplicationHelper] start-spock-replication.sh stderr:',
                stderr,
            );
        }
        await Promise.all(
            NODE_SERVICES.map((service) => this.waitForPort(service)),
        );
    }

    /**
     * Per-spec teardown is intentionally a NO-OP: the spock cluster is set up
     * once and reused by every spock spec (mirrors LogicalReplicationHelper).
     * remove() is the real cleanup.
     */
    async stop(): Promise<void> {
        await Promise.resolve();
    }

    /**
     * Stop AND remove the three dedicated spock-node containers plus their
     * anonymous volumes. Idempotent: `compose rm` is a no-op when the services
     * are already absent. Run from the docker/ directory (with POSTGRES_PASSWORD
     * set) so compose can parse the file and match the shared project.
     */
    async remove(): Promise<void> {
        await execAsync(
            'docker compose -f docker-compose.yml --profile spock rm -fsv '
            + NODE_SERVICES.join(' '),
            {
                cwd: DOCKER_DIR,
                timeout: DOCKER_COMMAND_TIMEOUT_MS,
                env: { ...process.env, POSTGRES_PASSWORD: this.pgPassword() },
            },
        );
    }

    /** Docker-network hostname of the given node (workbench connects here). */
    nodeHost(index: SpockNodeIndex): string {
        return NODE_SERVICES[index - 1];
    }

    /** In-container PostgreSQL port shared by all three nodes. */
    internalPort(): number {
        return INTERNAL_PORT;
    }

    /** Host-side connection string for a node (test setup/verify). */
    nodeConnString(index: SpockNodeIndex): string {
        return `postgresql://postgres:${this.pgPassword()}`
            + `@localhost:${HOST_PORTS[index - 1]}/ragdb`;
    }

    /**
     * The spock node names created by start-spock-replication.sh (n1/n2/n3).
     */
    nodeName(index: SpockNodeIndex): string {
        return `n${index}`;
    }

    /**
     * Subscription-name convention used by the mesh: the subscription ON
     * `target` that pulls from `from` is `sub_n<target>_from_n<from>`. Phase-4
     * conflict specs use this to disable/enable a specific link.
     */
    subName(target: SpockNodeIndex, from: SpockNodeIndex): string {
        return `sub_n${target}_from_n${from}`;
    }

    /**
     * Run a statement against a specific node (database `ragdb`) as the postgres
     * superuser via `docker exec psql`. Mirrors LogicalReplicationHelper.
     * execNode1: used for DDL and conflict generation the read-only MCP query
     * path cannot run. Returns trimmed tuples-only stdout.
     */
    async execNode(index: SpockNodeIndex, sql: string): Promise<string> {
        const id = await this.getContainerId(NODE_SERVICES[index - 1]);
        const quotedSql = `'${sql.replace(/'/g, `'\\''`)}'`;
        const { stdout } = await execAsync(
            `docker exec -e PGPASSWORD='${this.pgPassword()}' ${id} `
            + `psql -U postgres -d ragdb -tA -c ${quotedSql}`,
            { timeout: 30_000 },
        );
        return stdout.replace(/\n+$/, '');
    }

    /** Convenience wrapper: execNode on node1. */
    async execNode1(sql: string): Promise<string> {
        return this.execNode(1, sql);
    }

    /** Convenience wrapper: execNode on node2. */
    async execNode2(sql: string): Promise<string> {
        return this.execNode(2, sql);
    }

    /** Convenience wrapper: execNode on node3. */
    async execNode3(sql: string): Promise<string> {
        return this.execNode(3, sql);
    }

    /**
     * Disable a subscription on the given node (immediate:=true stops the apply
     * worker at once). Phase-4 conflict specs disable a link, write divergent
     * rows on both ends, then re-enable to force a conflict.
     */
    async disableSub(index: SpockNodeIndex, subName: string): Promise<void> {
        await this.execNode(
            index,
            `SELECT spock.sub_disable(subscription_name := '${subName}', `
            + 'immediate := true);',
        );
    }

    /** Re-enable a subscription on the given node (immediate:=true). */
    async enableSub(index: SpockNodeIndex, subName: string): Promise<void> {
        await this.execNode(
            index,
            `SELECT spock.sub_enable(subscription_name := '${subName}', `
            + 'immediate := true);',
        );
    }

    /**
     * Read the conflict-resolution audit rows from spock.resolutions on the
     * given node (last-update-wins records each resolved conflict here). Returns
     * one string per row of the selected columns, newest first; an empty array
     * when no conflicts have been resolved. Phase-4 specs assert on these.
     */
    async readResolutions(index: SpockNodeIndex): Promise<string[]> {
        const out = await this.execNode(
            index,
            'SELECT relname || \'|\' || conflict_type || \'|\' '
            + '|| conflict_resolution FROM spock.resolutions '
            + 'ORDER BY log_time DESC;',
        );
        return out === '' ? [] : out.split('\n');
    }

    /**
     * Read the spock.exception_log rows on the given node (populated when apply
     * fails and the error-handling strategy logs the offending change). Returns
     * one string per row, newest first; empty array when the log is clean.
     */
    async readExceptionLog(index: SpockNodeIndex): Promise<string[]> {
        const out = await this.execNode(
            index,
            'SELECT table_name || \'|\' || operation || \'|\' || error_message '
            + 'FROM spock.exception_log ORDER BY remote_commit_ts DESC;',
        );
        return out === '' ? [] : out.split('\n');
    }

    // ---------------------------------------------------------------
    // Private helpers
    // ---------------------------------------------------------------

    /**
     * Bring the three spock-node services up via docker compose (idempotent).
     * Run from the docker/ directory so the compose project name matches the
     * rest of the stack and the nodes join the shared e2e-network.
     */
    private async composeUpNodes(): Promise<void> {
        await execAsync(
            'docker compose -f docker-compose.yml --profile spock up -d '
            + NODE_SERVICES.join(' '),
            {
                cwd: DOCKER_DIR,
                timeout: DOCKER_COMMAND_TIMEOUT_MS,
                env: { ...process.env, POSTGRES_PASSWORD: this.pgPassword() },
            },
        );
    }

    private async getContainerId(service: string): Promise<string> {
        const { stdout } = await execAsync(
            `docker ps --filter "label=com.docker.compose.service=${service}" `
            + '--format "{{.ID}}"',
        );
        const id = stdout.trim().split('\n')[0];
        if (!id) {
            throw new Error(
                `${service} container not found — is the spock cluster running?`,
            );
        }
        return id;
    }

    private async waitForPort(
        service: string,
        timeoutMs = DOCKER_COMMAND_TIMEOUT_MS,
    ): Promise<void> {
        const id = await this.getContainerId(service);
        const deadline = Date.now() + timeoutMs;
        while (Date.now() < deadline) {
            try {
                await execAsync(
                    `docker exec ${id} pg_isready -h 127.0.0.1 `
                    + `-p ${INTERNAL_PORT} -U postgres`,
                    { timeout: 5_000 },
                );
                return;
            } catch {
                await new Promise<void>((r) => setTimeout(r, 1_000));
            }
        }
        throw new Error(
            `Timeout waiting for ${service} on port ${INTERNAL_PORT}`,
        );
    }

    private pgPassword(): string {
        return process.env['POSTGRES_PASSWORD'] ?? 'postgres';
    }
}

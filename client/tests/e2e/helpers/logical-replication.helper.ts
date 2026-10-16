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
const START_SCRIPT = path.join(DOCKER_DIR, 'start-logical-replication.sh');

// Compose service names (also the hostnames resolvable on e2e-network, which
// is how the workbench server/collector reach the nodes).
const NODE1_SERVICE = 'pg-logical-node1';
const NODE2_SERVICE = 'pg-logical-node2';
const INTERNAL_PORT = 5432;
// Host port mappings (docker-compose.yml) for test-side setup/verification.
const HOST_PORT_NODE1 = 5450;
const HOST_PORT_NODE2 = 5451;
const STARTUP_TIMEOUT_MS = 120_000;

// ---------------------------------------------------------------
// LogicalReplicationHelper
// ---------------------------------------------------------------

/**
 * Controls the two-node NATIVE logical-replication cluster used by the
 * logical E2E specs. Unlike the physical cluster (three instances inside the
 * single `postgres` container, see ReplicationHelper), each logical node is
 * its own container:
 *
 *   pg-logical-node1  publisher   (host 5450 -> 5432, ragdb)
 *   pg-logical-node2  subscriber  (host 5451 -> 5432, ragdb)
 *
 * The workbench reaches them over the docker network as
 * pg-logical-node1:5432 / pg-logical-node2:5432.
 *
 * Call start() in beforeAll() to (idempotently) establish the publication and
 * subscription. stop() is a deliberate no-op so the cluster persists across
 * specs; the stack teardown is the real cleanup.
 */
export class LogicalReplicationHelper {

    /**
     * Establishes logical replication by running start-logical-replication.sh
     * inside the node2 container (which can reach node1 over the e2e network).
     * Idempotent: reattaches to an already-configured cluster. Waits until
     * both nodes accept connections before returning.
     */
    async start(): Promise<void> {
        // Ensure both node containers are up. They are gated behind the
        // `logical` compose profile, so the default global-setup
        // `docker compose up -d` deliberately does NOT start them — they stay
        // down through the physical phase to keep the idle footprint minimal.
        // composeUpNodes() enables the profile and brings them up on demand
        // here (idempotent: a no-op when already running) before configuring
        // replication. Fresh containers get re-provisioned by the setup script.
        await this.composeUpNodes();

        const node2 = await this.getContainerId(NODE2_SERVICE);
        await execAsync(
            `docker cp ${START_SCRIPT} ${node2}:/tmp/start-logical-replication.sh`,
        );
        const { stderr } = await execAsync(
            `docker exec -u root -e POSTGRES_PASSWORD=${this.pgPassword()} `
            + `${node2} bash /tmp/start-logical-replication.sh`,
            { timeout: STARTUP_TIMEOUT_MS },
        );
        if (stderr) {
            console.log(
                '[LogicalReplicationHelper] start-logical-replication.sh '
                + 'stderr:',
                stderr,
            );
        }
        await Promise.all([
            this.waitForPort(NODE1_SERVICE),
            this.waitForPort(NODE2_SERVICE),
        ]);
    }

    /**
     * Per-spec teardown is intentionally a NO-OP: the logical cluster is set
     * up once and reused by every logical spec (see ReplicationHelper.stop
     * for the rationale). The stack teardown (`docker compose down -v`) is the
     * real cleanup.
     */
    async stop(): Promise<void> {
        await Promise.resolve();
    }

    /**
     * Stop AND remove the two dedicated logical-node containers
     * (pg-logical-node1 / pg-logical-node2) plus their anonymous volumes.
     *
     * Unlike the physical cluster — whose instances live inside the shared
     * `postgres` container and are merely halted by ReplicationHelper.forceStop
     * (there is no dedicated container to remove) — each logical node is its
     * OWN container, so the logical cleanup can and should tear them down to
     * free their resources. Idempotent: `compose rm` is a no-op when the
     * services are already absent. Run from the docker/ directory (with
     * POSTGRES_PASSWORD set) so compose can parse the file and match the
     * shared project.
     */
    async remove(): Promise<void> {
        await execAsync(
            'docker compose -f docker-compose.yml --profile logical rm -fsv '
            + `${NODE1_SERVICE} ${NODE2_SERVICE}`,
            {
                cwd: DOCKER_DIR,
                timeout: STARTUP_TIMEOUT_MS,
                env: { ...process.env, POSTGRES_PASSWORD: this.pgPassword() },
            },
        );
    }

    /** Docker-network hostname of the publisher node (workbench connects here). */
    node1Host(): string {
        return NODE1_SERVICE;
    }

    /** Docker-network hostname of the subscriber node. */
    node2Host(): string {
        return NODE2_SERVICE;
    }

    /** In-container PostgreSQL port shared by both nodes. */
    internalPort(): number {
        return INTERNAL_PORT;
    }

    /** Host-side connection string for the publisher (test setup/verify). */
    node1ConnString(): string {
        return `postgresql://postgres:${this.pgPassword()}`
            + `@localhost:${HOST_PORT_NODE1}/ragdb`;
    }

    /** Host-side connection string for the subscriber (test setup/verify). */
    node2ConnString(): string {
        return `postgresql://postgres:${this.pgPassword()}`
            + `@localhost:${HOST_PORT_NODE2}/ragdb`;
    }

    /**
     * Run a statement against the PUBLISHER node (pg-logical-node1, database
     * `ragdb`) as the postgres superuser via `docker exec psql`. Mirrors
     * StoreHelper.execPrimary (which targets the physical primary on 5440):
     * used for DDL the read-only MCP query path cannot run (CREATE TABLE,
     * pg_stat_statements_reset). Returns trimmed tuples-only stdout.
     */
    async execNode1(sql: string): Promise<string> {
        const id = await this.getContainerId(NODE1_SERVICE);
        const quotedSql = `'${sql.replace(/'/g, `'\\''`)}'`;
        const { stdout } = await execAsync(
            `docker exec -e PGPASSWORD='${this.pgPassword()}' ${id} `
            + `psql -U postgres -d ragdb -tA -c ${quotedSql}`,
            { timeout: 30_000 },
        );
        return stdout.replace(/\n+$/, '');
    }

    // ---------------------------------------------------------------
    // Private helpers
    // ---------------------------------------------------------------

    /**
     * Bring the two logical-node services up via docker compose (idempotent).
     * Run from the docker/ directory so the compose project name matches the
     * rest of the stack and the nodes join the shared e2e-network.
     */
    private async composeUpNodes(): Promise<void> {
        await execAsync(
            'docker compose -f docker-compose.yml --profile logical up -d '
            + `${NODE1_SERVICE} ${NODE2_SERVICE}`,
            {
                cwd: DOCKER_DIR,
                timeout: STARTUP_TIMEOUT_MS,
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
                `${service} container not found — is the logical cluster `
                + 'running?',
            );
        }
        return id;
    }

    private async waitForPort(
        service: string,
        timeoutMs = STARTUP_TIMEOUT_MS,
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

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

const DOCKER_DIR     = path.resolve(__dirname, '..', 'docker');
const START_SCRIPT   = path.join(DOCKER_DIR, 'start-replication.sh');
const STOP_SCRIPT    = path.join(DOCKER_DIR, 'stop-replication.sh');
const PRIMARY_PORT   = 5440;
const STANDBY1_PORT  = 5441;
const STANDBY2_PORT  = 5442;
const STARTUP_TIMEOUT_MS = 120_000;

// ---------------------------------------------------------------
// ReplicationHelper
// ---------------------------------------------------------------

/**
 * Controls the on-demand PostgreSQL replication cluster used in
 * E2E tests. The cluster runs three instances inside the postgres
 * container:
 *
 *   5440  ragdb primary
 *   5441  ragdb standby1 (hot standby)
 *   5442  ragdb standby2 (hot standby)
 *
 * Call start() in beforeAll() and stop() in afterAll() to bracket
 * the replication test suite.
 */
export class ReplicationHelper {

    /**
     * Starts the replication cluster by running start-replication.sh
     * inside the postgres container via docker exec. Waits until all
     * three ports accept TCP connections before returning.
     */
    async start(): Promise<void> {
        const id = await this.getContainerId();
        // Copy the script into the container so this works regardless of
        // whether the replication compose overlay was used when starting the stack.
        await execAsync(`docker cp ${START_SCRIPT} ${id}:/start-replication.sh`);
        await execAsync(`docker exec ${id} chmod +x /start-replication.sh`);
        const { stderr } = await execAsync(
            `docker exec ${id} /start-replication.sh`,
            { timeout: STARTUP_TIMEOUT_MS },
        );
        if (stderr) {
            console.log('[ReplicationHelper] start-replication.sh stderr:', stderr);
        }
        await Promise.all([
            this.waitForPort(PRIMARY_PORT),
            this.waitForPort(STANDBY1_PORT),
            this.waitForPort(STANDBY2_PORT),
        ]);
    }

    /**
     * Stops all replication instances by running stop-replication.sh
     * inside the postgres container via docker exec.
     */
    async stop(): Promise<void> {
        const id = await this.getContainerId();
        await execAsync(`docker cp ${STOP_SCRIPT} ${id}:/stop-replication.sh`);
        await execAsync(`docker exec ${id} chmod +x /stop-replication.sh`);
        await execAsync(`docker exec ${id} /stop-replication.sh`, {
            timeout: 30_000,
        });
    }

    /**
     * Returns the connection string for the replication primary.
     */
    primaryConnString(): string {
        return `postgresql://postgres:${this.pgPassword()}@localhost:${PRIMARY_PORT}/ragdb`;
    }

    /**
     * Returns the connection string for standby 1.
     */
    standby1ConnString(): string {
        return `postgresql://postgres:${this.pgPassword()}@localhost:${STANDBY1_PORT}/ragdb`;
    }

    /**
     * Returns the connection string for standby 2.
     */
    standby2ConnString(): string {
        return `postgresql://postgres:${this.pgPassword()}@localhost:${STANDBY2_PORT}/ragdb`;
    }

    /**
     * Restarts the collector so it immediately picks up any servers
     * added since its last config reload.
     *
     * Docker mode: restarts the `docker-collector-1` container via
     * `docker restart`.
     *
     * RPM mode: finds the `workbench` container via its compose label,
     * tries `systemctl restart pgedge-ai-dba-collector.service` first,
     * then falls back to kill+restart via `docker exec`.
     *
     * When `serverNames` is provided (non-empty), the method polls
     * the clusters API until none of the named servers report
     * `status === 'initialising'`. This avoids the race condition
     * where the fixed 5-second sleep was not long enough for the
     * collector to complete its first probe cycle.
     *
     * When `serverNames` is omitted or empty, the method falls back
     * to the original 5-second wait for backward compatibility.
     */
    async restartCollector(serverNames: string[] = []): Promise<void> {
        // In RPM mode the collector runs as a system service; there is no
        // Docker container to restart.  Skip the Docker restart and rely
        // solely on the API polling below to wait for the status change.
        if (process.env['INSTALL_MODE'] !== 'rpm') {
            await execAsync('docker restart docker-collector-1', {
                timeout: 30_000,
            });

            // Poll until the collector container is back in "running" state
            const deadline = Date.now() + 30_000;
            while (Date.now() < deadline) {
                try {
                    const { stdout } = await execAsync(
                        'docker ps --filter "label=com.docker.compose.service=collector" '
                        + '--format "{{.Status}}"',
                    );
                    if (stdout.trim().toLowerCase().startsWith('up')) {
                        break;
                    }
                } catch {
                    // Container not yet visible in docker ps — keep polling
                }
                await new Promise<void>(r => setTimeout(r, 1_000));
            }
        } else {
            // RPM: all services run inside a single Docker container
            // named "workbench". Use docker exec to reach inside it,
            // trying systemctl first, then falling back to kill+restart.
            try {
                const { stdout: wbOut } = await execAsync(
                    'docker ps --filter "label=com.docker.compose.service=workbench"'
                    + ' --format "{{.ID}}"',
                );
                const wbId = wbOut.trim().split('\n')[0];

                if (!wbId) {
                    console.warn(
                        '[ReplicationHelper] workbench container not found '
                        + '— skipping collector restart, falling through '
                        + 'to API polling',
                    );
                } else {
                    try {
                        // Try systemd first (correct service name)
                        await execAsync(
                            `docker exec ${wbId} systemctl restart`
                            + ' pgedge-ai-dba-collector.service',
                            { timeout: 30_000 },
                        );
                    } catch {
                        // systemctl unavailable — fall back to
                        // kill + direct restart
                        // Use process-name match (not -f full-cmdline) to
                        // avoid pkill self-matching the bash wrapper whose
                        // cmdline contains the path as an argument.
                        await execAsync(
                            `docker exec ${wbId} bash -c`
                            + " 'pkill ai-dba-collector; true'",
                            { timeout: 10_000 },
                        );
                        await new Promise<void>(r => setTimeout(r, 2_000));
                        await execAsync(
                            `docker exec -d ${wbId} bash -c`
                            + " '/usr/bin/ai-dba-collector"
                            + ' -config /etc/pgedge/ai-dba-collector.yaml'
                            + ' -pg-host postgres'
                            + ' -pg-database ai_workbench'
                            + ' -pg-username dba_collector'
                            + ' -pg-password-file'
                            + ' /etc/pgedge/dba-collector-password'
                            + ' -pg-sslmode disable'
                            + " >> /var/log/pgedge/collector.log 2>&1'",
                        );
                    }
                    // Give the collector time to initialise before
                    // the API polling loop begins.
                    await new Promise<void>(r => setTimeout(r, 5_000));
                }
            } catch (err) {
                console.warn(
                    '[ReplicationHelper] RPM collector restart failed'
                    + ' — falling through to API polling:',
                    err,
                );
            }
        }

        if (serverNames.length > 0) {
            // Poll the clusters API until none of the named servers
            // are still in 'initialising' status.
            const apiUrl = process.env['E2E_API_URL'] ?? 'http://localhost:8080';
            const cookie = process.env['E2E_ADMIN_COOKIE'] ?? '';
            const names = new Set(serverNames);
            const pollDeadline = Date.now() + 90_000;

            while (Date.now() < pollDeadline) {
                try {
                    const resp = await fetch(
                        `${apiUrl}/api/v1/clusters`,
                        {
                            headers: {
                                'Cookie': cookie,
                                'Accept': 'application/json',
                            },
                        },
                    );
                    if (resp.ok) {
                        const data: unknown = await resp.json();
                        if (!this.hasInitialisingServer(data, names)) {
                            return;
                        }
                    }
                } catch {
                    // Network error — collector may not be ready yet
                }
                await new Promise<void>(r => setTimeout(r, 3_000));
            }

            console.warn(
                '[ReplicationHelper] Timed out after 90s waiting for '
                + `servers [${serverNames.join(', ')}] to leave `
                + '\'initialising\' status — continuing anyway',
            );
        } else {
            // Backward-compatible fallback: give the collector time to
            // initialise and begin its first collection cycle.
            await new Promise<void>(r => setTimeout(r, 5_000));
        }
    }

    // ---------------------------------------------------------------
    // Private helpers
    // ---------------------------------------------------------------

    /**
     * Recursively searches a JSON value for any object whose `name`
     * property matches one of the given names AND whose `status`
     * property equals `'initialising'` or `'unknown'`.  Both are
     * transitional states shown as "Initializing" in the UI.
     * Returns `true` if at least one such match is found.
     */
    private hasInitialisingServer(
        data: unknown,
        names: Set<string>,
    ): boolean {
        if (data === null || data === undefined) {
            return false;
        }
        if (Array.isArray(data)) {
            return data.some(item => this.hasInitialisingServer(item, names));
        }
        if (typeof data === 'object') {
            const obj = data as Record<string, unknown>;
            if (
                typeof obj['name'] === 'string'
                && names.has(obj['name'])
                && (obj['status'] === 'initialising' || obj['status'] === 'unknown')
            ) {
                return true;
            }
            return Object.values(obj).some(
                val => this.hasInitialisingServer(val, names),
            );
        }
        return false;
    }

    private async getContainerId(): Promise<string> {
        // Use docker ps --filter instead of docker compose ps to avoid
        // requiring POSTGRES_PASSWORD to be set in the test runner environment
        // (docker compose ps parses the compose file and fails on required vars).
        const { stdout } = await execAsync(
            'docker ps --filter "label=com.docker.compose.service=postgres" --format "{{.ID}}"',
        );
        const id = stdout.trim().split('\n')[0];
        if (!id) {
            throw new Error(
                'postgres container not found — is the replication stack running?',
            );
        }
        return id;
    }

    private async waitForPort(
        port: number,
        timeoutMs = STARTUP_TIMEOUT_MS,
    ): Promise<void> {
        const id = await this.getContainerId();
        const deadline = Date.now() + timeoutMs;
        while (Date.now() < deadline) {
            try {
                await execAsync(
                    `docker exec ${id} pg_isready -h 127.0.0.1 -p ${port}`,
                    { timeout: 5_000 },
                );
                return;
            } catch {
                await new Promise<void>(r => setTimeout(r, 1_000));
            }
        }
        throw new Error(`Timeout waiting for PostgreSQL on port ${port}`);
    }

    private pgPassword(): string {
        return process.env['POSTGRES_PASSWORD'] ?? 'postgres';
    }
}

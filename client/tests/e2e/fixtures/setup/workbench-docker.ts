/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { execSync, spawnSync } from 'child_process';
import * as path from 'path';
import { ADMIN_USER, API_URL } from '../test-data';

const E2E_DIR = path.join(__dirname, '..', '..');
const COMPOSE_FILE = path.join(E2E_DIR, 'docker', 'docker-compose.yml');
const DOCKER_COMPOSE_ARGS = [
    'compose', '-f', COMPOSE_FILE, 'exec', '-T', 'server',
];

/** Start the Docker-based workbench stack if not already running. */
export async function setupWorkbenchDocker(forceRecreateServer = false): Promise<void> {
    console.log('[E2E setup] Install mode: Docker');
    // Evaluated here (not at module load) so E2E_CONFIG_DIR set by
    // generateE2EConfigs() is captured before docker compose runs.
    const COMPOSE_ENV = { ...process.env, POSTGRES_PASSWORD: 'postgres' };

    let serverAlreadyUp = false;
    try {
        const probe = await fetch(`${API_URL}/health`);
        serverAlreadyUp = probe.status < 500;
    } catch {
        serverAlreadyUp = false;
    }

    if (!serverAlreadyUp) {
        console.log('[E2E setup] Starting Docker stack (this may take a while on first run)...');
        execSync(`docker compose -f ${COMPOSE_FILE} down --volumes`, {
            cwd: E2E_DIR,
            stdio: 'pipe',
            env: COMPOSE_ENV,
        });
        execSync(`docker compose -f ${COMPOSE_FILE} up -d --build`, {
            cwd: E2E_DIR,
            stdio: 'inherit',
            env: COMPOSE_ENV,
        });
    } else if (forceRecreateServer) {
        console.log(
            '[E2E setup] Server already running — recreating server and alerter ' +
            'containers to apply LLM config...',
        );
        execSync(`docker compose -f ${COMPOSE_FILE} up -d --force-recreate server alerter`, {
            cwd: E2E_DIR,
            stdio: 'inherit',
            env: COMPOSE_ENV,
        });
    }
}

/** Create admin user inside the Docker server container. */
export function createAdminUserDocker(): void {
    const composeEnv = { ...process.env, POSTGRES_PASSWORD: 'postgres' };
    const spawnOpts = {
        cwd: E2E_DIR,
        stdio: 'pipe' as const,
        env: composeEnv,
    };

    spawnSync('docker', [
        ...DOCKER_COMPOSE_ARGS, 'ai-dba-server',
        '-add-user',
        '-username', ADMIN_USER.username,
        '-password', ADMIN_USER.password,
        '-data-dir', '/data',
    ], spawnOpts);

    spawnSync('docker', [
        ...DOCKER_COMPOSE_ARGS, 'ai-dba-server',
        '-set-superuser',
        '-username', ADMIN_USER.username,
        '-data-dir', '/data',
    ], spawnOpts);
}

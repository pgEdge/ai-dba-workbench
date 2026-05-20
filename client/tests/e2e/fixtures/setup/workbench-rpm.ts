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
import { type RepoChannel, type PlatformImage, PLATFORM_BASE_IMAGES } from '../e2e-config';
import { ADMIN_USER, API_URL } from '../test-data';

const E2E_DIR = path.join(__dirname, '..', '..');
const RPM_COMPOSE_FILE = path.join(E2E_DIR, 'docker', 'docker-compose.rpm.yml');

/** Container name defined in docker-compose.rpm.yml for exec access. */
const RPM_CONTAINER_SERVICE = 'workbench';

function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Start the RPM-based workbench stack.
 *
 * Launches a single container built from Dockerfile.rpm-installer
 * that installs and runs the pgEdge AI DBA Workbench via RPM packages.
 * PostgreSQL, Mailpit, and WireMock are also started from this compose
 * file. After the stack is up, runs a network connectivity preflight
 * check from inside the workbench container.
 */
export async function setupWorkbenchRPM(
    repoChannel: RepoChannel,
    platformImage: PlatformImage,
): Promise<void> {
    console.log('[E2E setup] Install mode: RPM');
    console.log(`[E2E setup] Repo channel:   ${repoChannel}`);
    console.log(`[E2E setup] Platform image: ${platformImage}`);

    const baseImage = PLATFORM_BASE_IMAGES[platformImage];
    const rpmEnv = {
        ...process.env,
        POSTGRES_PASSWORD: 'postgres',
        RPM_PLATFORM_IMAGE: baseImage,
        REPO_CHANNEL: repoChannel,
    };

    let serverAlreadyUp = false;
    try {
        const probe = await fetch(`${API_URL}/health`);
        serverAlreadyUp = probe.status < 500;
    } catch {
        serverAlreadyUp = false;
    }

    if (!serverAlreadyUp) {
        console.log('[E2E setup] Starting RPM stack (this may take a while on first run)...');
        execSync(`docker compose -f ${RPM_COMPOSE_FILE} down --volumes`, {
            cwd: E2E_DIR,
            stdio: 'pipe',
            env: rpmEnv,
        });
        execSync(`docker compose -f ${RPM_COMPOSE_FILE} up -d --build`, {
            cwd: E2E_DIR,
            stdio: 'inherit',
            env: rpmEnv,
        });
    }

    await validateRPMNetworkConnectivity();
}

/**
 * Validate network connectivity from inside the RPM workbench container.
 *
 * Executes rpm-preflight.sh inside the workbench container and waits up
 * to 60 seconds for the container to be exec-able. Throws on failure so
 * the test suite fails fast with a clear diagnostic message instead of
 * timing out on the health-check loop.
 */
export async function validateRPMNetworkConnectivity(): Promise<void> {
    console.log('[E2E preflight] Validating RPM container network connectivity...');

    const execEnv = { ...process.env, POSTGRES_PASSWORD: 'postgres' };
    const deadline = Date.now() + 60_000;
    let lastOutput = '';

    while (Date.now() < deadline) {
        const result = spawnSync(
            'docker',
            [
                'compose', '-f', RPM_COMPOSE_FILE,
                'exec', '-T', RPM_CONTAINER_SERVICE,
                '/usr/local/bin/rpm-preflight.sh',
            ],
            { cwd: E2E_DIR, stdio: 'pipe', env: execEnv },
        );

        const stdout = result.stdout?.toString() ?? '';
        const stderr = result.stderr?.toString() ?? '';
        lastOutput = stdout + stderr;

        if (result.status === 0) {
            process.stdout.write(stdout);
            console.log('[E2E preflight] All connectivity checks passed.');
            return;
        }

        // Container not yet exec-able or preflight failed — wait and retry.
        await sleep(2_000);
    }

    // Check whether the container exited (preflight failed at startup).
    const ps = spawnSync(
        'docker',
        ['compose', '-f', RPM_COMPOSE_FILE, 'ps', '--status', 'exited', RPM_CONTAINER_SERVICE],
        { cwd: E2E_DIR, stdio: 'pipe', env: execEnv },
    );
    const psOutput = ps.stdout?.toString() ?? '';
    const containerExited = psOutput.includes(RPM_CONTAINER_SERVICE);

    const hint = containerExited
        ? 'The workbench container exited — network preflight may have failed at startup. ' +
          'Run: docker compose -f docker/docker-compose.rpm.yml logs workbench'
        : 'The workbench container did not become exec-able within 60 s.';

    throw new Error(
        `[E2E preflight] RPM container network connectivity check failed.\n` +
        `${hint}\n\nLast output:\n${lastOutput}`,
    );
}

/** Create admin user inside the RPM workbench container. */
export function createAdminUserRPM(): void {
    const spawnOpts = {
        cwd: E2E_DIR,
        stdio: 'pipe' as const,
        env: { ...process.env, POSTGRES_PASSWORD: 'postgres' },
    };

    console.log(`[E2E setup] Creating RPM admin user "${ADMIN_USER.username}"...`);

    const addResult = spawnSync('docker', [
        'compose', '-f', RPM_COMPOSE_FILE,
        'exec', '-T', RPM_CONTAINER_SERVICE,
        '/usr/bin/ai-dba-server',
        '-add-user',
        '-username', ADMIN_USER.username,
        '-password', ADMIN_USER.password,
        '-data-dir', '/data',
    ], spawnOpts);

    if (addResult.status !== 0) {
        const stdout = addResult.stdout?.toString() ?? '';
        const stderr = addResult.stderr?.toString() ?? '';
        console.warn(
            `[E2E setup] Warning: Failed to add admin user (exit ${addResult.status}). ` +
            `User may already exist.\n${stdout}${stderr}`,
        );
    }

    const superuserResult = spawnSync('docker', [
        'compose', '-f', RPM_COMPOSE_FILE,
        'exec', '-T', RPM_CONTAINER_SERVICE,
        '/usr/bin/ai-dba-server',
        '-set-superuser',
        '-username', ADMIN_USER.username,
        '-data-dir', '/data',
    ], spawnOpts);

    if (superuserResult.status !== 0) {
        const stdout = superuserResult.stdout?.toString() ?? '';
        const stderr = superuserResult.stderr?.toString() ?? '';
        throw new Error(
            `[E2E setup] Failed to set admin user as superuser (exit ${superuserResult.status}):\n` +
            `${stdout}${stderr}`,
        );
    }

    console.log('[E2E setup] RPM admin user created and set as superuser.');
}

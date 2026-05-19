/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { chromium, type FullConfig } from '@playwright/test';
import { execSync, spawnSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { ApiHelper } from '../helpers/api.helper';
import { ADMIN_USER, API_URL, BASE_URL } from './test-data';

/**
 * Playwright global setup function.
 *
 * This runs once before all test files. It verifies the server is
 * reachable, authenticates as the admin user, and saves a browser
 * storage state file so that browser-based tests can skip the UI
 * login flow.
 */
async function globalSetup(_config: FullConfig): Promise<void> {
    const api = new ApiHelper(API_URL);

    // -------------------------------------------------------
    // 0. Start main stack if not already running
    // -------------------------------------------------------
    const e2eDir = path.join(__dirname, '..');
    const secretFile = path.join(e2eDir, 'secret', 'ai-dba.secret');

    // Generate secrets on first run.
    if (!fs.existsSync(secretFile)) {
        console.log('[E2E setup] Generating secrets...');
        execSync('bash scripts/setup-secrets.sh', {
            cwd: e2eDir,
            stdio: 'inherit',
        });
    }

    // Start the stack only if the server is not already reachable.
    let serverAlreadyUp = false;
    try {
        const probe = await fetch(`${API_URL}/health`);
        serverAlreadyUp = probe.status < 500;
    } catch {
        serverAlreadyUp = false;
    }

    if (!serverAlreadyUp) {
        console.log('[E2E setup] Starting main stack (this may take a while on first run)...');
        // Tear down first to remove any stale volumes whose data was
        // encrypted with a different secret (causes 401 on admin login).
        execSync(
            'docker compose -f ./docker/docker-compose.yml down --volumes',
            {
                cwd: e2eDir,
                stdio: 'pipe',
                env: { ...process.env, POSTGRES_PASSWORD: 'postgres' },
            },
        );
        execSync(
            'docker compose -f ./docker/docker-compose.yml up -d --build',
            {
                cwd: e2eDir,
                stdio: 'inherit',
                env: { ...process.env, POSTGRES_PASSWORD: 'postgres' },
            },
        );
    }

    // -------------------------------------------------------
    // 1. Health check
    // -------------------------------------------------------
    const maxAttempts = 30;
    const delayMs = 2_000;
    let healthy = false;
    for (let i = 0; i < maxAttempts; i++) {
        if (await api.healthCheck()) {
            healthy = true;
            break;
        }
        await sleep(delayMs);
    }
    if (!healthy) {
        throw new Error(
            `Server at ${API_URL}/health did not become reachable ` +
            `after ${maxAttempts} attempts. Is the E2E stack running?`,
        );
    }

    // -------------------------------------------------------
    // 2. Create admin user (idempotent — ignored if already exists)
    // -------------------------------------------------------
    // The server never auto-creates users on startup. Run the
    // server CLI inside the container to seed the admin account.
    // spawnSync avoids shell metacharacter issues with passwords.
    const dockerComposeArgs = [
        'compose', '-f', './docker/docker-compose.yml', 'exec', '-T', 'server',
    ];
    const serverBin = 'ai-dba-server';
    const spawnOpts = {
        cwd: e2eDir,
        stdio: 'pipe' as const,
        env: { ...process.env, POSTGRES_PASSWORD: 'postgres' },
    };

    // Create admin user (ignored if already exists).
    spawnSync('docker', [
        ...dockerComposeArgs, serverBin,
        '-add-user',
        '-username', ADMIN_USER.username,
        '-password', ADMIN_USER.password,
        '-data-dir', '/data',
    ], spawnOpts);

    // Grant superuser — bypasses all RBAC permission checks.
    spawnSync('docker', [
        ...dockerComposeArgs, serverBin,
        '-set-superuser',
        '-username', ADMIN_USER.username,
        '-data-dir', '/data',
    ], spawnOpts);

    // -------------------------------------------------------
    // 3. Authenticate as admin via API
    // -------------------------------------------------------
    // The health endpoint can return 200 before the server has
    // finished running migrations and creating the default admin
    // user. Retry login for up to 30 seconds to handle this race.
    let cookie: string | undefined;
    const loginDeadline = Date.now() + 30_000;
    let lastLoginError = '';
    while (Date.now() < loginDeadline) {
        try {
            const result = await api.login(
                ADMIN_USER.username,
                ADMIN_USER.password,
            );
            cookie = result.cookie;
            break;
        } catch (err) {
            lastLoginError = err instanceof Error ? err.message : String(err);
            await sleep(2_000);
        }
    }
    if (!cookie) {
        throw new Error(
            `[E2E setup] Admin login failed for user ` +
            `"${ADMIN_USER.username}" at ${API_URL}: ${lastLoginError}`,
        );
    }

    // Expose the raw cookie to helpers that run outside of a
    // Playwright browser context (e.g. ApiHelper in test hooks).
    process.env.E2E_ADMIN_COOKIE = cookie;

    // -------------------------------------------------------
    // 4. Save browser storage state for Playwright contexts
    // -------------------------------------------------------
    const authDir = path.resolve(__dirname, '..', '.auth');
    if (!fs.existsSync(authDir)) {
        fs.mkdirSync(authDir, { recursive: true });
    }

    const browser = await chromium.launch();
    const context = await browser.newContext();

    // Parse the cookie value from "session_token=<value>".
    const cookieValue = cookie.split('=').slice(1).join('=');
    const baseUrlObj = new URL(BASE_URL);

    await context.addCookies([
        {
            name: 'session_token',
            value: cookieValue,
            domain: baseUrlObj.hostname,
            path: '/',
            httpOnly: true,
            sameSite: 'Lax',
        },
    ]);

    // Navigate to the app so cookies are persisted.
    const page = await context.newPage();
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' });

    const statePath = path.join(authDir, 'admin.json');
    await context.storageState({ path: statePath });

    await browser.close();

    // -------------------------------------------------------
    // 5. Start notification mock services
    // -------------------------------------------------------
    const NOTIFICATIONS_COMPOSE = path.join(
        __dirname, '..', 'docker', 'docker-compose.notifications.yml',
    );
    execSync(`docker compose -f ${NOTIFICATIONS_COMPOSE} pull`, { stdio: 'pipe' });
    execSync(`docker compose -f ${NOTIFICATIONS_COMPOSE} up -d`, { stdio: 'pipe' });
    await waitForHttpService('http://localhost:8025/api/v1/messages');  // Mailpit
    await waitForHttpService('http://localhost:9090/__admin/requests'); // WireMock
    const wireMockUrl = 'http://localhost:9090';
    for (const p of ['/slack', '/mattermost', '/webhook']) {
        await registerWireMockStub(wireMockUrl, p);
    }
}

function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForHttpService(url: string, timeoutMs = 60_000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        try {
            const res = await fetch(url);
            if (res.status < 500) return;
        } catch {
            // not yet reachable
        }
        await sleep(1_000);
    }
    throw new Error(`Service at ${url} did not become ready within ${timeoutMs}ms`);
}

async function registerWireMockStub(baseUrl: string, stubPath: string): Promise<void> {
    await fetch(`${baseUrl}/__admin/mappings`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            request: { method: 'ANY', url: stubPath },
            response: { status: 200, body: 'ok' },
        }),
    });
}

export default globalSetup;

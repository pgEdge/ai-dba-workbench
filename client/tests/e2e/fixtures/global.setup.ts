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
import { execSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { ApiHelper } from '../helpers/api.helper';
import { ADMIN_USER, API_URL, BASE_URL } from './test-data';
import { type E2EConfig, loadE2EConfig } from './e2e-config';
import { setupWorkbenchDocker, createAdminUserDocker } from './setup/workbench-docker';
import { setupWorkbenchRPM, createAdminUserRPM } from './setup/workbench-rpm';

/**
 * Playwright global setup function.
 *
 * This runs once before all test files. It starts the workbench stack
 * in the configured install mode (Docker or RPM), verifies the server
 * is reachable, authenticates as the admin user, and saves a browser
 * storage state file so that browser-based tests can skip the UI
 * login flow.
 */
async function globalSetup(_config: FullConfig): Promise<void> {
    const e2eConfig = loadE2EConfig();
    const api = new ApiHelper(API_URL);

    // -------------------------------------------------------
    // 0. Generate secrets on first run
    // -------------------------------------------------------
    const e2eDir = path.join(__dirname, '..');
    const secretFile = path.join(e2eDir, 'secret', 'ai-dba.secret');
    if (!fs.existsSync(secretFile)) {
        console.log('[E2E setup] Generating secrets...');
        execSync('bash scripts/setup-secrets.sh', {
            cwd: e2eDir,
            stdio: 'inherit',
        });
    }

    // -------------------------------------------------------
    // 1. Start workbench stack (mode-specific)
    // -------------------------------------------------------
    await setupWorkbench(e2eConfig);

    // -------------------------------------------------------
    // 2. Health check
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
    // 3. Create admin user (mode-specific)
    // -------------------------------------------------------
    if (e2eConfig.installMode === 'docker') {
        createAdminUserDocker();
    } else {
        createAdminUserRPM();
    }

    // -------------------------------------------------------
    // 4. Authenticate as admin via API
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
    // 5. Save browser storage state for Playwright contexts
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
    // 6. Start notification mock services
    // -------------------------------------------------------
    // In CI the main docker-compose.yml already starts mailpit and
    // wiremock. Attempting to start them again via the notifications
    // compose would cause a port conflict and abort setup. Only start
    // them when they are not already reachable.
    const mailpitReady = await isHttpServiceReady('http://localhost:8025/api/v1/messages');
    const wiremockReady = await isHttpServiceReady('http://localhost:9090/__admin/requests');
    if (!mailpitReady || !wiremockReady) {
        const NOTIFICATIONS_COMPOSE = path.join(
            __dirname, '..', 'docker', 'docker-compose.notifications.yml',
        );
        execSync(`docker compose -f ${NOTIFICATIONS_COMPOSE} pull`, { stdio: 'pipe' });
        execSync(`docker compose -f ${NOTIFICATIONS_COMPOSE} up -d`, { stdio: 'pipe' });
    }
    await waitForHttpService('http://localhost:8025/api/v1/messages');  // Mailpit
    await waitForHttpService('http://localhost:9090/__admin/requests'); // WireMock
    const wireMockUrl = 'http://localhost:9090';
    for (const p of ['/slack', '/mattermost', '/webhook']) {
        await registerWireMockStub(wireMockUrl, p);
    }
}

/** Route to the correct workbench setup based on install mode. */
async function setupWorkbench(config: E2EConfig): Promise<void> {
    if (config.installMode === 'docker') {
        await setupWorkbenchDocker();
    } else {
        await setupWorkbenchRPM(config.repoChannel, config.platformImage);
    }
}

function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

async function isHttpServiceReady(url: string): Promise<boolean> {
    try {
        const res = await fetch(url);
        return res.status < 500;
    } catch {
        return false;
    }
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

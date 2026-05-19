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
    // 2. Authenticate as admin via API
    // -------------------------------------------------------
    // Brief delay after health check to let the server finish
    // any remaining startup work (rate limiter init, etc.).
    await sleep(2_000);

    let cookie: string;
    try {
        const result = await api.login(
            ADMIN_USER.username,
            ADMIN_USER.password,
        );
        cookie = result.cookie;
    } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        throw new Error(
            `[E2E setup] Admin login failed for user ` +
            `"${ADMIN_USER.username}" at ${API_URL}: ${msg}`,
        );
    }

    // Expose the raw cookie to helpers that run outside of a
    // Playwright browser context (e.g. ApiHelper in test hooks).
    process.env.E2E_ADMIN_COOKIE = cookie;

    // -------------------------------------------------------
    // 3. Save browser storage state for Playwright contexts
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
    // 4. Start notification mock services
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

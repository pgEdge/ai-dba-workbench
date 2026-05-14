/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Playwright E2E config
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { defineConfig, devices } from '@playwright/test';

const previewPort = process.env.E2E_PREVIEW_PORT ?? '4173';
const baseURL = `http://127.0.0.1:${previewPort}`;

export default defineConfig({
    testDir: './tests',
    fullyParallel: false,
    workers: 1,
    forbidOnly: Boolean(process.env.CI),
    retries: process.env.CI ? 2 : 0,
    reporter: process.env.CI
        ? [['list'], ['html', { open: 'never' }]]
        : [['list']],
    use: {
        baseURL,
        trace: 'on-first-retry',
        screenshot: 'only-on-failure',
        video: 'retain-on-failure',
        actionTimeout: 10_000,
        navigationTimeout: 15_000,
    },
    projects: [
        // One setup project per browser. The matching browser
        // project depends on its setup, so the storage state is
        // written using the same browser the dependent specs use.
        // The CI matrix installs only one browser per leg, so a
        // single chromium-pinned setup would 404 on firefox/webkit
        // legs.
        {
            name: 'setup-chromium',
            testMatch: /.*\.setup\.ts/,
            use: { ...devices['Desktop Chrome'] },
        },
        {
            name: 'setup-firefox',
            testMatch: /.*\.setup\.ts/,
            use: { ...devices['Desktop Firefox'] },
        },
        {
            name: 'setup-webkit',
            testMatch: /.*\.setup\.ts/,
            use: { ...devices['Desktop Safari'] },
        },
        {
            name: 'chromium',
            use: { ...devices['Desktop Chrome'] },
            dependencies: ['setup-chromium'],
        },
        {
            name: 'firefox',
            use: { ...devices['Desktop Firefox'] },
            dependencies: ['setup-firefox'],
        },
        {
            name: 'webkit',
            use: { ...devices['Desktop Safari'] },
            dependencies: ['setup-webkit'],
        },
    ],
});

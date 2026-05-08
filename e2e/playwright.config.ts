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
        // The setup project runs once and writes
        // `.auth/admin.json`, which the app-shell and admin-panel
        // specs reuse via `test.use({ storageState: ... })`. Each
        // browser project depends on it so the storage file always
        // exists before any spec that loads it runs.
        {
            name: 'setup',
            testMatch: /.*\.setup\.ts/,
        },
        {
            name: 'chromium',
            use: { ...devices['Desktop Chrome'] },
            dependencies: ['setup'],
        },
        {
            name: 'firefox',
            use: { ...devices['Desktop Firefox'] },
            dependencies: ['setup'],
        },
        {
            name: 'webkit',
            use: { ...devices['Desktop Safari'] },
            dependencies: ['setup'],
        },
    ],
});

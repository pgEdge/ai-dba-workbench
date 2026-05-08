/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
    plugins: [react()],
    test: {
        environment: 'jsdom',
        globals: true,
        setupFiles: ['./src/test/setup.ts'],
        include: ['src/**/*.{test,spec}.{js,jsx,ts,tsx}'],
        coverage: {
            // 'lcov' is required by the Codacy coverage upload step
            // in .github/workflows/ci-client.yml; it writes
            // coverage/lcov.info which the codacy-coverage-reporter
            // consumes. 'text' prints the per-file table during
            // local `make coverage` runs; 'json' and 'html' feed the
            // browser-viewable report under coverage/.
            reporter: ['text', 'json', 'html', 'lcov'],
            exclude: [
                'node_modules/',
                'src/test/',
            ],
        },
    },
});

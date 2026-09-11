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
            // Coverage gate: the run fails when any total drops below
            // these floors. The project floor is 90% (CLAUDE.md,
            // Tests); the values below are pinned at the totals
            // measured on 11 September 2026 so the gate holds the
            // line today and is raised towards 90 as coverage grows.
            thresholds: {
                lines: 90,
                statements: 88,
                functions: 86,
                branches: 77,
            },
        },
    },
});

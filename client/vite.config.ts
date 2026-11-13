/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { defineConfig, type ProxyOptions } from 'vite';
import react from '@vitejs/plugin-react';

const apiTarget = process.env.E2E_SERVER_URL ?? 'http://localhost:8080';

const apiProxy: Record<string, ProxyOptions> = {
    '/api': {
        target: apiTarget,
        changeOrigin: true,
        cookieDomainRewrite: '',
        configure: (proxy) => {
            proxy.on('proxyReq', (proxyReq, req) => {
                if (req.headers.cookie) {
                    proxyReq.setHeader('Cookie', req.headers.cookie);
                }
            });
        },
    },
};

export default defineConfig({
    plugins: [react()],
    resolve: {
        alias: [
            // Icons are imported one per file, for example
            // '@mui/icons-material/Add'. That path is the package's
            // CommonJS build, and because the client package is
            // "type": "module" the bundler applies Node interop and
            // the default import becomes the whole exports object
            // (React error #130 at render). Point every per-icon
            // path at the ES module copy under esm/ instead.
            // TypeScript still reads the types from the root .d.ts
            // files, and Vitest uses vitest.config.js, so tests keep
            // loading the CommonJS files natively.
            {
                find: /^@mui\/icons-material\/(?!esm\/)(.+)$/,
                replacement: '@mui/icons-material/esm/$1',
            },
        ],
    },
    server: {
        host: '127.0.0.1',
        port: 5173,
        proxy: apiProxy,
    },
    preview: {
        host: '127.0.0.1',
        port: 4173,
        proxy: apiProxy,
    },
    build: {
        outDir: 'dist',
        sourcemap: true,
    },
});

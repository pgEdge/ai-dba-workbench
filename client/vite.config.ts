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
    server: {
        port: 5173,
        proxy: apiProxy,
    },
    preview: {
        port: 4173,
        proxy: apiProxy,
    },
    build: {
        outDir: 'dist',
        sourcemap: true,
    },
});

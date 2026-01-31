/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
    plugins: [react()],
    server: {
        port: 5173,
        proxy: {
            // Proxy API endpoints to the server
            '/api': {
                target: 'http://localhost:8080',
                changeOrigin: true,
                // Ensure cookies are forwarded through the proxy
                cookieDomainRewrite: '',
                // Configure proxy to handle credentials
                configure: (proxy) => {
                    proxy.on('proxyReq', (proxyReq, req) => {
                        // Forward cookies from browser to server
                        if (req.headers.cookie) {
                            proxyReq.setHeader('Cookie', req.headers.cookie);
                        }
                    });
                },
            },
        },
    },
    build: {
        outDir: 'dist',
        sourcemap: true,
    },
});

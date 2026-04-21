/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import { formatConnectionContext } from '../connectionContext';

describe('formatConnectionContext', () => {
    describe('with empty or minimal context', () => {
        it('returns empty string for empty context', () => {
            expect(formatConnectionContext({})).toBe('');
        });

        it('returns empty string when no recognized fields exist', () => {
            expect(formatConnectionContext({ foo: 'bar', baz: 123 })).toBe('');
        });
    });

    describe('postgresql section', () => {
        it('formats PostgreSQL version', () => {
            const ctx = {
                postgresql: { version: '16.2' },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('PostgreSQL Version: 16.2');
        });

        it('formats max_connections', () => {
            const ctx = {
                postgresql: { max_connections: 100 },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Max Connections: 100');
        });

        it('formats installed extensions', () => {
            const ctx = {
                postgresql: {
                    installed_extensions: ['pg_stat_statements', 'pgcrypto', 'uuid-ossp'],
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Installed Extensions: pg_stat_statements, pgcrypto, uuid-ossp');
        });

        it('formats key settings', () => {
            const ctx = {
                postgresql: {
                    settings: {
                        shared_buffers: '256MB',
                        work_mem: '4MB',
                    },
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Key Settings:');
            expect(result).toContain('shared_buffers = 256MB');
            expect(result).toContain('work_mem = 4MB');
        });

        it('skips settings section when settings is empty', () => {
            const ctx = {
                postgresql: {
                    version: '16.2',
                    settings: {},
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).not.toContain('Key Settings:');
        });

        it('formats complete postgresql context', () => {
            const ctx = {
                postgresql: {
                    version: '16.2',
                    max_connections: 200,
                    installed_extensions: ['postgis'],
                    settings: {
                        max_wal_size: '1GB',
                    },
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('PostgreSQL Version: 16.2');
            expect(result).toContain('Max Connections: 200');
            expect(result).toContain('Installed Extensions: postgis');
            expect(result).toContain('Key Settings:');
            expect(result).toContain('max_wal_size = 1GB');
        });
    });

    describe('system section', () => {
        it('formats OS name and version', () => {
            const ctx = {
                system: {
                    os_name: 'Ubuntu',
                    os_version: '22.04',
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('OS: Ubuntu 22.04');
        });

        it('formats OS name without version', () => {
            const ctx = {
                system: {
                    os_name: 'macOS',
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('OS: macOS');
        });

        it('formats architecture', () => {
            const ctx = {
                system: { architecture: 'aarch64' },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Architecture: aarch64');
        });

        it('formats CPU model', () => {
            const ctx = {
                system: {
                    cpu: { model: 'Apple M2 Pro' },
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('CPU: Apple M2 Pro');
        });

        it('formats CPU cores with logical processors', () => {
            const ctx = {
                system: {
                    cpu: {
                        cores: 8,
                        logical_processors: 16,
                    },
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('CPU Cores: 8 (16 logical)');
        });

        it('formats CPU cores without logical_processors', () => {
            const ctx = {
                system: {
                    cpu: {
                        cores: 4,
                    },
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('CPU Cores: 4 (4 logical)');
        });

        it('formats total memory in GB', () => {
            const ctx = {
                system: {
                    memory: {
                        total_bytes: 17179869184, // 16 GB
                    },
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Total Memory: 16.0 GB');
        });

        it('formats memory with decimal precision', () => {
            const ctx = {
                system: {
                    memory: {
                        total_bytes: 34359738368, // 32 GB
                    },
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Total Memory: 32.0 GB');
        });

        it('formats disk information', () => {
            const ctx = {
                system: {
                    disks: [
                        {
                            mount_point: '/',
                            total_bytes: 536870912000, // ~500 GB
                            used_bytes: 214748364800, // ~200 GB
                        },
                    ],
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Disk /: 200.0/500.0 GB used');
        });

        it('formats multiple disks', () => {
            const ctx = {
                system: {
                    disks: [
                        {
                            mount_point: '/',
                            total_bytes: 107374182400, // 100 GB
                            used_bytes: 53687091200, // 50 GB
                        },
                        {
                            mount_point: '/data',
                            total_bytes: 1099511627776, // 1 TB
                            used_bytes: 549755813888, // 0.5 TB
                        },
                    ],
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Disk /: 50.0/100.0 GB used');
            expect(result).toContain('Disk /data: 512.0/1024.0 GB used');
        });

        it('handles empty disks array', () => {
            const ctx = {
                system: {
                    disks: [],
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toBe('');
        });
    });

    describe('combined context', () => {
        it('formats both postgresql and system sections', () => {
            const ctx = {
                postgresql: {
                    version: '15.4',
                    max_connections: 150,
                },
                system: {
                    os_name: 'Debian',
                    os_version: '12',
                    architecture: 'x86_64',
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('Server Context:');
            expect(result).toContain('PostgreSQL Version: 15.4');
            expect(result).toContain('Max Connections: 150');
            expect(result).toContain('OS: Debian 12');
            expect(result).toContain('Architecture: x86_64');
        });

        it('starts with newline and Server Context header', () => {
            const ctx = {
                postgresql: { version: '16.0' },
            };
            const result = formatConnectionContext(ctx);
            expect(result.startsWith('\nServer Context:\n')).toBe(true);
        });
    });

    describe('edge cases', () => {
        it('handles undefined nested objects gracefully', () => {
            const ctx = {
                postgresql: undefined,
                system: undefined,
            };
            const result = formatConnectionContext(ctx as Record<string, unknown>);
            expect(result).toBe('');
        });

        it('handles missing cpu/memory/disks in system', () => {
            const ctx = {
                system: {
                    os_name: 'Linux',
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).toContain('OS: Linux');
            expect(result).not.toContain('CPU');
            expect(result).not.toContain('Memory');
            expect(result).not.toContain('Disk');
        });

        it('handles memory without total_bytes', () => {
            const ctx = {
                system: {
                    memory: {
                        available_bytes: 1000000,
                    },
                },
            };
            const result = formatConnectionContext(ctx);
            expect(result).not.toContain('Total Memory');
        });
    });
});

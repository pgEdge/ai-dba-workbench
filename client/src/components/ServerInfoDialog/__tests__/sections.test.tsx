/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderWithTheme } from '../../../test/renderWithTheme';
import {
    SystemSection,
    PostgreSQLSection,
    DatabasesSection,
    ConfigurationSection,
} from '../sections';
import type {
    SystemInfo,
    PostgreSQLInfo,
    DatabaseInfoItem,
    ExtensionInfoItem,
    AIAnalysisInfo,
    SettingInfoItem,
} from '../serverInfoTypes';

describe('ServerInfoDialog sections', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
    });

    // -----------------------------------------------------------------------
    // SystemSection
    // -----------------------------------------------------------------------

    describe('SystemSection', () => {
        const baseSystemInfo: SystemInfo = {
            os_name: 'Linux',
            os_version: '6.1.0',
            architecture: 'x86_64',
            hostname: 'db-server-01',
            cpu_model: 'Intel Xeon E5-2680 v4',
            cpu_cores: 8,
            cpu_logical: 16,
            cpu_clock_speed: 2400000000,
            memory_total_bytes: 17179869184,
            memory_used_bytes: 8589934592,
            memory_free_bytes: 8589934592,
            swap_total_bytes: 4294967296,
            swap_used_bytes: 1073741824,
            disks: [
                {
                    mount_point: '/',
                    filesystem_type: 'ext4',
                    total_bytes: 107374182400,
                    used_bytes: 53687091200,
                    free_bytes: 53687091200,
                },
            ],
        };

        it('renders the section title', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('System & Hardware')).toBeInTheDocument();
        });

        it('displays operating system with version', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('Linux 6.1.0')).toBeInTheDocument();
        });

        it('displays architecture', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('x86_64')).toBeInTheDocument();
        });

        it('displays hostname', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('db-server-01')).toBeInTheDocument();
        });

        it('displays CPU model', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('Intel Xeon E5-2680 v4')).toBeInTheDocument();
        });

        it('displays CPU cores with logical count', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('8 physical / 16 logical')).toBeInTheDocument();
        });

        it('displays clock speed formatted as GHz', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('2.40 GHz')).toBeInTheDocument();
        });

        it('displays memory usage bar', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('Memory')).toBeInTheDocument();
            expect(screen.getByText(/8\.0 GB/)).toBeInTheDocument();
            expect(screen.getByText(/16\.0 GB/)).toBeInTheDocument();
        });

        it('displays swap usage bar when swap is available', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('Swap')).toBeInTheDocument();
        });

        it('does not display swap when swap_total_bytes is null', () => {
            const systemWithoutSwap = { ...baseSystemInfo, swap_total_bytes: null };
            renderWithTheme(<SystemSection system={systemWithoutSwap} />);

            expect(screen.queryByText('Swap')).not.toBeInTheDocument();
        });

        it('does not display swap when swap_total_bytes is 0', () => {
            const systemWithoutSwap = { ...baseSystemInfo, swap_total_bytes: 0 };
            renderWithTheme(<SystemSection system={systemWithoutSwap} />);

            expect(screen.queryByText('Swap')).not.toBeInTheDocument();
        });

        it('displays disk information', () => {
            renderWithTheme(<SystemSection system={baseSystemInfo} />);

            expect(screen.getByText('/')).toBeInTheDocument();
            expect(screen.getByText('ext4')).toBeInTheDocument();
        });

        it('does not display disks section when disks is null', () => {
            const systemWithoutDisks = { ...baseSystemInfo, disks: null };
            renderWithTheme(<SystemSection system={systemWithoutDisks} />);

            expect(screen.queryByText('Disks')).not.toBeInTheDocument();
        });

        it('does not display disks section when disks is empty', () => {
            const systemWithoutDisks = { ...baseSystemInfo, disks: [] };
            renderWithTheme(<SystemSection system={systemWithoutDisks} />);

            expect(screen.queryByText('Disks')).not.toBeInTheDocument();
        });

        it('displays multiple disks', () => {
            const systemWithMultipleDisks = {
                ...baseSystemInfo,
                disks: [
                    {
                        mount_point: '/',
                        filesystem_type: 'ext4',
                        total_bytes: 107374182400,
                        used_bytes: 53687091200,
                        free_bytes: 53687091200,
                    },
                    {
                        mount_point: '/data',
                        filesystem_type: 'xfs',
                        total_bytes: 1099511627776,
                        used_bytes: 549755813888,
                        free_bytes: 549755813888,
                    },
                ],
            };
            renderWithTheme(<SystemSection system={systemWithMultipleDisks} />);

            expect(screen.getByText('/')).toBeInTheDocument();
            expect(screen.getByText('/data')).toBeInTheDocument();
            expect(screen.getByText('ext4')).toBeInTheDocument();
            expect(screen.getByText('xfs')).toBeInTheDocument();
        });

        it('handles null fields gracefully', () => {
            const minimalSystem: SystemInfo = {
                os_name: null,
                os_version: null,
                architecture: null,
                hostname: null,
                cpu_model: null,
                cpu_cores: null,
                cpu_logical: null,
                cpu_clock_speed: null,
                memory_total_bytes: null,
                memory_used_bytes: null,
                memory_free_bytes: null,
                swap_total_bytes: null,
                swap_used_bytes: null,
                disks: null,
            };

            // Should render without crashing
            renderWithTheme(<SystemSection system={minimalSystem} />);

            expect(screen.getByText('System & Hardware')).toBeInTheDocument();
        });

        it('displays only OS name when version is null', () => {
            const systemNoVersion = { ...baseSystemInfo, os_version: null };
            renderWithTheme(<SystemSection system={systemNoVersion} />);

            expect(screen.getByText('Linux')).toBeInTheDocument();
        });

        it('displays cores without logical when cpu_logical is null', () => {
            const systemNoLogical = { ...baseSystemInfo, cpu_logical: null };
            renderWithTheme(<SystemSection system={systemNoLogical} />);

            expect(screen.getByText('8 physical')).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // PostgreSQLSection
    // -----------------------------------------------------------------------

    describe('PostgreSQLSection', () => {
        const basePgInfo: PostgreSQLInfo = {
            version: '16.3',
            cluster_name: 'main',
            data_directory: '/var/lib/postgresql/16/main',
            max_connections: 100,
            max_wal_senders: 10,
            max_replication_slots: 10,
        };

        it('renders the section title', () => {
            renderWithTheme(<PostgreSQLSection postgresql={basePgInfo} />);

            expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
        });

        it('displays version', () => {
            renderWithTheme(<PostgreSQLSection postgresql={basePgInfo} />);

            expect(screen.getByText('16.3')).toBeInTheDocument();
        });

        it('displays cluster name', () => {
            renderWithTheme(<PostgreSQLSection postgresql={basePgInfo} />);

            expect(screen.getByText('main')).toBeInTheDocument();
        });

        it('displays data directory', () => {
            renderWithTheme(<PostgreSQLSection postgresql={basePgInfo} />);

            expect(screen.getByText('/var/lib/postgresql/16/main')).toBeInTheDocument();
        });

        it('displays max connections', () => {
            renderWithTheme(<PostgreSQLSection postgresql={basePgInfo} />);

            expect(screen.getByText('100')).toBeInTheDocument();
        });

        it('displays max WAL senders', () => {
            renderWithTheme(<PostgreSQLSection postgresql={basePgInfo} />);

            expect(screen.getByText('Max WAL Senders')).toBeInTheDocument();
        });

        it('displays max replication slots', () => {
            renderWithTheme(<PostgreSQLSection postgresql={basePgInfo} />);

            expect(screen.getByText('Max Replication Slots')).toBeInTheDocument();
        });

        it('handles null fields gracefully', () => {
            const minimalPg: PostgreSQLInfo = {
                version: null,
                cluster_name: null,
                data_directory: null,
                max_connections: null,
                max_wal_senders: null,
                max_replication_slots: null,
            };

            renderWithTheme(<PostgreSQLSection postgresql={minimalPg} />);

            expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
        });

        it('does not show version label when version is null', () => {
            const pgNoVersion = { ...basePgInfo, version: null };
            renderWithTheme(<PostgreSQLSection postgresql={pgNoVersion} />);

            expect(screen.queryByText('Version')).not.toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // DatabasesSection
    // -----------------------------------------------------------------------

    describe('DatabasesSection', () => {
        const baseDatabases: DatabaseInfoItem[] = [
            {
                name: 'appdb',
                size_bytes: 1073741824,
                encoding: 'UTF8',
                connection_limit: -1,
                extensions: ['pgcrypto', 'uuid-ossp'],
            },
            {
                name: 'analytics',
                size_bytes: 5368709120,
                encoding: 'UTF8',
                connection_limit: 50,
                extensions: ['pg_stat_statements'],
            },
        ];

        const baseExtensions: ExtensionInfoItem[] = [
            { name: 'pgcrypto', version: '1.3', schema: 'public', database: 'appdb' },
            { name: 'uuid-ossp', version: '1.1', schema: 'public', database: 'appdb' },
            { name: 'pg_stat_statements', version: '1.10', schema: 'public', database: 'analytics' },
        ];

        const baseExtsByDb: Record<string, ExtensionInfoItem[]> = {
            appdb: [
                { name: 'pgcrypto', version: '1.3', schema: 'public', database: 'appdb' },
                { name: 'uuid-ossp', version: '1.1', schema: 'public', database: 'appdb' },
            ],
            analytics: [
                { name: 'pg_stat_statements', version: '1.10', schema: 'public', database: 'analytics' },
            ],
        };

        const baseAiAnalysis: AIAnalysisInfo = {
            databases: {
                appdb: 'This is a primary application database with crypto support.',
                analytics: 'This analytics database uses pg_stat_statements for monitoring.',
            },
            generated_at: '2025-06-15T10:35:00Z',
        };

        it('renders the section title', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('Databases')).toBeInTheDocument();
        });

        it('displays database count badge', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('2')).toBeInTheDocument();
        });

        it('displays database names', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('appdb')).toBeInTheDocument();
            expect(screen.getByText('analytics')).toBeInTheDocument();
        });

        it('displays database sizes', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('1.0 GB')).toBeInTheDocument();
            expect(screen.getByText('5.0 GB')).toBeInTheDocument();
        });

        it('displays database encoding', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            const encodings = screen.getAllByText('UTF8');
            expect(encodings.length).toBe(2);
        });

        it('displays connection limit when set', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('limit: 50')).toBeInTheDocument();
        });

        it('does not display connection limit when -1', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            // Only one limit should be shown (analytics with 50)
            const limits = screen.queryAllByText(/limit:/);
            expect(limits.length).toBe(1);
        });

        it('displays extensions for each database', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('pgcrypto')).toBeInTheDocument();
            expect(screen.getByText('uuid-ossp')).toBeInTheDocument();
            expect(screen.getByText('pg_stat_statements')).toBeInTheDocument();
        });

        it('displays extension versions', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('1.3')).toBeInTheDocument();
            expect(screen.getByText('1.1')).toBeInTheDocument();
            expect(screen.getByText('1.10')).toBeInTheDocument();
        });

        it('displays AI analysis when provided', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={baseAiAnalysis}
                    aiLoading={false}
                />
            );

            expect(screen.getByText(/primary application database with crypto support/)).toBeInTheDocument();
            expect(screen.getByText(/analytics database uses pg_stat_statements/)).toBeInTheDocument();
        });

        it('shows loading skeleton when AI is loading', () => {
            const { container } = renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={true}
                />
            );

            const skeletons = container.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });

        it('does not show AI box when AI analysis is null and not loading', () => {
            const { container } = renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={baseExtsByDb}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            const skeletons = container.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBe(0);
        });

        it('handles null extsByDb gracefully', () => {
            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={null}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('appdb')).toBeInTheDocument();
            // Extensions should not be shown
            expect(screen.queryByText('pgcrypto')).not.toBeInTheDocument();
        });

        it('handles empty extensions for a database', () => {
            const extsByDbPartial = {
                appdb: [],
            };

            renderWithTheme(
                <DatabasesSection
                    databases={baseDatabases}
                    extsByDb={extsByDbPartial}
                    aiAnalysis={null}
                    aiLoading={false}
                />
            );

            expect(screen.getByText('appdb')).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // ConfigurationSection
    // -----------------------------------------------------------------------

    describe('ConfigurationSection', () => {
        const baseSettings: SettingInfoItem[] = [
            { name: 'shared_buffers', setting: '4096', unit: 'MB', category: 'Resource Usage / Memory' },
            { name: 'work_mem', setting: '64', unit: 'MB', category: 'Resource Usage / Memory' },
            { name: 'wal_level', setting: 'replica', unit: null, category: 'Write-Ahead Log' },
            { name: 'max_connections', setting: '100', unit: null, category: 'Connections and Authentication' },
        ];

        const settingsByCategory: Record<string, SettingInfoItem[]> = {
            'Resource Usage / Memory': [
                { name: 'shared_buffers', setting: '4096', unit: 'MB', category: 'Resource Usage / Memory' },
                { name: 'work_mem', setting: '64', unit: 'MB', category: 'Resource Usage / Memory' },
            ],
            'Write-Ahead Log': [
                { name: 'wal_level', setting: 'replica', unit: null, category: 'Write-Ahead Log' },
            ],
            'Connections and Authentication': [
                { name: 'max_connections', setting: '100', unit: null, category: 'Connections and Authentication' },
            ],
        };

        it('renders the section title', () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={settingsByCategory}
                    totalCount={4}
                />
            );

            expect(screen.getByText('Configuration')).toBeInTheDocument();
        });

        it('displays total settings count badge', () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={settingsByCategory}
                    totalCount={4}
                />
            );

            expect(screen.getByText('4')).toBeInTheDocument();
        });

        it('is collapsed by default', () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={settingsByCategory}
                    totalCount={4}
                />
            );

            // Settings should not be visible initially
            expect(screen.queryByText('shared_buffers')).not.toBeVisible();
        });

        it('expands on click', async () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={settingsByCategory}
                    totalCount={4}
                />
            );

            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                expect(screen.getByText('shared_buffers')).toBeVisible();
            });
        });

        it('displays category headers', async () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={settingsByCategory}
                    totalCount={4}
                />
            );

            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                expect(screen.getByText('Resource Usage / Memory')).toBeInTheDocument();
                expect(screen.getByText('Write-Ahead Log')).toBeInTheDocument();
                expect(screen.getByText('Connections and Authentication')).toBeInTheDocument();
            });
        });

        it('displays setting names', async () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={settingsByCategory}
                    totalCount={4}
                />
            );

            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                expect(screen.getByText('shared_buffers')).toBeInTheDocument();
                expect(screen.getByText('work_mem')).toBeInTheDocument();
                expect(screen.getByText('wal_level')).toBeInTheDocument();
                expect(screen.getByText('max_connections')).toBeInTheDocument();
            });
        });

        it('displays formatted setting values with units', async () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={settingsByCategory}
                    totalCount={4}
                />
            );

            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                // 4096 MB = 4.0 GB, 64 MB = 64.0 MB
                expect(screen.getByText('4.0 GB')).toBeInTheDocument();
            });
            expect(screen.getByText('64.0 MB')).toBeInTheDocument();
        });

        it('displays setting values without units', async () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={settingsByCategory}
                    totalCount={4}
                />
            );

            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                expect(screen.getByText('replica')).toBeInTheDocument();
            });
        });

        it('handles empty settings by category', () => {
            renderWithTheme(
                <ConfigurationSection
                    settingsByCategory={{}}
                    totalCount={0}
                />
            );

            expect(screen.getByText('Configuration')).toBeInTheDocument();
            expect(screen.getByText('0')).toBeInTheDocument();
        });
    });
});

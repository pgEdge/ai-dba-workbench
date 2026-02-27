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
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ServerInfoDialog from '../ServerInfoDialog';
import * as apiClientModule from '../../utils/apiClient';

// Mock the apiClient module
vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
}));

const theme = createTheme();
const mockApiGet = apiClientModule.apiGet as ReturnType<typeof vi.fn>;

/**
 * Helper to render the ServerInfoDialog wrapped in a ThemeProvider.
 */
function renderWithTheme(ui: React.ReactElement) {
    return render(<ThemeProvider theme={theme}>{ui}</ThemeProvider>);
}

/** Build a valid ServerInfoResponse with sensible defaults. */
function makeServerInfoResponse(overrides: Record<string, unknown> = {}) {
    return {
        connection_id: 1,
        collected_at: '2025-06-15T10:30:00Z',
        system: {
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
        },
        postgresql: {
            version: '16.3',
            cluster_name: 'main',
            data_directory: '/var/lib/postgresql/16/main',
            max_connections: 100,
            max_wal_senders: 10,
            max_replication_slots: 10,
        },
        databases: [
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
        ],
        extensions: [
            { name: 'pgcrypto', version: '1.3', schema: 'public', database: 'appdb' },
            { name: 'uuid-ossp', version: '1.1', schema: 'public', database: 'appdb' },
            { name: 'pg_stat_statements', version: '1.10', schema: 'public', database: 'analytics' },
        ],
        key_settings: [
            { name: 'shared_buffers', setting: '4096', unit: 'MB', category: 'Resource Usage / Memory' },
            { name: 'work_mem', setting: '64', unit: 'MB', category: 'Resource Usage / Memory' },
            { name: 'wal_level', setting: 'replica', unit: null, category: 'Write-Ahead Log' },
        ],
        ai_analysis: null,
        ...overrides,
    };
}

/** Build a valid AIAnalysisInfo response. */
function makeAiAnalysisResponse(overrides: Record<string, unknown> = {}) {
    return {
        databases: {
            appdb: 'This is a primary application database with crypto and UUID support.',
            analytics: 'This analytics database uses pg_stat_statements for query monitoring.',
        },
        generated_at: '2025-06-15T10:35:00Z',
        ...overrides,
    };
}

const defaultProps = {
    open: true,
    onClose: vi.fn(),
    connectionId: 1,
    serverName: 'Production Server',
};

describe('ServerInfoDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        // By default, the server-info endpoint resolves with full data
        // and AI analysis resolves with null (no analysis).
        mockApiGet.mockImplementation((url: string) => {
            if (url.includes('/ai-analysis')) {
                return Promise.resolve(makeAiAnalysisResponse());
            }
            return Promise.resolve(makeServerInfoResponse());
        });
    });

    // -----------------------------------------------------------------------
    // Rendering
    // -----------------------------------------------------------------------

    describe('rendering', () => {
        it('renders the dialog title and server name when open', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);
            await waitFor(() => {
                expect(screen.getByText('Server Information: Production Server')).toBeInTheDocument();
            });
        });

        it('does not render dialog content when closed', () => {
            renderWithTheme(
                <ServerInfoDialog {...defaultProps} open={false} />
            );
            expect(screen.queryByText('Server Information: Production Server')).not.toBeInTheDocument();
        });

        it('does not fetch data when closed', () => {
            renderWithTheme(
                <ServerInfoDialog {...defaultProps} open={false} />
            );
            expect(mockApiGet).not.toHaveBeenCalled();
        });
    });

    // -----------------------------------------------------------------------
    // Loading state
    // -----------------------------------------------------------------------

    describe('loading state', () => {
        it('shows skeleton elements while loading', async () => {
            // Create a promise that never resolves during this test
            let resolvePromise: (value: unknown) => void;
            mockApiGet.mockReturnValue(
                new Promise((resolve) => {
                    resolvePromise = resolve;
                })
            );

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            // The LoadingSkeleton renders multiple MUI Skeleton elements
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);

            // Clean up by resolving
            resolvePromise!(makeServerInfoResponse());
        });
    });

    // -----------------------------------------------------------------------
    // Error state
    // -----------------------------------------------------------------------

    describe('error state', () => {
        it('shows error message when fetch fails', async () => {
            mockApiGet.mockRejectedValue(new Error('Network error'));

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Network error')).toBeInTheDocument();
            });
        });

        it('shows fallback error message for non-Error exceptions', async () => {
            mockApiGet.mockRejectedValue('something went wrong');

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(
                    screen.getByText('Failed to load server information')
                ).toBeInTheDocument();
            });
        });
    });

    // -----------------------------------------------------------------------
    // Data display: System & Hardware
    // -----------------------------------------------------------------------

    describe('System & Hardware section', () => {
        it('displays operating system information', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('System & Hardware')).toBeInTheDocument();
            });
            expect(screen.getByText('Linux 6.1.0')).toBeInTheDocument();
        });

        it('displays architecture', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('x86_64')).toBeInTheDocument();
            });
        });

        it('displays hostname', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('db-server-01')).toBeInTheDocument();
            });
        });

        it('displays CPU model', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Intel Xeon E5-2680 v4')).toBeInTheDocument();
            });
        });

        it('displays CPU cores', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('8 physical / 16 logical')).toBeInTheDocument();
            });
        });

        it('displays clock speed formatted as GHz', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('2.40 GHz')).toBeInTheDocument();
            });
        });

        it('displays disk mount point and filesystem type', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('/')).toBeInTheDocument();
            });
            expect(screen.getByText('ext4')).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // Data display: PostgreSQL
    // -----------------------------------------------------------------------

    describe('PostgreSQL section', () => {
        it('displays PostgreSQL version', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
            });
            expect(screen.getByText('16.3')).toBeInTheDocument();
        });

        it('displays cluster name', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('main')).toBeInTheDocument();
            });
        });

        it('displays data directory', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('/var/lib/postgresql/16/main')).toBeInTheDocument();
            });
        });

        it('displays max connections', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('100')).toBeInTheDocument();
            });
        });

        it('displays max WAL senders and replication slots', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Max WAL Senders')).toBeInTheDocument();
            });
            expect(screen.getByText('Max Replication Slots')).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // Data display: Databases
    // -----------------------------------------------------------------------

    describe('Databases section', () => {
        it('displays database names', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('appdb')).toBeInTheDocument();
            });
            expect(screen.getByText('analytics')).toBeInTheDocument();
        });

        it('displays database count badge', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Databases')).toBeInTheDocument();
            });
            expect(screen.getByText('2')).toBeInTheDocument();
        });

        it('displays database sizes formatted as bytes', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('1.0 GB')).toBeInTheDocument();
            });
            expect(screen.getByText('5.0 GB')).toBeInTheDocument();
        });

        it('displays database encoding', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                const encodings = screen.getAllByText('UTF8');
                expect(encodings.length).toBe(2);
            });
        });

        it('displays extensions for each database', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('pgcrypto')).toBeInTheDocument();
            });
            expect(screen.getByText('uuid-ossp')).toBeInTheDocument();
            expect(screen.getByText('pg_stat_statements')).toBeInTheDocument();
        });

        it('displays extension versions', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('1.3')).toBeInTheDocument();
            });
            expect(screen.getByText('1.1')).toBeInTheDocument();
            expect(screen.getByText('1.10')).toBeInTheDocument();
        });

        it('displays connection limit for databases', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('limit: 50')).toBeInTheDocument();
            });
        });
    });

    // -----------------------------------------------------------------------
    // Data display: Configuration
    // -----------------------------------------------------------------------

    describe('Configuration section', () => {
        it('displays configuration section header with count badge', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Configuration')).toBeInTheDocument();
            });
            expect(screen.getByText('3')).toBeInTheDocument();
        });

        it('displays settings after expanding the section', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Configuration')).toBeInTheDocument();
            });

            // Configuration is defaultOpen=false, so click to expand
            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                expect(screen.getByText('shared_buffers')).toBeInTheDocument();
            });
            expect(screen.getByText('work_mem')).toBeInTheDocument();
            expect(screen.getByText('wal_level')).toBeInTheDocument();
        });

        it('displays setting values and units', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Configuration')).toBeInTheDocument();
            });

            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                expect(screen.getByText('4.0 GB')).toBeInTheDocument();
            });
            expect(screen.getByText('replica')).toBeInTheDocument();
        });

        it('groups settings by category', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Configuration')).toBeInTheDocument();
            });

            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                expect(screen.getByText('Resource Usage / Memory')).toBeInTheDocument();
            });
            expect(screen.getByText('Write-Ahead Log')).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // Collapsible sections
    // -----------------------------------------------------------------------

    describe('collapsible sections', () => {
        it('collapses System & Hardware section on click', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('System & Hardware')).toBeInTheDocument();
            });

            // CPU model should be visible initially (section is open by default)
            expect(screen.getByText('Intel Xeon E5-2680 v4')).toBeInTheDocument();

            // Click the section header to collapse
            fireEvent.click(screen.getByText('System & Hardware'));

            // After collapse, the content should not be visible
            await waitFor(() => {
                expect(screen.getByText('Intel Xeon E5-2680 v4')).not.toBeVisible();
            });
        });

        it('expands Configuration section on click', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('Configuration')).toBeInTheDocument();
            });

            // Configuration is closed by default
            // Click to expand
            fireEvent.click(screen.getByText('Configuration'));

            await waitFor(() => {
                expect(screen.getByText('shared_buffers')).toBeVisible();
            });
        });

        it('toggles section open and closed', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
            });

            // Close the section
            fireEvent.click(screen.getByText('PostgreSQL'));

            await waitFor(() => {
                expect(screen.getByText('16.3')).not.toBeVisible();
            });

            // Reopen the section
            fireEvent.click(screen.getByText('PostgreSQL'));

            await waitFor(() => {
                expect(screen.getByText('16.3')).toBeVisible();
            });
        });
    });

    // -----------------------------------------------------------------------
    // AI Analysis
    // -----------------------------------------------------------------------

    describe('AI analysis', () => {
        it('shows AI skeleton placeholder while AI is loading', async () => {
            // Server info resolves immediately; AI analysis never resolves
            let resolveAi: (value: unknown) => void;
            mockApiGet.mockImplementation((url: string) => {
                if (url.includes('/ai-analysis')) {
                    return new Promise((resolve) => {
                        resolveAi = resolve;
                    });
                }
                return Promise.resolve(makeServerInfoResponse());
            });

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            // Wait for data to load
            await waitFor(() => {
                expect(screen.getByText('appdb')).toBeInTheDocument();
            });

            // AI skeletons should be visible in the Databases section
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);

            // Clean up
            resolveAi!(makeAiAnalysisResponse());
        });

        it('shows AI analysis descriptions when loaded', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(
                    screen.getByText(
                        'This is a primary application database with crypto and UUID support.'
                    )
                ).toBeInTheDocument();
            });
            expect(
                screen.getByText(
                    'This analytics database uses pg_stat_statements for query monitoring.'
                )
            ).toBeInTheDocument();
        });

        it('does not show AI box when analysis fails', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url.includes('/ai-analysis')) {
                    return Promise.reject(new Error('AI unavailable'));
                }
                return Promise.resolve(makeServerInfoResponse());
            });

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('appdb')).toBeInTheDocument();
            });

            // Wait a tick for the AI promise to settle
            await waitFor(() => {
                expect(
                    screen.queryByText(
                        'This is a primary application database with crypto and UUID support.'
                    )
                ).not.toBeInTheDocument();
            });
        });
    });

    // -----------------------------------------------------------------------
    // Close button
    // -----------------------------------------------------------------------

    describe('close button', () => {
        it('calls onClose when the Close button is clicked', async () => {
            const onClose = vi.fn();
            renderWithTheme(
                <ServerInfoDialog {...defaultProps} onClose={onClose} />
            );

            await waitFor(() => {
                expect(screen.getByText('Server Information: Production Server')).toBeInTheDocument();
            });

            // Click the close button in the app bar
            fireEvent.click(screen.getByRole('button', { name: 'close server info' }));

            expect(onClose).toHaveBeenCalledTimes(1);
        });

        it('calls onClose when the X icon button is clicked', async () => {
            const onClose = vi.fn();
            renderWithTheme(
                <ServerInfoDialog {...defaultProps} onClose={onClose} />
            );

            await waitFor(() => {
                expect(screen.getByText('Server Information: Production Server')).toBeInTheDocument();
            });

            // The IconButton with CloseIcon is the first button in the header
            const buttons = screen.getAllByRole('button');
            // The X button contains a CloseIcon SVG
            const closeIconButton = buttons.find(
                (btn) => btn.querySelector('[data-testid="CloseIcon"]')
            );
            expect(closeIconButton).toBeTruthy();
            fireEvent.click(closeIconButton!);

            expect(onClose).toHaveBeenCalledTimes(1);
        });
    });

    // -----------------------------------------------------------------------
    // Collected-at footer
    // -----------------------------------------------------------------------

    describe('footer', () => {
        it('displays the data collection timestamp', async () => {
            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText(/Data collected/)).toBeInTheDocument();
            });
        });
    });

    // -----------------------------------------------------------------------
    // Edge cases
    // -----------------------------------------------------------------------

    describe('edge cases', () => {
        it('handles null system info gracefully', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url.includes('/ai-analysis')) {
                    return Promise.resolve({ databases: {}, generated_at: '' });
                }
                return Promise.resolve(
                    makeServerInfoResponse({ system: null })
                );
            });

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
            });

            // System section should not appear
            expect(screen.queryByText('System & Hardware')).not.toBeInTheDocument();
        });

        it('handles null postgresql info gracefully', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url.includes('/ai-analysis')) {
                    return Promise.resolve({ databases: {}, generated_at: '' });
                }
                return Promise.resolve(
                    makeServerInfoResponse({ postgresql: null })
                );
            });

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('System & Hardware')).toBeInTheDocument();
            });

            // PostgreSQL section should not appear
            expect(screen.queryByText('PostgreSQL')).not.toBeInTheDocument();
        });

        it('handles empty databases array', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url.includes('/ai-analysis')) {
                    return Promise.resolve({ databases: {}, generated_at: '' });
                }
                return Promise.resolve(
                    makeServerInfoResponse({ databases: [] })
                );
            });

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
            });

            expect(screen.queryByText('Databases')).not.toBeInTheDocument();
        });

        it('handles null key_settings', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url.includes('/ai-analysis')) {
                    return Promise.resolve({ databases: {}, generated_at: '' });
                }
                return Promise.resolve(
                    makeServerInfoResponse({ key_settings: null })
                );
            });

            renderWithTheme(<ServerInfoDialog {...defaultProps} />);

            await waitFor(() => {
                expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
            });

            expect(screen.queryByText('Configuration')).not.toBeInTheDocument();
        });
    });
});

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
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import SystemResourcesSection from '../SystemResourcesSection';
import type { UseMetricsReturn } from '../../../../hooks/useMetrics';
import type { MetricQueryParams, MetricSeries } from '../../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type UseMetricsFn = (params: MetricQueryParams | null) => UseMetricsReturn;

const mockUseMetrics = vi.fn<UseMetricsFn>();
vi.mock('../../../../hooks/useMetrics', () => ({
    useMetrics: (params: MetricQueryParams | null) => mockUseMetrics(params),
}));

vi.mock('../../../../contexts/useDashboard', () => ({
    useDashboard: () => ({
        timeRange: { range: '1h' },
        refreshTrigger: 0,
    }),
}));

vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({
        capabilities: null,
        isLoading: false,
        error: null,
        refetch: vi.fn(),
        isAIEnabled: false,
    }),
}));

// Mock the Chart component to avoid ApexCharts dependencies. The
// series names and the analysis description are exposed as attributes
// so the tests can assert on what the section actually charts.
vi.mock('../../../Chart', () => ({
    Chart: ({ title, data, analysisContext }: {
        title: string;
        data: { series: { name: string }[] };
        analysisContext?: { metricDescription: string };
    }) => (
        <div
            data-testid="chart"
            data-series={data.series.map(s => s.name).join('|')}
            data-description={analysisContext?.metricDescription ?? ''}
        >
            {title}
        </div>
    ),
}));

// The disk mount lookup goes straight through apiGet rather than
// useMetrics, so it is mocked separately.
const mockApiGet = vi.fn();
vi.mock('../../../../utils/apiClient', () => ({
    apiGet: (url: string, options?: { signal?: AbortSignal }) =>
        mockApiGet(url, options) as unknown,
}));

vi.mock('../../../../utils/logger', () => ({
    logger: {
        error: vi.fn(),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a mock UseMetricsReturn with loading state */
const loadingMetrics = (): UseMetricsReturn => ({
    data: null,
    window: null,
    loading: true,
    error: null,
    refetch: vi.fn(),
});

/** Build a mock UseMetricsReturn with empty data (no system_stats) */
const emptyMetrics = (): UseMetricsReturn => ({
    data: [],
    window: null,
    loading: false,
    error: null,
    refetch: vi.fn(),
});

/** Build a mock UseMetricsReturn with data that has all zeros (no system_stats) */
const zeroMetrics = (): UseMetricsReturn => ({
    data: [
        {
            name: 'idle_mode_percent',
            metric: 'idle_mode_percent',
            data: [
                { time: '2024-01-01T00:00:00Z', value: 0 },
                { time: '2024-01-01T00:01:00Z', value: 0 },
            ],
        },
    ] as MetricSeries[],
    window: null,
    loading: false,
    error: null,
    refetch: vi.fn(),
});

/** Build a mock UseMetricsReturn with real system stats data */
const realMetrics = (): UseMetricsReturn => ({
    data: [
        {
            name: 'idle_mode_percent',
            metric: 'idle_mode_percent',
            data: [
                { time: '2024-01-01T00:00:00Z', value: 85.5 },
                { time: '2024-01-01T00:01:00Z', value: 90.2 },
            ],
        },
        {
            name: 'usermode_normal_process_percent',
            metric: 'usermode_normal_process_percent',
            data: [
                { time: '2024-01-01T00:00:00Z', value: 10.2 },
                { time: '2024-01-01T00:01:00Z', value: 8.5 },
            ],
        },
    ] as MetricSeries[],
    window: null,
    loading: false,
    error: null,
    refetch: vi.fn(),
});

/** Bucket times shared by every fixture series */
const T0 = '2024-01-01T00:00:00Z';
const T1 = '2024-01-01T00:01:00Z';

/** CPU series with real values, which is what hasSystemStats keys off */
const cpuSeries = (): MetricSeries[] => ([
    {
        name: 'idle_mode_percent',
        metric: 'idle_mode_percent',
        data: [{ time: T0, value: 85.5 }, { time: T1, value: 90.2 }],
    },
    {
        name: 'usermode_normal_process_percent',
        metric: 'usermode_normal_process_percent',
        data: [{ time: T0, value: 10.2 }, { time: T1, value: 8.5 }],
    },
] as MetricSeries[]);

/**
 * Memory series. `available` selects the available_memory shape:
 * 'absent' omits the series entirely, as an older collector would,
 * 'null' returns it with only null buckets, and an array supplies
 * real values.
 */
const memorySeries = (
    available: 'absent' | 'null' | number[],
): MetricSeries[] => {
    const series: MetricSeries[] = [
        {
            name: 'used_memory',
            metric: 'used_memory',
            data: [{ time: T0, value: 4e9 }, { time: T1, value: 5e9 }],
        },
        {
            name: 'total_memory',
            metric: 'total_memory',
            data: [{ time: T0, value: 16e9 }, { time: T1, value: 16e9 }],
        },
        {
            name: 'free_memory',
            metric: 'free_memory',
            data: [{ time: T0, value: 2e9 }, { time: T1, value: 1e9 }],
        },
        {
            name: 'cache_total',
            metric: 'cache_total',
            data: [{ time: T0, value: 9e9 }, { time: T1, value: 1e10 }],
        },
    ] as MetricSeries[];

    if (available !== 'absent') {
        const values = available === 'null' ? [null, null] : available;
        series.push({
            name: 'available_memory',
            metric: 'available_memory',
            data: [
                { time: T0, value: values[0] },
                { time: T1, value: values[1] },
            ],
        } as MetricSeries);
    }

    return series;
};

/**
 * Drive useMetrics so memory probes get the memory fixture and every
 * other probe gets the CPU fixture, which keeps hasSystemStats true.
 */
const mockMemory = (available: 'absent' | 'null' | number[]): void => {
    mockUseMetrics.mockImplementation((params) => ({
        data: params?.probeName === 'pg_sys_memory_info'
            ? memorySeries(available)
            : cpuSeries(),
        loading: false,
        error: null,
        refetch: vi.fn(),
    }));
};

/** The memory chart node, identified by its title text */
const memoryChart = (): HTMLElement => screen
    .getAllByTestId('chart')
    .find(el => el.textContent === 'Memory Usage Over Time') as HTMLElement;

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

/** Build a pg_sys_disk_info latest row for the mount lookup. */
const diskRow = (
    mountPoint: string,
    usedSpace: number,
    fileSystemType: string | null = 'ext4',
) => ({
    mount_point: mountPoint,
    file_system_type: fileSystemType,
    total_space: 1000,
    used_space: usedSpace,
    free_space: 1000 - usedSpace,
});

/** The parameters of every useMetrics call naming the disk probe. */
const diskCallParams = (): MetricQueryParams[] => mockUseMetrics.mock.calls
    .map(([params]) => params)
    .filter((params): params is MetricQueryParams =>
        params?.probeName === 'pg_sys_disk_info');

describe('SystemResourcesSection', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        vi.mocked(localStorage.setItem).mockClear();
        mockApiGet.mockResolvedValue([]);
    });

    describe('force collapse behavior', () => {
        it('shows expanded section with loading spinner during initial load', () => {
            mockUseMetrics.mockReturnValue(loadingMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Section should be visible with title
            expect(screen.getByText('System Resources')).toBeInTheDocument();

            // Should show loading indicator
            expect(screen.getByLabelText('Loading')).toBeInTheDocument();

            // Anti-flicker contract: while loading the section MUST stay
            // expanded and MUST NOT display the force-collapsed message.
            // This guards against forceCollapsed being applied before
            // the initial fetch resolves.
            const header = screen.getByRole('button', {
                name: /collapse system resources section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'true');
            expect(
                screen.queryByText(/No data available/),
            ).not.toBeInTheDocument();
        });

        it('forces collapse when system_stats extension is not installed (empty data)', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Wait for render to stabilize
            await waitFor(() => {
                // Section title should still be visible
                expect(screen.getByText('System Resources')).toBeInTheDocument();
            });

            // Should show the force collapsed message
            expect(
                screen.getByText(/No data available.*system_stats/i),
            ).toBeInTheDocument();

            // Header should indicate collapsed state
            const header = screen.getByRole('button', {
                name: /expand system resources section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'false');

            // Charts should NOT be rendered (collapsed)
            expect(screen.queryByTestId('chart')).not.toBeInTheDocument();
        });

        it('forces collapse when all metric values are zero', async () => {
            mockUseMetrics.mockReturnValue(zeroMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('System Resources')).toBeInTheDocument();
            });

            // Should show the force collapsed message
            expect(
                screen.getByText(/No data available.*system_stats/i),
            ).toBeInTheDocument();

            // Header should indicate collapsed state
            const header = screen.getByRole('button', {
                name: /expand system resources section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'false');
        });

        it('stays expanded when system_stats data is available', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('System Resources')).toBeInTheDocument();
            });

            // Should NOT show the force collapsed message
            expect(
                screen.queryByText(/No data available.*system_stats/i),
            ).not.toBeInTheDocument();

            // Header should indicate expanded state
            const header = screen.getByRole('button', {
                name: /collapse system resources section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // KPI tiles should be visible
            expect(screen.getByText('CPU Usage')).toBeInTheDocument();
            expect(screen.getByText('Memory Usage')).toBeInTheDocument();
            expect(screen.getByText('Disk Usage')).toBeInTheDocument();
            expect(screen.getByText('Load Average')).toBeInTheDocument();
        });

        it('does not modify localStorage when force collapsed', async () => {
            // Start with stored expanded state
            vi.mocked(localStorage.getItem).mockReturnValue('true');
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(
                    screen.getByText(/No data available.*system_stats/i),
                ).toBeInTheDocument();
            });

            // localStorage should NOT have been modified
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });

        it('allows user to manually expand a force-collapsed section', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Wait for force-collapse to apply after initial load
            await waitFor(() => {
                expect(
                    screen.getByText(/No data available.*system_stats/i),
                ).toBeInTheDocument();
            });

            // Section starts collapsed, KPI tiles hidden
            expect(screen.queryByText('CPU Usage')).not.toBeInTheDocument();

            // User clicks to expand
            const header = screen.getByRole('button', {
                name: /expand system resources section/i,
            });
            fireEvent.click(header);

            // After expand: aria-expanded flips, KPI tiles render, and
            // the force-collapsed message disappears from the header.
            expect(header).toHaveAttribute('aria-expanded', 'true');
            expect(screen.getByText('CPU Usage')).toBeInTheDocument();
            expect(
                screen.queryByText(/No data available.*system_stats/i),
            ).not.toBeInTheDocument();

            // localStorage MUST NOT be touched - the override is in-memory.
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });
    });

    describe('section title and icon', () => {
        it('renders section with Computer icon', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('System Resources')).toBeInTheDocument();
            });

            // MUI icons render as SVGs with data-testid
            expect(screen.getByTestId('ComputerIcon')).toBeInTheDocument();
        });
    });

    describe('KPI tiles', () => {
        it('renders the exact force-collapsed message wired by the section', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Must use the precise message the production component
            // passes to CollapsibleSection. A regression that swaps
            // the wording would make this assertion fail.
            await waitFor(() => {
                expect(
                    screen.getByText(
                        'No data available. Is the system_stats extension installed?',
                    ),
                ).toBeInTheDocument();
            });
        });

        it('shows placeholder dashes for KPIs when forced to expand on empty data', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // Wait for initial load + force-collapse to settle.
            await waitFor(() => {
                expect(
                    screen.getByText(/No data available.*system_stats/i),
                ).toBeInTheDocument();
            });

            // Manually expand to surface the KPI tiles.
            fireEvent.click(
                screen.getByRole('button', {
                    name: /expand system resources section/i,
                }),
            );

            // With no system_stats data, Memory/Disk/Load all render '--'.
            expect(screen.getByText('Memory Usage')).toBeInTheDocument();
            expect(screen.getByText('Disk Usage')).toBeInTheDocument();
            expect(screen.getByText('Load Average')).toBeInTheDocument();
            // Three placeholders: Memory, Disk, Load all show '--'.
            expect(screen.getAllByText('--').length).toBeGreaterThanOrEqual(3);
        });
    });

    describe('available memory', () => {
        it('requests available_memory for both the KPI and the chart', async () => {
            mockMemory([13.3e9, 13.3e9]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(mockUseMetrics).toHaveBeenCalled();
            });
            const requested = mockUseMetrics.mock.calls
                .filter(([params]) => params?.probeName === 'pg_sys_memory_info')
                .map(([params]) => params?.metrics ?? []);
            expect(requested).toHaveLength(2);
            requested.forEach(metrics => {
                expect(metrics).toContain('available_memory');
            });
        });

        it('charts a fourth Available (est.) series and says so in the analysis context', async () => {
            mockMemory([13.3e9, 13.3e9]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(memoryChart()).toBeInTheDocument();
            });
            expect(memoryChart()).toHaveAttribute(
                'data-series',
                'Used|Free|Cached|Available (est.)',
            );
            expect(memoryChart().getAttribute('data-description'))
                .toMatch(/estimated available/i);
        });

        it('explains the estimate in a caption beneath the memory chart', async () => {
            mockMemory([13.3e9, 13.3e9]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(memoryChart()).toBeInTheDocument();
            });
            expect(
                screen.getByText(/estimate of free memory plus reclaimable/i),
            ).toBeInTheDocument();
        });

        it('shows the available figure as the memory tile secondary line', async () => {
            mockMemory([13.3e9, 13.3e9]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('Memory Usage')).toBeInTheDocument();
            });
            expect(screen.getByText('12.4 GB available (est.)'))
                .toBeInTheDocument();
            expect(
                screen.getByLabelText(/12\.4 GB available \(est\.\)/),
            ).toBeInTheDocument();
        });

        it('omits the secondary line when available_memory is all null', async () => {
            mockMemory('null');

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('Memory Usage')).toBeInTheDocument();
            });
            expect(screen.queryByText(/available \(est\.\)/))
                .not.toBeInTheDocument();
            // The series is still charted, as a gap rather than a zero.
            expect(memoryChart()).toHaveAttribute(
                'data-series',
                'Used|Free|Cached|Available (est.)',
            );
        });

        it('omits the secondary line when the collector sends no available_memory', async () => {
            mockMemory('absent');

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('Memory Usage')).toBeInTheDocument();
            });
            expect(screen.queryByText(/available \(est\.\)/))
                .not.toBeInTheDocument();
        });

        it('shows neither the secondary line nor the caption without system_stats', async () => {
            mockUseMetrics.mockReturnValue(emptyMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(
                    screen.getByText(/No data available.*system_stats/i),
                ).toBeInTheDocument();
            });

            // Expand manually so the tiles and chart panels render.
            fireEvent.click(
                screen.getByRole('button', {
                    name: /expand system resources section/i,
                }),
            );

            expect(screen.queryByText(/available \(est\.\)/))
                .not.toBeInTheDocument();
            expect(
                screen.queryByText(/estimate of free memory plus reclaimable/i),
            ).not.toBeInTheDocument();
        });
    });

    describe('network chart', () => {
        it('requests per-second network metrics', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(mockUseMetrics).toHaveBeenCalled();
            });
            const requested = mockUseMetrics.mock.calls
                .map(([params]) => (params?.metrics ?? []).join(','));
            expect(requested).toContain('tx_bytes_per_sec,rx_bytes_per_sec');
        });

        it('reports a network query error in place of the empty message', async () => {
            mockUseMetrics.mockImplementation((params) => {
                const key = (params?.metrics ?? []).join(',');
                if (key === 'tx_bytes_per_sec,rx_bytes_per_sec') {
                    return {
                        data: null,
                        window: null,
                        loading: false,
                        error: 'metric not found in probe',
                        refetch: vi.fn(),
                    };
                }
                return realMetrics();
            });

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('metric not found in probe'))
                    .toBeInTheDocument();
            });
        });
    });

    describe('disk mount selection', () => {
        it('defaults to the fullest real filesystem', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());
            mockApiGet.mockResolvedValue([
                diskRow('/', 100),
                diskRow('/var/lib/postgresql', 900),
                diskRow('/dev/shm', 950, 'tmpfs'),
            ]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('Disk Usage (/var/lib/postgresql)'))
                    .toBeInTheDocument();
            });
            expect(
                screen.getByText('Disk Space Over Time (/var/lib/postgresql)'),
            ).toBeInTheDocument();
        });

        it('sends the selected mount with both disk queries', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());
            mockApiGet.mockResolvedValue([
                diskRow('/', 100),
                diskRow('/data', 900),
            ]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(diskCallParams().some(p => p.mountPoint === '/data'))
                    .toBe(true);
            });

            const withMount = diskCallParams()
                .filter(p => p.mountPoint === '/data');
            // The KPI query asks for the total so the percentage can be
            // taken against it rather than against used plus free.
            expect(withMount.some(
                p => p.metrics?.includes('total_space') === true
                    && p.buckets === 30,
            )).toBe(true);
            expect(withMount.some(p => p.buckets === 150)).toBe(true);
        });

        it('hides the selector when only one real filesystem exists', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());
            mockApiGet.mockResolvedValue([
                diskRow('/', 400),
                diskRow('/dev/shm', 950, 'tmpfs'),
                diskRow('/snap/core', 1000, 'squashfs'),
            ]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('Disk Usage (/)')).toBeInTheDocument();
            });
            expect(screen.queryByLabelText('Filesystem'))
                .not.toBeInTheDocument();
        });

        it('offers the real filesystems when there are several', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());
            mockApiGet.mockResolvedValue([
                diskRow('/', 100),
                diskRow('/data', 900),
            ]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            const select = await screen.findByLabelText('Filesystem');
            fireEvent.mouseDown(select);

            const options = await screen.findAllByRole('option');
            expect(options.map(o => o.textContent)).toEqual(['/data', '/']);
        });

        it('re-queries the newly selected mount', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());
            mockApiGet.mockResolvedValue([
                diskRow('/', 100),
                diskRow('/data', 900),
            ]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            const select = await screen.findByLabelText('Filesystem');
            fireEvent.mouseDown(select);
            fireEvent.click(await screen.findByRole('option', { name: '/' }));

            await waitFor(() => {
                expect(screen.getByText('Disk Usage (/)')).toBeInTheDocument();
            });
            expect(diskCallParams().some(p => p.mountPoint === '/')).toBe(true);
        });

        it('falls back to an empty state when no real mount is found', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());
            mockApiGet.mockResolvedValue([
                diskRow('/dev/shm', 950, 'tmpfs'),
                { ...diskRow('/broken', 0), total_space: 0 },
            ]);

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => {
                expect(screen.getByText('Disk Usage')).toBeInTheDocument();
            });
            expect(screen.getByText('Disk Space Over Time'))
                .toBeInTheDocument();
            expect(screen.queryByLabelText('Filesystem'))
                .not.toBeInTheDocument();

            // The empty state must say what is actually wrong rather
            // than blaming an extension that is plainly working.
            expect(
                screen.getByText('No real filesystem reported for this server.'),
            ).toBeInTheDocument();

            // Reverting to the averaged query is exactly the behaviour
            // issue #428 removes, so no disk query may have fired.
            expect(diskCallParams()).toHaveLength(0);
        });

        it('issues no disk query until the mount lookup resolves', async () => {
            mockUseMetrics.mockReturnValue(realMetrics());
            let resolveMounts: (rows: unknown) => void = () => {};
            mockApiGet.mockImplementation(() => new Promise((resolve) => {
                resolveMounts = resolve;
            }));

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            await waitFor(() => { expect(mockApiGet).toHaveBeenCalled(); });

            // Whilst the lookup is in flight the panel must show the
            // loading state, not an averaged figure and not an empty
            // state that would read as a missing extension.
            expect(diskCallParams()).toHaveLength(0);
            expect(screen.getAllByLabelText('Loading chart').length)
                .toBeGreaterThan(0);
            expect(
                screen.queryByText('No real filesystem reported for this server.'),
            ).not.toBeInTheDocument();

            resolveMounts([diskRow('/data', 700)]);

            await waitFor(() => {
                expect(diskCallParams().some(p => p.mountPoint === '/data'))
                    .toBe(true);
            });
        });

        it('takes the percentage from the reported total', async () => {
            mockApiGet.mockResolvedValue([diskRow('/data', 800)]);
            mockUseMetrics.mockImplementation((params) => {
                if (params?.probeName === 'pg_sys_disk_info'
                    && params.buckets === 30) {
                    return {
                        data: [
                            {
                                name: 'used_space',
                                metric: 'used_space',
                                data: [{ time: 't', value: 800 }],
                            },
                            {
                                name: 'free_space',
                                metric: 'free_space',
                                data: [{ time: 't', value: 100 }],
                            },
                            {
                                name: 'total_space',
                                metric: 'total_space',
                                data: [{ time: 't', value: 1000 }],
                            },
                        ] as MetricSeries[],
                        loading: false,
                        error: null,
                        refetch: vi.fn(),
                    };
                }
                return realMetrics();
            });

            render(
                <SystemResourcesSection
                    connectionId={1}
                    connectionName="Test Server"
                />,
            );

            // 800 / 1000 is 80.0; used over used-plus-free would have
            // given 88.9 on a filesystem with reserved blocks.
            await waitFor(() => {
                expect(screen.getByText('80.0')).toBeInTheDocument();
            });
        });
    });
});

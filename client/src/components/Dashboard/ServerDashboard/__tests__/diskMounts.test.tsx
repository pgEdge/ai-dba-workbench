/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { DiskMountRow } from '../diskMounts';
import {
    PSEUDO_FILESYSTEM_TYPES,
    isRealFilesystem,
    mountUsageFraction,
    selectRealMounts,
    useDiskMounts,
} from '../diskMounts';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockApiGet = vi.fn();
vi.mock('../../../../utils/apiClient', () => ({
    apiGet: (url: string, options?: { signal?: AbortSignal }) =>
        mockApiGet(url, options) as unknown,
}));

const mockLoggerError = vi.fn();
vi.mock('../../../../utils/logger', () => ({
    logger: {
        error: (...args: unknown[]) => mockLoggerError(...args),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a mount row with sensible real-filesystem defaults. */
const row = (overrides: Partial<DiskMountRow> = {}): DiskMountRow => ({
    mount_point: '/',
    file_system_type: 'ext4',
    total_space: 1000,
    used_space: 500,
    free_space: 500,
    ...overrides,
});

/** Deferred settlement handles for each queued apiGet call. */
interface Deferred {
    resolve: (value: unknown) => void;
    reject: (reason: unknown) => void;
}

/**
 * Make apiGet return promises the test settles by hand, so that every
 * assertion follows an observed state transition rather than the
 * initial pre-fetch render.
 */
const deferApiGet = (): Deferred[] => {
    const deferrals: Deferred[] = [];
    mockApiGet.mockImplementation(() => new Promise((resolve, reject) => {
        deferrals.push({ resolve, reject });
    }));
    return deferrals;
};

/** Probe component rendering what the hook returns. */
const MountProbe = ({ connectionId }: { connectionId: number }) => {
    const { mounts, settled } = useDiskMounts(connectionId);
    return (
        <>
            <div data-testid="mounts">
                {mounts.map(m => m.mount_point).join(',')}
            </div>
            <div data-testid="settled">{settled ? 'settled' : 'pending'}</div>
        </>
    );
};

const mountText = (): string =>
    screen.getByTestId('mounts').textContent ?? '';

const settledText = (): string =>
    screen.getByTestId('settled').textContent ?? '';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('diskMounts helpers', () => {
    describe('PSEUDO_FILESYSTEM_TYPES', () => {
        it('covers the kernel and image backed filesystem types', () => {
            expect(PSEUDO_FILESYSTEM_TYPES.size).toBe(23);
            for (const type of ['tmpfs', 'devtmpfs', 'squashfs', 'overlay',
                'binfmt_misc', 'bpf', 'nsfs']) {
                expect(PSEUDO_FILESYSTEM_TYPES.has(type)).toBe(true);
            }
            expect(PSEUDO_FILESYSTEM_TYPES.has('ext4')).toBe(false);
        });
    });

    describe('isRealFilesystem', () => {
        it('accepts a real filesystem with a positive total', () => {
            expect(isRealFilesystem(row())).toBe(true);
        });

        it('rejects a pseudo filesystem type', () => {
            expect(isRealFilesystem(row({ file_system_type: 'tmpfs' })))
                .toBe(false);
        });

        it('accepts a NULL filesystem type with a positive total', () => {
            expect(isRealFilesystem(row({ file_system_type: null })))
                .toBe(true);
        });

        it('accepts a missing filesystem type with a positive total', () => {
            expect(isRealFilesystem({ mount_point: '/', total_space: 10 }))
                .toBe(true);
        });

        it('rejects a zero total, which yields no usable fraction', () => {
            expect(isRealFilesystem(row({ total_space: 0 }))).toBe(false);
        });

        it('rejects a negative total', () => {
            expect(isRealFilesystem(row({ total_space: -1 }))).toBe(false);
        });

        it('rejects a NULL total', () => {
            expect(isRealFilesystem(row({ total_space: null }))).toBe(false);
        });

        it('rejects a missing total', () => {
            expect(isRealFilesystem({ mount_point: '/' })).toBe(false);
        });
    });

    describe('mountUsageFraction', () => {
        it('divides used by total', () => {
            expect(mountUsageFraction(row({ total_space: 200, used_space: 50 })))
                .toBeCloseTo(0.25);
        });

        it('returns zero when the total is not usable', () => {
            expect(mountUsageFraction(row({ total_space: 0 }))).toBe(0);
            expect(mountUsageFraction(row({ total_space: null }))).toBe(0);
        });

        it('returns zero when used space is missing', () => {
            expect(mountUsageFraction(row({ used_space: null }))).toBe(0);
        });
    });

    describe('selectRealMounts', () => {
        it('drops pseudo mounts and zero-total mounts', () => {
            const mounts = selectRealMounts([
                row({ mount_point: '/dev', file_system_type: 'devtmpfs' }),
                row({ mount_point: '/snap', file_system_type: 'squashfs' }),
                row({ mount_point: '/empty', total_space: 0 }),
                row({ mount_point: '/data' }),
            ]);
            expect(mounts.map(m => m.mount_point)).toEqual(['/data']);
        });

        it('sorts by usage fraction descending', () => {
            const mounts = selectRealMounts([
                row({ mount_point: '/a', used_space: 100 }),
                row({ mount_point: '/b', used_space: 900 }),
                row({ mount_point: '/c', used_space: 500 }),
            ]);
            expect(mounts.map(m => m.mount_point)).toEqual(['/b', '/c', '/a']);
        });

        it('breaks ties on the mount point ascending', () => {
            const mounts = selectRealMounts([
                row({ mount_point: '/wal', used_space: 500 }),
                row({ mount_point: '/data', used_space: 500 }),
            ]);
            expect(mounts.map(m => m.mount_point)).toEqual(['/data', '/wal']);
        });

        it('drops rows without a usable mount point', () => {
            const mounts = selectRealMounts([
                row({ mount_point: null }),
                row({ mount_point: '' }),
                row({ mount_point: '/keep' }),
            ]);
            expect(mounts.map(m => m.mount_point)).toEqual(['/keep']);
        });

        it('returns an empty list for a non-array payload', () => {
            expect(selectRealMounts(null)).toEqual([]);
            expect(selectRealMounts(undefined)).toEqual([]);
        });
    });
});

describe('useDiskMounts', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('queries the latest pg_sys_disk_info rows for the connection', async () => {
        mockApiGet.mockResolvedValue([row()]);

        render(<MountProbe connectionId={7} />);

        await waitFor(() => { expect(mockApiGet).toHaveBeenCalled(); });
        const url = mockApiGet.mock.calls[0][0] as string;
        expect(url).toContain('/api/v1/metrics/query?');
        expect(url).toContain('probe_name=pg_sys_disk_info');
        expect(url).toContain('connection_id=7');
        expect(url).toContain('limit=100');
        expect(url).toContain('order_by=collected_at');
        expect(url).toContain('order=desc');
    });

    it('returns only the real mounts, fullest first', async () => {
        mockApiGet.mockResolvedValue([
            row({ mount_point: '/dev', file_system_type: 'tmpfs' }),
            row({ mount_point: '/data', used_space: 100 }),
            row({ mount_point: '/wal', used_space: 900 }),
        ]);

        render(<MountProbe connectionId={1} />);

        await waitFor(() => {
            expect(mountText()).toBe('/wal,/data');
        });
    });

    it('aborts the in-flight request when the connection changes', async () => {
        const deferrals = deferApiGet();

        const { rerender } = render(<MountProbe connectionId={1} />);
        await waitFor(() => { expect(deferrals).toHaveLength(1); });

        const firstSignal = (
            mockApiGet.mock.calls[0][1] as { signal: AbortSignal }
        ).signal;
        expect(firstSignal.aborted).toBe(false);

        rerender(<MountProbe connectionId={2} />);
        expect(firstSignal.aborted).toBe(true);
        await waitFor(() => { expect(deferrals).toHaveLength(2); });

        // A late response from the abandoned connection must not be
        // adopted, even though the promise still settles.
        deferrals[0].resolve([row({ mount_point: '/stale' })]);
        deferrals[1].resolve([row({ mount_point: '/fresh' })]);

        await waitFor(() => { expect(mountText()).toBe('/fresh'); });
    });

    it('reports an unsettled lookup whilst a new connection is in flight', async () => {
        const deferrals = deferApiGet();

        const { rerender } = render(<MountProbe connectionId={1} />);
        await waitFor(() => { expect(deferrals).toHaveLength(1); });
        deferrals[0].resolve([row({ mount_point: '/first' })]);
        await waitFor(() => { expect(mountText()).toBe('/first'); });
        expect(settledText()).toBe('settled');

        rerender(<MountProbe connectionId={2} />);

        // The previous server's mounts must not be shown against the
        // new server's data, and the gap must read as a lookup still in
        // flight rather than as a host with no filesystems.
        expect(mountText()).toBe('');
        expect(settledText()).toBe('pending');
    });

    it('is unsettled before the first response arrives', async () => {
        const deferrals = deferApiGet();

        render(<MountProbe connectionId={1} />);
        await waitFor(() => { expect(deferrals).toHaveLength(1); });

        expect(settledText()).toBe('pending');

        deferrals[0].resolve([]);
        await waitFor(() => { expect(settledText()).toBe('settled'); });
        expect(mountText()).toBe('');
    });

    it('settles with no mounts when the host reports only pseudo filesystems', async () => {
        mockApiGet.mockResolvedValue([
            row({ mount_point: '/dev/shm', file_system_type: 'tmpfs' }),
        ]);

        render(<MountProbe connectionId={1} />);

        await waitFor(() => { expect(settledText()).toBe('settled'); });
        expect(mountText()).toBe('');
    });

    it('logs and reports no mounts when the lookup fails', async () => {
        mockApiGet.mockRejectedValue(new Error('boom'));

        render(<MountProbe connectionId={1} />);

        await waitFor(() => {
            expect(mockLoggerError).toHaveBeenCalledWith(
                'Error fetching disk mounts:',
                expect.any(Error),
            );
        });
        expect(mountText()).toBe('');
        expect(settledText()).toBe('settled');
    });

    it('ignores a rejection that arrives after the request was aborted', async () => {
        const deferrals = deferApiGet();

        const { unmount } = render(<MountProbe connectionId={1} />);
        await waitFor(() => { expect(deferrals).toHaveLength(1); });

        unmount();
        deferrals[0].reject(new Error('aborted'));

        await waitFor(() => {
            expect(mockLoggerError).not.toHaveBeenCalled();
        });
    });
});

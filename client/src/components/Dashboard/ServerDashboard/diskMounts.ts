/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useEffect, useMemo, useState } from 'react';
import { apiGet } from '../../../utils/apiClient';
import { logger } from '../../../utils/logger';

/**
 * Kernel-backed and image-backed filesystem types that are never a real
 * volume a DBA provisions, and so are never worth charting. The list
 * matches the alerter's exclusion fragment in
 * alerter/src/internal/database/metric_registry.go, and the two must be
 * kept in step so that the dashboard and the alerts describe the same
 * set of filesystems.
 */
export const PSEUDO_FILESYSTEM_TYPES: ReadonlySet<string> = new Set([
    'tmpfs',
    'devtmpfs',
    'devfs',
    'proc',
    'sysfs',
    'cgroup',
    'cgroup2',
    'overlay',
    'squashfs',
    'ramfs',
    'debugfs',
    'tracefs',
    'securityfs',
    'pstore',
    'autofs',
    'mqueue',
    'hugetlbfs',
    'configfs',
    'fusectl',
    'binfmt_misc',
    'efivarfs',
    'nsfs',
    'bpf',
]);

/**
 * One latest row of the pg_sys_disk_info probe. Every field is optional
 * because the collector records whatever the platform reports, and a
 * column may be absent or NULL for a given mount.
 */
export interface DiskMountRow {
    mount_point?: string | null;
    file_system_type?: string | null;
    total_space?: number | null;
    used_space?: number | null;
    free_space?: number | null;
}

/**
 * Whether a mount describes a real filesystem worth charting: its type
 * is not a pseudo filesystem and it reports a positive total size. A
 * zero or missing total is as uninterpretable as a pseudo mount, since
 * no usage fraction can be derived from it.
 */
export const isRealFilesystem = (row: DiskMountRow): boolean => {
    const type = row.file_system_type ?? '';
    if (PSEUDO_FILESYSTEM_TYPES.has(type)) { return false; }
    if (typeof row.total_space !== 'number') { return false; }
    return row.total_space > 0;
};

/**
 * The proportion of a mount that is in use, between 0 and 1. A row
 * without a usable total or used figure yields zero rather than a
 * non-finite value, so callers need not pre-filter.
 */
export const mountUsageFraction = (row: DiskMountRow): number => {
    const total = row.total_space;
    const used = row.used_space;
    if (typeof total !== 'number' || total <= 0) { return 0; }
    if (typeof used !== 'number') { return 0; }
    return used / total;
};

/**
 * The real filesystems among the given rows, fullest first. Ties break
 * on the mount point ascending, so that the default selection is stable
 * when two mounts are equally full.
 */
export const selectRealMounts = (
    rows: DiskMountRow[] | null | undefined,
): DiskMountRow[] => {
    if (!Array.isArray(rows)) { return []; }

    return rows
        .filter(row => typeof row.mount_point === 'string'
            && row.mount_point !== ''
            && isRealFilesystem(row))
        .sort((a, b) => {
            const diff = mountUsageFraction(b) - mountUsageFraction(a);
            if (diff !== 0) { return diff; }
            return (a.mount_point ?? '').localeCompare(b.mount_point ?? '');
        });
};

/**
 * The outcome of a mount lookup. `settled` distinguishes a lookup that
 * is still in flight, where the caller should show a loading state,
 * from one that has finished and found no real filesystem, where the
 * caller should show an empty state; both carry an empty mount list,
 * and the two want different things on screen.
 */
export interface DiskMountsState {
    mounts: DiskMountRow[];
    settled: boolean;
}

/** A mount lookup result tied to the connection it came from. */
interface DiskMountsResult extends DiskMountsState {
    connectionId: number;
}

/** Maximum number of latest rows requested from the metrics API. */
const MOUNT_ROW_LIMIT = 100;

/**
 * Stable empty result, so that a render before the lookup settles does
 * not hand callers a fresh array identity on every pass.
 */
const PENDING: DiskMountsState = { mounts: [], settled: false };

/**
 * Fetch the current set of real filesystems for a connection from the
 * most recent pg_sys_disk_info rows. The latest-row mode of the metrics
 * query applies DISTINCT ON over the probe's entity keys, so it returns
 * exactly one current row per mount with every column, including the
 * filesystem type and total size that the filtering needs.
 *
 * The result carries the connection it belongs to, so that switching
 * connections never offers the previous server's mounts against the new
 * server's data whilst the fresh lookup is still in flight; a result
 * from an abandoned connection reads as unsettled rather than as a
 * host with no filesystems.
 */
export const useDiskMounts = (connectionId: number): DiskMountsState => {
    const [result, setResult] = useState<DiskMountsResult>({
        connectionId,
        ...PENDING,
    });

    useEffect(() => {
        const controller = new AbortController();
        const params = new URLSearchParams({
            probe_name: 'pg_sys_disk_info',
            connection_id: connectionId.toString(),
            limit: MOUNT_ROW_LIMIT.toString(),
            order_by: 'collected_at',
            order: 'desc',
        });

        apiGet<DiskMountRow[]>(
            `/api/v1/metrics/query?${params.toString()}`,
            { signal: controller.signal },
        )
            .then((rows) => {
                if (controller.signal.aborted) { return; }
                setResult({
                    connectionId,
                    mounts: selectRealMounts(rows),
                    settled: true,
                });
            })
            .catch((err: unknown) => {
                if (controller.signal.aborted) { return; }
                logger.error('Error fetching disk mounts:', err);
                setResult({ connectionId, mounts: [], settled: true });
            });

        return () => { controller.abort(); };
    }, [connectionId]);

    return useMemo(
        () => (result.connectionId === connectionId
            ? { mounts: result.mounts, settled: result.settled }
            : PENDING),
        [result, connectionId],
    );
};

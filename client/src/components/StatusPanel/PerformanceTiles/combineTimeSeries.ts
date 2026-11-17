/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { CheckpointTimeSeries, TransactionTimeSeries } from './types';

/**
 * Rounds to one decimal place, matching the server's rounding of each
 * sample, so that summing values such as 0.1 and 0.2 does not leave
 * floating point noise in the chart tooltip.
 */
const roundToTenth = (value: number): number => Math.round(value * 10) / 10;

/**
 * Merges several per-server time series into one series with a single
 * point per time bucket.
 *
 * The performance summary endpoint buckets every connection with
 * date_bin using the same origin and interval within one response, so
 * samples from different servers for the same bucket carry the same
 * start time. Points are grouped on that time (compared as an instant,
 * so that equal times written differently still match), and `combine`
 * turns each group into the bucket's values. The result is sorted by
 * time. A bucket missing from some servers is combined from the
 * servers that have it.
 */
export function combineTimeSeries<T extends { time: string }>(
    seriesList: ReadonlyArray<ReadonlyArray<T> | null | undefined>,
    combine: (points: T[]) => Omit<T, 'time'>,
): T[] {
    const buckets = new Map<string, { time: string; sortKey: number; points: T[] }>();

    seriesList.forEach(series => {
        series?.forEach(point => {
            const instant = Date.parse(point.time);
            const key = Number.isNaN(instant) ? point.time : String(instant);
            const bucket = buckets.get(key);
            if (bucket) {
                bucket.points.push(point);
            } else {
                buckets.set(key, { time: point.time, sortKey: instant, points: [point] });
            }
        });
    });

    return Array.from(buckets.values())
        .sort((a, b) => (
            Number.isNaN(a.sortKey) || Number.isNaN(b.sortKey)
                ? a.time.localeCompare(b.time)
                : a.sortKey - b.sortKey
        ))
        .map(bucket => ({ ...combine(bucket.points), time: bucket.time }) as T);
}

/**
 * Combines one bucket's transaction samples across servers: commits
 * per second are summed, and the rollback percentage is averaged with
 * each server's commit rate as its weight, which is how the server
 * computes the estate-wide `aggregate.rollback_percent`. When no server
 * committed anything in the bucket the weights are all zero, so the
 * plain mean is used instead.
 */
export function combineTransactionPoints(
    points: TransactionTimeSeries[],
): Omit<TransactionTimeSeries, 'time'> {
    let commits = 0;
    let weightedRollback = 0;
    let rollbackSum = 0;

    points.forEach(p => {
        commits += p.commits_per_sec;
        weightedRollback += p.rollback_percent * p.commits_per_sec;
        rollbackSum += p.rollback_percent;
    });

    const rollback = commits > 0
        ? weightedRollback / commits
        : rollbackSum / points.length;

    return {
        commits_per_sec: roundToTenth(commits),
        rollback_percent: roundToTenth(rollback),
    };
}

/**
 * Combines one bucket's checkpoint samples across servers by summing
 * the write and sync times, giving the estate's total checkpoint time
 * in that bucket.
 */
export function combineCheckpointPoints(
    points: CheckpointTimeSeries[],
): Omit<CheckpointTimeSeries, 'time'> {
    let write = 0;
    let sync = 0;

    points.forEach(p => {
        write += p.write_time_ms;
        sync += p.sync_time_ms;
    });

    return {
        write_time_ms: roundToTenth(write),
        sync_time_ms: roundToTenth(sync),
    };
}

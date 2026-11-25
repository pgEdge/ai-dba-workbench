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
import {
    combineTimeSeries,
    combineTransactionPoints,
    combineCheckpointPoints,
} from '../combineTimeSeries';
import type { CheckpointTimeSeries, TransactionTimeSeries } from '../types';

const tx = (
    time: string,
    commits_per_sec: number,
    rollback_percent: number,
): TransactionTimeSeries => ({ time, commits_per_sec, rollback_percent });

const cp = (
    time: string,
    write_time_ms: number,
    sync_time_ms: number,
): CheckpointTimeSeries => ({ time, write_time_ms, sync_time_ms });

describe('combineTimeSeries', () => {
    it('returns an empty array when there are no series', () => {
        expect(combineTimeSeries([], combineTransactionPoints)).toEqual([]);
        expect(combineTimeSeries([null, undefined, []], combineTransactionPoints)).toEqual([]);
    });

    it('leaves a single server series unchanged apart from sorting', () => {
        const series = [
            tx('2024-01-01T11:00:00Z', 150, 0.3),
            tx('2024-01-01T10:00:00Z', 100, 0.5),
            tx('2024-01-01T12:00:00Z', 0, 100),
        ];

        expect(combineTimeSeries([series], combineTransactionPoints)).toEqual([
            tx('2024-01-01T10:00:00Z', 100, 0.5),
            tx('2024-01-01T11:00:00Z', 150, 0.3),
            tx('2024-01-01T12:00:00Z', 0, 100),
        ]);
    });

    it('combines points that share a bucket and keeps the others', () => {
        const a = [cp('2024-01-01T10:00:00Z', 10, 1), cp('2024-01-01T11:00:00Z', 20, 2)];
        const b = [cp('2024-01-01T11:00:00Z', 5, 3), cp('2024-01-01T12:00:00Z', 7, 4)];

        expect(combineTimeSeries([a, null, b], combineCheckpointPoints)).toEqual([
            cp('2024-01-01T10:00:00Z', 10, 1),
            cp('2024-01-01T11:00:00Z', 25, 5),
            cp('2024-01-01T12:00:00Z', 7, 4),
        ]);
    });

    it('matches equal instants written in different forms', () => {
        const a = [cp('2024-01-01T10:00:00Z', 10, 1)];
        const b = [cp('2024-01-01T10:00:00.000Z', 5, 3)];
        const c = [cp('2024-01-01T11:00:00+01:00', 1, 1)];

        expect(combineTimeSeries([a, b, c], combineCheckpointPoints)).toEqual([
            cp('2024-01-01T10:00:00Z', 16, 5),
        ]);
    });

    it('groups and orders unparseable times by their text', () => {
        const a = [cp('b', 1, 1), cp('a', 2, 2)];
        const b = [cp('b', 3, 3)];

        expect(combineTimeSeries([a, b], combineCheckpointPoints)).toEqual([
            cp('a', 2, 2),
            cp('b', 4, 4),
        ]);
    });
});

describe('combineTransactionPoints', () => {
    it('sums commits and weights the rollback percentage by commit rate', () => {
        expect(combineTransactionPoints([
            tx('t', 300, 1),
            tx('t', 100, 5),
        ])).toEqual({ commits_per_sec: 400, rollback_percent: 2 });
    });

    it('uses the plain mean of rollback percentages when nothing committed', () => {
        expect(combineTransactionPoints([
            tx('t', 0, 100),
            tx('t', 0, 0),
        ])).toEqual({ commits_per_sec: 0, rollback_percent: 50 });
    });

    it('rounds to one decimal place', () => {
        expect(combineTransactionPoints([
            tx('t', 0.1, 0.3),
            tx('t', 0.2, 0.3),
        ])).toEqual({ commits_per_sec: 0.3, rollback_percent: 0.3 });
    });
});

describe('combineCheckpointPoints', () => {
    it('sums write and sync times, rounded to one decimal place', () => {
        expect(combineCheckpointPoints([
            cp('t', 0.1, 1.25),
            cp('t', 0.2, 2),
        ])).toEqual({ write_time_ms: 0.3, sync_time_ms: 3.3 });
    });
});

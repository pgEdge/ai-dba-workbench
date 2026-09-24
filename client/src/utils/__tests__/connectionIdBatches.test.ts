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
    chunkConnectionIds,
    MAX_CONNECTION_IDS_PER_REQUEST,
} from '../connectionIdBatches';

const range = (count: number, from = 1): number[] =>
    Array.from({ length: count }, (_, i) => from + i);

describe('MAX_CONNECTION_IDS_PER_REQUEST', () => {
    // The server rejects a longer list with a 400; the two constants
    // must stay equal. See maxConnectionIDsPerRequest in
    // server/src/internal/api/request_helpers.go.
    it('matches the server cap of 100', () => {
        expect(MAX_CONNECTION_IDS_PER_REQUEST).toBe(100);
    });
});

describe('chunkConnectionIds', () => {
    it('returns no batches for an empty list', () => {
        expect(chunkConnectionIds([])).toEqual([]);
    });

    it('returns a single batch for a list within the cap', () => {
        const ids = range(MAX_CONNECTION_IDS_PER_REQUEST);

        expect(chunkConnectionIds(ids)).toEqual([ids]);
    });

    it('splits a list one over the cap into two batches', () => {
        const ids = range(MAX_CONNECTION_IDS_PER_REQUEST + 1);
        const batches = chunkConnectionIds(ids);

        expect(batches).toHaveLength(2);
        expect(batches[0]).toHaveLength(MAX_CONNECTION_IDS_PER_REQUEST);
        expect(batches[1]).toEqual([MAX_CONNECTION_IDS_PER_REQUEST + 1]);
    });

    it('preserves order and loses no ids across batches', () => {
        const ids = range(250);

        expect(chunkConnectionIds(ids).flat()).toEqual(ids);
    });

    it('honours an explicit batch size', () => {
        expect(chunkConnectionIds([1, 2, 3, 4, 5], 2)).toEqual([
            [1, 2], [3, 4], [5],
        ]);
    });

    it('treats a batch size below one as one, so the list is consumed', () => {
        expect(chunkConnectionIds([1, 2], 0)).toEqual([[1], [2]]);
        expect(chunkConnectionIds([1, 2], -5)).toEqual([[1], [2]]);
    });

    it('floors a fractional batch size', () => {
        expect(chunkConnectionIds([1, 2, 3], 2.9)).toEqual([[1, 2], [3]]);
    });

    it('clamps a batch size above the cap to the cap', () => {
        const ids = range(MAX_CONNECTION_IDS_PER_REQUEST + 1);
        const batches = chunkConnectionIds(
            ids,
            MAX_CONNECTION_IDS_PER_REQUEST + 50,
        );

        expect(batches).toHaveLength(2);
        expect(batches[0]).toHaveLength(MAX_CONNECTION_IDS_PER_REQUEST);
        expect(batches[1]).toEqual([MAX_CONNECTION_IDS_PER_REQUEST + 1]);
    });

    it('throws rather than dropping ids for a non-finite batch size', () => {
        expect(() => chunkConnectionIds([1, 2, 3], NaN)).toThrow(RangeError);
        expect(() => chunkConnectionIds([1, 2, 3], Infinity)).toThrow(
            RangeError,
        );
        expect(() => chunkConnectionIds([1, 2, 3], -Infinity)).toThrow(
            RangeError,
        );
    });

    it('does not mutate the list it is given', () => {
        const ids = range(5);

        chunkConnectionIds(ids, 2);

        expect(ids).toEqual(range(5));
    });
});

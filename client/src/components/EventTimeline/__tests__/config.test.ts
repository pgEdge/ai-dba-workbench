/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
    getInitialTimeRange,
    TIME_RANGE_OPTIONS,
    TIME_RANGE_STORAGE_KEY,
} from '../config';

const getItem = () => localStorage.getItem as ReturnType<typeof vi.fn>;

describe('getInitialTimeRange', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('restores a stored preset', () => {
        getItem().mockReturnValue('7d');

        expect(getInitialTimeRange()).toBe('7d');
        expect(getItem()).toHaveBeenCalledWith(TIME_RANGE_STORAGE_KEY);
    });

    it('falls back to 24h when nothing is stored', () => {
        getItem().mockReturnValue(null);

        expect(getInitialTimeRange()).toBe('24h');
    });

    it.each([
        ['a serialised custom window', '{"start":"2026-01-01T00:00:00.000Z"}'],
        ['a stringified object', '[object Object]'],
        ['an unknown preset', '90d'],
    ])('falls back to 24h for %s', (_label, stored) => {
        getItem().mockReturnValue(stored);

        expect(getInitialTimeRange()).toBe('24h');
    });

    it('falls back to 24h when localStorage is unavailable', () => {
        getItem().mockImplementation(() => {
            throw new Error('denied');
        });

        expect(getInitialTimeRange()).toBe('24h');
    });

    it('offers only preset options, so a custom window is never restorable', () => {
        expect(TIME_RANGE_OPTIONS.map((option) => option.value)).toEqual([
            '1h',
            '6h',
            '24h',
            '7d',
            '30d',
        ]);
    });
});

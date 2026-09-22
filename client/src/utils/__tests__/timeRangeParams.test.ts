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
    appendTimeRangeParams,
    isTimeRangeQueryable,
} from '../timeRangeParams';

describe('isTimeRangeQueryable', () => {
    it('accepts every preset range', () => {
        expect(isTimeRangeQueryable({ range: '1h' })).toBe(true);
        expect(isTimeRangeQueryable({ range: '24h' })).toBe(true);
        expect(isTimeRangeQueryable({ range: '30d' })).toBe(true);
    });

    it('accepts a custom range with both bounds', () => {
        expect(isTimeRangeQueryable({
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
            customEnd: '2026-09-02T00:00:00Z',
        })).toBe(true);
    });

    it('rejects a custom range missing either bound', () => {
        expect(isTimeRangeQueryable({ range: 'custom' })).toBe(false);
        expect(isTimeRangeQueryable({
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
        })).toBe(false);
        expect(isTimeRangeQueryable({
            range: 'custom',
            customEnd: '2026-09-02T00:00:00Z',
        })).toBe(false);
    });

    it('rejects a custom range with an empty bound', () => {
        expect(isTimeRangeQueryable({
            range: 'custom',
            customStart: '',
            customEnd: '2026-09-02T00:00:00Z',
        })).toBe(false);
    });
});

describe('appendTimeRangeParams', () => {
    it('emits the range alone for a preset', () => {
        const params = appendTimeRangeParams(
            new URLSearchParams(), { range: '6h' },
        );

        expect(params.toString()).toBe('time_range=6h');
    });

    it('ignores stray bounds on a preset range', () => {
        const params = appendTimeRangeParams(new URLSearchParams(), {
            range: '6h',
            customStart: '2026-09-01T00:00:00Z',
            customEnd: '2026-09-02T00:00:00Z',
        });

        expect(params.get('time_start')).toBeNull();
        expect(params.get('time_end')).toBeNull();
    });

    it('emits both bounds for a custom range', () => {
        const params = appendTimeRangeParams(new URLSearchParams(), {
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
            customEnd: '2026-09-02T00:00:00Z',
        });

        expect(params.get('time_range')).toBe('custom');
        expect(params.get('time_start')).toBe('2026-09-01T00:00:00Z');
        expect(params.get('time_end')).toBe('2026-09-02T00:00:00Z');
    });

    it('omits the bounds when only one is present', () => {
        const params = appendTimeRangeParams(new URLSearchParams(), {
            range: 'custom',
            customStart: '2026-09-01T00:00:00Z',
        });

        expect(params.toString()).toBe('time_range=custom');
    });

    it('preserves existing parameters and returns the same object', () => {
        const existing = new URLSearchParams({ connection_id: '7' });

        const params = appendTimeRangeParams(existing, { range: '1h' });

        expect(params).toBe(existing);
        expect(params.toString()).toBe('connection_id=7&time_range=1h');
    });

    it('replaces a range already present rather than repeating it', () => {
        const existing = new URLSearchParams({ time_range: '1h' });

        appendTimeRangeParams(existing, { range: '24h' });

        expect(existing.getAll('time_range')).toEqual(['24h']);
    });
});

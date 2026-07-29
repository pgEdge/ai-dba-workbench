/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, afterEach } from 'vitest';
import {
    isCustomTimeRange,
    resolveTimeRangeBounds,
    type TimeRangePreset,
} from '../timelineRange';
import { getTimeRangeBounds } from '../../components/EventTimeline/utils';

const NOW = new Date('2026-03-15T12:00:00.000Z');
const HOUR_MS = 60 * 60 * 1000;

const withFrozenClock = (run: () => void): void => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    try {
        run();
    } finally {
        vi.useRealTimers();
    }
};

describe('isCustomTimeRange', () => {
    it('accepts an object carrying both bounds', () => {
        expect(isCustomTimeRange({ start: NOW, end: NOW })).toBe(true);
    });

    it.each([
        ['a preset string', '24h'],
        ['null', null],
        ['undefined', undefined],
        ['an object with only a start', { start: NOW }],
        ['an object with only an end', { end: NOW }],
    ])('rejects %s', (_label, value) => {
        expect(isCustomTimeRange(value)).toBe(false);
    });
});

describe('resolveTimeRangeBounds', () => {
    afterEach(() => {
        vi.useRealTimers();
    });

    it.each<[TimeRangePreset, number]>([
        ['1h', 1],
        ['6h', 6],
        ['24h', 24],
        ['7d', 7 * 24],
        ['30d', 30 * 24],
    ])('resolves the %s preset to a window ending now', (preset, hours) => {
        withFrozenClock(() => {
            const { startTime, endTime } = resolveTimeRangeBounds(preset);

            expect(endTime.getTime()).toBe(NOW.getTime());
            expect(endTime.getTime() - startTime.getTime()).toBe(
                hours * HOUR_MS,
            );
        });
    });

    it('falls back to 24 hours for an unrecognised preset', () => {
        withFrozenClock(() => {
            const { startTime, endTime } = resolveTimeRangeBounds(
                '90d' as TimeRangePreset,
            );

            expect(endTime.getTime() - startTime.getTime()).toBe(24 * HOUR_MS);
        });
    });

    it('returns a custom window as given', () => {
        const start = new Date('2026-01-02T03:04:05.000Z');
        const end = new Date('2026-01-03T03:04:05.000Z');

        const bounds = resolveTimeRangeBounds({ start, end });

        expect(bounds.startTime.toISOString()).toBe(start.toISOString());
        expect(bounds.endTime.toISOString()).toBe(end.toISOString());
    });

    it('coerces custom bounds held as ISO strings into dates', () => {
        const start = '2026-01-02T03:04:05.000Z';
        const end = '2026-01-03T03:04:05.000Z';

        const bounds = resolveTimeRangeBounds({
            start,
            end,
        } as unknown as { start: Date; end: Date });

        expect(bounds.startTime).toBeInstanceOf(Date);
        expect(bounds.endTime).toBeInstanceOf(Date);
        expect(bounds.startTime.toISOString()).toBe(start);
        expect(bounds.endTime.toISOString()).toBe(end);
    });
});

describe('getTimeRangeBounds', () => {
    it('delegates to the shared calculation for presets', () => {
        withFrozenClock(() => {
            expect(getTimeRangeBounds('6h')).toEqual(
                resolveTimeRangeBounds('6h'),
            );
        });
    });

    it('handles a custom window', () => {
        const start = new Date('2026-04-01T00:00:00.000Z');
        const end = new Date('2026-04-01T08:00:00.000Z');

        const bounds = getTimeRangeBounds({ start, end });

        expect(bounds.startTime.toISOString()).toBe(start.toISOString());
        expect(bounds.endTime.toISOString()).toBe(end.toISOString());
    });
});

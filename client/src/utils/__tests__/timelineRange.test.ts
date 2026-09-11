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
    validateCustomWindow,
    MAX_CUSTOM_WINDOW_DAYS,
    MAX_CUSTOM_WINDOW_MS,
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

describe('validateCustomWindow', () => {
    const DAY_MS = 24 * HOUR_MS;
    const at = (offsetMs: number): Date => new Date(NOW.getTime() + offsetMs);

    it('exposes the server cap of 366 days', () => {
        expect(MAX_CUSTOM_WINDOW_DAYS).toBe(366);
        expect(MAX_CUSTOM_WINDOW_MS).toBe(366 * DAY_MS);
    });

    it('accepts a past window shorter than the cap', () => {
        expect(validateCustomWindow(at(-6 * HOUR_MS), at(-HOUR_MS), NOW)).toBeNull();
    });

    it('accepts a window spanning exactly the cap', () => {
        expect(
            validateCustomWindow(at(-MAX_CUSTOM_WINDOW_MS), NOW, NOW),
        ).toBeNull();
    });

    it('rejects an end before the start', () => {
        expect(validateCustomWindow(at(-HOUR_MS), at(-2 * HOUR_MS), NOW)).toBe(
            'end-not-after-start',
        );
    });

    it('rejects equal timestamps', () => {
        expect(validateCustomWindow(at(-HOUR_MS), at(-HOUR_MS), NOW)).toBe(
            'end-not-after-start',
        );
    });

    it('rejects a start in the future', () => {
        expect(validateCustomWindow(at(DAY_MS), at(2 * DAY_MS), NOW)).toBe(
            'start-in-future',
        );
    });

    it('rejects a start equal to now, as the server does', () => {
        expect(validateCustomWindow(NOW, at(HOUR_MS), NOW)).toBe(
            'start-in-future',
        );
    });

    it('rejects a span longer than the cap', () => {
        expect(
            validateCustomWindow(at(-MAX_CUSTOM_WINDOW_MS - 1), NOW, NOW),
        ).toBe('span-too-long');
    });

    it('clamps a future end to now before measuring the span', () => {
        // The raw span here exceeds the cap, but the server clamps the
        // end to now first, which brings it back inside the cap.
        expect(
            validateCustomWindow(
                at(-MAX_CUSTOM_WINDOW_MS + HOUR_MS),
                at(DAY_MS),
                NOW,
            ),
        ).toBeNull();
    });

    it('defaults now to the current clock', () => {
        withFrozenClock(() => {
            expect(validateCustomWindow(at(DAY_MS), at(2 * DAY_MS))).toBe(
                'start-in-future',
            );
            expect(validateCustomWindow(at(-DAY_MS), at(-HOUR_MS))).toBeNull();
        });
    });
});

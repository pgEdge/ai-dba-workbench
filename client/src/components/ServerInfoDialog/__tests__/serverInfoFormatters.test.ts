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
import { formatBytes, formatClockSpeed, pct } from '../serverInfoFormatters';

describe('serverInfoFormatters', () => {
    // -----------------------------------------------------------------------
    // formatBytes
    // -----------------------------------------------------------------------

    describe('formatBytes', () => {
        it('returns em dash for null', () => {
            expect(formatBytes(null)).toBe('\u2014');
        });

        it('returns em dash for undefined', () => {
            expect(formatBytes(undefined)).toBe('\u2014');
        });

        it('returns em dash for 0 bytes', () => {
            expect(formatBytes(0)).toBe('\u2014');
        });

        it('formats bytes less than 1024 as B', () => {
            expect(formatBytes(500)).toBe('500 B');
            expect(formatBytes(1)).toBe('1 B');
            expect(formatBytes(1023)).toBe('1023 B');
        });

        it('formats kilobytes correctly', () => {
            expect(formatBytes(1024)).toBe('1.0 KB');
            expect(formatBytes(1536)).toBe('1.5 KB');
            expect(formatBytes(10240)).toBe('10.0 KB');
        });

        it('formats megabytes correctly', () => {
            expect(formatBytes(1048576)).toBe('1.0 MB');
            expect(formatBytes(1572864)).toBe('1.5 MB');
            expect(formatBytes(104857600)).toBe('100.0 MB');
        });

        it('formats gigabytes correctly', () => {
            expect(formatBytes(1073741824)).toBe('1.0 GB');
            expect(formatBytes(1610612736)).toBe('1.5 GB');
            expect(formatBytes(17179869184)).toBe('16.0 GB');
        });

        it('formats terabytes correctly', () => {
            expect(formatBytes(1099511627776)).toBe('1.0 TB');
            expect(formatBytes(2199023255552)).toBe('2.0 TB');
        });

        it('formats petabytes correctly', () => {
            expect(formatBytes(1125899906842624)).toBe('1.0 PB');
        });

        it('caps at PB for very large values', () => {
            // Value that would exceed PB range
            const hugeValue = 1125899906842624 * 1024;
            expect(formatBytes(hugeValue)).toBe('1024.0 PB');
        });
    });

    // -----------------------------------------------------------------------
    // formatClockSpeed
    // -----------------------------------------------------------------------

    describe('formatClockSpeed', () => {
        it('returns em dash for null', () => {
            expect(formatClockSpeed(null)).toBe('\u2014');
        });

        it('returns em dash for undefined', () => {
            expect(formatClockSpeed(undefined)).toBe('\u2014');
        });

        it('formats Hz values (less than 1 MHz)', () => {
            expect(formatClockSpeed(500000)).toBe('500000 Hz');
            expect(formatClockSpeed(999999)).toBe('999999 Hz');
        });

        it('formats MHz values correctly', () => {
            expect(formatClockSpeed(1000000)).toBe('1 MHz');
            expect(formatClockSpeed(2400000)).toBe('2 MHz');
            expect(formatClockSpeed(100000000)).toBe('100 MHz');
            expect(formatClockSpeed(999000000)).toBe('999 MHz');
        });

        it('formats GHz values correctly', () => {
            expect(formatClockSpeed(1000000000)).toBe('1.00 GHz');
            expect(formatClockSpeed(2400000000)).toBe('2.40 GHz');
            expect(formatClockSpeed(3700000000)).toBe('3.70 GHz');
            expect(formatClockSpeed(5000000000)).toBe('5.00 GHz');
        });

        it('handles zero Hz', () => {
            expect(formatClockSpeed(0)).toBe('0 Hz');
        });
    });

    // -----------------------------------------------------------------------
    // pct
    // -----------------------------------------------------------------------

    describe('pct', () => {
        it('returns null when used is null', () => {
            expect(pct(null, 100)).toBeNull();
        });

        it('returns null when total is null', () => {
            expect(pct(50, null)).toBeNull();
        });

        it('returns null when both are null', () => {
            expect(pct(null, null)).toBeNull();
        });

        it('returns null when total is zero (division by zero)', () => {
            expect(pct(50, 0)).toBeNull();
        });

        it('calculates percentage correctly', () => {
            expect(pct(50, 100)).toBe(50);
            expect(pct(25, 100)).toBe(25);
            expect(pct(75, 100)).toBe(75);
            expect(pct(100, 100)).toBe(100);
        });

        it('rounds percentage to nearest integer', () => {
            expect(pct(33, 100)).toBe(33);
            expect(pct(1, 3)).toBe(33);
            expect(pct(2, 3)).toBe(67);
        });

        it('handles large numbers', () => {
            expect(pct(8589934592, 17179869184)).toBe(50);
        });

        it('returns 0 when used is 0', () => {
            expect(pct(0, 100)).toBe(0);
        });
    });
});

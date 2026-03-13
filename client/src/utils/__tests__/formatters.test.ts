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
    formatBytes,
    formatTime,
    formatNumber,
    formatCompactNumber,
    formatValue,
    formatLag,
    formatPgSetting,
} from '../formatters';

describe('formatBytes', () => {
    it('returns "--" for null', () => {
        expect(formatBytes(null)).toBe('--');
    });

    it('returns "--" for undefined', () => {
        expect(formatBytes(undefined)).toBe('--');
    });

    it('returns "0 B" for zero', () => {
        expect(formatBytes(0)).toBe('0 B');
    });

    it('formats small values in bytes', () => {
        expect(formatBytes(500)).toBe('500 B');
    });

    it('formats values in KB', () => {
        expect(formatBytes(1024)).toBe('1.0 KB');
    });

    it('formats fractional KB values', () => {
        expect(formatBytes(1536)).toBe('1.5 KB');
    });

    it('formats values in MB', () => {
        expect(formatBytes(1048576)).toBe('1.0 MB');
    });

    it('formats values in GB', () => {
        expect(formatBytes(1073741824)).toBe('1.0 GB');
    });

    it('formats values in TB', () => {
        expect(formatBytes(1099511627776)).toBe('1.0 TB');
    });

    it('formats values in PB', () => {
        expect(formatBytes(1125899906842624)).toBe('1.0 PB');
    });
});

describe('formatTime', () => {
    it('returns "--" for null', () => {
        expect(formatTime(null)).toBe('--');
    });

    it('returns "--" for undefined', () => {
        expect(formatTime(undefined)).toBe('--');
    });

    it('formats sub-millisecond values in microseconds', () => {
        expect(formatTime(0.5)).toBe('500 us');
    });

    it('formats values under 1000 ms in milliseconds', () => {
        expect(formatTime(45.2)).toBe('45.2 ms');
    });

    it('formats values in seconds', () => {
        expect(formatTime(5000)).toBe('5.00 s');
    });

    it('formats values in minutes', () => {
        expect(formatTime(120000)).toBe('2.0 min');
    });

    it('formats values in hours', () => {
        expect(formatTime(7200000)).toBe('2.0 hr');
    });

    it('formats values in days', () => {
        expect(formatTime(172800000)).toBe('2.0 d');
    });

    it('formats large cumulative time in days', () => {
        expect(formatTime(25172334000)).toBe('291.3 d');
    });

    it('handles boundary at exactly 1 ms', () => {
        expect(formatTime(1)).toBe('1.0 ms');
    });

    it('handles boundary at exactly 1000 ms', () => {
        expect(formatTime(1000)).toBe('1.00 s');
    });

    it('handles boundary at exactly 60000 ms', () => {
        expect(formatTime(60000)).toBe('1.0 min');
    });

    it('handles boundary at exactly 3600000 ms', () => {
        expect(formatTime(3600000)).toBe('1.0 hr');
    });

    it('handles boundary at exactly 86400000 ms', () => {
        expect(formatTime(86400000)).toBe('1.0 d');
    });
});

describe('formatNumber', () => {
    it('returns "--" for null', () => {
        expect(formatNumber(null)).toBe('--');
    });

    it('returns "--" for undefined', () => {
        expect(formatNumber(undefined)).toBe('--');
    });

    it('formats small numbers', () => {
        expect(formatNumber(42)).toBe('42');
    });

    it('formats zero', () => {
        expect(formatNumber(0)).toBe('0');
    });

    it('formats thousands with a separator', () => {
        const result = formatNumber(1234);
        expect(result).toMatch(/1.234/);
    });

    it('formats millions with separators', () => {
        const result = formatNumber(1234567);
        expect(result).toMatch(/1.234.567/);
    });
});

describe('formatCompactNumber', () => {
    it('returns "--" for null', () => {
        expect(formatCompactNumber(null)).toBe('--');
    });

    it('returns "--" for undefined', () => {
        expect(formatCompactNumber(undefined)).toBe('--');
    });

    it('formats small numbers with locale string', () => {
        expect(formatCompactNumber(42)).toBe('42');
    });

    it('formats numbers under 10K with locale separators', () => {
        const result = formatCompactNumber(9999);
        expect(result).toMatch(/9.999/);
    });

    it('formats 10K+ with K suffix', () => {
        expect(formatCompactNumber(12345)).toBe('12.3K');
    });

    it('formats millions with M suffix', () => {
        expect(formatCompactNumber(1234567)).toBe('1.2M');
    });

    it('formats billions with B suffix', () => {
        expect(formatCompactNumber(1234567890)).toBe('1.2B');
    });
});

describe('formatValue', () => {
    it('returns "--" for null', () => {
        expect(formatValue(null)).toBe('--');
    });

    it('returns "--" for undefined', () => {
        expect(formatValue(undefined)).toBe('--');
    });

    it('formats zero with default decimals', () => {
        expect(formatValue(0)).toBe('0.0');
    });

    it('formats a number with default 1 decimal place', () => {
        const result = formatValue(15284.3);
        expect(result).toMatch(/15.284\.3/);
    });

    it('formats a number with custom decimal places', () => {
        expect(formatValue(3.14159, 3)).toBe('3.142');
    });
});

describe('formatLag', () => {
    it('returns "--" for null', () => {
        expect(formatLag(null)).toBe('--');
    });

    it('returns "--" for undefined', () => {
        expect(formatLag(undefined)).toBe('--');
    });

    it('formats zero as milliseconds', () => {
        expect(formatLag(0)).toBe('0 ms');
    });

    it('formats sub-second values in milliseconds', () => {
        expect(formatLag(0.25)).toBe('250 ms');
    });

    it('formats values in seconds', () => {
        expect(formatLag(30)).toBe('30.0 s');
    });

    it('formats values in minutes', () => {
        expect(formatLag(300)).toBe('5.0 min');
    });

    it('formats values in hours', () => {
        expect(formatLag(7200)).toBe('2.0 hr');
    });

    it('formats values in days', () => {
        expect(formatLag(172800)).toBe('2.0 d');
    });
});

describe('formatPgSetting', () => {
    it('returns em dash for null', () => {
        expect(formatPgSetting(null)).toBe('\u2014');
    });

    it('returns em dash for empty string', () => {
        expect(formatPgSetting('')).toBe('\u2014');
    });

    it('converts 8kB unit to readable bytes', () => {
        const result = formatPgSetting('524288', '8kB');
        expect(result).toContain('GB');
    });

    it('converts kB unit to readable bytes', () => {
        const result = formatPgSetting('8192', 'kB');
        expect(result).toContain('MB');
    });

    it('converts MB unit to readable bytes', () => {
        const result = formatPgSetting('1024', 'MB');
        expect(result).toContain('GB');
    });

    it('converts ms unit to readable time', () => {
        const result = formatPgSetting('5000', 'ms');
        expect(result).toContain('s');
    });

    it('converts s unit to readable time', () => {
        const result = formatPgSetting('60', 's');
        expect(result).toContain('min');
    });

    it('converts min unit to readable time', () => {
        const result = formatPgSetting('60', 'min');
        expect(result).toContain('hr');
    });

    it('returns setting as-is when no unit is provided', () => {
        expect(formatPgSetting('on')).toBe('on');
    });

    it('appends unknown unit to the setting value', () => {
        expect(formatPgSetting('5', 'foo')).toBe('5 foo');
    });
});

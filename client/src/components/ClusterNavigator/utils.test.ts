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
    parseGroupNumericId,
    isAutoGroupId,
    getClusterType,
    getEffectiveRole,
    saveToStorage,
    loadFromStorage,
    formatRelativeTime,
} from './utils';

describe('parseGroupNumericId', () => {
    describe('matches database-backed ids', () => {
        it.each([
            ['group-0', 0],
            ['group-1', 1],
            ['group-42', 42],
            ['group-12345', 12345],
        ])('parses %s as %i', (input, expected) => {
            expect(parseGroupNumericId(input)).toBe(expected);
        });

        it('returns a number, never NaN, for valid ids', () => {
            const parsed = parseGroupNumericId('group-7');
            expect(typeof parsed).toBe('number');
            expect(Number.isNaN(parsed)).toBe(false);
            expect(parsed).toBe(7);
        });
    });

    describe('rejects non-matching inputs', () => {
        it.each([
            ['group-auto'],
            ['group-auto-foo'],
            ['group-auto-some-key'],
            ['group-'],
            ['group-1a'],
            ['group-a1'],
            ['group-Production'],
            ['group-1.5'],
            ['group- 1'],
            ['group-1 '],
            ['group'],
            [''],
            ['42'],
            ['1'],
            ['cluster-1'],
            ['server-1'],
        ])('returns undefined for %j', (input) => {
            expect(parseGroupNumericId(input)).toBeUndefined();
        });

        it('returns undefined for undefined input', () => {
            expect(parseGroupNumericId(undefined)).toBeUndefined();
        });
    });

    describe('return type guarantees', () => {
        it('never returns NaN, always number or undefined', () => {
            // Exercise a range of shapes and confirm the result is either
            // a finite number or undefined; NaN must never leak through.
            const inputs: (string | undefined)[] = [
                undefined,
                '',
                'group-0',
                'group-999999',
                'group-auto',
                'group-1a',
                'group-',
                'group-01', // leading zero numeric suffix still parses
            ];
            for (const input of inputs) {
                const result = parseGroupNumericId(input);
                if (result === undefined) {
                    expect(result).toBeUndefined();
                } else {
                    expect(typeof result).toBe('number');
                    expect(Number.isFinite(result)).toBe(true);
                    expect(Number.isNaN(result)).toBe(false);
                }
            }
        });

        it('parses leading-zero numeric suffixes as plain numbers', () => {
            // /^group-(\d+)$/ matches "group-01"; Number("01") === 1.
            expect(parseGroupNumericId('group-01')).toBe(1);
        });
    });
});

describe('isAutoGroupId', () => {
    describe('matches the bare and suffixed auto forms', () => {
        it.each([
            ['group-auto'],
            ['group-auto-foo'],
            ['group-auto-some_key-123'],
            ['group-auto-A_B-c_d'],
            ['group-auto-0'],
            ['group-auto-UPPER'],
            ['group-auto-a'],
            ['group-auto--'],
        ])('returns true for %j', (input) => {
            expect(isAutoGroupId(input)).toBe(true);
        });
    });

    describe('rejects malformed or unrelated inputs', () => {
        it.each([
            ['group-autobad'],
            ['group-automatic'],
            ['group-auto_foo'],
            ['group-auto-'],
            ['group-auto/evil'],
            ['group-auto-bad id'],
            ['group-'],
            ['group-5'],
            ['group-1a'],
            ['group-Production'],
            ['not-a-group'],
            ['auto'],
            ['group'],
            [''],
        ])('returns false for %j', (input) => {
            expect(isAutoGroupId(input)).toBe(false);
        });

        it('returns false for undefined input', () => {
            expect(isAutoGroupId(undefined)).toBe(false);
        });
    });
});

describe('getClusterType', () => {
    it('returns default for servers with no replication role', () => {
        expect(getClusterType({
            id: 'c1',
            name: 'plain',
            servers: [{ id: 1, name: 'pg-1', primary_role: 'standalone' }],
        })).toBe('default');
    });
});

describe('getEffectiveRole', () => {
    it('shows a binary primary in a logical cluster as a publisher', () => {
        expect(getEffectiveRole('binary_primary', 'logical')).toBe('logical_publisher');
    });

    it('keeps the role in any other cluster type', () => {
        expect(getEffectiveRole('binary_primary', 'binary')).toBe('binary_primary');
    });
});

describe('saveToStorage', () => {
    // The test setup replaces localStorage with vi.fn() stubs.
    const setItem = vi.mocked(localStorage.setItem);
    const getItem = vi.mocked(localStorage.getItem);

    beforeEach(() => {
        setItem.mockReset();
        getItem.mockReset();
    });

    it('stores the value as JSON that loadFromStorage reads back', () => {
        saveToStorage('utils-test-key', { a: 1 });
        expect(setItem).toHaveBeenCalledWith('utils-test-key', '{"a":1}');
        getItem.mockReturnValueOnce('{"a":1}');
        expect(loadFromStorage('utils-test-key', null)).toEqual({ a: 1 });
    });

    it('ignores a storage error', () => {
        setItem.mockImplementationOnce(() => {
            throw new Error('quota exceeded');
        });
        expect(() => saveToStorage('utils-test-key', 1)).not.toThrow();
    });
});

describe('formatRelativeTime', () => {
    it('returns an empty string without a date', () => {
        expect(formatRelativeTime(null)).toBe('');
    });

    it('reports a recent time as just now', () => {
        expect(formatRelativeTime(new Date())).toBe('just now');
    });

    it('reports older times in minutes', () => {
        expect(formatRelativeTime(new Date(Date.now() - 5 * 60 * 1000))).toBe('5m ago');
    });
});

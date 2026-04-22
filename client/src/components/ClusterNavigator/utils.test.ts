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
import { parseGroupNumericId } from './utils';

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
            const inputs: Array<string | undefined> = [
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

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
    validateName,
    NAME_MAX_LENGTH,
    NAME_PATTERN,
    NAME_ERROR_REQUIRED,
    NAME_ERROR_INVALID_CHARS,
    NAME_ERROR_TOO_LONG,
} from '../validateName';

describe('validateName', () => {
    describe('valid names', () => {
        it('accepts a simple alphabetic name', () => {
            expect(validateName('Production')).toBeNull();
        });

        it('accepts letters, digits, and spaces', () => {
            expect(validateName('Cluster 01')).toBeNull();
        });

        it('accepts periods', () => {
            expect(validateName('node.one')).toBeNull();
        });

        it('accepts underscores', () => {
            expect(validateName('my_cluster')).toBeNull();
        });

        it('accepts hyphens', () => {
            expect(validateName('east-west-1')).toBeNull();
        });

        it('accepts parentheses', () => {
            expect(validateName('Group (primary)')).toBeNull();
        });

        it('accepts a combination of all allowed characters', () => {
            expect(validateName('My_Group-01 (v1.2)')).toBeNull();
        });

        it('accepts a name composed only of digits', () => {
            expect(validateName('12345')).toBeNull();
        });

        it('trims leading and trailing whitespace before validating', () => {
            expect(validateName('   Valid Name   ')).toBeNull();
        });

        it('accepts a name exactly at the maximum length', () => {
            const name = 'a'.repeat(NAME_MAX_LENGTH);
            expect(name).toHaveLength(255);
            expect(validateName(name)).toBeNull();
        });

        it('accepts a name padded with whitespace that trims to the max length', () => {
            const name = `  ${'a'.repeat(NAME_MAX_LENGTH)}  `;
            expect(validateName(name)).toBeNull();
        });
    });

    describe('empty and whitespace-only names', () => {
        it('rejects an empty string', () => {
            expect(validateName('')).toBe(NAME_ERROR_REQUIRED);
        });

        it('rejects a whitespace-only string', () => {
            expect(validateName('     ')).toBe(NAME_ERROR_REQUIRED);
        });

        it('rejects a tab-and-newline-only string', () => {
            expect(validateName('\t\n')).toBe(NAME_ERROR_REQUIRED);
        });
    });

    describe('disallowed characters', () => {
        it.each([
            ['<', 'na<me'],
            ['>', 'na>me'],
            ['!', 'name!'],
            ['@', 'na@me'],
            ['#', 'na#me'],
            ['$', 'na$me'],
            ['%', 'na%me'],
        ])('rejects the %s character', (_char, value) => {
            expect(validateName(value)).toBe(NAME_ERROR_INVALID_CHARS);
        });

        it('rejects a name that is only special characters', () => {
            expect(validateName('<>!@#$%')).toBe(NAME_ERROR_INVALID_CHARS);
        });

        it('rejects names containing slashes', () => {
            expect(validateName('a/b')).toBe(NAME_ERROR_INVALID_CHARS);
        });
    });

    describe('length limits', () => {
        it('rejects a name one character over the maximum', () => {
            const name = 'a'.repeat(NAME_MAX_LENGTH + 1);
            expect(name).toHaveLength(256);
            expect(validateName(name)).toBe(NAME_ERROR_TOO_LONG);
        });

        it('reports too-long before checking characters', () => {
            // A 256-char string containing a disallowed char should still
            // report the length error first.
            const name = `${'<'.repeat(NAME_MAX_LENGTH + 1)}`;
            expect(validateName(name)).toBe(NAME_ERROR_TOO_LONG);
        });
    });

    describe('exported constants', () => {
        it('exposes the expected maximum length', () => {
            expect(NAME_MAX_LENGTH).toBe(255);
        });

        it('exposes a usable pattern that matches allowed names', () => {
            expect(NAME_PATTERN.test('Valid_Name-1 (a.b)')).toBe(true);
            expect(NAME_PATTERN.test('bad!')).toBe(false);
        });
    });
});

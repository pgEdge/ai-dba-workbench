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
    isValidUsername,
    isValidEmail,
    USERNAME_MAX_LENGTH,
} from '../userValidation';

describe('isValidUsername', () => {
    it('accepts a simple alphanumeric username', () => {
        expect(isValidUsername('charlie')).toBe(true);
        expect(isValidUsername('alice99')).toBe(true);
    });

    it('accepts a leading digit', () => {
        expect(isValidUsername('1user')).toBe(true);
    });

    it('accepts the allowed punctuation in later positions', () => {
        expect(isValidUsername('a_b.c-d@e')).toBe(true);
    });

    it('accepts a unicode letter as the first character', () => {
        // 'é' is 'e' with an acute accent (a Unicode letter).
        expect(isValidUsername('élodie')).toBe(true);
    });

    it('rejects an empty string', () => {
        expect(isValidUsername('')).toBe(false);
    });

    it('rejects a leading punctuation character', () => {
        expect(isValidUsername('_user')).toBe(false);
        expect(isValidUsername('.user')).toBe(false);
        expect(isValidUsername('-user')).toBe(false);
        expect(isValidUsername('@user')).toBe(false);
    });

    it('rejects disallowed characters in later positions', () => {
        expect(isValidUsername('user name')).toBe(false);
        expect(isValidUsername('user!')).toBe(false);
        expect(isValidUsername('user#tag')).toBe(false);
    });

    it('accepts a username at the maximum length', () => {
        expect(isValidUsername('a'.repeat(USERNAME_MAX_LENGTH))).toBe(true);
    });

    it('rejects a username beyond the maximum length', () => {
        expect(
            isValidUsername('a'.repeat(USERNAME_MAX_LENGTH + 1)),
        ).toBe(false);
    });
});

describe('isValidEmail', () => {
    it('accepts ordinary addresses', () => {
        expect(isValidEmail('c@example.com')).toBe(true);
        expect(isValidEmail('first.last@sub.example.co.uk')).toBe(true);
    });

    it('rejects a string with no @', () => {
        expect(isValidEmail('notanemail')).toBe(false);
    });

    it('rejects an address with no domain', () => {
        expect(isValidEmail('test@')).toBe(false);
    });

    it('rejects an address with no dot in the domain', () => {
        expect(isValidEmail('test@localhost')).toBe(false);
    });

    it('rejects an empty string', () => {
        expect(isValidEmail('')).toBe(false);
    });

    it('rejects addresses containing whitespace', () => {
        expect(isValidEmail('a b@example.com')).toBe(false);
        expect(isValidEmail('a@ex ample.com')).toBe(false);
    });
});

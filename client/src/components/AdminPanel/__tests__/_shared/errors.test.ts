import { describe, it, expect } from 'vitest';
import {
    extractErrorMessage,
    DEFAULT_ERROR_MESSAGE,
} from '../../_shared/errors';

describe('extractErrorMessage', () => {
    it('returns the message of an Error instance', () => {
        expect(extractErrorMessage(new Error('boom'))).toBe('boom');
    });

    it('returns the default fallback for non-Error values', () => {
        expect(extractErrorMessage('weird')).toBe(DEFAULT_ERROR_MESSAGE);
        expect(extractErrorMessage(null)).toBe(DEFAULT_ERROR_MESSAGE);
        expect(extractErrorMessage(undefined)).toBe(DEFAULT_ERROR_MESSAGE);
        expect(extractErrorMessage({ foo: 'bar' })).toBe(DEFAULT_ERROR_MESSAGE);
    });

    it('honours a caller-supplied fallback for non-Error values', () => {
        expect(extractErrorMessage('weird', 'custom fallback')).toBe(
            'custom fallback',
        );
    });

    it('prefers the Error message even when a fallback is supplied', () => {
        expect(extractErrorMessage(new Error('boom'), 'unused fallback')).toBe(
            'boom',
        );
    });

    it('exports the canonical default message constant', () => {
        expect(DEFAULT_ERROR_MESSAGE).toBe('An unexpected error occurred');
    });
});

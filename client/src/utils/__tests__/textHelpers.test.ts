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
    stripPreamble,
    truncateDescription,
    djb2Hash,
    slugify,
    ANALYSIS_CACHE_TTL_MS,
} from '../textHelpers';

describe('stripPreamble', () => {
    describe('text with markdown headings', () => {
        it('strips preamble before a second-level heading', () => {
            const input = 'Here is some intro text.\n\n## Heading\n\nContent';
            const result = stripPreamble(input);
            expect(result).toBe('## Heading\n\nContent');
        });

        it('strips multi-line preamble', () => {
            const input = 'Line 1\nLine 2\nLine 3\n\n## Analysis\n\nDetails';
            const result = stripPreamble(input);
            expect(result).toBe('## Analysis\n\nDetails');
        });

        it('handles multiple headings and returns from the first', () => {
            const input = 'Preamble\n## First\n\n## Second';
            const result = stripPreamble(input);
            expect(result).toBe('## First\n\n## Second');
        });
    });

    describe('text without preamble', () => {
        it('returns text unchanged when it starts with a heading', () => {
            const input = '## Heading\n\nContent here';
            const result = stripPreamble(input);
            expect(result).toBe('## Heading\n\nContent here');
        });

        it('returns text unchanged when heading is at position 0', () => {
            const input = '## Start';
            const result = stripPreamble(input);
            expect(result).toBe('## Start');
        });
    });

    describe('text without headings', () => {
        it('returns text unchanged when no heading exists', () => {
            const input = 'This is just plain text without any headings.';
            const result = stripPreamble(input);
            expect(result).toBe(input);
        });

        it('handles empty string', () => {
            expect(stripPreamble('')).toBe('');
        });

        it('ignores headings that are not second-level', () => {
            const input = 'Preamble\n# First Level\n### Third Level';
            const result = stripPreamble(input);
            expect(result).toBe(input);
        });
    });

    describe('edge cases with heading patterns', () => {
        it('requires space after ## for valid heading', () => {
            const input = 'Preamble\n##NoSpace';
            const result = stripPreamble(input);
            expect(result).toBe(input);
        });

        it('handles heading at start of a line (multiline flag)', () => {
            const input = 'Some text\n## Heading on new line';
            const result = stripPreamble(input);
            expect(result).toBe('## Heading on new line');
        });
    });
});

describe('truncateDescription', () => {
    describe('with normal inputs', () => {
        it('returns the first line when under max length', () => {
            const result = truncateDescription('Short description');
            expect(result).toBe('Short description');
        });

        it('truncates long first lines with ellipsis', () => {
            const longText = 'This is a very long description that exceeds the maximum allowed length for display';
            const result = truncateDescription(longText);
            // Truncates to first 60 chars then adds ...
            expect(result).toBe('This is a very long description that exceeds the maximum all...');
            expect(result.length).toBe(63); // 60 + '...'
        });

        it('returns only the first line of multiline text', () => {
            const multiline = 'First line\nSecond line\nThird line';
            const result = truncateDescription(multiline);
            expect(result).toBe('First line');
        });
    });

    describe('with custom max length', () => {
        it('respects custom max length', () => {
            const result = truncateDescription('Hello World', 5);
            expect(result).toBe('Hello...');
        });

        it('returns full text when under custom max length', () => {
            const result = truncateDescription('Hi', 10);
            expect(result).toBe('Hi');
        });

        it('handles max length of 0', () => {
            const result = truncateDescription('Test', 0);
            expect(result).toBe('...');
        });
    });

    describe('with edge cases', () => {
        it('returns empty string for empty input', () => {
            expect(truncateDescription('')).toBe('');
        });

        it('returns empty string for falsy input', () => {
            // TypeScript would normally prevent this, but testing runtime behavior
            expect(truncateDescription(null as unknown as string)).toBe('');
            expect(truncateDescription(undefined as unknown as string)).toBe('');
        });

        it('handles text exactly at max length', () => {
            const exactLength = 'a'.repeat(60);
            const result = truncateDescription(exactLength);
            expect(result).toBe(exactLength);
            expect(result.length).toBe(60);
        });

        it('handles text one character over max length', () => {
            const overLength = 'a'.repeat(61);
            const result = truncateDescription(overLength);
            expect(result).toBe(`${'a'.repeat(60)}...`);
        });

        it('handles newline at beginning', () => {
            const result = truncateDescription('\nSecond line');
            expect(result).toBe('');
        });
    });
});

describe('djb2Hash', () => {
    describe('hash consistency', () => {
        it('returns the same hash for the same input', () => {
            const hash1 = djb2Hash('test string');
            const hash2 = djb2Hash('test string');
            expect(hash1).toBe(hash2);
        });

        it('returns a string representation of the hash', () => {
            const hash = djb2Hash('hello');
            expect(typeof hash).toBe('string');
            expect(Number.isNaN(Number(hash))).toBe(false);
        });
    });

    describe('different inputs produce different hashes', () => {
        it('hashes differ for different strings', () => {
            const hash1 = djb2Hash('hello');
            const hash2 = djb2Hash('world');
            expect(hash1).not.toBe(hash2);
        });

        it('hashes differ for similar strings', () => {
            const hash1 = djb2Hash('test1');
            const hash2 = djb2Hash('test2');
            expect(hash1).not.toBe(hash2);
        });

        it('hashes differ by case', () => {
            const hash1 = djb2Hash('Hello');
            const hash2 = djb2Hash('hello');
            expect(hash1).not.toBe(hash2);
        });
    });

    describe('edge cases', () => {
        it('handles empty string', () => {
            const hash = djb2Hash('');
            expect(hash).toBe('5381'); // Initial hash value
        });

        it('handles single character', () => {
            const hash = djb2Hash('a');
            expect(typeof hash).toBe('string');
        });

        it('handles long strings', () => {
            const longString = 'a'.repeat(10000);
            const hash = djb2Hash(longString);
            expect(typeof hash).toBe('string');
        });

        it('handles special characters', () => {
            const hash = djb2Hash('!@#$%^&*()');
            expect(typeof hash).toBe('string');
        });

        it('handles unicode characters', () => {
            const hash = djb2Hash('\u4e2d\u6587\u6d4b\u8bd5');
            expect(typeof hash).toBe('string');
        });

        it('handles whitespace', () => {
            const hash1 = djb2Hash('hello world');
            const hash2 = djb2Hash('helloworld');
            expect(hash1).not.toBe(hash2);
        });
    });

    describe('hash properties', () => {
        it('returns a non-negative number as string', () => {
            const hash = djb2Hash('negative test');
            expect(Number(hash)).toBeGreaterThanOrEqual(0);
        });

        it('returns unsigned 32-bit value as string', () => {
            const hash = djb2Hash('overflow test');
            const num = Number(hash);
            expect(num).toBeLessThanOrEqual(4294967295); // 2^32 - 1
        });
    });
});

describe('slugify', () => {
    describe('basic transformations', () => {
        it('converts text to lowercase', () => {
            expect(slugify('Hello World')).toBe('hello-world');
        });

        it('replaces spaces with hyphens', () => {
            expect(slugify('some text here')).toBe('some-text-here');
        });

        it('handles single word', () => {
            expect(slugify('Word')).toBe('word');
        });
    });

    describe('special character handling', () => {
        it('removes special characters', () => {
            expect(slugify('Hello! World?')).toBe('hello-world');
        });

        it('removes punctuation', () => {
            expect(slugify("it's a test.")).toBe('it-s-a-test');
        });

        it('replaces multiple non-alphanumeric chars with single hyphen', () => {
            expect(slugify('hello   world')).toBe('hello-world');
            expect(slugify('hello---world')).toBe('hello-world');
            expect(slugify('hello!@#world')).toBe('hello-world');
        });
    });

    describe('edge handling', () => {
        it('removes leading hyphens', () => {
            expect(slugify('---hello')).toBe('hello');
        });

        it('removes trailing hyphens', () => {
            expect(slugify('hello---')).toBe('hello');
        });

        it('removes both leading and trailing hyphens', () => {
            expect(slugify('---hello---')).toBe('hello');
        });

        it('handles leading special characters', () => {
            expect(slugify('!@#$hello')).toBe('hello');
        });

        it('handles trailing special characters', () => {
            expect(slugify('hello!@#$')).toBe('hello');
        });
    });

    describe('number handling', () => {
        it('preserves numbers', () => {
            expect(slugify('test123')).toBe('test123');
        });

        it('handles text with numbers', () => {
            expect(slugify('Version 2.0 Release')).toBe('version-2-0-release');
        });

        it('handles numeric strings', () => {
            expect(slugify('12345')).toBe('12345');
        });
    });

    describe('edge cases', () => {
        it('returns empty string for empty input', () => {
            expect(slugify('')).toBe('');
        });

        it('returns empty string for only special characters', () => {
            expect(slugify('!@#$%^&*()')).toBe('');
        });

        it('returns empty string for only spaces', () => {
            expect(slugify('     ')).toBe('');
        });

        it('handles already slugified text', () => {
            expect(slugify('already-slugified')).toBe('already-slugified');
        });

        it('handles mixed case and special chars', () => {
            expect(slugify('The Quick Brown Fox!')).toBe('the-quick-brown-fox');
        });
    });
});

describe('ANALYSIS_CACHE_TTL_MS', () => {
    it('equals 30 minutes in milliseconds', () => {
        expect(ANALYSIS_CACHE_TTL_MS).toBe(30 * 60 * 1000);
        expect(ANALYSIS_CACHE_TTL_MS).toBe(1800000);
    });
});

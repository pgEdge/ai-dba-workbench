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
import { createCleanTheme } from '../markdownUtils';

describe('markdownUtils', () => {
    describe('createCleanTheme', () => {
        it('removes background from token styles', () => {
            const baseTheme = {
                comment: {
                    color: '#6a737d',
                    background: '#f6f8fa',
                    fontStyle: 'italic',
                },
                keyword: {
                    color: '#d73a49',
                    backgroundColor: '#fff',
                    fontWeight: 'bold',
                },
            };

            const result = createCleanTheme(baseTheme, '#1e1e1e');

            expect(result.comment).toEqual({
                color: '#6a737d',
                fontStyle: 'italic',
            });
            expect(result.keyword).toEqual({
                color: '#d73a49',
                fontWeight: 'bold',
            });
        });

        it('preserves non-object values', () => {
            const baseTheme = {
                someString: 'value',
                someNumber: 42,
            };

            const result = createCleanTheme(baseTheme, '#1e1e1e');

            expect(result.someString).toBe('value');
            expect(result.someNumber).toBe(42);
        });

        it('sets custom background on pre element', () => {
            const baseTheme = {
                'pre[class*="language-"]': {
                    background: '#original',
                    padding: '1em',
                },
            };

            const result = createCleanTheme(baseTheme, '#custom-bg');

            const preStyle = result['pre[class*="language-"]'] as Record<
                string,
                unknown
            >;
            expect(preStyle.background).toBe('#custom-bg');
            expect(preStyle.padding).toBe('1em');
        });

        it('sets transparent background on code element', () => {
            const baseTheme = {
                'code[class*="language-"]': {
                    background: '#original',
                    fontSize: '14px',
                },
            };

            const result = createCleanTheme(baseTheme, '#custom-bg');

            const codeStyle = result['code[class*="language-"]'] as Record<
                string,
                unknown
            >;
            expect(codeStyle.background).toBe('transparent');
            expect(codeStyle.fontSize).toBe('14px');
        });

        it('handles empty theme object', () => {
            const result = createCleanTheme({}, '#1e1e1e');
            expect(result).toEqual({});
        });

        it('handles null values in theme', () => {
            const baseTheme = {
                nullValue: null,
                validToken: { color: '#fff' },
            };

            const result = createCleanTheme(baseTheme, '#1e1e1e');

            expect(result.nullValue).toBeNull();
            expect(result.validToken).toEqual({ color: '#fff' });
        });

        it('preserves all other token properties', () => {
            const baseTheme = {
                string: {
                    color: '#032f62',
                    background: '#f6f8fa',
                    textDecoration: 'underline',
                    fontWeight: 'normal',
                    fontSize: '14px',
                },
            };

            const result = createCleanTheme(baseTheme, '#1e1e1e');

            expect(result.string).toEqual({
                color: '#032f62',
                textDecoration: 'underline',
                fontWeight: 'normal',
                fontSize: '14px',
            });
        });

        it('handles deeply nested token styles', () => {
            const baseTheme = {
                'token.punctuation': {
                    color: '#393A34',
                    background: '#fff',
                },
            };

            const result = createCleanTheme(baseTheme, '#1e1e1e');

            expect(result['token.punctuation']).toEqual({
                color: '#393A34',
            });
        });
    });
});

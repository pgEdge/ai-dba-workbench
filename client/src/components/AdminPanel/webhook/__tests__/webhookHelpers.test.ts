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
    parseAuthCredentials,
    buildAuthCredentials,
    headersObjectToArray,
    headersArrayToObject,
    HTTP_METHODS,
    AUTH_TYPES,
    DEFAULT_ALERT_FIRE_TEMPLATE,
    DEFAULT_ALERT_CLEAR_TEMPLATE,
    DEFAULT_REMINDER_TEMPLATE,
} from '../webhookHelpers';
import type { HeaderEntry } from '../webhookTypes';

describe('webhookHelpers', () => {
    describe('constants', () => {
        it('exports HTTP_METHODS array', () => {
            expect(HTTP_METHODS).toEqual(['POST', 'GET', 'PUT', 'PATCH']);
        });

        it('exports AUTH_TYPES array', () => {
            expect(AUTH_TYPES).toHaveLength(4);
            expect(AUTH_TYPES[0]).toEqual({ value: 'none', label: 'None' });
            expect(AUTH_TYPES[1]).toEqual({ value: 'basic', label: 'Basic' });
            expect(AUTH_TYPES[2]).toEqual({ value: 'bearer', label: 'Bearer Token' });
            expect(AUTH_TYPES[3]).toEqual({ value: 'api_key', label: 'API Key' });
        });

        it('exports default template strings', () => {
            expect(DEFAULT_ALERT_FIRE_TEMPLATE).toContain('"event": "alert_fire"');
            expect(DEFAULT_ALERT_CLEAR_TEMPLATE).toContain('"event": "alert_clear"');
            expect(DEFAULT_REMINDER_TEMPLATE).toContain('"event": "reminder"');
        });
    });

    describe('parseAuthCredentials', () => {
        describe('basic auth', () => {
            it('parses credentials with colon separator', () => {
                const result = parseAuthCredentials('basic', 'myuser:mypassword');
                expect(result).toEqual({
                    username: 'myuser',
                    password: 'mypassword',
                });
            });

            it('handles password containing colons', () => {
                const result = parseAuthCredentials('basic', 'user:pass:with:colons');
                expect(result).toEqual({
                    username: 'user',
                    password: 'pass:with:colons',
                });
            });

            it('handles credentials without colon separator', () => {
                const result = parseAuthCredentials('basic', 'usernameonly');
                expect(result).toEqual({
                    username: 'usernameonly',
                    password: '',
                });
            });

            it('handles empty credentials', () => {
                const result = parseAuthCredentials('basic', '');
                expect(result).toEqual({
                    username: '',
                    password: '',
                });
            });

            it('handles empty username with password', () => {
                const result = parseAuthCredentials('basic', ':password');
                expect(result).toEqual({
                    username: '',
                    password: 'password',
                });
            });
        });

        describe('bearer auth', () => {
            it('parses bearer token', () => {
                const result = parseAuthCredentials('bearer', 'my-jwt-token');
                expect(result).toEqual({ token: 'my-jwt-token' });
            });

            it('handles empty token', () => {
                const result = parseAuthCredentials('bearer', '');
                expect(result).toEqual({ token: '' });
            });
        });

        describe('api_key auth', () => {
            it('parses credentials with colon separator', () => {
                const result = parseAuthCredentials('api_key', 'X-API-Key:secret123');
                expect(result).toEqual({
                    headerName: 'X-API-Key',
                    apiKeyValue: 'secret123',
                });
            });

            it('handles API key value containing colons', () => {
                const result = parseAuthCredentials('api_key', 'X-Key:value:with:colons');
                expect(result).toEqual({
                    headerName: 'X-Key',
                    apiKeyValue: 'value:with:colons',
                });
            });

            it('handles credentials without colon separator', () => {
                const result = parseAuthCredentials('api_key', 'headernameonly');
                expect(result).toEqual({
                    headerName: 'headernameonly',
                    apiKeyValue: '',
                });
            });

            it('handles empty credentials', () => {
                const result = parseAuthCredentials('api_key', '');
                expect(result).toEqual({
                    headerName: '',
                    apiKeyValue: '',
                });
            });
        });

        describe('none and unknown auth', () => {
            it('returns empty object for none auth type', () => {
                const result = parseAuthCredentials('none', 'anything');
                expect(result).toEqual({});
            });

            it('returns empty object for unknown auth type', () => {
                const result = parseAuthCredentials('oauth', 'credentials');
                expect(result).toEqual({});
            });

            it('returns empty object for empty auth type', () => {
                const result = parseAuthCredentials('', 'credentials');
                expect(result).toEqual({});
            });
        });
    });

    describe('buildAuthCredentials', () => {
        describe('basic auth', () => {
            it('builds credentials from username and password', () => {
                const result = buildAuthCredentials('basic', {
                    username: 'myuser',
                    password: 'mypassword',
                });
                expect(result).toBe('myuser:mypassword');
            });

            it('handles missing username', () => {
                const result = buildAuthCredentials('basic', {
                    password: 'mypassword',
                });
                expect(result).toBe(':mypassword');
            });

            it('handles missing password', () => {
                const result = buildAuthCredentials('basic', {
                    username: 'myuser',
                });
                expect(result).toBe('myuser:');
            });

            it('handles empty fields object', () => {
                const result = buildAuthCredentials('basic', {});
                expect(result).toBe(':');
            });
        });

        describe('bearer auth', () => {
            it('builds credentials from token', () => {
                const result = buildAuthCredentials('bearer', { token: 'jwt-token' });
                expect(result).toBe('jwt-token');
            });

            it('handles missing token', () => {
                const result = buildAuthCredentials('bearer', {});
                expect(result).toBe('');
            });
        });

        describe('api_key auth', () => {
            it('builds credentials from header name and value', () => {
                const result = buildAuthCredentials('api_key', {
                    headerName: 'X-API-Key',
                    apiKeyValue: 'secret123',
                });
                expect(result).toBe('X-API-Key:secret123');
            });

            it('handles missing header name', () => {
                const result = buildAuthCredentials('api_key', {
                    apiKeyValue: 'secret',
                });
                expect(result).toBe(':secret');
            });

            it('handles missing API key value', () => {
                const result = buildAuthCredentials('api_key', {
                    headerName: 'X-Key',
                });
                expect(result).toBe('X-Key:');
            });

            it('handles empty fields object', () => {
                const result = buildAuthCredentials('api_key', {});
                expect(result).toBe(':');
            });
        });

        describe('none and unknown auth', () => {
            it('returns empty string for none auth type', () => {
                const result = buildAuthCredentials('none', { anything: 'value' });
                expect(result).toBe('');
            });

            it('returns empty string for unknown auth type', () => {
                const result = buildAuthCredentials('oauth', { token: 'value' });
                expect(result).toBe('');
            });

            it('returns empty string for empty auth type', () => {
                const result = buildAuthCredentials('', { token: 'value' });
                expect(result).toBe('');
            });
        });
    });

    describe('headersObjectToArray', () => {
        beforeEach(() => {
            vi.stubGlobal('crypto', {
                randomUUID: vi.fn()
                    .mockReturnValueOnce('uuid-1')
                    .mockReturnValueOnce('uuid-2')
                    .mockReturnValueOnce('uuid-3'),
            });
        });

        it('converts empty object to empty array', () => {
            const result = headersObjectToArray({});
            expect(result).toEqual([]);
        });

        it('converts single entry with generated ID', () => {
            const result = headersObjectToArray({ 'Content-Type': 'application/json' });
            expect(result).toEqual([
                { id: 'uuid-1', key: 'Content-Type', value: 'application/json' },
            ]);
        });

        it('converts multiple entries with unique IDs', () => {
            const result = headersObjectToArray({
                'Content-Type': 'application/json',
                'Authorization': 'Bearer token',
            });
            expect(result).toHaveLength(2);
            expect(result[0]).toEqual({
                id: 'uuid-1',
                key: 'Content-Type',
                value: 'application/json',
            });
            expect(result[1]).toEqual({
                id: 'uuid-2',
                key: 'Authorization',
                value: 'Bearer token',
            });
        });

        it('generates unique IDs for each entry', () => {
            const result = headersObjectToArray({
                'X-Header-1': 'value1',
                'X-Header-2': 'value2',
                'X-Header-3': 'value3',
            });
            const ids = result.map((h) => h.id);
            expect(ids).toEqual(['uuid-1', 'uuid-2', 'uuid-3']);
        });

        it('preserves empty values', () => {
            const result = headersObjectToArray({ 'X-Empty': '' });
            expect(result).toEqual([{ id: 'uuid-1', key: 'X-Empty', value: '' }]);
        });
    });

    describe('headersArrayToObject', () => {
        it('converts empty array to empty object', () => {
            const result = headersArrayToObject([]);
            expect(result).toEqual({});
        });

        it('converts single entry', () => {
            const headers: HeaderEntry[] = [
                { id: 'id-1', key: 'Content-Type', value: 'application/json' },
            ];
            const result = headersArrayToObject(headers);
            expect(result).toEqual({ 'Content-Type': 'application/json' });
        });

        it('converts multiple entries', () => {
            const headers: HeaderEntry[] = [
                { id: 'id-1', key: 'Content-Type', value: 'application/json' },
                { id: 'id-2', key: 'Authorization', value: 'Bearer token' },
            ];
            const result = headersArrayToObject(headers);
            expect(result).toEqual({
                'Content-Type': 'application/json',
                'Authorization': 'Bearer token',
            });
        });

        it('filters out entries with blank keys', () => {
            const headers: HeaderEntry[] = [
                { id: 'id-1', key: 'Valid-Key', value: 'valid-value' },
                { id: 'id-2', key: '', value: 'orphan-value' },
                { id: 'id-3', key: '   ', value: 'whitespace-key' },
            ];
            const result = headersArrayToObject(headers);
            expect(result).toEqual({ 'Valid-Key': 'valid-value' });
        });

        it('trims key names', () => {
            const headers: HeaderEntry[] = [
                { id: 'id-1', key: '  Content-Type  ', value: 'application/json' },
            ];
            const result = headersArrayToObject(headers);
            expect(result).toEqual({ 'Content-Type': 'application/json' });
        });

        it('preserves empty values', () => {
            const headers: HeaderEntry[] = [
                { id: 'id-1', key: 'X-Empty', value: '' },
            ];
            const result = headersArrayToObject(headers);
            expect(result).toEqual({ 'X-Empty': '' });
        });

        it('ignores the id field in output', () => {
            const headers: HeaderEntry[] = [
                { id: 'should-not-appear', key: 'X-Test', value: 'test' },
            ];
            const result = headersArrayToObject(headers);
            expect(result).toEqual({ 'X-Test': 'test' });
            expect('id' in result).toBe(false);
        });
    });

    describe('round-trip conversions', () => {
        beforeEach(() => {
            let counter = 0;
            vi.stubGlobal('crypto', {
                randomUUID: vi.fn(() => `uuid-${++counter}`),
            });
        });

        it('parse then build basic auth preserves data', () => {
            const original = 'myuser:mypassword';
            const parsed = parseAuthCredentials('basic', original);
            const rebuilt = buildAuthCredentials('basic', parsed);
            expect(rebuilt).toBe(original);
        });

        it('parse then build bearer auth preserves data', () => {
            const original = 'my-jwt-token';
            const parsed = parseAuthCredentials('bearer', original);
            const rebuilt = buildAuthCredentials('bearer', parsed);
            expect(rebuilt).toBe(original);
        });

        it('parse then build api_key auth preserves data', () => {
            const original = 'X-API-Key:secret123';
            const parsed = parseAuthCredentials('api_key', original);
            const rebuilt = buildAuthCredentials('api_key', parsed);
            expect(rebuilt).toBe(original);
        });

        it('array to object to array preserves header data (excluding IDs)', () => {
            const original: HeaderEntry[] = [
                { id: 'orig-1', key: 'Content-Type', value: 'application/json' },
                { id: 'orig-2', key: 'Authorization', value: 'Bearer token' },
            ];
            const asObject = headersArrayToObject(original);
            const backToArray = headersObjectToArray(asObject);

            // IDs will be different, but keys and values should match
            expect(backToArray).toHaveLength(2);
            const keys = backToArray.map((h) => h.key);
            const values = backToArray.map((h) => h.value);
            expect(keys).toContain('Content-Type');
            expect(keys).toContain('Authorization');
            expect(values).toContain('application/json');
            expect(values).toContain('Bearer token');
        });

        it('object to array to object preserves header data', () => {
            const original: Record<string, string> = {
                'Content-Type': 'application/json',
                'X-Custom': 'custom-value',
            };
            const asArray = headersObjectToArray(original);
            const backToObject = headersArrayToObject(asArray);
            expect(backToObject).toEqual(original);
        });
    });
});

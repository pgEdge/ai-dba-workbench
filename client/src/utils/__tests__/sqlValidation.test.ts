/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Coverage for the SQL validation client added by issue #532: request
// shape, the bounded concurrency window, result caching, graceful
// degradation on failure, and the cluster routing-comment validator.

import { describe, it, expect, vi, beforeEach } from 'vitest';

const mockApiFetch = vi.fn();
vi.mock('../apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

import {
    validateSql,
    createSqlValidator,
    createRoutedSqlValidator,
    extractSqlCodeBlocks,
    clearSqlValidationCache,
    MAX_CONCURRENT_VALIDATIONS,
} from '../sqlValidation';

const validResponse = {
    valid: true,
    total_statements: 1,
    statements: [{ query: 'SELECT 1', status: 'valid', error: '' }],
};

const okResponse = (body: unknown) => ({
    ok: true,
    status: 200,
    json: vi.fn().mockResolvedValue(body),
});

beforeEach(() => {
    mockApiFetch.mockReset();
    clearSqlValidationCache();
});

describe('validateSql', () => {
    it('posts the query to the validation endpoint', async () => {
        mockApiFetch.mockResolvedValue(okResponse(validResponse));

        const result = await validateSql(4, 'SELECT 1', 'appdb');

        expect(result).toEqual(validResponse);
        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/connections/4/query/validate',
            expect.objectContaining({ method: 'POST' }),
        );
        const init = mockApiFetch.mock.calls[0][1] as { body: string };
        expect(JSON.parse(init.body)).toEqual({
            query: 'SELECT 1',
            database_name: 'appdb',
        });
    });

    it('omits database_name when none is supplied', async () => {
        mockApiFetch.mockResolvedValue(okResponse(validResponse));

        await validateSql(4, 'SELECT 1');

        const init = mockApiFetch.mock.calls[0][1] as { body: string };
        expect(JSON.parse(init.body)).toEqual({ query: 'SELECT 1' });
    });

    it('resolves to null without a request for empty SQL', async () => {
        expect(await validateSql(4, '   ')).toBeNull();
        expect(mockApiFetch).not.toHaveBeenCalled();
    });

    it('caches results so repeated renders do not re-validate', async () => {
        mockApiFetch.mockResolvedValue(okResponse(validResponse));

        await validateSql(4, 'SELECT 1');
        await validateSql(4, 'SELECT 1');

        expect(mockApiFetch).toHaveBeenCalledTimes(1);
    });

    it('does not cache a failed request', async () => {
        mockApiFetch.mockResolvedValueOnce({ ok: false, status: 500 });
        expect(await validateSql(4, 'SELECT 1')).toBeNull();

        mockApiFetch.mockResolvedValueOnce(okResponse(validResponse));
        expect(await validateSql(4, 'SELECT 1')).toEqual(validResponse);
        expect(mockApiFetch).toHaveBeenCalledTimes(2);
    });

    it('resolves to null when the request throws', async () => {
        mockApiFetch.mockRejectedValue(new Error('offline'));
        expect(await validateSql(4, 'SELECT 1')).toBeNull();
    });

    it('resolves to null when the payload has no statements array', async () => {
        mockApiFetch.mockResolvedValue(okResponse({ valid: true }));
        expect(await validateSql(4, 'SELECT 1')).toBeNull();
    });

    it('bounds the number of requests in flight at once', async () => {
        let peak = 0;
        let live = 0;

        mockApiFetch.mockImplementation(async () => {
            live++;
            peak = Math.max(peak, live);
            await new Promise((resolve) => setTimeout(resolve, 0));
            live--;
            return okResponse(validResponse);
        });

        await Promise.all(
            Array.from({ length: 10 }, (_, i) =>
                validateSql(4, `SELECT ${i}`)),
        );

        expect(mockApiFetch).toHaveBeenCalledTimes(10);
        expect(peak).toBe(MAX_CONCURRENT_VALIDATIONS);
    });
});

describe('createSqlValidator', () => {
    it('binds the connection and database', async () => {
        mockApiFetch.mockResolvedValue(okResponse(validResponse));
        const validator = createSqlValidator(9, 'reports');

        await validator('SELECT 1');

        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/connections/9/query/validate',
            expect.anything(),
        );
    });
});

describe('createRoutedSqlValidator', () => {
    it('routes on the connection_id comment and strips it', async () => {
        mockApiFetch.mockResolvedValue(okResponse(validResponse));
        const validator = createRoutedSqlValidator();

        await validator('-- connection_id: 12\nSELECT 1;');

        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/connections/12/query/validate',
            expect.anything(),
        );
        const init = mockApiFetch.mock.calls[0][1] as { body: string };
        expect(JSON.parse(init.body).query).toBe('SELECT 1;');
    });

    it('falls back to the default connection', async () => {
        mockApiFetch.mockResolvedValue(okResponse(validResponse));
        const validator = createRoutedSqlValidator(3);

        await validator('SELECT 1;');

        expect(mockApiFetch).toHaveBeenCalledWith(
            '/api/v1/connections/3/query/validate',
            expect.anything(),
        );
    });

    it('skips a block with neither a comment nor a default', async () => {
        const validator = createRoutedSqlValidator();
        expect(await validator('SELECT 1;')).toBeNull();
        expect(mockApiFetch).not.toHaveBeenCalled();
    });
});

describe('extractSqlCodeBlocks', () => {
    it('returns the body of each fenced sql block in order', () => {
        const markdown = [
            'Intro text',
            '```sql',
            'SELECT 1;',
            '```',
            'More prose',
            '```bash',
            'ls -l',
            '```',
            '```SQL',
            'SELECT 2;',
            '```',
        ].join('\n');

        expect(extractSqlCodeBlocks(markdown)).toEqual([
            'SELECT 1;',
            'SELECT 2;',
        ]);
    });

    it('ignores empty blocks and documents with none', () => {
        expect(extractSqlCodeBlocks('```sql\n\n```')).toEqual([]);
        expect(extractSqlCodeBlocks('no code here')).toEqual([]);
    });
});

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
    buildNotExplainableMessage,
    getStatementKeyword,
    isExplainable,
} from '../sqlHelpers';

// ---------------------------------------------------------------------------
// Statements PostgreSQL's EXPLAIN accepts (verified on PostgreSQL 18)
// ---------------------------------------------------------------------------

const EXPLAINABLE = [
    'SELECT 1',
    'select * from users where id = $1',
    "INSERT INTO t (a) VALUES ('x')",
    'UPDATE t SET a = 1 WHERE b = 2',
    'DELETE FROM t WHERE a = 1',
    'MERGE INTO t USING s ON true WHEN MATCHED THEN DO NOTHING',
    'VALUES (1), (2)',
    'EXECUTE my_plan(1)',
    'DECLARE c CURSOR FOR SELECT 1',
    'WITH cte AS (SELECT 1) SELECT * FROM cte',
    'TABLE users',
    '(SELECT 1) UNION (SELECT 2)',
    'CREATE TABLE snapshot AS SELECT * FROM t',
    'CREATE TEMP TABLE snapshot AS SELECT * FROM t',
    'CREATE GLOBAL TEMPORARY TABLE s AS SELECT 1',
    'CREATE UNLOGGED TABLE s AS SELECT 1',
    'CREATE MATERIALIZED VIEW mv AS SELECT 1',
    'REFRESH MATERIALIZED VIEW mv',
];

// ---------------------------------------------------------------------------
// Statements EXPLAIN rejects (verified on PostgreSQL 18)
// ---------------------------------------------------------------------------

const NOT_EXPLAINABLE = [
    'VACUUM',
    'VACUUM (ANALYZE, VERBOSE)',
    'ANALYZE',
    'ANALYZE public.users',
    'REINDEX INDEX users_pkey',
    'CLUSTER users USING users_pkey',
    'CREATE INDEX idx ON t (a)',
    'CREATE TABLE t (a int)',
    'CREATE VIEW v AS SELECT 1',
    'ALTER TABLE t ADD COLUMN b int',
    'DROP TABLE t',
    'TRUNCATE t',
    'COPY t TO STDOUT',
    "SET work_mem = '4MB'",
    'SHOW all',
    'CALL my_proc()',
    'DO $$ BEGIN END $$',
    'CHECKPOINT',
    'BEGIN',
    'COMMIT',
    'GRANT SELECT ON t TO app',
    'REFRESH PUBLICATION pub',
    'LISTEN chan',
];

describe('getStatementKeyword', () => {
    it('returns the leading keyword in upper case', () => {
        expect(getStatementKeyword('vacuum verbose')).toBe('VACUUM');
        expect(getStatementKeyword('SELECT 1')).toBe('SELECT');
    });

    it('skips leading whitespace and line comments', () => {
        expect(
            getStatementKeyword('  -- daily job\n  VACUUM'),
        ).toBe('VACUUM');
    });

    it('skips leading block comments', () => {
        expect(
            getStatementKeyword('/* app: cron */ ANALYZE users'),
        ).toBe('ANALYZE');
    });

    it('skips leading parentheses', () => {
        expect(getStatementKeyword('((SELECT 1))')).toBe('SELECT');
    });

    it('returns an empty string when no keyword is present', () => {
        expect(getStatementKeyword('')).toBe('');
        expect(getStatementKeyword('   ')).toBe('');
        expect(getStatementKeyword('-- only a comment')).toBe('');
        expect(getStatementKeyword('123')).toBe('');
    });

    it('tolerates a nullish query', () => {
        expect(
            getStatementKeyword(
                undefined as unknown as string,
            ),
        ).toBe('');
    });
});

describe('isExplainable', () => {
    it.each(EXPLAINABLE)('accepts %s', (query) => {
        expect(isExplainable(query)).toBe(true);
    });

    it.each(NOT_EXPLAINABLE)('rejects %s', (query) => {
        expect(isExplainable(query)).toBe(false);
    });

    it('is case insensitive', () => {
        expect(isExplainable('create materialized view mv as select 1'))
            .toBe(true);
        expect(isExplainable('vacuum full')).toBe(false);
    });

    it('ignores leading comments when classifying', () => {
        expect(isExplainable('-- nightly\nVACUUM ANALYZE')).toBe(false);
        expect(isExplainable('/* app */ SELECT 1')).toBe(true);
    });

    it('rejects empty or unrecognisable text', () => {
        expect(isExplainable('')).toBe(false);
        expect(isExplainable('   ')).toBe(false);
        expect(
            isExplainable(undefined as unknown as string),
        ).toBe(false);
    });
});

describe('buildNotExplainableMessage', () => {
    it('names the statement type when known', () => {
        const msg = buildNotExplainableMessage('VACUUM');
        expect(msg).toContain('VACUUM statements');
        expect(msg).toContain('EXPLAIN supports only');
    });

    it('falls back to a generic subject when unknown', () => {
        expect(buildNotExplainableMessage('')).toContain(
            'this statement',
        );
    });
});

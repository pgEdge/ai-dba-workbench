/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Unit coverage for the SQL detection helpers used by the markdown code
 * block components. The helpers decide whether a fenced code block is
 * SQL, extract executable SQL out of mixed payloads (LLMs sometimes
 * paste shell commands or postgresql.conf entries inside a sql block),
 * and pull the language tag out of a className. All four helpers are
 * pure functions, so the tests are direct input/output assertions.
 */

import { describe, it, expect } from 'vitest';
import {
    SQL_KEYWORDS_RE,
    SQL_STATEMENT_KEYWORDS,
    CONNECTION_ID_COMMENT_RE,
    splitSqlStatements,
    tokenizeSql,
    hasSqlParameters,
    stripConnectionIdComment,
    extractExecutableSQL,
    isSqlCodeBlock,
    extractLanguage,
} from '../sqlDetection';

describe('SQL_KEYWORDS_RE', () => {
    it('matches common DML/DDL keywords case-insensitively', () => {
        for (const kw of ['select', 'WITH', 'Show', 'EXPLAIN', 'CREATE', 'drop']) {
            expect(SQL_KEYWORDS_RE.test(`${kw} something`)).toBe(true);
        }
    });

    it('does not match arbitrary tokens', () => {
        expect(SQL_KEYWORDS_RE.test('echo hello')).toBe(false);
        expect(SQL_KEYWORDS_RE.test('# config entry')).toBe(false);
        expect(SQL_KEYWORDS_RE.test('SELECTOR')).toBe(false);
    });
});

describe('SQL_STATEMENT_KEYWORDS', () => {
    it('matches common DML/DDL keywords with leading whitespace', () => {
        expect(SQL_STATEMENT_KEYWORDS.test('  SELECT 1')).toBe(true);
        expect(SQL_STATEMENT_KEYWORDS.test('\tINSERT INTO t VALUES (1)')).toBe(true);
        expect(SQL_STATEMENT_KEYWORDS.test('REFRESH MATERIALIZED VIEW v')).toBe(true);
    });

    it('does not match non-SQL prefixes', () => {
        expect(SQL_STATEMENT_KEYWORDS.test('echo hi')).toBe(false);
        expect(SQL_STATEMENT_KEYWORDS.test('shared_buffers = 128MB')).toBe(false);
    });
});

describe('extractExecutableSQL', () => {
    it('returns SQL chunks reassembled with semicolons and double newlines', () => {
        const input = 'SELECT 1; UPDATE t SET x=1';
        expect(extractExecutableSQL(input)).toBe(
            'SELECT 1;\n\nUPDATE t SET x=1;',
        );
    });

    it('drops chunks that do not start with a recognised SQL keyword', () => {
        // The shell command is dropped; only the SELECT survives.
        const input = 'echo hello; SELECT 2';
        expect(extractExecutableSQL(input)).toBe('SELECT 2;');
    });

    it('ignores leading SQL comment lines when classifying chunks', () => {
        const input = `
            -- a leading comment
            SELECT 42
        `;
        const result = extractExecutableSQL(input);
        // The comment line stays in the preserved chunk; the chunk is
        // recognised because the non-comment content starts with SELECT.
        expect(result).toContain('SELECT 42');
        expect(result.endsWith(';')).toBe(true);
    });

    it('returns empty string when no chunk is SQL', () => {
        expect(extractExecutableSQL('-- only a comment')).toBe('');
        expect(extractExecutableSQL('echo a; echo b')).toBe('');
        expect(extractExecutableSQL('')).toBe('');
    });

    it('skips empty chunks created by trailing or repeated semicolons', () => {
        const input = 'SELECT 1;;;;';
        // Only one SQL chunk emerges.
        expect(extractExecutableSQL(input)).toBe('SELECT 1;');
    });

    it('keeps multiline SQL chunks intact', () => {
        const input = `SELECT a,
            b,
            c
        FROM t`;
        const result = extractExecutableSQL(input);
        expect(result).toContain('SELECT a,');
        expect(result).toContain('b,');
        expect(result).toContain('FROM t');
        expect(result.endsWith(';')).toBe(true);
    });
});

describe('isSqlCodeBlock', () => {
    it('returns true for explicit language-sql class', () => {
        expect(isSqlCodeBlock('language-sql', 'anything')).toBe(true);
        expect(isSqlCodeBlock('something language-SQL more', 'foo')).toBe(true);
    });

    it('returns false for a non-sql language tag', () => {
        expect(isSqlCodeBlock('language-bash', 'SELECT 1')).toBe(false);
        expect(isSqlCodeBlock('language-python', 'WITH x AS (SELECT 1)')).toBe(
            false,
        );
    });

    it('returns true for untagged blocks that begin with a SQL keyword', () => {
        expect(isSqlCodeBlock(undefined, '  SELECT * FROM t')).toBe(true);
        expect(isSqlCodeBlock(undefined, 'WITH x AS (SELECT 1) SELECT 2')).toBe(
            true,
        );
    });

    it('returns false for untagged blocks that do not start with SQL', () => {
        expect(isSqlCodeBlock(undefined, 'echo hello')).toBe(false);
        expect(isSqlCodeBlock(undefined, '')).toBe(false);
    });

    it('returns false when className is set but lacks a language- prefix', () => {
        // Hits the `!className` short-circuit branch only when className
        // is undefined; a non-empty className without a `language-`
        // match falls through to `return false`.
        expect(isSqlCodeBlock('hljs custom', 'SELECT 1')).toBe(false);
    });
});

describe('extractLanguage', () => {
    it('returns the language portion of a language- class', () => {
        expect(extractLanguage('language-sql')).toBe('sql');
        expect(extractLanguage('hljs language-typescript foo')).toBe(
            'typescript',
        );
    });

    it('returns empty string when no language- token is present', () => {
        expect(extractLanguage('hljs')).toBe('');
        expect(extractLanguage(undefined)).toBe('');
        expect(extractLanguage('')).toBe('');
    });
});

// ---------------------------------------------------------------------------
// SQL-aware splitting and parameter detection (issue #532)
// ---------------------------------------------------------------------------

describe('splitSqlStatements', () => {
    it('splits on top-level semicolons', () => {
        expect(splitSqlStatements('SELECT 1; SELECT 2;')).toEqual([
            'SELECT 1',
            'SELECT 2',
        ]);
    });

    it('ignores a semicolon inside a single-quoted literal', () => {
        expect(splitSqlStatements(
            "SELECT 'a;b' AS x; SELECT 2;",
        )).toEqual(["SELECT 'a;b' AS x", 'SELECT 2']);
    });

    it('honours the doubled-quote escape inside a literal', () => {
        expect(splitSqlStatements(
            "SELECT 'it''s; fine' AS x;",
        )).toEqual(["SELECT 'it''s; fine' AS x"]);
    });

    it('ignores a semicolon inside a quoted identifier', () => {
        expect(splitSqlStatements(
            'SELECT 1 AS "odd;name";',
        )).toEqual(['SELECT 1 AS "odd;name"']);
    });

    it('ignores semicolons inside a dollar-quoted body', () => {
        const body = 'CREATE FUNCTION f() RETURNS int AS $$'
            + '\nBEGIN; RETURN 1; END;\n$$ LANGUAGE plpgsql;';
        expect(splitSqlStatements(body)).toHaveLength(1);
    });

    it('ignores semicolons inside a tagged dollar-quoted body', () => {
        const body = 'CREATE FUNCTION f() RETURNS int AS $body$'
            + '\nSELECT 1; SELECT 2;\n$body$ LANGUAGE sql;';
        expect(splitSqlStatements(body)).toHaveLength(1);
    });

    it('ignores a semicolon inside a line comment', () => {
        expect(splitSqlStatements(
            'SELECT 1 -- trailing; note\n;SELECT 2;',
        )).toEqual(['SELECT 1 -- trailing; note', 'SELECT 2']);
    });

    it('ignores semicolons inside a nested block comment', () => {
        expect(splitSqlStatements(
            'SELECT 1 /* a; /* b; */ c; */ ;SELECT 2;',
        )).toEqual([
            'SELECT 1 /* a; /* b; */ c; */',
            'SELECT 2',
        ]);
    });

    it('tolerates an unterminated literal or comment', () => {
        expect(splitSqlStatements("SELECT 'unterminated")).toEqual([
            "SELECT 'unterminated",
        ]);
        expect(splitSqlStatements('SELECT 1 /* open')).toEqual([
            'SELECT 1 /* open',
        ]);
        expect(splitSqlStatements('SELECT $$open')).toEqual([
            'SELECT $$open',
        ]);
    });

    it('drops empty statements', () => {
        expect(splitSqlStatements(';;  ;')).toEqual([]);
        expect(splitSqlStatements('')).toEqual([]);
    });
});

describe('extractExecutableSQL with literals', () => {
    it('keeps a statement whose literal contains a semicolon intact', () => {
        expect(extractExecutableSQL(
            "SELECT * FROM t WHERE c = 'a;b';",
        )).toBe("SELECT * FROM t WHERE c = 'a;b';");
    });

    it('still discards non-SQL chunks', () => {
        expect(extractExecutableSQL(
            'shared_buffers = 8GB;\nSELECT 1;',
        )).toBe('SELECT 1;');
    });

    it('keeps a leading comment attached to its statement', () => {
        expect(extractExecutableSQL('-- why\nSELECT 1;')).toBe(
            '-- why\nSELECT 1;',
        );
    });
});

describe('hasSqlParameters', () => {
    it('detects numbered bind parameters', () => {
        expect(hasSqlParameters('SELECT * FROM t WHERE id = $1;')).toBe(true);
        expect(hasSqlParameters('SELECT $1, $2;')).toBe(true);
    });

    it('returns false for SQL without parameters', () => {
        expect(hasSqlParameters('SELECT * FROM t WHERE id = 1;')).toBe(false);
        expect(hasSqlParameters('')).toBe(false);
    });

    it('ignores a dollar-quoted body and its contents', () => {
        expect(hasSqlParameters(
            'CREATE FUNCTION f() RETURNS int AS $$SELECT $1$$ LANGUAGE sql;',
        )).toBe(false);
    });

    it('ignores a placeholder inside a comment or literal', () => {
        expect(hasSqlParameters('SELECT 1; -- use $1 here')).toBe(false);
        expect(hasSqlParameters("SELECT 'costs $5' AS x;")).toBe(false);
    });
});

describe('stripConnectionIdComment', () => {
    it('removes the routing comment', () => {
        expect(stripConnectionIdComment(
            '-- connection_id: 12\nSELECT 1;',
        )).toBe('SELECT 1;');
    });

    it('leaves a block without the comment unchanged', () => {
        expect(stripConnectionIdComment('SELECT 1;')).toBe('SELECT 1;');
    });

    it('captures the connection id', () => {
        const match = CONNECTION_ID_COMMENT_RE.exec(
            '-- connection_id: 7\nSELECT 1;',
        );
        expect(match?.[1]).toBe('7');
    });
});

describe('tokenizeSql', () => {
    it('labels each lexical run', () => {
        const kinds = tokenizeSql("SELECT 'x'; -- c").map(t => t.kind);
        expect(kinds).toEqual(['plain', 'literal', 'separator', 'plain', 'comment']);
    });
});

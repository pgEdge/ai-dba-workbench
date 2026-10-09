/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - SQL detection helpers for code blocks.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Regex matching common SQL keywords at the start of a string.
 * Used by isSqlCodeBlock to detect untagged SQL code blocks.
 */
export const SQL_KEYWORDS_RE = /^(SELECT|WITH|SHOW|EXPLAIN|SET|ALTER|CREATE|DROP|INSERT|UPDATE|DELETE|VACUUM|ANALYZE|REINDEX|CLUSTER)\b/i;

/**
 * Regex matching the start of a SQL statement keyword.
 * Used by extractExecutableSQL to identify valid SQL chunks.
 */
export const SQL_STATEMENT_KEYWORDS = /^\s*(SELECT|WITH|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|EXPLAIN|SET|VACUUM|REINDEX|GRANT|REVOKE|TRUNCATE|CLUSTER|REFRESH|COMMENT|TABLE|ANALYZE)\b/i;

/**
 * Regex matching the routing comment that cluster analysis prepends to a
 * SQL block to say which connection the statement should run against.
 * The first capture group holds the connection ID. Like any `--`
 * comment, it ends at a carriage return or a newline.
 */
export const CONNECTION_ID_COMMENT_RE = /^--\s*connection_id:\s*(\d+)\s*[\r\n]/;

/**
 * Regex matching a `$N` bind-parameter placeholder.
 *
 * Follows the `hasParameters` check in `useQueryPlan`: a statement
 * carrying placeholders is a template, not something the user can run
 * without supplying values. A `$` preceded by an identifier character is
 * part of an identifier such as `col$1`, which PostgreSQL allows after
 * the first character, so it is not a placeholder.
 */
export const SQL_PARAMETER_RE = /(?<![\p{L}\p{N}_$])\$\d+/u;

/**
 * Regex matching the opening delimiter of a dollar-quoted string, either
 * the anonymous `$$` form or a tagged `$tag$` form.
 */
const DOLLAR_TAG_RE = /^\$(?:\$|[A-Za-z_][A-Za-z0-9_]*\$)/;

/** The kinds of token the SQL scanner recognises. */
export type SqlTokenKind = 'plain' | 'comment' | 'literal' | 'separator';

/** A single lexical run of SQL text. */
export interface SqlToken {
    kind: SqlTokenKind;
    text: string;
}

/**
 * Strip the `-- connection_id: N` routing comment from a code block.
 * Blocks without the comment are returned unchanged.
 */
export const stripConnectionIdComment = (code: string): string =>
    code.replace(CONNECTION_ID_COMMENT_RE, '');

/**
 * Read a dollar-quoted string starting at `start`, returning the index
 * one past its closing delimiter, or -1 when `start` is not the opening
 * delimiter of a dollar-quoted string.
 */
const readDollarQuoted = (code: string, start: number): number => {
    const openMatch = DOLLAR_TAG_RE.exec(code.slice(start));
    if (!openMatch) {
        return -1;
    }
    const delimiter = openMatch[0];
    const closeIndex = code.indexOf(delimiter, start + delimiter.length);
    // An unterminated body runs to the end of the block.
    return closeIndex === -1 ? code.length : closeIndex + delimiter.length;
};

/**
 * Regex matching a character that can continue a PostgreSQL identifier,
 * which allows `$` after the first character.
 */
const IDENTIFIER_CHAR_RE = /[\p{L}\p{N}_$]/u;

/**
 * Read a quoted run (single-quoted literal or double-quoted identifier)
 * starting at `start`, honouring the doubled-quote escape, and return the
 * index one past the closing quote. In an escape string constant
 * (`E'...'`) a backslash also escapes the character after it.
 */
const readQuoted = (
    code: string,
    start: number,
    quote: string,
    backslashEscapes = false
): number => {
    let i = start + 1;
    while (i < code.length) {
        if (backslashEscapes && code[i] === '\\') {
            i += 2;
            continue;
        }
        if (code[i] === quote) {
            if (code[i + 1] === quote) {
                // Doubled quote: an escaped quote, not the terminator.
                i += 2;
                continue;
            }
            return i + 1;
        }
        i++;
    }
    return code.length;
};

/**
 * Read a block comment starting at `start`, honouring PostgreSQL's
 * nesting rules, and return the index one past its terminator.
 */
const readBlockComment = (code: string, start: number): number => {
    let depth = 0;
    let i = start;
    while (i < code.length) {
        if (code[i] === '/' && code[i + 1] === '*') {
            depth++;
            i += 2;
        } else if (code[i] === '*' && code[i + 1] === '/') {
            depth--;
            i += 2;
            if (depth === 0) {
                return i;
            }
        } else {
            i++;
        }
    }
    return code.length;
};

/**
 * Return the index one past a comment starting at `start`, or -1 when no
 * comment starts there. As in PostgreSQL's lexer and the server's
 * splitter, a `--` comment ends at the first `\r` or `\n`, which is left
 * outside the comment.
 */
const readComment = (code: string, start: number): number => {
    if (code.startsWith('--', start)) {
        let end = start + 2;
        while (end < code.length && code[end] !== '\r' && code[end] !== '\n') {
            end++;
        }
        return end;
    }
    if (code.startsWith('/*', start)) {
        return readBlockComment(code, start);
    }
    return -1;
};

/**
 * Report whether the `$` at `index` continues an identifier, as in
 * `col$a$`, rather than starting a token of its own.
 */
const continuesIdentifier = (code: string, index: number): boolean =>
    index > 0 && IDENTIFIER_CHAR_RE.test(code[index - 1]);

/**
 * Return the index one past a string literal, quoted identifier or
 * dollar-quoted string starting at `start`, or -1 when none starts there.
 */
const readLiteral = (code: string, start: number): number => {
    const ch = code[start];
    if (ch === "'" || ch === '"') {
        // An E or e prefix that starts a word makes this an escape
        // string constant, in which a backslash escapes the quote.
        const prefix = code[start - 1];
        const escapeString =
            ch === "'" &&
            (prefix === 'E' || prefix === 'e') &&
            !continuesIdentifier(code, start - 1);
        return readQuoted(code, start, ch, escapeString);
    }
    // A `$` that continues an identifier cannot open a dollar-quoted
    // string.
    if (ch === '$' && !continuesIdentifier(code, start)) {
        return readDollarQuoted(code, start);
    }
    return -1;
};

/**
 * Split SQL text into lexical tokens.
 *
 * Only enough of PostgreSQL's lexical structure is modelled to tell
 * statement separators apart from semicolons that sit inside string
 * literals, quoted identifiers, dollar-quoted bodies or comments.
 */
export const tokenizeSql = (code: string): SqlToken[] => {
    const tokens: SqlToken[] = [];
    let plain = '';
    let i = 0;

    const push = (kind: SqlTokenKind, stop: number): void => {
        if (plain) {
            tokens.push({ kind: 'plain', text: plain });
            plain = '';
        }
        if (stop > i) {
            tokens.push({ kind, text: code.slice(i, stop) });
        }
        i = stop;
    };

    while (i < code.length) {
        const commentStop = readComment(code, i);
        if (commentStop !== -1) {
            push('comment', commentStop);
            continue;
        }

        const literalStop = readLiteral(code, i);
        if (literalStop !== -1) {
            push('literal', literalStop);
            continue;
        }

        if (code[i] === ';') {
            push('separator', i + 1);
            continue;
        }

        plain += code[i];
        i++;
    }

    push('plain', i);
    return tokens;
};

/**
 * Split a SQL block into individual statements on top-level semicolons.
 *
 * Unlike a naive `split(';')`, semicolons inside string literals, quoted
 * identifiers, dollar-quoted function bodies and comments do not end a
 * statement. Empty statements are dropped and the trailing semicolon is
 * not included in the returned text.
 */
export const splitSqlStatements = (code: string): string[] => {
    const statements: string[] = [];
    let current = '';

    for (const token of tokenizeSql(code)) {
        if (token.kind === 'separator') {
            if (current.trim()) {
                statements.push(current.trim());
            }
            current = '';
            continue;
        }
        current += token.text;
    }

    if (current.trim()) {
        statements.push(current.trim());
    }

    return statements;
};

/**
 * Return the statement text with comments and literals blanked out, so
 * that keyword and placeholder checks only ever see real code. Each is
 * replaced by a space rather than removed, since PostgreSQL treats a
 * comment as whitespace, so `LIMIT`, an empty block comment and `$1`
 * must not run together as `LIMIT$1`.
 */
const sqlCodeOnly = (statement: string): string =>
    tokenizeSql(statement)
        .map((token) =>
            token.kind === 'literal' || token.kind === 'comment' ? ' ' : token.text
        )
        .join('')
        .trim();

/**
 * Determine whether a SQL block contains `$N` bind-parameter
 * placeholders, which make it a template rather than a runnable query.
 *
 * Placeholders inside comments, string literals and dollar-quoted bodies
 * are ignored, since those are ordinary text rather than parameters.
 */
export const hasSqlParameters = (code: string): boolean =>
    SQL_PARAMETER_RE.test(sqlCodeOnly(code));

/**
 * Extract only executable SQL from a code block.
 *
 * The LLM sometimes mixes configuration file entries or shell commands
 * into SQL code blocks.  This function splits the content into
 * statements using the SQL-aware splitter, keeps only those that start
 * with a recognised SQL keyword, and reassembles the result.
 */
export const extractExecutableSQL = (code: string): string => {
    const sqlParts: string[] = [];

    for (const statement of splitSqlStatements(code)) {
        const content = sqlCodeOnly(statement);
        if (content && SQL_STATEMENT_KEYWORDS.test(content)) {
            sqlParts.push(statement);
        }
    }

    return sqlParts.map((p) => `${p};`).join('\n\n');
};

/**
 * Determine whether a code block contains SQL.
 * Returns true when the block has a `language-sql` class, or when it has
 * no language tag and the trimmed content starts with a common SQL keyword.
 */
export const isSqlCodeBlock = (className: string | undefined, content: string): boolean => {
    const langMatch = /language-(\w+)/.exec(className ?? '');
    if (langMatch) {
        return langMatch[1].toLowerCase() === 'sql';
    }
    // No language tag -- check for SQL keyword at start
    if (!className) {
        return SQL_KEYWORDS_RE.test(content.trim());
    }
    return false;
};

/**
 * Return the language string extracted from a className, or empty string.
 */
export const extractLanguage = (className: string | undefined): string => {
    const match = /language-(\w+)/.exec(className ?? '');
    return match ? match[1] : '';
};

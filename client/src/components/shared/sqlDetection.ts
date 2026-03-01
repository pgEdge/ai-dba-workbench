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
 * Extract only executable SQL from a code block.
 *
 * The LLM sometimes mixes configuration file entries or shell commands
 * into SQL code blocks.  This function splits the content on semicolons,
 * keeps only the chunks that contain a recognised SQL keyword, and
 * reassembles the result.
 */
export const extractExecutableSQL = (code: string): string => {
    const parts = code.split(';');
    const sqlParts: string[] = [];

    for (const part of parts) {
        const trimmed = part.trim();
        if (!trimmed) {continue;}

        // Strip comment-only lines so we can inspect the real content
        const contentLines = trimmed
            .split('\n')
            .filter((line) => {
                const t = line.trim();
                return t && !t.startsWith('--');
            });

        const content = contentLines.join('\n').trim();
        if (content && SQL_STATEMENT_KEYWORDS.test(content)) {
            sqlParts.push(trimmed);
        }
    }

    return sqlParts.map((p) => p + ';').join('\n\n');
};

/**
 * Determine whether a code block contains SQL.
 * Returns true when the block has a `language-sql` class, or when it has
 * no language tag and the trimmed content starts with a common SQL keyword.
 */
export const isSqlCodeBlock = (className: string | undefined, content: string): boolean => {
    const langMatch = /language-(\w+)/.exec(className || '');
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
    const match = /language-(\w+)/.exec(className || '');
    return match ? match[1] : '';
};

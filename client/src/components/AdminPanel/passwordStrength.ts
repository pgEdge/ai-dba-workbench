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
 * Password strength heuristic shared by the AdminUsers password field
 * and its tests. The implementation lives in a plain TypeScript module
 * so it can be re-exported from the component file without tripping
 * the React Refresh "only export components" rule.
 *
 * The scoring function is intentionally lightweight; the server applies
 * the authoritative policy (NIST SP 800-63B alignment with a common
 * password dictionary). The client uses this routine purely to drive
 * the live strength meter.
 */

/**
 * Minimum password length aligned with NIST SP 800-63B and the
 * server-side policy. The server is authoritative; this constant is
 * used only for client-side feedback.
 */
export const PASSWORD_MIN_LENGTH = 12;

/**
 * Maximum password length matching the bcrypt 72-byte limit enforced
 * by the server. Values beyond this would be silently truncated by
 * bcrypt.
 */
export const PASSWORD_MAX_LENGTH = 72;

/**
 * Numeric score buckets returned by the strength scorer. The buckets
 * are ordered so that higher numbers indicate stronger passwords.
 */
export type PasswordStrength = 0 | 1 | 2 | 3 | 4;

/**
 * Character class buckets used by `charsetSize`. Each entry contributes
 * its `size` once when the password contains at least one character
 * matching its `pattern`. The "other" bucket approximates the printable
 * ASCII symbol set; non-ASCII glyphs also fall into that bucket.
 *
 * Encoding the buckets as a table (rather than an inline chain of
 * if-statements) keeps the cyclomatic complexity of `charsetSize` to a
 * single loop predicate, regardless of how many character classes the
 * heuristic eventually grows to recognise.
 */
const CHARSET_BUCKETS: readonly { pattern: RegExp; size: number }[] = [
    { pattern: /[a-z]/, size: 26 },
    { pattern: /[A-Z]/, size: 26 },
    { pattern: /[0-9]/, size: 10 },
    { pattern: /[^a-zA-Z0-9]/, size: 33 },
];

/**
 * Charset size estimate for a single character. We use rough buckets
 * rather than counting exact unicode classes because the goal is a
 * lightweight heuristic, not a cryptographic measure.
 */
function charsetSize(value: string): number {
    const size = CHARSET_BUCKETS.reduce(
        (acc, bucket) => acc + (bucket.pattern.test(value) ? bucket.size : 0),
        0,
    );
    return size || 1;
}

/**
 * Counts repeated-character runs of length 3 or more (e.g. "aaa") and
 * simple monotonic sequences of length 3 or more (e.g. "abc", "123").
 * Each occurrence reduces the effective entropy below.
 */
function repetitionPenalty(value: string): number {
    let runs = 0;
    let i = 0;
    while (i < value.length) {
        let j = i + 1;
        while (j < value.length && value[j] === value[i]) {
            j += 1;
        }
        if (j - i >= 3) {
            runs += 1;
        }
        i = j;
    }

    let sequences = 0;
    for (let k = 0; k + 2 < value.length; k += 1) {
        const a = value.charCodeAt(k);
        const b = value.charCodeAt(k + 1);
        const c = value.charCodeAt(k + 2);
        if ((b - a === 1 && c - b === 1) || (a - b === 1 && b - c === 1)) {
            sequences += 1;
        }
    }
    return runs + sequences;
}

/**
 * Counts the Unicode code points in a string. This matches the
 * server-side `utf8.RuneCountInString` behaviour and avoids the
 * UTF-16 code-unit miscount that JavaScript's native `.length`
 * property produces for characters outside the Basic Multilingual
 * Plane (for example, most emoji).
 */
export const codePointLength = (value: string): number => {
    return Array.from(value).length;
};

/**
 * Returns the UTF-8 byte length of a string. The server enforces a
 * 72-byte upper bound (the bcrypt limit) using Go's `len(password)`,
 * which counts bytes; this helper mirrors that count so the client
 * can flag strings the server would reject.
 */
export const utf8ByteLength = (value: string): number => {
    return new TextEncoder().encode(value).length;
};

/**
 * Returns an integer strength score from 0 (too short) through 4
 * (strong) using a length-and-entropy heuristic. Bucket boundaries
 * are chosen so that a 12-character password using mixed character
 * classes lands in the "good" bucket and longer or more varied
 * passwords reach "strong". Length is measured in code points so the
 * scorer agrees with the server's rune-based minimum check.
 */
export const scorePasswordStrength = (value: string): PasswordStrength => {
    const charCount = codePointLength(value);
    if (!value || charCount < PASSWORD_MIN_LENGTH) {
        return 0;
    }
    const entropy = charCount * Math.log2(charsetSize(value));
    const penalty = repetitionPenalty(value) * 6;
    const adjusted = Math.max(0, entropy - penalty);
    if (adjusted < 50) {
        return 1;
    }
    if (adjusted < 70) {
        return 2;
    }
    if (adjusted < 90) {
        return 3;
    }
    return 4;
};

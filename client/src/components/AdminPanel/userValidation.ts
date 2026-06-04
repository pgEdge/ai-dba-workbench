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
 * Username and email validation helpers shared by the AdminUsers create
 * and edit dialogs and their tests. These live in a plain TypeScript
 * module so they can be imported without tripping the React Refresh
 * "only export components" rule.
 *
 * The username rule mirrors the authoritative server-side check in
 * `server/src/internal/auth/store.go` (`ValidateUsername`): the first
 * character must be a Unicode letter or digit, subsequent characters may
 * be letters, digits, or one of `_ . @ -`, and the length must not
 * exceed 128 code points. The server remains authoritative; these
 * helpers exist purely to give immediate inline feedback.
 */

/**
 * Maximum username length in Unicode code points, matching the server's
 * `utf8.RuneCountInString` bound in `ValidateUsername`.
 */
export const USERNAME_MAX_LENGTH = 128;

/**
 * Inline helper text shown when a username contains characters the
 * server would reject or otherwise fails the format rule.
 */
export const USERNAME_HELPER_TEXT =
    'Username may only contain letters, digits, and . _ - @';

/**
 * Inline helper text shown when an email address is not well-formed.
 */
export const EMAIL_HELPER_TEXT = 'Enter a valid email address';

// First character: a Unicode letter or digit. The `u` flag enables the
// \p{L} (letter) and \p{N} (number) property escapes so the client
// agrees with the server's unicode.IsLetter / unicode.IsDigit checks.
const USERNAME_FIRST_CHAR = /[\p{L}\p{N}]/u;

// Subsequent characters: letters, digits, or one of _ . @ - .
const USERNAME_REST_CHAR = /[\p{L}\p{N}_.@-]/u;

/**
 * Counts the Unicode code points in a string so the length check agrees
 * with the server's rune-based bound rather than JavaScript's UTF-16
 * code-unit `.length`.
 */
const codePoints = (value: string): string[] => Array.from(value);

/**
 * Returns true when the username satisfies the server-side format rule:
 * a leading letter or digit, then letters, digits, or `_ . @ -`, with a
 * maximum of 128 code points. An empty string is treated as invalid
 * here; callers gate on emptiness separately via the field's `required`
 * behaviour so the inline error only appears once the user types.
 */
export const isValidUsername = (username: string): boolean => {
    const chars = codePoints(username);
    if (chars.length === 0 || chars.length > USERNAME_MAX_LENGTH) {
        return false;
    }
    if (!USERNAME_FIRST_CHAR.test(chars[0])) {
        return false;
    }
    for (let i = 1; i < chars.length; i += 1) {
        if (!USERNAME_REST_CHAR.test(chars[i])) {
            return false;
        }
    }
    return true;
};

// A pragmatic email shape: a non-empty local part with no whitespace or
// '@', a single '@', then a domain that contains at least one dot with
// non-empty labels. This rejects "notanemail" (no '@') and "test@" (no
// domain) while accepting ordinary addresses. The server applies the
// authoritative validation.
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/**
 * Returns true when the value looks like a valid email address. An empty
 * string is treated as invalid; callers decide whether an empty email is
 * acceptable (it is optional on both dialogs) before applying the rule.
 */
export const isValidEmail = (email: string): boolean => {
    return EMAIL_PATTERN.test(email);
};

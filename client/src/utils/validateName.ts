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
 * The maximum allowed length, in characters, for a name field after
 * leading and trailing whitespace has been trimmed.
 */
export const NAME_MAX_LENGTH = 255;

/**
 * The set of characters permitted in a name. Letters, digits, spaces,
 * periods, underscores, parentheses, and hyphens are allowed; everything
 * else (for example, `<`, `>`, `!`, `@`, `#`, `$`, `%`) is rejected.
 */
export const NAME_PATTERN = /^[A-Za-z0-9 ._()-]+$/;

/**
 * Human-readable validation messages used across the name-bearing dialogs.
 */
export const NAME_ERROR_REQUIRED = 'Name is required';
export const NAME_ERROR_INVALID_CHARS =
    'Name may only contain letters, numbers, spaces, and the characters . _ - ( )';
export const NAME_ERROR_TOO_LONG = `Name must be ${NAME_MAX_LENGTH} characters or fewer`;

/**
 * Validate a user-supplied name. The name is trimmed of leading and
 * trailing whitespace before evaluation.
 *
 * A name is valid when, after trimming, it is non-empty, contains only
 * the allowed characters, and is at most NAME_MAX_LENGTH characters long.
 *
 * @param name - The raw name value entered by the user.
 * @returns An error message when the name is invalid, or null when valid.
 */
export const validateName = (name: string): string | null => {
    const trimmed = name.trim();

    if (!trimmed) {
        return NAME_ERROR_REQUIRED;
    }

    if (trimmed.length > NAME_MAX_LENGTH) {
        return NAME_ERROR_TOO_LONG;
    }

    if (!NAME_PATTERN.test(trimmed)) {
        return NAME_ERROR_INVALID_CHARS;
    }

    return null;
};

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ReactNode } from 'react';
import {
    PASSWORD_MAX_LENGTH,
    PASSWORD_MIN_LENGTH,
    codePointLength,
    scorePasswordStrength,
    utf8ByteLength,
} from './passwordStrength';
import type { PasswordStrength } from './passwordStrength';

/**
 * Human-readable label aligned with each strength bucket. Exposed for
 * the meter caption in PasswordStrengthField.
 */
export const STRENGTH_LABELS: Record<PasswordStrength, string> = {
    0: 'Too short',
    1: 'Weak',
    2: 'Fair',
    3: 'Good',
    4: 'Strong',
};

/**
 * Default policy hint shown beneath the field when the parent does not
 * supply its own helper text.
 */
export const DEFAULT_POLICY_HELPER =
    'At least 12 characters. Avoid common passwords and reused passwords.';

/**
 * Maps a strength bucket to the MUI palette color used for the meter
 * and helper text.
 */
export function strengthColor(
    strength: PasswordStrength,
): 'error' | 'warning' | 'info' | 'success' {
    if (strength <= 1) {
        return 'error';
    }
    if (strength === 2) {
        return 'warning';
    }
    if (strength === 3) {
        return 'info';
    }
    return 'success';
}

/**
 * Bundle of derived values produced by `derivePasswordState`. The
 * caller only needs to compute the password string once; the helper
 * runs the policy heuristics and returns every flag the field needs to
 * render its feedback. Centralising the logic keeps the React
 * component focused on JSX.
 */
export interface PasswordFieldState {
    strength: PasswordStrength;
    charCount: number;
    byteLength: number;
    isEmpty: boolean;
    meetsMinimum: boolean;
    tooLong: boolean;
    tooShort: boolean;
    showFeedback: boolean;
    showStrengthMeter: boolean;
}

/**
 * Computes every derived flag the password field needs from the raw
 * input value. The output is plain data, so React.memo / useMemo can
 * cache it without referential identity worries.
 */
export function derivePasswordState(
    value: string,
    hideFeedbackWhenEmpty: boolean,
): PasswordFieldState {
    const charCount = codePointLength(value);
    const byteLength = utf8ByteLength(value);
    const isEmpty = charCount === 0;
    const meetsMinimum = charCount >= PASSWORD_MIN_LENGTH;
    const tooLong = byteLength > PASSWORD_MAX_LENGTH;
    const tooShort = !isEmpty && !meetsMinimum;
    const showFeedback = !(hideFeedbackWhenEmpty && isEmpty);
    const showStrengthMeter = !isEmpty && meetsMinimum && !tooLong;
    return {
        strength: scorePasswordStrength(value),
        charCount,
        byteLength,
        isEmpty,
        meetsMinimum,
        tooLong,
        tooShort,
        showFeedback,
        showStrengthMeter,
    };
}

/**
 * Inputs needed to compute the helper text shown beneath the password
 * field. Centralising the logic here keeps the component focused on
 * rendering and lowers its cyclomatic complexity.
 */
export interface FeedbackHelperInput {
    helperText: ReactNode;
    showFeedback: boolean;
    isEmpty: boolean;
    tooLong: boolean;
    tooShort: boolean;
    charCount: number;
    byteLength: number;
    strength: PasswordStrength;
}

/**
 * Returns the helper text to display under the password field. The
 * caller-supplied `helperText` wins when no live feedback should be
 * displayed (the field is hidden, empty, or feedback is suppressed).
 * When the field is in error, the byte-length error takes precedence
 * over the minimum-length error because exceeding the bcrypt 72-byte
 * limit is the more severe failure mode.
 */
export function buildFeedbackHelper(input: FeedbackHelperInput): ReactNode {
    const policyHelper = input.helperText ?? DEFAULT_POLICY_HELPER;
    if (!input.showFeedback || input.isEmpty) {
        return policyHelper;
    }
    if (input.tooLong) {
        return (
            'Password exceeds the 72-byte server limit '
            + `(currently ${input.byteLength} bytes).`
        );
    }
    if (input.tooShort) {
        return (
            `Password is ${input.charCount} of ${PASSWORD_MIN_LENGTH}`
            + ' minimum characters.'
        );
    }
    const lengthLine =
        `${input.charCount} / ${PASSWORD_MIN_LENGTH} characters`;
    return `${lengthLine} • Strength: ${STRENGTH_LABELS[input.strength]}`;
}

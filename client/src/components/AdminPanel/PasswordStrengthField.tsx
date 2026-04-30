/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo } from 'react';
import {
    Box,
    LinearProgress,
    TextField,
    Typography,
} from '@mui/material';
import type { TextFieldProps } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    PASSWORD_MAX_LENGTH,
    PASSWORD_MIN_LENGTH,
    codePointLength,
    scorePasswordStrength,
    utf8ByteLength,
} from './passwordStrength';
import type { PasswordStrength } from './passwordStrength';

/**
 * Human-readable label aligned with each strength bucket.
 */
const STRENGTH_LABELS: Record<PasswordStrength, string> = {
    0: 'Too short',
    1: 'Weak',
    2: 'Fair',
    3: 'Good',
    4: 'Strong',
};

/**
 * Maps a strength bucket to the MUI palette color used for the meter
 * and helper text.
 */
const strengthColor = (
    strength: PasswordStrength,
): 'error' | 'warning' | 'info' | 'success' => {
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
};

/**
 * Props that the parent component may forward to the underlying
 * TextField. The `onChange` and `value` props are required because the
 * field is always controlled.
 */
export type PasswordStrengthFieldProps = Omit<
    TextFieldProps,
    'type' | 'onChange' | 'value'
> & {
    value: string;
    onChange: (value: string) => void;
    /**
     * Optional callback invoked whenever the input or its derived
     * validity changes. The parent uses this to gate Submit on the
     * 12-character minimum without re-implementing the policy.
     */
    onValidityChange?: (info: {
        meetsMinimum: boolean;
        isEmpty: boolean;
        strength: PasswordStrength;
        tooLong: boolean;
        byteLength: number;
    }) => void;
    /**
     * When true, the policy hint and strength meter are hidden until
     * the user enters at least one character. Used for the optional
     * password field in the Edit User dialog.
     */
    hideFeedbackWhenEmpty?: boolean;
};

/**
 * Reusable password input that renders an MUI TextField with live
 * feedback aligned with NIST SP 800-63B guidance. The field never blocks
 * submission on its own; the parent decides based on
 * `onValidityChange`. Server-side validation remains authoritative.
 */
const PasswordStrengthField: React.FC<PasswordStrengthFieldProps> = ({
    value,
    onChange,
    onValidityChange,
    hideFeedbackWhenEmpty = false,
    helperText,
    inputProps,
    sx,
    ...rest
}) => {
    const theme = useTheme();
    const strength = useMemo(
        () => scorePasswordStrength(value),
        [value],
    );
    // Count code points (matches Go's utf8.RuneCountInString) rather
    // than UTF-16 code units so that emoji and other supplementary
    // characters agree with the server's minimum-length check.
    const charCount = useMemo(() => codePointLength(value), [value]);
    // Count UTF-8 bytes (matches Go's len(password)) so the client can
    // flag values bcrypt would reject.
    const byteLength = useMemo(() => utf8ByteLength(value), [value]);
    const isEmpty = charCount === 0;
    const meetsMinimum = charCount >= PASSWORD_MIN_LENGTH;
    const tooLong = byteLength > PASSWORD_MAX_LENGTH;
    const showFeedback = !(hideFeedbackWhenEmpty && isEmpty);
    const showStrengthMeter = !isEmpty && meetsMinimum && !tooLong;
    const tooShort = !isEmpty && !meetsMinimum;

    React.useEffect(() => {
        onValidityChange?.({
            meetsMinimum,
            isEmpty,
            strength,
            tooLong,
            byteLength,
        });
    }, [
        meetsMinimum,
        isEmpty,
        strength,
        tooLong,
        byteLength,
        onValidityChange,
    ]);

    // Map the 0-4 score to a 0-100 progress value for the meter.
    const progressValue = Math.max(0, strength) * 25;
    const colorKey = strengthColor(strength);
    const meterColor = theme.palette[colorKey].main;

    const lengthLine = isEmpty
        ? null
        : `${charCount} / ${PASSWORD_MIN_LENGTH} characters`;

    const policyHelper = helperText
        ?? 'At least 12 characters. Avoid common passwords and reused passwords.';

    let feedbackHelper: React.ReactNode = policyHelper;
    if (showFeedback && !isEmpty) {
        if (tooLong) {
            // Byte-length error takes precedence over the
            // minimum-length helper because exceeding the bcrypt
            // 72-byte limit is the more severe failure mode.
            feedbackHelper = 'Password exceeds the 72-byte server'
                + ` limit (currently ${byteLength} bytes).`;
        } else if (tooShort) {
            feedbackHelper = `Password is ${charCount} of ${PASSWORD_MIN_LENGTH}`
                + ' minimum characters.';
        } else {
            feedbackHelper = `${lengthLine} • Strength: `
                + `${STRENGTH_LABELS[strength]}`;
        }
    }

    return (
        <Box>
            <TextField
                autoComplete="new-password"
                {...rest}
                type="password"
                value={value}
                onChange={(event) => { onChange(event.target.value); }}
                error={tooShort || tooLong || rest.error}
                helperText={showFeedback ? feedbackHelper : undefined}
                // Caller-supplied inputProps are spread first so that the
                // security-critical maxLength below cannot be overridden.
                inputProps={{
                    ...(inputProps || {}),
                    maxLength: PASSWORD_MAX_LENGTH,
                }}
                sx={sx}
            />
            {showFeedback && showStrengthMeter && (
                <Box sx={{ mt: 0.5, px: 1.75 }}>
                    <LinearProgress
                        variant="determinate"
                        value={progressValue}
                        aria-label="Password strength"
                        aria-valuenow={progressValue}
                        sx={{
                            height: 6,
                            borderRadius: 3,
                            backgroundColor: theme.palette.action.hover,
                            '& .MuiLinearProgress-bar': {
                                backgroundColor: meterColor,
                            },
                        }}
                    />
                    <Typography
                        variant="caption"
                        sx={{ color: meterColor, mt: 0.25, display: 'block' }}
                    >
                        {STRENGTH_LABELS[strength]}
                    </Typography>
                </Box>
            )}
        </Box>
    );
};

export default PasswordStrengthField;

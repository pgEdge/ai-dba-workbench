/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useEffect, useMemo } from 'react';
import type { JSX } from 'react';
import { Box, LinearProgress, TextField, Typography } from '@mui/material';
import type { TextFieldProps } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { PASSWORD_MAX_LENGTH } from './passwordStrength';
import type { PasswordStrength } from './passwordStrength';
import {
    STRENGTH_LABELS,
    buildFeedbackHelper,
    derivePasswordState,
    strengthColor,
} from './passwordStrengthHelpers';

interface StrengthMeterProps {
    strength: PasswordStrength;
    color: string;
    progressValue: number;
}

/**
 * Visual strength bar with the bucket label underneath. Rendered only
 * when the parent decides feedback should be visible.
 */
function StrengthMeter(props: StrengthMeterProps): JSX.Element {
    const { strength, color, progressValue } = props;
    const theme = useTheme();
    return (
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
                        backgroundColor: color,
                    },
                }}
            />
            <Typography
                variant="caption"
                sx={{ color, mt: 0.25, display: 'block' }}
            >
                {STRENGTH_LABELS[strength]}
            </Typography>
        </Box>
    );
}

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
    onChange: (_value: string) => void;
    /**
     * Optional callback invoked whenever the input or its derived
     * validity changes. The parent uses this to gate Submit on the
     * 12-character minimum without re-implementing the policy.
     */
    onValidityChange?: (_info: {
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
function PasswordStrengthField(
    props: PasswordStrengthFieldProps,
): JSX.Element {
    const {
        value,
        onChange,
        onValidityChange,
        hideFeedbackWhenEmpty = false,
        helperText,
        inputProps,
        sx,
        ...rest
    } = props;
    const theme = useTheme();
    // `derivePasswordState` runs the rune count, byte count, scorer,
    // and policy flags in one pass; centralising them keeps the
    // component's cyclomatic complexity low.
    const state = useMemo(
        () => derivePasswordState(value, hideFeedbackWhenEmpty),
        [value, hideFeedbackWhenEmpty],
    );
    const {
        strength,
        charCount,
        byteLength,
        isEmpty,
        meetsMinimum,
        tooLong,
        tooShort,
        showFeedback,
        showStrengthMeter,
    } = state;

    useEffect(() => {
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
    const meterColor = theme.palette[strengthColor(strength)].main;
    const feedbackHelper = buildFeedbackHelper({
        helperText,
        showFeedback,
        isEmpty,
        tooLong,
        tooShort,
        charCount,
        byteLength,
        strength,
    });

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
                <StrengthMeter
                    strength={strength}
                    color={meterColor}
                    progressValue={progressValue}
                />
            )}
        </Box>
    );
}

export default PasswordStrengthField;

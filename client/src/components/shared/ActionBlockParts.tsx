/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - presentational pieces of ActionBlock: the
 * frame, the review heading, the statements, the run's outcome, any
 * returned rows and the buttons that act on the action.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import { useTheme } from '@mui/material/styles';
import type { Theme } from '@mui/material/styles';
import CloseIcon from '@mui/icons-material/Close';
import ErrorIcon from '@mui/icons-material/Error';
import { dangerColour } from '../../types/actions';
import type { Action, ActionStatementResult } from '../../types/actions';
import {
    sxMonoFont,
    getTableSx,
    getQueryResultWrapperSx,
    getQueryErrorSx,
} from './markdownStyles';
import type { ActionFlags } from './actionBlockState';
import {
    dangerLabelOf,
    pluralise,
    reviewLabelOf,
    rowsAffectedOf,
} from './actionBlockState';

const monoSx = { ...sxMonoFont, wordBreak: 'break-word' as const };

type StatusTone = 'success' | 'error' | 'warning';

/**
 * Text in a status colour. The palette's `main` shades fall short of
 * 4.5:1 on the light surface (success 2.28:1, warning 2.15:1, error
 * 3.76:1), so status text takes the per-mode `chipText` shade instead;
 * see `.claude/react-expert/color-contrast-guidelines.md`.
 */
const statusTextColour = (tone: StatusTone) =>
    (theme: Theme): string => theme.palette.custom.chipText[tone];

/**
 * Execute sits on the warning fill, which white text cannot read on
 * (2.15:1), so its label is the darkest grey in both modes: 8.26:1 on
 * the fill and 5.57:1 on the hover shade in light mode.
 */
const executeButtonSx = {
    textTransform: 'none',
    color: (theme: Theme) => theme.palette.grey[900],
    '&:hover': { color: (theme: Theme) => theme.palette.grey[900] },
} as const;

/** The outlined error button, with its label and edge in the text shade. */
const overrideButtonSx = {
    textTransform: 'none',
    color: statusTextColour('error'),
    borderColor: statusTextColour('error'),
} as const;

export const ActionFrame: React.FC<{ children: React.ReactNode }> = ({
    children,
}) => {
    const theme = useTheme();
    return (
        <Box data-testid="action-block" sx={getQueryResultWrapperSx(theme)}>
            {children}
        </Box>
    );
};

export const ActionErrorRow: React.FC<{
    children: React.ReactNode;
    /** When given, a Dismiss control closes the block. */
    onDismiss?: () => void;
}> = ({ children, onDismiss }) => {
    const theme = useTheme();
    return (
        <Box sx={getQueryErrorSx(theme)} role="alert">
            <ErrorIcon sx={{ fontSize: 16, color: 'error.main', mt: 0.25 }} />
            <Typography variant="body2" sx={{ flex: 1, ...monoSx }}>
                {children}
            </Typography>
            {onDismiss && (
                <IconButton size="small" aria-label="Dismiss" onClick={onDismiss}>
                    <CloseIcon sx={{ fontSize: 14 }} />
                </IconButton>
            )}
        </Box>
    );
};

export const ActionLoading: React.FC<{ label: string }> = ({ label }) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 1.5 }}>
        <CircularProgress size={14} aria-label="Loading action" />
        <Typography variant="body2">{label}</Typography>
    </Box>
);

interface ActionHeaderProps {
    current: Action;
    flags: ActionFlags;
    onDismiss?: () => void;
}

export const ActionHeader: React.FC<ActionHeaderProps> = ({
    current,
    flags,
    onDismiss,
}) => {
    const { reviewing, vetoed, finished } = flags;
    const danger = current.review?.danger;
    return (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {reviewing && (
                <CircularProgress size={14} aria-label="Supervisor reviewing" />
            )}
            <Typography
                variant="subtitle2"
                sx={{
                    color: vetoed ? statusTextColour('error') : 'text.primary',
                    flex: 1,
                }}
            >
                {reviewLabelOf(current)}
            </Typography>
            {!reviewing && danger && (
                <Chip
                    size="small"
                    label={dangerLabelOf(danger)}
                    color={dangerColour(danger)}
                />
            )}
            {onDismiss && (finished || vetoed) && (
                <IconButton size="small" aria-label="Dismiss" onClick={onDismiss}>
                    <CloseIcon sx={{ fontSize: 14 }} />
                </IconButton>
            )}
        </Box>
    );
};

/** The supervisor's reasons for a veto; nothing when it gave none. */
export const VetoReasons: React.FC<{ reasons?: string[] }> = ({ reasons }) => {
    if (!reasons || reasons.length === 0) {
        return null;
    }
    return (
        <Box component="ul" sx={{ m: 0, pl: 3 }}>
            {reasons.map((reason, idx) => (
                <li key={idx}>
                    <Typography variant="body2">{reason}</Typography>
                </li>
            ))}
        </Box>
    );
};

export const StatementList: React.FC<{ statements: string[] }> = ({
    statements,
}) => (
    <Box sx={{ m: 0 }}>
        {statements.map((stmt, idx) => (
            <Typography
                key={idx}
                variant="body2"
                sx={{ ...monoSx, whiteSpace: 'pre-wrap' }}
            >
                {stmt}
            </Typography>
        ))}
    </Box>
);

const OutcomeHeading: React.FC<{
    tone: StatusTone;
    label: string;
    children?: React.ReactNode;
}> = ({ tone, label, children }) => (
    <Box>
        <Typography variant="subtitle2" sx={{ color: statusTextColour(tone) }}>
            {label}
        </Typography>
        {children}
    </Box>
);

interface ExecStatusProps {
    current: Action;
    onCancel: () => void;
}

/** The run's progress or outcome; nothing before it has started. */
export const ExecStatus: React.FC<ExecStatusProps> = ({ current, onCancel }) => {
    switch (current.exec_status) {
        case 'executing':
            return (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <CircularProgress size={14} aria-label="Running" />
                    <Typography variant="body2" sx={{ flex: 1 }}>
                        Running…
                    </Typography>
                    <Button
                        size="small"
                        onClick={onCancel}
                        sx={{ textTransform: 'none' }}
                    >
                        Cancel
                    </Button>
                </Box>
            );
        case 'succeeded':
            return (
                <OutcomeHeading tone="success" label="Succeeded">
                    <Typography variant="body2">
                        {pluralise(rowsAffectedOf(current), 'row')} affected
                    </Typography>
                </OutcomeHeading>
            );
        case 'failed':
            return <FailedOutcome current={current} />;
        case 'unknown':
            return (
                <Typography
                    variant="subtitle2"
                    sx={{ color: statusTextColour('warning') }}
                >
                    Outcome unknown: check the database
                </Typography>
            );
        case 'cancelled':
            return <Typography variant="subtitle2">Cancelled</Typography>;
        default:
            return null;
    }
};

const FailedOutcome: React.FC<{ current: Action }> = ({ current }) => {
    const firstError = (current.results ?? []).find((r) => r.error)?.error;
    return (
        <OutcomeHeading tone="error" label="Failed">
            {firstError && (
                <Typography variant="body2" sx={monoSx}>
                    {firstError}
                </Typography>
            )}
        </OutcomeHeading>
    );
};

const ResultTable: React.FC<{ columns: string[]; rows: string[][] }> = ({
    columns,
    rows,
}) => {
    const theme = useTheme();
    return (
        <Box component="table" sx={getTableSx(theme)}>
            <thead>
                <tr>
                    {columns.map((col, i) => (
                        <th key={i}>
                            <Typography variant="body2" sx={{ ...sxMonoFont, fontWeight: 600 }}>
                                {col}
                            </Typography>
                        </th>
                    ))}
                </tr>
            </thead>
            <tbody>
                {rows.map((row, ri) => (
                    <tr key={ri}>
                        {row.map((cell, ci) => (
                            <td key={ci}>
                                <Typography variant="body2" sx={sxMonoFont}>
                                    {cell}
                                </Typography>
                            </td>
                        ))}
                    </tr>
                ))}
            </tbody>
        </Box>
    );
};

/** A server caveat about one statement, shown as a warning line. */
const StatementNote: React.FC<{ note: string }> = ({ note }) => (
    <Typography
        variant="body2"
        role="note"
        sx={{ color: statusTextColour('warning'), wordBreak: 'break-word' }}
    >
        {note}
    </Typography>
);

const StatementRows: React.FC<{ result: ActionStatementResult }> = ({
    result,
}) =>
    result.columns && result.rows ? (
        <Box sx={{ overflowX: 'auto' }}>
            <ResultTable columns={result.columns} rows={result.rows} />
            {result.truncated && (
                <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                    Results limited to {pluralise(result.rows.length, 'row')}
                </Typography>
            )}
        </Box>
    ) : null;

/** The rows and notes any statement returned, in statement order. */
export const ActionResults: React.FC<{
    results?: ActionStatementResult[] | null;
}> = ({ results }) => (
    <>
        {(results ?? []).map((result, idx) => (
            <Box key={idx}>
                <StatementRows result={result} />
                {result.note && <StatementNote note={result.note} />}
            </Box>
        ))}
    </>
);

interface ActionButtonsProps {
    flags: ActionFlags;
    busy: boolean;
    onExecute: () => void;
    onOpenOverride: () => void;
}

/** Controls for an action that has not started. */
export const ActionButtons: React.FC<ActionButtonsProps> = ({
    flags,
    busy,
    onExecute,
    onOpenOverride,
}) => {
    const { canExecute, canRetry, canRetryOverride, canOverride } = flags;
    return (
        <Box sx={{ display: 'flex', gap: 1 }}>
            {canExecute && (
                <Button
                    size="small"
                    variant="contained"
                    color="warning"
                    disabled={busy}
                    onClick={onExecute}
                    sx={executeButtonSx}
                >
                    Execute
                </Button>
            )}
            {(canRetry || canRetryOverride) && (
                <Button
                    size="small"
                    variant="outlined"
                    disabled={busy}
                    onClick={canRetryOverride ? onOpenOverride : onExecute}
                    sx={{ textTransform: 'none' }}
                >
                    Retry
                </Button>
            )}
            {canOverride && (
                <Button
                    size="small"
                    variant="outlined"
                    color="error"
                    disabled={busy}
                    onClick={onOpenOverride}
                    sx={overrideButtonSx}
                >
                    Override and run
                </Button>
            )}
        </Box>
    );
};

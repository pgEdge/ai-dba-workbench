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
import { neutralButtonSx } from './actionButtonSx';
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
function statusTextColour(tone: StatusTone) {
    return function textShade(theme: Theme): string {
        return theme.palette.custom.chipText[tone];
    };
}

function darkestGrey(theme: Theme): string {
    return theme.palette.grey[900];
}

/**
 * Execute sits on the warning fill, which white text cannot read on
 * (2.15:1), so its label is the darkest grey in both modes: 8.26:1 on
 * the fill and 5.57:1 on the hover shade in light mode.
 */
const executeButtonSx = {
    textTransform: 'none',
    color: darkestGrey,
    '&:hover': { color: darkestGrey },
} as const;

/** The outlined error button, with its label and edge in the text shade. */
const overrideButtonSx = {
    textTransform: 'none',
    color: statusTextColour('error'),
    borderColor: statusTextColour('error'),
} as const;

interface ActionFrameProps {
    children: React.ReactNode;
}

export const ActionFrame: React.FC<ActionFrameProps> = ({
    children,
}) => {
    const theme = useTheme();
    return (
        <Box data-testid="action-block" sx={getQueryResultWrapperSx(theme)}>
            {children}
        </Box>
    );
};

interface ActionErrorRowProps {
    children: React.ReactNode;
    /** When given, a Dismiss control closes the block. */
    onDismiss?: () => void;
}

export const ActionErrorRow: React.FC<ActionErrorRowProps> = ({ children, onDismiss }) => {
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

interface ActionLoadingProps {
    label: string;
}

export const ActionLoading: React.FC<ActionLoadingProps> = ({ label }) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 1.5 }}>
        <CircularProgress aria-label="Loading action" size={14} />
        <Typography variant="body2">{label}</Typography>
    </Box>
);

interface ActionHeaderProps {
    current: Action;
    flags: ActionFlags;
    onDismiss?: () => void;
}

interface DangerChipProps {
    danger?: string;
}

const DangerChip: React.FC<DangerChipProps> = ({ danger }) =>
    danger ? (
        <Chip
            size="small"
            label={dangerLabelOf(danger)}
            color={dangerColour(danger)}
        />
    ) : null;

interface DismissButtonProps {
    onDismiss?: () => void;
}

const DismissButton: React.FC<DismissButtonProps> = ({ onDismiss }) =>
    onDismiss ? (
        <IconButton size="small" aria-label="Dismiss" onClick={onDismiss}>
            <CloseIcon sx={{ fontSize: 14 }} />
        </IconButton>
    ) : null;

export const ActionHeader: React.FC<ActionHeaderProps> = ({
    current,
    flags,
    onDismiss,
}) => {
    const headingSx = {
        color: flags.vetoed ? statusTextColour('error') : 'text.primary',
        flex: 1,
    };
    return (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {flags.reviewing && (
                <CircularProgress aria-label="Supervisor reviewing" size={14} />
            )}
            <Typography variant="subtitle2" sx={headingSx}>
                {reviewLabelOf(current)}
            </Typography>
            {!flags.reviewing && <DangerChip danger={current.review?.danger} />}
            {flags.dismissible && <DismissButton onDismiss={onDismiss} />}
        </Box>
    );
};

/** The supervisor's reasons for a veto; nothing when it gave none. */
interface VetoReasonsProps {
    reasons?: string[];
}

export const VetoReasons: React.FC<VetoReasonsProps> = ({ reasons }) => {
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

interface StatementListProps {
    statements: string[];
}

export const StatementList: React.FC<StatementListProps> = ({
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

interface OutcomeHeadingProps {
    tone: StatusTone;
    label: string;
    children?: React.ReactNode;
}

const OutcomeHeading: React.FC<OutcomeHeadingProps> = ({ tone, label, children }) => (
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
                    <CircularProgress aria-label="Running" size={14} />
                    <Typography variant="body2" sx={{ flex: 1 }}>
                        Running…
                    </Typography>
                    <Button
                        size="small"
                        onClick={onCancel}
                        sx={neutralButtonSx}
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

interface FailedOutcomeProps {
    current: Action;
}

const FailedOutcome: React.FC<FailedOutcomeProps> = ({ current }) => {
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

interface ResultTableProps {
    columns: string[];
    rows: string[][];
}

const ResultTable: React.FC<ResultTableProps> = ({
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
interface StatementNoteProps {
    note: string;
}

const StatementNote: React.FC<StatementNoteProps> = ({ note }) => (
    <Typography
        variant="body2"
        role="note"
        sx={{ color: statusTextColour('warning'), wordBreak: 'break-word' }}
    >
        {note}
    </Typography>
);

interface StatementRowsProps {
    result: ActionStatementResult;
}

const StatementRows: React.FC<StatementRowsProps> = ({
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
interface ActionResultsProps {
    results?: ActionStatementResult[] | null;
}

export const ActionResults: React.FC<ActionResultsProps> = ({ results }) => (
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
                    sx={neutralButtonSx}
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

/**
 * Shown in place of the controls once the server has refused to run the
 * action for lack of write access, so the user is not left retrying.
 */
export const WriteDeniedNote: React.FC = () => (
    <Typography variant="body2" color="text.secondary">
        You can review this action, but running it needs write access to
        this connection. Ask an administrator to grant it, or have someone
        with access run it.
    </Typography>
);

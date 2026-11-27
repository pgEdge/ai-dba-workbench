/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - presentational parts of RunnableCodeBlock:
 * the Run button, notices, the write confirmation prompt and the query
 * results.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import CircularProgress from '@mui/material/CircularProgress';
import Tooltip from '@mui/material/Tooltip';
import { alpha } from '@mui/material/styles';
import type { Theme } from '@mui/material/styles';
import CloseIcon from '@mui/icons-material/Close';
import ErrorIcon from '@mui/icons-material/Error';
import WarningIcon from '@mui/icons-material/Warning';
import PlayArrowIcon from '@mui/icons-material/PlayArrow';
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import type { QueryResponse, StatementResult } from './useRunnableQuery';
import {
    sxMonoFont,
    sxConfirmationActions,
    getTableSx,
    getCopyButtonSx,
    getQueryResultWrapperSx,
    getQueryResultHeaderSx,
    getQueryErrorSx,
    getConfirmationPanelSx,
    getConfirmationTitleSx,
    getConfirmationTextSx,
    getConfirmationStatementSx,
} from './markdownStyles';

const errorTextColour = (theme: Theme): string =>
    theme.palette.mode === 'dark'
        ? theme.palette.error.light
        : theme.palette.error.dark;

const noticeIconSx = { fontSize: 16, mt: 0.25, flexShrink: 0 };

const footnoteSx = {
    fontSize: '0.875rem',
    color: 'text.disabled',
    px: 1.5,
    py: 0.5,
};

// ---------------------------------------------------------------------------
// Run button
// ---------------------------------------------------------------------------

interface RunButtonProps {
    theme: Theme;
    /** Stable accessible label naming the action. */
    targetLabel: string;
    tooltipTitle: string;
    disabled: boolean;
    loading: boolean;
    validating: boolean;
    onRun: () => void;
}

export const RunButton: React.FC<RunButtonProps> = ({
    theme,
    targetLabel,
    tooltipTitle,
    disabled,
    loading,
    validating,
    onRun,
}) => (
    <Tooltip title={tooltipTitle} placement="left">
        {/* The label stays stable across validation states so assistive
            technology announces the action, not its transient status. */}
        <span aria-label={targetLabel}>
            <IconButton
                size="small"
                onClick={onRun}
                disabled={disabled}
                sx={getCopyButtonSx(theme)}
            >
                {(loading || validating) ? (
                    <CircularProgress
                        aria-label={
                            validating ? 'Validating query' : 'Running query'
                        }
                        size={14}
                        sx={{ color: alpha(theme.palette.secondary.main, 0.7) }}
                    />
                ) : (
                    <PlayArrowIcon sx={{ fontSize: 18 }} />
                )}
            </IconButton>
        </span>
    </Tooltip>
);

// ---------------------------------------------------------------------------
// Notices
// ---------------------------------------------------------------------------

interface InfoNoticeProps {
    theme: Theme;
    testId: string;
    children: React.ReactNode;
}

/** An informational notice rendered beneath the code block. */
export const InfoNotice: React.FC<InfoNoticeProps> = ({
    theme,
    testId,
    children,
}) => (
    <Box data-testid={testId} sx={getQueryResultWrapperSx(theme)}>
        <Box sx={getQueryErrorSx(theme)}>
            <InfoOutlinedIcon
                sx={{ ...noticeIconSx, color: theme.palette.info.main }}
            />
            <Typography
                sx={{ fontSize: '0.875rem', color: 'text.secondary', flex: 1 }}
            >
                {children}
            </Typography>
        </Box>
    </Box>
);

interface ValidationFailedNoticeProps {
    theme: Theme;
    error: string;
    /** Whether the reviewer has already chosen to run it anyway. */
    runAnyway: boolean;
    onRunAnyway: () => void;
}

/** Validation failure: Run is disabled, with an escape hatch. */
export const ValidationFailedNotice: React.FC<ValidationFailedNoticeProps> = ({
    theme,
    error,
    runAnyway,
    onRunAnyway,
}) => (
    <Box
        data-testid="sql-validation-error"
        sx={getQueryResultWrapperSx(theme)}
    >
        <Box sx={getQueryErrorSx(theme)}>
            <WarningIcon
                sx={{ ...noticeIconSx, color: theme.palette.warning.main }}
            />
            <Box sx={{ flex: 1 }}>
                <Typography
                    sx={{
                        fontSize: '0.875rem',
                        color: errorTextColour(theme),
                        ...sxMonoFont,
                        wordBreak: 'break-word',
                    }}
                >
                    This query did not validate: {error}
                </Typography>
                {!runAnyway && (
                    <Button
                        size="small"
                        onClick={onRunAnyway}
                        sx={{ textTransform: 'none', mt: 0.5 }}
                    >
                        Run anyway
                    </Button>
                )}
            </Box>
        </Box>
    </Box>
);

// ---------------------------------------------------------------------------
// Write confirmation
// ---------------------------------------------------------------------------

interface WriteConfirmationProps {
    theme: Theme;
    targetName: string;
    writeStatements: string[];
    onCancel: () => void;
    onConfirm: () => void;
}

export const WriteConfirmation: React.FC<WriteConfirmationProps> = ({
    theme,
    targetName,
    writeStatements,
    onCancel,
    onConfirm,
}) => (
    <Box sx={getQueryResultWrapperSx(theme)}>
        <Box sx={getConfirmationPanelSx(theme)}>
            <Box sx={getConfirmationTitleSx(theme)}>
                <WarningIcon sx={{
                    fontSize: 18,
                    color: theme.palette.warning.main,
                    mt: 0.125,
                    flexShrink: 0,
                }} />
                <Typography sx={getConfirmationTextSx(theme)}>
                    {targetName
                        ? `This code block contains statements that modify the database on ${targetName}:`
                        : 'This code block contains statements that modify the database:'}
                </Typography>
            </Box>
            {writeStatements.map((stmt, idx) => (
                <Typography key={idx} sx={getConfirmationStatementSx(theme)}>
                    {'•'} {stmt}
                </Typography>
            ))}
            <Box sx={sxConfirmationActions}>
                <Button
                    size="small"
                    onClick={onCancel}
                    sx={{ textTransform: 'none' }}
                >
                    Cancel
                </Button>
                <Button
                    size="small"
                    variant="contained"
                    color="warning"
                    onClick={onConfirm}
                    sx={{ textTransform: 'none' }}
                >
                    Execute
                </Button>
            </Box>
        </Box>
    </Box>
);

// ---------------------------------------------------------------------------
// Query results
// ---------------------------------------------------------------------------

interface ErrorLineProps {
    theme: Theme;
    message: string;
    onDismiss?: () => void;
}

/** An error message with an optional dismiss control. */
export const ErrorLine: React.FC<ErrorLineProps> = ({
    theme,
    message,
    onDismiss,
}) => (
    <Box sx={getQueryErrorSx(theme)}>
        <ErrorIcon sx={{ ...noticeIconSx, color: theme.palette.error.main }} />
        <Typography sx={{
            fontSize: '0.875rem',
            color: errorTextColour(theme),
            flex: 1,
            ...sxMonoFont,
            wordBreak: 'break-word',
        }}>
            {message}
        </Typography>
        {onDismiss && (
            <IconButton
                size="small"
                onClick={onDismiss}
                sx={{ p: 0.25, color: 'text.secondary', flexShrink: 0 }}
            >
                <CloseIcon sx={{ fontSize: 14 }} />
            </IconButton>
        )}
    </Box>
);

const cellTextSx = {
    fontSize: '0.875rem',
    ...sxMonoFont,
    whiteSpace: 'nowrap',
};

interface ResultTableProps {
    theme: Theme;
    columns: string[];
    rows: string[][];
}

const ResultTable: React.FC<ResultTableProps> = ({ theme, columns, rows }) => (
    <Box sx={{ overflowX: 'auto' }}>
        <Box component="table" sx={getTableSx(theme)}>
            <thead>
                <tr>
                    {columns.map((col, i) => (
                        <th key={i}>
                            <Typography sx={{ ...cellTextSx, fontWeight: 600 }}>
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
                                <Typography sx={cellTextSx}>{cell}</Typography>
                            </td>
                        ))}
                    </tr>
                ))}
            </tbody>
        </Box>
    </Box>
);

/** The row-count footnote beneath a statement's results. */
interface RowCountProps {
    result: StatementResult;
}

const RowCount: React.FC<RowCountProps> = ({ result }) => {
    if (result.truncated) {
        return (
            <Typography sx={footnoteSx}>
                Results limited to {result.row_count} rows
            </Typography>
        );
    }
    if (result.row_count === undefined) {
        return null;
    }
    return (
        <Typography sx={footnoteSx}>
            {result.row_count} row{result.row_count !== 1 ? 's' : ''}
        </Typography>
    );
};

interface StatementResultViewProps {
    theme: Theme;
    result: StatementResult;
    first: boolean;
}

const StatementResultView: React.FC<StatementResultViewProps> = ({
    theme,
    result,
    first,
}) => (
    <Box>
        {/* Query label */}
        <Typography sx={{
            ...footnoteSx,
            ...sxMonoFont,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            borderTop: first ? 'none' : '1px solid',
            borderColor: theme.palette.mode === 'dark'
                ? theme.palette.grey[700]
                : theme.palette.grey[200],
        }}>
            {result.query}
        </Typography>
        {result.error && <ErrorLine theme={theme} message={result.error} />}
        {result.columns && result.rows && (
            <>
                <ResultTable
                    theme={theme}
                    columns={result.columns}
                    rows={result.rows}
                />
                <RowCount result={result} />
            </>
        )}
    </Box>
);

interface QueryResultsProps {
    theme: Theme;
    response: QueryResponse & { results: StatementResult[] };
    onDismiss: () => void;
}

/** The results of every statement in a run. */
export const QueryResults: React.FC<QueryResultsProps> = ({
    theme,
    response,
    onDismiss,
}) => (
    <Box sx={getQueryResultWrapperSx(theme)}>
        <Box sx={getQueryResultHeaderSx(theme)}>
            <Typography sx={{
                fontSize: '0.875rem',
                color: 'text.secondary',
                ...sxMonoFont,
            }}>
                {response.total_statements} statement{response.total_statements !== 1 ? 's' : ''}
            </Typography>
            <IconButton
                size="small"
                onClick={onDismiss}
                sx={{ p: 0.25, color: 'text.secondary' }}
            >
                <CloseIcon sx={{ fontSize: 14 }} />
            </IconButton>
        </Box>
        {response.results.map((result, idx) => (
            <StatementResultView
                key={idx}
                theme={theme}
                result={result}
                first={idx === 0}
            />
        ))}
    </Box>
);

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - RunnableCodeBlock component. A code block
 * wrapper that adds a Run button for SQL and displays query results
 * inline beneath the code.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useCallback, useEffect, useMemo } from 'react';
import {
    Box,
    Typography,
    Button,
    IconButton,
    alpha,
    CircularProgress,
    Tooltip,
} from '@mui/material';
import type { Theme } from '@mui/material/styles';
import {
    Close as CloseIcon,
    Error as ErrorIcon,
    Warning as WarningIcon,
    PlayArrow as PlayArrowIcon,
    InfoOutlined as InfoOutlinedIcon,
} from '@mui/icons-material';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { apiFetch } from '../../utils/apiClient';
import { useSqlValidation } from '../../hooks/useSqlValidation';
import { extractExecutableSQL, hasSqlParameters } from './sqlDetection';
import CopyCodeButton from './CopyCodeButton';
import {
    sxMonoFont,
    sxConfirmationActions,
    getCodeBlockWrapperSx,
    getCodeBlockCustomStyle,
    getTableSx,
    getCodeBlockButtonGroupSx,
    getCopyButtonSx,
    getQueryResultWrapperSx,
    getQueryResultHeaderSx,
    getQueryErrorSx,
    getConfirmationPanelSx,
    getConfirmationTitleSx,
    getConfirmationTextSx,
    getConfirmationStatementSx,
} from './markdownStyles';

// ---------------------------------------------------------------------------
// Query result types
// ---------------------------------------------------------------------------

export interface StatementResult {
    columns?: string[];
    rows?: string[][];
    row_count?: number;
    truncated?: boolean;
    query: string;
    error?: string;
}

export interface QueryResponse {
    results?: StatementResult[];
    total_statements?: number;
    requires_confirmation?: boolean;
    write_statements?: string[];
    confirmation_message?: string;
}

export interface QueryState {
    loading: boolean;
    response: QueryResponse | null;
    error: string | null;
    pendingConfirmation: boolean;
    writeStatements: string[];
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

/** Label shown for a block that carries `$N` parameter placeholders. */
export const SQL_TEMPLATE_MESSAGE =
    'This is a query template: it uses $1-style parameter placeholders, '
    + 'so it needs parameter values substituted in before it can be run.';

/** Hint shown when PostgreSQL cannot plan the statement for validation. */
export const SQL_UNVALIDATED_MESSAGE =
    'Not validated: PostgreSQL cannot plan this kind of statement in '
    + 'advance. Review it before running it.';

export interface RunnableCodeBlockProps {
    codeContent: string;
    language: string;
    isDark: boolean;
    connectionId: number;
    databaseName?: string;
    serverName?: string;
    syntaxTheme: Record<string, unknown>;
    customBackground: string;
    theme: Theme;
    isSql: boolean;
    props: Record<string, unknown>;
}

const RunnableCodeBlock: React.FC<RunnableCodeBlockProps> = ({
    codeContent,
    language,
    isDark: _isDark,
    connectionId,
    databaseName,
    serverName,
    syntaxTheme,
    customBackground,
    theme,
    isSql,
    props,
}) => {
    const [queryState, setQueryState] = useState<QueryState>({
        loading: false,
        response: null,
        error: null,
        pendingConfirmation: false,
        writeStatements: [],
    });

    // Store the cleaned SQL so the confirmation "Execute" button can
    // re-send the same payload with confirmed=true.
    const [cleanedSQL, setCleanedSQL] = useState<string>('');

    // The SQL that would actually be sent to the server, which is also
    // what gets validated.
    const executableSQL = useMemo(
        () => extractExecutableSQL(codeContent),
        [codeContent],
    );

    // A block with `$N` placeholders is a template rather than a query:
    // it cannot be run without parameter values, so it gets no Run
    // button and is never sent for validation.
    const isTemplate = isSql && hasSqlParameters(codeContent);

    const validation = useSqlValidation({
        sql: executableSQL,
        connectionId,
        databaseName,
        enabled: isSql && !isTemplate,
    });

    // Escape hatch: a reviewer who disagrees with the validation result
    // can still run the query. Reset whenever the target changes.
    const [runAnyway, setRunAnyway] = useState(false);
    useEffect(() => {
        setRunAnyway(false);
    }, [executableSQL, connectionId, databaseName]);

    const validating = validation.status === 'pending';
    const blockedByValidation = validation.status === 'invalid' && !runAnyway;

    const handleRun = useCallback(async (confirmed = false) => {
        // On the initial run, extract executable SQL from the code block.
        // When re-running after confirmation, reuse the previously cleaned SQL.
        const sql = confirmed ? cleanedSQL : executableSQL;
        if (!confirmed) {
            setCleanedSQL(sql);
        }

        if (!sql.trim()) {
            setQueryState({
                loading: false,
                response: null,
                error: 'No executable SQL found in this code block',
                pendingConfirmation: false,
                writeStatements: [],
            });
            return;
        }

        setQueryState({
            loading: true,
            response: null,
            error: null,
            pendingConfirmation: false,
            writeStatements: [],
        });

        try {
            const body: Record<string, unknown> = { query: sql };
            if (databaseName) {
                body.database_name = databaseName;
            }
            if (confirmed) {
                body.confirmed = true;
            }

            const response = await apiFetch(
                `/api/v1/connections/${connectionId}/query`,
                {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(body),
                },
            );

            if (!response.ok) {
                let errorMessage = `Query failed with status ${response.status}`;
                try {
                    const errorData = await response.json();
                    if (errorData.error) {
                        errorMessage = errorData.error;
                    }
                } catch {
                    // If JSON parsing fails, try plain text
                    const errorText = await response.text();
                    if (errorText) {
                        errorMessage = errorText;
                    }
                }
                throw new Error(errorMessage);
            }

            const data: QueryResponse = await response.json();

            if (data.requires_confirmation) {
                setQueryState({
                    loading: false,
                    response: null,
                    error: null,
                    pendingConfirmation: true,
                    writeStatements: data.write_statements || [],
                });
                return;
            }

            setQueryState({
                loading: false,
                response: data,
                error: null,
                pendingConfirmation: false,
                writeStatements: [],
            });
        } catch (err) {
            setQueryState({
                loading: false,
                response: null,
                error: (err as Error).message,
                pendingConfirmation: false,
                writeStatements: [],
            });
        }
    }, [executableSQL, connectionId, databaseName, cleanedSQL]);

    const handleDismiss = useCallback(() => {
        setQueryState({
            loading: false,
            response: null,
            error: null,
            pendingConfirmation: false,
            writeStatements: [],
        });
    }, []);

    const targetLabel = serverName
        ? databaseName
            ? `Run on ${serverName}/${databaseName}`
            : `Run on ${serverName}`
        : 'Run query';

    let tooltipTitle = targetLabel;
    if (validating) {
        tooltipTitle = 'Validating query...';
    } else if (blockedByValidation) {
        tooltipTitle = 'This query failed validation';
    } else if (validation.status === 'unsupported') {
        tooltipTitle = `${targetLabel} (not validated)`;
    }

    // Notices render directly beneath the code block, so the block keeps
    // its square bottom corners when one is showing.
    const showNotice = isTemplate
        || validation.status === 'invalid'
        || validation.status === 'unsupported';

    return (
        <>
            <Box sx={{
                ...getCodeBlockWrapperSx(theme),
                position: 'relative',
                // Remove bottom border radius when results, errors, a
                // confirmation prompt or a validation notice are showing
                ...((queryState.response || queryState.error || queryState.pendingConfirmation || showNotice) ? {
                    borderBottomLeftRadius: 0,
                    borderBottomRightRadius: 0,
                    mb: 0,
                } : {}),
            }}>
                <SyntaxHighlighter
                    style={syntaxTheme}
                    language={language || 'sql'}
                    PreTag="div"
                    customStyle={getCodeBlockCustomStyle(
                        customBackground,
                        isSql ? 2 : 1,
                    )}
                    {...props}
                >
                    {codeContent}
                </SyntaxHighlighter>
                <Box sx={getCodeBlockButtonGroupSx()}>
                    <CopyCodeButton code={codeContent} theme={theme} />
                    {isSql && !isTemplate && (
                        <Tooltip title={tooltipTitle} placement="left">
                            {/* The label stays stable across validation
                                states so assistive technology announces
                                the action, not its transient status. */}
                            <span aria-label={targetLabel}>
                                <IconButton
                                    size="small"
                                    onClick={() => handleRun()}
                                    disabled={
                                        queryState.loading
                                        || validating
                                        || blockedByValidation
                                    }
                                    sx={getCopyButtonSx(theme)}
                                >
                                    {(queryState.loading || validating) ? (
                                        <CircularProgress
                                            size={14}
                                            sx={{ color: alpha(theme.palette.secondary.main, 0.7) }}
                                            aria-label={
                                                validating
                                                    ? 'Validating query'
                                                    : 'Running query'
                                            }
                                        />
                                    ) : (
                                        <PlayArrowIcon sx={{ fontSize: 18 }} />
                                    )}
                                </IconButton>
                            </span>
                        </Tooltip>
                    )}
                </Box>
            </Box>

            {/* Parameter template: not runnable without values */}
            {isTemplate && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <Box sx={getQueryErrorSx(theme)}>
                        <InfoOutlinedIcon sx={{
                            fontSize: 16,
                            color: theme.palette.info.main,
                            mt: 0.25,
                            flexShrink: 0,
                        }} />
                        <Typography
                            data-testid="sql-template-notice"
                            sx={{
                                fontSize: '0.875rem',
                                color: 'text.secondary',
                                flex: 1,
                            }}
                        >
                            {SQL_TEMPLATE_MESSAGE}
                        </Typography>
                    </Box>
                </Box>
            )}

            {/* Validation failure: Run is disabled, with an escape hatch */}
            {!isTemplate && validation.status === 'invalid' && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <Box sx={getQueryErrorSx(theme)}>
                        <WarningIcon sx={{
                            fontSize: 16,
                            color: theme.palette.warning.main,
                            mt: 0.25,
                            flexShrink: 0,
                        }} />
                        <Box sx={{ flex: 1 }}>
                            <Typography
                                data-testid="sql-validation-error"
                                sx={{
                                    fontSize: '0.875rem',
                                    color: theme.palette.mode === 'dark'
                                        ? theme.palette.error.light
                                        : theme.palette.error.dark,
                                    ...sxMonoFont,
                                    wordBreak: 'break-word',
                                }}
                            >
                                This query did not validate: {validation.error}
                            </Typography>
                            {!runAnyway && (
                                <Button
                                    size="small"
                                    onClick={() => setRunAnyway(true)}
                                    sx={{ textTransform: 'none', mt: 0.5 }}
                                >
                                    Run anyway
                                </Button>
                            )}
                        </Box>
                    </Box>
                </Box>
            )}

            {/* Statement kind that EXPLAIN cannot plan in advance */}
            {!isTemplate && validation.status === 'unsupported' && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <Box sx={getQueryErrorSx(theme)}>
                        <InfoOutlinedIcon sx={{
                            fontSize: 16,
                            color: theme.palette.info.main,
                            mt: 0.25,
                            flexShrink: 0,
                        }} />
                        <Typography
                            data-testid="sql-unvalidated-notice"
                            sx={{
                                fontSize: '0.875rem',
                                color: 'text.secondary',
                                flex: 1,
                            }}
                        >
                            {SQL_UNVALIDATED_MESSAGE}
                        </Typography>
                    </Box>
                </Box>
            )}

            {/* Write-statement confirmation prompt */}
            {queryState.pendingConfirmation && (
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
                                This code block contains statements that modify the database:
                            </Typography>
                        </Box>
                        {queryState.writeStatements.map((stmt, idx) => (
                            <Typography key={idx} sx={getConfirmationStatementSx(theme)}>
                                {'\u2022'} {stmt}
                            </Typography>
                        ))}
                        <Box sx={sxConfirmationActions}>
                            <Button
                                size="small"
                                onClick={handleDismiss}
                                sx={{ textTransform: 'none' }}
                            >
                                Cancel
                            </Button>
                            <Button
                                size="small"
                                variant="contained"
                                color="warning"
                                onClick={() => handleRun(true)}
                                sx={{ textTransform: 'none' }}
                            >
                                Execute
                            </Button>
                        </Box>
                    </Box>
                </Box>
            )}

            {/* Query results - multiple statement results */}
            {queryState.response?.results && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <Box sx={getQueryResultHeaderSx(theme)}>
                        <Typography sx={{
                            fontSize: '0.875rem',
                            color: 'text.secondary',
                            ...sxMonoFont,
                        }}>
                            {queryState.response.total_statements} statement{queryState.response.total_statements !== 1 ? 's' : ''}
                        </Typography>
                        <IconButton
                            size="small"
                            onClick={handleDismiss}
                            sx={{ p: 0.25, color: 'text.secondary' }}
                        >
                            <CloseIcon sx={{ fontSize: 14 }} />
                        </IconButton>
                    </Box>
                    {queryState.response.results.map((result, idx) => (
                        <Box key={idx}>
                            {/* Query label */}
                            <Typography sx={{
                                fontSize: '0.875rem',
                                color: 'text.disabled',
                                ...sxMonoFont,
                                px: 1.5,
                                py: 0.5,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                                borderTop: idx > 0 ? '1px solid' : 'none',
                                borderColor: theme.palette.mode === 'dark'
                                    ? theme.palette.grey[700]
                                    : theme.palette.grey[200],
                            }}>
                                {result.query}
                            </Typography>

                            {/* Statement error */}
                            {result.error && (
                                <Box sx={getQueryErrorSx(theme)}>
                                    <ErrorIcon sx={{
                                        fontSize: 16,
                                        color: theme.palette.error.main,
                                        mt: 0.25,
                                        flexShrink: 0,
                                    }} />
                                    <Typography sx={{
                                        fontSize: '0.875rem',
                                        color: theme.palette.mode === 'dark'
                                            ? theme.palette.error.light
                                            : theme.palette.error.dark,
                                        flex: 1,
                                        ...sxMonoFont,
                                        wordBreak: 'break-word',
                                    }}>
                                        {result.error}
                                    </Typography>
                                </Box>
                            )}

                            {/* Statement results */}
                            {result.columns && result.rows && (
                                <>
                                    <Box sx={{ overflowX: 'auto' }}>
                                        <Box component="table" sx={getTableSx(theme)}>
                                            <thead>
                                                <tr>
                                                    {result.columns.map((col, i) => (
                                                        <th key={i}>
                                                            <Typography sx={{
                                                                fontSize: '0.875rem',
                                                                fontWeight: 600,
                                                                ...sxMonoFont,
                                                                whiteSpace: 'nowrap',
                                                            }}>
                                                                {col}
                                                            </Typography>
                                                        </th>
                                                    ))}
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {result.rows.map((row, ri) => (
                                                    <tr key={ri}>
                                                        {row.map((cell, ci) => (
                                                            <td key={ci}>
                                                                <Typography sx={{
                                                                    fontSize: '0.875rem',
                                                                    ...sxMonoFont,
                                                                    whiteSpace: 'nowrap',
                                                                }}>
                                                                    {cell}
                                                                </Typography>
                                                            </td>
                                                        ))}
                                                    </tr>
                                                ))}
                                            </tbody>
                                        </Box>
                                    </Box>
                                    {result.truncated && (
                                        <Typography sx={{
                                            fontSize: '0.875rem',
                                            color: 'text.disabled',
                                            px: 1.5,
                                            py: 0.5,
                                        }}>
                                            Results limited to {result.row_count} rows
                                        </Typography>
                                    )}
                                    {!result.truncated && result.row_count !== undefined && (
                                        <Typography sx={{
                                            fontSize: '0.875rem',
                                            color: 'text.disabled',
                                            px: 1.5,
                                            py: 0.5,
                                        }}>
                                            {result.row_count} row{result.row_count !== 1 ? 's' : ''}
                                        </Typography>
                                    )}
                                </>
                            )}
                        </Box>
                    ))}
                </Box>
            )}

            {/* Top-level fetch error (network failure, non-200 status) */}
            {queryState.error && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <Box sx={getQueryErrorSx(theme)}>
                        <ErrorIcon sx={{
                            fontSize: 16,
                            color: theme.palette.error.main,
                            mt: 0.25,
                            flexShrink: 0,
                        }} />
                        <Typography sx={{
                            fontSize: '0.875rem',
                            color: theme.palette.mode === 'dark'
                                ? theme.palette.error.light
                                : theme.palette.error.dark,
                            flex: 1,
                            ...sxMonoFont,
                            wordBreak: 'break-word',
                        }}>
                            {queryState.error}
                        </Typography>
                        <IconButton
                            size="small"
                            onClick={handleDismiss}
                            sx={{ p: 0.25, color: 'text.secondary', flexShrink: 0 }}
                        >
                            <CloseIcon sx={{ fontSize: 14 }} />
                        </IconButton>
                    </Box>
                </Box>
            )}
        </>
    );
};

export default RunnableCodeBlock;

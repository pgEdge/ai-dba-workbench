/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ActionBlock component. Shows a proposed
 * database write through its life: the supervisor's review, the user's
 * approval, the run and its outcome. It either creates the action from
 * SQL (and runs it at once if the review passes) or follows one that
 * already exists, such as one Ellie proposed in chat.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useContext, useEffect, useRef, useState } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import { useTheme } from '@mui/material/styles';
import CloseIcon from '@mui/icons-material/Close';
import ErrorIcon from '@mui/icons-material/Error';
import { useAction } from '../../hooks/useAction';
import AuthContext from '../../contexts/AuthContext';
import { isTerminalExecStatus } from '../../types/actions';
import type {
    Action,
    ActionCreateRequest,
    ClientActionSource,
} from '../../types/actions';
import OverrideActionDialog, { dangerColour } from './OverrideActionDialog';
import {
    sxMonoFont,
    getTableSx,
    getQueryResultWrapperSx,
    getQueryErrorSx,
} from './markdownStyles';

export interface ActionBlockProps {
    /** An action already in hand, for example one from a chat reply. */
    action?: Action;
    /** The id of an action to load, when only the id is known. */
    actionId?: number;
    /** SQL to propose; it is created and run at once on mount. */
    sql?: string;
    connectionId: number;
    databaseName?: string;
    /** The connection's name, which an override must be confirmed with. */
    serverName?: string;
    source: ClientActionSource;
    context?: string;
    alertId?: number;
    conversationId?: string;
    /** When given, a close button appears once there is nothing to wait for. */
    onDismiss?: () => void;
}

const REVIEWING = 'Supervisor reviewing…';

const pluralise = (count: number, word: string): string =>
    `${count} ${word}${count === 1 ? '' : 's'}`;

const ActionBlock: React.FC<ActionBlockProps> = ({
    action: initialAction,
    actionId,
    sql,
    connectionId,
    databaseName,
    serverName,
    source,
    context,
    alertId,
    conversationId,
    onDismiss,
}) => {
    const theme = useTheme();
    const auth = useContext(AuthContext);
    const { action: polled, busy, error, create, execute, cancel, load } =
        useAction();
    const [overrideOpen, setOverrideOpen] = useState(false);

    // Creating twice would record two actions, so the mount-time request
    // is guarded against effects that run twice in development.
    const startedRef = useRef(false);
    useEffect(() => {
        if (startedRef.current) {
            return;
        }
        startedRef.current = true;

        if (sql !== undefined) {
            const body: ActionCreateRequest = {
                sql,
                source,
                execute: true,
            };
            if (databaseName) {
                body.database_name = databaseName;
            }
            if (context) {
                body.context = context;
            }
            if (alertId !== undefined) {
                body.alert_id = alertId;
            }
            if (conversationId) {
                body.conversation_id = conversationId;
            }
            void create(connectionId, body);
        } else if (initialAction) {
            // Only a run in progress needs following; one awaiting its
            // approval or already finished will not change by itself.
            if (initialAction.exec_status === 'executing') {
                void load(initialAction.id);
            }
        } else if (actionId !== undefined) {
            void load(actionId);
        }
    }, [
        sql, source, databaseName, context, alertId, conversationId,
        connectionId, initialAction, actionId, create, load,
    ]);

    const current = polled ?? initialAction ?? null;

    const mono = { ...sxMonoFont, wordBreak: 'break-word' as const };

    const frame = (children: React.ReactNode) => (
        <Box
            data-testid="action-block"
            sx={getQueryResultWrapperSx(theme)}
        >
            {children}
        </Box>
    );

    const errorRow = error ? (
        <Box sx={getQueryErrorSx(theme)} role="alert">
            <ErrorIcon sx={{ fontSize: 16, color: 'error.main', mt: 0.25 }} />
            <Typography variant="body2" sx={{ flex: 1, ...mono }}>
                {error}
            </Typography>
        </Box>
    ) : null;

    if (!current) {
        if (error) {
            return frame(errorRow);
        }
        return frame(
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 1.5 }}>
                <CircularProgress size={14} aria-label="Loading action" />
                <Typography variant="body2">
                    {sql !== undefined || busy ? REVIEWING : 'Loading action…'}
                </Typography>
            </Box>,
        );
    }

    const review = current.review;
    const reviewing = current.review_status === 'pending';
    const vetoed = current.review_status === 'vetoed';
    const finished = isTerminalExecStatus(current.exec_status);
    const results = current.results ?? [];
    const rowsAffected = results.reduce(
        (sum, r) => sum + (r.error ? 0 : (r.rows_affected ?? 0)),
        0,
    );
    const firstError = results.find((r) => r.error)?.error;

    // A user may only override an action they proposed, or any action if
    // they are a superuser; the server still checks write access.
    const canOverride = Boolean(
        vetoed
        && current.exec_status === 'not_started'
        && serverName
        && (auth?.user?.isSuperuser
            || (auth?.user?.username && auth.user.username === current.proposed_by)),
    );

    const reviewLabel = reviewing
        ? REVIEWING
        : vetoed
            ? 'Vetoed by Ellie\'s supervisor'
            : review?.reviewer === 'none'
                ? 'Not yet reviewed'
                : 'Approved by Ellie\'s supervisor';

    const reviewTone = vetoed ? 'error.main' : 'text.primary';

    return frame(
        <Box sx={{ p: 1.5, display: 'flex', flexDirection: 'column', gap: 1 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                {reviewing && (
                    <CircularProgress size={14} aria-label="Supervisor reviewing" />
                )}
                <Typography variant="subtitle2" sx={{ color: reviewTone, flex: 1 }}>
                    {reviewLabel}
                </Typography>
                {!reviewing && review?.danger && (
                    <Chip
                        size="small"
                        label={`Danger: ${review.danger}`}
                        color={dangerColour(review.danger)}
                    />
                )}
                {onDismiss && (finished || vetoed) && (
                    <IconButton
                        size="small"
                        aria-label="Dismiss"
                        onClick={onDismiss}
                    >
                        <CloseIcon sx={{ fontSize: 14 }} />
                    </IconButton>
                )}
            </Box>

            {vetoed && (review?.reasons?.length ?? 0) > 0 && (
                <Box component="ul" sx={{ m: 0, pl: 3 }}>
                    {review?.reasons?.map((reason, idx) => (
                        <li key={idx}>
                            <Typography variant="body2">{reason}</Typography>
                        </li>
                    ))}
                </Box>
            )}

            {sql === undefined && current.statements.length > 0 && (
                <Box sx={{ m: 0 }}>
                    {current.statements.map((stmt, idx) => (
                        <Typography
                            key={idx}
                            variant="body2"
                            sx={{ ...mono, whiteSpace: 'pre-wrap' }}
                        >
                            {stmt}
                        </Typography>
                    ))}
                </Box>
            )}

            {current.exec_status === 'executing' && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <CircularProgress size={14} aria-label="Running" />
                    <Typography variant="body2" sx={{ flex: 1 }}>
                        Running…
                    </Typography>
                    <Button
                        size="small"
                        onClick={() => { void cancel(current.id); }}
                        sx={{ textTransform: 'none' }}
                    >
                        Cancel
                    </Button>
                </Box>
            )}

            {current.exec_status === 'succeeded' && (
                <Box>
                    <Typography variant="subtitle2" sx={{ color: 'success.main' }}>
                        Succeeded
                    </Typography>
                    <Typography variant="body2">
                        {pluralise(rowsAffected, 'row')} affected
                    </Typography>
                </Box>
            )}

            {current.exec_status === 'failed' && (
                <Box>
                    <Typography variant="subtitle2" sx={{ color: 'error.main' }}>
                        Failed
                    </Typography>
                    {firstError && (
                        <Typography variant="body2" sx={mono}>
                            {firstError}
                        </Typography>
                    )}
                </Box>
            )}

            {current.exec_status === 'unknown' && (
                <Typography variant="subtitle2" sx={{ color: 'warning.main' }}>
                    Outcome unknown: check the database
                </Typography>
            )}

            {current.exec_status === 'cancelled' && (
                <Typography variant="subtitle2">Cancelled</Typography>
            )}

            {results.map((result, idx) =>
                result.columns && result.rows ? (
                    <Box key={idx} sx={{ overflowX: 'auto' }}>
                        <Box component="table" sx={getTableSx(theme)}>
                            <thead>
                                <tr>
                                    {result.columns.map((col, i) => (
                                        <th key={i}>
                                            <Typography variant="body2" sx={{ ...sxMonoFont, fontWeight: 600 }}>
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
                                                <Typography variant="body2" sx={sxMonoFont}>
                                                    {cell}
                                                </Typography>
                                            </td>
                                        ))}
                                    </tr>
                                ))}
                            </tbody>
                        </Box>
                        {result.truncated && (
                            <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                                Results limited to {pluralise(result.rows.length, 'row')}
                            </Typography>
                        )}
                    </Box>
                ) : null,
            )}

            {current.exec_status === 'not_started' && (
                <Box sx={{ display: 'flex', gap: 1 }}>
                    {current.review_status === 'passed'
                        && current.approval === 'none' && (
                        <Button
                            size="small"
                            variant="contained"
                            color="warning"
                            disabled={busy}
                            onClick={() => { void execute(current.id); }}
                            sx={{ textTransform: 'none' }}
                        >
                            Execute
                        </Button>
                    )}
                    {current.approval !== 'none' && (
                        <Button
                            size="small"
                            variant="outlined"
                            disabled={busy}
                            onClick={() => { void execute(current.id); }}
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
                            onClick={() => { setOverrideOpen(true); }}
                            sx={{ textTransform: 'none' }}
                        >
                            Override and run
                        </Button>
                    )}
                </Box>
            )}

            {errorRow}

            {canOverride && serverName && (
                <OverrideActionDialog
                    open={overrideOpen}
                    action={current}
                    connectionName={serverName}
                    onClose={() => { setOverrideOpen(false); }}
                    onConfirm={(confirm) => {
                        setOverrideOpen(false);
                        void execute(current.id, { confirm });
                    }}
                />
            )}
        </Box>,
    );
};

export default ActionBlock;

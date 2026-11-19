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
import { useContext, useState } from 'react';
import Box from '@mui/material/Box';
import AuthContext from '../../contexts/AuthContext';
import { useAction } from '../../hooks/useAction';
import type { UseActionResult } from '../../hooks/useAction';
import type { Action, ClientActionSource } from '../../types/actions';
import OverrideActionDialog from './OverrideActionDialog';
import type { ActionFlags } from './actionBlockState';
import {
    REVIEWING,
    deriveActionFlags,
    selectCurrentAction,
} from './actionBlockState';
import {
    ActionButtons,
    ActionErrorRow,
    ActionFrame,
    ActionHeader,
    ActionLoading,
    ActionResults,
    ExecStatus,
    StatementList,
    VetoReasons,
} from './ActionBlockParts';
import { useActionBlockStart } from './useActionBlockStart';

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
    /** Required when `sql` is given; unused for an existing action. */
    source?: ClientActionSource;
    context?: string;
    alertId?: number;
    conversationId?: string;
    /** When given, a close button appears once there is nothing to wait for. */
    onDismiss?: () => void;
}

interface ActionOverrideDialogProps {
    open: boolean;
    current: Action;
    serverName: string;
    setOpen: (open: boolean) => void;
    execute: UseActionResult['execute'];
}

const ActionOverrideDialog: React.FC<ActionOverrideDialogProps> = ({
    open,
    current,
    serverName,
    setOpen,
    execute,
}) => (
    <OverrideActionDialog
        open={open}
        action={current}
        connectionName={serverName}
        onClose={() => { setOpen(false); }}
        onConfirm={(confirm) => {
            setOpen(false);
            void execute(current.id, { confirm });
        }}
    />
);

interface ActionDetailsProps {
    current: Action;
    flags: ActionFlags;
    busy: boolean;
    errorRow: React.ReactNode;
    /** Whether the block created the action from SQL it already shows. */
    fromSql: boolean;
    serverName?: string;
    overrideOpen: boolean;
    setOverrideOpen: (open: boolean) => void;
    execute: UseActionResult['execute'];
    cancel: UseActionResult['cancel'];
    onDismiss?: () => void;
}

const ActionDetails: React.FC<ActionDetailsProps> = ({
    current,
    flags,
    busy,
    errorRow,
    fromSql,
    serverName,
    overrideOpen,
    setOverrideOpen,
    execute,
    cancel,
    onDismiss,
}) => {
    return (
        <Box sx={{ p: 1.5, display: 'flex', flexDirection: 'column', gap: 1 }}>
            <ActionHeader current={current} flags={flags} onDismiss={onDismiss} />
            {flags.vetoed && <VetoReasons reasons={current.review?.reasons} />}
            {!fromSql && current.statements.length > 0 && (
                <StatementList statements={current.statements} />
            )}
            <ExecStatus
                current={current}
                onCancel={() => { void cancel(current.id); }}
            />
            <ActionResults results={current.results} />
            {current.exec_status === 'not_started' && (
                <ActionButtons
                    flags={flags}
                    busy={busy}
                    onExecute={() => { void execute(current.id); }}
                    onOpenOverride={() => { setOverrideOpen(true); }}
                />
            )}
            {errorRow}
            {flags.showOverrideDialog && serverName && (
                <ActionOverrideDialog
                    open={overrideOpen}
                    current={current}
                    serverName={serverName}
                    setOpen={setOverrideOpen}
                    execute={execute}
                />
            )}
        </Box>
    );
};

const ActionBlock: React.FC<ActionBlockProps> = (props) => {
    const { action: initialAction, actionId, sql, serverName, source } = props;
    const { action: polled, busy, error, create, execute, cancel, load } =
        useAction();
    const [overrideOpen, setOverrideOpen] = useState(false);
    const user = useContext(AuthContext)?.user;

    useActionBlockStart({ ...props, initialAction, create, load });

    const current = selectCurrentAction(polled, initialAction, actionId, sql);
    const errorRow = error ? <ActionErrorRow>{error}</ActionErrorRow> : null;

    if (sql !== undefined && !source) {
        return (
            <ActionFrame>
                <ActionErrorRow>
                    This SQL cannot be proposed because its origin is
                    unknown.
                </ActionErrorRow>
            </ActionFrame>
        );
    }

    if (!current) {
        const label = sql !== undefined || busy ? REVIEWING : 'Loading action…';
        return (
            <ActionFrame>
                {errorRow ?? <ActionLoading label={label} />}
            </ActionFrame>
        );
    }

    return (
        <ActionFrame>
            <ActionDetails
                current={current}
                flags={deriveActionFlags(current, user, serverName)}
                busy={busy}
                errorRow={errorRow}
                fromSql={sql !== undefined}
                serverName={serverName}
                overrideOpen={overrideOpen}
                setOverrideOpen={setOverrideOpen}
                execute={execute}
                cancel={cancel}
                onDismiss={props.onDismiss}
            />
        </ActionFrame>
    );
};

export default ActionBlock;

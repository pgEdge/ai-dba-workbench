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
import { useState, useEffect, useMemo } from 'react';
import Box from '@mui/material/Box';
import type { Theme } from '@mui/material/styles';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import ActionBlock from './ActionBlock';
import type { ClientActionSource } from '../../types/actions';
import { useSqlValidation } from '../../hooks/useSqlValidation';
import type { SqlValidationState } from '../../hooks/useSqlValidation';
import { extractExecutableSQL, hasSqlParameters } from './sqlDetection';
import CopyCodeButton from './CopyCodeButton';
import {
    getCodeBlockWrapperSx,
    getCodeBlockCustomStyle,
    getCodeBlockButtonGroupSx,
    getQueryResultWrapperSx,
} from './markdownStyles';
import { useRunnableQuery } from './useRunnableQuery';
import type { QueryState, UseRunnableQueryResult } from './useRunnableQuery';
import {
    RunButton,
    InfoNotice,
    ValidationFailedNotice,
    WriteConfirmation,
    QueryResults,
    ErrorLine,
} from './RunnableCodeBlockParts';

export type {
    StatementResult,
    QueryResponse,
    QueryState,
} from './useRunnableQuery';

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

/**
 * Label shown for a block that carries placeholders (`$1`, `<name>`,
 * `{{name}}`, `${name}` or `:name`); see `hasSqlParameters`.
 */
export const SQL_TEMPLATE_MESSAGE =
    'This is a query template: it uses placeholders such as $1, <name>, '
    + '{{name}}, ${name} or :name, so it needs values substituted in before '
    + 'it can be run.';

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
    /** Where a write confirmed from this block is recorded as coming from. */
    actionSource?: ClientActionSource;
    /** What the user was looking at, kept with any action created. */
    actionContext?: string;
    alertId?: number;
    conversationId?: string;
}

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
    /** Where a write confirmed from this block is recorded as coming from. */
    actionSource?: ClientActionSource;
    /** What the user was looking at, kept with any action created. */
    actionContext?: string;
    alertId?: number;
    conversationId?: string;
}

/** The server and database the block runs on, as a display name. */
const targetNameOf = (serverName?: string, databaseName?: string): string => {
    if (!serverName) {
        return '';
    }
    return databaseName ? `${serverName}/${databaseName}` : serverName;
};

/** The Run button's tooltip for the current validation state. */
const runTooltipOf = (
    targetLabel: string,
    validation: SqlValidationState,
    blockedByValidation: boolean,
): string => {
    if (validation.status === 'pending') {
        return 'Validating query...';
    }
    if (blockedByValidation) {
        return 'This query failed validation';
    }
    if (validation.status === 'unsupported') {
        return `${targetLabel} (not validated)`;
    }
    return targetLabel;
};

/** Whether anything renders beneath the code block. */
const hasOutputBelow = (queryState: QueryState, showNotice: boolean): boolean =>
    queryState.response !== null
    || Boolean(queryState.error)
    || queryState.pendingConfirmation
    || showNotice;

interface BlockNoticesProps {
    theme: Theme;
    isTemplate: boolean;
    validation: SqlValidationState;
    runAnyway: boolean;
    onRunAnyway: () => void;
}

/** Template and validation notices shown directly beneath the code. */
const BlockNotices: React.FC<BlockNoticesProps> = ({
    theme,
    isTemplate,
    validation,
    runAnyway,
    onRunAnyway,
}) => {
    if (isTemplate) {
        // Parameter template: not runnable without values
        return (
            <InfoNotice theme={theme} testId="sql-template-notice">
                {SQL_TEMPLATE_MESSAGE}
            </InfoNotice>
        );
    }
    if (validation.status === 'invalid') {
        return (
            <ValidationFailedNotice
                theme={theme}
                error={validation.error}
                runAnyway={runAnyway}
                onRunAnyway={onRunAnyway}
            />
        );
    }
    if (validation.status === 'unsupported') {
        // Statement kind that EXPLAIN cannot plan in advance
        return (
            <InfoNotice theme={theme} testId="sql-unvalidated-notice">
                {SQL_UNVALIDATED_MESSAGE}
            </InfoNotice>
        );
    }
    return null;
};

interface QueryOutputProps {
    blockProps: RunnableCodeBlockProps;
    query: UseRunnableQueryResult;
    targetName: string;
}

/** The confirmation prompt, action, results or error for a run. */
const QueryOutput: React.FC<QueryOutputProps> = ({
    blockProps,
    query,
    targetName,
}) => {
    const { theme } = blockProps;
    const { queryState, confirmedWrite, confirm, dismiss } = query;
    return (
        <>
            {/* A confirmed write is proposed, reviewed and run as an action */}
            {queryState.pendingConfirmation && confirmedWrite !== null && (
                <ActionBlock
                    sql={confirmedWrite.sql}
                    connectionId={confirmedWrite.target.connectionId}
                    databaseName={confirmedWrite.target.databaseName}
                    serverName={blockProps.serverName}
                    source={blockProps.actionSource ?? 'chat_block'}
                    context={blockProps.actionContext}
                    alertId={blockProps.alertId}
                    conversationId={blockProps.conversationId}
                    onDismiss={dismiss}
                />
            )}
            {queryState.pendingConfirmation && confirmedWrite === null && (
                <WriteConfirmation
                    theme={theme}
                    targetName={targetName}
                    writeStatements={queryState.writeStatements}
                    onCancel={dismiss}
                    onConfirm={confirm}
                />
            )}
            {queryState.response?.results && (
                <QueryResults
                    theme={theme}
                    response={{
                        ...queryState.response,
                        results: queryState.response.results,
                    }}
                    onDismiss={dismiss}
                />
            )}
            {/* Top-level fetch error (network failure, non-200 status) */}
            {queryState.error && (
                <Box sx={getQueryResultWrapperSx(theme)}>
                    <ErrorLine
                        theme={theme}
                        message={queryState.error}
                        onDismiss={dismiss}
                    />
                </Box>
            )}
        </>
    );
};

interface BlockRunControlProps {
    theme: Theme;
    targetName: string;
    validation: SqlValidationState;
    blockedByValidation: boolean;
    query: UseRunnableQueryResult;
}

/** The Run button, reflecting validation and any run or action. */
const BlockRunControl: React.FC<BlockRunControlProps> = ({
    theme,
    targetName,
    validation,
    blockedByValidation,
    query,
}) => {
    const targetLabel = targetName ? `Run on ${targetName}` : 'Run query';
    const validating = validation.status === 'pending';
    return (
        <RunButton
            theme={theme}
            targetLabel={targetLabel}
            tooltipTitle={runTooltipOf(
                targetLabel, validation, blockedByValidation,
            )}
            // While an action is showing, Run stays off: running again
            // would drop the action's status and Cancel whilst the
            // server works, and could propose the same write twice.
            disabled={
                query.queryState.loading
                || validating
                || blockedByValidation
                || query.confirmedWrite !== null
            }
            loading={query.queryState.loading}
            validating={validating}
            onRun={() => { void query.run(); }}
        />
    );
};

const RunnableCodeBlock: React.FC<RunnableCodeBlockProps> = (blockProps) => {
    const {
        codeContent,
        language,
        connectionId,
        databaseName,
        serverName,
        syntaxTheme,
        customBackground,
        theme,
        isSql,
        props,
    } = blockProps;

    // The SQL that would actually be sent to the server, which is also
    // what gets validated.
    const executableSQL = useMemo(
        () => extractExecutableSQL(codeContent),
        [codeContent],
    );

    // A block with placeholders is a template rather than a query:
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

    const query = useRunnableQuery(executableSQL, connectionId, databaseName);

    const blockedByValidation = validation.status === 'invalid' && !runAnyway;
    const targetName = targetNameOf(serverName, databaseName);

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
                ...(hasOutputBelow(query.queryState, showNotice) ? {
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
                        <BlockRunControl
                            theme={theme}
                            targetName={targetName}
                            validation={validation}
                            blockedByValidation={blockedByValidation}
                            query={query}
                        />
                    )}
                </Box>
            </Box>
            <BlockNotices
                theme={theme}
                isTemplate={isTemplate}
                validation={validation}
                runAnyway={runAnyway}
                onRunAnyway={() => { setRunAnyway(true); }}
            />
            <QueryOutput
                blockProps={blockProps}
                query={query}
                targetName={targetName}
            />
        </>
    );
};

export default RunnableCodeBlock;

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - SQL code block for Ellie's chat replies. Adds
 * a Run button bound to the server and database the reply was about, or
 * a server selector when that is not clear.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useContext, useMemo } from 'react';
import type { Theme } from '@mui/material/styles';
import RunnableCodeBlock from '../shared/RunnableCodeBlock';
import ConnectionSelectorCodeBlock from '../shared/ConnectionSelectorCodeBlock';
import { planSqlBlock } from './chatSqlTarget';
import ReplyTargetsContext from './replyTargetsContext';
import { useKnownServers } from './useKnownServers';

interface ChatSqlCodeBlockProps {
    code: string;
    language: string;
    isDark: boolean;
    syntaxTheme: Record<string, unknown>;
    customBackground: string;
    theme: Theme;
    props: Record<string, unknown>;
}

const ChatSqlCodeBlock: React.FC<ChatSqlCodeBlockProps> = ({
    code,
    language,
    isDark,
    syntaxTheme,
    customBackground,
    theme,
    props,
}) => {
    const reply = useContext(ReplyTargetsContext);
    const servers = useKnownServers();
    const plan = useMemo(
        () => planSqlBlock(code, reply, servers),
        [code, reply, servers],
    );

    const shared = {
        language,
        isDark,
        syntaxTheme,
        customBackground,
        theme,
        props,
    };

    if (plan.kind === 'run') {
        return (
            <RunnableCodeBlock
                {...shared}
                codeContent={plan.code}
                connectionId={plan.target.connectionId}
                serverName={plan.target.serverName}
                databaseName={plan.target.databaseName}
                isSql={true}
            />
        );
    }

    if (plan.kind === 'select') {
        return (
            <ConnectionSelectorCodeBlock
                {...shared}
                codeContent={plan.code}
                options={plan.options}
                requireSelection={true}
            />
        );
    }

    if (plan.kind === 'template') {
        // RunnableCodeBlock recognises the placeholders itself, so it
        // shows the template notice without a Run button or validation.
        return (
            <RunnableCodeBlock
                {...shared}
                codeContent={plan.code}
                connectionId={0}
                isSql={true}
            />
        );
    }

    // No runnable target: render the block with a Copy button only.
    return (
        <RunnableCodeBlock
            {...shared}
            codeContent={code}
            connectionId={0}
            isSql={false}
        />
    );
};

export default ChatSqlCodeBlock;

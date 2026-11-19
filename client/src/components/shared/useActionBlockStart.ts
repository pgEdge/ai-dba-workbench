/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - the mount-time request of an ActionBlock:
 * create the action from SQL, or load an existing one to follow it.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useEffect, useRef } from 'react';
import type { UseActionResult } from '../../hooks/useAction';
import type { Action, ClientActionSource } from '../../types/actions';
import { actionIdToLoad, buildCreateRequest } from './actionBlockState';

export interface ActionBlockStartOptions {
    initialAction?: Action;
    actionId?: number;
    sql?: string;
    connectionId: number;
    databaseName?: string;
    source?: ClientActionSource;
    context?: string;
    alertId?: number;
    conversationId?: string;
    create: UseActionResult['create'];
    load: UseActionResult['load'];
}

export const useActionBlockStart = ({
    initialAction,
    actionId,
    sql,
    connectionId,
    databaseName,
    source,
    context,
    alertId,
    conversationId,
    create,
    load,
}: ActionBlockStartOptions): void => {
    // Creating twice would record two actions, so the mount-time request
    // is guarded against effects that run twice in development. The guard
    // is keyed by what the block shows, so a different action id starts a
    // fresh load.
    const startedRef = useRef<string | null>(null);
    const startKey = sql !== undefined
        ? 'sql'
        : `action:${initialAction?.id ?? actionId ?? ''}`;
    useEffect(() => {
        if (startedRef.current === startKey) {
            return;
        }
        startedRef.current = startKey;

        if (sql !== undefined) {
            if (source) {
                void create(connectionId, buildCreateRequest(sql, source, {
                    databaseName, context, alertId, conversationId,
                }));
            }
            return;
        }
        const id = actionIdToLoad(initialAction, actionId);
        if (id !== undefined) {
            void load(id);
        }
    }, [
        startKey, sql, source, databaseName, context, alertId,
        conversationId, connectionId, initialAction, actionId, create, load,
    ]);
};

export default useActionBlockStart;

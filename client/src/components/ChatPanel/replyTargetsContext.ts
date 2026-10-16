/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Context carrying where a chat reply's tool
 * calls ran down to its SQL code blocks.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { createContext } from 'react';
import type { ReplyTargets } from './chatSqlTarget';

/**
 * Targets of the reply being rendered. A context is used rather than a
 * dependency of the memoised markdown components because changing those
 * components remounts every code block, discarding any query results
 * the user has already run.
 */
export const EMPTY_REPLY_TARGETS: ReplyTargets = {
    targets: [],
    unattributed: false,
    usedDatastore: false,
};

const ReplyTargetsContext = createContext<ReplyTargets>(EMPTY_REPLY_TARGETS);

export default ReplyTargetsContext;

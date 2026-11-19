/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ToolActivity } from './ToolStatus';

export interface ProposedActionBlock {
    actionId: number;
    connectionId: number;
    databaseName?: string;
}

/**
 * Work out which proposed actions a reply should show. Each
 * `propose_action` call that revises an earlier attempt in the same reply
 * belongs to that attempt's chain; the latest attempt in a chain is the one
 * shown, provided the supervisor passed it or ran out of attempts (in which
 * case the best attempt is shown). A chain still awaiting a revision shows
 * nothing, and separate chains each get a block, in the order they began.
 */
export const collectProposedActionBlocks = (
    activity: ToolActivity[] | undefined,
): ProposedActionBlock[] => {
    // Maps an action id to the index of the chain it belongs to.
    const chainOf = new Map<number, number>();
    const latest: ToolActivity[] = [];

    for (const call of activity ?? []) {
        const result = call.proposal;
        if (!result || call.connectionId === undefined) {
            continue;
        }
        const parent = call.revisesActionId !== undefined
            ? chainOf.get(call.revisesActionId)
            : undefined;
        const chain = parent ?? latest.length;
        chainOf.set(result.action_id, chain);
        latest[chain] = call;
    }

    const blocks: ProposedActionBlock[] = [];
    const seen = new Set<number>();
    for (const call of latest) {
        const result = call.proposal;
        if (!result || call.connectionId === undefined) {
            continue;
        }
        if (result.review_status !== 'passed' && !result.chain_exhausted) {
            continue;
        }
        const actionId = result.chain_exhausted
            ? (result.best_action_id ?? result.action_id)
            : result.action_id;
        if (seen.has(actionId)) {
            continue;
        }
        seen.add(actionId);
        blocks.push({
            actionId,
            connectionId: call.connectionId,
            databaseName: call.databaseName,
        });
    }
    return blocks;
};

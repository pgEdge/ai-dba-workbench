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

/** A tool call that proposed an action on a known connection. */
type ProposalCall = ToolActivity & {
    proposal: NonNullable<ToolActivity['proposal']>;
    connectionId: number;
};

const isProposalCall = (call: ToolActivity): call is ProposalCall =>
    Boolean(call.proposal) && call.connectionId !== undefined;

/**
 * Group the proposals into revision chains, keeping the latest attempt of
 * each chain in the order the chains began.
 */
const latestOfEachChain = (calls: ProposalCall[]): ProposalCall[] => {
    // Maps an action id to the index of the chain it belongs to.
    const chainOf = new Map<number, number>();
    const latest: ProposalCall[] = [];
    for (const call of calls) {
        const parent = call.revisesActionId !== undefined
            ? chainOf.get(call.revisesActionId)
            : undefined;
        const chain = parent ?? latest.length;
        chainOf.set(call.proposal.action_id, chain);
        latest[chain] = call;
    }
    return latest;
};

/**
 * The action a chain's latest attempt shows: itself if the supervisor
 * passed it, the best attempt if the chain ran out of attempts, or none
 * while a revision is still awaited.
 */
const shownActionId = (
    result: ProposalCall['proposal'],
): number | undefined => {
    if (result.chain_exhausted) {
        return result.best_action_id ?? result.action_id;
    }
    return result.review_status === 'passed' ? result.action_id : undefined;
};

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
    const latest = latestOfEachChain((activity ?? []).filter(isProposalCall));
    const blocks: ProposedActionBlock[] = [];
    const seen = new Set<number>();
    for (const call of latest) {
        const actionId = shownActionId(call.proposal);
        if (actionId === undefined || seen.has(actionId)) {
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

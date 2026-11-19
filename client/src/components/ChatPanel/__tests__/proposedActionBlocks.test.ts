/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import { collectProposedActionBlocks } from '../proposedActionBlocks';
import type { ToolActivity } from '../ToolStatus';
import type { ProposeActionResult } from '../../../types/actions';

const call = (
    proposal: ProposeActionResult,
    extra: Partial<ToolActivity> = {},
): ToolActivity => ({
    name: 'propose_action',
    status: 'completed',
    connectionId: 1,
    proposal,
    ...extra,
});

describe('collectProposedActionBlocks', () => {
    it('returns nothing without activity', () => {
        expect(collectProposedActionBlocks(undefined)).toEqual([]);
    });

    it('skips calls with no proposal or connection', () => {
        expect(collectProposedActionBlocks([
            { name: 'propose_action', status: 'error' },
            call(
                { action_id: 1, review_status: 'passed' },
                { connectionId: undefined },
            ),
        ])).toEqual([]);
    });

    it('follows a revision chain to its latest attempt', () => {
        const blocks = collectProposedActionBlocks([
            call({ action_id: 1, review_status: 'vetoed' }),
            call({ action_id: 2, review_status: 'vetoed' }, { revisesActionId: 1 }),
            call({ action_id: 3, review_status: 'passed' }, { revisesActionId: 2 }),
        ]);
        expect(blocks).toEqual([
            { actionId: 3, connectionId: 1, databaseName: undefined },
        ]);
    });

    it('treats a revision of an unknown action as a new chain', () => {
        const blocks = collectProposedActionBlocks([
            call({ action_id: 5, review_status: 'passed' }, { revisesActionId: 99 }),
        ]);
        expect(blocks.map((b) => b.actionId)).toEqual([5]);
    });

    it('uses the best attempt of an exhausted chain and drops duplicates', () => {
        const blocks = collectProposedActionBlocks([
            call({
                action_id: 4, review_status: 'vetoed',
                chain_exhausted: true, best_action_id: 2,
            }),
            call({
                action_id: 8, review_status: 'vetoed',
                chain_exhausted: true, best_action_id: 2,
            }),
        ]);
        expect(blocks.map((b) => b.actionId)).toEqual([2]);
    });
});

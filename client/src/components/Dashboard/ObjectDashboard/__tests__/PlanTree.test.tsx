/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import PlanTree from '../PlanTree';
import { layoutPlanTree } from '../PlanTree';
import { PlanNode } from '../../../../hooks/useQueryPlan';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeNode(overrides: Partial<PlanNode> = {}): PlanNode {
    return {
        'Node Type': 'Seq Scan',
        'Total Cost': 100.0,
        'Startup Cost': 0.0,
        'Plan Rows': 1000,
        'Plan Width': 50,
        ...overrides,
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('PlanTree', () => {
    it('renders "No plan data available" when plan is empty', () => {
        render(<PlanTree plan={[]} />);

        expect(
            screen.getByText('No plan data available'),
        ).toBeInTheDocument();
    });

    it('renders node type for a simple single-node plan', () => {
        const plan = [makeNode({ 'Node Type': 'Index Scan' })];

        render(<PlanTree plan={plan} />);

        expect(screen.getByText('Index Scan')).toBeInTheDocument();
    });

    it('renders relation name in tiles', () => {
        const plan = [makeNode({
            'Relation Name': 'users',
        })];

        render(<PlanTree plan={plan} />);

        expect(screen.getByText('users')).toBeInTheDocument();
    });

    it('shows join type in parentheses when present', () => {
        const plan = [makeNode({
            'Node Type': 'Hash Join',
            'Join Type': 'Inner',
        })];

        render(<PlanTree plan={plan} />);

        expect(
            screen.getByText('Hash Join (Inner)'),
        ).toBeInTheDocument();
    });

    it('renders all node types for a multi-node plan', () => {
        const childA = makeNode({
            'Node Type': 'Index Scan',
            'Total Cost': 10.0,
        });
        const childB = makeNode({
            'Node Type': 'Seq Scan',
            'Total Cost': 20.0,
        });
        const parentNode = makeNode({
            'Node Type': 'Nested Loop',
            Plans: [childA, childB],
        });

        render(<PlanTree plan={[parentNode]} />);

        expect(
            screen.getByText('Nested Loop'),
        ).toBeInTheDocument();
        expect(
            screen.getByText('Index Scan'),
        ).toBeInTheDocument();
        expect(
            screen.getByText('Seq Scan'),
        ).toBeInTheDocument();
    });

    it('opens popover with detail properties on tile click', () => {
        const plan = [makeNode({
            'Node Type': 'Index Scan',
            'Startup Cost': 1.5,
            'Total Cost': 42.75,
            'Plan Rows': 500,
            'Plan Width': 32,
            'Relation Name': 'orders',
            'Filter': '(active = true)',
        })];

        render(<PlanTree plan={plan} />);

        // Click the tile to open the popover.
        const tile = screen.getByRole('button', {
            name: /view details for index scan/i,
        });
        fireEvent.click(tile);

        // The popover should show cost, rows, width, and filter.
        expect(
            screen.getByText('Cost: 1.50..42.75'),
        ).toBeInTheDocument();
        expect(
            screen.getByText('Rows: 500'),
        ).toBeInTheDocument();
        expect(
            screen.getByText('Width: 32'),
        ).toBeInTheDocument();
        expect(
            screen.getByText(/Relation: orders/),
        ).toBeInTheDocument();
        expect(
            screen.getByText(/Filter: \(active = true\)/),
        ).toBeInTheDocument();
    });

    it('renders the SVG overlay', () => {
        const childNode = makeNode({
            'Node Type': 'Index Scan',
            'Total Cost': 10.0,
        });
        const parentNode = makeNode({
            'Node Type': 'Hash Join',
            Plans: [childNode],
        });

        render(<PlanTree plan={[parentNode]} />);

        const svg = screen.getByTestId('plan-tree-svg');
        expect(svg).toBeInTheDocument();
        expect(svg.tagName.toLowerCase()).toBe('svg');
    });

    it('renders schema-qualified relation name in tile', () => {
        const plan = [makeNode({
            'Relation Name': 'users',
            'Schema': 'public',
        })];

        render(<PlanTree plan={plan} />);

        expect(
            screen.getByText('public.users'),
        ).toBeInTheDocument();
    });

    it('shows output columns in popover (truncated to 5)', () => {
        const plan = [makeNode({
            'Node Type': 'Seq Scan',
            'Output': [
                'col1', 'col2', 'col3',
                'col4', 'col5', 'col6',
            ],
        })];

        render(<PlanTree plan={plan} />);

        const tile = screen.getByRole('button', {
            name: /view details for seq scan/i,
        });
        fireEvent.click(tile);

        expect(
            screen.getByText(/Output:/),
        ).toBeInTheDocument();
        expect(
            screen.getByText(/\.\.\./),
        ).toBeInTheDocument();
    });

    it('shows strategy in popover', () => {
        const plan = [makeNode({
            'Node Type': 'HashAggregate',
            'Strategy': 'Hash',
        })];

        render(<PlanTree plan={plan} />);

        const tile = screen.getByRole('button', {
            name: /view details for hashaggregate/i,
        });
        fireEvent.click(tile);

        expect(
            screen.getByText('Strategy: Hash'),
        ).toBeInTheDocument();
    });

    it('shows worker counts in popover', () => {
        const plan = [makeNode({
            'Node Type': 'Gather',
            'Workers Planned': 4,
            'Workers Launched': 3,
        })];

        render(<PlanTree plan={plan} />);

        const tile = screen.getByRole('button', {
            name: /view details for gather/i,
        });
        fireEvent.click(tile);

        expect(
            screen.getByText('Workers: 4 planned, 3 launched'),
        ).toBeInTheDocument();
    });

    it('shows merge cond in popover', () => {
        const plan = [makeNode({
            'Node Type': 'Merge Join',
            'Merge Cond': '(a.id = b.id)',
        })];

        render(<PlanTree plan={plan} />);

        const tile = screen.getByRole('button', {
            name: /view details for merge join/i,
        });
        fireEvent.click(tile);

        expect(
            screen.getByText('Merge Cond: (a.id = b.id)'),
        ).toBeInTheDocument();
    });

    it('shows recheck cond in popover', () => {
        const plan = [makeNode({
            'Node Type': 'Bitmap Heap Scan',
            'Recheck Cond': '(status = true)',
        })];

        render(<PlanTree plan={plan} />);

        const tile = screen.getByRole('button', {
            name: /view details for bitmap heap scan/i,
        });
        fireEvent.click(tile);

        expect(
            screen.getByText('Recheck Cond: (status = true)'),
        ).toBeInTheDocument();
    });
});

describe('layoutPlanTree', () => {
    it('returns empty array for empty plan', () => {
        expect(layoutPlanTree([])).toEqual([]);
    });

    it('positions a single node at the padding offset', () => {
        const plan = [makeNode()];
        const result = layoutPlanTree(plan);

        expect(result).toHaveLength(1);
        expect(result[0].x).toBe(20);
        expect(result[0].y).toBe(20);
        expect(result[0].parentId).toBeNull();
    });

    it('places leaf nodes to the left of parent nodes', () => {
        const child = makeNode({
            'Node Type': 'Seq Scan',
            'Total Cost': 10,
        });
        const root = makeNode({
            'Node Type': 'Sort',
            Plans: [child],
        });
        const result = layoutPlanTree([root]);

        // Child (depth 1) should have a smaller X than root
        // (depth 0) because column = maxDepth - depth.
        const rootLayout = result.find(
            n => n.node['Node Type'] === 'Sort',
        )!;
        const childLayout = result.find(
            n => n.node['Node Type'] === 'Seq Scan',
        )!;

        expect(childLayout.x).toBeLessThan(rootLayout.x);
    });

    it('centers parent on children Y span', () => {
        const childA = makeNode({
            'Node Type': 'Scan A',
            'Total Cost': 5,
        });
        const childB = makeNode({
            'Node Type': 'Scan B',
            'Total Cost': 5,
        });
        const root = makeNode({
            'Node Type': 'Join',
            Plans: [childA, childB],
        });
        const result = layoutPlanTree([root]);

        const rootLayout = result.find(
            n => n.node['Node Type'] === 'Join',
        )!;
        const layoutA = result.find(
            n => n.node['Node Type'] === 'Scan A',
        )!;
        const layoutB = result.find(
            n => n.node['Node Type'] === 'Scan B',
        )!;

        const expectedY = (layoutA.y + layoutB.y) / 2;
        expect(rootLayout.y).toBe(expectedY);
    });
});

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
import { describe, it, expect, vi, beforeEach } from 'vitest';
import QueryPlanPanel from '../QueryPlanPanel';
import { useQueryPlan } from '../../../../hooks/useQueryPlan';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockFetch = vi.fn();

vi.mock('../../../../hooks/useQueryPlan', () => ({
    useQueryPlan: vi.fn(() => ({
        textPlan: null,
        jsonPlan: null,
        loading: false,
        error: null,
        fetch: mockFetch,
    })),
}));

const mockUseQueryPlan = vi.mocked(useQueryPlan);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('QueryPlanPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();

        // The global localStorage mock uses vi.fn() which returns
        // undefined by default. CollapsibleSection checks
        // `stored !== null`, so undefined would incorrectly enter
        // the branch. Return null to let defaultExpanded take
        // effect.
        vi.mocked(localStorage.getItem).mockReturnValue(null);

        mockUseQueryPlan.mockReturnValue({
            textPlan: null,
            jsonPlan: null,
            loading: false,
            error: null,
            notExplainable: null,
            fetch: mockFetch,
        });
    });

    it('renders "Query Plan" section title', () => {
        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        expect(screen.getByText('Query Plan')).toBeInTheDocument();
    });

    it('calls fetch on mount', () => {
        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        expect(mockFetch).toHaveBeenCalledTimes(1);
    });

    it('shows loading spinner when loading with no plan data', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: null,
            jsonPlan: null,
            loading: true,
            error: null,
            notExplainable: null,
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        expect(
            screen.getByLabelText('Loading query plan'),
        ).toBeInTheDocument();
    });

    it('shows error alert when error is set', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: null,
            jsonPlan: null,
            loading: false,
            error: 'Something went wrong',
            notExplainable: null,
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        expect(
            screen.getByText('Something went wrong'),
        ).toBeInTheDocument();
    });

    it('shows friendly message for parameter type errors', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: null,
            jsonPlan: null,
            loading: false,
            error: 'could not determine data type of parameter $1',
            notExplainable: null,
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT * FROM users WHERE id = $1"
            />,
        );

        expect(
            screen.getByText(/PostgreSQL 16\+/),
        ).toBeInTheDocument();
    });

    it('shows a friendly message for non-explainable statements', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: null,
            jsonPlan: null,
            loading: false,
            error: null,
            notExplainable: 'VACUUM',
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="VACUUM"
            />,
        );

        expect(
            screen.getByText(
                /cannot produce a query plan for VACUUM statements/,
            ),
        ).toBeInTheDocument();
        expect(
            screen.queryByRole('tab', { name: 'Visual' }),
        ).not.toBeInTheDocument();
    });

    it('prefers the non-explainable notice over a stale error', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: null,
            jsonPlan: null,
            loading: false,
            error: 'syntax error at or near "VACUUM"',
            notExplainable: '',
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="/* comment only */"
            />,
        );

        expect(
            screen.getByText(
                /cannot produce a query plan for this statement/,
            ),
        ).toBeInTheDocument();
        expect(
            screen.queryByText(/syntax error at or near/),
        ).not.toBeInTheDocument();
    });

    it('shows text plan in monospace when text tab is selected', () => {
        const planText = 'Seq Scan on users (cost=0.00..35.50)';
        mockUseQueryPlan.mockReturnValue({
            textPlan: planText,
            jsonPlan: null,
            loading: false,
            error: null,
            notExplainable: null,
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        // Visual is the default tab (index 0); click Text to switch
        fireEvent.click(screen.getByText('Text'));

        const planElement = screen.getByText(planText);
        expect(planElement).toBeInTheDocument();
        expect(planElement.tagName).toBe('PRE');
    });

    it('shows visual tree when visual tab is selected with jsonPlan', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: 'Seq Scan on users',
            jsonPlan: [{
                'Node Type': 'Seq Scan',
                'Total Cost': 35.5,
                'Startup Cost': 0.0,
                'Plan Rows': 2550,
                'Plan Width': 4,
            }],
            loading: false,
            error: null,
            notExplainable: null,
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        // Visual is the default tab (index 0); renders immediately
        expect(screen.getByText('Seq Scan')).toBeInTheDocument();
    });

    it('shows "Visual plan not available" when jsonPlan is null', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: 'Seq Scan on users',
            jsonPlan: null,
            loading: false,
            error: null,
            notExplainable: null,
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        // Visual is the default tab (index 0); fallback renders immediately
        expect(
            screen.getByText(/Visual plan not available/),
        ).toBeInTheDocument();
    });

    it('shows Visual tab as the default selected tab', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: 'Seq Scan on users',
            jsonPlan: null,
            loading: false,
            error: null,
            notExplainable: null,
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        const visualTab = screen.getByRole('tab', { name: 'Visual' });
        expect(visualTab).toHaveAttribute('aria-selected', 'true');
    });

    it('refresh button calls fetch', () => {
        mockUseQueryPlan.mockReturnValue({
            textPlan: 'Seq Scan on users',
            jsonPlan: null,
            loading: false,
            error: null,
            notExplainable: null,
            fetch: mockFetch,
        });

        render(
            <QueryPlanPanel
                connectionId={1}
                databaseName="testdb"
                queryText="SELECT 1"
            />,
        );

        // Clear the mount call
        mockFetch.mockClear();

        const refreshButton = screen.getByLabelText(
            'Refresh query plan',
        );
        fireEvent.click(refreshButton);

        expect(mockFetch).toHaveBeenCalledTimes(1);
    });
});

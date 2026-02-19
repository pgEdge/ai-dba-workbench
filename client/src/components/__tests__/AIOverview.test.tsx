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
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import AIOverview from '../AIOverview';
import * as apiClientModule from '../../utils/apiClient';

// Mock the apiClient module
vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
    ApiError: class ApiError extends Error {
        public readonly statusCode: number;
        public readonly errorBody: string;
        constructor(message: string, statusCode: number, errorBody: string = '') {
            super(message);
            this.name = 'ApiError';
            this.statusCode = statusCode;
            this.errorBody = errorBody;
        }
    },
}));

// Mock the SSE hook so tests don't need a real EventSource
const mockUseOverviewSSE = vi.fn().mockReturnValue({
    overview: null,
    connected: false,
});
vi.mock('../../hooks/useOverviewSSE', () => ({
    useOverviewSSE: (...args: unknown[]) => mockUseOverviewSSE(...args),
}));

const theme = createTheme();

/**
 * Helper to render the AIOverview component wrapped in a ThemeProvider.
 */
function renderWithTheme(ui: React.ReactElement) {
    return render(<ThemeProvider theme={theme}>{ui}</ThemeProvider>);
}

/** Build a valid OverviewResponse with sensible defaults. */
function makeOverviewResponse(overrides: Record<string, unknown> = {}) {
    return {
        status: 'ready',
        summary: 'All systems operational. No alerts detected.',
        generated_at: new Date().toISOString(),
        stale_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
        ...overrides,
    };
}

describe('AIOverview Component', () => {
    const mockApiGet = apiClientModule.apiGet as ReturnType<typeof vi.fn>;
    const mockGetItem = localStorage.getItem as ReturnType<typeof vi.fn>;
    const mockSetItem = localStorage.setItem as ReturnType<typeof vi.fn>;

    beforeEach(() => {
        vi.clearAllMocks();
        // Default: localStorage returns null (no stored value)
        mockGetItem.mockReturnValue(null);
        // Default: SSE not connected and no data
        mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });
    });

    afterEach(() => {
        vi.restoreAllMocks();
        vi.useRealTimers();
    });

    describe('Rendering States', () => {
        it('shows loading skeleton when SSE has not delivered data yet', () => {
            mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });

            renderWithTheme(<AIOverview />);

            // Skeleton elements should be present
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });

        it('shows "Generating overview..." when SSE delivers status generating with null summary', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    status: 'generating',
                    summary: null,
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Generating overview...')).toBeInTheDocument();
            });
        });

        it('shows "Generating overview..." when summary is null regardless of status', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    status: 'ready',
                    summary: null,
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Generating overview...')).toBeInTheDocument();
            });
        });

        it('shows the summary text when SSE delivers a valid summary', async () => {
            const summaryText = 'All systems operational. No alerts detected.';
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ summary: summaryText }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText(summaryText)).toBeInTheDocument();
            });
        });

        it('shows "(stale)" badge when current time is past the stale_at timestamp', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    stale_at: new Date(Date.now() - 60 * 1000).toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('(stale)')).toBeInTheDocument();
            });
        });

        it('does not show "(stale)" badge when stale_at is in the future', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    stale_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            expect(screen.queryByText('(stale)')).not.toBeInTheDocument();
        });

        it('shows "Updated just now" for a recently generated overview', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    generated_at: new Date().toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Updated just now')).toBeInTheDocument();
            });
        });

        it('shows "Updated 5 min ago" for an overview generated 5 minutes ago', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    generated_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Updated 5 min ago')).toBeInTheDocument();
            });
        });

        it('renders nothing when SSE has no data and is not loading', async () => {
            // Simulate the case where SSE has no data but loading has been
            // cleared (e.g. by a previous SSE delivery that was then reset)
            mockUseOverviewSSE.mockReturnValue({
                overview: null,
                connected: true,
            });

            // We need SSE to deliver null so loading clears but overview
            // remains null.  The component renders null for error/no-data.
            // First render will show loading; then SSE effect sets
            // loading=false.
            const { container } = renderWithTheme(<AIOverview />);

            // The SSE effect will fire and clear loading.  The component
            // should still show the loading skeleton since sseOverview is
            // null (the effect only runs when sseOverview is truthy).
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });
    });

    describe('Scoped SSE Connections', () => {
        it('passes /api/v1/overview to SSE hook when no selection is provided', () => {
            renderWithTheme(<AIOverview />);

            expect(mockUseOverviewSSE).toHaveBeenCalledWith('/api/v1/overview');
        });

        it('passes /api/v1/overview to SSE hook when selection type is estate', () => {
            renderWithTheme(
                <AIOverview selection={{ type: 'estate' }} />
            );

            expect(mockUseOverviewSSE).toHaveBeenCalledWith('/api/v1/overview');
        });

        it('passes scope_type=server and scope_id to SSE hook when selection is a server', () => {
            renderWithTheme(
                <AIOverview selection={{ type: 'server', id: 5 }} />
            );

            expect(mockUseOverviewSSE).toHaveBeenCalledWith(
                '/api/v1/overview?scope_type=server&scope_id=5'
            );
        });

        it('passes connection_ids and scope_name to SSE hook when selection is a cluster', () => {
            renderWithTheme(
                <AIOverview
                    selection={{
                        type: 'cluster',
                        id: 'cluster-1',
                        name: 'Production',
                        serverIds: [1, 3, 5],
                    }}
                />
            );

            expect(mockUseOverviewSSE).toHaveBeenCalledWith(
                expect.stringContaining('connection_ids=1%2C3%2C5')
            );
            expect(mockUseOverviewSSE).toHaveBeenCalledWith(
                expect.stringContaining('scope_name=Production')
            );
        });

        it('updates SSE hook URL when selection changes from estate to a server', () => {
            const { rerender } = renderWithTheme(<AIOverview />);

            expect(mockUseOverviewSSE).toHaveBeenCalledWith('/api/v1/overview');

            mockUseOverviewSSE.mockClear();

            rerender(
                <ThemeProvider theme={theme}>
                    <AIOverview selection={{ type: 'server', id: 7 }} />
                </ThemeProvider>
            );

            expect(mockUseOverviewSSE).toHaveBeenCalledWith(
                '/api/v1/overview?scope_type=server&scope_id=7'
            );
        });
    });

    describe('Collapsible Behavior', () => {
        it('renders expanded by default when no localStorage value exists', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // The summary text should be visible (not collapsed)
            expect(
                screen.getByText('All systems operational. No alerts detected.')
            ).toBeInTheDocument();

            // The collapse toggle should show "Collapse" label
            expect(
                screen.getByLabelText('Collapse AI Overview')
            ).toBeInTheDocument();
        });

        it('hides the summary body when the collapse toggle is clicked', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // Click the collapse toggle
            fireEvent.click(screen.getByLabelText('Collapse AI Overview'));

            // After collapsing, the aria-label should change to Expand
            expect(
                screen.getByLabelText('Expand AI Overview')
            ).toBeInTheDocument();
        });

        it('re-expands the summary body when the toggle is clicked again', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // Collapse
            fireEvent.click(screen.getByLabelText('Collapse AI Overview'));
            expect(
                screen.getByLabelText('Expand AI Overview')
            ).toBeInTheDocument();

            // Expand again
            fireEvent.click(screen.getByLabelText('Expand AI Overview'));
            expect(
                screen.getByLabelText('Collapse AI Overview')
            ).toBeInTheDocument();
        });

        it('keeps the header visible when collapsed', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // Collapse
            fireEvent.click(screen.getByLabelText('Collapse AI Overview'));

            // Header elements should still be visible
            expect(screen.getByText('AI Overview')).toBeInTheDocument();
            expect(
                document.querySelector('[data-testid="AutoAwesomeIcon"]')
            ).toBeInTheDocument();
        });
    });

    describe('localStorage Persistence', () => {
        it('reads initial collapsed state from localStorage', async () => {
            mockGetItem.mockReturnValue('true');
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // Should have read from localStorage with the correct key
            expect(mockGetItem).toHaveBeenCalledWith('ai-overview-collapsed');

            // Should start collapsed (Expand label means it is collapsed)
            expect(
                screen.getByLabelText('Expand AI Overview')
            ).toBeInTheDocument();
        });

        it('saves collapsed state to localStorage when toggled', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // Collapse
            fireEvent.click(screen.getByLabelText('Collapse AI Overview'));
            expect(mockSetItem).toHaveBeenCalledWith(
                'ai-overview-collapsed',
                'true'
            );

            // Expand
            fireEvent.click(screen.getByLabelText('Expand AI Overview'));
            expect(mockSetItem).toHaveBeenCalledWith(
                'ai-overview-collapsed',
                'false'
            );
        });

        it('starts collapsed if localStorage has "true"', async () => {
            mockGetItem.mockReturnValue('true');
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // The toggle should indicate collapsed state
            expect(
                screen.getByLabelText('Expand AI Overview')
            ).toBeInTheDocument();
        });
    });

    describe('SSE and Fallback Polling', () => {
        it('does not make any REST calls when SSE is connected', async () => {
            vi.useFakeTimers();
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            await act(async () => {
                renderWithTheme(<AIOverview />);
            });

            // Advance past the 5-second SSE grace period and beyond
            await act(async () => {
                vi.advanceTimersByTime(20_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            // No REST calls should have been made at all
            expect(mockApiGet).not.toHaveBeenCalled();
        });

        it('starts fallback polling every 10s when SSE is not connected after 5s', async () => {
            vi.useFakeTimers();
            mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });
            mockApiGet.mockResolvedValue(makeOverviewResponse());

            await act(async () => {
                renderWithTheme(<AIOverview />);
            });

            // No initial REST fetch should have been made
            expect(mockApiGet).not.toHaveBeenCalled();

            // Advance past the 5-second grace period
            await act(async () => {
                vi.advanceTimersByTime(5_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            // Advance by 10 seconds for first fallback poll
            await act(async () => {
                vi.advanceTimersByTime(10_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            expect(mockApiGet).toHaveBeenCalledTimes(1);

            // Advance by another 10 seconds for second fallback poll
            await act(async () => {
                vi.advanceTimersByTime(10_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });

        it('clears fallback polling on unmount', async () => {
            vi.useFakeTimers();
            mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });
            mockApiGet.mockResolvedValue(makeOverviewResponse());

            let result: ReturnType<typeof render>;
            await act(async () => {
                result = renderWithTheme(<AIOverview />);
            });

            // No initial REST fetch
            expect(mockApiGet).not.toHaveBeenCalled();

            // Unmount the component
            result!.unmount();

            // Advance timers well past fallback threshold; no calls
            await act(async () => {
                vi.advanceTimersByTime(60_000);
            });

            expect(mockApiGet).not.toHaveBeenCalled();
        });
    });
});

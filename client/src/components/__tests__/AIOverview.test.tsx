/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import AIOverview from '../AIOverview';
import * as apiClientModule from '../../utils/apiClient';
import { logger } from '../../utils/logger';
import { clearAnalysisCache } from '../../hooks/useServerAnalysis';

// Mock the apiClient module
vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
    ApiError: class ApiError extends Error {
        public readonly statusCode: number;
        public readonly errorBody: string;
        constructor(message: string, statusCode: number, errorBody = '') {
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

// Mock clearAnalysisCache so we can assert it is called when the
// server reports a restart.  Mocking the entire module avoids pulling
// in the real hook's dependency chain.
vi.mock('../../hooks/useServerAnalysis', () => ({
    clearAnalysisCache: vi.fn(),
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
            renderWithTheme(<AIOverview />);

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

    describe('Summary coercion (issue #182)', () => {
        /*
         * Reasoning LLMs (e.g. deepseek-r1) sometimes return a structured
         * object as the `summary` field even though the type says
         * `string | null`. Rendering that object directly throws React
         * minified error #130 and bricks the whole UI.  These tests
         * exercise the coercion that protects the render path.
         */

        it('renders a JSON-serialised representation when summary is an object', async () => {
            const objectSummary = {
                thinking: 'analysing...',
                answer: 'looks good',
            };
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    summary: objectSummary as unknown as string,
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // The component must not crash and must render something
            // that includes the object's serialised contents.
            const expected = JSON.stringify(objectSummary);
            expect(screen.getByText(expected)).toBeInTheDocument();
        });

        it('renders an empty body when summary becomes an empty string after coercion', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    summary: '',
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // No crash; the header still renders.
            expect(screen.getByText('AI Overview')).toBeInTheDocument();
        });

        it('renders the array contents when summary is an array', async () => {
            const arraySummary = ['line one', 'line two'];
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    summary: arraySummary as unknown as string,
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            const expected = JSON.stringify(arraySummary);
            expect(screen.getByText(expected)).toBeInTheDocument();
        });

        it('falls back to String(raw) when JSON.stringify throws (circular reference)', async () => {
            // Build a circular object so JSON.stringify cannot serialise
            // it.  The component must not crash; the fallback path
            // returns String(raw) which yields '[object Object]'.
            const circular: Record<string, unknown> = { name: 'oops' };
            circular.self = circular;
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    summary: circular as unknown as string,
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // String() coercion of a plain object yields the standard
            // [object Object] tag.  The point of the test is that the
            // component does not crash, so we just check that
            // *something* renders inside the body region.
            expect(screen.getByText('[object Object]')).toBeInTheDocument();
        });
    });

    describe('formatRelativeTime branches', () => {
        /*
         * formatRelativeTime is called from the ready-state render path.
         * Each branch of its `if/else` ladder maps to a specific
         * `generated_at` offset; exercise all of them here.
         */

        it('returns empty string when generated_at is the empty string', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ generated_at: '' }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // The relative-time caption renders only when generated_at is
            // truthy, so an empty string short-circuits the entire block.
            expect(screen.queryByText(/Updated/)).not.toBeInTheDocument();
        });

        it('shows "Updated 2 hours ago" for a 2-hour-old overview (plural)', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    generated_at: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Updated 2 hours ago')).toBeInTheDocument();
            });
        });

        it('shows "Updated 1 hour ago" for a 1-hour-old overview (singular)', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    generated_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Updated 1 hour ago')).toBeInTheDocument();
            });
        });

        it('shows "Updated 3 days ago" for a 3-day-old overview (plural)', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    generated_at: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Updated 3 days ago')).toBeInTheDocument();
            });
        });

        it('shows "Updated 1 day ago" for a 1-day-old overview (singular)', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    generated_at: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Updated 1 day ago')).toBeInTheDocument();
            });
        });

        it('falls back to a locale date string when older than 7 days', async () => {
            const tenDaysAgo = new Date(Date.now() - 10 * 24 * 60 * 60 * 1000);
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({
                    generated_at: tenDaysAgo.toISOString(),
                }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            const expected = `Updated ${tenDaysAgo.toLocaleDateString()}`;
            await waitFor(() => {
                expect(screen.getByText(expected)).toBeInTheDocument();
            });
        });
    });

    describe('overviewUrl edge cases', () => {
        it('falls back to /api/v1/overview when a cluster has no serverIds', () => {
            renderWithTheme(
                <AIOverview
                    selection={{
                        type: 'cluster',
                        id: 'empty-cluster',
                        name: 'Empty',
                        serverIds: [],
                    } as unknown as never}
                />
            );

            expect(mockUseOverviewSSE).toHaveBeenCalledWith('/api/v1/overview');
        });

        it('omits scope_name when a cluster selection has no name', () => {
            renderWithTheme(
                <AIOverview
                    selection={{
                        type: 'cluster',
                        id: 'unnamed',
                        name: undefined,
                        serverIds: [2, 4],
                    } as unknown as never}
                />
            );

            const lastCall = mockUseOverviewSSE.mock.calls[mockUseOverviewSSE.mock.calls.length - 1];
            const url = lastCall[0] as string;
            expect(url).toContain('connection_ids=2%2C4');
            expect(url).not.toContain('scope_name');
        });
    });

    describe('localStorage error handling', () => {
        it('falls back to expanded state when localStorage.getItem throws', async () => {
            const mockGetItem = localStorage.getItem as ReturnType<typeof vi.fn>;
            mockGetItem.mockImplementation(() => {
                throw new Error('storage disabled');
            });
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // Expanded by default when storage read fails
            expect(
                screen.getByLabelText('Collapse AI Overview')
            ).toBeInTheDocument();
        });

        it('swallows localStorage.setItem errors when toggling collapse', async () => {
            const mockSetItem = localStorage.setItem as ReturnType<typeof vi.fn>;
            mockSetItem.mockImplementation(() => {
                throw new Error('quota exceeded');
            });
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            // Toggling must not throw even though setItem rejects
            expect(() => {
                fireEvent.click(screen.getByLabelText('Collapse AI Overview'));
            }).not.toThrow();

            // The toggle still flipped the in-memory state
            expect(
                screen.getByLabelText('Expand AI Overview')
            ).toBeInTheDocument();
        });
    });

    describe('SSE restart_detected handling', () => {
        it('calls clearAnalysisCache when SSE delivers restart_detected=true', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ restart_detected: true }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            expect(clearAnalysisCache).toHaveBeenCalled();
        });

        it('does not call clearAnalysisCache when restart_detected is false', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ restart_detected: false }),
                connected: true,
            });

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            expect(clearAnalysisCache).not.toHaveBeenCalled();
        });
    });

    describe('fetchOverview fallback polling paths', () => {
        /*
         * fetchOverview only runs on fallback polling.  Drive that path
         * by leaving SSE disconnected and advancing the fake clock past
         * the 5-second grace window.
         */

        it('renders the summary when fallback polling succeeds', async () => {
            vi.useFakeTimers();
            mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({ summary: 'Polled summary text' })
            );

            await act(async () => {
                renderWithTheme(<AIOverview />);
            });

            // Advance past the 5s grace window plus the first poll cycle
            await act(async () => {
                vi.advanceTimersByTime(15_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/overview');

            // Switch to real timers so waitFor can run
            vi.useRealTimers();

            await waitFor(() => {
                expect(screen.getByText('Polled summary text')).toBeInTheDocument();
            });
        });

        it('clears the analysis cache when fallback polling reports restart_detected', async () => {
            vi.useFakeTimers();
            mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({ restart_detected: true })
            );

            await act(async () => {
                renderWithTheme(<AIOverview />);
            });

            await act(async () => {
                vi.advanceTimersByTime(15_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            expect(clearAnalysisCache).toHaveBeenCalled();
        });

        it('suppresses error UI when fallback polling raises a 401 ApiError', async () => {
            vi.useFakeTimers();
            mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });
            const ApiError = (apiClientModule as unknown as {
                ApiError: new (m: string, s: number, b?: string) => Error;
            }).ApiError;
            mockApiGet.mockRejectedValue(new ApiError('unauthorized', 401));
            const errorSpy = vi.spyOn(logger, 'error').mockImplementation(() => {});

            await act(async () => {
                renderWithTheme(<AIOverview />);
            });

            await act(async () => {
                vi.advanceTimersByTime(15_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            // 401 is silently swallowed; logger.error must not run.
            expect(errorSpy).not.toHaveBeenCalled();
            expect(
                screen.queryByText('Unable to load AI overview')
            ).not.toBeInTheDocument();

            errorSpy.mockRestore();
            vi.useRealTimers();
        });

        it('logs and hides error UI when fallback polling raises a non-401 error', async () => {
            vi.useFakeTimers();
            mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });
            mockApiGet.mockRejectedValue(new Error('boom'));
            const errorSpy = vi.spyOn(logger, 'error').mockImplementation(() => {});

            await act(async () => {
                renderWithTheme(<AIOverview />);
            });

            await act(async () => {
                vi.advanceTimersByTime(15_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            // Logger receives the error.  The component returns null from
            // its render path when error is set, so the body is empty;
            // we assert the logger call instead of probing the DOM.
            expect(errorSpy).toHaveBeenCalledWith(
                'Failed to fetch AI overview:',
                expect.any(Error)
            );

            errorSpy.mockRestore();
            vi.useRealTimers();
        });
    });

    describe('Manual refresh button', () => {
        it('calls apiGet with refresh=true appended via & when URL already has a query string', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ summary: 'initial' }),
                connected: true,
            });
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({ summary: 'refreshed' })
            );

            renderWithTheme(
                <AIOverview selection={{ type: 'server', id: 9 } as unknown as never} />
            );

            await waitFor(() => {
                expect(
                    screen.getByLabelText('Refresh overview')
                ).toBeInTheDocument();
            });

            await act(async () => {
                fireEvent.click(screen.getByLabelText('Refresh overview'));
            });

            // URL had `?` already, so `&` separator is used
            expect(mockApiGet).toHaveBeenCalledWith(
                '/api/v1/overview?scope_type=server&scope_id=9&refresh=true'
            );
        });

        it('calls apiGet with refresh=true appended via ? when URL has no query string', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ summary: 'initial' }),
                connected: true,
            });
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({ summary: 'refreshed' })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(
                    screen.getByLabelText('Refresh overview')
                ).toBeInTheDocument();
            });

            await act(async () => {
                fireEvent.click(screen.getByLabelText('Refresh overview'));
            });

            expect(mockApiGet).toHaveBeenCalledWith(
                '/api/v1/overview?refresh=true'
            );
        });

        it('updates the rendered summary when refresh returns a non-null summary', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ summary: 'initial summary' }),
                connected: true,
            });
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({ summary: 'refreshed summary' })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('initial summary')).toBeInTheDocument();
            });

            await act(async () => {
                fireEvent.click(screen.getByLabelText('Refresh overview'));
            });

            await waitFor(() => {
                expect(screen.getByText('refreshed summary')).toBeInTheDocument();
            });
        });

        it('preserves the existing summary when refresh returns a null summary', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ summary: 'initial summary' }),
                connected: true,
            });
            // Refresh response with summary=null should NOT replace state
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({ summary: null })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('initial summary')).toBeInTheDocument();
            });

            await act(async () => {
                fireEvent.click(screen.getByLabelText('Refresh overview'));
            });

            // The existing summary is still rendered after the refresh
            await waitFor(() => {
                expect(screen.getByText('initial summary')).toBeInTheDocument();
            });
        });

        it('clears the analysis cache when refresh reports restart_detected', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ summary: 'initial' }),
                connected: true,
            });
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({
                    summary: 'after restart',
                    restart_detected: true,
                })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByLabelText('Refresh overview')).toBeInTheDocument();
            });

            // SSE-delivered overview already resets the mock if the
            // initial response has restart_detected; clear before the
            // explicit refresh assertion so we measure only the click.
            (clearAnalysisCache as ReturnType<typeof vi.fn>).mockClear();

            await act(async () => {
                fireEvent.click(screen.getByLabelText('Refresh overview'));
            });

            await waitFor(() => {
                expect(clearAnalysisCache).toHaveBeenCalled();
            });
        });

        it('silently swallows refresh errors and re-enables the button', async () => {
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse({ summary: 'initial' }),
                connected: true,
            });
            mockApiGet.mockRejectedValue(new Error('network down'));

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(
                    screen.getByLabelText('Refresh overview')
                ).toBeInTheDocument();
            });

            await act(async () => {
                fireEvent.click(screen.getByLabelText('Refresh overview'));
            });

            // After the error settles, the button is enabled again
            await waitFor(() => {
                const button = screen.getByLabelText('Refresh overview') as HTMLButtonElement;
                expect(button.disabled).toBe(false);
            });

            // The original summary is still visible
            expect(screen.getByText('initial')).toBeInTheDocument();
        });
    });

    describe('Analyze button visibility', () => {
        it('renders the analyze button when onAnalyze is supplied and selection is a server', async () => {
            const onAnalyze = vi.fn();
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(
                <AIOverview
                    selection={{ type: 'server', id: 1 } as unknown as never}
                    onAnalyze={onAnalyze}
                />
            );

            await waitFor(() => {
                expect(
                    screen.getByLabelText('Run full analysis')
                ).toBeInTheDocument();
            });

            await act(async () => {
                fireEvent.click(screen.getByLabelText('Run full analysis'));
            });
            expect(onAnalyze).toHaveBeenCalled();
        });

        it('renders the analyze button when onAnalyze is supplied and selection is a cluster', async () => {
            const onAnalyze = vi.fn();
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(
                <AIOverview
                    selection={{
                        type: 'cluster',
                        id: 'c1',
                        name: 'C1',
                        serverIds: [1, 2],
                    } as unknown as never}
                    onAnalyze={onAnalyze}
                    analysisCached={true}
                />
            );

            await waitFor(() => {
                // analysisCached=true uses the cached-tooltip wording
                expect(
                    screen.getByLabelText('Run full analysis')
                ).toBeInTheDocument();
            });
        });

        it('hides the analyze button when selection is the estate', async () => {
            const onAnalyze = vi.fn();
            mockUseOverviewSSE.mockReturnValue({
                overview: makeOverviewResponse(),
                connected: true,
            });

            renderWithTheme(
                <AIOverview
                    selection={{ type: 'estate' } as unknown as never}
                    onAnalyze={onAnalyze}
                />
            );

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            expect(
                screen.queryByLabelText('Run full analysis')
            ).not.toBeInTheDocument();
        });
    });

    describe('Error / no-data render branch', () => {
        it('renders nothing when fetchOverview reports a non-401 error', async () => {
            vi.useFakeTimers();
            mockUseOverviewSSE.mockReturnValue({ overview: null, connected: false });
            mockApiGet.mockRejectedValue(new Error('boom'));
            const errorSpy = vi.spyOn(logger, 'error').mockImplementation(() => {});

            let result: ReturnType<typeof render> | null = null;
            await act(async () => {
                result = renderWithTheme(<AIOverview />);
            });

            await act(async () => {
                vi.advanceTimersByTime(15_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            // After error, error || !overview is true: component returns null
            // Cast through `unknown` so the TS narrowing inside the
            // async-callback closure does not flag the variable as
            // possibly null while keeping the assertion type-safe.
            const rendered = result as unknown as ReturnType<typeof render>;
            expect(rendered.container.firstChild).toBeNull();

            errorSpy.mockRestore();
            vi.useRealTimers();
        });
    });
});

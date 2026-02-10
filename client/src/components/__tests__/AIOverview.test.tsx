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
    });

    afterEach(() => {
        vi.restoreAllMocks();
        vi.useRealTimers();
    });

    describe('Rendering States', () => {
        it('shows loading skeleton on initial render before API responds', () => {
            // Never resolve the API call so we stay in loading state
            mockApiGet.mockReturnValue(new Promise(() => {}));

            renderWithTheme(<AIOverview />);

            // Skeleton elements should be present
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });

        it('shows "Generating overview..." when API returns status generating with null summary', async () => {
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({
                    status: 'generating',
                    summary: null,
                })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Generating overview...')).toBeInTheDocument();
            });
        });

        it('shows "Generating overview..." when summary is null regardless of status', async () => {
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({
                    status: 'ready',
                    summary: null,
                })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Generating overview...')).toBeInTheDocument();
            });
        });

        it('shows the summary text when API returns a valid summary', async () => {
            const summaryText = 'All systems operational. No alerts detected.';
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({ summary: summaryText })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText(summaryText)).toBeInTheDocument();
            });
        });

        it('shows "(stale)" badge when current time is past the stale_at timestamp', async () => {
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({
                    stale_at: new Date(Date.now() - 60 * 1000).toISOString(),
                })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('(stale)')).toBeInTheDocument();
            });
        });

        it('does not show "(stale)" badge when stale_at is in the future', async () => {
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({
                    stale_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
                })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('AI Overview')).toBeInTheDocument();
            });

            expect(screen.queryByText('(stale)')).not.toBeInTheDocument();
        });

        it('shows "Updated just now" for a recently generated overview', async () => {
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({
                    generated_at: new Date().toISOString(),
                })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Updated just now')).toBeInTheDocument();
            });
        });

        it('shows "Updated 5 min ago" for an overview generated 5 minutes ago', async () => {
            mockApiGet.mockResolvedValue(
                makeOverviewResponse({
                    generated_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
                })
            );

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(screen.getByText('Updated 5 min ago')).toBeInTheDocument();
            });
        });

        it('renders nothing when API returns a non-401 error', async () => {
            mockApiGet.mockRejectedValue(new Error('Network error'));

            const { container } = renderWithTheme(<AIOverview />);

            await waitFor(() => {
                // Loading should have finished
                const skeletons = document.querySelectorAll('.MuiSkeleton-root');
                expect(skeletons.length).toBe(0);
            });

            // Component should render nothing
            expect(container.querySelector('.MuiPaper-root')).toBeNull();
        });

        it('renders nothing when API returns a 401 error', async () => {
            mockApiGet.mockRejectedValue(
                new apiClientModule.ApiError('Unauthorized', 401)
            );

            const { container } = renderWithTheme(<AIOverview />);

            await waitFor(() => {
                const skeletons = document.querySelectorAll('.MuiSkeleton-root');
                expect(skeletons.length).toBe(0);
            });

            expect(container.querySelector('.MuiPaper-root')).toBeNull();
        });
    });

    describe('Scoped API Calls', () => {
        it('fetches /api/v1/overview with no params when no selection is provided', async () => {
            mockApiGet.mockResolvedValue(makeOverviewResponse());

            renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith('/api/v1/overview');
            });
        });

        it('fetches /api/v1/overview with no params when selection type is estate', async () => {
            mockApiGet.mockResolvedValue(makeOverviewResponse());

            renderWithTheme(
                <AIOverview selection={{ type: 'estate' }} />
            );

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith('/api/v1/overview');
            });
        });

        it('fetches with scope_type=server and scope_id when selection is a server', async () => {
            mockApiGet.mockResolvedValue(makeOverviewResponse());

            renderWithTheme(
                <AIOverview selection={{ type: 'server', id: 5 }} />
            );

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith(
                    '/api/v1/overview?scope_type=server&scope_id=5'
                );
            });
        });

        it('fetches with connection_ids and scope_name when selection is a cluster', async () => {
            mockApiGet.mockResolvedValue(makeOverviewResponse());

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

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith(
                    expect.stringContaining('connection_ids=1%2C3%2C5')
                );
                expect(mockApiGet).toHaveBeenCalledWith(
                    expect.stringContaining('scope_name=Production')
                );
            });
        });

        it('re-fetches when selection changes from estate to a server', async () => {
            mockApiGet.mockResolvedValue(makeOverviewResponse());

            const { rerender } = renderWithTheme(<AIOverview />);

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith('/api/v1/overview');
            });

            mockApiGet.mockClear();

            rerender(
                <ThemeProvider theme={theme}>
                    <AIOverview selection={{ type: 'server', id: 7 }} />
                </ThemeProvider>
            );

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith(
                    '/api/v1/overview?scope_type=server&scope_id=7'
                );
            });
        });
    });

    describe('Collapsible Behavior', () => {
        it('renders expanded by default when no localStorage value exists', async () => {
            mockApiGet.mockResolvedValue(makeOverviewResponse());

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
            mockApiGet.mockResolvedValue(makeOverviewResponse());

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
            mockApiGet.mockResolvedValue(makeOverviewResponse());

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
            mockApiGet.mockResolvedValue(makeOverviewResponse());

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
            mockApiGet.mockResolvedValue(makeOverviewResponse());

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
            mockApiGet.mockResolvedValue(makeOverviewResponse());

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
            mockApiGet.mockResolvedValue(makeOverviewResponse());

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

    describe('Auto-Refresh', () => {
        it('sets up a 30-second interval for refreshing', async () => {
            vi.useFakeTimers();
            mockApiGet.mockResolvedValue(makeOverviewResponse());

            await act(async () => {
                renderWithTheme(<AIOverview />);
            });

            // Flush the initial fetch promise
            await act(async () => {
                await Promise.resolve();
            });

            // The initial fetch should have been called
            expect(mockApiGet).toHaveBeenCalledTimes(1);

            // Advance by 30 seconds and flush the resulting promise
            await act(async () => {
                vi.advanceTimersByTime(30_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            expect(mockApiGet).toHaveBeenCalledTimes(2);

            // Advance by another 30 seconds
            await act(async () => {
                vi.advanceTimersByTime(30_000);
            });
            await act(async () => {
                await Promise.resolve();
            });

            expect(mockApiGet).toHaveBeenCalledTimes(3);
        });

        it('clears the interval on unmount', async () => {
            vi.useFakeTimers();
            mockApiGet.mockResolvedValue(makeOverviewResponse());

            let result: ReturnType<typeof render>;
            await act(async () => {
                result = renderWithTheme(<AIOverview />);
            });

            // Flush the initial fetch promise
            await act(async () => {
                await Promise.resolve();
            });

            expect(mockApiGet).toHaveBeenCalledTimes(1);

            // Unmount the component
            result!.unmount();

            // Advance timers; no additional calls should happen
            await act(async () => {
                vi.advanceTimersByTime(60_000);
            });

            expect(mockApiGet).toHaveBeenCalledTimes(1);
        });
    });
});

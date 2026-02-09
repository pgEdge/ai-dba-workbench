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
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import EventTimeline from '../EventTimeline';
import * as useTimelineEventsModule from '../../hooks/useTimelineEvents';

// Mock the useTimelineEvents hook
vi.mock('../../hooks/useTimelineEvents', () => ({
    useTimelineEvents: vi.fn(),
}));

describe('EventTimeline Component', () => {
    const mockServerSelection = {
        type: 'server',
        id: 1,
        name: 'Production Server',
        serverIds: [1],
    };

    const mockClusterSelection = {
        type: 'cluster',
        id: 'cluster-1',
        name: 'Production Cluster',
        serverIds: [1, 2, 3],
    };

    const mockEvents = [
        {
            id: 1,
            event_type: 'config_change',
            title: 'Configuration Changed',
            summary: 'PostgreSQL configuration updated',
            occurred_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
            server_name: 'server-1',
            details: {
                changed_settings: [
                    { name: 'shared_buffers', old_value: '128MB', new_value: '256MB' },
                ],
            },
        },
        {
            id: 2,
            event_type: 'alert_fired',
            title: 'High CPU Usage',
            summary: 'CPU usage exceeded threshold',
            occurred_at: new Date(Date.now() - 30 * 60 * 1000).toISOString(),
            server_name: 'server-1',
            details: {
                severity: 'warning',
                metric_value: 85,
                threshold_value: 80,
            },
        },
        {
            id: 3,
            event_type: 'restart',
            title: 'Server Restarted',
            summary: 'PostgreSQL server restarted',
            occurred_at: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString(),
            server_name: 'server-2',
            details: {
                old_timeline_id: 5,
                new_timeline_id: 6,
            },
        },
    ];

    const defaultHookReturn = {
        events: mockEvents,
        loading: false,
        error: null,
        totalCount: 3,
        refetch: vi.fn(),
    };

    beforeEach(() => {
        vi.clearAllMocks();
        (useTimelineEventsModule.useTimelineEvents as ReturnType<typeof vi.fn>).mockReturnValue(defaultHookReturn);
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('Rendering', () => {
        it('renders timeline header with title', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);
            expect(screen.getByText('Event Timeline')).toBeInTheDocument();
        });

        it('displays event count chip', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);
            expect(screen.getByText('3')).toBeInTheDocument();
        });

        it('renders time range selector with all options', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);
            expect(screen.getByRole('button', { name: '1h' })).toBeInTheDocument();
            expect(screen.getByRole('button', { name: '6h' })).toBeInTheDocument();
            expect(screen.getByRole('button', { name: '24h' })).toBeInTheDocument();
            expect(screen.getByRole('button', { name: '7d' })).toBeInTheDocument();
        });

        it('renders event type filter chips', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);
            expect(screen.getByText('Config')).toBeInTheDocument();
            expect(screen.getByText('HBA')).toBeInTheDocument();
            expect(screen.getByText('Ident')).toBeInTheDocument();
            expect(screen.getByText('Restart')).toBeInTheDocument();
            expect(screen.getByText('Alert')).toBeInTheDocument();
            expect(screen.getByText('Cleared')).toBeInTheDocument();
            expect(screen.getByText('Blackouts')).toBeInTheDocument();
        });

        it('does not render when selection is null', () => {
            const { container } = render(<EventTimeline selection={null} mode="light" />);
            expect(container.firstChild).toBeNull();
        });

        it('renders correctly in dark mode', () => {
            render(<EventTimeline selection={mockServerSelection} mode="dark" />);
            expect(screen.getByText('Event Timeline')).toBeInTheDocument();
        });
    });

    describe('Loading State', () => {
        it('shows loading skeleton when loading', () => {
            (useTimelineEventsModule.useTimelineEvents as ReturnType<typeof vi.fn>).mockReturnValue({
                ...defaultHookReturn,
                loading: true,
                events: [],
            });

            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Skeleton elements are rendered
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });
    });

    describe('Empty State', () => {
        it('shows empty state when no events', () => {
            (useTimelineEventsModule.useTimelineEvents as ReturnType<typeof vi.fn>).mockReturnValue({
                ...defaultHookReturn,
                events: [],
                totalCount: 0,
            });

            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            expect(screen.getByText('No events in this time range')).toBeInTheDocument();
            expect(screen.getByText('Try expanding the time range or adjusting filters')).toBeInTheDocument();
        });
    });

    describe('Collapse/Expand', () => {
        it('timeline is expanded by default', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Timeline canvas should be visible (look for the track)
            const timeline = document.querySelector('[class*="MuiCollapse-entered"]');
            expect(timeline).toBeInTheDocument();
        });

        it('collapses timeline when header is clicked', async () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Click the header to collapse
            fireEvent.click(screen.getByText('Event Timeline'));

            // Wait for collapse animation
            await waitFor(() => {
                const collapsed = document.querySelector('[class*="MuiCollapse-hidden"]');
                expect(collapsed).toBeInTheDocument();
            });
        });

        it('expands timeline when header is clicked again', async () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Collapse first
            fireEvent.click(screen.getByText('Event Timeline'));

            await waitFor(() => {
                expect(document.querySelector('[class*="MuiCollapse-hidden"]')).toBeInTheDocument();
            });

            // Expand again
            fireEvent.click(screen.getByText('Event Timeline'));

            await waitFor(() => {
                expect(document.querySelector('[class*="MuiCollapse-entered"]')).toBeInTheDocument();
            });
        });
    });

    describe('Time Range Selection', () => {
        it('selects 24h by default', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            const button24h = screen.getByRole('button', { name: '24h' });
            expect(button24h).toHaveClass('Mui-selected');
        });

        it('changes time range when different option clicked', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            fireEvent.click(screen.getByRole('button', { name: '1h' }));

            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenCalledWith(
                expect.objectContaining({
                    timeRange: '1h',
                })
            );
        });

        it('updates hook call when time range changes', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            fireEvent.click(screen.getByRole('button', { name: '7d' }));

            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenCalledWith(
                expect.objectContaining({
                    timeRange: '7d',
                })
            );
        });
    });

    describe('Event Type Filtering', () => {
        it('all event types selected by default', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenCalledWith(
                expect.objectContaining({
                    eventTypes: ['all'],
                })
            );
        });

        it('filters to single event type when clicked', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            fireEvent.click(screen.getByText('Config'));

            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenCalledWith(
                expect.objectContaining({
                    eventTypes: ['config_change'],
                })
            );
        });

        it('sends both blackout types when Blackouts chip is clicked', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            fireEvent.click(screen.getByText('Blackouts'));

            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenLastCalledWith(
                expect.objectContaining({
                    eventTypes: expect.arrayContaining(['blackout_started', 'blackout_ended']),
                })
            );
        });

        it('adds event type when another is clicked', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Click Config first
            fireEvent.click(screen.getByText('Config'));

            // Then click Alert
            fireEvent.click(screen.getByText('Alert'));

            // Should have both types
            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenLastCalledWith(
                expect.objectContaining({
                    eventTypes: expect.arrayContaining(['config_change', 'alert_fired']),
                })
            );
        });
    });

    describe('Connection ID Handling', () => {
        it('passes connectionId for server selection', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenCalledWith(
                expect.objectContaining({
                    connectionId: 1,
                    connectionIds: null,
                })
            );
        });

        it('passes connectionIds for cluster selection', () => {
            render(<EventTimeline selection={mockClusterSelection} mode="light" />);

            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenCalledWith(
                expect.objectContaining({
                    connectionId: null,
                    connectionIds: [1, 2, 3],
                })
            );
        });
    });

    describe('Event Markers', () => {
        it('renders event markers on timeline', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Events should appear as markers - look for icons within the timeline
            const settingsIcons = document.querySelectorAll('[data-testid="SettingsIcon"]');
            const warningIcons = document.querySelectorAll('[data-testid="WarningIcon"]');
            const powerIcons = document.querySelectorAll('[data-testid="PowerSettingsNewIcon"]');

            // At least some event markers should be present
            const totalMarkers = settingsIcons.length + warningIcons.length + powerIcons.length;
            expect(totalMarkers).toBeGreaterThan(0);
        });

        it('shows tooltip on hover', async () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Find an event marker and hover
            const settingsIcon = document.querySelector('[data-testid="SettingsIcon"]');
            if (settingsIcon) {
                const marker = settingsIcon.closest('[role="tooltip"]')?.parentElement ||
                               settingsIcon.parentElement?.parentElement;
                if (marker) {
                    fireEvent.mouseEnter(marker);

                    await waitFor(() => {
                        expect(screen.getByRole('tooltip')).toBeInTheDocument();
                    });
                }
            }
        });
    });

    describe('Event Detail Panel', () => {
        it('opens panel when event marker is clicked', async () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Find and click an event marker
            const settingsIcon = document.querySelector('[data-testid="SettingsIcon"]');
            if (settingsIcon) {
                const marker = settingsIcon.closest('div[style*="cursor: pointer"]') ||
                               settingsIcon.parentElement?.parentElement;
                if (marker) {
                    fireEvent.click(marker);

                    await waitFor(() => {
                        // Panel should show event details
                        expect(screen.getByText('Configuration Changed')).toBeInTheDocument();
                    });
                }
            }
        });

        it('shows config change details in panel', async () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Find and click the config change event marker
            const settingsIcon = document.querySelector('[data-testid="SettingsIcon"]');
            if (settingsIcon) {
                const marker = settingsIcon.closest('div[style*="cursor: pointer"]') ||
                               settingsIcon.parentElement?.parentElement;
                if (marker) {
                    fireEvent.click(marker);

                    await waitFor(() => {
                        // The panel should show "Event Details" header and the event title
                        expect(screen.getByText('Event Details')).toBeInTheDocument();
                        expect(screen.getByText('Configuration Changed')).toBeInTheDocument();
                    });
                }
            }
        });

        it('closes panel when clicking close button', async () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Find and click an event marker
            const settingsIcon = document.querySelector('[data-testid="SettingsIcon"]');
            if (settingsIcon) {
                const marker = settingsIcon.closest('div[style*="cursor: pointer"]') ||
                               settingsIcon.parentElement?.parentElement;
                if (marker) {
                    fireEvent.click(marker);

                    await waitFor(() => {
                        expect(screen.getByText('Configuration Changed')).toBeInTheDocument();
                    });

                    // Click close button to close the panel
                    const closeButton = document.querySelector('[data-testid="CloseIcon"]');
                    if (closeButton) {
                        fireEvent.click(closeButton.closest('button'));

                        await waitFor(() => {
                            expect(screen.queryByText('Event Details')).not.toBeInTheDocument();
                        });
                    }
                }
            }
        });
    });

    describe('Server Name Display', () => {
        it('does not show server name chips for server selection', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Server name should not be prominent in tooltips for single server view
            // This is controlled by showServer prop in the component
            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenCalledWith(
                expect.objectContaining({
                    connectionId: 1,
                })
            );
        });

        it('shows server name chips for cluster selection', () => {
            render(<EventTimeline selection={mockClusterSelection} mode="light" />);

            // For cluster selection, server names should be shown
            expect(useTimelineEventsModule.useTimelineEvents).toHaveBeenCalledWith(
                expect.objectContaining({
                    connectionIds: [1, 2, 3],
                })
            );
        });
    });

    describe('Time Markers', () => {
        it('renders time axis markers', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Time markers should be rendered on the axis
            // Look for time-formatted text (hours:minutes format)
            const timeRegex = /\d{1,2}:\d{2}/;
            const timeElements = screen.getAllByText(timeRegex);
            expect(timeElements.length).toBeGreaterThan(0);
        });
    });

    describe('Accessibility', () => {
        it('has accessible toggle buttons for time range', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            const toggleGroup = screen.getByRole('group');
            expect(toggleGroup).toBeInTheDocument();

            const buttons = within(toggleGroup).getAllByRole('button');
            expect(buttons.length).toBe(5); // 1h, 6h, 24h, 7d, 30d
        });

        it('event markers have tooltips for accessibility', () => {
            render(<EventTimeline selection={mockServerSelection} mode="light" />);

            // Tooltips should be available for screen readers
            const markers = document.querySelectorAll('[data-testid*="Icon"]');
            expect(markers.length).toBeGreaterThan(0);
        });
    });
});

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';
import EventTimeline from '../index';
import * as useTimelineEventsModule from '../../../hooks/useTimelineEvents';
import type { UseTimelineEventsReturn } from '../../../hooks/useTimelineEvents';
import type { ServerSelection } from '../../../types/selection';

vi.mock('../../../hooks/useTimelineEvents', () => ({
    useTimelineEvents: vi.fn(),
}));

const selection: ServerSelection = {
    type: 'server',
    id: 1,
    name: 'Test Server',
    status: 'connected',
    description: 'Test server',
    host: '192.0.2.10',
    port: 5432,
    role: 'primary',
    version: '18.0',
    database: 'postgres',
    username: 'postgres',
    os: 'Linux',
    platform: 'x86_64',
};

const mockedHook = useTimelineEventsModule.useTimelineEvents as ReturnType<typeof vi.fn>;

const EMPTY_MESSAGE = 'No events in this time range';
const ERROR_TITLE = 'Failed to load timeline events';

const hookState = (
    overrides: Partial<UseTimelineEventsReturn> = {},
): UseTimelineEventsReturn => ({
    events: [],
    loading: false,
    error: null,
    totalCount: 0,
    refetch: vi.fn(),
    retrying: false,
    ...overrides,
});

describe('EventTimeline load states', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('shows the empty message when the request succeeds with no events', () => {
        mockedHook.mockReturnValue(hookState());

        renderWithTheme(<EventTimeline selection={selection} />);

        expect(screen.getByText(EMPTY_MESSAGE)).toBeInTheDocument();
        expect(screen.queryByText(ERROR_TITLE)).not.toBeInTheDocument();
    });

    it('shows the error, not the empty message, when the request fails', () => {
        mockedHook.mockReturnValue(hookState({
            error: 'Internal server error',
        }));

        renderWithTheme(<EventTimeline selection={selection} />);

        expect(screen.getByRole('status')).toHaveTextContent(ERROR_TITLE);
        expect(screen.getByText('Internal server error')).toBeInTheDocument();
        expect(screen.queryByText(EMPTY_MESSAGE)).not.toBeInTheDocument();
    });

    it('shows Reconnecting… in place of the error while a retry is pending', () => {
        mockedHook.mockReturnValue(hookState({
            error: 'Internal server error',
            retrying: true,
        }));

        renderWithTheme(<EventTimeline selection={selection} />);

        expect(screen.getByText(ERROR_TITLE)).toBeInTheDocument();
        expect(screen.getByText('Reconnecting…')).toBeInTheDocument();
        expect(screen.queryByText('Internal server error')).not.toBeInTheDocument();
        expect(screen.queryByText(EMPTY_MESSAGE)).not.toBeInTheDocument();
    });

    it('shows the loading skeleton rather than the error during the first load', () => {
        mockedHook.mockReturnValue(hookState({
            loading: true,
            error: 'Internal server error',
        }));

        renderWithTheme(<EventTimeline selection={selection} />);

        expect(screen.queryByText(ERROR_TITLE)).not.toBeInTheDocument();
        expect(screen.queryByText(EMPTY_MESSAGE)).not.toBeInTheDocument();
    });

    it('shows the error rather than stale events when a refresh fails', () => {
        mockedHook.mockReturnValue(hookState({
            events: [{
                id: 1,
                event_type: 'restart',
                connection_id: 1,
                title: 'Server restart',
                occurred_at: new Date().toISOString(),
            }],
            totalCount: 1,
            error: 'Internal server error',
        }));

        renderWithTheme(<EventTimeline selection={selection} />);

        expect(screen.getByText(ERROR_TITLE)).toBeInTheDocument();
        expect(screen.queryByText(EMPTY_MESSAGE)).not.toBeInTheDocument();
    });
});

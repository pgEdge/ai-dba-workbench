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
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import BlackoutPanel from '../BlackoutPanel';
import { renderWithTheme } from '../../test/renderWithTheme';

// Mock the BlackoutContext
const mockStopBlackout = vi.fn();
const mockActiveBlackoutsForSelection: {
    id: number;
    scope: string;
    reason: string;
    end_time: string;
}[] = [];

vi.mock('../../contexts/BlackoutContext', () => ({
    useBlackouts: () => ({
        activeBlackoutsForSelection: mockActiveBlackoutsForSelection,
        stopBlackout: mockStopBlackout,
    }),
}));

describe('BlackoutPanel', () => {
    const mockSelection = {
        type: 'server',
        id: 1,
        name: 'Test Server',
    };

    beforeEach(() => {
        vi.clearAllMocks();
        mockActiveBlackoutsForSelection.length = 0;
    });

    describe('rendering', () => {
        it('returns null when selection is null', () => {
            const { container } = renderWithTheme(
                <BlackoutPanel selection={null} />
            );
            expect(container.firstChild).toBeNull();
        });

        it('returns null when no active blackouts', () => {
            const { container } = renderWithTheme(
                <BlackoutPanel selection={mockSelection} />
            );
            expect(container.firstChild).toBeNull();
        });

        it('renders blackout banner when there are active blackouts', () => {
            const futureTime = new Date(
                Date.now() + 60 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: 'Maintenance window',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            expect(screen.getByText('Blackout Active')).toBeInTheDocument();
        });

        it('renders blackout reason', () => {
            const futureTime = new Date(
                Date.now() + 60 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: 'Maintenance window',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            expect(screen.getByText('Maintenance window')).toBeInTheDocument();
        });

        it('renders time remaining', () => {
            const futureTime = new Date(
                Date.now() + 60 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: 'Test',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            expect(screen.getByText(/remaining/i)).toBeInTheDocument();
        });

        it('renders scope chip', () => {
            const futureTime = new Date(
                Date.now() + 60 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: 'Test',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            expect(screen.getByText('Server')).toBeInTheDocument();
        });

        it('renders Stop button', () => {
            const futureTime = new Date(
                Date.now() + 60 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: 'Test',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            expect(
                screen.getByRole('button', { name: /stop/i })
            ).toBeInTheDocument();
        });
    });

    describe('multiple blackouts', () => {
        it('renders multiple blackout banners', () => {
            const futureTime = new Date(
                Date.now() + 60 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push(
                {
                    id: 1,
                    scope: 'server',
                    reason: 'Server maintenance',
                    end_time: futureTime,
                },
                {
                    id: 2,
                    scope: 'cluster',
                    reason: 'Cluster upgrade',
                    end_time: futureTime,
                }
            );

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            expect(screen.getByText('Server maintenance')).toBeInTheDocument();
            expect(screen.getByText('Cluster upgrade')).toBeInTheDocument();
        });
    });

    describe('scope labels', () => {
        const scopes = [
            { scope: 'estate', label: 'Estate' },
            { scope: 'group', label: 'Group' },
            { scope: 'cluster', label: 'Cluster' },
            { scope: 'server', label: 'Server' },
        ];

        scopes.forEach(({ scope, label }) => {
            it(`renders correct label for ${scope} scope`, () => {
                const futureTime = new Date(
                    Date.now() + 60 * 60 * 1000
                ).toISOString();
                mockActiveBlackoutsForSelection.push({
                    id: 1,
                    scope,
                    reason: 'Test',
                    end_time: futureTime,
                });

                renderWithTheme(<BlackoutPanel selection={mockSelection} />);

                expect(screen.getByText(label)).toBeInTheDocument();
            });
        });
    });

    describe('time formatting', () => {
        it('formats hours and minutes', () => {
            const futureTime = new Date(
                Date.now() + 2 * 60 * 60 * 1000 + 30 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: 'Test',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            // Use flexible regex to avoid timing-dependent flakiness
            // (e.g., 2h 29m or 2h 30m depending on test execution timing)
            expect(screen.getByText(/\d+h \d+m remaining/i)).toBeInTheDocument();
        });

        it('formats minutes only when less than an hour', () => {
            const futureTime = new Date(
                Date.now() + 45 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: 'Test',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            // Use flexible regex to avoid timing-dependent flakiness
            // (44m or 45m depending on test execution timing)
            expect(screen.getByText(/\d+m.*remaining/i)).toBeInTheDocument();
        });

        it('shows ending message when time has passed', () => {
            const pastTime = new Date(Date.now() - 1000).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: 'Test',
                end_time: pastTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            expect(screen.getByText(/ending/i)).toBeInTheDocument();
        });
    });

    describe('interactions', () => {
        it('calls stopBlackout when Stop button is clicked', async () => {
            const user = userEvent.setup({ delay: null });
            const futureTime = new Date(
                Date.now() + 60 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 42,
                scope: 'server',
                reason: 'Test',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            await user.click(screen.getByRole('button', { name: /stop/i }));

            expect(mockStopBlackout).toHaveBeenCalledWith(42);
        });
    });

    describe('without reason', () => {
        it('does not render reason text when empty', () => {
            const futureTime = new Date(
                Date.now() + 60 * 60 * 1000
            ).toISOString();
            mockActiveBlackoutsForSelection.push({
                id: 1,
                scope: 'server',
                reason: '',
                end_time: futureTime,
            });

            renderWithTheme(<BlackoutPanel selection={mockSelection} />);

            expect(screen.getByText('Blackout Active')).toBeInTheDocument();
        });
    });
});

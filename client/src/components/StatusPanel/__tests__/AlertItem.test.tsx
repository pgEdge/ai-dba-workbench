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
import { render, screen } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import { describe, it, expect } from 'vitest';
import AlertItem from '../AlertItem';
import GroupedAlertItem from '../GroupedAlertItem';
import { createPgedgeTheme } from '../../../theme/pgedgeTheme';

// Use the real project theme so custom palette branches (custom.status,
// etc.) that AlertItem/GroupedAlertItem read from are present.
const testTheme = createPgedgeTheme('dark');

const renderWithTheme = (ui: React.ReactElement) =>
    render(<ThemeProvider theme={testTheme}>{ui}</ThemeProvider>);

const baseAlert = {
    id: 1,
    severity: 'warning',
    title: 'High CPU Usage',
    description: 'CPU usage exceeded threshold',
    time: '5 min ago',
    server: 'server-1',
    alertType: 'threshold',
};

describe('AlertItem last updated display', () => {
    it('does not render "Last updated" when lastUpdated matches triggeredAt', () => {
        const triggeredAt = '2026-04-20T12:00:00Z';
        renderWithTheme(
            <AlertItem
                alert={{
                    ...baseAlert,
                    triggeredAt,
                    lastUpdated: triggeredAt,
                    lastUpdatedTime: '5 min ago',
                }}
            />
        );
        expect(screen.queryByText(/Last updated/)).not.toBeInTheDocument();
    });

    it('does not render "Last updated" when lastUpdated is absent', () => {
        renderWithTheme(
            <AlertItem
                alert={{
                    ...baseAlert,
                    triggeredAt: '2026-04-20T12:00:00Z',
                }}
            />
        );
        expect(screen.queryByText(/Last updated/)).not.toBeInTheDocument();
    });

    it('renders "Last updated" when lastUpdated differs from triggeredAt', () => {
        renderWithTheme(
            <AlertItem
                alert={{
                    ...baseAlert,
                    triggeredAt: '2026-04-20T12:00:00Z',
                    lastUpdated: '2026-04-20T12:30:00Z',
                    lastUpdatedTime: '1 min ago',
                }}
            />
        );
        expect(
            screen.getByText((content) =>
                content.startsWith('Last updated') && content.includes('1 min ago')
            )
        ).toBeInTheDocument();
    });

    it('treats sub-second differences as the same timestamp', () => {
        renderWithTheme(
            <AlertItem
                alert={{
                    ...baseAlert,
                    triggeredAt: '2026-04-20T12:00:00.000Z',
                    lastUpdated: '2026-04-20T12:00:00.500Z',
                    lastUpdatedTime: '5 min ago',
                }}
            />
        );
        expect(screen.queryByText(/Last updated/)).not.toBeInTheDocument();
    });
});

describe('GroupedAlertItem last updated display', () => {
    const makeGroupedAlerts = (overrides: Record<string, unknown>[]) =>
        overrides.map((o, idx) => ({
            ...baseAlert,
            id: idx + 1,
            ...o,
        }));

    it('renders "Last updated" on instances whose lastUpdated differs from triggeredAt', () => {
        const alerts = makeGroupedAlerts([
            {
                triggeredAt: '2026-04-20T12:00:00Z',
                lastUpdated: '2026-04-20T12:30:00Z',
                lastUpdatedTime: '1 min ago',
            },
            {
                triggeredAt: '2026-04-20T12:00:00Z',
                lastUpdated: '2026-04-20T12:00:00Z',
                lastUpdatedTime: '5 min ago',
            },
        ]);
        renderWithTheme(
            <GroupedAlertItem title="High CPU Usage" alerts={alerts} />
        );
        const matches = screen.queryAllByText((content) =>
            content.startsWith('Last updated')
        );
        expect(matches).toHaveLength(1);
        expect(matches[0].textContent).toContain('1 min ago');
    });

    it('omits "Last updated" when all instances have matching timestamps', () => {
        const alerts = makeGroupedAlerts([
            {
                triggeredAt: '2026-04-20T12:00:00Z',
                lastUpdated: '2026-04-20T12:00:00Z',
                lastUpdatedTime: '5 min ago',
            },
            {
                triggeredAt: '2026-04-20T11:55:00Z',
            },
        ]);
        renderWithTheme(
            <GroupedAlertItem title="High CPU Usage" alerts={alerts} />
        );
        expect(screen.queryByText(/Last updated/)).not.toBeInTheDocument();
    });
});

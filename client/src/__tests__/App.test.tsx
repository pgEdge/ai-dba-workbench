/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Verifies that the application root installs the date-time pickers'
 * LocalizationProvider. Without it, any picker mounted anywhere in the
 * tree (for example the custom time range popover, issue #345) throws
 * "Can not find the date and time pickers localization context" the
 * moment it renders.
 *
 * The heavyweight children are stubbed so the assertions stay about the
 * root provider chain and the layout wiring rather than about the panels,
 * each of which has its own test suite.
 */

import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { DateTimePicker } from '@mui/x-date-pickers/DateTimePicker';

const mockApiGet = vi.fn();

vi.mock('../utils/apiClient', () => ({
    ApiError: class ApiError extends Error {},
    apiGet: (url: string) => mockApiGet(url) as unknown,
    apiPost: vi.fn().mockResolvedValue({}),
    apiPut: vi.fn().mockResolvedValue({}),
    apiPatch: vi.fn().mockResolvedValue({}),
    apiDelete: vi.fn().mockResolvedValue({}),
    apiFetch: vi.fn(),
    onDisconnect: vi.fn(() => () => { /* no-op unsubscribe */ }),
    resetConnectionHealth: vi.fn(),
}));

vi.mock('../utils/logger', () => ({
    logger: {
        error: vi.fn(),
        warn: vi.fn(),
        info: vi.fn(),
        debug: vi.fn(),
    },
}));

/*
 * The Login screen stands in as the picker host: it is the first thing
 * App renders once the session check reports no user, so mounting a
 * picker there exercises the real provider chain.
 */
vi.mock('../components/Login', () => ({
    default: () => <DateTimePicker label="Probe" />,
}));

vi.mock('../components/Header', () => ({
    default: ({ onToggleTheme }: { onToggleTheme: () => void }) => (
        <button type="button" onClick={onToggleTheme}>toggle theme</button>
    ),
}));

vi.mock('../components/ClusterNavigator', () => ({
    default: () => <div>navigator</div>,
}));

vi.mock('../components/StatusPanel', () => ({
    default: () => <div>status panel</div>,
}));

vi.mock('../components/ChatPanel', () => ({
    default: () => <div>chat panel</div>,
}));

vi.mock('../components/ChatPanel/ChatFAB', () => ({
    default: ({ onClick }: { onClick: () => void }) => (
        <button type="button" onClick={onClick}>chat fab</button>
    ),
}));

vi.mock('../components/ConnectionLostOverlay', () => ({
    default: () => null,
}));

import App from '../App';

/** Minimal responses for every endpoint the provider tree calls. */
const authenticatedResponses: Record<string, unknown> = {
    '/api/v1/user/info': {
        authenticated: true,
        username: 'test_user',
        is_superuser: false,
        admin_permissions: [],
    },
    '/api/v1/capabilities': { ai_enabled: true },
    '/api/v1/alerts/counts': {},
    '/api/v1/blackouts': { blackouts: [] },
    '/api/v1/blackout-schedules': { schedules: [] },
};

const respondAuthenticated = (url: string): Promise<unknown> => {
    const match = Object.keys(authenticatedResponses).find(
        (key) => url.startsWith(key),
    );
    return Promise.resolve(
        match === undefined ? {} : authenticatedResponses[match],
    );
};

describe('App', () => {
    beforeEach(() => {
        vi.mocked(window.localStorage.getItem).mockReturnValue(null);
        mockApiGet.mockReset();
    });

    it('lets a date-time picker render anywhere in the tree', async () => {
        mockApiGet.mockRejectedValue(new Error('no session'));

        render(<App />);

        expect(await screen.findByLabelText('Probe')).toBeInTheDocument();
    });

    it('shows a loading indicator whilst the session check runs', () => {
        mockApiGet.mockReturnValue(new Promise(() => { /* never settles */ }));

        render(<App />);

        expect(
            screen.getByLabelText('Loading application'),
        ).toBeInTheDocument();
    });

    it('renders the main layout for an authenticated user', async () => {
        mockApiGet.mockImplementation(respondAuthenticated);

        render(<App />);

        expect(await screen.findByText('navigator')).toBeInTheDocument();
        expect(screen.getByText('status panel')).toBeInTheDocument();
        // The AI capabilities call resolves after the first paint, so the
        // chat FAB (shown whilst the panel is closed) arrives late.
        expect(await screen.findByText('chat fab')).toBeInTheDocument();
    });

    it('opens the chat panel from the floating action button', async () => {
        mockApiGet.mockImplementation(respondAuthenticated);

        render(<App />);

        fireEvent.click(await screen.findByText('chat fab'));

        expect(await screen.findByText('chat panel')).toBeInTheDocument();
        expect(screen.queryByText('chat fab')).not.toBeInTheDocument();
    });

    it('persists a theme toggle to localStorage', async () => {
        mockApiGet.mockImplementation(respondAuthenticated);

        render(<App />);

        fireEvent.click(await screen.findByText('toggle theme'));

        await waitFor(() => {
            expect(window.localStorage.setItem).toHaveBeenCalledWith(
                'theme-mode',
                'dark',
            );
        });
    });

    it('restores a saved dark theme preference', async () => {
        vi.mocked(window.localStorage.getItem).mockReturnValue('dark');
        mockApiGet.mockImplementation(respondAuthenticated);

        render(<App />);

        await screen.findByText('navigator');

        expect(window.localStorage.setItem).toHaveBeenCalledWith(
            'theme-mode',
            'dark',
        );
    });
});

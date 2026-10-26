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
import { screen, fireEvent, within, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import AlertItem from '../AlertItem';
import { AuthProvider } from '../../../contexts/AuthContext';
import { useAuth } from '../../../contexts/useAuth';
import { renderWithTheme } from '../../../test/renderWithTheme';
import { clearAlertReasoningCache } from '../../../hooks/useAlertReasoning';
import type { TransformedAlert } from '../types';

vi.mock('../../../utils/logger', () => ({
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
}));

const LABEL = 'Why was this flagged?';

const anomalyAlert: TransformedAlert = {
    id: 42,
    severity: 'warning',
    title: 'Connection count anomaly',
    time: '2 min ago',
    server: 'server-1',
    connectionId: 7,
    alertType: 'anomaly',
};

const jsonResponse = (body: unknown) => ({
    ok: true,
    status: 200,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
});

// A fake server holding one session cookie: login sets the user, logout
// clears it, and the reasoning endpoint answers per user, as an
// authorisation check that differed between users would.
let sessionUser: string | null = null;
const reasoningRequests: string[] = [];
const fakeFetch = vi.fn((url: string, init?: RequestInit) => {
    if (url.includes('/api/v1/auth/login')) {
        sessionUser = (JSON.parse(String(init?.body)) as { username: string }).username;
        return Promise.resolve(jsonResponse({ success: true }));
    }
    if (url.includes('/api/v1/auth/logout')) {
        sessionUser = null;
        return Promise.resolve(jsonResponse({ success: true }));
    }
    if (url.includes('/api/v1/user/info')) {
        return Promise.resolve(jsonResponse(
            sessionUser ? { authenticated: true, username: sessionUser } : { authenticated: false },
        ));
    }
    if (url.includes('/api/v1/alerts/reasoning')) {
        reasoningRequests.push(sessionUser ?? 'anonymous');
        return Promise.resolve(jsonResponse({
            alert_id: 42,
            status: 'available',
            source: 'decision',
            reasoning: `Explanation fetched for ${sessionUser ?? 'nobody'}.`,
        }));
    }
    return Promise.reject(new Error(`unexpected request ${url}`));
});

// Harness shows the alert only whilst someone is logged in, and offers
// the login and logout actions of the real AuthProvider.
const Harness: React.FC = () => {
    const { user, login, logout } = useAuth();
    return (
        <>
            <button type="button" onClick={() => { void login('user-b', 'pw'); }}>log in B</button>
            <button type="button" onClick={() => { void logout(); }}>log out</button>
            {user && <p>signed in as {user.username}</p>}
            {user && <AlertItem alert={anomalyAlert} />}
        </>
    );
};

const openPopover = () => {
    fireEvent.click(screen.getByRole('button', { name: LABEL }));
    return screen.getByRole('dialog', { name: 'Detection reasoning' });
};

describe('AlertReasoningButton across a logout', () => {
    beforeEach(() => {
        sessionUser = 'user-a';
        reasoningRequests.length = 0;
        fakeFetch.mockClear();
        clearAlertReasoningCache();
        vi.stubGlobal('fetch', fakeFetch);
    });

    afterEach(() => {
        vi.unstubAllGlobals();
    });

    it('does not show user A\'s reasoning to user B on the same alert', async () => {
        renderWithTheme(<AuthProvider><Harness /></AuthProvider>);
        await screen.findByText('signed in as user-a');

        const first = openPopover();
        expect(await within(first).findByText('Explanation fetched for user-a.')).toBeInTheDocument();
        fireEvent.keyDown(first, { key: 'Escape' });

        await act(async () => {
            fireEvent.click(screen.getByRole('button', { name: 'log out' }));
        });
        expect(screen.queryByText(/signed in as/)).not.toBeInTheDocument();

        await act(async () => {
            fireEvent.click(screen.getByRole('button', { name: 'log in B' }));
        });
        await screen.findByText('signed in as user-b');

        const second = openPopover();
        expect(await within(second).findByText('Explanation fetched for user-b.')).toBeInTheDocument();
        expect(within(second).queryByText('Explanation fetched for user-a.')).not.toBeInTheDocument();
        expect(reasoningRequests).toEqual(['user-a', 'user-b']);
    });
});

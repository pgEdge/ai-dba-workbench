/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import Login from '../Login';
import { AuthProvider } from '../../contexts/AuthContext';
import AuthCapabilitiesContext from '../../contexts/AuthCapabilitiesContext';
import type { AuthCapabilities } from '../../contexts/AuthCapabilitiesContext';

// Mock fetch for API calls
global.fetch = vi.fn() as unknown as typeof fetch;

/*
 * jsdom makes `window.location.assign` read-only, so the navigation
 * helper is mocked rather than the location itself.
 */
vi.mock('../../utils/navigation', () => ({
    navigateTo: vi.fn(),
}));

import { navigateTo } from '../../utils/navigation';
const mockNavigateTo = navigateTo as unknown as ReturnType<typeof vi.fn>;

const LOCAL_ONLY: AuthCapabilities = {
    localEnabled: true,
    oidcEnabled: false,
    oidcLabel: '',
};

const renderLogin = (
    capabilities: AuthCapabilities = LOCAL_ONLY,
    loading = false,
) => {
    return render(
        <AuthCapabilitiesContext.Provider value={{ ...capabilities, loading }}>
            <AuthProvider>
                <Login />
            </AuthProvider>
        </AuthCapabilitiesContext.Provider>
    );
};

describe('Login Component', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        (window.localStorage.getItem as ReturnType<typeof vi.fn>).mockReturnValue(null);
        (window.sessionStorage.getItem as ReturnType<typeof vi.fn>).mockReturnValue(null);
        window.history.replaceState({}, '', '/');
    });

    afterEach(() => {
        vi.restoreAllMocks();
        window.history.replaceState({}, '', '/');
    });

    it('renders login form with correct title', () => {
        renderLogin();
        expect(screen.getByRole('heading', { name: /AI DBA Workbench/i })).toBeInTheDocument();
    });

    it('renders username and password fields', () => {
        renderLogin();
        expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    });

    it('renders sign in button', () => {
        renderLogin();
        expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument();
    });

    it('allows entering username and password', () => {
        renderLogin();

        const usernameInput = screen.getByLabelText(/username/i);
        const passwordInput = screen.getByLabelText(/password/i);

        fireEvent.change(usernameInput, { target: { value: 'testuser' } });
        fireEvent.change(passwordInput, { target: { value: 'testpass' } });

        expect((usernameInput as HTMLInputElement).value).toBe('testuser');
        expect((passwordInput as HTMLInputElement).value).toBe('testpass');
    });

    it('shows error message on login failure', async () => {
        (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
            ok: false,
            json: () => Promise.resolve({ error: 'Invalid credentials' })
        });

        renderLogin();

        const usernameInput = screen.getByLabelText(/username/i);
        const passwordInput = screen.getByLabelText(/password/i);
        const submitButton = screen.getByRole('button', { name: /sign in/i });

        fireEvent.change(usernameInput, { target: { value: 'testuser' } });
        fireEvent.change(passwordInput, { target: { value: 'wrongpass' } });
        fireEvent.click(submitButton);

        await waitFor(() => {
            expect(screen.getByRole('alert')).toBeInTheDocument();
        });
    });

    it('displays copyright footer', () => {
        renderLogin();
        expect(screen.getByText(/2025 - 2026, pgEdge, Inc/i)).toBeInTheDocument();
    });

    describe('authentication capabilities', () => {
        it('renders the password form when local authentication is enabled', async () => {
            renderLogin({ localEnabled: true, oidcEnabled: false, oidcLabel: '' });

            expect(await screen.findByTestId('login-username-input')).toBeInTheDocument();
            expect(screen.queryByTestId('login-oidc-button')).not.toBeInTheDocument();
        });

        it('renders the provider button alongside the password form', async () => {
            renderLogin({
                localEnabled: true,
                oidcEnabled: true,
                oidcLabel: 'Sign in with Okta',
            });

            expect(await screen.findByTestId('login-username-input')).toBeInTheDocument();
            expect(screen.getByTestId('login-oidc-button')).toHaveTextContent(
                'Sign in with Okta',
            );
            expect(screen.getByText('or')).toBeInTheDocument();
        });

        it('falls back to a generic label when the operator configured none', async () => {
            renderLogin({ localEnabled: true, oidcEnabled: true, oidcLabel: '' });

            expect(await screen.findByTestId('login-oidc-button')).toHaveTextContent(
                'Sign in with SSO',
            );
        });

        it('hides the password form when local authentication is disabled', async () => {
            renderLogin({
                localEnabled: false,
                oidcEnabled: true,
                oidcLabel: 'Sign in with Okta',
            });

            expect(await screen.findByTestId('login-oidc-button')).toBeInTheDocument();
            expect(screen.queryByTestId('login-username-input')).not.toBeInTheDocument();
            expect(screen.queryByTestId('login-submit')).not.toBeInTheDocument();
            expect(
                screen.queryByText(/contact your administrator to create an account/i),
            ).not.toBeInTheDocument();
            expect(screen.queryByText('or')).not.toBeInTheDocument();
        });

        it('shows a labelled progress indicator whilst the capabilities load', () => {
            renderLogin(
                { localEnabled: true, oidcEnabled: true, oidcLabel: 'Sign in with Okta' },
                true,
            );

            const busy = screen.getByTestId('login-capabilities-loading');
            expect(busy).toHaveAttribute('aria-busy', 'true');
            expect(
                within(busy).getByLabelText('Loading sign-in options'),
            ).toBeInTheDocument();
            expect(screen.queryByTestId('login-username-input')).not.toBeInTheDocument();
            expect(screen.queryByTestId('login-oidc-button')).not.toBeInTheDocument();
        });

        it('isolates the provider label from the surrounding interface text', async () => {
            // U+202E RIGHT-TO-LEFT OVERRIDE inside the label.
            const label = 'Sign in with ‮Okta';
            renderLogin({
                localEnabled: false,
                oidcEnabled: true,
                oidcLabel: label,
            });

            const button = await screen.findByTestId('login-oidc-button');
            const isolated = button.querySelector('bdi');
            expect(isolated).not.toBeNull();
            expect(isolated).toHaveTextContent(label);
            expect(isolated?.textContent).toBe(label);
        });

        it('sends the browser to the start endpoint when the provider button is clicked', async () => {
            renderLogin({
                localEnabled: false,
                oidcEnabled: true,
                oidcLabel: 'Sign in with Okta',
            });

            await userEvent.click(await screen.findByTestId('login-oidc-button'));

            expect(mockNavigateTo).toHaveBeenCalledWith('/api/v1/auth/oidc/start');
        });
    });

    describe('disconnect warning', () => {
        it('shows and clears a stored disconnect message', async () => {
            const getItem = window.sessionStorage.getItem as ReturnType<typeof vi.fn>;
            getItem.mockReturnValue('Your session was disconnected');

            renderLogin();

            const warning = await screen.findByText('Your session was disconnected');
            expect(warning).toBeInTheDocument();
            expect(window.sessionStorage.removeItem).toHaveBeenCalledWith(
                'disconnectMessage',
            );

            await userEvent.click(screen.getByRole('button', { name: /close/i }));

            expect(
                screen.queryByText('Your session was disconnected'),
            ).not.toBeInTheDocument();
        });
    });

    describe('federated sign-in failures', () => {
        it('shows a message when the provider redirect reports a failure', async () => {
            window.history.replaceState({}, '', '/?login_error=provider');

            renderLogin({
                localEnabled: true,
                oidcEnabled: true,
                oidcLabel: 'Sign in with Okta',
            });

            expect(await screen.findByTestId('login-error')).toHaveTextContent(
                /could not be completed/i,
            );
        });

        it('removes the parameter once the message has been shown', async () => {
            window.history.replaceState({}, '', '/?login_error=provider&next=%2Fx');

            renderLogin();

            await screen.findByTestId('login-error');

            expect(window.location.search).toBe('?next=%2Fx');
        });
    });
});

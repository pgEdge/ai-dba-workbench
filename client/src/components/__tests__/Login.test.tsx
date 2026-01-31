/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import Login from '../Login';
import { AuthProvider } from '../../contexts/AuthContext';

// Mock fetch for API calls
global.fetch = vi.fn() as unknown as typeof fetch;

const renderLogin = () => {
    return render(
        <AuthProvider>
            <Login />
        </AuthProvider>
    );
};

describe('Login Component', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        (window.localStorage.getItem as ReturnType<typeof vi.fn>).mockReturnValue(null);
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
});

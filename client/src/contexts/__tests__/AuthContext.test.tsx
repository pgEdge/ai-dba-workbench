/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - AuthContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, act, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { ADMIN_PERMISSION_WILDCARD, AuthProvider, useAuth } from '../AuthContext';

// Mock fetch
const mockFetch = vi.fn();
global.fetch = mockFetch as unknown as typeof fetch;

// Suppress console.error output during tests that expect errors
const originalConsoleError = console.error;

describe('AuthContext', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        console.error = vi.fn();
    });

    afterEach(() => {
        vi.restoreAllMocks();
        console.error = originalConsoleError;
    });

    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <AuthProvider>{children}</AuthProvider>
    );

    // Helper to create a mock Response with both .json() and .text()
    const mockResponse = (body: Record<string, unknown>, status = 200, ok = true) => ({
        ok,
        status,
        json: () => Promise.resolve(body),
        text: () => Promise.resolve(JSON.stringify(body)),
    });

    // Helper to set up the initial checkAuth response (called on mount)
    const mockCheckAuthUnauthenticated = () => {
        mockFetch.mockResolvedValueOnce(
            mockResponse({ authenticated: false })
        );
    };

    const mockCheckAuthAuthenticated = (options?: {
        username?: string;
        is_superuser?: boolean;
        admin_permissions?: string[];
    }) => {
        mockFetch.mockResolvedValueOnce(
            mockResponse({
                authenticated: true,
                username: options?.username ?? 'testuser',
                is_superuser: options?.is_superuser ?? false,
                admin_permissions: options?.admin_permissions ?? [],
            })
        );
    };

    const mockCheckAuthFailure = () => {
        mockFetch.mockRejectedValueOnce(new Error('Network error'));
    };

    describe('Default state', () => {
        it('starts in a loading state', () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            expect(result.current.loading).toBe(true);
        });

        it('resolves to unauthenticated when no session exists', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.user).toBeNull();
            expect(result.current.adminPermissions).toEqual([]);
            expect(result.current.hasAnyAdminAccess).toBe(false);
        });

        it('provides all expected context properties', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current).toHaveProperty('user');
            expect(result.current).toHaveProperty('loading');
            expect(result.current).toHaveProperty('login');
            expect(result.current).toHaveProperty('logout');
            expect(result.current).toHaveProperty('forceLogout');
            expect(result.current).toHaveProperty('adminPermissions');
            expect(result.current).toHaveProperty('hasPermission');
            expect(result.current).toHaveProperty('hasAnyAdminAccess');
        });
    });

    describe('Loading state during authentication', () => {
        it('shows loading true while checkAuth is in progress', async () => {
            // Use a controlled promise so we can observe the loading state
            let resolveCheckAuth: ((value: unknown) => void) | undefined;
            const controlledPromise = new Promise((resolve) => {
                resolveCheckAuth = resolve;
            });

            mockFetch.mockReturnValueOnce(controlledPromise);

            const { result } = renderHook(() => useAuth(), { wrapper });

            // Loading should be true while awaiting the checkAuth call
            expect(result.current.loading).toBe(true);
            expect(result.current.user).toBeNull();

            // Resolve the promise
            await act(async () => {
                if (resolveCheckAuth) {
                    resolveCheckAuth(
                        mockResponse({ authenticated: false })
                    );
                }
            });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });
        });

        it('sets loading to false even when checkAuth fails', async () => {
            mockCheckAuthFailure();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.user).toBeNull();
        });
    });

    describe('Session persistence (checkAuth on mount)', () => {
        it('restores user state from an existing session', async () => {
            mockCheckAuthAuthenticated({
                username: 'sessionuser',
                is_superuser: false,
                admin_permissions: ['manage_users'],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.user).toEqual({
                authenticated: true,
                username: 'sessionuser',
                isSuperuser: false,
            });
            expect(result.current.adminPermissions).toEqual(['manage_users']);
        });

        it('restores superuser state from an existing session', async () => {
            mockCheckAuthAuthenticated({
                username: 'admin',
                is_superuser: true,
                admin_permissions: ['manage_users', 'manage_probes'],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.user?.isSuperuser).toBe(true);
            expect(result.current.hasAnyAdminAccess).toBe(true);
        });

        it('clears user when session check returns unauthenticated', async () => {
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    authenticated: false,
                    username: '',
                })
            );

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.user).toBeNull();
            expect(result.current.adminPermissions).toEqual([]);
        });

        it('clears user when session check returns a non-OK response', async () => {
            mockFetch.mockResolvedValueOnce(
                mockResponse({}, 401, false)
            );

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.user).toBeNull();
        });

        it('calls user/info endpoint with credentials on mount', async () => {
            mockCheckAuthUnauthenticated();

            renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(mockFetch).toHaveBeenCalledWith(
                    '/api/v1/user/info',
                    expect.objectContaining({
                        method: 'GET',
                        credentials: 'include',
                    })
                );
            });
        });
    });

    describe('Login flow', () => {
        it('logs in successfully and sets user state', async () => {
            // First call: checkAuth on mount (unauthenticated)
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Second call: login POST
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    success: true,
                    expires_at: '2026-03-01T00:00:00Z',
                })
            );

            // Third call: user/info after login
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    authenticated: true,
                    username: 'testuser',
                    is_superuser: false,
                    admin_permissions: ['manage_probes'],
                })
            );

            await act(async () => {
                await result.current.login('testuser', 'password123');
            });

            expect(result.current.user).toEqual({
                authenticated: true,
                username: 'testuser',
                isSuperuser: false,
                expiresAt: '2026-03-01T00:00:00Z',
            });
            expect(result.current.adminPermissions).toEqual(['manage_probes']);
        });

        it('sends correct login request', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Login POST
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    success: true,
                    expires_at: '2026-03-01T00:00:00Z',
                })
            );

            // user/info after login
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    authenticated: true,
                    username: 'testuser',
                    is_superuser: false,
                    admin_permissions: [],
                })
            );

            await act(async () => {
                await result.current.login('testuser', 'password123');
            });

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/auth/login',
                expect.objectContaining({
                    method: 'POST',
                    headers: expect.objectContaining({ 'Content-Type': 'application/json' }),
                    credentials: 'include',
                    body: JSON.stringify({ username: 'testuser', password: 'password123' }),
                })
            );
        });

        it('falls back to username when user/info fetch fails after login', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Login POST succeeds
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    success: true,
                    expires_at: '2026-03-01T00:00:00Z',
                })
            );

            // user/info fails
            mockFetch.mockResolvedValueOnce(
                mockResponse({}, 500, false)
            );

            await act(async () => {
                await result.current.login('fallbackuser', 'password123');
            });

            expect(result.current.user).toEqual({
                authenticated: true,
                username: 'fallbackuser',
                expiresAt: '2026-03-01T00:00:00Z',
            });
            expect(result.current.adminPermissions).toEqual([]);
        });

        it('sets superuser state for admin users', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Login POST
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    success: true,
                    expires_at: '2026-03-01T00:00:00Z',
                })
            );

            // user/info returns superuser
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    authenticated: true,
                    username: 'admin',
                    is_superuser: true,
                    admin_permissions: ['manage_users', 'manage_probes'],
                })
            );

            await act(async () => {
                await result.current.login('admin', 'adminpass');
            });

            expect(result.current.user?.isSuperuser).toBe(true);
            expect(result.current.hasAnyAdminAccess).toBe(true);
        });
    });

    describe('Login failure', () => {
        it('throws an error when the server returns a non-OK response', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Login POST returns 401
            mockFetch.mockResolvedValueOnce(
                mockResponse({ error: 'Invalid credentials' }, 401, false)
            );

            await expect(
                act(async () => {
                    await result.current.login('baduser', 'wrongpass');
                })
            ).rejects.toThrow('Invalid credentials');

            expect(result.current.user).toBeNull();
        });

        it('throws an error when the server returns success=false', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Login POST returns success=false
            mockFetch.mockResolvedValueOnce(
                mockResponse({
                    success: false,
                    message: 'Account locked',
                })
            );

            await expect(
                act(async () => {
                    await result.current.login('lockeduser', 'password123');
                })
            ).rejects.toThrow('Account locked');

            expect(result.current.user).toBeNull();
        });

        it('throws an error when the network request fails', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Login POST throws a network error
            mockFetch.mockRejectedValueOnce(new Error('Network error'));

            await expect(
                act(async () => {
                    await result.current.login('testuser', 'password123');
                })
            ).rejects.toThrow('Network error');

            expect(result.current.user).toBeNull();
        });

        it('provides a default message when error has no message', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            // Login POST returns non-OK with no error field
            mockFetch.mockResolvedValueOnce(
                mockResponse({}, 500, false)
            );

            await expect(
                act(async () => {
                    await result.current.login('testuser', 'password123');
                })
            ).rejects.toThrow();
        });
    });

    describe('Logout flow', () => {
        it('clears user state after logout', async () => {
            // Mount with authenticated session
            mockCheckAuthAuthenticated({ username: 'testuser' });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.user).not.toBeNull();
            });

            // Logout POST (apiPost with no body returns 204-style or JSON)
            mockFetch.mockResolvedValueOnce(
                mockResponse({ success: true })
            );

            await act(async () => {
                await result.current.logout();
            });

            expect(result.current.user).toBeNull();
            expect(result.current.adminPermissions).toEqual([]);
            expect(result.current.hasAnyAdminAccess).toBe(false);
        });

        it('sends logout request to the server', async () => {
            mockCheckAuthAuthenticated({ username: 'testuser' });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.user).not.toBeNull();
            });

            mockFetch.mockResolvedValueOnce(
                mockResponse({ success: true })
            );

            await act(async () => {
                await result.current.logout();
            });

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/v1/auth/logout',
                expect.objectContaining({
                    method: 'POST',
                    credentials: 'include',
                })
            );
        });

        it('clears user state even when server logout fails', async () => {
            mockCheckAuthAuthenticated({ username: 'testuser' });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.user).not.toBeNull();
            });

            // Logout POST fails
            mockFetch.mockRejectedValueOnce(new Error('Server unreachable'));

            await act(async () => {
                await result.current.logout();
            });

            expect(result.current.user).toBeNull();
            expect(result.current.adminPermissions).toEqual([]);
        });
    });

    describe('Force logout', () => {
        it('clears user state immediately', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                admin_permissions: ['manage_users'],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.user).not.toBeNull();
            });

            // forceLogout still sends the logout request
            mockFetch.mockResolvedValueOnce(
                mockResponse({ success: true })
            );

            await act(async () => {
                await result.current.forceLogout();
            });

            expect(result.current.user).toBeNull();
            expect(result.current.adminPermissions).toEqual([]);
        });

        it('clears state even when the logout request fails', async () => {
            mockCheckAuthAuthenticated({ username: 'testuser' });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.user).not.toBeNull();
            });

            // forceLogout request fails
            mockFetch.mockRejectedValueOnce(new Error('Network error'));

            await act(async () => {
                await result.current.forceLogout();
            });

            expect(result.current.user).toBeNull();
            expect(result.current.adminPermissions).toEqual([]);
        });
    });

    describe('Permissions', () => {
        it('hasPermission returns false for regular users without the permission', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                is_superuser: false,
                admin_permissions: ['manage_probes'],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasPermission('manage_users')).toBe(false);
        });

        it('hasPermission returns true for regular users with the permission', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                is_superuser: false,
                admin_permissions: ['manage_probes', 'manage_users'],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasPermission('manage_users')).toBe(true);
        });

        it('hasPermission returns true for superusers regardless of permissions list', async () => {
            mockCheckAuthAuthenticated({
                username: 'admin',
                is_superuser: true,
                admin_permissions: [],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasPermission('manage_users')).toBe(true);
            expect(result.current.hasPermission('manage_probes')).toBe(true);
            expect(result.current.hasPermission('any_permission')).toBe(true);
        });

        it('hasPermission returns true for any admin permission when wildcard is granted', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                is_superuser: false,
                admin_permissions: [ADMIN_PERMISSION_WILDCARD],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasPermission('manage_users')).toBe(true);
            expect(result.current.hasPermission('manage_groups')).toBe(true);
            expect(result.current.hasPermission('manage_permissions')).toBe(true);
            expect(result.current.hasPermission('manage_token_scopes')).toBe(true);
        });

        it('hasPermission returns true for all permissions when wildcard is combined with specific perms', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                is_superuser: false,
                admin_permissions: [ADMIN_PERMISSION_WILDCARD, 'manage_users'],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasPermission('manage_users')).toBe(true);
            expect(result.current.hasPermission('manage_groups')).toBe(true);
            expect(result.current.hasPermission('manage_permissions')).toBe(true);
            expect(result.current.hasPermission('manage_token_scopes')).toBe(true);
        });

        it('hasPermission returns false for any permission when admin_permissions is empty', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                is_superuser: false,
                admin_permissions: [],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasPermission('manage_users')).toBe(false);
            expect(result.current.hasPermission('manage_groups')).toBe(false);
            expect(result.current.hasPermission('manage_permissions')).toBe(false);
            expect(result.current.hasPermission('manage_token_scopes')).toBe(false);
        });

        it('hasPermission does not treat a specific permission as a wildcard (regression guard)', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                is_superuser: false,
                admin_permissions: ['manage_users'],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasPermission('manage_users')).toBe(true);
            expect(result.current.hasPermission('manage_groups')).toBe(false);
            expect(result.current.hasPermission('manage_permissions')).toBe(false);
            expect(result.current.hasPermission('manage_token_scopes')).toBe(false);
        });

        it('hasAnyAdminAccess is true for superusers', async () => {
            mockCheckAuthAuthenticated({
                username: 'admin',
                is_superuser: true,
                admin_permissions: [],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasAnyAdminAccess).toBe(true);
        });

        it('hasAnyAdminAccess is true when user has admin permissions', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                is_superuser: false,
                admin_permissions: ['manage_probes'],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasAnyAdminAccess).toBe(true);
        });

        it('hasAnyAdminAccess is false when user has no admin permissions', async () => {
            mockCheckAuthAuthenticated({
                username: 'testuser',
                is_superuser: false,
                admin_permissions: [],
            });

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasAnyAdminAccess).toBe(false);
        });

        it('hasAnyAdminAccess is false for unauthenticated users', async () => {
            mockCheckAuthUnauthenticated();

            const { result } = renderHook(() => useAuth(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.hasAnyAdminAccess).toBe(false);
        });
    });

    describe('useAuth hook outside provider', () => {
        it('throws an error when used outside AuthProvider', () => {
            expect(() => {
                renderHook(() => useAuth());
            }).toThrow('useAuth must be used within an AuthProvider');
        });
    });
});

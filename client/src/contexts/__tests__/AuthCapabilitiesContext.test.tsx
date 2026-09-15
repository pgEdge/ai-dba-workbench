/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - AuthCapabilitiesContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { renderHook, waitFor, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import { AuthCapabilitiesProvider } from '../AuthCapabilitiesContext';
import { useAuthCapabilities } from '../useAuthCapabilities';

vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
}));

import { apiGet } from '../../utils/apiClient';
const mockApiGet = apiGet as unknown as ReturnType<typeof vi.fn>;

describe('AuthCapabilitiesContext', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <AuthCapabilitiesProvider>{children}</AuthCapabilitiesProvider>
    );

    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('starts in a loading state with the usable defaults', () => {
        mockApiGet.mockReturnValue(new Promise(() => { /* never resolves */ }));

        const { result } = renderHook(() => useAuthCapabilities(), { wrapper });

        expect(result.current.loading).toBe(true);
        expect(result.current.localEnabled).toBe(true);
        expect(result.current.oidcEnabled).toBe(false);
        expect(result.current.oidcLabel).toBe('');
    });

    it('maps the auth block from the capabilities response', async () => {
        mockApiGet.mockResolvedValueOnce({
            ai_enabled: true,
            auth: {
                local_enabled: false,
                oidc_enabled: true,
                oidc_label: 'Sign in with Okta',
            },
        });

        const { result } = renderHook(() => useAuthCapabilities(), { wrapper });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.localEnabled).toBe(false);
        expect(result.current.oidcEnabled).toBe(true);
        expect(result.current.oidcLabel).toBe('Sign in with Okta');
    });

    it('treats a non-boolean oidc_enabled as disabled', async () => {
        mockApiGet.mockResolvedValueOnce({
            auth: { local_enabled: true, oidc_enabled: 'yes', oidc_label: 42 },
        });

        const { result } = renderHook(() => useAuthCapabilities(), { wrapper });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.oidcEnabled).toBe(false);
        expect(result.current.oidcLabel).toBe('');
    });

    it('falls back to local login when the server omits the auth block', async () => {
        mockApiGet.mockResolvedValueOnce({ ai_enabled: false });

        const { result } = renderHook(() => useAuthCapabilities(), { wrapper });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.localEnabled).toBe(true);
        expect(result.current.oidcEnabled).toBe(false);
    });

    it('falls back to local login when the fetch fails', async () => {
        mockApiGet.mockRejectedValueOnce(new Error('Network error'));

        const { result } = renderHook(() => useAuthCapabilities(), { wrapper });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.localEnabled).toBe(true);
        expect(result.current.oidcEnabled).toBe(false);
        expect(result.current.oidcLabel).toBe('');
    });

    it('calls /api/v1/capabilities with an abort signal', async () => {
        mockApiGet.mockResolvedValueOnce({ auth: {} });

        renderHook(() => useAuthCapabilities(), { wrapper });

        await waitFor(() => {
            expect(mockApiGet).toHaveBeenCalledWith('/api/v1/capabilities', {
                signal: expect.any(AbortSignal),
            });
        });
    });

    it('applies the defaults when the request never answers', async () => {
        vi.useFakeTimers();
        try {
            // A promise that never settles stands in for a connection
            // held open by a proxy; `apiGet` sets no timeout of its
            // own, so the provider must impose one.
            mockApiGet.mockReturnValue(new Promise(() => { /* hangs */ }));

            const { result } = renderHook(() => useAuthCapabilities(), {
                wrapper,
            });

            expect(result.current.loading).toBe(true);

            await act(async () => {
                await vi.advanceTimersByTimeAsync(5000);
            });

            expect(result.current.loading).toBe(false);
            expect(result.current.localEnabled).toBe(true);
            expect(result.current.oidcEnabled).toBe(false);

            const signal = mockApiGet.mock.calls[0][1].signal as AbortSignal;
            expect(signal.aborted).toBe(true);
        } finally {
            vi.useRealTimers();
        }
    });

    it('does nothing once the provider has unmounted', async () => {
        vi.useFakeTimers();
        try {
            mockApiGet.mockReturnValue(new Promise(() => { /* hangs */ }));

            const { unmount } = renderHook(() => useAuthCapabilities(), {
                wrapper,
            });

            unmount();

            await act(async () => {
                await vi.advanceTimersByTimeAsync(5000);
            });

            // The request is abandoned on unmount, and the timeout
            // that fires afterwards must not touch React state.
            const signal = mockApiGet.mock.calls[0][1].signal as AbortSignal;
            expect(signal.aborted).toBe(true);
        } finally {
            vi.useRealTimers();
        }
    });

    it('ignores an answer that arrives after the timeout', async () => {
        vi.useFakeTimers();
        try {
            let resolveLate: (value: unknown) => void = () => undefined;
            mockApiGet.mockReturnValue(
                new Promise((resolve) => { resolveLate = resolve; }),
            );

            const { result } = renderHook(() => useAuthCapabilities(), {
                wrapper,
            });

            await act(async () => {
                await vi.advanceTimersByTimeAsync(5000);
            });

            await act(async () => {
                resolveLate({
                    auth: {
                        local_enabled: false,
                        oidc_enabled: true,
                        oidc_label: 'Too late',
                    },
                });
                await Promise.resolve();
            });

            expect(result.current.localEnabled).toBe(true);
            expect(result.current.oidcEnabled).toBe(false);
        } finally {
            vi.useRealTimers();
        }
    });

    it('throws when the hook is used outside the provider', () => {
        expect(() => {
            renderHook(() => useAuthCapabilities());
        }).toThrow(
            'useAuthCapabilities must be used within an AuthCapabilitiesProvider',
        );
    });
});

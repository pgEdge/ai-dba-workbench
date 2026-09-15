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
        // Several tests install a persistent implementation, and
        // `clearAllMocks` only clears the recorded calls.
        mockApiGet.mockReset();
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

    it('offers both methods when the fetch fails', async () => {
        mockApiGet.mockRejectedValue(new Error('Network error'));

        const { result } = renderHook(() => useAuthCapabilities(), { wrapper });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        // Nothing is known about the server, so neither affordance may
        // be hidden: a method that turns out to be disabled costs an
        // error, whilst hiding the working one is a dead end.
        expect(result.current.localEnabled).toBe(true);
        expect(result.current.oidcEnabled).toBe(true);
        expect(result.current.oidcLabel).toBe('');
    });

    it('retries once before giving up', async () => {
        mockApiGet.mockRejectedValue(new Error('Network error'));

        const { result } = renderHook(() => useAuthCapabilities(), { wrapper });

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(mockApiGet).toHaveBeenCalledTimes(2);
    });

    it('takes the answer from a successful retry', async () => {
        mockApiGet
            .mockRejectedValueOnce(new Error('Network error'))
            .mockResolvedValueOnce({
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

        expect(mockApiGet).toHaveBeenCalledTimes(2);
        expect(result.current.localEnabled).toBe(false);
        expect(result.current.oidcLabel).toBe('Sign in with Okta');
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

    it('offers both methods when the request never answers', async () => {
        vi.useFakeTimers();
        try {
            // A promise that never settles stands in for a connection
            // held open by a proxy; `apiGet` sets no timeout of its
            // own, so the provider must impose one.
            mockApiGet.mockImplementation(
                () => new Promise(() => { /* hangs */ }),
            );

            const { result } = renderHook(() => useAuthCapabilities(), {
                wrapper,
            });

            expect(result.current.loading).toBe(true);

            // The first attempt times out and is retried; the second
            // decides it.
            await act(async () => {
                await vi.advanceTimersByTimeAsync(4000);
            });
            expect(result.current.loading).toBe(true);
            expect(mockApiGet).toHaveBeenCalledTimes(2);

            await act(async () => {
                await vi.advanceTimersByTimeAsync(4000);
            });

            expect(result.current.loading).toBe(false);
            expect(result.current.localEnabled).toBe(true);
            expect(result.current.oidcEnabled).toBe(true);

            for (const call of mockApiGet.mock.calls) {
                expect((call[1].signal as AbortSignal).aborted).toBe(true);
            }
        } finally {
            vi.useRealTimers();
        }
    });

    it('does nothing once the provider has unmounted', async () => {
        vi.useFakeTimers();
        try {
            mockApiGet.mockImplementation(
                () => new Promise(() => { /* hangs */ }),
            );

            const { unmount } = renderHook(() => useAuthCapabilities(), {
                wrapper,
            });

            unmount();

            await act(async () => {
                await vi.advanceTimersByTimeAsync(8000);
            });

            // The request is abandoned on unmount, and the timeout
            // that fires afterwards must not touch React state.
            const signal = mockApiGet.mock.calls[0][1].signal as AbortSignal;
            expect(signal.aborted).toBe(true);
        } finally {
            vi.useRealTimers();
        }
    });

    it('does not retry a request that fails after unmount', async () => {
        vi.useFakeTimers();
        try {
            mockApiGet.mockImplementation(
                () => new Promise((_, reject) => {
                    setTimeout(() => { reject(new Error('Network error')); },
                        1000);
                }),
            );

            const { unmount } = renderHook(() => useAuthCapabilities(), {
                wrapper,
            });

            unmount();

            await act(async () => {
                await vi.advanceTimersByTimeAsync(1000);
            });

            // The retry would be pointless work against a provider
            // nobody is watching any more.
            expect(mockApiGet).toHaveBeenCalledTimes(1);
        } finally {
            vi.useRealTimers();
        }
    });

    it('ignores an answer that arrives after the timeout', async () => {
        vi.useFakeTimers();
        try {
            let resolveLate: (value: unknown) => void = () => undefined;
            mockApiGet
                .mockReturnValueOnce(
                    new Promise((resolve) => { resolveLate = resolve; }),
                )
                .mockImplementation(
                    () => new Promise(() => { /* hangs */ }),
                );

            const { result } = renderHook(() => useAuthCapabilities(), {
                wrapper,
            });

            // Both attempts time out, so the fallback is in place
            // before the first request finally answers.
            await act(async () => {
                await vi.advanceTimersByTimeAsync(8000);
            });

            expect(result.current.loading).toBe(false);

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
            expect(result.current.oidcEnabled).toBe(true);
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

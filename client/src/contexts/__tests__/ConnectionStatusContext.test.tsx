/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ConnectionStatusContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

import { AuthProvider } from '../AuthContext';
import {
    ConnectionStatusProvider,
    useConnectionStatus,
} from '../ConnectionStatusContext';

// Capture the disconnect listener registered by the provider so tests
// can invoke it directly.
let capturedListener: ((reason: string) => void) | null = null;
const unsubscribeSpy = vi.fn();
const resetConnectionHealthSpy = vi.fn();

vi.mock('../../utils/apiClient', () => ({
    onDisconnect: (listener: (reason: string) => void) => {
        capturedListener = listener;
        return unsubscribeSpy;
    },
    resetConnectionHealth: () => resetConnectionHealthSpy(),
}));

// Mock AuthContext so ConnectionStatusProvider can pull forceLogout.
const forceLogoutMock = vi.fn();
vi.mock('../AuthContext', () => ({
    AuthProvider: ({ children }: { children: React.ReactNode }) => (
        <>{children}</>
    ),
    useAuth: () => ({
        forceLogout: forceLogoutMock,
    }),
}));

describe('ConnectionStatusContext', () => {
    beforeEach(() => {
        capturedListener = null;
        unsubscribeSpy.mockClear();
        resetConnectionHealthSpy.mockClear();
        forceLogoutMock.mockClear();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <AuthProvider>
            <ConnectionStatusProvider>{children}</ConnectionStatusProvider>
        </AuthProvider>
    );

    describe('default state', () => {
        it('starts connected with empty reason', () => {
            const { result } = renderHook(() => useConnectionStatus(), { wrapper });

            expect(result.current.disconnected).toBe(false);
            expect(result.current.reason).toBe('');
            expect(typeof result.current.reconnect).toBe('function');
        });

        it('registers a disconnect listener on mount', () => {
            renderHook(() => useConnectionStatus(), { wrapper });
            expect(capturedListener).not.toBeNull();
        });

        it('unsubscribes the listener on unmount', () => {
            const { unmount } = renderHook(() => useConnectionStatus(), { wrapper });
            unmount();
            expect(unsubscribeSpy).toHaveBeenCalled();
        });
    });

    describe('disconnect events', () => {
        it('transitions to disconnected state with the reason', () => {
            const { result } = renderHook(() => useConnectionStatus(), { wrapper });

            act(() => {
                capturedListener?.('auth');
            });

            expect(result.current.disconnected).toBe(true);
            expect(result.current.reason).toBe('auth');
        });

        it('captures different disconnect reasons', () => {
            const { result } = renderHook(() => useConnectionStatus(), { wrapper });

            act(() => {
                capturedListener?.('network');
            });

            expect(result.current.reason).toBe('network');

            // Reset state and check another reason.
            act(() => {
                result.current.reconnect();
            });

            act(() => {
                capturedListener?.('server');
            });

            expect(result.current.reason).toBe('server');
        });
    });

    describe('reconnect', () => {
        it('resets disconnected state and calls forceLogout', () => {
            const { result } = renderHook(() => useConnectionStatus(), { wrapper });

            act(() => {
                capturedListener?.('auth');
            });

            expect(result.current.disconnected).toBe(true);

            act(() => {
                result.current.reconnect();
            });

            expect(result.current.disconnected).toBe(false);
            expect(result.current.reason).toBe('');
            expect(resetConnectionHealthSpy).toHaveBeenCalled();
            expect(forceLogoutMock).toHaveBeenCalled();
        });
    });

    describe('hook outside provider', () => {
        it('throws when used outside provider', () => {
            const originalError = console.error;
            console.error = vi.fn();
            try {
                expect(() => {
                    renderHook(() => useConnectionStatus());
                }).toThrow(
                    'useConnectionStatus must be used within a ConnectionStatusProvider',
                );
            } finally {
                console.error = originalError;
            }
        });
    });
});

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - AICapabilitiesContext Tests
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
    AICapabilitiesProvider,
    useAICapabilities,
} from '../AICapabilitiesContext';

// Mock apiClient.apiGet
vi.mock('../../utils/apiClient', () => ({
    apiGet: vi.fn(),
}));

import { apiGet } from '../../utils/apiClient';
const mockApiGet = apiGet as unknown as ReturnType<typeof vi.fn>;

describe('AICapabilitiesContext', () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
        <AICapabilitiesProvider>{children}</AICapabilitiesProvider>
    );

    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    describe('initial state and fetch', () => {
        it('starts in a loading state with default values', () => {
            // Never-resolving promise to keep in loading state
            mockApiGet.mockReturnValue(new Promise(() => {}));

            const { result } = renderHook(() => useAICapabilities(), { wrapper });

            expect(result.current.loading).toBe(true);
            expect(result.current.aiEnabled).toBe(false);
            expect(result.current.maxIterations).toBe(50);
        });

        it('sets aiEnabled and maxIterations from API response', async () => {
            mockApiGet.mockResolvedValueOnce({
                ai_enabled: true,
                max_iterations: 25,
            });

            const { result } = renderHook(() => useAICapabilities(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.aiEnabled).toBe(true);
            expect(result.current.maxIterations).toBe(25);
        });

        it('defaults maxIterations to 50 when not provided', async () => {
            mockApiGet.mockResolvedValueOnce({
                ai_enabled: true,
            });

            const { result } = renderHook(() => useAICapabilities(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.maxIterations).toBe(50);
        });

        it('sets aiEnabled to false when not strictly true', async () => {
            mockApiGet.mockResolvedValueOnce({
                ai_enabled: 'yes',
            });

            const { result } = renderHook(() => useAICapabilities(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.aiEnabled).toBe(false);
        });

        it('sets aiEnabled to false when fetch fails', async () => {
            mockApiGet.mockRejectedValueOnce(new Error('Network error'));

            const { result } = renderHook(() => useAICapabilities(), { wrapper });

            await waitFor(() => {
                expect(result.current.loading).toBe(false);
            });

            expect(result.current.aiEnabled).toBe(false);
        });

        it('calls /api/v1/capabilities', async () => {
            mockApiGet.mockResolvedValueOnce({ ai_enabled: false });

            renderHook(() => useAICapabilities(), { wrapper });

            await waitFor(() => {
                expect(mockApiGet).toHaveBeenCalledWith('/api/v1/capabilities');
            });
        });
    });

    describe('useAICapabilities hook outside provider', () => {
        it('throws when used outside provider', () => {
            const originalError = console.error;
            console.error = vi.fn();
            try {
                expect(() => {
                    renderHook(() => useAICapabilities());
                }).toThrow('useAICapabilities must be used within an AICapabilitiesProvider');
            } finally {
                console.error = originalError;
            }
        });
    });
});

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useOverviewSSE, type OverviewResponse } from '../useOverviewSSE';

// ---------------------------------------------------------------------------
// Mock EventSource
// ---------------------------------------------------------------------------

type EventSourceListener = (event: MessageEvent) => void;

class MockEventSource {
    static instances: MockEventSource[] = [];

    url: string;
    withCredentials: boolean;
    onopen: (() => void) | null = null;
    onerror: (() => void) | null = null;
    readyState = 0;
    close = vi.fn();

    private listeners: Record<string, EventSourceListener[]> = {};

    constructor(url: string, init?: EventSourceInit) {
        this.url = url;
        this.withCredentials = init?.withCredentials ?? false;
        MockEventSource.instances.push(this);
    }

    addEventListener(type: string, listener: EventSourceListener): void {
        if (!this.listeners[type]) {
            this.listeners[type] = [];
        }
        this.listeners[type].push(listener);
    }

    removeEventListener(type: string, listener: EventSourceListener): void {
        if (this.listeners[type]) {
            this.listeners[type] = this.listeners[type].filter(
                (l) => l !== listener,
            );
        }
    }

    /** Simulate the server sending a named event. */
    simulateEvent(type: string, data: string): void {
        const event = new MessageEvent(type, { data });
        (this.listeners[type] ?? []).forEach((l) => l(event));
    }

    /** Simulate the connection opening. */
    simulateOpen(): void {
        this.readyState = 1;
        this.onopen?.();
    }

    /** Simulate an error. */
    simulateError(): void {
        this.readyState = 2;
        this.onerror?.();
    }
}

// ---------------------------------------------------------------------------
// Install / teardown
// ---------------------------------------------------------------------------

const OriginalEventSource = globalThis.EventSource;

beforeEach(() => {
    MockEventSource.instances = [];
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    globalThis.EventSource = MockEventSource as any;
});

afterEach(() => {
    globalThis.EventSource = OriginalEventSource;
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function latestInstance(): MockEventSource {
    return MockEventSource.instances[MockEventSource.instances.length - 1];
}

const samplePayload: OverviewResponse = {
    summary: 'All systems operational.',
    generated_at: '2026-02-19T12:00:00Z',
    stale_at: '2026-02-19T12:05:00Z',
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useOverviewSSE', () => {
    it('derives the correct stream URL from a plain overview URL', () => {
        renderHook(() => useOverviewSSE('/api/v1/overview'));
        expect(latestInstance().url).toBe('/api/v1/overview/stream');
    });

    it('preserves query parameters in the stream URL', () => {
        renderHook(() =>
            useOverviewSSE(
                '/api/v1/overview?scope_type=server&scope_id=123',
            ),
        );
        expect(latestInstance().url).toBe(
            '/api/v1/overview/stream?scope_type=server&scope_id=123',
        );
    });

    it('enables withCredentials for cookie auth', () => {
        renderHook(() => useOverviewSSE('/api/v1/overview'));
        expect(latestInstance().withCredentials).toBe(true);
    });

    it('parses overview event data and updates state', async () => {
        const { result } = renderHook(() =>
            useOverviewSSE('/api/v1/overview'),
        );

        const es = latestInstance();

        act(() => {
            es.simulateOpen();
            es.simulateEvent('overview', JSON.stringify(samplePayload));
        });

        await waitFor(() => {
            expect(result.current.overview).toEqual(samplePayload);
        });
    });

    it('sets connected to true on open', async () => {
        const { result } = renderHook(() =>
            useOverviewSSE('/api/v1/overview'),
        );

        expect(result.current.connected).toBe(false);

        act(() => {
            latestInstance().simulateOpen();
        });

        await waitFor(() => {
            expect(result.current.connected).toBe(true);
        });
    });

    it('sets connected to false on error', async () => {
        const { result } = renderHook(() =>
            useOverviewSSE('/api/v1/overview'),
        );

        act(() => {
            latestInstance().simulateOpen();
        });

        await waitFor(() => {
            expect(result.current.connected).toBe(true);
        });

        act(() => {
            latestInstance().simulateError();
        });

        await waitFor(() => {
            expect(result.current.connected).toBe(false);
        });
    });

    it('closes EventSource on unmount', () => {
        const { unmount } = renderHook(() =>
            useOverviewSSE('/api/v1/overview'),
        );

        const es = latestInstance();
        expect(es.close).not.toHaveBeenCalled();

        unmount();

        expect(es.close).toHaveBeenCalledTimes(1);
    });

    it('closes old EventSource and creates a new one on URL change', () => {
        const { rerender } = renderHook(
            ({ url }: { url: string }) => useOverviewSSE(url),
            { initialProps: { url: '/api/v1/overview' } },
        );

        const firstEs = latestInstance();
        expect(MockEventSource.instances).toHaveLength(1);

        rerender({ url: '/api/v1/overview?scope_type=server&scope_id=5' });

        expect(firstEs.close).toHaveBeenCalledTimes(1);
        expect(MockEventSource.instances).toHaveLength(2);

        const secondEs = latestInstance();
        expect(secondEs.url).toBe(
            '/api/v1/overview/stream?scope_type=server&scope_id=5',
        );
    });

    it('ignores malformed JSON payloads without crashing', async () => {
        const { result } = renderHook(() =>
            useOverviewSSE('/api/v1/overview'),
        );

        const es = latestInstance();

        act(() => {
            es.simulateOpen();
            es.simulateEvent('overview', 'not valid json');
        });

        // State should remain null after invalid payload.
        expect(result.current.overview).toBeNull();

        // A valid event afterward should still work.
        act(() => {
            es.simulateEvent('overview', JSON.stringify(samplePayload));
        });

        await waitFor(() => {
            expect(result.current.overview).toEqual(samplePayload);
        });
    });
});

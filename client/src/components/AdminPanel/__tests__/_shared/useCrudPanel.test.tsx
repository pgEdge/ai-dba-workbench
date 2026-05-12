/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useCrudPanel } from '../../_shared/useCrudPanel';

interface Item {
    id: number;
    name: string;
}

const ITEMS: Item[] = [
    { id: 1, name: 'one' },
    { id: 2, name: 'two' },
];

describe('useCrudPanel', () => {
    it('fetches the initial list and exposes loading transitions', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel({ fetchItems }));

        // The initial synchronous render shows loading=true and no items.
        expect(result.current.loading).toBe(true);
        expect(result.current.items).toEqual([]);

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        expect(result.current.items).toEqual(ITEMS);
        expect(fetchItems).toHaveBeenCalledTimes(1);
    });

    it('reports a fetch failure on the page-level error slot', async () => {
        const fetchItems = vi.fn().mockRejectedValue(new Error('Network down'));
        const { result } = renderHook(() => useCrudPanel({ fetchItems }));

        await waitFor(() => {
            expect(result.current.error).toBe('Network down');
        });
        expect(result.current.loading).toBe(false);
    });

    it('falls back to a generic message for non-Error fetch rejections', async () => {
        const fetchItems = vi.fn().mockRejectedValue('weird');
        const { result } = renderHook(() => useCrudPanel({ fetchItems }));

        await waitFor(() => {
            expect(result.current.error).toBe('An unexpected error occurred');
        });
    });

    it('re-runs the fetch when refresh() is called', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel({ fetchItems }));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        await act(async () => {
            await result.current.refresh();
        });
        expect(fetchItems).toHaveBeenCalledTimes(2);
    });

    it('re-runs the fetch when deps change', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        let depValue = 'a';
        const { result, rerender } = renderHook(
            () => useCrudPanel({ fetchItems, deps: [depValue] }),
        );

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });
        expect(fetchItems).toHaveBeenCalledTimes(1);

        depValue = 'b';
        rerender();
        await waitFor(() => {
            expect(fetchItems).toHaveBeenCalledTimes(2);
        });
    });

    it('opens the dialog in create mode', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel({ fetchItems }));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        act(() => {
            result.current.setDialogError('stale error');
        });
        expect(result.current.dialogError).toBe('stale error');

        act(() => {
            result.current.openCreate();
        });
        expect(result.current.dialogOpen).toBe(true);
        expect(result.current.editingItem).toBeNull();
        expect(result.current.dialogError).toBeNull();
    });

    it('opens the dialog in edit mode and tracks the editing item', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel<Item>({ fetchItems }));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        act(() => {
            result.current.openEdit(ITEMS[0]);
        });
        expect(result.current.dialogOpen).toBe(true);
        expect(result.current.editingItem).toEqual(ITEMS[0]);
    });

    it('closeDialog clears editing state and dialog error', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel<Item>({ fetchItems }));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        act(() => {
            result.current.openEdit(ITEMS[0]);
            result.current.setDialogError('boom');
        });
        act(() => {
            result.current.closeDialog();
        });
        expect(result.current.dialogOpen).toBe(false);
        expect(result.current.editingItem).toBeNull();
        expect(result.current.dialogError).toBeNull();
    });

    it('closeDialog does nothing while saving is in flight', async () => {
        let resolveSave: (() => void) | null = null;
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel<Item>({ fetchItems }));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        act(() => {
            result.current.openEdit(ITEMS[0]);
        });

        // Start a mutation that we control via an external resolver.
        const slowMutation = new Promise<void>((res) => {
            resolveSave = res;
        });
        let mutationPromise: Promise<unknown> | undefined;
        act(() => {
            mutationPromise = result.current.runMutation(() => slowMutation);
        });
        await waitFor(() => expect(result.current.saving).toBe(true));

        // Try to close mid-save; the hook must keep the dialog open.
        act(() => {
            result.current.closeDialog();
        });
        expect(result.current.dialogOpen).toBe(true);
        expect(result.current.editingItem).toEqual(ITEMS[0]);

        // Let the mutation finish so the hook can settle.
        act(() => {
            resolveSave?.();
        });
        await mutationPromise;
        await waitFor(() => expect(result.current.saving).toBe(false));
    });

    it('opens and closes the delete confirmation dialog', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel<Item>({ fetchItems }));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        act(() => {
            result.current.openDelete(ITEMS[1]);
        });
        expect(result.current.deleteOpen).toBe(true);
        expect(result.current.deleteItem).toEqual(ITEMS[1]);

        act(() => {
            result.current.closeDelete();
        });
        expect(result.current.deleteOpen).toBe(false);
        expect(result.current.deleteItem).toBeNull();
    });

    it('setError and setSuccess update page-level toasts', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel<Item>({ fetchItems }));

        await waitFor(() => {
            expect(result.current.loading).toBe(false);
        });

        act(() => {
            result.current.setError('page error');
            result.current.setSuccess('did the thing');
        });
        expect(result.current.error).toBe('page error');
        expect(result.current.success).toBe('did the thing');

        act(() => {
            result.current.setError(null);
            result.current.setSuccess(null);
        });
        expect(result.current.error).toBeNull();
        expect(result.current.success).toBeNull();
    });

    it('setItems replaces the list and supports updater fns', async () => {
        const fetchItems = vi.fn().mockResolvedValue(ITEMS);
        const { result } = renderHook(() => useCrudPanel<Item>({ fetchItems }));
        await waitFor(() => expect(result.current.loading).toBe(false));

        act(() => {
            result.current.setItems([{ id: 9, name: 'nine' }]);
        });
        expect(result.current.items).toEqual([{ id: 9, name: 'nine' }]);

        act(() => {
            result.current.setItems((prev) => [
                ...prev,
                { id: 10, name: 'ten' },
            ]);
        });
        expect(result.current.items).toHaveLength(2);
    });

    describe('runMutation', () => {
        it('runs the mutation, refreshes, and posts a success toast', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));
            fetchItems.mockClear();

            const fn = vi.fn().mockResolvedValue('ok');
            let returned: unknown;
            await act(async () => {
                returned = await result.current.runMutation(fn, {
                    successMessage: 'created',
                });
            });
            expect(returned).toEqual({ ok: true, value: 'ok' });
            expect(result.current.success).toBe('created');
            expect(fetchItems).toHaveBeenCalledTimes(1);
        });

        it('skips refresh when refresh: false is set', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));
            fetchItems.mockClear();

            await act(async () => {
                await result.current.runMutation(
                    () => Promise.resolve(),
                    { refresh: false },
                );
            });
            expect(fetchItems).not.toHaveBeenCalled();
        });

        it('writes errors to the dialog slot by default', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            const fn = vi.fn().mockRejectedValue(new Error('save bad'));
            await act(async () => {
                await result.current.runMutation(fn);
            });
            expect(result.current.dialogError).toBe('save bad');
            expect(result.current.error).toBeNull();
            expect(result.current.saving).toBe(false);
        });

        it('writes errors to the page slot when errorTarget=page', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            const fn = vi.fn().mockRejectedValue(new Error('delete bad'));
            await act(async () => {
                await result.current.runMutation(fn, { errorTarget: 'page' });
            });
            expect(result.current.error).toBe('delete bad');
            expect(result.current.dialogError).toBeNull();
            expect(result.current.deleteLoading).toBe(false);
        });

        it('uses errorFallback for non-Error rejections', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            const fn = vi.fn().mockRejectedValue('weird');
            await act(async () => {
                await result.current.runMutation(fn, {
                    errorTarget: 'page',
                    errorFallback: 'custom message',
                });
            });
            expect(result.current.error).toBe('custom message');
        });

        it('honours a mapError function to translate errors', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            const fn = vi.fn().mockRejectedValue(
                new Error('UNIQUE constraint violated'),
            );
            await act(async () => {
                await result.current.runMutation(fn, {
                    mapError: (err) =>
                        err instanceof Error
                            && err.message.includes('UNIQUE constraint')
                            ? 'Already exists.'
                            : 'other',
                });
            });
            expect(result.current.dialogError).toBe('Already exists.');
        });

        it('returns { ok: false } when the mutation fails', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            const fn = vi.fn().mockRejectedValue(new Error('nope'));
            let returned: unknown = 'sentinel';
            await act(async () => {
                returned = await result.current.runMutation(fn);
            });
            expect(returned).toEqual({ ok: false });
        });

        it(
            'returns { ok: true, value: undefined } for a void-returning success',
            async () => {
                // Regression guard for issue #214: an `apiDelete` that
                // resolves to `undefined` on HTTP 204 must be reported
                // as a success, NOT a failure. With the previous
                // `R | undefined` return type, callers could not tell
                // the two cases apart and the AdminGroups delete dialog
                // never closed. The tagged result removes the ambiguity
                // at the type level, and this test pins the runtime
                // behaviour so a future refactor cannot regress it.
                const fetchItems = vi.fn().mockResolvedValue(ITEMS);
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));
                fetchItems.mockClear();

                const fn = vi.fn().mockResolvedValue(undefined);
                let returned: unknown;
                await act(async () => {
                    returned = await result.current.runMutation(fn);
                });
                expect(returned).toEqual({ ok: true, value: undefined });
                // The success branch must still trigger a refresh by
                // default, just like any other successful mutation.
                expect(fetchItems).toHaveBeenCalledTimes(1);
            },
        );

        it('clears prior dialog error before each mutation', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            act(() => {
                result.current.setDialogError('stale');
            });
            await act(async () => {
                await result.current.runMutation(() => Promise.resolve('ok'));
            });
            expect(result.current.dialogError).toBeNull();
        });

        it('clears prior page error before page-target mutations', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            act(() => {
                result.current.setError('stale');
            });
            await act(async () => {
                await result.current.runMutation(() => Promise.resolve('ok'), {
                    errorTarget: 'page',
                });
            });
            expect(result.current.error).toBeNull();
        });

        it('sets saving=true during dialog mutations and resets after', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            let resolver: ((value: string) => void) | null = null;
            const fn = vi.fn(
                () => new Promise<string>((res) => { resolver = res; }),
            );
            let mutationPromise: Promise<unknown> | undefined;
            act(() => {
                mutationPromise = result.current.runMutation(fn);
            });
            await waitFor(() => expect(result.current.saving).toBe(true));
            act(() => resolver?.('done'));
            await mutationPromise;
            await waitFor(() => expect(result.current.saving).toBe(false));
        });

        it('sets deleteLoading during page-target mutations', async () => {
            const fetchItems = vi.fn().mockResolvedValue(ITEMS);
            const { result } = renderHook(() =>
                useCrudPanel<Item>({ fetchItems }),
            );
            await waitFor(() => expect(result.current.loading).toBe(false));

            let resolver: ((value: string) => void) | null = null;
            const fn = vi.fn(
                () => new Promise<string>((res) => { resolver = res; }),
            );
            let mutationPromise: Promise<unknown> | undefined;
            act(() => {
                mutationPromise = result.current.runMutation(fn, {
                    errorTarget: 'page',
                });
            });
            await waitFor(() => expect(result.current.deleteLoading).toBe(true));
            act(() => resolver?.('done'));
            await mutationPromise;
            await waitFor(() =>
                expect(result.current.deleteLoading).toBe(false),
            );
        });
    });
});

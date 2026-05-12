/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { afterEach, describe, it, expect, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useCrudPanel } from '../../_shared/useCrudPanel';

/**
 * Build an externally-resolvable promise. Useful for ordering multiple
 * in-flight fetches in tests that exercise the stale-result race.
 */
function deferred<T>(): {
    promise: Promise<T>;
    resolve: (value: T) => void;
    reject: (reason?: unknown) => void;
} {
    let resolve!: (value: T) => void;
    let reject!: (reason?: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return { promise, resolve, reject };
}

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

        it(
            'suppresses success toast when the follow-on refresh fails',
            async () => {
                // Regression guard for issue #215: a mutation that
                // succeeds but whose refresh fetch fails must NOT leave
                // a success toast on screen alongside the refresh
                // error. The user-facing outcome is "the data on screen
                // is stale and we could not reload" — showing "Saved"
                // beside "Failed to load …" is contradictory.
                const fetchItems = vi.fn<() => Promise<Item[]>>()
                    .mockResolvedValueOnce(ITEMS)
                    .mockRejectedValueOnce(new Error('Failed to load groups'));
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));

                const fn = vi.fn().mockResolvedValue('saved-id');
                let returned: unknown;
                await act(async () => {
                    returned = await result.current.runMutation(fn, {
                        successMessage: 'Saved',
                    });
                });

                // The mutation itself succeeded, so the tagged result
                // must report ok=true with the mutation's value.
                expect(returned).toEqual({ ok: true, value: 'saved-id' });
                // No stale success toast: the page-level refresh error
                // is the only thing the user should see.
                expect(result.current.success).toBeNull();
                expect(result.current.error).toBe('Failed to load groups');
                // The follow-on refresh did run.
                expect(fetchItems).toHaveBeenCalledTimes(2);
            },
        );

        it(
            'keeps the success toast when refresh succeeds (issue #215 control)',
            async () => {
                // Control case for the suppression test above: a
                // healthy refresh path must NOT swallow the success
                // toast. Without this guard, a buggy "always suppress"
                // implementation could pass the failing-refresh test
                // while breaking every happy-path mutation in the app.
                const fetchItems = vi.fn().mockResolvedValue(ITEMS);
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));

                const fn = vi.fn().mockResolvedValue('saved-id');
                await act(async () => {
                    await result.current.runMutation(fn, {
                        successMessage: 'Saved',
                    });
                });
                expect(result.current.success).toBe('Saved');
                expect(result.current.error).toBeNull();
            },
        );

        it(
            'keeps the success toast when refresh: false skips the refresh',
            async () => {
                // When the caller opts out of refresh entirely, there
                // is no fetch that could fail, so the success toast
                // must always fire if `successMessage` is provided.
                const fetchItems = vi.fn().mockResolvedValue(ITEMS);
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));
                fetchItems.mockClear();

                await act(async () => {
                    await result.current.runMutation(
                        () => Promise.resolve('ok'),
                        { successMessage: 'Saved', refresh: false },
                    );
                });
                expect(result.current.success).toBe('Saved');
                expect(fetchItems).not.toHaveBeenCalled();
            },
        );

        it(
            'returns true from refresh() on the happy path',
            async () => {
                // The new Promise<boolean> contract: a clean refresh
                // resolves to `true`. Callers (notably runMutation) use
                // this to decide whether to surface the success toast.
                const fetchItems = vi.fn().mockResolvedValue(ITEMS);
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));

                let outcome: boolean | undefined;
                await act(async () => {
                    outcome = await result.current.refresh();
                });
                expect(outcome).toBe(true);
            },
        );

        it(
            'returns false from refresh() when the fetch rejects',
            async () => {
                // Mirror of the happy-path control: a failing refresh
                // resolves to `false` so runMutation can suppress its
                // success toast. The error message is also written to
                // the page-level error slot, matching pre-#215 behaviour.
                const fetchItems = vi.fn<() => Promise<Item[]>>()
                    .mockResolvedValueOnce(ITEMS)
                    .mockRejectedValueOnce(new Error('boom'));
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));

                let outcome: boolean | undefined;
                await act(async () => {
                    outcome = await result.current.refresh();
                });
                expect(outcome).toBe(false);
                expect(result.current.error).toBe('boom');
            },
        );

        it(
            'clears a prior success toast when a later refresh fails',
            async () => {
                // Regression guard for the residual issue #215 gap that
                // CodeRabbit flagged on PR #226: even after the
                // suppress-on-failed-refresh fix, a successful mutation
                // followed by an *independent* failing refresh would
                // leave both toasts on screen — "Saved" from the prior
                // mutation and "Failed to load …" from the new error.
                // The authoritative-failure branch in refresh() must
                // clear success before writing error.
                const fetchItems = vi.fn<() => Promise<Item[]>>()
                    // Initial mount.
                    .mockResolvedValueOnce(ITEMS)
                    // Refresh kicked off by the mutation — succeeds.
                    .mockResolvedValueOnce(ITEMS)
                    // Later, independent refresh — rejects.
                    .mockRejectedValueOnce(new Error('Failed to load groups'));
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));

                // Step 1: mutation A succeeds AND its refresh succeeds,
                // so the success toast lands.
                const fn = vi.fn().mockResolvedValue('saved-id');
                await act(async () => {
                    await result.current.runMutation(fn, {
                        successMessage: 'Saved',
                    });
                });
                expect(result.current.success).toBe('Saved');
                expect(result.current.error).toBeNull();

                // Step 2: a later refresh (manual reload, deps change,
                // follow-on refresh from a different mutation, etc.)
                // fails. The old "Saved" toast must be cleared so it
                // doesn't sit on screen next to the new error.
                let outcome: boolean | undefined;
                await act(async () => {
                    outcome = await result.current.refresh();
                });
                expect(outcome).toBe(false);
                expect(result.current.success).toBeNull();
                expect(result.current.error).toBe('Failed to load groups');
            },
        );

        it(
            'does not clear success when a superseded refresh fails',
            async () => {
                // Companion guard: only the *authoritative* failure
                // branch in refresh() clears success. A superseded
                // failure (one whose generation was already overtaken)
                // returns early before reaching the setSuccess(null)
                // call, so a stale rejection cannot wipe out a success
                // toast that legitimately belongs to the newer
                // generation. This pins the "do nothing on stale
                // failure" half of the refresh contract.
                const slow = deferred<Item[]>();
                const fast = deferred<Item[]>();
                const fetchItems = vi.fn<() => Promise<Item[]>>()
                    // Initial mount fetch — resolves immediately.
                    .mockResolvedValueOnce(ITEMS)
                    // Slow refresh that will eventually REJECT after
                    // being superseded by a newer generation.
                    .mockImplementationOnce(() => slow.promise)
                    // Fast refresh that wins the race.
                    .mockImplementationOnce(() => fast.promise);
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));

                // Manually seed a success toast (as if a prior
                // successful mutation left one behind).
                act(() => {
                    result.current.setSuccess('Saved');
                });

                // Start the slow refresh; it is now the latest
                // generation in flight.
                let slowRefresh: Promise<boolean> | undefined;
                act(() => {
                    slowRefresh = result.current.refresh();
                });
                await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(2));

                // Start the fast refresh; it bumps the generation, so
                // the slow refresh is now superseded.
                let fastRefresh: Promise<boolean> | undefined;
                act(() => {
                    fastRefresh = result.current.refresh();
                });
                await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(3));

                // Resolve fast (newer) refresh successfully.
                await act(async () => {
                    fast.resolve(ITEMS);
                    await fastRefresh;
                });

                // Now reject the slow (superseded) refresh. Because the
                // catch handler hits the superseded-guard early, it
                // must NOT clear the success toast.
                await act(async () => {
                    slow.reject(new Error('stale failure'));
                    await slowRefresh;
                });

                expect(result.current.success).toBe('Saved');
                expect(result.current.error).toBeNull();
            },
        );

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

    describe('stale-result protection', () => {
        afterEach(() => {
            vi.restoreAllMocks();
        });

        it('drops a stale fetch that resolves after a newer one (deps change)', async () => {
            // The hook should only commit results from the most recent
            // refresh. Here the first fetch (triggered by mount) is slow
            // and returns ['stale']; the deps change kicks off a second
            // fetch that resolves immediately with ['fresh']. The final
            // `items` must be ['fresh'].
            const slow = deferred<string[]>();
            const fast = deferred<string[]>();
            const fetchItems = vi.fn<() => Promise<string[]>>()
                .mockImplementationOnce(() => slow.promise)
                .mockImplementationOnce(() => fast.promise);

            let dep = 'a';
            const { result, rerender } = renderHook(
                () => useCrudPanel<string>({ fetchItems, deps: [dep] }),
            );

            // First fetch is in flight; loading must be true and no items
            // committed yet.
            expect(result.current.loading).toBe(true);
            expect(result.current.items).toEqual([]);
            await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(1));

            // Change deps; this triggers a second fetch with the next
            // generation. Both fetches are now in flight.
            dep = 'b';
            rerender();
            await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(2));

            // Resolve the fast (newer) fetch first.
            await act(async () => {
                fast.resolve(['fresh']);
                await fast.promise;
            });
            await waitFor(() => expect(result.current.items).toEqual(['fresh']));
            await waitFor(() => expect(result.current.loading).toBe(false));

            // Now resolve the slow (older) fetch. It must NOT overwrite
            // the fresh result, and must NOT flip `loading` back to true
            // or anything else.
            await act(async () => {
                slow.resolve(['stale']);
                await slow.promise;
            });

            expect(result.current.items).toEqual(['fresh']);
            expect(result.current.loading).toBe(false);
        });

        it(
            'a superseded refresh reports outcome=true so callers do not suppress success',
            async () => {
                // Deliberate design choice (see issue #215 thread):
                // when a refresh is superseded by a newer generation,
                // it is no longer authoritative for *any* observable
                // state, including the runMutation success-toast gate.
                // Returning `true` from the superseded path means
                // runMutation will surface its success toast and let
                // the newer generation, which IS authoritative, decide
                // whether to overwrite the page with an error.
                //
                // Treating "superseded" as "failed" instead would let
                // a user-triggered manual refresh that lost a race
                // silently swallow the previous mutation's success
                // toast, which is the opposite of what we want.
                const slow = deferred<Item[]>();
                const fast = deferred<Item[]>();
                const fetchItems = vi.fn<() => Promise<Item[]>>()
                    // Initial mount fetch.
                    .mockResolvedValueOnce(ITEMS)
                    // Slow refresh kicked off by the mutation.
                    .mockImplementationOnce(() => slow.promise)
                    // Fast refresh kicked off by an external caller
                    // (e.g. a deps change in the parent component).
                    .mockImplementationOnce(() => fast.promise);
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));

                // Drive the mutation. Its refresh awaits `slow`.
                const mutationFn = vi.fn().mockResolvedValue('id');
                let mutation: Promise<unknown> | undefined;
                act(() => {
                    mutation = result.current.runMutation(mutationFn, {
                        successMessage: 'Saved',
                    });
                });
                // Wait until the mutation's refresh is in flight.
                await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(2));

                // Now kick off a second, fresher refresh from outside
                // the mutation. It bumps the generation counter.
                let externalRefresh: Promise<boolean> | undefined;
                act(() => {
                    externalRefresh = result.current.refresh();
                });
                await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(3));

                // Resolve the fast (newer) refresh first.
                await act(async () => {
                    fast.resolve(ITEMS);
                    await externalRefresh;
                });

                // Now resolve the slow (superseded) refresh. Its
                // return value flows back into runMutation.
                await act(async () => {
                    slow.resolve(ITEMS);
                    await mutation;
                });

                // The superseded refresh returned true, so the
                // success toast is set.
                expect(result.current.success).toBe('Saved');
                expect(result.current.error).toBeNull();
            },
        );

        it(
            'a superseded refresh error is dropped, not surfaced',
            async () => {
                // Companion to the test above: when the superseded
                // fetch *rejects*, the error must be swallowed.
                // Writing it to `setError` would let a stale failure
                // overwrite the page state owned by the newer
                // generation. The boolean return is also `true` so
                // runMutation does not suppress its success toast.
                const slow = deferred<Item[]>();
                const fast = deferred<Item[]>();
                const fetchItems = vi.fn<() => Promise<Item[]>>()
                    .mockResolvedValueOnce(ITEMS)
                    .mockImplementationOnce(() => slow.promise)
                    .mockImplementationOnce(() => fast.promise);
                const { result } = renderHook(() =>
                    useCrudPanel<Item>({ fetchItems }),
                );
                await waitFor(() => expect(result.current.loading).toBe(false));

                const mutationFn = vi.fn().mockResolvedValue('id');
                let mutation: Promise<unknown> | undefined;
                act(() => {
                    mutation = result.current.runMutation(mutationFn, {
                        successMessage: 'Saved',
                    });
                });
                await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(2));

                let externalRefresh: Promise<boolean> | undefined;
                act(() => {
                    externalRefresh = result.current.refresh();
                });
                await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(3));

                await act(async () => {
                    fast.resolve(ITEMS);
                    await externalRefresh;
                });

                await act(async () => {
                    slow.reject(new Error('stale failure'));
                    await mutation;
                });

                // Success toast survives; the stale error is dropped.
                expect(result.current.success).toBe('Saved');
                expect(result.current.error).toBeNull();
            },
        );

        it('drops a stale fetch error that arrives after a successful refresh', async () => {
            // Same shape as the success-race test, but the older fetch
            // rejects after a newer fetch resolves. The error must not
            // leak onto the page-level error slot.
            const slow = deferred<string[]>();
            const fast = deferred<string[]>();
            const fetchItems = vi.fn<() => Promise<string[]>>()
                .mockImplementationOnce(() => slow.promise)
                .mockImplementationOnce(() => fast.promise);

            let dep = 'a';
            const { result, rerender } = renderHook(
                () => useCrudPanel<string>({ fetchItems, deps: [dep] }),
            );

            await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(1));

            dep = 'b';
            rerender();
            await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(2));

            await act(async () => {
                fast.resolve(['fresh']);
                await fast.promise;
            });
            await waitFor(() => expect(result.current.items).toEqual(['fresh']));

            await act(async () => {
                slow.reject(new Error('stale failure'));
                await slow.promise.catch(() => {});
            });

            expect(result.current.error).toBeNull();
            expect(result.current.items).toEqual(['fresh']);
            expect(result.current.loading).toBe(false);
        });

        it('does not write state after unmount and logs no warning', async () => {
            const consoleError = vi
                .spyOn(console, 'error')
                .mockImplementation(() => {});

            const pending = deferred<string[]>();
            const fetchItems = vi.fn<() => Promise<string[]>>(
                () => pending.promise,
            );

            const { result, unmount } = renderHook(
                () => useCrudPanel<string>({ fetchItems }),
            );

            // The initial fetch is in flight.
            await waitFor(() => expect(fetchItems).toHaveBeenCalledTimes(1));
            expect(result.current.loading).toBe(true);

            // Unmount before the fetch resolves.
            unmount();

            // Resolve the pending fetch. The hook must drop the result;
            // no setState-after-unmount warnings should appear.
            await act(async () => {
                pending.resolve(['after-unmount']);
                await pending.promise;
            });

            const warnings = consoleError.mock.calls
                .map((args) => String(args[0] ?? ''))
                .filter((msg) =>
                    msg.includes('unmounted')
                    || msg.includes('memory leak'),
                );
            expect(warnings).toEqual([]);
        });

        it('still completes the loading transition on the happy path', async () => {
            // Regression guard: with the generation/mount guards in place,
            // a plain refresh() must still flip loading false and commit
            // the result.
            const fetchItems = vi.fn().mockResolvedValue(['only']);
            const { result } = renderHook(() =>
                useCrudPanel<string>({ fetchItems }),
            );

            await waitFor(() => expect(result.current.loading).toBe(false));
            expect(result.current.items).toEqual(['only']);

            fetchItems.mockResolvedValueOnce(['again']);
            await act(async () => {
                await result.current.refresh();
            });
            expect(result.current.items).toEqual(['again']);
            expect(result.current.loading).toBe(false);
        });
    });
});

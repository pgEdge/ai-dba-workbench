/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { extractErrorMessage } from './errors';

/**
 * Generic CRUD-panel hook used by the AdminPanel screens.
 *
 * Every AdminPanel screen previously re-implemented the same machinery:
 *
 *   1. fetch a list, manage `loading` + `error`;
 *   2. surface page-level success/error toasts;
 *   3. drive a create/edit dialog with its own `saving` + `dialogError`;
 *   4. drive a delete-confirmation dialog with `deleteLoading` and
 *      open/close handlers;
 *   5. wrap every mutation in `try/catch` boilerplate that maps thrown
 *      values to a user-facing message and refreshes the list on success.
 *
 * This hook collapses that boilerplate. Each panel still owns its
 * dialog markup, form state, and per-mutation API request — those vary
 * too much between panels to share without contortions — but the
 * surrounding lifecycle is uniform.
 *
 * The hook is intentionally narrow in scope. It is NOT a generic data
 * layer; it is an AdminPanel-specific helper. Adding fields here should
 * be cheap, removing them is hard.
 */

/**
 * Where {@link runMutation} routes a mutation's error message.
 *
 * - `'page'`   writes to `crud.error` (page-level Alert above the table).
 * - `'dialog'` writes to `crud.dialogError` (Alert inside the dialog).
 * - `'inline'` writes nowhere; the caller reads the returned error
 *   from {@link RunMutationResult} and surfaces it itself (toast, row
 *   highlight, local state, etc.).
 */
export type ErrorTarget = 'page' | 'dialog' | 'inline';

/**
 * Which busy flag {@link runMutation} toggles while the mutation runs.
 *
 * - `'save'`   toggles `crud.saving` (dialog spinner / disabled state).
 * - `'delete'` toggles `crud.deleteLoading` (delete dialog spinner).
 * - `'inline'` toggles neither. The caller owns inline busy state, so
 *   an inline toggle never flickers a global delete or save indicator.
 */
export type BusyTarget = 'save' | 'delete' | 'inline';

/**
 * Tagged result returned by {@link CrudPanelApi.runMutation}.
 *
 * Callers must inspect the `ok` discriminant before reading `value`,
 * which is only present on the success branch. The discriminated union
 * prevents the "void success vs. failure" ambiguity that existed when
 * `runMutation` returned `R | undefined`: an `apiDelete` that resolves
 * to `undefined` on HTTP 204 is now unambiguously distinguishable from
 * a rejection, regardless of the value of `R`.
 *
 * The failure branch carries the user-facing `error` message (after
 * `mapError` / `errorFallback` are applied) so callers using
 * `errorTarget: 'inline'` can surface it locally without re-deriving
 * the message themselves.
 */
export type RunMutationResult<R> =
    | { ok: true; value: R }
    | { ok: false; error: string };

/**
 * Options for a single mutation invocation.
 *
 * `errorTarget` and `busyTarget` are intentionally split: where the
 * error message is shown is independent of which busy flag the hook
 * toggles. When `busyTarget` is omitted it defaults from `errorTarget`
 * for back-compat with the previous coupled API:
 *
 *   - `errorTarget: 'page'`   -> default `busyTarget: 'delete'`
 *   - `errorTarget: 'dialog'` -> default `busyTarget: 'save'`
 *   - `errorTarget: 'inline'` -> default `busyTarget: 'inline'`
 */
export interface RunMutationOptions {
    /** Toast to show on success. Omit to leave success state untouched. */
    successMessage?: string;
    /** Where to route the error message. Defaults to `'dialog'`. */
    errorTarget?: ErrorTarget;
    /** Which busy flag to toggle. Defaults from `errorTarget`. */
    busyTarget?: BusyTarget;
    /** Whether to refresh the list on success. Defaults to `true`. */
    refresh?: boolean;
    /** Fallback message for non-Error throws. */
    errorFallback?: string;
    /** Maps the thrown value to a user-facing message before display. */
    mapError?: (err: unknown) => string;
}

/**
 * Public API returned by {@link useCrudPanel}.
 */
export interface CrudPanelApi<T> {
    // --- List state ---
    items: T[];
    setItems: React.Dispatch<React.SetStateAction<T[]>>;
    loading: boolean;
    error: string | null;
    setError: (value: string | null) => void;
    success: string | null;
    setSuccess: (value: string | null) => void;
    /**
     * Re-fetch the list. Resolves to `true` when the fetch produced a
     * fresh result (or was superseded by a newer generation; see the
     * implementation note below). Resolves to `false` when the fetch
     * rejected and the hook wrote the message to {@link CrudPanelApi.error}.
     *
     * Callers that do not care about the outcome (page mount, manual
     * reload button) may discard the boolean. {@link runMutation} uses
     * it to suppress a stale success toast when the follow-on refresh
     * fails — see issue #215.
     */
    refresh: () => Promise<boolean>;

    // --- Edit/create dialog state ---
    dialogOpen: boolean;
    editingItem: T | null;
    saving: boolean;
    dialogError: string | null;
    setDialogError: (value: string | null) => void;
    openCreate: () => void;
    openEdit: (item: T) => void;
    closeDialog: () => void;

    // --- Delete-confirmation dialog state ---
    deleteOpen: boolean;
    deleteItem: T | null;
    deleteLoading: boolean;
    openDelete: (item: T) => void;
    closeDelete: () => void;

    // --- Mutation helper ---
    runMutation: <R>(
        fn: () => Promise<R>,
        options?: RunMutationOptions,
    ) => Promise<RunMutationResult<R>>;
}

/**
 * Configures the hook. The fetcher is the only required option; an empty
 * panel that just lists items is a valid use of the hook.
 */
export interface UseCrudPanelOptions<T> {
    /**
     * Asynchronously fetch the list of items. Should throw on failure
     * so the hook can capture and display the error.
     */
    fetchItems: () => Promise<T[]>;
    /**
     * Optional list of values whose change forces a refresh. The
     * fetcher itself is always tracked, so consumers usually wrap it
     * in `useCallback` and put cross-cutting deps inside the closure.
     *
     * `deps` must have a stable length across renders; React throws
     * "The final argument passed to useEffect changed size between
     * renders" if a caller passes a variable-length array.
     */
    deps?: ReadonlyArray<unknown>;
}

/**
 * Hook that owns list, dialog, and delete state for an AdminPanel screen.
 *
 * Usage sketch:
 *
 *   const crud = useCrudPanel<MyItem>({
 *       fetchItems: useCallback(async () => {
 *           const data = await apiGet<{ items: MyItem[] }>('/api/v1/things');
 *           return data.items ?? [];
 *       }, []),
 *   });
 *
 *   const handleSave = async () => {
 *       const result = await crud.runMutation(
 *           () => apiPost('/api/v1/things', body),
 *           { successMessage: 'Created!' },
 *       );
 *       if (result.ok) {
 *           crud.closeDialog();
 *       }
 *   };
 */
export function useCrudPanel<T>(options: UseCrudPanelOptions<T>): CrudPanelApi<T> {
    const { fetchItems, deps } = options;

    // --- List state ---
    const [items, setItems] = useState<T[]>([]);
    const [loading, setLoading] = useState<boolean>(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // --- Dialog state ---
    const [dialogOpen, setDialogOpen] = useState<boolean>(false);
    const [editingItem, setEditingItem] = useState<T | null>(null);
    const [saving, setSaving] = useState<boolean>(false);
    const [dialogError, setDialogError] = useState<string | null>(null);

    // --- Delete dialog state ---
    const [deleteOpen, setDeleteOpen] = useState<boolean>(false);
    const [deleteItem, setDeleteItem] = useState<T | null>(null);
    const [deleteLoading, setDeleteLoading] = useState<boolean>(false);

    // Generation counter for `refresh()`. Each invocation captures its
    // own generation; only the most recent generation is allowed to
    // commit results to state. This prevents an earlier, slower fetch
    // from overwriting a later, faster one when `deps` change while a
    // request is in flight.
    const fetchGenerationRef = useRef<number>(0);

    // Tracks whether the component is still mounted. State writes after
    // unmount are silently dropped to avoid React's "setState on an
    // unmounted component" dev-mode warning.
    const isMountedRef = useRef<boolean>(true);

    const refresh = useCallback(async (): Promise<boolean> => {
        const generation = fetchGenerationRef.current + 1;
        fetchGenerationRef.current = generation;
        setLoading(true);
        try {
            const next = await fetchItems();
            // Drop the result if a newer fetch started, or if the
            // component unmounted while this request was in flight.
            // A superseded fetch is treated as a clean outcome from
            // *this* caller's perspective: the newer generation will
            // report its own success or failure, and the success toast
            // that gated on us belongs to a mutation whose intent has
            // not been contradicted. Returning `true` here means
            // runMutation does NOT suppress its success toast on a
            // stale-but-still-pending refresh; the newer refresh owns
            // that decision.
            if (
                !isMountedRef.current
                || fetchGenerationRef.current !== generation
            ) {
                return true;
            }
            setItems(next);
            return true;
        } catch (err: unknown) {
            // Same rule as the success branch: an error from a
            // superseded fetch is dropped entirely (not written to
            // `error`, not reported to the caller). The newer
            // generation is authoritative.
            if (
                !isMountedRef.current
                || fetchGenerationRef.current !== generation
            ) {
                return true;
            }
            // Clear any lingering success toast so the refresh error is alone (see #215).
            setSuccess(null);
            setError(extractErrorMessage(err));
            return false;
        } finally {
            // Only the latest in-flight generation owns the loading
            // flag; earlier generations must not flip it back to
            // false while a fresh request is still pending.
            if (
                isMountedRef.current
                && fetchGenerationRef.current === generation
            ) {
                setLoading(false);
            }
        }
    }, [fetchItems]);

    // Run refresh once on mount and whenever the caller's `deps` change.
    // `refresh` is included in the deps array; the eslint-disable below
    // is required because we spread caller-supplied `deps`, which the
    // exhaustive-deps rule cannot statically verify. The cleanup hook
    // flips `isMountedRef` so any in-flight fetch that resolves after
    // unmount becomes a no-op. A deps-change does not unmount the
    // component, so we also re-arm the ref each time the effect runs.
    useEffect(() => {
        isMountedRef.current = true;
        void refresh();
        return () => {
            isMountedRef.current = false;
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [refresh, ...(deps ?? [])]);

    const openCreate = useCallback(() => {
        setEditingItem(null);
        setDialogError(null);
        setDialogOpen(true);
    }, []);

    const openEdit = useCallback((item: T) => {
        setEditingItem(item);
        setDialogError(null);
        setDialogOpen(true);
    }, []);

    const closeDialog = useCallback(() => {
        // Block close while a save is in flight — the saving handler
        // owns the close transition.
        setSaving((isSaving) => {
            if (!isSaving) {
                setDialogOpen(false);
                setEditingItem(null);
                setDialogError(null);
            }
            return isSaving;
        });
    }, []);

    const openDelete = useCallback((item: T) => {
        setDeleteItem(item);
        setDeleteOpen(true);
    }, []);

    const closeDelete = useCallback(() => {
        setDeleteOpen(false);
        setDeleteItem(null);
    }, []);

    const runMutation = useCallback(async function runMutationInner<R>(
        fn: () => Promise<R>,
        opts: RunMutationOptions = {},
    ): Promise<RunMutationResult<R>> {
        const {
            successMessage,
            errorTarget = 'dialog',
            busyTarget,
            refresh: shouldRefresh = true,
            errorFallback,
            mapError,
        } = opts;

        // Default `busyTarget` from `errorTarget` so callers that
        // pre-date the split keep their previous coupled behaviour.
        const resolvedBusyTarget: BusyTarget = busyTarget
            ?? (errorTarget === 'dialog'
                ? 'save'
                : errorTarget === 'page'
                    ? 'delete'
                    : 'inline');

        // Inline mutations toggle no shared busy flag; the caller owns
        // any local spinner. This prevents inline actions (e.g. a row
        // toggle) from flickering a global delete or save indicator.
        const setBusy: ((value: boolean) => void) | null = (() => {
            if (resolvedBusyTarget === 'save') { return setSaving; }
            if (resolvedBusyTarget === 'delete') { return setDeleteLoading; }
            return null;
        })();

        try {
            setBusy?.(true);
            // Clear only the slot we're about to write into. Inline
            // targets write nowhere, so nothing to clear pre-emptively.
            if (errorTarget === 'dialog') {
                setDialogError(null);
            } else if (errorTarget === 'page') {
                setError(null);
            }
            const value = await fn();
            // Defer the success toast until we know whether the
            // follow-on refresh succeeded. Setting it now and clearing
            // it on refresh failure would briefly flash "Saved" before
            // "Failed to load…" replaces it; gating instead avoids the
            // flash entirely. See issue #215.
            let refreshClean = true;
            if (shouldRefresh) {
                refreshClean = await refresh();
            }
            if (successMessage && refreshClean) {
                setSuccess(successMessage);
            }
            // Wrap the success value in the tagged result so callers can
            // distinguish a void-returning success (`apiDelete` on HTTP
            // 204) from a failure without inspecting `value` at all.
            // Note: the mutation itself succeeded even if the refresh
            // didn't, so we still return ok=true here. The refresh
            // error is surfaced via `error`, not via this return value.
            return { ok: true, value };
        } catch (err: unknown) {
            const message = mapError
                ? mapError(err)
                : extractErrorMessage(err, errorFallback);
            // Route the message based on `errorTarget`. Inline targets
            // do NOT write to either shared slot; the caller reads the
            // message from the returned result and surfaces it itself.
            if (errorTarget === 'dialog') {
                setDialogError(message);
            } else if (errorTarget === 'page') {
                setError(message);
            }
            return { ok: false, error: message };
        } finally {
            setBusy?.(false);
        }
    }, [refresh]);

    return {
        // List
        items,
        setItems,
        loading,
        error,
        setError,
        success,
        setSuccess,
        refresh,
        // Dialog
        dialogOpen,
        editingItem,
        saving,
        dialogError,
        setDialogError,
        openCreate,
        openEdit,
        closeDialog,
        // Delete
        deleteOpen,
        deleteItem,
        deleteLoading,
        openDelete,
        closeDelete,
        // Mutation
        runMutation,
    };
}

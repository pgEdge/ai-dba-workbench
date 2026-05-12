/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useCallback, useEffect, useState } from 'react';
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
 * Tagged result returned by {@link CrudPanelApi.runMutation}.
 *
 * Callers must inspect the `ok` discriminant before reading `value`,
 * which is only present on the success branch. The discriminated union
 * prevents the "void success vs. failure" ambiguity that existed when
 * `runMutation` returned `R | undefined`: an `apiDelete` that resolves
 * to `undefined` on HTTP 204 is now unambiguously distinguishable from
 * a rejection, regardless of the value of `R`.
 */
export type RunMutationResult<R> =
    | { ok: true; value: R }
    | { ok: false };

/**
 * Options for a single mutation invocation.
 */
export interface RunMutationOptions {
    /**
     * Toast to show on success. Omit to leave success state untouched.
     */
    successMessage?: string;
    /**
     * Where to report errors. The default reports to the dialog-level
     * `dialogError` slot, which matches the create/edit flow. Page-level
     * mutations (delete, inline toggles, etc.) should pass `'page'`.
     */
    errorTarget?: 'page' | 'dialog';
    /**
     * Whether to refresh the list after a successful mutation. Defaults
     * to `true` because the vast majority of mutations need it.
     */
    refresh?: boolean;
    /**
     * Custom fallback message for non-Error throws. Defaults to the
     * shared {@link extractErrorMessage} fallback.
     */
    errorFallback?: string;
    /**
     * Maps the thrown value to a user-facing message before display.
     * Useful for translating server constraint errors into friendlier
     * wording (e.g. UNIQUE constraint -> "already exists").
     */
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
    refresh: () => Promise<void>;

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

    const refresh = useCallback(async () => {
        try {
            setLoading(true);
            const next = await fetchItems();
            setItems(next);
        } catch (err: unknown) {
            setError(extractErrorMessage(err));
        } finally {
            setLoading(false);
        }
    }, [fetchItems]);

    // Run refresh once on mount and whenever the caller's `deps` change.
    // `refresh` is included in the deps array; the eslint-disable below
    // is required because we spread caller-supplied `deps`, which the
    // exhaustive-deps rule cannot statically verify.
    useEffect(() => {
        void refresh();
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
            refresh: shouldRefresh = true,
            errorFallback,
            mapError,
        } = opts;

        const setBusy = errorTarget === 'dialog' ? setSaving : setDeleteLoading;
        const writeError = errorTarget === 'dialog' ? setDialogError : setError;

        try {
            setBusy(true);
            if (errorTarget === 'dialog') {
                setDialogError(null);
            } else {
                setError(null);
            }
            const value = await fn();
            if (successMessage) {
                setSuccess(successMessage);
            }
            if (shouldRefresh) {
                await refresh();
            }
            // Wrap the success value in the tagged result so callers can
            // distinguish a void-returning success (`apiDelete` on HTTP
            // 204) from a failure without inspecting `value` at all.
            return { ok: true, value };
        } catch (err: unknown) {
            const message = mapError
                ? mapError(err)
                : extractErrorMessage(err, errorFallback);
            writeError(message);
            return { ok: false };
        } finally {
            setBusy(false);
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

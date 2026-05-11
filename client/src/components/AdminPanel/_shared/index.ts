/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Shared helpers for AdminPanel CRUD screens. Each AdminPanel component
 * (groups, alert rules, messaging channels, etc.) re-implemented similar
 * list-fetch / dialog / delete / mutation machinery. This module exposes
 * narrow primitives that those panels can compose, eliminating the bulk
 * of the duplication without forcing a single component shape on screens
 * with substantially different forms.
 */

export {
    useCrudPanel,
    type CrudPanelApi,
    type RunMutationOptions,
    type UseCrudPanelOptions,
} from './useCrudPanel';
export { extractErrorMessage, DEFAULT_ERROR_MESSAGE } from './errors';

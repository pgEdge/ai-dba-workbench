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
 * ScopedOverridesPanel is a generic, reusable scaffolding component
 * for displaying and managing per-scope (server / cluster / group)
 * overrides of monitoring rules. It is shared between the probe and
 * alert override panels, both of which expose near-identical fetch,
 * list, edit, and reset behaviour but differ in the schema of each
 * row and in the field set of the edit dialog.
 *
 * Design notes:
 *
 * - The component owns the cross-cutting concerns that are genuinely
 *   identical between the two panels: data fetching, loading state,
 *   error and success banners, the bordered table shell, empty-state
 *   rendering, optional category grouping, and the Reset action.
 *
 * - The component does NOT own the edit dialog. The edit dialog field
 *   sets differ materially between probe rules (enabled / interval /
 *   retention days) and alert rules (enabled / operator / threshold /
 *   severity), so it would be a bad abstraction to merge them. The
 *   caller renders its own dialog via the `renderEditDialog` prop and
 *   receives helper callbacks (`onSaveSuccess`, `onSaveError`,
 *   `refresh`) that wire the dialog into the shared state.
 *
 * - The Edit button on each row simply forwards the row's item to the
 *   caller via `onEditRequested`. The caller is responsible for
 *   storing the selected item and toggling the dialog's open state.
 *
 * - This component intentionally does not abstract a "scope tree
 *   selector" because the existing override panels receive `scope`
 *   and `scopeId` as props from their parent (see e.g. the cluster
 *   detail and server detail screens). Adding a tree selector here
 *   would be a different feature, not a refactor.
 */

import type React from 'react';
import { Fragment, useCallback, useEffect, useState } from 'react';
import {
    Alert,
    Box,
    CircularProgress,
    IconButton,
    Paper,
    Table,
    TableBody,
    TableCell,
    TableContainer,
    TableHead,
    TableRow,
    Tooltip,
    Typography,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Edit as EditIcon,
    RestartAlt as ResetIcon,
} from '@mui/icons-material';
import { apiDelete, apiGet } from '../../utils/apiClient';
import {
    categoryLabelSx,
    emptyRowSx,
    emptyRowTextSx,
    getTableContainerSx,
    loadingContainerSx,
    tableHeaderCellSx,
} from '../AdminPanel/styles';

/**
 * Description of a column header rendered above the body rows.
 */
export interface ScopedOverridesColumn {
    /** Visible header label. */
    label: string;
    /** Cell alignment; defaults to left. */
    align?: 'left' | 'right' | 'center';
}

/**
 * Helpers passed to the caller's edit-dialog render prop. The dialog
 * uses these to feed messages back into the shared error and success
 * banners and to trigger a list refresh after a successful save.
 */
export interface ScopedOverridesEditHelpers {
    /** Called by the dialog after a successful save. */
    onSaveSuccess: (message: string) => void;
    /** Called by the dialog when a save fails. */
    onSaveError: (err: unknown) => void;
    /** Re-runs the fetch to refresh the list. */
    refresh: () => void;
}

export interface ScopedOverridesPanelProps<T> {
    /** Scope kind in the API URL. */
    scope: 'server' | 'cluster' | 'group';
    /** Numeric identifier of the scope target. */
    scopeId: number;
    /**
     * Base path for the override API (without trailing slash). The
     * component issues `GET <basePath>/<scope>/<scopeId>` to load,
     * and `DELETE <basePath>/<scope>/<scopeId>/<key>` to reset.
     */
    apiBasePath: string;
    /**
     * Returns the unique identifier used both as the React key and
     * as the trailing path segment in the reset DELETE URL.
     */
    itemKey: (item: T) => string | number;
    /** Returns the human-readable name used in success messages. */
    itemDisplayName: (item: T) => string;
    /** Returns whether the row currently has an explicit override. */
    hasOverride: (item: T) => boolean;
    /** Column header definitions (in order). */
    columns: ScopedOverridesColumn[];
    /**
     * Render the data cells for a single row. The shared component
     * appends the standard Actions cell with edit and reset buttons.
     */
    renderRowCells: (item: T) => React.ReactNode;
    /**
     * Optional row grouping function. When provided, rows are
     * grouped under a category header row (sorted alphabetically).
     */
    groupBy?: (item: T) => string;
    /** Message rendered when the API returns an empty list. */
    emptyMessage: string;
    /** ARIA label for the loading spinner. */
    loadingLabel: string;
    /** Handler invoked when the user clicks the row's edit button. */
    onEditRequested: (item: T) => void;
    /**
     * Render prop for the caller-owned edit dialog. The shared
     * component does not manage dialog open state or field state;
     * the caller injects its own dialog and wires success and error
     * outcomes back into the shared banners using the helpers.
     */
    renderEditDialog: (helpers: ScopedOverridesEditHelpers) => React.ReactNode;
}

/**
 * Generic, reusable scaffolding for monitoring override panels.
 *
 * @typeParam T - The shape of an individual override row. The
 *                shared component is agnostic to the fields T
 *                exposes; the caller projects them into the table
 *                via `renderRowCells`.
 */
function ScopedOverridesPanel<T>(
    props: ScopedOverridesPanelProps<T>,
): React.ReactElement {
    const {
        scope,
        scopeId,
        apiBasePath,
        itemKey,
        itemDisplayName,
        hasOverride,
        columns,
        renderRowCells,
        groupBy,
        emptyMessage,
        loadingLabel,
        onEditRequested,
        renderEditDialog,
    } = props;

    const theme = useTheme();

    const [overrides, setOverrides] = useState<T[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    const fetchOverrides = useCallback(async () => {
        try {
            setLoading(true);
            const data = await apiGet<T[]>(
                `${apiBasePath}/${scope}/${String(scopeId)}`,
            );
            setOverrides(data || []);
        } catch (err: unknown) {
            if (err instanceof Error) {
                setError(err.message);
            } else {
                setError('An unexpected error occurred');
            }
        } finally {
            setLoading(false);
        }
    }, [apiBasePath, scope, scopeId]);

    useEffect(() => {
        void fetchOverrides();
    }, [fetchOverrides]);

    /**
     * Centralised error projection used by both the reset action
     * and the dialog's onSaveError helper. Anything `Error`-shaped
     * surfaces its message; everything else falls back to a generic
     * label so the user is never left staring at nothing.
     */
    const projectError = useCallback((err: unknown) => {
        if (err instanceof Error) {
            setError(err.message);
        } else {
            setError('An unexpected error occurred');
        }
    }, []);

    const handleResetOverride = useCallback(
        async (item: T, e: React.MouseEvent) => {
            e.stopPropagation();
            try {
                setError(null);
                await apiDelete(
                    `${apiBasePath}/${scope}/${String(scopeId)}/${String(itemKey(item))}`,
                );
                setSuccess(
                    `Override for "${itemDisplayName(item)}" reset to default.`,
                );
                void fetchOverrides();
            } catch (err: unknown) {
                projectError(err);
            }
        },
        [
            apiBasePath,
            scope,
            scopeId,
            itemKey,
            itemDisplayName,
            fetchOverrides,
            projectError,
        ],
    );

    const handleEditClick = useCallback(
        (item: T, e: React.MouseEvent) => {
            e.stopPropagation();
            // Clear any stale error so the dialog opens with a fresh
            // banner state. Success messages are intentionally left
            // alone — the user just took an explicit action so the
            // previous "saved" message is still relevant context.
            setError(null);
            onEditRequested(item);
        },
        [onEditRequested],
    );

    if (loading) {
        return (
            <Box sx={loadingContainerSx}>
                <CircularProgress aria-label={loadingLabel} />
            </Box>
        );
    }

    const tableContainerSx = getTableContainerSx(theme);

    // Total column count including the trailing Actions cell. Used
    // for the colSpan on the empty-state row and on category headers.
    const totalColumns = columns.length + 1;

    /**
     * Render the data row for a single override item, including the
     * trailing Actions cell. The Reset button only appears when the
     * row has an override of its own.
     */
    const renderItemRow = (item: T) => (
        <TableRow key={String(itemKey(item))} hover>
            {renderRowCells(item)}
            <TableCell align="right">
                <Tooltip title="Edit override">
                    <IconButton
                        size="small"
                        onClick={(e) => {
                            handleEditClick(item, e);
                        }}
                        aria-label="edit override"
                    >
                        <EditIcon fontSize="small" />
                    </IconButton>
                </Tooltip>
                {hasOverride(item) && (
                    <Tooltip title="Reset to default">
                        <IconButton
                            size="small"
                            onClick={(e) => {
                                void handleResetOverride(item, e);
                            }}
                            aria-label="reset override to default"
                        >
                            <ResetIcon fontSize="small" />
                        </IconButton>
                    </Tooltip>
                )}
            </TableCell>
        </TableRow>
    );

    /**
     * Render the body rows. When `groupBy` is provided, items are
     * organised under category header rows in alphabetical order;
     * otherwise the rows render as a flat list. Empty input renders
     * a single full-width "no data" row.
     */
    const renderBody = (): React.ReactNode => {
        if (overrides.length === 0) {
            return (
                <TableRow>
                    <TableCell colSpan={totalColumns} align="center" sx={emptyRowSx}>
                        <Typography color="text.secondary" sx={emptyRowTextSx}>
                            {emptyMessage}
                        </Typography>
                    </TableCell>
                </TableRow>
            );
        }

        if (!groupBy) {
            return overrides.map(renderItemRow);
        }

        const categories = Array.from(
            new Set(overrides.map((item) => groupBy(item))),
        ).sort();

        return categories.map((category) => (
            <Fragment key={category}>
                <TableRow>
                    <TableCell
                        colSpan={totalColumns}
                        sx={{
                            ...categoryLabelSx,
                            bgcolor: theme.palette.action.hover,
                            py: 0.75,
                        }}
                    >
                        {category}
                    </TableCell>
                </TableRow>
                {overrides
                    .filter((item) => groupBy(item) === category)
                    .map(renderItemRow)}
            </Fragment>
        ));
    };

    const editHelpers: ScopedOverridesEditHelpers = {
        onSaveSuccess: (message) => {
            setSuccess(message);
            setError(null);
        },
        onSaveError: projectError,
        refresh: () => {
            void fetchOverrides();
        },
    };

    return (
        <Box>
            {error && (
                <Alert
                    severity="error"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={() => {
                        setError(null);
                    }}
                >
                    {error}
                </Alert>
            )}
            {success && (
                <Alert
                    severity="success"
                    sx={{ mb: 2, borderRadius: 1 }}
                    onClose={() => {
                        setSuccess(null);
                    }}
                >
                    {success}
                </Alert>
            )}

            <TableContainer component={Paper} elevation={0} sx={tableContainerSx}>
                <Table>
                    <TableHead>
                        <TableRow>
                            {columns.map((col) => (
                                <TableCell
                                    key={col.label}
                                    sx={tableHeaderCellSx}
                                    align={col.align}
                                >
                                    {col.label}
                                </TableCell>
                            ))}
                            <TableCell sx={tableHeaderCellSx} align="right">
                                Actions
                            </TableCell>
                        </TableRow>
                    </TableHead>
                    <TableBody>{renderBody()}</TableBody>
                </Table>
            </TableContainer>

            {renderEditDialog(editHelpers)}
        </Box>
    );
}

export default ScopedOverridesPanel;

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
 * ProbeOverridesPanel renders and manages per-scope overrides for
 * collector probes. The cross-cutting list, fetch, banner, reset and
 * scaffolding behaviour is delegated to ScopedOverridesPanel; this
 * file owns only the probe-specific row projection and edit dialog.
 */

import type React from 'react';
import { useState } from 'react';
import {
    Box,
    Button,
    CircularProgress,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    Switch,
    TableCell,
    TextField,
    Typography,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { apiPut } from '../utils/apiClient';
import { SELECT_FIELD_SX } from './shared/formStyles';
import {
    dialogActionsSx,
    dialogTitleSx,
    getContainedButtonSx,
} from './AdminPanel/styles';
import ScopedOverridesPanel, {
    type ScopedOverridesColumn,
    type ScopedOverridesEditHelpers,
} from './shared/ScopedOverridesPanel';

const API_BASE_URL = '/api/v1';

interface ProbeOverride {
    name: string;
    description: string;
    default_enabled: boolean;
    default_interval_seconds: number;
    default_retention_days: number;
    has_override: boolean;
    override_enabled: boolean | null;
    override_interval_seconds: number | null;
    override_retention_days: number | null;
}

interface ProbeOverridesPanelProps {
    scope: 'server' | 'cluster' | 'group';
    scopeId: number;
}

/** Column definitions for the probe overrides table. */
const PROBE_COLUMNS: ScopedOverridesColumn[] = [
    { label: 'Name' },
    { label: 'Description' },
    { label: 'Enabled' },
    { label: 'Interval' },
    { label: 'Retention' },
];

/** Italic, dimmed style applied to cells displaying inherited defaults. */
const inheritedCellSx = {
    fontStyle: 'italic',
    opacity: 0.6,
};

/**
 * Resolve the effective values for a probe row. When no override is
 * present the defaults flow through unchanged; when an override is
 * present any null fields fall back to the corresponding default.
 */
const getDisplayValues = (item: ProbeOverride) => {
    if (item.has_override) {
        return {
            enabled: item.override_enabled ?? item.default_enabled,
            interval:
                item.override_interval_seconds ?? item.default_interval_seconds,
            retention:
                item.override_retention_days ?? item.default_retention_days,
        };
    }
    return {
        enabled: item.default_enabled,
        interval: item.default_interval_seconds,
        retention: item.default_retention_days,
    };
};

const ProbeOverridesPanel: React.FC<ProbeOverridesPanelProps> = ({
    scope,
    scopeId,
}) => {
    const theme = useTheme();

    // The shared scaffold owns the list, alerts, and reset action;
    // the dialog and its form state remain local because the field
    // set is probe-specific.
    const [editOpen, setEditOpen] = useState(false);
    const [editOverride, setEditOverride] = useState<ProbeOverride | null>(null);
    const [editEnabled, setEditEnabled] = useState(false);
    const [editInterval, setEditInterval] = useState('');
    const [editRetention, setEditRetention] = useState('');
    const [saving, setSaving] = useState(false);

    /**
     * Pre-populate the edit dialog with the row's effective values
     * so the user starts from "current state" rather than blanks.
     */
    const handleEditRequested = (override: ProbeOverride) => {
        const display = getDisplayValues(override);
        setEditOverride(override);
        setEditEnabled(display.enabled);
        setEditInterval(String(display.interval));
        setEditRetention(String(display.retention));
        setEditOpen(true);
    };

    const renderRowCells = (item: ProbeOverride) => {
        const display = getDisplayValues(item);
        const cellSx = item.has_override ? {} : inheritedCellSx;
        return (
            <>
                <TableCell sx={cellSx}>{item.name}</TableCell>
                <TableCell sx={cellSx}>{item.description}</TableCell>
                <TableCell>
                    <Switch
                        checked={display.enabled}
                        size="small"
                        disabled
                        sx={!item.has_override ? { opacity: 0.6 } : undefined}
                    />
                </TableCell>
                <TableCell sx={cellSx}>{display.interval}s</TableCell>
                <TableCell sx={cellSx}>{display.retention} days</TableCell>
            </>
        );
    };

    const containedButtonSx = getContainedButtonSx(theme);

    /**
     * Render the probe-specific edit dialog. Saving wires success
     * and failure outcomes back through the helpers from the shared
     * panel so the user sees a single error/success banner row.
     */
    const renderEditDialog = (helpers: ScopedOverridesEditHelpers) => {
        const handleSaveOverride = async () => {
            if (!editOverride) {
                return;
            }
            const intervalNum = parseInt(editInterval, 10);
            if (Number.isNaN(intervalNum) || intervalNum < 1) {
                helpers.onSaveError(
                    new Error('Collection interval must be a positive integer.'),
                );
                return;
            }
            const retentionNum = parseInt(editRetention, 10);
            if (Number.isNaN(retentionNum) || retentionNum < 1) {
                helpers.onSaveError(
                    new Error('Retention days must be a positive integer.'),
                );
                return;
            }
            try {
                setSaving(true);
                await apiPut(
                    `${API_BASE_URL}/probe-overrides/${scope}/${String(scopeId)}/${editOverride.name}`,
                    {
                        is_enabled: editEnabled,
                        collection_interval_seconds: intervalNum,
                        retention_days: retentionNum,
                    },
                );
                setEditOpen(false);
                helpers.onSaveSuccess(
                    `Override for "${editOverride.name}" saved successfully.`,
                );
                helpers.refresh();
            } catch (err: unknown) {
                // Surface the failure on the shared banner so the
                // user sees it even after the dialog closes.
                helpers.onSaveError(err);
            } finally {
                setSaving(false);
            }
        };

        return (
            <Dialog
                open={editOpen}
                onClose={() => {
                    if (!saving) {
                        setEditOpen(false);
                    }
                }}
                maxWidth="xs"
                fullWidth
            >
                <DialogTitle sx={dialogTitleSx}>
                    Edit override: {editOverride?.name}
                </DialogTitle>
                <DialogContent>
                    <Box sx={{ display: 'flex', alignItems: 'center', mt: 1, mb: 2 }}>
                        <Typography sx={{ flex: 1 }}>Enabled</Typography>
                        <Switch
                            checked={editEnabled}
                            onChange={(e) => {
                                setEditEnabled(e.target.checked);
                            }}
                            disabled={saving}
                        />
                    </Box>
                    <TextField
                        label="Collection Interval (seconds)"
                        type="number"
                        fullWidth
                        margin="dense"
                        value={editInterval}
                        onChange={(e) => {
                            setEditInterval(e.target.value);
                        }}
                        disabled={saving}
                        inputProps={{ min: 1 }}
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    />
                    <TextField
                        label="Retention Days"
                        type="number"
                        fullWidth
                        margin="dense"
                        value={editRetention}
                        onChange={(e) => {
                            setEditRetention(e.target.value);
                        }}
                        disabled={saving}
                        inputProps={{ min: 1 }}
                        InputLabelProps={{ shrink: true }}
                        sx={SELECT_FIELD_SX}
                    />
                </DialogContent>
                <DialogActions sx={dialogActionsSx}>
                    <Button
                        onClick={() => {
                            setEditOpen(false);
                        }}
                        disabled={saving}
                    >
                        Cancel
                    </Button>
                    <Button
                        onClick={() => {
                            void handleSaveOverride();
                        }}
                        variant="contained"
                        disabled={saving}
                        sx={containedButtonSx}
                    >
                        {saving ? (
                            <CircularProgress
                                size={20}
                                color="inherit"
                                aria-label="Saving"
                            />
                        ) : (
                            'Save'
                        )}
                    </Button>
                </DialogActions>
            </Dialog>
        );
    };

    return (
        <ScopedOverridesPanel<ProbeOverride>
            scope={scope}
            scopeId={scopeId}
            apiBasePath={`${API_BASE_URL}/probe-overrides`}
            itemKey={(item) => item.name}
            itemDisplayName={(item) => item.name}
            hasOverride={(item) => item.has_override}
            columns={PROBE_COLUMNS}
            renderRowCells={renderRowCells}
            emptyMessage="No probe configurations found."
            loadingLabel="Loading probe overrides"
            onEditRequested={handleEditRequested}
            renderEditDialog={renderEditDialog}
        />
    );
};

export default ProbeOverridesPanel;

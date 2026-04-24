/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog for creating manual blackout periods
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useEffect, useRef, useMemo } from 'react';
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    TextField,
    Button,
    Box,
    Typography,
    Alert,
    CircularProgress,
    Chip,
    RadioGroup,
    FormControlLabel,
    Radio,
    alpha,
    ToggleButton,
    ToggleButtonGroup,
} from '@mui/material';
import { useTheme, type Theme } from '@mui/material/styles';
import {
    Language as EstateIcon,
    FolderSpecial as GroupIcon,
    Dns as ClusterIcon,
    Storage as ServerIcon,
} from '@mui/icons-material';
import { useBlackouts } from '../contexts/useBlackouts';
import type { CreateBlackoutRequest } from '../contexts/BlackoutContext';
import type { Selection } from '../types/selection';
import { SELECT_FIELD_SX } from './shared/formStyles';

// ---- Style constants ----

const dialogPaperSx = {
    borderRadius: 2,
};

const dialogTitleSx = {
    fontWeight: 600,
    color: 'text.primary',
    pb: 1,
};

const sectionLabelSx = {
    color: 'text.secondary',
    mb: 1,
    mt: 2,
    textTransform: 'uppercase',
    fontSize: '0.875rem',
    letterSpacing: '0.05em',
};

const textFieldSx = {
    '& .MuiOutlinedInput-root': {
        borderRadius: 1,
        '&:hover .MuiOutlinedInput-notchedOutline': {
            borderColor: 'grey.400',
        },
        '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
            borderColor: 'primary.main',
            borderWidth: 2,
        },
    },
    '& .MuiInputLabel-root.Mui-focused': {
        color: 'primary.main',
    },
};

const cancelButtonSx = {
    color: 'text.secondary',
    textTransform: 'none',
    fontWeight: 500,
};

const getSaveButtonSx = (theme: Theme) => ({
    textTransform: 'none',
    fontWeight: 600,
    minWidth: 80,
    background: theme.palette.primary.main,
    boxShadow: '0 4px 14px 0 rgba(14, 165, 233, 0.39)',
    '&:hover': {
        background: theme.palette.primary.dark,
        boxShadow: '0 6px 20px 0 rgba(14, 165, 233, 0.5)',
    },
    '&.Mui-disabled': {
        background: theme.palette.grey[200],
        color: theme.palette.grey[400],
    },
});

const dialogActionsSx = {
    px: 3,
    pb: 2,
};

const DURATION_PRESETS = [
    { label: '30m', minutes: 30 },
    { label: '1h', minutes: 60 },
    { label: '2h', minutes: 120 },
    { label: '4h', minutes: 240 },
    { label: '8h', minutes: 480 },
];

const SCOPE_OPTIONS = [
    { value: 'estate', label: 'Estate', icon: EstateIcon },
    { value: 'group', label: 'Group', icon: GroupIcon },
    { value: 'cluster', label: 'Cluster', icon: ClusterIcon },
    { value: 'server', label: 'Server', icon: ServerIcon },
];

const extractNumericId = (prefixedId: string | number | undefined | null): number | undefined => {
    if (prefixedId == null) {return undefined;}
    if (typeof prefixedId === 'number') {return prefixedId;}
    const match = prefixedId.match(/(\d+)$/);
    return match ? parseInt(match[1], 10) : undefined;
};

const DB_BACKED_GROUP = /^group-\d+$/;
const DB_BACKED_CLUSTER = /^cluster-\d+$/;

const isScopeAvailable = (
    scopeValue: string,
    selection: Selection | null | undefined,
): boolean => {
    if (!selection) {return scopeValue === 'estate';}

    if (scopeValue === 'estate') {return true;}

    if (scopeValue === 'group') {
        if (selection.type === 'server' || selection.type === 'cluster') {
            const gid = selection.groupId;
            return !!gid && DB_BACKED_GROUP.test(gid);
        }
        return false;
    }

    if (scopeValue === 'cluster') {
        if (selection.type === 'server') {
            const cid = selection.clusterId;
            return !!cid && DB_BACKED_CLUSTER.test(cid)
                && !selection.isStandalone;
        }
        if (selection.type === 'cluster') {
            return !!selection.id && DB_BACKED_CLUSTER.test(String(selection.id));
        }
        return false;
    }

    if (scopeValue === 'server') {
        return selection.type === 'server';
    }

    return false;
};

const scopeLabel = (
    scopeValue: string,
    selection: Selection | null | undefined,
): string | undefined => {
    if (!selection) {return undefined;}

    if (scopeValue === 'estate') {return 'All Servers';}

    if (scopeValue === 'group') {
        if (selection.type === 'server' || selection.type === 'cluster') {
            return selection.groupName || undefined;
        }
        return undefined;
    }

    if (scopeValue === 'cluster') {
        if (selection.type === 'server') {
            return selection.clusterName || undefined;
        }
        if (selection.type === 'cluster') {
            return selection.name || undefined;
        }
        return undefined;
    }

    if (scopeValue === 'server') {
        return selection.type === 'server'
            ? selection.name || undefined
            : undefined;
    }

    return undefined;
};

const resolveEntityId = (
    scopeValue: string,
    selection: Selection | null | undefined,
): number | undefined => {
    if (!selection) {return undefined;}

    if (scopeValue === 'server') {
        if (selection.type === 'server') {
            return extractNumericId(selection.id);
        }
        return undefined;
    }
    if (scopeValue === 'cluster') {
        if (selection.type === 'server') {
            return extractNumericId(selection.clusterId);
        }
        if (selection.type === 'cluster') {
            return extractNumericId(selection.id);
        }
        return undefined;
    }
    if (scopeValue === 'group') {
        if (selection.type === 'server' || selection.type === 'cluster') {
            return extractNumericId(selection.groupId);
        }
        return undefined;
    }
    return undefined;
};

// ---- Component ----

interface BlackoutDialogProps {
    open: boolean;
    onClose: () => void;
    onSuccess?: () => void;
    selection?: Selection | null;
}

/**
 * BlackoutDialog - Dialog for creating manual blackout periods
 */
const BlackoutDialog: React.FC<BlackoutDialogProps> = ({
    open,
    onClose,
    onSuccess,
    selection,
}) => {
    const theme = useTheme();
    const { createBlackout } = useBlackouts();

    const [scope, setScope] = useState('server');
    const [mode, setMode] = useState<'now' | 'future'>('now');
    const [selectedPreset, setSelectedPreset] = useState<number | null>(60);
    const [customHours, setCustomHours] = useState('');
    const [customMinutes, setCustomMinutes] = useState('');
    const [startDateTime, setStartDateTime] = useState('');
    const [endDateTime, setEndDateTime] = useState('');
    const [reason, setReason] = useState('');
    const [error, setError] = useState('');
    const [isSaving, setIsSaving] = useState(false);
    const prevOpenRef = useRef(false);

    // Reset form only when dialog opens (false -> true transition)
    useEffect(() => {
        if (open && !prevOpenRef.current) {
            const selType = selection?.type || 'server';
            const preferred = isScopeAvailable(selType, selection)
                ? selType
                : ['server', 'cluster', 'group', 'estate']
                    .find(s => isScopeAvailable(s, selection)) || 'estate';
            setScope(preferred);
            setMode('now');
            setSelectedPreset(60);
            setCustomHours('');
            setCustomMinutes('');
            setStartDateTime('');
            setEndDateTime('');
            setReason('');
            setError('');
        }
        prevOpenRef.current = open;
    }, [open, selection]);

    // Compute end time for "Start Now" mode
    const computedEndTime = useMemo(() => {
        let totalMinutes = 0;
        if (selectedPreset !== null) {
            totalMinutes = selectedPreset;
        } else {
            const h = parseInt(customHours, 10) || 0;
            const m = parseInt(customMinutes, 10) || 0;
            totalMinutes = h * 60 + m;
        }
        if (totalMinutes <= 0) {return null;}
        const end = new Date(Date.now() + totalMinutes * 60000);
        return end;
    }, [selectedPreset, customHours, customMinutes]);

    const computedEndTimeLabel = useMemo(() => {
        if (!computedEndTime) {return '';}
        return computedEndTime.toLocaleString(undefined, {
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
        });
    }, [computedEndTime]);

    // Validate form
    const isValid = useMemo(() => {
        if (mode === 'now') {
            if (selectedPreset !== null) {return true;}
            const h = parseInt(customHours, 10) || 0;
            const m = parseInt(customMinutes, 10) || 0;
            return (h * 60 + m) > 0;
        }
        return !!startDateTime && !!endDateTime;
    }, [mode, selectedPreset, customHours, customMinutes, startDateTime, endDateTime]);

    const handlePresetClick = (minutes: number) => {
        setSelectedPreset(minutes);
        setCustomHours('');
        setCustomMinutes('');
    };

    const handleCustomChange = (field: 'hours' | 'minutes', value: string) => {
        setSelectedPreset(null);
        if (field === 'hours') {setCustomHours(value);}
        else {setCustomMinutes(value);}
    };

    const handleSubmit = async () => {
        setError('');
        setIsSaving(true);

        try {
            let startTime: string;
            let endTime: string;

            if (mode === 'now') {
                if (computedEndTime === null) {
                    setError('Please select a valid duration.');
                    setIsSaving(false);
                    return;
                }
                startTime = new Date().toISOString();
                endTime = computedEndTime.toISOString();
            } else {
                startTime = new Date(startDateTime).toISOString();
                endTime = new Date(endDateTime).toISOString();
            }

            const entityId = resolveEntityId(scope, selection);
            const payload: CreateBlackoutRequest = {
                scope: scope as CreateBlackoutRequest['scope'],
                start_time: startTime,
                end_time: endTime,
                reason: reason.trim(),
                ...(scope === 'group' && entityId != null && { group_id: entityId }),
                ...(scope === 'cluster' && entityId != null && { cluster_id: entityId }),
                ...(scope === 'server' && entityId != null && { connection_id: entityId }),
            };

            await createBlackout(payload);

            onSuccess?.();
            onClose();
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : 'Failed to create blackout');
        } finally {
            setIsSaving(false);
        }
    };

    const handleClose = () => {
        if (!isSaving) {onClose();}
    };

    // Scope chip styles
    const scopeChipSx = useMemo(() => ({
        height: 20,
        fontSize: '0.875rem',
        fontWeight: 500,
        bgcolor: alpha(theme.palette.primary.main, 0.1),
        color: theme.palette.primary.main,
        '& .MuiChip-label': { px: 0.75 },
    }), [theme]);

    const presetChipSx = (isSelected: boolean) => ({
        fontWeight: isSelected ? 700 : 500,
        bgcolor: isSelected
            ? alpha(theme.palette.primary.main, 0.15)
            : alpha(theme.palette.grey[500], 0.08),
        color: isSelected ? theme.palette.primary.main : 'text.secondary',
        borderColor: isSelected
            ? alpha(theme.palette.primary.main, 0.4)
            : 'transparent',
        border: '1px solid',
        cursor: 'pointer',
        '&:hover': {
            bgcolor: isSelected
                ? alpha(theme.palette.primary.main, 0.2)
                : alpha(theme.palette.grey[500], 0.15),
        },
    });

    const modeToggleSx = useMemo(() => ({
        '& .MuiToggleButton-root': {
            textTransform: 'none',
            fontSize: '1rem',
            fontWeight: 500,
            px: 2,
            py: 0.5,
            '&.Mui-selected': {
                bgcolor: alpha(theme.palette.primary.main, 0.12),
                color: theme.palette.primary.main,
                fontWeight: 600,
                '&:hover': {
                    bgcolor: alpha(theme.palette.primary.main, 0.18),
                },
            },
        },
    }), [theme]);

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="sm"
            fullWidth
            PaperProps={{ sx: dialogPaperSx }}
        >
            <DialogTitle sx={dialogTitleSx}>
                Start blackout
            </DialogTitle>

            <DialogContent>
                {error && (
                    <Alert
                        severity="error"
                        sx={{ mb: 2, borderRadius: 1 }}
                        onClose={() => setError('')}
                    >
                        {error}
                    </Alert>
                )}

                {/* Scope selector */}
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Scope
                </Typography>
                <RadioGroup
                    value={scope}
                    onChange={(e) => setScope(e.target.value)}
                >
                    {SCOPE_OPTIONS.map((opt) => {
                        const Icon = opt.icon;
                        const available = isScopeAvailable(opt.value, selection);
                        const chipLabel = scopeLabel(opt.value, selection);
                        return (
                            <Box key={opt.value} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                                <FormControlLabel
                                    value={opt.value}
                                    control={<Radio size="small" disabled={isSaving || !available} />}
                                    label={
                                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                                            <Icon sx={{ fontSize: 16, color: available ? 'text.secondary' : 'text.disabled' }} />
                                            <Typography sx={{ fontSize: '1rem', color: available ? 'text.primary' : 'text.disabled' }}>
                                                {opt.label}
                                            </Typography>
                                        </Box>
                                    }
                                    sx={{ mr: 0 }}
                                />
                                {scope === opt.value && chipLabel && (
                                    <Chip label={chipLabel} size="small" sx={scopeChipSx} />
                                )}
                            </Box>
                        );
                    })}
                </RadioGroup>

                {/* Mode toggle */}
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Timing
                </Typography>
                <ToggleButtonGroup
                    value={mode}
                    exclusive
                    onChange={(_, val) => val && setMode(val)}
                    size="small"
                    sx={modeToggleSx}
                >
                    <ToggleButton value="now" disabled={isSaving}>
                        Start Now
                    </ToggleButton>
                    <ToggleButton value="future" disabled={isSaving}>
                        Schedule Future
                    </ToggleButton>
                </ToggleButtonGroup>

                {/* Start Now mode */}
                {mode === 'now' && (
                    <Box sx={{ mt: 2 }}>
                        <Typography sx={{ fontSize: '0.875rem', color: 'text.secondary', mb: 1 }}>
                            Duration
                        </Typography>
                        <Box sx={{ display: 'flex', gap: 0.75, flexWrap: 'wrap', mb: 2 }}>
                            {DURATION_PRESETS.map((preset) => (
                                <Chip
                                    key={preset.minutes}
                                    label={preset.label}
                                    size="small"
                                    onClick={() => handlePresetClick(preset.minutes)}
                                    sx={presetChipSx(selectedPreset === preset.minutes)}
                                />
                            ))}
                        </Box>
                        <Box sx={{ display: 'flex', gap: 1.5, alignItems: 'center' }}>
                            <TextField
                                label="Hours"
                                type="number"
                                size="small"
                                value={customHours}
                                onChange={(e) => handleCustomChange('hours', e.target.value)}
                                disabled={isSaving}
                                inputProps={{ min: 0, max: 72 }}
                                InputLabelProps={{ shrink: true }}
                                sx={{
                                    width: 100,
                                    ...textFieldSx,
                                    ...SELECT_FIELD_SX,
                                }}
                            />
                            <TextField
                                label="Minutes"
                                type="number"
                                size="small"
                                value={customMinutes}
                                onChange={(e) => handleCustomChange('minutes', e.target.value)}
                                disabled={isSaving}
                                inputProps={{ min: 0, max: 59 }}
                                InputLabelProps={{ shrink: true }}
                                sx={{
                                    width: 100,
                                    ...textFieldSx,
                                    ...SELECT_FIELD_SX,
                                }}
                            />
                        </Box>
                        {computedEndTimeLabel && (
                            <Typography sx={{ fontSize: '0.875rem', color: 'text.disabled', mt: 1 }}>
                                Ends at {computedEndTimeLabel}
                            </Typography>
                        )}
                    </Box>
                )}

                {/* Schedule Future mode */}
                {mode === 'future' && (
                    <Box sx={{ mt: 2, display: 'flex', flexDirection: 'column', gap: 2 }}>
                        <TextField
                            label="Start Date/Time"
                            type="datetime-local"
                            size="small"
                            fullWidth
                            value={startDateTime}
                            onChange={(e) => setStartDateTime(e.target.value)}
                            disabled={isSaving}
                            InputLabelProps={{ shrink: true }}
                            sx={textFieldSx}
                        />
                        <TextField
                            label="End Date/Time"
                            type="datetime-local"
                            size="small"
                            fullWidth
                            value={endDateTime}
                            onChange={(e) => setEndDateTime(e.target.value)}
                            disabled={isSaving}
                            InputLabelProps={{ shrink: true }}
                            sx={textFieldSx}
                        />
                    </Box>
                )}

                {/* Reason */}
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Reason
                </Typography>
                <TextField
                    fullWidth
                    label="Reason"
                    placeholder="e.g., Scheduled maintenance window, Deployment in progress"
                    multiline
                    rows={2}
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    disabled={isSaving}
                    size="small"
                    InputLabelProps={{ shrink: true }}
                    sx={{ ...textFieldSx, ...SELECT_FIELD_SX }}
                />
            </DialogContent>

            <DialogActions sx={dialogActionsSx}>
                <Button
                    onClick={handleClose}
                    disabled={isSaving}
                    sx={cancelButtonSx}
                >
                    Cancel
                </Button>
                <Button
                    variant="contained"
                    onClick={handleSubmit}
                    disabled={!isValid || isSaving}
                    sx={getSaveButtonSx(theme)}
                >
                    {isSaving ? (
                        <CircularProgress size={20} sx={{ color: 'inherit' }} aria-label="Saving" />
                    ) : (
                        'Create Blackout'
                    )}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default BlackoutDialog;

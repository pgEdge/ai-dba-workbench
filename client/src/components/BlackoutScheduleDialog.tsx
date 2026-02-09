/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog for creating and editing recurring blackout schedules
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useMemo } from 'react';
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
    Switch,
    alpha,
} from '@mui/material';
import { useTheme, Theme } from '@mui/material/styles';
import {
    Language as EstateIcon,
    FolderSpecial as GroupIcon,
    Dns as ClusterIcon,
    Storage as ServerIcon,
} from '@mui/icons-material';
import { useBlackouts } from '../contexts/BlackoutContext';

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
    fontSize: '0.75rem',
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

// ---- Cron presets ----

interface CronPreset {
    label: string;
    cron: string;
    description: string;
}

const CRON_PRESETS: CronPreset[] = [
    { label: 'Daily', cron: '0 {H} * * *', description: 'Every day' },
    { label: 'Weekdays', cron: '0 {H} * * 1-5', description: 'Monday through Friday' },
    { label: 'Weekends', cron: '0 {H} * * 0,6', description: 'Saturday and Sunday' },
    { label: 'Weekly', cron: '0 {H} * * 0', description: 'Every Sunday' },
    { label: 'Monthly', cron: '0 {H} 1 * *', description: 'First day of each month' },
];

const DURATION_PRESETS = [
    { label: '30m', minutes: 30 },
    { label: '1h', minutes: 60 },
    { label: '2h', minutes: 120 },
    { label: '4h', minutes: 240 },
];

const SCOPE_OPTIONS = [
    { value: 'estate', label: 'Estate', icon: EstateIcon },
    { value: 'group', label: 'Group', icon: GroupIcon },
    { value: 'cluster', label: 'Cluster', icon: ClusterIcon },
    { value: 'server', label: 'Server', icon: ServerIcon },
];

/**
 * Build a cron expression from a preset template, substituting
 * the hour and minute placeholders.
 */
const buildCron = (template: string, hour: string, minute: string): string => {
    return template
        .replace('{H}', hour || '0')
        .replace(/^0 /, `${minute || '0'} `);
};

/**
 * Describe a cron expression in human-readable form.
 */
const describeCron = (cron: string): string => {
    if (!cron || !cron.trim()) {return '';}
    const parts = cron.trim().split(/\s+/);
    if (parts.length !== 5) {return 'Invalid cron expression';}

    const [min, hour, dom, , dow] = parts;

    let timeStr = '';
    const h = parseInt(hour, 10);
    const m = parseInt(min, 10);
    if (!isNaN(h) && !isNaN(m)) {
        const ampm = h >= 12 ? 'PM' : 'AM';
        const h12 = h === 0 ? 12 : h > 12 ? h - 12 : h;
        timeStr = `at ${h12}:${m.toString().padStart(2, '0')} ${ampm}`;
    } else {
        timeStr = `at ${hour}:${min}`;
    }

    if (dow === '1-5') {return `Weekdays ${timeStr}`;}
    if (dow === '0,6') {return `Weekends ${timeStr}`;}
    if (dow === '0') {return `Every Sunday ${timeStr}`;}
    if (dom === '1' && dow === '*') {return `First of each month ${timeStr}`;}
    if (dom === '*' && dow === '*') {return `Daily ${timeStr}`;}

    return `${cron} (${timeStr})`;
};

const extractNumericId = (prefixedId: string | number | undefined | null): number | undefined => {
    if (prefixedId == null) {return undefined;}
    if (typeof prefixedId === 'number') {return prefixedId;}
    const match = prefixedId.match(/(\d+)$/);
    return match ? parseInt(match[1], 10) : undefined;
};

// ---- Component ----

interface BlackoutSchedule {
    id?: number;
    scope?: string;
    name?: string;
    cron_expression?: string;
    duration_minutes?: number;
    timezone?: string;
    reason?: string;
    enabled?: boolean;
}

interface BlackoutScheduleDialogProps {
    open: boolean;
    onClose: () => void;
    onSuccess?: () => void;
    selection?: Record<string, unknown> | null;
    schedule?: BlackoutSchedule | null;
}

/**
 * BlackoutScheduleDialog - Dialog for creating/editing recurring
 * blackout schedules with a cron builder interface.
 */
const BlackoutScheduleDialog: React.FC<BlackoutScheduleDialogProps> = ({
    open,
    onClose,
    onSuccess,
    selection,
    schedule,
}) => {
    const theme = useTheme();
    const { createSchedule, updateSchedule } = useBlackouts();
    const isEdit = !!schedule?.id;

    const [scope, setScope] = useState('server');
    const [name, setName] = useState('');
    const [selectedPreset, setSelectedPreset] = useState<string | null>('Daily');
    const [customCron, setCustomCron] = useState('');
    const [isCustom, setIsCustom] = useState(false);
    const [hour, setHour] = useState('2');
    const [minute, setMinute] = useState('0');
    const [durationMinutes, setDurationMinutes] = useState(60);
    const [customDuration, setCustomDuration] = useState('');
    const [timezone, setTimezone] = useState(
        Intl.DateTimeFormat().resolvedOptions().timeZone
    );
    const [reason, setReason] = useState('');
    const [enabled, setEnabled] = useState(true);
    const [error, setError] = useState('');
    const [nameError, setNameError] = useState('');
    const [isSaving, setIsSaving] = useState(false);

    // Reset form when dialog opens
    useEffect(() => {
        if (open) {
            if (isEdit && schedule) {
                setScope(schedule.scope || 'server');
                setName(schedule.name || '');
                setReason(schedule.reason || '');
                setEnabled(schedule.enabled !== false);
                setDurationMinutes(schedule.duration_minutes || 60);
                setCustomDuration('');
                setTimezone(schedule.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone);

                // Parse existing cron
                if (schedule.cron_expression) {
                    setIsCustom(true);
                    setCustomCron(schedule.cron_expression);
                    setSelectedPreset(null);
                    // Try to extract hour/minute from cron
                    const parts = schedule.cron_expression.trim().split(/\s+/);
                    if (parts.length >= 2) {
                        setMinute(parts[0]);
                        setHour(parts[1]);
                    }
                }
            } else {
                const selType = (selection?.type as string) || 'server';
                setScope(selType);
                setName('');
                setSelectedPreset('Daily');
                setIsCustom(false);
                setCustomCron('');
                setHour('2');
                setMinute('0');
                setDurationMinutes(60);
                setCustomDuration('');
                setTimezone(Intl.DateTimeFormat().resolvedOptions().timeZone);
                setReason('');
                setEnabled(true);
                setError('');
                setNameError('');
            }
        }
    }, [open, schedule, isEdit, selection]);

    // Compute the final cron expression
    const cronExpression = useMemo(() => {
        if (isCustom) {return customCron;}
        if (!selectedPreset) {return '';}
        const preset = CRON_PRESETS.find((p) => p.label === selectedPreset);
        if (!preset) {return '';}
        return buildCron(preset.cron, hour, minute);
    }, [isCustom, customCron, selectedPreset, hour, minute]);

    const cronDescription = useMemo(() => describeCron(cronExpression), [cronExpression]);

    // Effective duration
    const effectiveDuration = useMemo(() => {
        if (customDuration) {
            const val = parseInt(customDuration, 10);
            return isNaN(val) || val <= 0 ? 0 : val;
        }
        return durationMinutes;
    }, [durationMinutes, customDuration]);

    // Validation
    const isValid = useMemo(() => {
        if (!name.trim()) {return false;}
        if (!cronExpression.trim()) {return false;}
        if (effectiveDuration <= 0) {return false;}
        return true;
    }, [name, cronExpression, effectiveDuration]);

    const handlePresetClick = (presetLabel: string) => {
        setSelectedPreset(presetLabel);
        setIsCustom(false);
        setCustomCron('');
    };

    const handleCustomClick = () => {
        setIsCustom(true);
        setSelectedPreset(null);
        if (!customCron) {
            setCustomCron(cronExpression || '0 2 * * *');
        }
    };

    const handleDurationPresetClick = (minutes: number) => {
        setDurationMinutes(minutes);
        setCustomDuration('');
    };

    const handleSubmit = async () => {
        setError('');
        setNameError('');

        if (!name.trim()) {
            setNameError('Name is required');
            return;
        }

        setIsSaving(true);

        try {
            const payload: Record<string, unknown> = {
                scope,
                name: name.trim(),
                cron_expression: cronExpression.trim(),
                duration_minutes: effectiveDuration,
                timezone: timezone.trim(),
                reason: reason.trim(),
                enabled,
            };
            const entityId = extractNumericId(selection?.id as string | number | undefined);
            if (scope === 'group' && entityId != null) {payload.group_id = entityId;}
            if (scope === 'cluster' && entityId != null) {payload.cluster_id = entityId;}
            if (scope === 'server' && entityId != null) {payload.connection_id = entityId;}

            if (isEdit && schedule?.id) {
                await updateSchedule(schedule.id, payload);
            } else {
                await createSchedule(payload);
            }

            onSuccess?.();
            onClose();
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : 'Failed to save schedule');
        } finally {
            setIsSaving(false);
        }
    };

    const handleClose = () => {
        if (!isSaving) {onClose();}
    };

    // Styles
    const scopeChipSx = useMemo(() => ({
        height: 20,
        fontSize: '0.6875rem',
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

    const cronFieldSx = useMemo(() => ({
        ...textFieldSx,
        '& .MuiOutlinedInput-root': {
            ...textFieldSx['& .MuiOutlinedInput-root'],
            fontFamily: '"JetBrains Mono", "SF Mono", monospace',
            fontSize: '0.8125rem',
        },
    }), []);

    const selectionName = (selection?.name as string) || '';

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="sm"
            fullWidth
            PaperProps={{ sx: dialogPaperSx }}
        >
            <DialogTitle sx={dialogTitleSx}>
                {isEdit ? 'Edit Blackout Schedule' : 'Create Blackout Schedule'}
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
                        return (
                            <Box key={opt.value} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                                <FormControlLabel
                                    value={opt.value}
                                    control={<Radio size="small" disabled={isSaving} />}
                                    label={
                                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                                            <Icon sx={{ fontSize: 16, color: 'text.secondary' }} />
                                            <Typography sx={{ fontSize: '0.875rem' }}>
                                                {opt.label}
                                            </Typography>
                                        </Box>
                                    }
                                    sx={{ mr: 0 }}
                                />
                                {scope === opt.value && selectionName && (
                                    <Chip label={selectionName} size="small" sx={scopeChipSx} />
                                )}
                            </Box>
                        );
                    })}
                </RadioGroup>

                {/* Name */}
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Name
                </Typography>
                <TextField
                    fullWidth
                    placeholder="e.g., Nightly maintenance window"
                    value={name}
                    onChange={(e) => {
                        setName(e.target.value);
                        if (nameError) {setNameError('');}
                    }}
                    error={!!nameError}
                    helperText={nameError}
                    disabled={isSaving}
                    required
                    size="small"
                    sx={textFieldSx}
                />

                {/* Cron builder */}
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Recurrence
                </Typography>
                <Box sx={{ display: 'flex', gap: 0.75, flexWrap: 'wrap', mb: 1.5 }}>
                    {CRON_PRESETS.map((preset) => (
                        <Chip
                            key={preset.label}
                            label={preset.label}
                            size="small"
                            onClick={() => handlePresetClick(preset.label)}
                            sx={presetChipSx(!isCustom && selectedPreset === preset.label)}
                        />
                    ))}
                    <Chip
                        label="Custom"
                        size="small"
                        onClick={handleCustomClick}
                        sx={presetChipSx(isCustom)}
                    />
                </Box>

                {/* Time of day (for presets) */}
                {!isCustom && (
                    <Box sx={{ display: 'flex', gap: 1.5, mb: 1.5 }}>
                        <TextField
                            label="Hour"
                            type="number"
                            size="small"
                            value={hour}
                            onChange={(e) => setHour(e.target.value)}
                            disabled={isSaving}
                            inputProps={{ min: 0, max: 23 }}
                            sx={(sxTheme: Theme) => ({
                                width: 90,
                                ...textFieldSx,
                                '& input[type=number]': {
                                    colorScheme: sxTheme.palette.mode === 'dark' ? 'dark' : 'light',
                                },
                            })}
                        />
                        <TextField
                            label="Minute"
                            type="number"
                            size="small"
                            value={minute}
                            onChange={(e) => setMinute(e.target.value)}
                            disabled={isSaving}
                            inputProps={{ min: 0, max: 59 }}
                            sx={(sxTheme: Theme) => ({
                                width: 90,
                                ...textFieldSx,
                                '& input[type=number]': {
                                    colorScheme: sxTheme.palette.mode === 'dark' ? 'dark' : 'light',
                                },
                            })}
                        />
                    </Box>
                )}

                {/* Cron expression display/edit */}
                <TextField
                    fullWidth
                    label="Cron Expression"
                    value={isCustom ? customCron : cronExpression}
                    onChange={(e) => isCustom && setCustomCron(e.target.value)}
                    InputProps={{ readOnly: !isCustom }}
                    disabled={isSaving}
                    size="small"
                    sx={cronFieldSx}
                />
                {cronDescription && (
                    <Typography sx={{ fontSize: '0.6875rem', color: 'text.disabled', mt: 0.5 }}>
                        {cronDescription}
                    </Typography>
                )}

                {/* Duration */}
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Duration
                </Typography>
                <Box sx={{ display: 'flex', gap: 0.75, flexWrap: 'wrap', mb: 2 }}>
                    {DURATION_PRESETS.map((preset) => (
                        <Chip
                            key={preset.minutes}
                            label={preset.label}
                            size="small"
                            onClick={() => handleDurationPresetClick(preset.minutes)}
                            sx={presetChipSx(!customDuration && durationMinutes === preset.minutes)}
                        />
                    ))}
                </Box>
                <TextField
                    label="Custom (minutes)"
                    type="number"
                    size="small"
                    value={customDuration}
                    onChange={(e) => setCustomDuration(e.target.value)}
                    disabled={isSaving}
                    placeholder="e.g., 90"
                    InputLabelProps={{ shrink: true }}
                    inputProps={{ min: 1 }}
                    sx={(sxTheme: Theme) => ({
                        width: 200,
                        ...textFieldSx,
                        '& input[type=number]': {
                            colorScheme: sxTheme.palette.mode === 'dark' ? 'dark' : 'light',
                        },
                    })}
                />

                {/* Timezone */}
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Timezone
                </Typography>
                <TextField
                    fullWidth
                    value={timezone}
                    onChange={(e) => setTimezone(e.target.value)}
                    disabled={isSaving}
                    size="small"
                    placeholder="e.g., America/New_York, UTC, Europe/London"
                    sx={textFieldSx}
                />

                {/* Reason */}
                <Typography variant="subtitle2" sx={sectionLabelSx}>
                    Reason
                </Typography>
                <TextField
                    fullWidth
                    placeholder="e.g., Nightly backup window"
                    multiline
                    rows={2}
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    disabled={isSaving}
                    size="small"
                    sx={textFieldSx}
                />

                {/* Enabled toggle */}
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 2 }}>
                    <Switch
                        checked={enabled}
                        onChange={(e) => setEnabled(e.target.checked)}
                        disabled={isSaving}
                        size="small"
                    />
                    <Typography sx={{ fontSize: '0.875rem', color: 'text.primary' }}>
                        {enabled ? 'Enabled' : 'Disabled'}
                    </Typography>
                </Box>
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
                        <CircularProgress size={20} sx={{ color: 'inherit' }} />
                    ) : (
                        isEdit ? 'Update Blackout Schedule' : 'Create Blackout Schedule'
                    )}
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default BlackoutScheduleDialog;

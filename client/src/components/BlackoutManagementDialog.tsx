/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog for managing blackouts and schedules
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useMemo } from 'react';
import {
    Box,
    Stack,
    Typography,
    Chip,
    alpha,
    IconButton,
    Tooltip,
    Button,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    DarkMode as MoonIcon,
    PauseCircle as PauseIcon,
    Delete as DeleteIcon,
    Stop as StopIcon,
    Schedule as ClockIcon,
    Person as PersonIcon,
    Language as EstateIcon,
    FolderSpecial as GroupIcon,
    Dns as ClusterIcon,
    Storage as ServerIcon,
    Repeat as RepeatIcon,
    Close as CloseIcon,
    Add as AddIcon,
} from '@mui/icons-material';
import { useBlackouts } from '../contexts/useBlackouts';
import type { Selection } from '../types/selection';
import BlackoutDialog from './BlackoutDialog';
import BlackoutScheduleDialog from './BlackoutScheduleDialog';
import DeleteConfirmationDialog from './DeleteConfirmationDialog';

// ---- Static style constants ----

const CHIP_LABEL_SX = { px: 0.5 };
const ICON_16_SX = { fontSize: 16 };
const ICON_14_SX = { fontSize: 14 };

const LIST_SX = {
    display: 'flex',
    flexDirection: 'column',
    gap: 0.5,
};

const BANNER_TITLE_SX = {
    fontWeight: 600,
    fontSize: '1rem',
    lineHeight: 1.2,
};

const BANNER_REASON_SX = {
    fontSize: '0.875rem',
    mt: 0.25,
    wordBreak: 'break-word',
};

const ITEM_REASON_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    mt: 0.25,
    wordBreak: 'break-word',
};

const ITEM_META_SX = {
    color: 'text.disabled',
    fontSize: '0.875rem',
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
};

const SCHEDULE_NAME_SX = {
    fontWeight: 600,
    fontSize: '1rem',
    lineHeight: 1.2,
    color: 'text.primary',
};

const SCHEDULE_CRON_SX = {
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    color: 'text.secondary',
};

const SCHEDULE_DETAIL_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
};

const SECTION_LABEL_SX = {
    fontSize: '0.875rem',
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    color: 'text.disabled',
    mb: 0.75,
};

const DIALOG_PAPER_SX = {
    borderRadius: 2,
};

// ---- Helper functions ----

const getScopeIcon = (scope: string) => {
    switch (scope) {
        case 'estate': return EstateIcon;
        case 'group': return GroupIcon;
        case 'cluster': return ClusterIcon;
        case 'server': return ServerIcon;
        default: return ServerIcon;
    }
};

const getScopeLabel = (scope: string) => {
    switch (scope) {
        case 'estate': return 'Estate';
        case 'group': return 'Group';
        case 'cluster': return 'Cluster';
        case 'server': return 'Server';
        default: return scope;
    }
};

const formatTimeRemaining = (endTime: string): string => {
    const end = new Date(endTime);
    const now = new Date();
    const diffMs = end.getTime() - now.getTime();

    if (diffMs <= 0) {return 'Ending...';}

    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const remainingMins = diffMins % 60;

    if (diffHours > 0) {
        return `${diffHours}h ${remainingMins}m remaining`;
    }
    return `${diffMins}m remaining`;
};

const formatTimeRange = (startTime: string, endTime: string): string => {
    const start = new Date(startTime);
    const end = new Date(endTime);
    const opts: Intl.DateTimeFormatOptions = {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
    };
    return `${start.toLocaleDateString(undefined, opts)} - ${end.toLocaleDateString(undefined, opts)}`;
};

// ---- Props ----

interface BlackoutManagementDialogProps {
    open: boolean;
    onClose: () => void;
    selection: Selection | null;
}

// ---- Main component ----

const BlackoutManagementDialog: React.FC<BlackoutManagementDialogProps> = ({
    open,
    onClose,
    selection,
}) => {
    const theme = useTheme();
    const {
        blackouts,
        schedules,
        activeBlackoutsForSelection,
        stopBlackout,
        deleteBlackout,
        deleteSchedule,
    } = useBlackouts();

    const [blackoutDialogOpen, setBlackoutDialogOpen] = useState(false);
    const [scheduleDialogOpen, setScheduleDialogOpen] = useState(false);
    const [pendingDelete, setPendingDelete] = useState<{ id: number; type: 'blackout' | 'schedule'; isActive?: boolean } | null>(null);
    const [deleteLoading, setDeleteLoading] = useState(false);

    // Filter non-active blackouts
    const nonActiveBlackouts = useMemo(() => {
        const activeIds = new Set(
            (activeBlackoutsForSelection || []).map((b) => b.id)
        );
        return (blackouts || []).filter((b) => !activeIds.has(b.id));
    }, [blackouts, activeBlackoutsForSelection]);

    const activeList = activeBlackoutsForSelection || [];
    const scheduleList = schedules || [];
    const isEmpty = activeList.length === 0 && nonActiveBlackouts.length === 0 && scheduleList.length === 0;

    const amberColor = theme.palette.warning.main;

    // ---- Memoized styles ----

    const activeContainerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        px: 1.25,
        py: 0.75,
        borderRadius: 1,
        bgcolor: alpha(amberColor, 0.12),
        border: '1px solid',
        borderColor: alpha(amberColor, 0.25),
    }), [amberColor]);

    const activeIconSx = useMemo(() => ({
        fontSize: 18,
        color: amberColor,
        flexShrink: 0,
    }), [amberColor]);

    const activeScopeChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        fontWeight: 600,
        textTransform: 'uppercase',
        bgcolor: alpha(amberColor, 0.15),
        color: amberColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [amberColor]);

    const activeTimeChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        fontWeight: 600,
        bgcolor: alpha(amberColor, 0.12),
        color: amberColor,
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [amberColor]);

    const activeStopButtonSx = useMemo(() => ({
        fontSize: '0.875rem',
        textTransform: 'none',
        fontWeight: 600,
        color: amberColor,
        borderColor: alpha(amberColor, 0.4),
        '&:hover': {
            borderColor: amberColor,
            bgcolor: alpha(amberColor, 0.12),
        },
    }), [amberColor]);

    const itemContainerSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1,
        px: 1.25,
        py: 0.75,
        borderRadius: 1,
        bgcolor: alpha(theme.palette.grey[500], 0.10),
        border: '1px solid',
        borderColor: alpha(theme.palette.grey[500], 0.15),
    }), [theme]);

    const itemScopeChipSx = useMemo(() => ({
        height: 16,
        fontSize: '0.875rem',
        bgcolor: alpha(theme.palette.grey[500], 0.15),
        color: 'text.secondary',
        '& .MuiChip-label': CHIP_LABEL_SX,
    }), [theme.palette.grey]);

    const deleteButtonSx = useMemo(() => ({
        p: 0.5,
        color: theme.palette.grey[500],
        '&:hover': {
            bgcolor: alpha(theme.palette.error.main, 0.08),
            color: theme.palette.error.main,
        },
    }), [theme]);

    const splitButtonSx = useMemo(() => ({
        fontSize: '1rem',
        textTransform: 'none',
        fontWeight: 600,
    }), []);

    const handleDeleteConfirm = async () => {
        if (!pendingDelete) {return;}
        setDeleteLoading(true);
        try {
            if (pendingDelete.type === 'blackout') {
                await deleteBlackout(pendingDelete.id);
            } else {
                await deleteSchedule(pendingDelete.id);
            }
        } finally {
            setDeleteLoading(false);
            setPendingDelete(null);
        }
    };

    const deleteDialogMessage = pendingDelete?.type === 'blackout' && pendingDelete?.isActive
        ? 'Are you sure you want to delete this active blackout? This will immediately resume normal alert processing.'
        : pendingDelete?.type === 'schedule'
            ? 'Are you sure you want to delete this schedule? This action cannot be undone.'
            : 'Are you sure you want to delete this blackout? This action cannot be undone.';

    const handleStartBlackout = () => {
        setBlackoutDialogOpen(true);
    };

    return (
        <>
            <Dialog
                open={open}
                onClose={onClose}
                maxWidth="sm"
                fullWidth
                PaperProps={{ sx: DIALOG_PAPER_SX }}
            >
                <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <Typography variant="h6" component="span" sx={{ fontWeight: 600 }}>
                        Blackout management
                    </Typography>
                    <IconButton
                        aria-label="close"
                        onClick={onClose}
                        size="small"
                        sx={{ color: 'text.secondary' }}
                    >
                        <CloseIcon />
                    </IconButton>
                </DialogTitle>

                <DialogContent dividers>
                    {isEmpty ? (
                        <Typography
                            sx={{
                                color: 'text.disabled',
                                fontSize: '0.875rem',
                                py: 4,
                                textAlign: 'center',
                            }}
                        >
                            No blackouts or schedules configured
                        </Typography>
                    ) : (
                        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                            {/* Active blackouts */}
                            {activeList.length > 0 && (
                                <Box>
                                    <Typography sx={SECTION_LABEL_SX}>Active</Typography>
                                    <Box sx={LIST_SX}>
                                        {activeList.map((blackout) => {
                                            const ScopeIcon = getScopeIcon(blackout.scope);
                                            return (
                                                <Box key={blackout.id} sx={activeContainerSx}>
                                                    <MoonIcon sx={activeIconSx} />
                                                    <Box sx={{ flex: 1, minWidth: 0 }}>
                                                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
                                                            <Typography sx={{ ...BANNER_TITLE_SX, color: amberColor }}>
                                                                Blackout Active
                                                            </Typography>
                                                            <Chip
                                                                icon={<ScopeIcon sx={{ fontSize: '0.5rem !important' }} />}
                                                                label={getScopeLabel(blackout.scope)}
                                                                size="small"
                                                                sx={activeScopeChipSx}
                                                            />
                                                            <Chip
                                                                label={formatTimeRemaining(blackout.end_time)}
                                                                size="small"
                                                                sx={activeTimeChipSx}
                                                            />
                                                        </Box>
                                                        {blackout.reason && (
                                                            <Typography sx={{ ...BANNER_REASON_SX, color: alpha(amberColor, 0.85) }}>
                                                                {blackout.reason}
                                                            </Typography>
                                                        )}
                                                    </Box>
                                                    <Button
                                                        variant="outlined"
                                                        size="small"
                                                        startIcon={<StopIcon sx={ICON_14_SX} />}
                                                        onClick={() => stopBlackout(blackout.id)}
                                                        sx={activeStopButtonSx}
                                                    >
                                                        Stop
                                                    </Button>
                                                </Box>
                                            );
                                        })}
                                    </Box>
                                </Box>
                            )}

                            {/* Non-active blackouts */}
                            {nonActiveBlackouts.length > 0 && (
                                <Box>
                                    <Typography sx={SECTION_LABEL_SX}>Blackouts</Typography>
                                    <Box sx={LIST_SX}>
                                        {nonActiveBlackouts.map((blackout) => {
                                            const ScopeIcon = getScopeIcon(blackout.scope);
                                            return (
                                                <Box key={blackout.id} sx={itemContainerSx}>
                                                    <PauseIcon sx={{ fontSize: 16, color: theme.palette.grey[500], flexShrink: 0 }} />
                                                    <Box sx={{ flex: 1, minWidth: 0 }}>
                                                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
                                                            <Chip
                                                                icon={<ScopeIcon sx={{ fontSize: '0.5rem !important' }} />}
                                                                label={getScopeLabel(blackout.scope)}
                                                                size="small"
                                                                sx={itemScopeChipSx}
                                                            />
                                                            <Typography sx={ITEM_META_SX}>
                                                                <ClockIcon sx={{ fontSize: 10 }} />
                                                                {formatTimeRange(blackout.start_time, blackout.end_time)}
                                                            </Typography>
                                                        </Box>
                                                        {blackout.reason && (
                                                            <Typography sx={ITEM_REASON_SX}>
                                                                {blackout.reason}
                                                            </Typography>
                                                        )}
                                                        {blackout.created_by && (
                                                            <Typography sx={ITEM_META_SX}>
                                                                <PersonIcon sx={{ fontSize: 10 }} />
                                                                {blackout.created_by}
                                                            </Typography>
                                                        )}
                                                    </Box>
                                                    <Tooltip title="Delete blackout" placement="left">
                                                        <IconButton
                                                            size="small"
                                                            onClick={() => setPendingDelete({ id: blackout.id, type: 'blackout', isActive: false })}
                                                            sx={deleteButtonSx}
                                                        >
                                                            <DeleteIcon sx={ICON_16_SX} />
                                                        </IconButton>
                                                    </Tooltip>
                                                </Box>
                                            );
                                        })}
                                    </Box>
                                </Box>
                            )}

                            {/* Schedules */}
                            {scheduleList.length > 0 && (
                                <Box>
                                    <Typography sx={SECTION_LABEL_SX}>Schedules</Typography>
                                    <Box sx={LIST_SX}>
                                        {scheduleList.map((schedule) => {
                                            const durationMinutes = schedule.duration_minutes || 0;
                                            const durationLabel = durationMinutes >= 60
                                                ? `${Math.floor(durationMinutes / 60)}h ${durationMinutes % 60 > 0 ? `${durationMinutes % 60}m` : ''}`
                                                : `${durationMinutes}m`;

                                            return (
                                                <Box key={schedule.id} sx={itemContainerSx}>
                                                    <RepeatIcon sx={{ fontSize: 16, color: theme.palette.grey[500], flexShrink: 0 }} />
                                                    <Box
                                                        sx={{
                                                            width: 6,
                                                            height: 6,
                                                            borderRadius: '50%',
                                                            bgcolor: schedule.enabled
                                                                ? theme.palette.success.main
                                                                : theme.palette.grey[400],
                                                            flexShrink: 0,
                                                        }}
                                                    />
                                                    <Box sx={{ flex: 1, minWidth: 0 }}>
                                                        <Typography sx={SCHEDULE_NAME_SX}>
                                                            {schedule.name}
                                                        </Typography>
                                                        <Typography sx={SCHEDULE_CRON_SX}>
                                                            {schedule.cron_expression}
                                                        </Typography>
                                                        <Typography sx={SCHEDULE_DETAIL_SX}>
                                                            {durationLabel} duration
                                                            {schedule.timezone ? ` (${schedule.timezone})` : ''}
                                                        </Typography>
                                                    </Box>
                                                    <Tooltip title="Delete schedule" placement="left">
                                                        <IconButton
                                                            size="small"
                                                            onClick={() => setPendingDelete({ id: schedule.id, type: 'schedule' })}
                                                            sx={deleteButtonSx}
                                                        >
                                                            <DeleteIcon sx={ICON_16_SX} />
                                                        </IconButton>
                                                    </Tooltip>
                                                </Box>
                                            );
                                        })}
                                    </Box>
                                </Box>
                            )}
                        </Box>
                    )}
                </DialogContent>

                <DialogActions sx={{ px: 3, py: 1.5 }}>
                    <Box sx={{ flex: 1 }} />
                    <Stack direction="row" spacing={1}>
                        <Button
                            variant="contained"
                            size="small"
                            startIcon={<AddIcon sx={ICON_14_SX} />}
                            onClick={handleStartBlackout}
                            sx={splitButtonSx}
                        >
                            New One Time Blackout
                        </Button>
                        <Button
                            variant="contained"
                            size="small"
                            startIcon={<AddIcon sx={ICON_14_SX} />}
                            onClick={() => setScheduleDialogOpen(true)}
                            sx={splitButtonSx}
                        >
                            New Scheduled Blackout
                        </Button>
                    </Stack>
                </DialogActions>
            </Dialog>

            {/* Sub-dialogs */}
            <BlackoutDialog
                open={blackoutDialogOpen}
                onClose={() => setBlackoutDialogOpen(false)}
                selection={selection}
            />
            <BlackoutScheduleDialog
                open={scheduleDialogOpen}
                onClose={() => setScheduleDialogOpen(false)}
                selection={selection}
            />
            <DeleteConfirmationDialog
                open={pendingDelete !== null}
                onClose={() => setPendingDelete(null)}
                onConfirm={handleDeleteConfirm}
                title={pendingDelete?.type === 'schedule' ? 'Delete schedule' : 'Delete blackout'}
                message={deleteDialogMessage}
                loading={deleteLoading}
            />
        </>
    );
};

export default BlackoutManagementDialog;

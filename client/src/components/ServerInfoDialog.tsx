/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Server Info Dialog
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog component for displaying comprehensive server information
 * including hardware, PostgreSQL configuration, databases, extensions,
 * and AI-generated database analysis.
 *
 *-------------------------------------------------------------------------
 */

import React, { useEffect, useMemo, useState, useCallback } from 'react';
import {
    Box,
    Typography,
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
    IconButton,
    alpha,
    Skeleton,
    Collapse,
    Fade,
    LinearProgress,
} from '@mui/material';
import { useTheme, Theme } from '@mui/material/styles';
import {
    Close as CloseIcon,
    Storage as StorageIcon,
    Memory as MemoryIcon,
    Dns as DnsIcon,
Tune as TuneIcon,
    AutoAwesome as SparkleIcon,
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
    Computer as ComputerIcon,
} from '@mui/icons-material';
import { apiGet } from '../utils/apiClient';

// ---------------------------------------------------------------------------
// Types matching the server API response
// ---------------------------------------------------------------------------

interface DiskInfo {
    mount_point: string;
    filesystem_type: string;
    total_bytes: number;
    used_bytes: number;
    free_bytes: number;
}

interface SystemInfo {
    os_name: string | null;
    os_version: string | null;
    architecture: string | null;
    hostname: string | null;
    cpu_model: string | null;
    cpu_cores: number | null;
    cpu_logical: number | null;
    cpu_clock_speed: number | null;
    memory_total_bytes: number | null;
    memory_used_bytes: number | null;
    memory_free_bytes: number | null;
    swap_total_bytes: number | null;
    swap_used_bytes: number | null;
    disks: DiskInfo[] | null;
}

interface PostgreSQLInfo {
    version: string | null;
    cluster_name: string | null;
    data_directory: string | null;
    max_connections: number | null;
    max_wal_senders: number | null;
    max_replication_slots: number | null;
}

interface DatabaseInfoItem {
    name: string;
    size_bytes: number | null;
    encoding: string | null;
    connection_limit: number | null;
    extensions: string[] | null;
}

interface ExtensionInfoItem {
    name: string;
    version: string | null;
    schema: string | null;
    database: string;
}

interface SettingInfoItem {
    name: string;
    setting: string | null;
    unit?: string | null;
    category: string | null;
}

interface AIAnalysisInfo {
    databases: Record<string, string>;
    generated_at: string;
}

interface ServerInfoResponse {
    connection_id: number;
    collected_at: string | null;
    system: SystemInfo | null;
    postgresql: PostgreSQLInfo | null;
    databases: DatabaseInfoItem[] | null;
    extensions: ExtensionInfoItem[] | null;
    key_settings: SettingInfoItem[] | null;
    ai_analysis: AIAnalysisInfo | null;
}

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

interface ServerInfoDialogProps {
    open: boolean;
    onClose: () => void;
    connectionId: number;
    serverName: string;
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

const MONO_FONT = '"JetBrains Mono", "SF Mono", monospace';

function formatBytes(bytes: number | null | undefined): string {
    if (bytes == null || bytes === 0) return '—';
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    let val = bytes;
    let idx = 0;
    while (val >= 1024 && idx < units.length - 1) {
        val /= 1024;
        idx++;
    }
    return `${val.toFixed(idx === 0 ? 0 : 1)} ${units[idx]}`;
}

function formatClockSpeed(hz: number | null | undefined): string {
    if (hz == null) return '—';
    if (hz >= 1_000_000_000) return `${(hz / 1_000_000_000).toFixed(2)} GHz`;
    if (hz >= 1_000_000) return `${(hz / 1_000_000).toFixed(0)} MHz`;
    return `${hz} Hz`;
}

function pct(used: number | null, total: number | null): number | null {
    if (used == null || total == null || total === 0) return null;
    return Math.round((used / total) * 100);
}

// ---------------------------------------------------------------------------
// Style helpers
// ---------------------------------------------------------------------------

const sxMono = { fontFamily: MONO_FONT };

const getDialogPaperSx = (theme: Theme) => ({
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
    backgroundImage: 'none',
    borderRadius: 2,
    border: '1px solid',
    borderColor: theme.palette.mode === 'dark'
        ? theme.palette.divider
        : theme.palette.grey[200],
    boxShadow: theme.palette.mode === 'dark'
        ? '0 25px 50px -12px rgba(0, 0, 0, 0.5)'
        : '0 25px 50px -12px rgba(0, 0, 0, 0.15)',
    maxHeight: '85vh',
});

const getDialogTitleSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 1.5,
    pb: 1.5,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.5)
        : theme.palette.background.paper,
});

const getIconBoxSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 40,
    height: 40,
    borderRadius: 1.5,
    bgcolor: alpha(
        theme.palette.primary.main,
        theme.palette.mode === 'dark' ? 0.15 : 0.1
    ),
    flexShrink: 0,
});

const getContentSx = (theme: Theme) => ({
    p: 0,
    bgcolor: theme.palette.mode === 'dark'
        ? theme.palette.background.default
        : theme.palette.grey[50],
    '&::-webkit-scrollbar': { width: 6 },
    '&::-webkit-scrollbar-thumb': {
        borderRadius: 3,
        backgroundColor: theme.palette.mode === 'dark' ? '#475569' : '#D1D5DB',
    },
    '&::-webkit-scrollbar-track': {
        backgroundColor: 'transparent',
    },
});

const getSectionHeaderSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 0.75,
    px: 2.5,
    py: 1,
    cursor: 'pointer',
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.3)
        : alpha(theme.palette.grey[100], 0.5),
    '&:hover': {
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.background.paper, 0.5)
            : alpha(theme.palette.grey[100], 0.8),
    },
    userSelect: 'none',
});

const getSectionIconSx = (theme: Theme) => ({
    fontSize: 16,
    color: theme.palette.primary.main,
});

const getSectionTitleSx = () => ({
    fontSize: '0.8125rem',
    fontWeight: 700,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.08em',
    color: 'text.secondary',
    flex: 1,
});

const getSectionContentSx = (theme: Theme) => ({
    px: 2.5,
    py: 1.5,
    borderBottom: '1px solid',
    borderColor: theme.palette.divider,
});

const getKvGridSx = () => ({
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))',
    gap: 1.5,
});

const getKvLabelSx = (theme: Theme) => ({
    fontSize: '0.625rem',
    fontWeight: 700,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.1em',
    lineHeight: 1,
    color: theme.palette.grey[500],
    mb: 0.25,
});

const getKvValueSx = () => ({
    fontSize: '0.875rem',
    fontWeight: 500,
    lineHeight: 1.3,
    color: 'text.primary',
    ...sxMono,
    wordBreak: 'break-word' as const,
});

const getProgressBarSx = (theme: Theme, percentage: number) => ({
    height: 4,
    borderRadius: 2,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.5)
        : alpha(theme.palette.grey[200], 0.8),
    '& .MuiLinearProgress-bar': {
        borderRadius: 2,
        bgcolor: percentage > 90
            ? theme.palette.error.main
            : percentage > 75
                ? theme.palette.warning.main
                : theme.palette.primary.main,
    },
});

const getDbRowSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'flex-start',
    gap: 1.5,
    py: 1,
    borderBottom: '1px solid',
    borderColor: alpha(theme.palette.divider, 0.5),
    '&:last-child': { borderBottom: 'none', pb: 0 },
    '&:first-of-type': { pt: 0 },
});

const getExtChipSx = (theme: Theme) => ({
    display: 'inline-flex',
    alignItems: 'center',
    gap: 0.5,
    px: 0.75,
    py: 0.25,
    borderRadius: 0.5,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.grey[700], 0.4)
        : alpha(theme.palette.grey[200], 0.6),
    fontSize: '0.8125rem',
    ...sxMono,
    color: 'text.secondary',
});

const getAiBoxSx = (theme: Theme) => ({
    mt: 0.5,
    px: 1.25,
    py: 0.75,
    borderRadius: 1,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.4)
        : alpha(theme.palette.grey[50], 0.8),
    border: '1px solid',
    borderColor: alpha(theme.palette.divider, 0.5),
});

const getSettingRowSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'baseline',
    justifyContent: 'space-between',
    gap: 2,
    py: 0.5,
    borderBottom: '1px solid',
    borderColor: alpha(theme.palette.divider, 0.3),
    '&:last-child': { borderBottom: 'none' },
});

const getFooterSx = (theme: Theme) => ({
    px: 2.5,
    py: 1.5,
    borderTop: '1px solid',
    borderColor: theme.palette.divider,
    bgcolor: theme.palette.mode === 'dark'
        ? alpha(theme.palette.background.paper, 0.5)
        : theme.palette.background.paper,
});

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

/** Collapsible section wrapper */
const Section: React.FC<{
    icon: React.ReactNode;
    title: string;
    defaultOpen?: boolean;
    children: React.ReactNode;
    badge?: string;
}> = ({ icon, title, defaultOpen = true, children, badge }) => {
    const theme = useTheme();
    const [open, setOpen] = useState(defaultOpen);

    return (
        <Box>
            <Box
                sx={getSectionHeaderSx(theme)}
                onClick={() => setOpen(!open)}
            >
                {icon}
                <Typography sx={getSectionTitleSx()}>
                    {title}
                </Typography>
                {badge && (
                    <Typography sx={{
                        fontSize: '0.6875rem',
                        fontWeight: 600,
                        color: 'text.disabled',
                        ...sxMono,
                    }}>
                        {badge}
                    </Typography>
                )}
                {open
                    ? <ExpandLessIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
                    : <ExpandMoreIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
                }
            </Box>
            <Collapse in={open}>
                <Box sx={getSectionContentSx(theme)}>
                    {children}
                </Box>
            </Collapse>
        </Box>
    );
};

/** Key-value pair display */
const KV: React.FC<{
    label: string;
    value: React.ReactNode;
    mono?: boolean;
    span?: boolean;
}> = ({ label, value, mono = true, span = false }) => {
    const theme = useTheme();
    return (
        <Box sx={span ? { gridColumn: '1 / -1' } : undefined}>
            <Typography sx={getKvLabelSx(theme)}>{label}</Typography>
            <Typography sx={{
                ...getKvValueSx(),
                fontFamily: mono ? MONO_FONT : 'inherit',
            }}>
                {value || '—'}
            </Typography>
        </Box>
    );
};

/** Small usage bar with label */
const UsageBar: React.FC<{
    label: string;
    used: number | null;
    total: number | null;
}> = ({ label, used, total }) => {
    const theme = useTheme();
    const percentage = pct(used, total);
    if (percentage == null) return null;

    return (
        <Box sx={{ gridColumn: '1 / -1' }}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.25 }}>
                <Typography sx={getKvLabelSx(theme)}>{label}</Typography>
                <Typography sx={{
                    fontSize: '0.6875rem',
                    color: 'text.disabled',
                    ...sxMono,
                }}>
                    {formatBytes(used)} / {formatBytes(total)} ({percentage}%)
                </Typography>
            </Box>
            <LinearProgress
                variant="determinate"
                value={percentage}
                sx={getProgressBarSx(theme, percentage)}
            />
        </Box>
    );
};

// ---------------------------------------------------------------------------
// Loading skeleton
// ---------------------------------------------------------------------------

const LoadingSkeleton: React.FC = () => {
    const theme = useTheme();
    const bg = {
        bgcolor: theme.palette.mode === 'dark'
            ? theme.palette.grey[700]
            : theme.palette.grey[200],
    };

    return (
        <Box sx={{ p: 2.5 }}>
            {/* System section skeleton */}
            <Skeleton variant="text" width="40%" height={20} sx={{ ...bg, mb: 1.5 }} />
            <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 1.5, mb: 3 }}>
                {[1, 2, 3, 4, 5, 6].map(i => (
                    <Box key={i}>
                        <Skeleton variant="text" width="60%" height={12} sx={bg} />
                        <Skeleton variant="text" width="80%" height={18} sx={{ ...bg, mt: 0.5 }} />
                    </Box>
                ))}
            </Box>
            {/* PostgreSQL section skeleton */}
            <Skeleton variant="text" width="35%" height={20} sx={{ ...bg, mb: 1.5 }} />
            <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 1.5, mb: 3 }}>
                {[1, 2, 3].map(i => (
                    <Box key={i}>
                        <Skeleton variant="text" width="60%" height={12} sx={bg} />
                        <Skeleton variant="text" width="80%" height={18} sx={{ ...bg, mt: 0.5 }} />
                    </Box>
                ))}
            </Box>
            {/* Databases section skeleton */}
            <Skeleton variant="text" width="30%" height={20} sx={{ ...bg, mb: 1.5 }} />
            {[1, 2].map(i => (
                <Box key={i} sx={{ mb: 1.5 }}>
                    <Skeleton variant="text" width="25%" height={16} sx={bg} />
                    <Skeleton variant="text" width="90%" height={14} sx={{ ...bg, mt: 0.5 }} />
                </Box>
            ))}
        </Box>
    );
};

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

const ServerInfoDialog: React.FC<ServerInfoDialogProps> = ({
    open,
    onClose,
    connectionId,
    serverName,
}) => {
    const theme = useTheme();
    const [data, setData] = useState<ServerInfoResponse | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [aiAnalysis, setAiAnalysis] = useState<AIAnalysisInfo | null>(null);
    const [aiLoading, setAiLoading] = useState(false);

    const fetchInfo = useCallback(async () => {
        if (!connectionId) return;
        setLoading(true);
        setError(null);
        try {
            const resp = await apiGet<ServerInfoResponse>(
                `/api/v1/server-info/${connectionId}`
            );
            setData(resp);
        } catch (err) {
            console.error('Failed to fetch server info:', err);
            setError(
                err instanceof Error ? err.message : 'Failed to load server information'
            );
        } finally {
            setLoading(false);
        }
    }, [connectionId]);

    useEffect(() => {
        if (open) {
            fetchInfo();
        }
    }, [open, fetchInfo]);

    useEffect(() => {
        if (!open || !data || !connectionId) return;
        // Fetch AI analysis asynchronously
        let cancelled = false;
        setAiLoading(true);
        apiGet<AIAnalysisInfo>(`/api/v1/server-info/${connectionId}/ai-analysis`)
            .then((resp) => {
                if (!cancelled) setAiAnalysis(resp);
            })
            .catch((err) => {
                console.error('Failed to fetch AI analysis:', err);
            })
            .finally(() => {
                if (!cancelled) setAiLoading(false);
            });
        return () => { cancelled = true; };
    }, [open, data, connectionId]);

    useEffect(() => {
        if (!open) {
            setData(null);
            setError(null);
            setAiAnalysis(null);
            setAiLoading(false);
        }
    }, [open]);

    // Derived data
    const sys = data?.system;
    const pg = data?.postgresql;
    const dbs = data?.databases;
    const exts = data?.extensions;
    const settings = data?.key_settings;
    const ai = aiAnalysis;
    const hasSystem = sys && (
        sys.os_name || sys.cpu_model || sys.memory_total_bytes
    );

    // Group settings by category
    const settingsByCategory = useMemo(() => {
        if (!settings?.length) return null;
        const groups: Record<string, SettingInfoItem[]> = {};
        for (const s of settings) {
            const cat = s.category || 'Other';
            if (!groups[cat]) groups[cat] = [];
            groups[cat].push(s);
        }
        return groups;
    }, [settings]);

    // Group extensions by database
    const extsByDb = useMemo(() => {
        if (!exts?.length) return null;
        const groups: Record<string, ExtensionInfoItem[]> = {};
        for (const ext of exts) {
            const db = ext.database || 'unknown';
            if (!groups[db]) groups[db] = [];
            groups[db].push(ext);
        }
        return groups;
    }, [exts]);

    return (
        <Dialog
            open={open}
            onClose={onClose}
            maxWidth="md"
            fullWidth
            PaperProps={{ sx: getDialogPaperSx(theme) }}
        >
            {/* Header */}
            <DialogTitle sx={getDialogTitleSx(theme)}>
                <Box sx={getIconBoxSx(theme)}>
                    <StorageIcon sx={{
                        fontSize: 22,
                        color: theme.palette.mode === 'dark'
                            ? theme.palette.primary.light
                            : theme.palette.primary.main,
                    }} />
                </Box>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography sx={{
                        fontWeight: 600,
                        color: 'text.primary',
                        fontSize: '1.0625rem',
                        lineHeight: 1.3,
                    }}>
                        Server Information
                    </Typography>
                    <Typography sx={{
                        fontSize: '0.75rem',
                        color: 'text.secondary',
                        ...sxMono,
                        mt: 0.125,
                    }}>
                        {serverName}
                    </Typography>
                </Box>
                <IconButton
                    onClick={onClose}
                    size="small"
                    sx={{
                        color: 'text.secondary',
                        '&:hover': {
                            bgcolor: alpha(theme.palette.grey[400], 0.1),
                        },
                    }}
                >
                    <CloseIcon sx={{ fontSize: 20 }} />
                </IconButton>
            </DialogTitle>

            {/* Content */}
            <DialogContent sx={getContentSx(theme)}>
                {loading && <LoadingSkeleton />}

                {error && !loading && (
                    <Box sx={{ p: 3 }}>
                        <Typography sx={{
                            color: theme.palette.error.main,
                            fontSize: '0.875rem',
                        }}>
                            {error}
                        </Typography>
                    </Box>
                )}

                {data && !loading && (
                    <Fade in timeout={200}>
                        <Box>
                            {/* System & Hardware */}
                            {hasSystem && (
                                <Section
                                    icon={<ComputerIcon sx={getSectionIconSx(theme)} />}
                                    title="System & Hardware"
                                >
                                    <Box sx={getKvGridSx()}>
                                        {sys.os_name && (
                                            <KV
                                                label="Operating System"
                                                value={[sys.os_name, sys.os_version].filter(Boolean).join(' ')}
                                                mono={false}
                                            />
                                        )}
                                        {sys.architecture && (
                                            <KV label="Architecture" value={sys.architecture} />
                                        )}
                                        {sys.hostname && (
                                            <KV label="Hostname" value={sys.hostname} />
                                        )}
                                        {sys.cpu_model && (
                                            <KV
                                                label="CPU"
                                                value={sys.cpu_model}
                                                mono={false}
                                                span
                                            />
                                        )}
                                        {sys.cpu_cores != null && (
                                            <KV label="Cores" value={`${sys.cpu_cores} physical${sys.cpu_logical ? ` / ${sys.cpu_logical} logical` : ''}`} />
                                        )}
                                        {sys.cpu_clock_speed != null && (
                                            <KV label="Clock Speed" value={formatClockSpeed(sys.cpu_clock_speed)} />
                                        )}
                                        <UsageBar
                                            label="Memory"
                                            used={sys.memory_used_bytes}
                                            total={sys.memory_total_bytes}
                                        />
                                        {sys.swap_total_bytes != null && sys.swap_total_bytes > 0 && (
                                            <UsageBar
                                                label="Swap"
                                                used={sys.swap_used_bytes}
                                                total={sys.swap_total_bytes}
                                            />
                                        )}
                                    </Box>

                                    {/* Disk info */}
                                    {sys.disks && sys.disks.length > 0 && (
                                        <Box sx={{ mt: 1.5 }}>
                                            <Typography sx={{
                                                ...getKvLabelSx(theme),
                                                mb: 0.75,
                                            }}>
                                                Disks
                                            </Typography>
                                            {sys.disks.map((disk, idx) => {
                                                const diskPct = pct(disk.used_bytes, disk.total_bytes);
                                                return (
                                                    <Box key={idx} sx={{ mb: idx < sys.disks!.length - 1 ? 1 : 0 }}>
                                                        <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.25 }}>
                                                            <Typography sx={{
                                                                fontSize: '0.8125rem',
                                                                ...sxMono,
                                                                color: 'text.primary',
                                                                fontWeight: 500,
                                                            }}>
                                                                {disk.mount_point}
                                                                <Typography component="span" sx={{
                                                                    fontSize: '0.6875rem',
                                                                    color: 'text.disabled',
                                                                    ml: 0.75,
                                                                }}>
                                                                    {disk.filesystem_type}
                                                                </Typography>
                                                            </Typography>
                                                            <Typography sx={{
                                                                fontSize: '0.6875rem',
                                                                color: 'text.disabled',
                                                                ...sxMono,
                                                            }}>
                                                                {formatBytes(disk.used_bytes)} / {formatBytes(disk.total_bytes)}
                                                                {diskPct != null && ` (${diskPct}%)`}
                                                            </Typography>
                                                        </Box>
                                                        {diskPct != null && (
                                                            <LinearProgress
                                                                variant="determinate"
                                                                value={diskPct}
                                                                sx={getProgressBarSx(theme, diskPct)}
                                                            />
                                                        )}
                                                    </Box>
                                                );
                                            })}
                                        </Box>
                                    )}
                                </Section>
                            )}

                            {/* PostgreSQL */}
                            {pg && (
                                <Section
                                    icon={<DnsIcon sx={getSectionIconSx(theme)} />}
                                    title="PostgreSQL"
                                >
                                    <Box sx={getKvGridSx()}>
                                        {pg.version && (
                                            <KV label="Version" value={pg.version} />
                                        )}
                                        {pg.cluster_name && (
                                            <KV label="Cluster Name" value={pg.cluster_name} />
                                        )}
                                        {pg.data_directory && (
                                            <KV label="Data Directory" value={pg.data_directory} span />
                                        )}
                                        {pg.max_connections != null && (
                                            <KV label="Max Connections" value={String(pg.max_connections)} />
                                        )}
                                        {pg.max_wal_senders != null && (
                                            <KV label="Max WAL Senders" value={String(pg.max_wal_senders)} />
                                        )}
                                        {pg.max_replication_slots != null && (
                                            <KV label="Max Replication Slots" value={String(pg.max_replication_slots)} />
                                        )}
                                    </Box>
                                </Section>
                            )}

                            {/* Databases with AI Analysis */}
                            {dbs && dbs.length > 0 && (
                                <Section
                                    icon={<MemoryIcon sx={getSectionIconSx(theme)} />}
                                    title="Databases"
                                    badge={`${dbs.length}`}
                                >
                                    {dbs.map((db) => (
                                        <Box key={db.name} sx={getDbRowSx(theme)}>
                                            <Box sx={{ flex: 1, minWidth: 0 }}>
                                                <Box sx={{
                                                    display: 'flex',
                                                    alignItems: 'baseline',
                                                    gap: 1,
                                                    mb: 0.25,
                                                }}>
                                                    <Typography sx={{
                                                        fontSize: '0.875rem',
                                                        fontWeight: 600,
                                                        color: 'text.primary',
                                                        ...sxMono,
                                                    }}>
                                                        {db.name}
                                                    </Typography>
                                                    {db.encoding && (
                                                        <Typography sx={{
                                                            fontSize: '0.6875rem',
                                                            color: 'text.disabled',
                                                        }}>
                                                            {db.encoding}
                                                        </Typography>
                                                    )}
                                                </Box>
                                                <Box sx={{
                                                    display: 'flex',
                                                    alignItems: 'center',
                                                    gap: 1.5,
                                                    mb: 0.5,
                                                }}>
                                                    <Typography sx={{
                                                        fontSize: '0.75rem',
                                                        color: 'text.secondary',
                                                        ...sxMono,
                                                    }}>
                                                        {formatBytes(db.size_bytes)}
                                                    </Typography>
                                                    {db.connection_limit != null && db.connection_limit >= 0 && (
                                                        <Typography sx={{
                                                            fontSize: '0.6875rem',
                                                            color: 'text.disabled',
                                                        }}>
                                                            limit: {db.connection_limit}
                                                        </Typography>
                                                    )}
                                                </Box>
                                                {/* Extensions inline */}
                                                {extsByDb?.[db.name] && extsByDb[db.name].length > 0 && (
                                                    <Box sx={{
                                                        display: 'flex',
                                                        flexWrap: 'wrap',
                                                        gap: 0.5,
                                                        mb: 0.5,
                                                    }}>
                                                        {extsByDb[db.name].map((ext) => (
                                                            <Box key={`${db.name}-${ext.name}`} sx={getExtChipSx(theme)}>
                                                                <Typography component="span" sx={{
                                                                    fontSize: '0.8125rem',
                                                                    color: 'text.primary',
                                                                    fontWeight: 500,
                                                                    ...sxMono,
                                                                }}>
                                                                    {ext.name}
                                                                </Typography>
                                                                {ext.version && (
                                                                    <Typography component="span" sx={{
                                                                        fontSize: '0.625rem',
                                                                        color: 'text.disabled',
                                                                        ...sxMono,
                                                                    }}>
                                                                        {ext.version}
                                                                    </Typography>
                                                                )}
                                                            </Box>
                                                        ))}
                                                    </Box>
                                                )}
                                                {/* AI Analysis for this database */}
                                                {ai?.databases?.[db.name] ? (
                                                    <Box sx={getAiBoxSx(theme)}>
                                                        <Box sx={{
                                                            display: 'flex',
                                                            alignItems: 'flex-start',
                                                            gap: 0.5,
                                                        }}>
                                                            <SparkleIcon sx={{
                                                                fontSize: 12,
                                                                color: 'primary.main',
                                                                mt: 0.125,
                                                                flexShrink: 0,
                                                            }} />
                                                            <Typography sx={{
                                                                fontSize: '0.8125rem',
                                                                color: 'text.secondary',
                                                                lineHeight: 1.5,
                                                            }}>
                                                                {ai.databases[db.name]}
                                                            </Typography>
                                                        </Box>
                                                    </Box>
                                                ) : aiLoading ? (
                                                    <Box sx={getAiBoxSx(theme)}>
                                                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                                                            <SparkleIcon sx={{ fontSize: 12, color: 'primary.main', flexShrink: 0 }} />
                                                            <Skeleton variant="text" width="60%" sx={{ bgcolor: alpha(theme.palette.grey[500], 0.1) }} />
                                                        </Box>
                                                    </Box>
                                                ) : null}
                                            </Box>
                                        </Box>
                                    ))}
                                </Section>
                            )}

                            {/* Key Settings */}
                            {settingsByCategory && (
                                <Section
                                    icon={<TuneIcon sx={getSectionIconSx(theme)} />}
                                    title="Configuration"
                                    badge={`${settings?.length || 0}`}
                                    defaultOpen={false}
                                >
                                    {Object.entries(settingsByCategory).map(([category, catSettings]) => (
                                        <Box key={category} sx={{ mb: 1.5, '&:last-child': { mb: 0 } }}>
                                            <Typography sx={{
                                                fontSize: '0.625rem',
                                                fontWeight: 700,
                                                textTransform: 'uppercase',
                                                letterSpacing: '0.1em',
                                                color: theme.palette.grey[500],
                                                mb: 0.5,
                                            }}>
                                                {category}
                                            </Typography>
                                            {catSettings.map((s) => (
                                                <Box key={s.name} sx={getSettingRowSx(theme)}>
                                                    <Typography sx={{
                                                        fontSize: '0.8125rem',
                                                        color: 'text.secondary',
                                                        ...sxMono,
                                                        flexShrink: 0,
                                                    }}>
                                                        {s.name}
                                                    </Typography>
                                                    <Typography sx={{
                                                        fontSize: '0.8125rem',
                                                        color: 'text.primary',
                                                        fontWeight: 500,
                                                        ...sxMono,
                                                        textAlign: 'right',
                                                    }}>
                                                        {s.setting || '—'}
                                                        {s.unit && (
                                                            <Typography component="span" sx={{
                                                                fontSize: '0.6875rem',
                                                                color: 'text.disabled',
                                                                ml: 0.5,
                                                            }}>
                                                                {s.unit}
                                                            </Typography>
                                                        )}
                                                    </Typography>
                                                </Box>
                                            ))}
                                        </Box>
                                    ))}
                                </Section>
                            )}
                        </Box>
                    </Fade>
                )}
            </DialogContent>

            {/* Footer */}
            <DialogActions sx={getFooterSx(theme)}>
                {data?.collected_at && (
                    <Typography sx={{
                        fontSize: '0.6875rem',
                        color: 'text.disabled',
                        mr: 'auto',
                    }}>
                        Data collected {new Date(data.collected_at).toLocaleString()}
                    </Typography>
                )}
                <Button
                    onClick={onClose}
                    variant="contained"
                    size="small"
                >
                    Close
                </Button>
            </DialogActions>
        </Dialog>
    );
};

export default ServerInfoDialog;

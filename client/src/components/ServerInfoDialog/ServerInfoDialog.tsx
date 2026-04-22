/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useEffect, useMemo, useState, useCallback } from 'react';
import {
    Box,
    Typography,
    Dialog,
    AppBar,
    Toolbar,
    IconButton,
    alpha,
    Fade,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Close as CloseIcon } from '@mui/icons-material';
import { apiGet } from '../../utils/apiClient';
import SlideTransition from '../shared/SlideTransition';
import { LoadingSkeleton } from './components';
import {
    SystemSection,
    PostgreSQLSection,
    DatabasesSection,
    ConfigurationSection,
} from './sections';
import { getContentSx } from './serverInfoStyles';
import type {
    ServerInfoDialogProps,
    ServerInfoResponse,
    AIAnalysisInfo,
    SettingInfoItem,
    ExtensionInfoItem,
} from './serverInfoTypes';

/**
 * Full-screen dialog displaying comprehensive server information
 * including hardware, PostgreSQL configuration, databases, extensions,
 * and AI-generated database analysis.
 */
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

    const fetchInfo = useCallback(async (signal?: AbortSignal) => {
        if (!connectionId) {
            setLoading(false);
            setData(null);
            setError(null);
            return;
        }
        setLoading(true);
        setError(null);
        setData(null);
        try {
            const resp = await apiGet<ServerInfoResponse>(
                `/api/v1/server-info/${connectionId}`,
                { signal }
            );
            if (!signal?.aborted) {
                setData(resp);
            }
        } catch (err) {
            if (signal?.aborted) return;
            console.error('Failed to fetch server info:', err);
            setError(
                err instanceof Error ? err.message : 'Failed to load server information'
            );
        } finally {
            if (!signal?.aborted) {
                setLoading(false);
            }
        }
    }, [connectionId]);

    useEffect(() => {
        if (open) {
            const controller = new AbortController();
            fetchInfo(controller.signal);
            return () => controller.abort();
        }
    }, [open, fetchInfo]);

    useEffect(() => {
        if (!open || !data || data.connection_id !== connectionId || !connectionId) {
            return;
        }
        // Fetch AI analysis asynchronously
        let cancelled = false;
        const controller = new AbortController();
        setAiLoading(true);
        setAiAnalysis(null);
        apiGet<AIAnalysisInfo | null>(`/api/v1/server-info/${connectionId}/ai-analysis`, { signal: controller.signal })
            .then((resp) => {
                if (!cancelled) {
                    setAiAnalysis(resp);
                }
            })
            .catch((err) => {
                if (!cancelled) {
                    console.error('Failed to fetch AI analysis:', err);
                }
            })
            .finally(() => {
                if (!cancelled) {
                    setAiLoading(false);
                }
            });
        return () => {
            cancelled = true;
            controller.abort();
        };
    }, [open, data, connectionId]);

    useEffect(() => {
        if (!open) {
            setLoading(false);
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
    const hasSystem = !!sys && (
        sys.os_name ||
        sys.os_version ||
        sys.architecture ||
        sys.hostname ||
        sys.cpu_model ||
        sys.cpu_cores != null ||
        sys.cpu_logical != null ||
        sys.cpu_clock_speed != null ||
        sys.memory_total_bytes != null ||
        sys.memory_used_bytes != null ||
        sys.memory_free_bytes != null ||
        sys.swap_total_bytes != null ||
        sys.swap_used_bytes != null ||
        (sys.disks?.length ?? 0) > 0
    );

    // Group settings by category
    const settingsByCategory = useMemo(() => {
        if (!settings?.length) {
            return null;
        }
        const groups: Record<string, SettingInfoItem[]> = {};
        for (const s of settings) {
            const cat = s.category || 'Other';
            if (!groups[cat]) {
                groups[cat] = [];
            }
            groups[cat].push(s);
        }
        return groups;
    }, [settings]);

    // Group extensions by database
    const extsByDb = useMemo(() => {
        if (!exts?.length) {
            return null;
        }
        const groups: Record<string, ExtensionInfoItem[]> = {};
        for (const ext of exts) {
            const db = ext.database || 'unknown';
            if (!groups[db]) {
                groups[db] = [];
            }
            groups[db].push(ext);
        }
        return groups;
    }, [exts]);

    return (
        <Dialog
            fullScreen
            open={open}
            onClose={onClose}
            TransitionComponent={SlideTransition}
        >
            <AppBar
                position="static"
                elevation={0}
                sx={{
                    bgcolor: 'background.paper',
                    borderBottom: '1px solid',
                    borderColor: 'divider',
                }}
            >
                <Toolbar>
                    <IconButton
                        edge="start"
                        onClick={onClose}
                        aria-label="close server info"
                        sx={{ color: 'text.secondary', mr: 2 }}
                    >
                        <CloseIcon />
                    </IconButton>
                    <Typography
                        variant="h6"
                        component="div"
                        sx={{
                            flexGrow: 1,
                            fontWeight: 600,
                            color: 'text.primary',
                        }}
                    >
                        Server Information: {serverName}
                    </Typography>
                </Toolbar>
            </AppBar>
            <Box sx={getContentSx(theme)}>
                {loading && <LoadingSkeleton />}

                {error && !loading && (
                    <Box sx={{ p: 3 }}>
                        <Typography sx={{
                            color: theme.palette.error.main,
                            fontSize: '1rem',
                        }}>
                            {error}
                        </Typography>
                    </Box>
                )}

                {data && !loading && (
                    <Fade in timeout={200}>
                        <Box>
                            {/* System & Hardware */}
                            {hasSystem && sys && (
                                <SystemSection system={sys} />
                            )}

                            {/* PostgreSQL */}
                            {pg && (
                                <PostgreSQLSection postgresql={pg} />
                            )}

                            {/* Databases with AI Analysis */}
                            {dbs && dbs.length > 0 && (
                                <DatabasesSection
                                    databases={dbs}
                                    extsByDb={extsByDb}
                                    aiAnalysis={ai}
                                    aiLoading={aiLoading}
                                />
                            )}

                            {/* Key Settings */}
                            {settingsByCategory && (
                                <ConfigurationSection
                                    settingsByCategory={settingsByCategory}
                                    totalCount={settings?.length || 0}
                                />
                            )}

                            {/* Footer metadata */}
                            {data.collected_at && (
                                <Box sx={{
                                    px: 2.5,
                                    py: 1.5,
                                    borderTop: '1px solid',
                                    borderColor: theme.palette.divider,
                                    bgcolor: theme.palette.mode === 'dark'
                                        ? alpha(theme.palette.background.paper, 0.5)
                                        : theme.palette.background.paper,
                                }}>
                                    <Typography sx={{
                                        fontSize: '0.875rem',
                                        color: 'text.disabled',
                                    }}>
                                        Data collected {new Date(data.collected_at).toLocaleString(undefined, {
                                            month: 'short',
                                            day: 'numeric',
                                            year: 'numeric',
                                            hour: '2-digit',
                                            minute: '2-digit',
                                            second: '2-digit',
                                        })}
                                    </Typography>
                                </Box>
                            )}
                        </Box>
                    </Fade>
                )}
            </Box>
        </Dialog>
    );
};

export default ServerInfoDialog;

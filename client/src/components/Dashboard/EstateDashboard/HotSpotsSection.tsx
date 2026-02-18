/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useMemo, useState, useCallback, useEffect, useRef } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import Chip from '@mui/material/Chip';
import { alpha, useTheme } from '@mui/material/styles';
import { useAuth } from '../../../contexts/AuthContext';
import { useClusterData } from '../../../contexts/ClusterDataContext';
import { HotSpotEntry } from '../types';

interface HotSpotsSectionProps {
    selection: Record<string, unknown>;
    serverIds: number[];
}

const LOADING_CONTAINER_SX = {
    display: 'flex',
    justifyContent: 'center',
    py: 3,
};

const EMPTY_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    textAlign: 'center',
    py: 3,
};

const HOTSPOT_LIST_SX = {
    display: 'flex',
    flexDirection: 'column',
    gap: 0.5,
};

const HOTSPOT_NAME_SX = {
    fontWeight: 600,
    fontSize: '0.875rem',
    color: 'text.primary',
    flex: 1,
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const HOTSPOT_METRIC_SX = {
    fontSize: '0.75rem',
    color: 'text.secondary',
    fontWeight: 500,
};

const HOTSPOT_VALUE_SX = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.875rem',
    fontWeight: 600,
    flexShrink: 0,
};

/**
 * Compute hot spots from performance summary connection data.
 * Identifies servers with the worst metrics and returns them
 * sorted by severity.
 */
const computeHotSpots = (
    connections: Array<Record<string, unknown>>
): HotSpotEntry[] => {
    const hotSpots: HotSpotEntry[] = [];

    connections.forEach(conn => {
        const connectionId = conn.connection_id as number;
        const serverName = conn.connection_name as string || `Server ${connectionId}`;

        const cacheHit = conn.cache_hit_ratio as Record<string, unknown> | undefined;
        if (cacheHit && typeof cacheHit.current === 'number' && cacheHit.current < 95) {
            hotSpots.push({
                connectionId,
                serverName,
                metric: 'Cache Hit Ratio',
                value: Math.round(cacheHit.current * 100) / 100,
                unit: '%',
                severity: cacheHit.current < 90 ? 'critical' : 'warning',
            });
        }

        const txns = conn.transactions as Record<string, unknown> | undefined;
        if (txns && typeof txns.rollback_percent === 'number' && txns.rollback_percent > 5) {
            hotSpots.push({
                connectionId,
                serverName,
                metric: 'Rollback Rate',
                value: Math.round(txns.rollback_percent * 100) / 100,
                unit: '%',
                severity: txns.rollback_percent > 10 ? 'critical' : 'warning',
            });
        }

        const xidAge = conn.xid_age as Array<Record<string, unknown>> | undefined;
        if (xidAge) {
            xidAge.forEach(entry => {
                const percent = entry.percent as number;
                if (typeof percent === 'number' && percent > 50) {
                    hotSpots.push({
                        connectionId,
                        serverName,
                        metric: `XID Age (${entry.database_name})`,
                        value: Math.round(percent * 100) / 100,
                        unit: '%',
                        severity: percent > 75 ? 'critical' : 'warning',
                    });
                }
            });
        }
    });

    hotSpots.sort((a, b) => {
        const severityOrder = { critical: 0, warning: 1 };
        const aSev = severityOrder[a.severity];
        const bSev = severityOrder[b.severity];
        if (aSev !== bSev) { return aSev - bSev; }
        return b.value - a.value;
    });

    return hotSpots.slice(0, 10);
};

/**
 * HotSpotsSection displays the most critical issues across the
 * fleet. It fetches performance summary data and computes hot
 * spots based on cache hit ratio, rollback rate, and transaction
 * ID age. Results are sorted by severity with critical issues first.
 */
const HotSpotsSection: React.FC<HotSpotsSectionProps> = ({ selection: _selection, serverIds }) => {
    const theme = useTheme();
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [hotSpots, setHotSpots] = useState<HotSpotEntry[]>([]);
    const [loading, setLoading] = useState<boolean>(false);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);
    const serverIdsKey = serverIds.join(',');

    const fetchHotSpots = useCallback(async (): Promise<void> => {
        if (!user || serverIds.length === 0) { return; }

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }

        try {
            const response = await fetch(
                `/api/v1/metrics/performance-summary?connection_ids=${serverIds.join(',')}&time_range=24h`,
                { credentials: 'include' }
            );

            if (response.ok && isMountedRef.current) {
                const data = await response.json();
                const connections = data.connections || [];
                const computed = computeHotSpots(connections);
                setHotSpots(computed);
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching hot spots data:', err);
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, serverIds]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [serverIdsKey]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user && serverIds.length > 0) {
            fetchHotSpots();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, serverIds.length, fetchHotSpots, lastRefresh]);

    const hotSpotRowSx = useMemo(() => ({
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        py: 1,
        px: 1.5,
        borderRadius: 1,
        '&:not(:last-child)': {
            borderBottom: '1px solid',
            borderColor: 'divider',
        },
        '&:hover': {
            bgcolor: theme.palette.mode === 'dark'
                ? alpha(theme.palette.grey[700], 0.3)
                : alpha(theme.palette.grey[100], 0.8),
        },
    }), [theme]);

    if (loading && !initialLoadDoneRef.current) {
        return (
            <Box sx={LOADING_CONTAINER_SX}>
                <CircularProgress size={28} aria-label="Loading hot spots" />
            </Box>
        );
    }

    if (hotSpots.length === 0) {
        return (
            <Typography sx={EMPTY_SX}>
                No hot spots detected across the fleet.
            </Typography>
        );
    }

    return (
        <Box sx={HOTSPOT_LIST_SX}>
            {hotSpots.map((spot, index) => (
                <Box key={`${spot.connectionId}-${spot.metric}-${index}`} sx={hotSpotRowSx}>
                    <Chip
                        label={spot.severity}
                        size="small"
                        color={spot.severity === 'critical' ? 'error' : 'warning'}
                        sx={{
                            height: 20,
                            fontSize: '0.7rem',
                            fontWeight: 700,
                            textTransform: 'uppercase',
                            minWidth: 64,
                        }}
                    />
                    <Typography sx={HOTSPOT_NAME_SX}>
                        {spot.serverName}
                    </Typography>
                    <Typography sx={HOTSPOT_METRIC_SX}>
                        {spot.metric}
                    </Typography>
                    <Typography
                        sx={{
                            ...HOTSPOT_VALUE_SX,
                            color: spot.severity === 'critical'
                                ? theme.palette.error.main
                                : theme.palette.warning.main,
                        }}
                    >
                        {spot.value}{spot.unit || ''}
                    </Typography>
                </Box>
            ))}
        </Box>
    );
};

export default HotSpotsSection;

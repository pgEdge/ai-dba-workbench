/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback, useEffect, useRef, useMemo } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { useAuth } from '../../../contexts/AuthContext';
import { useDashboard } from '../../../contexts/DashboardContext';
import { useMetrics } from '../../../hooks/useMetrics';
import { MetricQueryParams } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import { Chart } from '../../Chart';
import {
    ObjectDetailProps,
    IndexDetailData,
    buildChartData,
    formatBytes,
    formatNumber,
} from './types';

/** Number of data buckets for full charts */
const CHART_BUCKETS = 150;

/** Chart height in pixels */
const CHART_HEIGHT = 250;

/**
 * IndexDetail displays comprehensive information for a single
 * index including KPI tiles for size, scan counts, and tuples
 * read/fetched, plus a time-series chart of index scan activity.
 */
const IndexDetail: React.FC<ObjectDetailProps> = ({
    connectionId,
    databaseName,
    schemaName,
    objectName,
}) => {
    const { user } = useAuth();
    const { timeRange, refreshTrigger } = useDashboard();

    const [indexData, setIndexData] = useState<IndexDetailData | null>(
        null
    );
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    // Fetch the latest snapshot row for this index
    const fetchIndexData = useCallback(async (): Promise<void> => {
        if (!user) { return; }

        const params = new URLSearchParams({
            probe_name: 'pg_stat_all_indexes',
            connection_id: connectionId.toString(),
            database_name: databaseName,
            limit: '1',
            order_by: 'idx_scan',
            order: 'desc',
        });
        if (schemaName) {
            params.append('schema_name', schemaName);
        }
        params.append('index_name', objectName);

        const url = `/api/v1/metrics/query?${params.toString()}`;

        if (!initialLoadDoneRef.current) {
            setLoading(true);
        }
        setError(null);

        try {
            const response = await fetch(url, {
                credentials: 'include',
            });

            if (!response.ok) {
                const errorData = await response.json().catch(
                    () => ({})
                ) as { error?: string };
                throw new Error(
                    errorData.error
                    || `Failed to fetch index data: `
                    + `${response.status}`
                );
            }

            if (isMountedRef.current) {
                const result = await response.json();
                const rows = Array.isArray(result)
                    ? result
                    : (result.rows ?? []);
                setIndexData(
                    rows.length > 0 ? rows[0] : null
                );
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching index detail:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch index data'
                );
                setIndexData(null);
            }
        } finally {
            if (isMountedRef.current) {
                setLoading(false);
            }
        }
    }, [user, connectionId, databaseName, schemaName, objectName]);

    useEffect(() => {
        initialLoadDoneRef.current = false;
    }, [connectionId, databaseName, schemaName, objectName]);

    useEffect(() => {
        isMountedRef.current = true;

        if (user) {
            fetchIndexData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, fetchIndexData, refreshTrigger]);

    // Chart query - index scan activity over time
    const scanChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_all_indexes',
        connectionId,
        databaseName,
        schemaName,
        tableName: objectName,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['idx_scan_per_sec'],
    }), [
        connectionId, databaseName, schemaName,
        objectName, timeRange.range,
    ]);

    const scanChart = useMetrics(scanChartParams);

    const scanChartData = useMemo(
        () => buildChartData(
            scanChart.data,
            ['idx_scan_per_sec'],
            ['Index Scans/s'],
        ),
        [scanChart.data]
    );

    const displayName = schemaName
        ? `${schemaName}.${objectName}`
        : objectName;

    if (loading && !indexData) {
        return (
            <Box sx={{
                display: 'flex',
                justifyContent: 'center',
                py: 4,
            }}>
                <CircularProgress size={32} />
            </Box>
        );
    }

    if (error) {
        return (
            <Typography
                variant="body2"
                color="error"
                sx={{ textAlign: 'center', py: 4 }}
            >
                {error}
            </Typography>
        );
    }

    return (
        <Box>
            <Typography
                sx={{
                    fontWeight: 600,
                    fontSize: '1.1rem',
                    fontFamily:
                        '"JetBrains Mono", "SF Mono", monospace',
                    mb: 0.5,
                }}
            >
                {displayName}
            </Typography>
            {indexData?.relname && (
                <Typography
                    sx={{
                        fontSize: '0.8125rem',
                        color: 'text.secondary',
                        mb: 2,
                    }}
                >
                    on table {indexData.relname}
                </Typography>
            )}

            <CollapsibleSection
                title="Index Overview"
                defaultExpanded
            >
                <Box sx={KPI_GRID_SX}>
                    <KpiTile
                        label="Index Size"
                        value={indexData?.index_size_pretty
                            ?? formatBytes(
                                indexData?.index_size ?? null
                            )}
                    />
                    <KpiTile
                        label="Index Scans"
                        value={indexData
                            ? formatNumber(indexData.idx_scan)
                            : '--'}
                    />
                    <KpiTile
                        label="Tuples Read"
                        value={indexData
                            ? formatNumber(indexData.idx_tup_read)
                            : '--'}
                    />
                    <KpiTile
                        label="Tuples Fetched"
                        value={indexData
                            ? formatNumber(indexData.idx_tup_fetch)
                            : '--'}
                    />
                </Box>
            </CollapsibleSection>

            <CollapsibleSection
                title="Scan Activity"
                defaultExpanded
            >
                <Box sx={CHART_SECTION_SX}>
                    <Box>
                        {scanChart.loading
                            && !scanChartData ? (
                                <Box sx={{
                                    display: 'flex',
                                    justifyContent: 'center',
                                    alignItems: 'center',
                                    height: CHART_HEIGHT,
                                }}>
                                    <CircularProgress size={24} />
                                </Box>
                            ) : scanChartData ? (
                                <Chart
                                    type="line"
                                    data={scanChartData}
                                    title={
                                        'Index Scan Activity'
                                        + ' Over Time'
                                    }
                                    height={CHART_HEIGHT}
                                    smooth
                                    showLegend
                                    showTooltip
                                    showToolbar={false}
                                />
                            ) : (
                                <Typography
                                    variant="body2"
                                    color="text.secondary"
                                    sx={{
                                        textAlign: 'center',
                                        py: 4,
                                    }}
                                >
                                    No index scan data available
                                </Typography>
                            )}
                    </Box>
                </Box>
            </CollapsibleSection>
        </Box>
    );
};

export default IndexDetail;

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
    TableDetailData,
    buildChartData,
    formatBytes,
    formatNumber,
    formatTimestamp,
} from './types';

/** Number of data buckets for full charts */
const CHART_BUCKETS = 150;

/** Chart height in pixels */
const CHART_HEIGHT = 250;

/** Maintenance info row layout */
const MAINT_ROW_SX = {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    py: 0.5,
    px: 1,
    '&:not(:last-child)': {
        borderBottom: '1px solid',
        borderColor: 'divider',
    },
};

/** Maintenance label */
const MAINT_LABEL_SX = {
    fontSize: '0.8125rem',
    color: 'text.secondary',
};

/** Maintenance value */
const MAINT_VALUE_SX = {
    fontSize: '0.8125rem',
    fontWeight: 600,
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    color: 'text.primary',
};

/**
 * Determine status for dead tuple ratio values.
 */
const getDeadTupleStatus = (
    value: number | null
): 'good' | 'warning' | 'critical' | undefined => {
    if (value === null) { return undefined; }
    if (value <= 5) { return 'good'; }
    if (value <= 20) { return 'warning'; }
    return 'critical';
};

/**
 * TableDetail displays comprehensive information for a single
 * table including KPI tiles, time-series charts for tuple
 * operations and scan types, and maintenance timestamps.
 */
const TableDetail: React.FC<ObjectDetailProps> = ({
    connectionId,
    databaseName,
    schemaName,
    objectName,
}) => {
    const { user } = useAuth();
    const { timeRange, refreshTrigger } = useDashboard();

    const [tableData, setTableData] = useState<TableDetailData | null>(
        null
    );
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const isMountedRef = useRef<boolean>(true);
    const initialLoadDoneRef = useRef<boolean>(false);

    // Fetch the latest snapshot row for this table
    const fetchTableData = useCallback(async (): Promise<void> => {
        if (!user) { return; }

        const params = new URLSearchParams({
            probe_name: 'pg_stat_all_tables',
            connection_id: connectionId.toString(),
            database_name: databaseName,
            limit: '1',
            order_by: 'n_live_tup',
            order: 'desc',
        });
        if (schemaName) {
            params.append('schema_name', schemaName);
        }
        params.append('table_name', objectName);

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
                    || `Failed to fetch table data: ${response.status}`
                );
            }

            if (isMountedRef.current) {
                const result = await response.json();
                const rows = Array.isArray(result)
                    ? result
                    : (result.rows ?? []);
                setTableData(
                    rows.length > 0 ? rows[0] : null
                );
                initialLoadDoneRef.current = true;
            }
        } catch (err) {
            console.error('Error fetching table detail:', err);
            if (isMountedRef.current) {
                setError(
                    (err as Error).message
                    || 'Failed to fetch table data'
                );
                setTableData(null);
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
            fetchTableData();
        }

        return () => {
            isMountedRef.current = false;
        };
    }, [user, fetchTableData, refreshTrigger]);

    // Chart queries - tuple operations over time
    const tupleChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_all_tables',
        connectionId,
        databaseName,
        schemaName,
        tableName: objectName,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: [
            'n_tup_ins_per_sec',
            'n_tup_upd_per_sec',
            'n_tup_del_per_sec',
            'n_tup_hot_upd_per_sec',
        ],
    }), [
        connectionId, databaseName, schemaName,
        objectName, timeRange.range,
    ]);

    // Chart queries - seq vs index scans over time
    const scanChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_all_tables',
        connectionId,
        databaseName,
        schemaName,
        tableName: objectName,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['seq_scan_per_sec', 'idx_scan_per_sec'],
    }), [
        connectionId, databaseName, schemaName,
        objectName, timeRange.range,
    ]);

    // Chart queries - dead tuple ratio over time
    const deadTupleChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_all_tables',
        connectionId,
        databaseName,
        schemaName,
        tableName: objectName,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['dead_tuple_ratio'],
    }), [
        connectionId, databaseName, schemaName,
        objectName, timeRange.range,
    ]);

    const tupleChart = useMetrics(tupleChartParams);
    const scanChart = useMetrics(scanChartParams);
    const deadTupleChart = useMetrics(deadTupleChartParams);

    // Build chart datasets
    const tupleChartData = useMemo(
        () => buildChartData(
            tupleChart.data,
            [
                'n_tup_ins_per_sec',
                'n_tup_upd_per_sec',
                'n_tup_del_per_sec',
                'n_tup_hot_upd_per_sec',
            ],
            ['Inserts/s', 'Updates/s', 'Deletes/s', 'HOT Updates/s'],
        ),
        [tupleChart.data]
    );

    const scanChartData = useMemo(
        () => buildChartData(
            scanChart.data,
            ['seq_scan_per_sec', 'idx_scan_per_sec'],
            ['Sequential Scans/s', 'Index Scans/s'],
        ),
        [scanChart.data]
    );

    const deadTupleChartData = useMemo(
        () => buildChartData(
            deadTupleChart.data,
            ['dead_tuple_ratio'],
            ['Dead Tuple Ratio %'],
        ),
        [deadTupleChart.data]
    );

    // Compute KPI values from the snapshot data
    const liveTuples = tableData?.n_live_tup ?? null;
    const deadTuples = tableData?.n_dead_tup ?? null;
    const deadRatio = useMemo(() => {
        if (liveTuples === null || deadTuples === null) {
            return null;
        }
        const total = liveTuples + deadTuples;
        if (total === 0) { return 0; }
        return (deadTuples / total) * 100;
    }, [liveTuples, deadTuples]);
    const tableSize = tableData?.table_size ?? null;
    const seqScans = tableData?.seq_scan ?? null;

    const displayName = schemaName
        ? `${schemaName}.${objectName}`
        : objectName;

    if (loading && !tableData) {
        return (
            <Box sx={{
                display: 'flex',
                justifyContent: 'center',
                py: 4,
            }}>
                <CircularProgress size={32} aria-label="Loading table details" />
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
                    mb: 2,
                }}
            >
                {displayName}
            </Typography>

            <CollapsibleSection
                title="Table Overview"
                defaultExpanded
            >
                <Box sx={KPI_GRID_SX}>
                    <KpiTile
                        label="Live Tuples"
                        value={liveTuples !== null
                            ? formatNumber(liveTuples)
                            : '--'}
                    />
                    <KpiTile
                        label="Dead Tuples"
                        value={deadTuples !== null
                            ? formatNumber(deadTuples)
                            : '--'}
                        unit={deadRatio !== null
                            ? `(${deadRatio.toFixed(1)}% dead)`
                            : undefined}
                        status={getDeadTupleStatus(deadRatio)}
                    />
                    <KpiTile
                        label="Table Size"
                        value={tableData?.table_size_pretty
                            ?? formatBytes(tableSize)}
                    />
                    <KpiTile
                        label="Sequential Scans"
                        value={seqScans !== null
                            ? formatNumber(seqScans)
                            : '--'}
                    />
                </Box>
            </CollapsibleSection>

            <CollapsibleSection
                title="Activity Charts"
                defaultExpanded
            >
                <Box sx={CHART_SECTION_SX}>
                    <Box>
                        {tupleChart.loading
                            && !tupleChartData ? (
                                <Box sx={{
                                    display: 'flex',
                                    justifyContent: 'center',
                                    alignItems: 'center',
                                    height: CHART_HEIGHT,
                                }}>
                                    <CircularProgress size={24} aria-label="Loading chart" />
                                </Box>
                            ) : tupleChartData ? (
                                <Chart
                                    type="line"
                                    data={tupleChartData}
                                    title="Tuple Operations Over Time"
                                    height={CHART_HEIGHT}
                                    smooth
                                    showLegend
                                    showTooltip
                                    enableExport={false}
                                    analysisContext={{
                                        metricDescription: `Tuple operations (inserts, updates, deletes, hot updates) for table ${schemaName}.${objectName}`,
                                        connectionId,
                                        databaseName,
                                        timeRange: timeRange.range,
                                    }}
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
                                    No tuple operation data available
                                </Typography>
                            )}
                    </Box>

                    <Box>
                        {scanChart.loading
                            && !scanChartData ? (
                                <Box sx={{
                                    display: 'flex',
                                    justifyContent: 'center',
                                    alignItems: 'center',
                                    height: CHART_HEIGHT,
                                }}>
                                    <CircularProgress size={24} aria-label="Loading chart" />
                                </Box>
                            ) : scanChartData ? (
                                <Chart
                                    type="line"
                                    data={scanChartData}
                                    title={
                                        'Sequential vs Index Scans'
                                        + ' Over Time'
                                    }
                                    height={CHART_HEIGHT}
                                    smooth
                                    showLegend
                                    showTooltip
                                    enableExport={false}
                                    analysisContext={{
                                        metricDescription: `Sequential scan vs index scan activity for table ${schemaName}.${objectName}`,
                                        connectionId,
                                        databaseName,
                                        timeRange: timeRange.range,
                                    }}
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
                                    No scan data available
                                </Typography>
                            )}
                    </Box>

                    <Box>
                        {deadTupleChart.loading
                            && !deadTupleChartData ? (
                                <Box sx={{
                                    display: 'flex',
                                    justifyContent: 'center',
                                    alignItems: 'center',
                                    height: CHART_HEIGHT,
                                }}>
                                    <CircularProgress size={24} aria-label="Loading chart" />
                                </Box>
                            ) : deadTupleChartData ? (
                                <Chart
                                    type="line"
                                    data={deadTupleChartData}
                                    title={
                                        'Dead Tuple Ratio Over Time'
                                    }
                                    height={CHART_HEIGHT}
                                    smooth
                                    areaFill
                                    showLegend
                                    showTooltip
                                    enableExport={false}
                                    analysisContext={{
                                        metricDescription: `Dead tuple ratio indicating vacuum effectiveness for table ${schemaName}.${objectName}`,
                                        connectionId,
                                        databaseName,
                                        timeRange: timeRange.range,
                                    }}
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
                                    No dead tuple data available
                                </Typography>
                            )}
                    </Box>
                </Box>
            </CollapsibleSection>

            {tableData && (
                <CollapsibleSection
                    title="Maintenance Info"
                    defaultExpanded
                >
                    <Box>
                        <Box sx={MAINT_ROW_SX}>
                            <Typography sx={MAINT_LABEL_SX}>
                                Last Vacuum
                            </Typography>
                            <Typography sx={MAINT_VALUE_SX}>
                                {formatTimestamp(
                                    tableData.last_vacuum
                                )}
                            </Typography>
                        </Box>
                        <Box sx={MAINT_ROW_SX}>
                            <Typography sx={MAINT_LABEL_SX}>
                                Last Autovacuum
                            </Typography>
                            <Typography sx={MAINT_VALUE_SX}>
                                {formatTimestamp(
                                    tableData.last_autovacuum
                                )}
                            </Typography>
                        </Box>
                        <Box sx={MAINT_ROW_SX}>
                            <Typography sx={MAINT_LABEL_SX}>
                                Last Analyze
                            </Typography>
                            <Typography sx={MAINT_VALUE_SX}>
                                {formatTimestamp(
                                    tableData.last_analyze
                                )}
                            </Typography>
                        </Box>
                        <Box sx={MAINT_ROW_SX}>
                            <Typography sx={MAINT_LABEL_SX}>
                                Last Autoanalyze
                            </Typography>
                            <Typography sx={MAINT_VALUE_SX}>
                                {formatTimestamp(
                                    tableData.last_autoanalyze
                                )}
                            </Typography>
                        </Box>
                    </Box>
                </CollapsibleSection>
            )}
        </Box>
    );
};

export default TableDetail;

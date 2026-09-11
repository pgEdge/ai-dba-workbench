/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import { Storage as StorageIcon } from '@mui/icons-material';
import { useDashboard } from '../../../contexts/useDashboard';
import { useMetrics } from '../../../hooks/useMetrics';
import type { MetricDataPoint, MetricQueryParams, MetricSeries } from '../types';
import { KPI_GRID_SX, CHART_SECTION_SX } from '../styles';
import KpiTile from '../KpiTile';
import CollapsibleSection from '../CollapsibleSection';
import { Chart } from '../../Chart';
import ChartPanel from '../ChartPanel';
import { apiGet } from '../../../utils/apiClient';
import { logger } from '../../../utils/logger';
import { formatBytes, formatValue, formatNumber } from '../../../utils/formatters';
import {
    type ServerSectionProps, extractSparklineData, extractLatestValue,
    extractLatestRate,
} from './types';

/** Number of data buckets for KPI sparklines */
const KPI_BUCKETS = 30;

/** Number of data buckets for full charts */
const CHART_BUCKETS = 150;

/** Chart height in pixels */
const CHART_HEIGHT = 250;

/**
 * Titles for the connection charts. The pg_stat_database probe filters
 * on current_database(), so both charts cover the monitored database
 * rather than the whole server; the titles say so explicitly.
 */
const CONNECTIONS_CHART_TITLE = 'Connections (Monitored Database)';
const SESSIONS_CHART_TITLE = 'Sessions Established (Monitored Database)';

/**
 * Build chart data from metric series for the Chart component.
 */
const buildChartData = (
    series: MetricSeries[] | null,
    metricNames: string[],
    displayNames?: string[],
) => {
    if (!series) { return null; }

    const matchedSeries = metricNames.map((metric, idx) => {
        const found = series.find(s => s.metric === metric);
        return {
            name: displayNames?.[idx] ?? metric,
            data: found?.data.map(d => d.value) ?? [],
            categories: found?.data.map(d => d.time) ?? [],
        };
    });

    if (matchedSeries.every(s => s.data.length === 0)) { return null; }

    const categories = matchedSeries.find(
        s => s.categories.length > 0
    )?.categories ?? [];

    return {
        categories,
        series: matchedSeries.map(s => ({
            name: s.name,
            data: s.data,
        })),
    };
};

/** Shape of the latest pg_server_info row used for max_connections. */
interface ServerInfoRow {
    max_connections?: number | null;
}

/** A max_connections lookup result tied to the connection it came from. */
interface MaxConnectionsResult {
    connectionId: number;
    value: number | null;
}

/**
 * Fetch the configured max_connections for a connection from the most
 * recent pg_server_info row. The probe only stores a row when the
 * server configuration changes, so the latest-row query is used rather
 * than a time-bucketed series that would usually be empty.
 *
 * The result carries the connection it belongs to, so that switching
 * connections never draws the previous server's limit over the new
 * server's backend count whilst the fresh lookup is still in flight.
 */
const useMaxConnections = (connectionId: number): number | null => {
    const [result, setResult] = useState<MaxConnectionsResult>({
        connectionId,
        value: null,
    });

    useEffect(() => {
        const controller = new AbortController();
        const setValue = (value: number | null) => {
            setResult({ connectionId, value });
        };
        const params = new URLSearchParams({
            probe_name: 'pg_server_info',
            connection_id: connectionId.toString(),
            limit: '1',
            order_by: 'collected_at',
            order: 'desc',
        });

        apiGet<ServerInfoRow[]>(
            `/api/v1/metrics/query?${params.toString()}`,
            { signal: controller.signal },
        )
            .then((rows) => {
                if (controller.signal.aborted) { return; }
                const value = Array.isArray(rows) ? rows[0]?.max_connections : null;
                setValue(typeof value === 'number' && value > 0 ? value : null);
            })
            .catch((err: unknown) => {
                if (controller.signal.aborted) { return; }
                logger.error('Error fetching max_connections:', err);
                setValue(null);
            });

        return () => { controller.abort(); };
    }, [connectionId]);

    return result.connectionId === connectionId ? result.value : null;
};

/**
 * PostgreSQL Overview section displays database-specific metrics
 * including connections, transactions, block I/O, and tuple
 * operation statistics from pg_stat_database.
 */
const PostgresOverviewSection: React.FC<ServerSectionProps> = ({
    connectionId,
    connectionName,
}) => {
    const { timeRange } = useDashboard();

    // KPI queries (30 buckets) - all from pg_stat_database
    const connectionsKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['numbackends'],
    }), [connectionId, timeRange.range]);

    const txnKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['xact_commit_per_sec'],
    }), [connectionId, timeRange.range]);

    const cacheKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['blks_hit', 'blks_read'],
    }), [connectionId, timeRange.range]);

    const tempKpiParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: KPI_BUCKETS,
        aggregation: 'avg',
        metrics: ['temp_bytes_delta'],
    }), [connectionId, timeRange.range]);

    // Chart queries (150 buckets)
    const connectionChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['numbackends', 'sessions'],
    }), [connectionId, timeRange.range]);

    const txnChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['xact_commit_per_sec', 'xact_rollback_per_sec'],
    }), [connectionId, timeRange.range]);

    const blockIoChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: ['blks_hit_per_sec', 'blks_read_per_sec'],
    }), [connectionId, timeRange.range]);

    const tupleChartParams = useMemo((): MetricQueryParams => ({
        probeName: 'pg_stat_database',
        connectionId,
        timeRange: timeRange.range,
        buckets: CHART_BUCKETS,
        aggregation: 'avg',
        metrics: [
            'tup_fetched_per_sec',
            'tup_inserted_per_sec',
            'tup_updated_per_sec',
            'tup_deleted_per_sec',
        ],
    }), [connectionId, timeRange.range]);

    // Fetch KPI data
    const connectionsKpi = useMetrics(connectionsKpiParams);
    const txnKpi = useMetrics(txnKpiParams);
    const cacheKpi = useMetrics(cacheKpiParams);
    const tempKpi = useMetrics(tempKpiParams);

    // Fetch chart data
    const connectionChart = useMetrics(connectionChartParams);
    const maxConnections = useMaxConnections(connectionId);
    const txnChart = useMetrics(txnChartParams);
    const blockIoChart = useMetrics(blockIoChartParams);
    const tupleChart = useMetrics(tupleChartParams);

    // Extract current values
    const numBackends = extractLatestValue(
        connectionsKpi.data, 'numbackends'
    );
    const xactCommitRate = extractLatestRate(
        txnKpi.data, 'xact_commit_per_sec'
    );
    const blksHit = extractLatestValue(
        cacheKpi.data, 'blks_hit'
    );
    const blksRead = extractLatestValue(
        cacheKpi.data, 'blks_read'
    );
    const cacheHitRatio = useMemo(() => {
        if (blksHit !== null && blksRead !== null) {
            const total = blksHit + blksRead;
            if (total > 0) { return (blksHit / total) * 100; }
        }
        return null;
    }, [blksHit, blksRead]);
    const cacheHitRatioSparkline = useMemo(() => {
        const hitData = extractSparklineData(cacheKpi.data, 'blks_hit');
        const readData = extractSparklineData(cacheKpi.data, 'blks_read');
        if (hitData.length === 0 && readData.length === 0) { return []; }
        const len = Math.max(hitData.length, readData.length);
        const result: MetricDataPoint[] = [];
        for (let i = 0; i < len; i++) {
            const hit = i < hitData.length ? hitData[i].value : 0;
            const read = i < readData.length ? readData[i].value : 0;
            const total = hit + read;
            result.push({
                time: (i < hitData.length
                    ? hitData[i] : readData[i]).time,
                value: total > 0 ? (hit / total) * 100 : 0,
            });
        }
        return result;
    }, [cacheKpi.data]);
    /*
     * temp_bytes_delta reports the bytes spilled to temporary files in
     * each bucket, so the figure for the selected window is the sum of
     * the buckets rather than the latest one.
     */
    const tempBytesTotal = useMemo(() => {
        const points = extractSparklineData(tempKpi.data, 'temp_bytes_delta');
        if (points.length === 0) { return null; }
        return points.reduce((total, point) => total + point.value, 0);
    }, [tempKpi.data]);

    // Build chart datasets. Backends and sessions are deliberately kept
    // apart: numbackends is a gauge bounded by max_connections, whilst
    // sessions is a counter that only ever climbs, so plotting them on
    // one axis flattens the gauge and invites a false comparison.
    const connectionChartData = useMemo(() => {
        const base = buildChartData(
            connectionChart.data,
            ['numbackends'],
            ['Backends'],
        );
        if (!base || maxConnections === null) { return base; }
        return {
            ...base,
            series: [
                ...base.series,
                {
                    name: 'Max Connections',
                    data: base.categories.map(() => maxConnections),
                },
            ],
        };
    }, [connectionChart.data, maxConnections]);

    const sessionChartData = useMemo(
        () => buildChartData(
            connectionChart.data,
            ['sessions'],
            ['Cumulative Sessions'],
        ),
        [connectionChart.data]
    );

    const txnChartData = useMemo(
        () => buildChartData(
            txnChart.data,
            ['xact_commit_per_sec', 'xact_rollback_per_sec'],
            ['Commits/s', 'Rollbacks/s'],
        ),
        [txnChart.data]
    );

    const blockIoChartData = useMemo(
        () => buildChartData(
            blockIoChart.data,
            ['blks_hit_per_sec', 'blks_read_per_sec'],
            ['Blocks Hit/s', 'Blocks Read/s'],
        ),
        [blockIoChart.data]
    );

    const tupleChartData = useMemo(
        () => buildChartData(
            tupleChart.data,
            [
                'tup_fetched_per_sec',
                'tup_inserted_per_sec',
                'tup_updated_per_sec',
                'tup_deleted_per_sec',
            ],
            ['Fetched/s', 'Inserted/s', 'Updated/s', 'Deleted/s'],
        ),
        [tupleChart.data]
    );

    const isKpiLoading = connectionsKpi.loading || txnKpi.loading
        || cacheKpi.loading || tempKpi.loading;

    return (
        <CollapsibleSection title="PostgreSQL Overview" icon={<StorageIcon sx={{ fontSize: 16 }} />} defaultExpanded>
            {isKpiLoading && !connectionsKpi.data && (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
                    <CircularProgress size={24} aria-label="Loading" />
                </Box>
            )}
            <Box sx={KPI_GRID_SX}>
                <KpiTile
                    label="Backends"
                    value={numBackends !== null
                        ? formatNumber(Math.round(numBackends))
                        : '--'}
                    sparklineData={extractSparklineData(
                        connectionsKpi.data, 'numbackends'
                    )}
                    analysisContext={{
                        metricDescription: 'PostgreSQL backend connection count over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Commits"
                    value={formatValue(xactCommitRate)}
                    unit={xactCommitRate !== null ? '/s' : undefined}
                    sparklineData={extractSparklineData(
                        txnKpi.data, 'xact_commit_per_sec'
                    )}
                    analysisContext={{
                        metricDescription: 'Transaction commits per second over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Cache Hit Ratio"
                    value={cacheHitRatio !== null
                        ? formatValue(cacheHitRatio)
                        : '--'}
                    unit={cacheHitRatio !== null ? '%' : undefined}
                    sparklineData={cacheHitRatioSparkline}
                    analysisContext={{
                        metricDescription: 'Buffer cache hit ratio over time',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
                <KpiTile
                    label="Temp Bytes"
                    value={formatBytes(tempBytesTotal)}
                    sparklineData={extractSparklineData(
                        tempKpi.data, 'temp_bytes_delta'
                    )}
                    analysisContext={{
                        metricDescription: 'Temporary file bytes written per interval',
                        connectionId,
                        connectionName,
                        timeRange: timeRange.range,
                    }}
                />
            </Box>

            <Box sx={CHART_SECTION_SX}>
                <Box>
                    <ChartPanel
                        title={CONNECTIONS_CHART_TITLE}
                        loading={connectionChart.loading && !connectionChartData}
                        hasData={!!connectionChartData}
                        emptyMessage="No connection data available"
                        errorMessage={connectionChart.error}
                        height={CHART_HEIGHT}
                    >
                        {connectionChartData && (
                            <Chart
                                type="line"
                                data={connectionChartData}
                                title={CONNECTIONS_CHART_TITLE}
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'PostgreSQL backends connected to the monitored database over time, against the max_connections limit',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>

                <Box>
                    <ChartPanel
                        title={SESSIONS_CHART_TITLE}
                        loading={connectionChart.loading && !sessionChartData}
                        hasData={!!sessionChartData}
                        emptyMessage="No session data available"
                        errorMessage={connectionChart.error}
                        height={CHART_HEIGHT}
                    >
                        {sessionChartData && (
                            <Chart
                                type="line"
                                data={sessionChartData}
                                title={SESSIONS_CHART_TITLE}
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Cumulative sessions established against the monitored database since the last statistics reset',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>

                <Box>
                    <ChartPanel
                        title="Transactions"
                        loading={txnChart.loading && !txnChartData}
                        hasData={!!txnChartData}
                        emptyMessage="No transaction data available"
                        errorMessage={txnChart.error}
                        height={CHART_HEIGHT}
                    >
                        {txnChartData && (
                            <Chart
                                type="line"
                                data={txnChartData}
                                title="Transactions"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Transaction commits and rollbacks per second',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>

                <Box>
                    <ChartPanel
                        title="Block I/O"
                        loading={blockIoChart.loading && !blockIoChartData}
                        hasData={!!blockIoChartData}
                        emptyMessage="No block I/O data available"
                        errorMessage={blockIoChart.error}
                        height={CHART_HEIGHT}
                    >
                        {blockIoChartData && (
                            <Chart
                                type="line"
                                data={blockIoChartData}
                                title="Block I/O"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Block I/O per second showing cache hits vs disk reads',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>

                <Box>
                    <ChartPanel
                        title="Tuple Operations"
                        loading={tupleChart.loading && !tupleChartData}
                        hasData={!!tupleChartData}
                        emptyMessage="No tuple operation data available"
                        errorMessage={tupleChart.error}
                        height={CHART_HEIGHT}
                    >
                        {tupleChartData && (
                            <Chart
                                type="line"
                                data={tupleChartData}
                                title="Tuple Operations"
                                height={CHART_HEIGHT}
                                smooth
                                showLegend
                                showTooltip
                                enableExport={false}
                                analysisContext={{
                                    metricDescription: 'Tuple operations per second showing rows fetched, inserted, updated, and deleted',
                                    connectionId,
                                    connectionName,
                                    timeRange: timeRange.range,
                                }}
                            />
                        )}
                    </ChartPanel>
                </Box>
            </Box>
        </CollapsibleSection>
    );
};

export default PostgresOverviewSection;

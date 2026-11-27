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
import { useMemo, useState, useCallback, useEffect, useRef } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import { useTheme } from '@mui/material/styles';
import { Chart } from '../../Chart';
import { useAuth } from '../../../contexts/useAuth';
import { useClusterData } from '../../../contexts/useClusterData';
import { apiFetch } from '../../../utils/apiClient';
import { computeEstateServerCounts } from '../../../utils/clusterHelpers';
import { chunkConnectionIds } from '../../../utils/connectionIdBatches';
import { logger } from '../../../utils/logger';
import { useRetryingFetch } from '../../../hooks/useRetryingFetch';
import { useRequestSequence } from '../../../hooks/useRequestSequence';
import type { EstateSelection } from '../../../types/selection';

interface HealthOverviewSectionProps {
    selection: EstateSelection;
    serverIds: number[];
}

interface AlertCounts {
    critical: number;
    warning: number;
    info: number;
}

const SEVERITIES = ['critical', 'warning', 'info'] as const;

/** The part of a `/api/v1/alerts/counts` response the chart reads. */
interface SeverityCountsResponse {
    by_severity?: Partial<Record<(typeof SEVERITIES)[number], unknown>>;
}

const NO_ALERTS: AlertCounts = { critical: 0, warning: 0, info: 0 };

/**
 * The longest one alert-count request may take, response body
 * included, before it is aborted and the attempt fails, so a request
 * that never answers cannot hold off the retry. The bound is per
 * request rather than per attempt because the requests run one after
 * another, one per batch of servers, so an attempt over many batches
 * may legitimately take longer than this.
 */
export const ALERT_COUNT_REQUEST_TIMEOUT_MS = 15000;

/** A complete set of counts and the servers they were fetched for. */
interface AlertCountsResult {
    serverIdsKey: string;
    counts: AlertCounts;
}

/** The attempt in flight, which a refresh for the same servers joins. */
interface InFlightAttempt {
    serverIdsKey: string;
    user: ReturnType<typeof useAuth>['user'];
    isCurrent: () => boolean;
    promise: Promise<boolean>;
}

const RINGS_CONTAINER_SX = {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
    gap: 2,
    '@media (max-width: 600px)': {
        gridTemplateColumns: 'minmax(0, 1fr)',
    },
};

const RING_LABEL_SX = {
    textAlign: 'center',
    fontWeight: 600,
    fontSize: '0.875rem',
    color: 'text.secondary',
    mt: 1,
};

const ALERT_PANEL_SX = {
    height: 200,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
};

/**
 * HealthOverviewSection shows fleet health via two donut charts:
 * one for server status distribution (online/warning/offline) and
 * one for the active alerts on the estate's servers by severity
 * (critical/warning/info). The alert counts refresh with the
 * estate data.
 */
const HealthOverviewSection: React.FC<HealthOverviewSectionProps> = ({ selection, serverIds }) => {
    const theme = useTheme();
    const { user } = useAuth();
    const { lastRefresh } = useClusterData();
    const [alertResult, setAlertResult] = useState<AlertCountsResult | null>(null);
    const [alertsLoading, setAlertsLoading] = useState<boolean>(false);
    // useRetryingFetch checks its own attempt generation only after the
    // fetch has written state, so overlapping refreshes are ordered
    // here instead.
    const { beginRequest, supersedeRequest } = useRequestSequence();
    const { run } = useRetryingFetch({
        resetKey: lastRefresh,
        enabled: !!user && serverIds.length > 0,
    });

    const serverCounts = useMemo(() => computeEstateServerCounts(selection), [selection]);
    const serverIdsKey = serverIds.join(',');

    const inFlightRef = useRef<InFlightAttempt | null>(null);

    const startAttempt = useCallback(async (
        key: string,
        isCurrent: () => boolean,
    ): Promise<boolean> => {
        // The counts match the Active Alerts tile: /alerts/counts counts
        // only active alerts, and naming the estate's servers leaves out
        // system alerts and other servers. Each response counts every
        // severity from one query, so the three figures cannot disagree.
        // The batches are disjoint, so their counts sum.
        const urls = chunkConnectionIds(serverIds).map(batch => {
            const params = new URLSearchParams({
                connection_ids: batch.join(','),
            });
            return `/api/v1/alerts/counts?${params.toString()}`;
        });

        setAlertsLoading(true);

        try {
            // The requests run one at a time and the attempt stops at
            // the first failure. apiFetch counts every failed request
            // toward the app-wide disconnect threshold, so requests
            // issued together could spend that whole budget in one
            // failed attempt; one at a time, an attempt counts at most
            // one failure, and a failure is acted on at once rather
            // than after every other request has answered. Aborting
            // concurrent requests on the first failure would not help,
            // as apiFetch counts an aborted request as a failure too;
            // for the same reason a superseded attempt is not aborted
            // while its request is in flight; it sends no further
            // request once its current one answers. Only a request's own
            // timeout aborts it in flight, and that counts as the
            // attempt's one failure. Once apiFetch has returned, the
            // status is already recorded, so a response the attempt
            // discards is aborted to cancel its body, which the timeout
            // would otherwise have bounded.
            const counts: AlertCounts = { ...NO_ALERTS };
            for (const url of urls) {
                const controller = new AbortController();
                const timer = setTimeout(
                    () => controller.abort(),
                    ALERT_COUNT_REQUEST_TIMEOUT_MS,
                );
                let body: SeverityCountsResponse;
                try {
                    const response = await apiFetch(url, { signal: controller.signal });

                    // A superseded or unmounted attempt is not a failure.
                    if (!isCurrent()) {
                        controller.abort();
                        return true;
                    }

                    // One failed request fails the refresh, so the chart
                    // keeps its last complete distribution and the fetch
                    // is retried rather than showing a partial one.
                    if (!response.ok) {
                        controller.abort();
                        return false;
                    }

                    body = await response.json() as SeverityCountsResponse;
                } finally {
                    clearTimeout(timer);
                }
                if (!isCurrent()) { return true; }
                for (const severity of SEVERITIES) {
                    const count = body.by_severity?.[severity];
                    if (typeof count === 'number') {
                        counts[severity] += count;
                    }
                }
            }

            setAlertResult({ serverIdsKey: key, counts });
            return true;
        } catch (err) {
            logger.error('Error fetching alert counts:', err);
            return false;
        } finally {
            if (isCurrent()) {
                setAlertsLoading(false);
            }
        }
    }, [serverIds]);

    const fetchAlertCounts = useCallback(async (): Promise<boolean> => {
        if (!user || serverIds.length === 0) { return true; }

        const key = serverIds.join(',');

        // An attempt over many batches can outlast the refresh interval,
        // so a refresh that superseded it would never let it publish. A
        // refresh for the same servers therefore joins the attempt in
        // flight, and returns its outcome so that run() retries a
        // failure as usual. Different servers, or an attempt already
        // superseded, start afresh.
        const inFlight = inFlightRef.current;
        if (
            inFlight?.serverIdsKey === key
            && inFlight.user === user
            && inFlight.isCurrent()
        ) {
            return inFlight.promise;
        }

        const isCurrent = beginRequest();
        const promise = startAttempt(key, isCurrent);
        inFlightRef.current = { serverIdsKey: key, user, isCurrent, promise };
        try {
            return await promise;
        } finally {
            if (inFlightRef.current?.promise === promise) {
                inFlightRef.current = null;
            }
        }
    }, [user, serverIds, beginRequest, startAttempt]);

    // An estate with no servers has no alerts to fetch; drop any
    // request still in flight for the old servers.
    useEffect(() => {
        if (serverIdsKey === '') {
            supersedeRequest();
            setAlertsLoading(false);
        }
    }, [serverIdsKey, supersedeRequest]);

    // Counts are shown only for the servers they were fetched for, so
    // a different set of servers is a fresh load and the previous
    // servers' counts are never shown as this set's. An estate with no
    // servers has no alerts.
    const alertCounts: AlertCounts | null = serverIdsKey === ''
        ? NO_ALERTS
        : alertResult?.serverIdsKey === serverIdsKey ? alertResult.counts : null;

    // Refetch on mount, when the servers change, and on every estate
    // data refresh (lastRefresh), as the KPI tiles do; a refresh while
    // an attempt for the same servers is in flight joins that attempt.
    useEffect(() => {
        if (user && serverIds.length > 0) {
            void run(fetchAlertCounts);
        }
    }, [user, serverIds.length, run, fetchAlertCounts, lastRefresh]);

    const serverStatusData = useMemo(() => ({
        categories: ['OK', 'Warning', 'Offline'],
        series: [{
            name: 'Servers',
            data: [serverCounts.online, serverCounts.warning, serverCounts.offline],
        }],
    }), [serverCounts]);

    const alertDistData = useMemo(() => ({
        categories: ['Critical', 'Warning', 'Info'],
        series: [{
            name: 'Alerts',
            data: alertCounts
                ? [alertCounts.critical, alertCounts.warning, alertCounts.info]
                : [0, 0, 0],
        }],
    }), [alertCounts]);

    const serverColors = useMemo(() => [
        theme.palette.success.main,
        theme.palette.warning.main,
        theme.palette.error.main,
    ], [theme]);

    const alertColors = useMemo(() => [
        theme.palette.error.main,
        theme.palette.warning.main,
        theme.palette.info.main,
    ], [theme]);

    const totalAlerts = alertCounts
        ? alertCounts.critical + alertCounts.warning + alertCounts.info
        : 0;

    // Server Status comes from the selection and always renders; only
    // the Alert Distribution panel waits on the alert counts. Without a
    // complete result for these servers, an attempt is either in flight
    // or failed and being retried. A failure says nothing about whether
    // alerts exist, so the panel says the counts are unavailable rather
    // than that there are none.
    let alertPanel: React.ReactNode;
    if (alertCounts && totalAlerts > 0) {
        alertPanel = (
            <Chart
                type="donut"
                data={alertDistData}
                height={200}
                showLegend
                showTooltip
                enableExport={false}
                colorPalette={alertColors}
                showPercentage
                analysisContext={{
                    metricDescription: 'Distribution of alerts by severity across the estate',
                }}
            />
        );
    } else if (!alertCounts && alertsLoading) {
        alertPanel = (
            <Box sx={ALERT_PANEL_SX}>
                <CircularProgress size={32} aria-label="Loading alert distribution" />
            </Box>
        );
    } else {
        alertPanel = (
            <Box sx={ALERT_PANEL_SX}>
                <Typography
                    sx={{
                        color: 'text.secondary',
                        fontSize: '0.875rem',
                    }}
                >
                    {alertCounts ? 'No active alerts' : 'Alert counts unavailable'}
                </Typography>
            </Box>
        );
    }

    return (
        <Box sx={RINGS_CONTAINER_SX}>
            <Box>
                <Chart
                    type="donut"
                    data={serverStatusData}
                    height={200}
                    showLegend
                    showTooltip
                    enableExport={false}
                    colorPalette={serverColors}
                    showPercentage
                    analysisContext={{
                        metricDescription: 'Distribution of server health statuses across the estate',
                    }}
                />
                <Typography sx={RING_LABEL_SX}>
                    Server Status
                </Typography>
            </Box>
            <Box>
                {alertPanel}
                <Typography sx={RING_LABEL_SX}>
                    Alert Distribution
                </Typography>
            </Box>
        </Box>
    );
};

export default HealthOverviewSection;

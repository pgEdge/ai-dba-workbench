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
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import FormControl from '@mui/material/FormControl';
import InputLabel from '@mui/material/InputLabel';
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import type { SelectChangeEvent } from '@mui/material/Select';
import Typography from '@mui/material/Typography';
import { useTheme } from '@mui/material/styles';
import { Insights as InsightsIcon } from '@mui/icons-material';
import {
    useAnomalyComparison,
    useComparisonMetrics,
    type AnomalyComparisonQuery,
} from '../../../hooks/useAnomalyComparison';
import { useDatabaseSummaries } from '../../../hooks/useDatabaseSummaries';
import { DASHBOARD_CONTROL_TEXT_SX } from '../../../theme/tokens';
import { Chart } from '../../Chart';
import type { TimeRangeState } from '../types';
import ChartPanel from '../ChartPanel';
import CollapsibleSection from '../CollapsibleSection';
import type { ServerSectionProps } from './types';
import {
    MODEL_OPTIONS,
    SENSITIVITY_OPTIONS,
    buildComparisonChart,
    buildComparisonOptions,
    describeComparisonChart,
    filterEngineAlerts,
    friendlyMetricLabel,
    nearestSensitivity,
    noBaselineCaption,
    type ComparisonModel,
} from './baselineComparison';

/** Metric shown first, when the server offers it. */
const DEFAULT_METRIC = 'pg_sys_cpu_usage_info.processor_time_percent';

/** The database list only needs names, so a fixed window will do. */
const DATABASE_LIST_TIME_RANGE: TimeRangeState = { range: '24h' };

const CHART_HEIGHT = 320;

const CONTROLS_SX = {
    display: 'flex',
    flexWrap: 'wrap' as const,
    gap: 1.5,
    mb: 1.5,
};

const CONTROL_SX = {
    minWidth: 140,
    '& .MuiInputBase-root': DASHBOARD_CONTROL_TEXT_SX,
    '& .MuiInputLabel-root': DASHBOARD_CONTROL_TEXT_SX,
};

const METRIC_CONTROL_SX = { ...CONTROL_SX, minWidth: 240 };

const MESSAGE_SX = { textAlign: 'center' as const, py: 3 };

/** One labelled select; the label is the select's accessible name. */
const ControlSelect: React.FC<{
    id: string;
    label: string;
    value: string;
    onChange: (value: string) => void;
    options: { value: string; label: string }[];
    sx?: object;
}> = ({ id, label, value, onChange, options, sx = CONTROL_SX }) => (
    <FormControl size="small" sx={sx}>
        <InputLabel id={`${id}-label`}>{label}</InputLabel>
        <Select
            labelId={`${id}-label`}
            id={id}
            label={label}
            value={value}
            onChange={(event: SelectChangeEvent) => onChange(event.target.value)}
        >
            {options.map((option) => (
                <MenuItem key={option.value} value={option.value}>
                    {option.label}
                </MenuItem>
            ))}
        </Select>
    </FormControl>
);

/**
 * Baseline comparison charts one baselined metric against the band the
 * alerter's anomaly engine scores it with. Solid markers are the
 * anomaly alerts the engine raised; hollow markers are points outside
 * the band that raised none, which shows how a different model or
 * sensitivity would have behaved.
 */
const BaselineComparisonSection: React.FC<ServerSectionProps> = ({ connectionId }) => {
    const theme = useTheme();
    const metricsState = useComparisonMetrics();
    const { metrics } = metricsState;

    const [metricChoice, setMetricChoice] = useState<string | null>(null);
    const [databaseChoice, setDatabaseChoice] = useState<string | null>(null);
    const [model, setModel] = useState<ComparisonModel>('as_scored');
    // Null until the server's default is known or the user picks one.
    const [sensitivity, setSensitivity] = useState<number | null>(null);

    const metric = useMemo(() => {
        const chosen = metrics.find((m) => m.name === metricChoice);
        return chosen ?? metrics.find((m) => m.name === DEFAULT_METRIC) ?? metrics[0] ?? null;
    }, [metrics, metricChoice]);
    const perDatabase = metric?.per_database ?? false;

    const { databases } = useDatabaseSummaries(
        connectionId,
        0,
        DATABASE_LIST_TIME_RANGE,
        perDatabase
    );
    const databaseNames = useMemo(
        () =>
            databases
                .map((db) => db.database_name)
                .filter(Boolean)
                .sort((a, b) => a.localeCompare(b)),
        [databases]
    );
    const databaseName = perDatabase
        ? (databaseNames.find((name) => name === databaseChoice) ?? databaseNames[0] ?? null)
        : null;

    const query = useMemo<AnomalyComparisonQuery | null>(() => {
        if (!metric || (perDatabase && databaseName === null)) {
            return null;
        }
        return {
            connectionId,
            metric: metric.name,
            databaseName,
            model,
            sensitivity,
        };
    }, [connectionId, metric, perDatabase, databaseName, model, sensitivity]);

    const { data, alerts, window, loading, error, alertsError, alertsCompleteFromMs } =
        useAnomalyComparison(query);

    // Adopt the server's default once it is known. A default the
    // selector does not offer is rounded and asked for explicitly, so
    // that the band drawn matches the sensitivity shown.
    const defaultSensitivity = data ? nearestSensitivity(data.default_sensitivity) : null;
    const usedSensitivity = data?.sensitivity;
    useEffect(() => {
        if (
            sensitivity === null &&
            defaultSensitivity !== null &&
            defaultSensitivity !== usedSensitivity
        ) {
            setSensitivity(defaultSensitivity);
        }
    }, [sensitivity, defaultSensitivity, usedSensitivity]);
    const shownSensitivity = sensitivity ?? defaultSensitivity ?? nearestSensitivity(undefined);

    const valueLabel = metric ? friendlyMetricLabel(metric.name) : '';
    const title = `${valueLabel} against its baseline`;
    const points = useMemo(() => data?.points ?? [], [data]);

    const chartData = useMemo(() => {
        if (!data || !window || points.length === 0) {
            return null;
        }
        const engineAlerts = filterEngineAlerts(alerts, data.metric, data.database_name);
        const chart = buildComparisonChart(
            points,
            engineAlerts,
            data.sensitivity,
            window,
            alertsCompleteFromMs
        );
        const options = buildComparisonOptions(chart, valueLabel, data.sensitivity, {
            value: theme.palette.primary.main,
            band: theme.palette.info.main,
            engine: theme.palette.error.main,
            outside: theme.palette.warning.main,
            background: theme.palette.background.paper,
        });
        return {
            options,
            hasBand: chart.hasBand,
            hasBelowFloor: chart.hasBelowFloor,
            summary: describeComparisonChart(chart, valueLabel, window),
            // Sizes the y axis and supplies the categories; the series
            // drawn are those in the options, which replace these.
            data: {
                categories: chart.categories,
                series: [
                    { name: valueLabel, data: points.map((p) => p.value) },
                    { name: 'upper', data: chart.upper },
                    { name: 'lower', data: chart.lower },
                ],
            },
        };
    }, [data, window, points, alerts, alertsCompleteFromMs, valueLabel, theme]);

    const metricOptions = useMemo(
        () =>
            metrics.map((m) => ({
                value: m.name,
                label: friendlyMetricLabel(m.name),
            })),
        [metrics]
    );

    const renderBody = (): React.ReactNode => {
        if (metricsState.loading) {
            return (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                    <CircularProgress size={24} aria-label="Loading baselined metrics" />
                </Box>
            );
        }
        if (metricsState.error) {
            return (
                <Typography variant="body2" color="error" sx={MESSAGE_SX}>
                    {metricsState.error}
                </Typography>
            );
        }
        if (!metric) {
            return (
                <Typography variant="body2" color="text.secondary" sx={MESSAGE_SX}>
                    No baselined metrics are available.
                </Typography>
            );
        }

        return (
            <>
                <Box sx={CONTROLS_SX} role="group" aria-label="Comparison settings">
                    <ControlSelect
                        id="baseline-comparison-metric"
                        label="Metric"
                        value={metric.name}
                        onChange={setMetricChoice}
                        options={metricOptions}
                        sx={METRIC_CONTROL_SX}
                    />
                    {perDatabase && databaseName !== null && (
                        <ControlSelect
                            id="baseline-comparison-database"
                            label="Database"
                            value={databaseName}
                            onChange={setDatabaseChoice}
                            options={databaseNames.map((name) => ({
                                value: name,
                                label: name,
                            }))}
                        />
                    )}
                    <ControlSelect
                        id="baseline-comparison-model"
                        label="Baseline model"
                        value={model}
                        onChange={(value) => setModel(value as ComparisonModel)}
                        options={MODEL_OPTIONS}
                    />
                    <ControlSelect
                        id="baseline-comparison-sensitivity"
                        label="Sensitivity"
                        value={shownSensitivity.toString()}
                        onChange={(value) => setSensitivity(Number(value))}
                        options={SENSITIVITY_OPTIONS.map((k) => ({
                            value: k.toString(),
                            label: `${k}σ`,
                        }))}
                    />
                </Box>

                {perDatabase && databaseName === null ? (
                    <Typography variant="body2" color="text.secondary" sx={MESSAGE_SX}>
                        No databases are known for this server.
                    </Typography>
                ) : (
                    <>
                        {data?.scoring_source === 'defaults' && (
                            <Alert severity="info" sx={{ mb: 1.5 }}>
                                Scoring settings: defaults. The alerter has not published its
                                anomaly settings, so the band uses the default sensitivity.
                            </Alert>
                        )}
                        <ChartPanel
                            title={title}
                            loading={loading}
                            hasData={chartData !== null}
                            emptyMessage="No samples of this metric were collected in the selected period."
                            errorMessage={error}
                            height={CHART_HEIGHT}
                        >
                            {chartData && (
                                // The canvas has no text of its own, so the
                                // wrapper names what it shows.
                                <Box role="img" aria-label={chartData.summary}>
                                    <Chart
                                        type="line"
                                        data={chartData.data}
                                        title={title}
                                        height={CHART_HEIGHT}
                                        showLegend
                                        showTooltip
                                        showToolbar={false}
                                        enableExport={false}
                                        echartsOptions={chartData.options}
                                    />
                                </Box>
                            )}
                        </ChartPanel>
                        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                            {chartData && !chartData.hasBand
                                ? noBaselineCaption(data?.model ?? model)
                                : 'Bands are the current baselines applied across the window.'}
                            {chartData?.hasBelowFloor &&
                                ' Some unmarked points beyond the band are below the metric' +
                                    "'s value floor, where the engine does not alert."}
                            {alertsError &&
                                ' The anomaly alerts could not be loaded, so' +
                                    ' engine alerts are not marked.'}
                            {alertsCompleteFromMs !== null &&
                                ' Not every engine alert could be loaded, so' +
                                    ' older points are not marked as outside' +
                                    ' with no engine alert.'}
                        </Typography>
                    </>
                )}
            </>
        );
    };

    return (
        <CollapsibleSection
            title="Baseline comparison"
            icon={<InsightsIcon sx={{ fontSize: 16 }} />}
            defaultExpanded
            storageKey="dashboard-section-baseline-comparison-expanded"
        >
            {renderBody()}
        </CollapsibleSection>
    );
};

export default BaselineComparisonSection;

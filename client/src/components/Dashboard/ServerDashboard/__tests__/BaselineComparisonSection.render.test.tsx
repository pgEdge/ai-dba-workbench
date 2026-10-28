/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ThemeProvider, createTheme, type Theme } from '@mui/material/styles';
import * as echarts from 'echarts';
import BaselineComparisonSection from '../BaselineComparisonSection';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

/*
 * These tests check what ECharts actually paints, not the props the
 * section hands to `Chart`. The section replaces the line chart's
 * series with a stacked band and marker-only series, and a marker-only
 * series separated by gaps paints nothing unless its symbols are
 * shown, which no prop-shape assertion would catch. As in
 * `Sparkline.render.test.tsx`, the real `Chart` is rendered with only
 * the `echarts-for-react` wrapper replaced by ECharts' server-side SVG
 * renderer.
 */
vi.mock('echarts-for-react/esm/core', () => ({
    default: ({ option }: { option: Record<string, unknown> }) => {
        const chart = echarts.init(null, null, {
            renderer: 'svg', ssr: true, width: 600, height: 320,
        });
        chart.setOption({ ...option, animation: false });
        const svg = chart.renderToSVGString();
        chart.dispose();
        return <div data-testid="comparison-svg">{svg}</div>;
    },
}));

vi.mock('../../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: false }),
}));

vi.mock('../../../../hooks/useDatabaseSummaries', () => ({
    useDatabaseSummaries: () => ({ databases: [], loading: false, error: null }),
}));

const CPU = 'pg_sys_cpu_usage_info.processor_time_percent';
const START = Date.parse('2026-10-28T11:00:00Z');
const BUCKET_MS = 12_000;
const at = (index: number): string =>
    new Date(START + index * BUCKET_MS).toISOString();

/** Point 4 is outside with no alert; point 7 is outside and alerted. */
const OUTSIDE_INDEX = 4;
const ALERTED_INDEX = 7;

vi.mock('../../../../hooks/useAnomalyComparison', () => ({
    useComparisonMetrics: () => ({
        metrics: [{ name: CPU, probe_name: 'cpu', per_database: false }],
        loading: false,
        error: null,
    }),
    useAnomalyComparison: () => ({
        data: {
            metric: CPU,
            connection_id: 1,
            database_name: null,
            model: 'as_scored',
            sensitivity: 3,
            default_sensitivity: 3,
            scoring_source: 'alerter',
            baselines_as_of: null,
            points: Array.from({ length: 10 }, (_, i) => ({
                time: at(i),
                value: i === OUTSIDE_INDEX ? 60 : i === ALERTED_INDEX ? 70 : 30,
                period_type: 'hourly',
                mean: 30,
                stddev: 2,
                cold: false,
                z_score: 1,
                outside: i === OUTSIDE_INDEX || i === ALERTED_INDEX,
            })),
        },
        alerts: [{
            id: 1,
            metric_name: CPU,
            metric_value: 70,
            anomaly_score: 20,
            anomaly_details: '{}',
            triggered_at: at(ALERTED_INDEX),
        }],
        window: { startMs: START, endMs: START + 300 * BUCKET_MS, buckets: 300 },
        loading: false,
        error: null,
        alertsError: null,
    }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const PATH_RE = /<path[^>]*>/g;
const CLIP_PATH_SECTION_RE = /<defs[\s\S]*<\/defs>/;

/** The painted `<path>` elements, without the clip paths. */
const paths = (theme: Theme): string[] => {
    render(
        <ThemeProvider theme={theme}>
            <BaselineComparisonSection connectionId={1} />
        </ThemeProvider>,
    );
    const svg = screen.getByTestId('comparison-svg').textContent ?? '';
    return svg.replace(CLIP_PATH_SECTION_RE, '').match(PATH_RE) ?? [];
};

/** The data symbols drawn in the plot area, by data index. */
const symbolsAt = (painted: string[], index: number): string[] =>
    painted.filter(p =>
        p.includes('ecmeta_ssr_type="chart"')
        && p.includes(`ecmeta_data_index="${index}"`));

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('BaselineComparisonSection rendering', () => {
    it.each([
        ['light', createTheme()],
        ['dark', createTheme({ palette: { mode: 'dark' } })],
    ])('paints the band and both kinds of marker in the %s theme', (_, theme) => {
        const painted = paths(theme);

        // The warm band is a filled, shaded area.
        expect(painted.some(p =>
            p.includes(`fill="${theme.palette.info.main}"`)
            && p.includes('fill-opacity="0.25"')
            && /d="M[^"]*L/.test(p))).toBe(true);

        // The alerted point carries the solid engine marker only.
        const alerted = symbolsAt(painted, ALERTED_INDEX);
        expect(alerted).toHaveLength(1);
        expect(alerted[0]).toContain(`fill="${theme.palette.error.main}"`);

        // The unalerted outside point is hollow: filled with the
        // background and outlined in the warning colour.
        const outside = symbolsAt(painted, OUTSIDE_INDEX);
        expect(outside).toHaveLength(1);
        expect(outside[0]).toContain(
            `fill="${theme.palette.background.paper}"`,
        );
        expect(outside[0]).toContain(`stroke="${theme.palette.warning.main}"`);

        // Points inside the band carry no marker.
        expect(symbolsAt(painted, 0)).toHaveLength(0);
    });
});

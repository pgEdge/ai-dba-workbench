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
import { screen } from '@testing-library/react';
import * as echarts from 'echarts';

import Sparkline from '../Sparkline';
import { renderWithTheme } from '../../../test/renderWithTheme';
import type { SparklinePoint } from '../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

/*
 * These tests exercise what ECharts actually draws, rather than the shape
 * of the props `Sparkline` hands to `Chart`: issue #458 was a chart that
 * received entirely reasonable-looking options and still painted an empty
 * canvas, which no prop-shape assertion could catch.
 *
 * jsdom has no canvas, so the real `Chart` is rendered with only the
 * `echarts-for-react` wrapper replaced: the wrapper takes the fully merged
 * option object that `Chart` built and runs it through ECharts' own
 * server-side SVG renderer, exposing the drawing commands as text. Every
 * option-building step under test, from `buildLineOptions` through the
 * deep merge of the sparkline's overrides, is the production one.
 */
const CANVAS_WIDTH = 275;

vi.mock('echarts-for-react/esm/core', () => ({
    default: ({ option, style }: {
        option: Record<string, unknown>;
        style?: { height?: number | string };
    }) => {
        const height = typeof style?.height === 'number' ? style.height : 30;
        const chart = echarts.init(null, null, {
            renderer: 'svg',
            ssr: true,
            width: CANVAS_WIDTH,
            height,
        });
        chart.setOption({ ...option, animation: false });
        const svg = chart.renderToSVGString();
        chart.dispose();

        return (
            <div data-testid="sparkline-svg" data-height={height}>{svg}</div>
        );
    },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

interface DrawnMark {
    /** Horizontal extent of the mark, in canvas pixels. */
    width: number;
    /** Vertical extent of the mark, in canvas pixels. */
    height: number;
}

const PATH_RE = /<path[^>]*\bd="([^"]*)"[^>]*>/g;
const NUMBER_RE = /-?\d+(?:\.\d+)?/g;
const SYMBOL_TRANSFORM_RE = /transform="matrix\([^)]*\)"/;
const STROKE_ONLY_RE = /fill="none"/;
const DRAW_COMMAND_RE = /[LlCcAaHhVvQqSsTt]/;
const CLIP_PATH_SECTION_RE = /<defs[\s\S]*<\/defs>/;

/**
 * Extracts the marks ECharts actually painted from an SSR SVG string.
 *
 * Each `<path>` is split into its subpaths and measured, because the bug
 * under test produced geometry that looks substantial in the `d` attribute
 * and puts no ink on the canvas: a single-point area series emits a
 * zero-width `M x y L x y Z` subpath, and its line series emits a
 * move-only subpath. A filled subpath therefore counts only when it spans
 * more than a pixel in both directions, a stroked subpath only when it
 * spans more than a pixel in one direction and carries a drawing command
 * beyond the initial move, and a marker counts whenever it appears, since
 * ECharts emits one as unit geometry placed by a `transform` matrix. Clip
 * paths under `<defs>` are ignored: they mask the drawing rather than
 * contribute to it.
 */
const drawnMarks = (svg: string): DrawnMark[] => {
    const body = svg.replace(CLIP_PATH_SECTION_RE, '');
    const marks: DrawnMark[] = [];

    for (const match of body.matchAll(PATH_RE)) {
        const [element, commands] = match;
        const strokeOnly = STROKE_ONLY_RE.test(element);
        const isSymbol = SYMBOL_TRANSFORM_RE.test(element);

        for (const subpath of commands.split('M').filter(part => part.trim())) {
            const numbers = (subpath.match(NUMBER_RE) ?? []).map(Number);
            if (numbers.length < 2) {
                continue;
            }

            const xs = numbers.filter((_, index) => index % 2 === 0);
            const ys = numbers.filter((_, index) => index % 2 === 1);
            const width = Math.max(...xs) - Math.min(...xs);
            const height = Math.max(...ys) - Math.min(...ys);
            const hasDrawCommand = DRAW_COMMAND_RE.test(subpath);

            if (isSymbol) {
                marks.push({ width: 1, height: 1 });
            } else if (strokeOnly) {
                if (hasDrawCommand && (width > 1 || height > 1)) {
                    marks.push({ width, height });
                }
            } else if (width > 1 && height > 1) {
                marks.push({ width, height });
            }
        }
    }

    return marks;
};

const readSvg = (): string => screen.getByTestId('sparkline-svg').textContent
    ?? '';

const series = (values: (number | null)[]): SparklinePoint[] => values.map(
    (value, index) => ({
        time: new Date(Date.UTC(2026, 0, 1, 0, index * 5)).toISOString(),
        value,
    }),
);

const TWELVE_POINTS = series(
    Array.from({ length: 12 }, (_, index) => 97 + Math.sin(index) * 2),
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Sparkline rendering (issue #458)', () => {
    it('paints a single-point series as a visible marker', () => {
        renderWithTheme(<Sparkline data={series([97])} height={30} />);

        expect(drawnMarks(readSvg())).not.toHaveLength(0);
    });

    it('paints a series whose points are all separated by gaps', () => {
        renderWithTheme(
            <Sparkline data={series([null, 95, null, 98])} height={30} />,
        );

        expect(drawnMarks(readSvg())).not.toHaveLength(0);
    });

    it('paints a multi-point series as a line spanning the canvas', () => {
        renderWithTheme(<Sparkline data={TWELVE_POINTS} height={30} />);

        const widest = Math.max(...drawnMarks(readSvg()).map(m => m.width));

        // The contain-label layout pass used to reserve room for hidden
        // axis labels, leaving a plot area narrower than the canvas and
        // barely any of its height.
        expect(widest).toBeGreaterThan(CANVAS_WIDTH * 0.9);
    });

    it('fills most of the canvas height at the smallest sparkline size', () => {
        renderWithTheme(<Sparkline data={TWELVE_POINTS} height={30} />);

        const tallest = Math.max(...drawnMarks(readSvg()).map(m => m.height));

        expect(tallest).toBeGreaterThan(30 * 0.6);
    });

    it('renders no chart at all when every bucket is a null gap', () => {
        renderWithTheme(<Sparkline data={series([null, null])} />);

        expect(screen.queryByTestId('sparkline-svg')).toBeNull();
    });

    it('renders no chart at all for an empty series', () => {
        renderWithTheme(<Sparkline data={[]} />);

        expect(screen.queryByTestId('sparkline-svg')).toBeNull();
    });
});

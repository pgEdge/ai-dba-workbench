/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';

import {
    INDICATOR_SIZES,
    ICON_10_SX,
    ICON_14_SX,
    ICON_16_SX,
    CHART_AXIS_LABEL_FONTSIZE,
    MONO_CAPTION_SX,
    METRIC_LABEL_SX,
    METRIC_VALUE_BASE_SX,
    SERVER_INFO_LABEL_BASE_SX,
    SERVER_INFO_VALUE_BASE_SX,
    ALERT_TITLE_BASE_SX,
    ALERT_THRESHOLD_SX,
    ALERT_DESCRIPTION_SX,
    ALERT_ACK_TEXT_SX,
    ALERT_TIME_SX,
    ALERT_LAST_UPDATED_SX,
    SEVERITY_CHIP_BASE_SX,
    ALERT_TYPE_CHIP_BASE_SX,
} from '../tokens';
import * as themeBarrel from '../index';

describe('design tokens', () => {
    describe('INDICATOR_SIZES', () => {
        it('exposes numeric pixel sizes for small/medium/large', () => {
            expect(INDICATOR_SIZES).toEqual({
                small: 14,
                medium: 18,
                large: 22,
            });
            for (const value of Object.values(INDICATOR_SIZES)) {
                expect(typeof value).toBe('number');
                expect(value).toBeGreaterThan(0);
            }
        });
    });

    describe('icon-size sx tokens', () => {
        it('ICON_10_SX renders a 10px font size', () => {
            expect(ICON_10_SX).toEqual({ fontSize: 10 });
        });

        it('ICON_14_SX renders a 14px font size', () => {
            expect(ICON_14_SX).toEqual({ fontSize: 14 });
        });

        it('ICON_16_SX renders a 16px font size', () => {
            expect(ICON_16_SX).toEqual({ fontSize: 16 });
        });
    });

    describe('chart and mono tokens', () => {
        it('CHART_AXIS_LABEL_FONTSIZE is the 14px numeric chart label size', () => {
            expect(CHART_AXIS_LABEL_FONTSIZE).toBe(14);
            expect(typeof CHART_AXIS_LABEL_FONTSIZE).toBe('number');
        });

        it('MONO_CAPTION_SX combines the 14px size with JetBrains Mono', () => {
            expect(MONO_CAPTION_SX.fontSize).toBe('0.875rem');
            expect(MONO_CAPTION_SX.fontFamily).toContain('JetBrains Mono');
        });
    });

    describe('metric label/value typography', () => {
        it('METRIC_LABEL_SX is an uppercase caption-style label', () => {
            expect(METRIC_LABEL_SX.color).toBe('text.secondary');
            expect(METRIC_LABEL_SX.fontSize).toBe('0.875rem');
            expect(METRIC_LABEL_SX.fontWeight).toBe(500);
            expect(METRIC_LABEL_SX.textTransform).toBe('uppercase');
            expect(METRIC_LABEL_SX.letterSpacing).toBe('0.05em');
        });

        it('METRIC_VALUE_BASE_SX is a monospace large-number style', () => {
            expect(METRIC_VALUE_BASE_SX.fontWeight).toBe(700);
            expect(METRIC_VALUE_BASE_SX.fontSize).toBe('1.75rem');
            expect(METRIC_VALUE_BASE_SX.lineHeight).toBe(1);
            expect(METRIC_VALUE_BASE_SX.fontFamily).toContain('JetBrains Mono');
        });
    });

    describe('server-info label/value typography', () => {
        it('SERVER_INFO_LABEL_BASE_SX is a tightly-spaced uppercase label', () => {
            expect(SERVER_INFO_LABEL_BASE_SX.fontSize).toBe('0.875rem');
            expect(SERVER_INFO_LABEL_BASE_SX.fontWeight).toBe(700);
            expect(SERVER_INFO_LABEL_BASE_SX.textTransform).toBe('uppercase');
            expect(SERVER_INFO_LABEL_BASE_SX.letterSpacing).toBe('0.1em');
            expect(SERVER_INFO_LABEL_BASE_SX.lineHeight).toBe(1);
        });

        it('SERVER_INFO_VALUE_BASE_SX is the readable paired value style', () => {
            expect(SERVER_INFO_VALUE_BASE_SX.color).toBe('text.primary');
            expect(SERVER_INFO_VALUE_BASE_SX.fontSize).toBe('0.9375rem');
            expect(SERVER_INFO_VALUE_BASE_SX.fontWeight).toBe(500);
            expect(SERVER_INFO_VALUE_BASE_SX.whiteSpace).toBe('nowrap');
        });
    });

    describe('alert typography variants', () => {
        it('ALERT_TITLE_BASE_SX is a bold 1rem title', () => {
            expect(ALERT_TITLE_BASE_SX).toEqual({
                fontWeight: 600,
                fontSize: '1rem',
                lineHeight: 1.2,
            });
        });

        it('ALERT_THRESHOLD_SX uses monospace for numeric thresholds', () => {
            expect(ALERT_THRESHOLD_SX.fontFamily).toContain('JetBrains Mono');
            expect(ALERT_THRESHOLD_SX.fontSize).toBe('0.875rem');
            expect(ALERT_THRESHOLD_SX.color).toBe('text.secondary');
        });

        it('ALERT_THRESHOLD_SX adds a top margin to separate from the title', () => {
            expect(ALERT_THRESHOLD_SX.mt).toBe(0.25);
        });

        it('ALERT_DESCRIPTION_SX wraps long words and uses secondary text', () => {
            expect(ALERT_DESCRIPTION_SX.color).toBe('text.secondary');
            expect(ALERT_DESCRIPTION_SX.fontSize).toBe('0.875rem');
            expect(ALERT_DESCRIPTION_SX.wordBreak).toBe('break-word');
        });

        it('ALERT_ACK_TEXT_SX is italic secondary text', () => {
            expect(ALERT_ACK_TEXT_SX.color).toBe('text.secondary');
            expect(ALERT_ACK_TEXT_SX.fontStyle).toBe('italic');
        });

        it('ALERT_TIME_SX is a flex container for the time-ago caption', () => {
            expect(ALERT_TIME_SX.color).toBe('text.disabled');
            expect(ALERT_TIME_SX.display).toBe('flex');
            expect(ALERT_TIME_SX.alignItems).toBe('center');
        });

        it('ALERT_LAST_UPDATED_SX is a secondary-color caption', () => {
            expect(ALERT_LAST_UPDATED_SX).toEqual({
                color: 'text.secondary',
                fontSize: '0.875rem',
            });
        });

        it('SEVERITY_CHIP_BASE_SX is a compact uppercase chip', () => {
            expect(SEVERITY_CHIP_BASE_SX.height).toBe(16);
            expect(SEVERITY_CHIP_BASE_SX.textTransform).toBe('uppercase');
            expect(SEVERITY_CHIP_BASE_SX.fontWeight).toBe(600);
        });

        it('ALERT_TYPE_CHIP_BASE_SX is a compact capitalize chip', () => {
            expect(ALERT_TYPE_CHIP_BASE_SX.height).toBe(16);
            expect(ALERT_TYPE_CHIP_BASE_SX.textTransform).toBe('capitalize');
            expect(ALERT_TYPE_CHIP_BASE_SX.fontWeight).toBe(600);
        });
    });

    describe('theme barrel', () => {
        it('re-exports the design tokens', () => {
            expect(themeBarrel.INDICATOR_SIZES).toBe(INDICATOR_SIZES);
            expect(themeBarrel.ICON_16_SX).toBe(ICON_16_SX);
            expect(themeBarrel.METRIC_LABEL_SX).toBe(METRIC_LABEL_SX);
            expect(themeBarrel.ALERT_TITLE_BASE_SX).toBe(ALERT_TITLE_BASE_SX);
        });

        it('re-exports the pgedge theme factory', () => {
            expect(typeof themeBarrel.createPgedgeTheme).toBe('function');
            expect(themeBarrel.loginTheme).toBeDefined();
        });
    });
});

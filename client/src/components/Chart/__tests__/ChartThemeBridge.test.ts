/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { renderHook } from '@testing-library/react';
import { ThemeProvider, createTheme, PaletteOptions } from '@mui/material/styles';
import { describe, it, expect } from 'vitest';
import { useEChartsTheme, getDefaultColorPalette } from '../ChartThemeBridge';

const lightTheme = createTheme({
    palette: {
        mode: 'light',
        custom: {
            status: {
                purple: '#8B5CF6',
                cyan: '#06B6D4',
                sky: '#0EA5E9',
            },
        },
    } as PaletteOptions,
});

const darkTheme = createTheme({
    palette: {
        mode: 'dark',
        custom: {
            status: {
                purple: '#A78BFA',
                cyan: '#22D3EE',
                sky: '#38BDF8',
            },
        },
    } as PaletteOptions,
});

function createWrapper(theme: ReturnType<typeof createTheme>) {
    return function Wrapper({ children }: { children: React.ReactNode }) {
        return React.createElement(ThemeProvider, { theme }, children);
    };
}

describe('getDefaultColorPalette', () => {
    it('returns an array of 9 colors', () => {
        const palette = getDefaultColorPalette(lightTheme);
        expect(palette).toHaveLength(9);
    });

    it('starts with pgEdge brand cyan/teal and all colors are valid hex', () => {
        const palette = getDefaultColorPalette(lightTheme);
        const hexRegex = /^#[0-9A-Fa-f]{6}$/;

        // First color should be in the cyan/teal family
        expect(palette[0]).toBe('#0C8599');

        // Every color must be a valid 6-digit hex string
        for (const color of palette) {
            expect(color).toMatch(hexRegex);
        }
    });

    it('contains 9 distinct colors with significant differences', () => {
        const palette = getDefaultColorPalette(lightTheme);

        // All colors must be unique (no duplicates)
        const unique = new Set(palette);
        expect(unique.size).toBe(palette.length);

        // Each pair of colors must differ significantly
        // (at least 30 in total RGB distance)
        function hexToRgb(hex: string): [number, number, number] {
            const n = parseInt(hex.slice(1), 16);
            return [(n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff];
        }
        for (let i = 0; i < palette.length; i++) {
            for (let j = i + 1; j < palette.length; j++) {
                const [r1, g1, b1] = hexToRgb(palette[i]);
                const [r2, g2, b2] = hexToRgb(palette[j]);
                const dist =
                    Math.abs(r1 - r2) +
                    Math.abs(g1 - g2) +
                    Math.abs(b1 - b2);
                expect(dist).toBeGreaterThan(30);
            }
        }
    });
});

describe('useEChartsTheme', () => {
    it('returns an object with backgroundColor set to transparent', () => {
        const { result } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(lightTheme),
        });
        expect(result.current.backgroundColor).toBe('transparent');
    });

    it('returns a color array matching the default palette', () => {
        const { result } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(lightTheme),
        });
        const expectedPalette = getDefaultColorPalette(lightTheme);
        expect(result.current.color).toEqual(expectedPalette);
    });

    it('includes textStyle with color and fontFamily', () => {
        const { result } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(lightTheme),
        });
        expect(result.current.textStyle).toBeDefined();
        expect(result.current.textStyle.color).toBe(
            lightTheme.palette.text.primary
        );
        expect(result.current.textStyle.fontFamily).toBeDefined();
    });

    it('includes title, legend, tooltip, xAxis, yAxis, and dataZoom keys', () => {
        const { result } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(lightTheme),
        });
        expect(result.current).toHaveProperty('title');
        expect(result.current).toHaveProperty('legend');
        expect(result.current).toHaveProperty('tooltip');
        expect(result.current).toHaveProperty('xAxis');
        expect(result.current).toHaveProperty('yAxis');
        expect(result.current).toHaveProperty('dataZoom');
    });

    it('produces different textStyle colors for light vs dark mode', () => {
        const { result: lightResult } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(lightTheme),
        });
        const { result: darkResult } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(darkTheme),
        });
        expect(lightResult.current.textStyle.color).not.toBe(
            darkResult.current.textStyle.color
        );
    });

    it('produces different palette colors for light vs dark mode', () => {
        const lightPalette = getDefaultColorPalette(lightTheme);
        const darkPalette = getDefaultColorPalette(darkTheme);
        expect(lightPalette).not.toEqual(darkPalette);
    });

    it('produces different tooltip background colors for light vs dark mode', () => {
        const { result: lightResult } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(lightTheme),
        });
        const { result: darkResult } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(darkTheme),
        });
        expect(lightResult.current.tooltip.backgroundColor).not.toBe(
            darkResult.current.tooltip.backgroundColor
        );
    });

    it('dataZoom is an array with at least one entry', () => {
        const { result } = renderHook(() => useEChartsTheme(), {
            wrapper: createWrapper(lightTheme),
        });
        expect(Array.isArray(result.current.dataZoom)).toBe(true);
        expect(result.current.dataZoom.length).toBeGreaterThanOrEqual(1);
    });
});

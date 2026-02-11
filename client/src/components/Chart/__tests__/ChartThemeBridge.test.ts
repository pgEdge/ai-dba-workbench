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

    it('includes theme primary and secondary colors', () => {
        const palette = getDefaultColorPalette(lightTheme);
        expect(palette[0]).toBe(lightTheme.palette.primary.main);
        expect(palette[1]).toBe(lightTheme.palette.secondary.main);
    });

    it('includes custom status colors', () => {
        const palette = getDefaultColorPalette(lightTheme);
        expect(palette).toContain('#8B5CF6');
        expect(palette).toContain('#06B6D4');
        expect(palette).toContain('#0EA5E9');
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

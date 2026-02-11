/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

const MARKER_SYMBOL_MAP: Record<string, string> = {
    circle: 'circle',
    square: 'rect',
    triangle: 'triangle',
    diamond: 'diamond',
};

export const getMarkerSymbol = (
    shape: 'circle' | 'square' | 'triangle' | 'diamond'
): string => {
    return MARKER_SYMBOL_MAP[shape] || 'circle';
};

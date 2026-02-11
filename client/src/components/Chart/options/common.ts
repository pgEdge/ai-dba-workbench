/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

export function buildTooltip(show: boolean): object {
    return {
        show,
        trigger: 'axis',
    };
}

export function buildLegend(show: boolean): object {
    return {
        show,
        bottom: 0,
    };
}

export function buildGrid(): object {
    return {
        left: '3%',
        right: '4%',
        bottom: '15%',
        top: '10%',
        containLabel: true,
    };
}

export function buildXAxis(categories?: string[]): object {
    return {
        type: 'category',
        data: categories ?? [],
        boundaryGap: true,
    };
}

export function buildYAxis(): object {
    return {
        type: 'value',
    };
}

export function buildDataZoom(enabled: boolean): object[] {
    return [
        {
            type: 'slider',
            show: enabled,
            start: 0,
            end: 100,
        },
    ];
}

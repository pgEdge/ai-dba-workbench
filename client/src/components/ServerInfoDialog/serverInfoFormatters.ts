/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Format a byte value into a human-readable string.
 */
export function formatBytes(bytes: number | null | undefined): string {
    if (bytes == null || bytes === 0) {
        return '\u2014';
    }
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    let val = bytes;
    let idx = 0;
    while (val >= 1024 && idx < units.length - 1) {
        val /= 1024;
        idx++;
    }
    return `${val.toFixed(idx === 0 ? 0 : 1)} ${units[idx]}`;
}

/**
 * Format a clock speed in Hz into a human-readable string.
 */
export function formatClockSpeed(hz: number | null | undefined): string {
    if (hz == null) {
        return '\u2014';
    }
    if (hz >= 1_000_000_000) {
        return `${(hz / 1_000_000_000).toFixed(2)} GHz`;
    }
    if (hz >= 1_000_000) {
        return `${(hz / 1_000_000).toFixed(0)} MHz`;
    }
    return `${hz} Hz`;
}

/**
 * Calculate the percentage of used out of total.
 */
export function pct(used: number | null, total: number | null): number | null {
    if (used == null || total == null || total <= 0) {
        return null;
    }
    const raw = Math.round((used / total) * 100);
    return Math.min(100, Math.max(0, raw));
}

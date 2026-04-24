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
 * Format byte values into a human-readable string.
 *
 * @param bytes - The number of bytes, or null/undefined.
 * @returns A formatted string such as "1.2 GB" or "--" for null/undefined.
 */
export function formatBytes(bytes: number | null | undefined): string {
    if (bytes === null || bytes === undefined) {
        return '--';
    }

    if (bytes === 0) {
        return '0 B';
    }

    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    const k = 1024;
    const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k));
    const unitIndex = Math.min(i, units.length - 1);

    if (unitIndex === 0) {
        return `${bytes} B`;
    }

    const value = bytes / Math.pow(k, unitIndex);
    return `${value.toFixed(1)} ${units[unitIndex]}`;
}

/**
 * Format a duration in milliseconds into a human-readable string.
 *
 * @param ms - The duration in milliseconds, or null/undefined.
 * @returns A formatted string such as "350 us", "45.2 ms", "5.2 min",
 *          or "--" for null/undefined.
 */
export function formatTime(ms: number | null | undefined): string {
    if (ms === null || ms === undefined) {
        return '--';
    }
    if (ms < 1) {
        return `${Math.round(ms * 1000)} us`;
    }
    if (ms < 1000) {
        return `${ms.toFixed(1)} ms`;
    }
    if (ms < 60000) {
        return `${(ms / 1000).toFixed(2)} s`;
    }
    if (ms < 3600000) {
        return `${(ms / 60000).toFixed(1)} min`;
    }
    if (ms < 86400000) {
        return `${(ms / 3600000).toFixed(1)} hr`;
    }
    return `${(ms / 86400000).toFixed(1)} d`;
}

/**
 * Format large numbers with locale-appropriate separators.
 *
 * @param num - The number to format, or null/undefined.
 * @returns A formatted string with comma separators, or "--" for
 *          null/undefined.
 */
export function formatNumber(num: number | null | undefined): string {
    if (num === null || num === undefined) {
        return '--';
    }
    return num.toLocaleString();
}

/**
 * Format large numbers with abbreviations for space-constrained contexts
 * such as KPI tiles.
 *
 * @param num - The number to format, or null/undefined.
 * @returns A compact string such as "1.2B", "3.4M", "12.3K", "9,999",
 *          or "--" for null/undefined.
 */
export function formatCompactNumber(num: number | null | undefined): string {
    if (num === null || num === undefined) {
        return '--';
    }
    if (num >= 1_000_000_000) {
        return `${(num / 1_000_000_000).toFixed(1)}B`;
    }
    if (num >= 1_000_000) {
        return `${(num / 1_000_000).toFixed(1)}M`;
    }
    if (num >= 10_000) {
        return `${(num / 1_000).toFixed(1)}K`;
    }
    return num.toLocaleString();
}

/**
 * Format a numeric value with fixed decimal places and locale separators.
 *
 * @param value - The number to format, or null/undefined.
 * @param decimals - The number of decimal places (default: 1).
 * @returns A formatted string or "--" for null/undefined.
 */
export function formatValue(
    value: number | null | undefined,
    decimals = 1
): string {
    if (value === null || value === undefined) {
        return '--';
    }
    return Number(value.toFixed(decimals)).toLocaleString(undefined, {
        minimumFractionDigits: decimals,
        maximumFractionDigits: decimals,
    });
}

/**
 * Format a replication lag value from seconds into a human-readable string.
 *
 * @param lagSeconds - The lag in seconds, or null.
 * @returns A formatted string such as "250 ms", "12.3 s", or "--" for null.
 */
export function formatLag(lagSeconds: number | null | undefined): string {
    if (lagSeconds === null || lagSeconds === undefined) {
        return '--';
    }

    if (lagSeconds < 1) {
        return `${(lagSeconds * 1000).toFixed(0)} ms`;
    }
    if (lagSeconds < 60) {
        return `${lagSeconds.toFixed(1)} s`;
    }
    if (lagSeconds < 3600) {
        return `${(lagSeconds / 60).toFixed(1)} min`;
    }
    if (lagSeconds < 86400) {
        return `${(lagSeconds / 3600).toFixed(1)} hr`;
    }
    return `${(lagSeconds / 86400).toFixed(1)} d`;
}

/**
 * Convert a raw PostgreSQL pg_settings value and unit into a
 * human-readable display string.
 *
 * @param setting - The raw setting value as a string, or null.
 * @param unit - The pg_settings unit (e.g., "8kB", "kB", "MB", "ms", "s",
 *               "min"), or null for unitless settings.
 * @returns A formatted string such as "4.0 GB" or "5.2 min".
 */
export function formatPgSetting(
    setting: string | null,
    unit?: string | null
): string {
    if (setting === null || setting === '') {
        return '\u2014';
    }

    const numValue = parseFloat(setting);

    if (unit === '8kB' && !Number.isNaN(numValue)) {
        return formatBytes(numValue * 8192);
    }
    if (unit === 'kB' && !Number.isNaN(numValue)) {
        return formatBytes(numValue * 1024);
    }
    if (unit === 'MB' && !Number.isNaN(numValue)) {
        return formatBytes(numValue * 1048576);
    }
    if (unit === 'ms' && !Number.isNaN(numValue)) {
        return formatTime(numValue);
    }
    if (unit === 's' && !Number.isNaN(numValue)) {
        return formatTime(numValue * 1000);
    }
    if (unit === 'min' && !Number.isNaN(numValue)) {
        return formatTime(numValue * 60000);
    }

    if (unit) {
        return `${setting} ${unit}`;
    }

    return setting;
}

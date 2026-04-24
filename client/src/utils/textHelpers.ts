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
 * Strip any conversational preamble before the first markdown heading.
 * LLMs sometimes add introductory text despite instructions not to.
 */
export function stripPreamble(text: string): string {
    const headingIndex = text.search(/^##\s/m);
    if (headingIndex > 0) {
        return text.substring(headingIndex);
    }
    return text;
}

/**
 * Truncate a description string to the first line, capped at a maximum
 * length. Returns an empty string for falsy input.
 */
export const truncateDescription = (desc: string, maxLength = 60): string => {
    if (!desc) {return '';}
    const firstLine = desc.split('\n')[0];
    if (firstLine.length <= maxLength) {return firstLine;}
    return `${firstLine.substring(0, maxLength)}...`;
};

/**
 * Compute a djb2 hash of the given string and return it as a string.
 */
export function djb2Hash(str: string): string {
    let hash = 5381;
    for (let i = 0; i < str.length; i++) {
        hash = ((hash << 5) + hash + str.charCodeAt(i)) | 0;
    }
    return String(hash >>> 0);
}

/**
 * Slugify a string for use in filenames.
 */
export const slugify = (text: string): string =>
    text
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');

/** Analysis cache time-to-live: 30 minutes. */
export const ANALYSIS_CACHE_TTL_MS = 30 * 60 * 1000;

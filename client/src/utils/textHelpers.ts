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
    return firstLine.substring(0, maxLength) + '...';
};

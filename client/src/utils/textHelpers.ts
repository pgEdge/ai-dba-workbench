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
 * Truncate a description string to the first line, capped at a maximum
 * length. Returns an empty string for falsy input.
 */
export const truncateDescription = (desc: string, maxLength = 60): string => {
    if (!desc) {return '';}
    const firstLine = desc.split('\n')[0];
    if (firstLine.length <= maxLength) {return firstLine;}
    return firstLine.substring(0, maxLength) + '...';
};

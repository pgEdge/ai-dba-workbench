/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Markdown utility functions
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Create a modified Prism theme that removes token background colors.
 * This prevents subtle background color conflicts with our custom backgrounds.
 */
export const createCleanTheme = (
    baseTheme: Record<string, unknown>,
    customBackground: string,
): Record<string, unknown> => {
    const cleanTheme: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(baseTheme)) {
        if (typeof value === 'object' && value !== null) {
            // Remove background from token styles, keep other properties
            const {
                background: _background,
                backgroundColor: _backgroundColor,
                ...rest
            } = value as Record<string, unknown>;
            cleanTheme[key] = rest;
        } else {
            cleanTheme[key] = value;
        }
    }
    // Set the base code block background
    const preKey = 'pre[class*="language-"]';
    const codeKey = 'code[class*="language-"]';
    if (cleanTheme[preKey] && typeof cleanTheme[preKey] === 'object') {
        (cleanTheme[preKey] as Record<string, unknown>).background =
            customBackground;
    }
    if (cleanTheme[codeKey] && typeof cleanTheme[codeKey] === 'object') {
        (cleanTheme[codeKey] as Record<string, unknown>).background =
            'transparent';
    }
    return cleanTheme;
};

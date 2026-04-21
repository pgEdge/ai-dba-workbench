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
 * Copy text to the clipboard.
 *
 * Tries the modern Clipboard API first (requires a secure context). When
 * that is unavailable (plain HTTP), falls back to the legacy
 * `document.execCommand('copy')` approach with a temporary textarea.
 *
 * @param text - The string to place on the clipboard.
 * @returns A promise that resolves on success.
 * @throws On failure so callers can report the error to the user.
 */
export async function copyToClipboard(text: string): Promise<void> {
    // Prefer the modern Clipboard API when available.
    if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        return;
    }

    // Fallback: create a temporary textarea, select its content, and
    // invoke the deprecated execCommand('copy').
    const textarea = document.createElement('textarea');
    textarea.value = text;

    // Keep the element invisible and out of the layout flow.
    textarea.style.position = 'fixed';
    textarea.style.left = '-9999px';
    textarea.style.top = '-9999px';
    textarea.style.opacity = '0';

    document.body.appendChild(textarea);
    try {
        textarea.select();
        const ok = document.execCommand('copy');
        if (!ok) {
            throw new Error(
                'Clipboard API unavailable and execCommand("copy") failed.'
            );
        }
    } finally {
        document.body.removeChild(textarea);
    }
}

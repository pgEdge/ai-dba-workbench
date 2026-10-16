/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Colour-contrast helpers for accessibility assertions in tests.
 *
 * These implement the WCAG 2.1 relative luminance and contrast ratio
 * definitions so that tests can assert a real measured ratio rather
 * than pinning a hex value whose contrast nobody has checked.
 *
 *-------------------------------------------------------------------------
 */

/** The WCAG AA minimum contrast ratio for normal-sized body text. */
export const WCAG_AA_NORMAL_TEXT = 4.5;

type Rgb = [number, number, number];

/**
 * Parse a `#RRGGBB` or `#RGB` colour into its red, green and blue
 * channels, each in the range 0-255.
 */
export function hexToRgb(hex: string): Rgb {
    const value = hex.replace('#', '');
    const full = value.length === 3
        ? value.split('').map((c) => c + c).join('')
        : value;
    if (!/^[0-9a-fA-F]{6}$/.test(full)) {
        throw new Error(`Not a hex colour: ${hex}`);
    }
    return [
        parseInt(full.slice(0, 2), 16),
        parseInt(full.slice(2, 4), 16),
        parseInt(full.slice(4, 6), 16),
    ];
}

/**
 * Composite a translucent foreground colour over an opaque background,
 * returning the resulting opaque colour. This is what the browser
 * paints for MUI's `alpha(colour, opacity)` backgrounds, and it is the
 * colour that text sitting on such a background is really read against.
 */
export function compositeOver(
    foreground: string,
    opacity: number,
    background: string,
): Rgb {
    const fg = hexToRgb(foreground);
    const bg = hexToRgb(background);
    return [0, 1, 2].map(
        (i) => fg[i] * opacity + bg[i] * (1 - opacity),
    ) as Rgb;
}

/** WCAG 2.1 relative luminance of an sRGB colour. */
function relativeLuminance(rgb: Rgb): number {
    const [r, g, b] = rgb.map((channel) => {
        const s = channel / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/**
 * The WCAG 2.1 contrast ratio between two opaque colours, from 1 (no
 * contrast) to 21 (black on white). Each colour may be given as a hex
 * string or as already-composited RGB channels.
 */
export function contrastRatio(a: string | Rgb, b: string | Rgb): number {
    const first = relativeLuminance(typeof a === 'string' ? hexToRgb(a) : a);
    const second = relativeLuminance(typeof b === 'string' ? hexToRgb(b) : b);
    const lighter = Math.max(first, second);
    const darker = Math.min(first, second);
    return (lighter + 0.05) / (darker + 0.05);
}

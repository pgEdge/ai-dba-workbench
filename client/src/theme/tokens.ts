/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Shared design tokens used across the client.
 *
 * These constants describe app-wide concepts (icon sizes, metric label
 * typography, server-info label/value typography, alert text variants,
 * etc.) and are intended to be consumed from anywhere in the codebase.
 * Component-specific layout (grid templates, paddings, container
 * styling) does NOT belong here.
 *
 * The intent is to give the codebase a single, canonical place to
 * import these tokens from. Prefer adding new shared visual tokens to
 * this file (or a sibling under `client/src/theme/`) rather than
 * defining them inside a component's local `styles.ts`.
 *
 *-------------------------------------------------------------------------
 */

// ---- Icon sizes -----------------------------------------------------------

/**
 * Header status indicator icon sizes (in pixels), keyed by the size
 * prop accepted by the indicator component. Kept as a numeric map so
 * callers can derive `fontSize` directly.
 */
export const INDICATOR_SIZES = {
    small: 14,
    medium: 18,
    large: 22,
};

/** Inline icon-size sx tokens, used for `<Icon sx={ICON_NN_SX} />`. */
export const ICON_10_SX = { fontSize: 10 };
export const ICON_14_SX = { fontSize: 14 };
export const ICON_16_SX = { fontSize: 16 };

/**
 * Numeric font-size used for chart axis labels (ECharts/Recharts). The
 * chart libraries take a numeric `fontSize` rather than an `sx` block,
 * so this is exposed as a plain number.
 */
export const CHART_AXIS_LABEL_FONTSIZE = 14;

/**
 * Monospace caption typography (14px JetBrains Mono). Used for short
 * technical values that sit next to ordinary text, such as alert
 * threshold lines and Spock version/node labels. Callers compose this
 * with a `color` from the palette.
 */
export const MONO_CAPTION_SX = {
    fontSize: '0.875rem',
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

// ---- Metric label/value typography ----------------------------------------

/**
 * Uppercase caption-style label used above metric values (e.g. the
 * label that sits above a numeric KPI).
 */
export const METRIC_LABEL_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontWeight: 500,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
};

/**
 * Base typography for a large numeric metric value. Callers compose
 * this with a color from the theme palette.
 */
export const METRIC_VALUE_BASE_SX = {
    fontWeight: 700,
    fontSize: '1.75rem',
    lineHeight: 1,
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
};

// ---- Server-info label/value typography -----------------------------------

/**
 * Uppercase, tightly-spaced label used for short identifying labels
 * in server-info style displays (key/value pair lists).
 */
export const SERVER_INFO_LABEL_BASE_SX = {
    fontSize: '0.875rem',
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.1em',
    lineHeight: 1,
};

/** Value typography paired with `SERVER_INFO_LABEL_BASE_SX`. */
export const SERVER_INFO_VALUE_BASE_SX = {
    color: 'text.primary',
    fontSize: '0.9375rem',
    fontWeight: 500,
    lineHeight: 1.2,
    whiteSpace: 'nowrap',
};

// ---- Alert typography variants --------------------------------------------

/** Title text shown at the top of an alert row. */
export const ALERT_TITLE_BASE_SX = {
    fontWeight: 600,
    fontSize: '1rem',
    lineHeight: 1.2,
};

/** Monospace threshold/metric line below the alert title. */
export const ALERT_THRESHOLD_SX = {
    ...MONO_CAPTION_SX,
    color: 'text.secondary',
    mt: 0.25,
};

/** Free-form description text below the alert title. */
export const ALERT_DESCRIPTION_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    mt: 0.25,
    wordBreak: 'break-word',
};

/** Acknowledgement attribution line (e.g. "Acknowledged by ..."). */
export const ALERT_ACK_TEXT_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
    fontStyle: 'italic',
};

/** Time-ago caption shown alongside an alert. */
export const ALERT_TIME_SX = {
    color: 'text.disabled',
    fontSize: '0.875rem',
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
};

/**
 * Caption used when an alert's `last_updated` timestamp differs from
 * its `triggered_at` (for example after reactivation).
 */
export const ALERT_LAST_UPDATED_SX = {
    color: 'text.secondary',
    fontSize: '0.875rem',
};

/** Base typography for the severity chip (Critical/Warning/Info). */
export const SEVERITY_CHIP_BASE_SX = {
    height: 16,
    fontSize: '0.875rem',
    fontWeight: 600,
    textTransform: 'uppercase',
};

/** Base typography for the alert-type chip (Anomaly/Threshold). */
export const ALERT_TYPE_CHIP_BASE_SX = {
    height: 16,
    fontSize: '0.875rem',
    fontWeight: 600,
    textTransform: 'capitalize',
};

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Helpers for system alerts: alerts the alerter raises about the
 * Workbench itself (such as a failing anomaly detection provider)
 * rather than about a monitored server. A system alert has the alert
 * type 'system', a null connection_id and no server name.
 *
 *-------------------------------------------------------------------------
 */

/** The alert_type the server reports for a system alert. */
export const SYSTEM_ALERT_TYPE = 'system';

/**
 * Label shown in place of a server name for a system alert, so the
 * alert is never presented as belonging to a monitored server.
 */
export const SYSTEM_ALERT_SOURCE_LABEL = 'AI DBA Workbench';

/** Tooltip text explaining where a system alert comes from. */
export const SYSTEM_ALERT_SOURCE_DESCRIPTION =
    'Raised by the AI DBA Workbench alerter about the Workbench itself, '
    + 'not about a monitored server';

/**
 * Report whether a raw API alert record is a system alert. The alert
 * type is the primary signal; a null connection_id is treated the same
 * way because no other kind of alert can lack a connection, and code
 * that assumed a connection would otherwise run against a missing one.
 * An absent connection_id (undefined) is not enough on its own, since
 * partial records built in the client may simply omit it.
 */
export const isSystemAlertRecord = (
    alertType: string | null | undefined,
    connectionId: number | null | undefined,
): boolean => alertType === SYSTEM_ALERT_TYPE || connectionId === null;

/**
 * Report whether a transformed alert is a system alert, from either
 * the precomputed `isSystem` flag or the alert type.
 */
export const isSystemAlert = (
    alert: { alertType?: string | null; isSystem?: boolean } | null | undefined,
): boolean => !!alert && (alert.isSystem === true
    || alert.alertType === SYSTEM_ALERT_TYPE);

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect } from 'vitest';
import {
    FRIENDLY_ALERT_TITLES,
    FRIENDLY_PROBE_NAMES,
    getFriendlyProbeName,
    getFriendlyTitle,
} from '../friendlyNames';

describe('getFriendlyProbeName', () => {
    it('returns the mapped name for a known probe', () => {
        expect(getFriendlyProbeName('pg_stat_activity')).toBe(
            'Database Activity',
        );
    });

    it('returns the mapped name for a system probe', () => {
        expect(getFriendlyProbeName('pg_sys_cpu_usage_info')).toBe(
            'CPU Usage',
        );
    });

    it('falls back to title-cased text for an unknown probe', () => {
        expect(getFriendlyProbeName('custom_probe_name')).toBe(
            'Custom Probe Name',
        );
    });

    it('returns the raw value when given an empty string', () => {
        expect(getFriendlyProbeName('')).toBe('');
    });
});

describe('getFriendlyTitle', () => {
    it('returns "Alert" for an empty string', () => {
        expect(getFriendlyTitle('')).toBe('Alert');
    });

    it('preserves connection error prefixes verbatim', () => {
        expect(getFriendlyTitle('Connection error: db1.example.com')).toBe(
            'Connection Error: db1.example.com',
        );
    });

    it('preserves a lowercase connection error prefix', () => {
        expect(getFriendlyTitle('connection error: host')).toBe(
            'Connection Error: host',
        );
    });

    it('returns an exact mapped rule name', () => {
        expect(getFriendlyTitle('high_max_connections')).toBe(
            'High Max Connections',
        );
    });

    it('matches case-insensitively', () => {
        expect(getFriendlyTitle('HIGH_MAX_CONNECTIONS')).toBe(
            'High Max Connections',
        );
    });

    it('resolves partial matches that contain a known key', () => {
        // "alert: cpu_usage_high - host1" contains "cpu_usage_high"
        expect(getFriendlyTitle('alert: cpu_usage_high - host1')).toBe(
            'High CPU Usage',
        );
    });

    it('returns dotted metric names verbatim when not mapped', () => {
        expect(getFriendlyTitle('pg_custom_probe.some_metric')).toBe(
            'pg_custom_probe.some_metric',
        );
    });

    it('title-cases unmapped underscore-separated names', () => {
        expect(getFriendlyTitle('some_unknown_alert')).toBe(
            'Some Unknown Alert',
        );
    });

    // -----------------------------------------------------------------
    // Anomaly metric-name entries (Status Panel display)
    // -----------------------------------------------------------------

    it('maps pg_replication_slots.max_retained_bytes', () => {
        expect(getFriendlyTitle('pg_replication_slots.max_retained_bytes'))
            .toBe('Replication Slot WAL Retention');
    });

    it('maps pg_replication_slots.retained_bytes', () => {
        expect(getFriendlyTitle('pg_replication_slots.retained_bytes')).toBe(
            'Replication Slot WAL Retention',
        );
    });

    it('maps pg_replication_slots.inactive', () => {
        expect(getFriendlyTitle('pg_replication_slots.inactive')).toBe(
            'Replication Slot Inactive',
        );
    });

    it('maps pg_replication_slots.inactive_count', () => {
        expect(getFriendlyTitle('pg_replication_slots.inactive_count')).toBe(
            'Inactive Replication Slots',
        );
    });

    it('maps pg_settings.max_connections', () => {
        expect(getFriendlyTitle('pg_settings.max_connections')).toBe(
            'Max Connections',
        );
    });

    it('maps pg_stat_replication.lag_bytes', () => {
        expect(getFriendlyTitle('pg_stat_replication.lag_bytes')).toBe(
            'Replication Lag',
        );
    });

    it('maps pg_stat_replication.standby_disconnected', () => {
        expect(
            getFriendlyTitle('pg_stat_replication.standby_disconnected'),
        ).toBe('Standby Disconnected');
    });

    it('maps pg_stat_activity.blocked_count', () => {
        expect(getFriendlyTitle('pg_stat_activity.blocked_count')).toBe(
            'Blocked Queries',
        );
    });

    it('maps pg_stat_activity.max_lock_wait_seconds', () => {
        expect(
            getFriendlyTitle('pg_stat_activity.max_lock_wait_seconds'),
        ).toBe('Lock Wait Time');
    });

    it('maps pg_stat_checkpointer.checkpoints_req_delta', () => {
        expect(
            getFriendlyTitle('pg_stat_checkpointer.checkpoints_req_delta'),
        ).toBe('Checkpoint Requests');
    });

    it('maps pg_stat_archiver.failed_count_delta', () => {
        expect(
            getFriendlyTitle('pg_stat_archiver.failed_count_delta'),
        ).toBe('WAL Archive Failures');
    });

    it('maps pg_sys_cpu_usage_info.processor_time_percent', () => {
        expect(
            getFriendlyTitle(
                'pg_sys_cpu_usage_info.processor_time_percent',
            ),
        ).toBe('CPU Usage');
    });

    it('maps pg_sys_load_avg_info.load_avg_fifteen_minutes', () => {
        expect(
            getFriendlyTitle(
                'pg_sys_load_avg_info.load_avg_fifteen_minutes',
            ),
        ).toBe('Load Average');
    });

    it('maps spock_exception_log.recent_count', () => {
        expect(getFriendlyTitle('spock_exception_log.recent_count')).toBe(
            'Spock Exceptions',
        );
    });

    it('maps spock_resolutions.recent_count', () => {
        expect(getFriendlyTitle('spock_resolutions.recent_count')).toBe(
            'Spock Conflict Resolutions',
        );
    });

    // -----------------------------------------------------------------
    // New alert rule names from migration v3 (AdminAlertRules display)
    // -----------------------------------------------------------------

    it('maps the spock_recent_exceptions_present rule name', () => {
        expect(getFriendlyTitle('spock_recent_exceptions_present')).toBe(
            'Spock Exceptions (Warning)',
        );
    });

    it('maps the spock_recent_exceptions_high rule name', () => {
        expect(getFriendlyTitle('spock_recent_exceptions_high')).toBe(
            'Spock Exceptions (Critical)',
        );
    });

    it('maps the spock_recent_resolutions_present rule name', () => {
        expect(getFriendlyTitle('spock_recent_resolutions_present')).toBe(
            'Spock Conflict Resolutions (Warning)',
        );
    });

    it('maps the spock_recent_resolutions_high rule name', () => {
        expect(getFriendlyTitle('spock_recent_resolutions_high')).toBe(
            'Spock Conflict Resolutions (Critical)',
        );
    });

    it('maps the replication_slot_retention_warn rule name', () => {
        expect(getFriendlyTitle('replication_slot_retention_warn')).toBe(
            'Replication Slot WAL Retention (Warning)',
        );
    });

    it('maps the replication_slot_retention_high rule name', () => {
        expect(getFriendlyTitle('replication_slot_retention_high')).toBe(
            'Replication Slot WAL Retention (Critical)',
        );
    });

    // -----------------------------------------------------------------
    // Map sanity checks
    // -----------------------------------------------------------------

    it('exports a non-empty FRIENDLY_ALERT_TITLES map', () => {
        expect(Object.keys(FRIENDLY_ALERT_TITLES).length).toBeGreaterThan(0);
    });

    it('exports a non-empty FRIENDLY_PROBE_NAMES map', () => {
        expect(Object.keys(FRIENDLY_PROBE_NAMES).length).toBeGreaterThan(0);
    });
});

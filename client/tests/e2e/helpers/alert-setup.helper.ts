/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { ApiHelper } from './api.helper';
import { StoreHelper } from './store.helper';

/**
 * A ground-truth alert row read directly from the `ai_workbench` store,
 * used to cross-check the GUI and REST API against what the real alerter
 * actually persisted.
 */
export interface StoreAlertRow {
    id: number;
    ruleId: number | null;
    status: string;
    severity: string;
    acknowledgedAt: string | null;
}

/**
 * AlertSetupHelper drives the REAL alerter deterministically by seeding
 * the metric rows its rules evaluate, rather than inserting alert rows
 * directly. It writes into the `ai_workbench` metrics tables via
 * StoreHelper (docker exec psql on the store) and cross-checks results
 * against the store and the REST API.
 *
 * The seed helpers below match the exact SQL the alerter runs in
 * `alerter/src/internal/database/metric_registry.go`:
 *
 * - `pg_stat_activity.count` counts `client backend` rows at the newest
 *   `collected_at` per connection within a 5-minute window.
 * - `pg_sys_cpu_usage_info.processor_time_percent` prefers the Windows
 *   `processor_time_percent` column, so seeding that column sets the
 *   busy percentage directly.
 * - `pg_sys_memory_info.used_percent` is `used_memory / total_memory *
 *   100`; `pg_sys_disk_info.used_percent` is `used_space / total_space *
 *   100` (per mount, `total_space > 0`).
 * - `pg_sys_load_avg_info.load_avg_fifteen_minutes` is the raw column.
 *
 * The alerter's threshold metrics window on `collected_at > NOW() - 15m`
 * (5m for connection count), so seeded rows must carry a current
 * timestamp, which `injectMetricRow` supplies by default.
 *
 * NOTE ON SAFETY: every value interpolated into the SQL below is a
 * test-controlled number or a literal from a fixed column/table
 * allow-list, never free-form user input. String values are SQL-quoted
 * via `sqlString` (single quotes doubled); StoreHelper additionally
 * shell-escapes the whole statement.
 */
export class AlertSetupHelper {
    /**
     * The alerter evaluates threshold rules on a fixed cadence. Specs
     * that seed a metric and wait for the real alerter to fire should
     * size their waits against this cycle (with generous slack for CI).
     */
    static readonly EVAL_CYCLE_MS = 60_000;

    /**
     * The alert cleaner runs on its own cadence, clearing alerts whose
     * underlying metric has recovered below threshold.
     */
    static readonly ALERT_CLEANER_MS = 30_000;

    /**
     * After an alert clears, the same rule will not re-fire until this
     * cooldown elapses, preventing alert storms.
     */
    static readonly REFIRE_COOLDOWN_MS = 5 * 60_000;

    private readonly store: StoreHelper;
    private readonly api: ApiHelper;

    constructor(store: StoreHelper = new StoreHelper(), api: ApiHelper = new ApiHelper()) {
        this.store = store;
        this.api = api;
    }

    // ---------------------------------------------------------------
    // Generic metric seeding
    // ---------------------------------------------------------------

    /**
     * Insert a single row into `metrics.<table>` for a test. The caller
     * supplies the non-timestamp columns (including `connection_id`);
     * `collected_at` is appended automatically from `collectedAtExpr`
     * (default `NOW()`). Number values are inlined; string values are
     * SQL-quoted.
     */
    async injectMetricRow(
        table: string,
        cols: Record<string, string | number>,
        collectedAtExpr: string = 'NOW()',
    ): Promise<void> {
        const names = Object.keys(cols);
        const values = names.map((name) => AlertSetupHelper.formatValue(cols[name]));
        const sql =
            `INSERT INTO metrics.${table} ` +
            `(${names.join(', ')}, collected_at) ` +
            `VALUES (${values.join(', ')}, ${collectedAtExpr})`;
        await this.store.query(sql);
    }

    // ---------------------------------------------------------------
    // Typed convenience seeders (one per threshold metric)
    // ---------------------------------------------------------------

    /**
     * Seed the client-backend connection count for a connection. Inserts
     * `count` rows into `metrics.pg_stat_activity` sharing a single
     * `collected_at` so the alerter's `COUNT(*)` over the newest snapshot
     * yields exactly `count` (matching `pg_stat_activity.count`). Distinct
     * pids satisfy the table's `(connection_id, collected_at, pid)`
     * primary key.
     */
    async seedConnectionCount(connectionId: number, count: number): Promise<void> {
        if (count <= 0) {
            return;
        }
        const sql =
            `INSERT INTO metrics.pg_stat_activity ` +
            `(connection_id, pid, backend_type, state, collected_at) ` +
            `SELECT ${connectionId}, gs.pid, 'client backend', 'active', NOW() ` +
            `FROM generate_series(1, ${count}) AS gs(pid)`;
        await this.store.query(sql);
    }

    /**
     * Seed the CPU busy percentage. The alerter prefers the Windows
     * `processor_time_percent` column, so writing it sets the derived
     * value directly (clamped to 0-100 server-side).
     */
    async seedCpu(connectionId: number, percent: number): Promise<void> {
        await this.injectMetricRow('pg_sys_cpu_usage_info', {
            connection_id: connectionId,
            processor_time_percent: percent,
        });
    }

    /**
     * Seed the memory used-percentage. The alerter computes
     * `used_memory / total_memory * 100`, so a fixed total of 1,000,000
     * with `used_memory = round(percent * 10000)` yields exactly
     * `percent`.
     */
    async seedMemory(connectionId: number, percent: number): Promise<void> {
        const total = 1_000_000;
        const used = Math.round(percent * 10_000);
        await this.injectMetricRow('pg_sys_memory_info', {
            connection_id: connectionId,
            total_memory: total,
            used_memory: used,
        });
    }

    /**
     * Seed the 15-minute load average (the column the alerter reads
     * directly for `pg_sys_load_avg_info.load_avg_fifteen_minutes`).
     */
    async seedLoad(connectionId: number, value: number): Promise<void> {
        await this.injectMetricRow('pg_sys_load_avg_info', {
            connection_id: connectionId,
            load_avg_fifteen_minutes: value,
        });
    }

    /**
     * Seed the disk used-percentage for a mount point. The alerter takes
     * `MAX(used_space / total_space * 100)` over the newest row per mount
     * (requiring `total_space > 0`), so a fixed total of 1,000,000 with
     * `used_space = round(percent * 10000)` yields exactly `percent`.
     */
    async seedDisk(
        connectionId: number,
        percent: number,
        mountPoint: string = '/',
    ): Promise<void> {
        const total = 1_000_000;
        const used = Math.round(percent * 10_000);
        await this.injectMetricRow('pg_sys_disk_info', {
            connection_id: connectionId,
            mount_point: mountPoint,
            total_space: total,
            used_space: used,
        });
    }

    // ---------------------------------------------------------------
    // Node role / status control
    // ---------------------------------------------------------------

    /**
     * Control a server's topology status by writing a `pg_node_role`
     * snapshot at a chosen age. Topology treats a node as online when the
     * newest snapshot is under 6 minutes old, warning between 6 and 12,
     * and offline beyond 12 (or absent).
     *
     * Existing `pg_node_role` rows for the connection are removed first so
     * that the seeded row is unambiguously the newest — otherwise the real
     * collector's fresher snapshots would win. Callers that need a node to
     * stay offline may need to pause the collector or repeat this call,
     * since the collector keeps inserting fresh rows.
     */
    async ageNodeRole(connectionId: number, minutesAgo: number): Promise<void> {
        await this.store.query(
            `DELETE FROM metrics.pg_node_role WHERE connection_id = ${connectionId}`,
        );
        await this.injectMetricRow(
            'pg_node_role',
            {
                connection_id: connectionId,
                is_in_recovery: 'false',
                primary_role: 'primary',
            },
            `NOW() - INTERVAL '${minutesAgo} minutes'`,
        );
    }

    /**
     * Raise or clear the critical connection alert by setting (or
     * clearing) `connections.connection_error`. Passing `null` clears it.
     */
    async setConnectionError(
        connectionId: number,
        error: string | null,
    ): Promise<void> {
        const value = error === null ? 'NULL' : AlertSetupHelper.sqlString(error);
        await this.store.query(
            `UPDATE connections SET connection_error = ${value} WHERE id = ${connectionId}`,
        );
    }

    /**
     * Deterministically drive the real alerter to CLEAR a threshold alert.
     *
     * The alerter's resolution check (engine/cleanup.go `checkAlertResolved`)
     * compares the live metric against the alert row's OWN stored
     * `threshold_value` — captured at fire time — NOT the current override.
     * So an alert fired at threshold 0 (`> 0`, always true for a live
     * metric) can never clear by raising the OVERRIDE; the cleaner keeps
     * seeing `value > 0`. Waiting on the 5-minute `pg_stat_activity`
     * staleness window is the only other natural path and is too slow/flaky.
     *
     * Instead we raise the alert row's stored threshold directly so the next
     * cleaner cycle (~30s) sees the condition resolved and clears it through
     * the real engine. The caller must remove the forcing override first so
     * the evaluator does not re-fire (and reset the stored threshold) on its
     * next pass.
     */
    async raiseStoredThreshold(
        alertId: number,
        value: number = 999_999_999,
    ): Promise<void> {
        await this.store.query(
            `UPDATE alerts SET threshold_value = ${value} WHERE id = ${alertId}`,
        );
    }

    // ---------------------------------------------------------------
    // Direct alert seeding (fixtures)
    // ---------------------------------------------------------------

    /**
     * Insert a single ACTIVE threshold alert for a connection directly into
     * the store and return its id.
     *
     * This is the ONE place that deliberately departs from this helper's
     * "drive the real alerter" philosophy. The metric seeders above make the
     * alerter fire on its next evaluation cycle, which is right for lifecycle
     * assertions but too slow — and cross-spec-dependent — when a
     * data-dependent, single-tool check just needs a real alert to report on.
     * The Ask-Ellie `get_alert_history` coverage (contract + screenshot) runs
     * in isolation, where no alert has ever fired, so it seeds one row here,
     * asserts the tool returns it, and ALWAYS removes it again via
     * {@link deleteAlert} in teardown — exactly as the `get_blackouts`
     * coverage creates and deletes a temporary blackout.
     *
     * `marker` is written verbatim into the alert title so the caller can
     * assert the seeded row appears in a tool response; keep it to safe
     * characters (letters, digits, hyphens) so TSV formatting cannot alter
     * it. `rule_id` is left NULL (the column is nullable, FK ON DELETE SET
     * NULL), so the insert depends on no pre-existing `alert_rules` row.
     *
     * SAFETY: `connectionId` is a test-controlled integer and every string
     * is SQL-quoted via `sqlString`; StoreHelper additionally shell-escapes
     * the statement. The column list and literals are a fixed allow-list.
     */
    async seedActiveAlert(connectionId: number, marker: string): Promise<number> {
        const title = AlertSetupHelper.sqlString(marker);
        const description = AlertSetupHelper.sqlString(
            `E2E seeded active alert (${marker})`,
        );
        const id = await this.store.queryScalar(
            `INSERT INTO alerts ` +
            `(alert_type, connection_id, metric_name, metric_value, ` +
            `threshold_value, operator, severity, title, description, ` +
            `status, triggered_at) ` +
            `VALUES ('threshold', ${connectionId}, ` +
            `'connection_utilization_percent', 95, 80, '>', 'warning', ` +
            `${title}, ${description}, 'active', NOW()) ` +
            `RETURNING id`,
        );
        const parsed = id === null ? Number.NaN : Number.parseInt(id, 10);
        if (!Number.isInteger(parsed) || parsed <= 0) {
            throw new Error(
                `seedActiveAlert: expected a positive alert id, got ${id}`,
            );
        }
        return parsed;
    }

    /**
     * Delete a single alert row by id (cascades to `alert_acknowledgments`).
     * Pairs with {@link seedActiveAlert} so a seeded fixture alert can be
     * removed by its exact id in teardown, leaving any other alerts intact.
     */
    async deleteAlert(alertId: number): Promise<void> {
        await this.store.query(`DELETE FROM alerts WHERE id = ${alertId}`);
    }

    // ---------------------------------------------------------------
    // Ground-truth reads & cleanup
    // ---------------------------------------------------------------

    /**
     * Read the alerter's persisted alerts for a connection directly from
     * the store, for a ground-truth cross-check against the GUI/API.
     * `acknowledgedAt` is joined from `alert_acknowledgments` (null when
     * the alert is not acknowledged).
     */
    async readAlertsFromStore(connectionId: number): Promise<StoreAlertRow[]> {
        const rows = await this.store.query(
            `SELECT a.id, a.rule_id, a.status, a.severity, ` +
            `MAX(ak.acknowledged_at) ` +
            `FROM alerts a ` +
            `LEFT JOIN alert_acknowledgments ak ON ak.alert_id = a.id ` +
            `WHERE a.connection_id = ${connectionId} ` +
            `GROUP BY a.id, a.rule_id, a.status, a.severity ` +
            `ORDER BY a.id`,
        );
        return rows.map((row) => ({
            id: Number.parseInt(row[0], 10),
            ruleId: row[1] === '' ? null : Number.parseInt(row[1], 10),
            status: row[2],
            severity: row[3],
            acknowledgedAt: row[4] === '' ? null : row[4],
        }));
    }

    /**
     * Remove all test-owned rows for a connection. The alert tables and
     * seeded metric rows are NOT covered by the e2e-test-* teardown, so
     * specs must self-clean. Deletes the connection's alerts (cascading to
     * `alert_acknowledgments`), its server-scoped threshold overrides, the
     * seeded metric snapshots, and clears any injected connection error.
     */
    async cleanup(connectionId: number): Promise<void> {
        // Alerts (cascades to alert_acknowledgments via FK ON DELETE CASCADE).
        await this.store.query(
            `DELETE FROM alerts WHERE connection_id = ${connectionId}`,
        );
        // Server-scoped threshold overrides for this connection.
        await this.store.query(
            `DELETE FROM alert_thresholds WHERE connection_id = ${connectionId}`,
        );
        // Seeded metric rows. Each seeder writes a current timestamp, so a
        // generous window removes them without touching real historical data.
        for (const table of AlertSetupHelper.SEEDED_METRIC_TABLES) {
            await this.store.query(
                `DELETE FROM metrics.${table} ` +
                `WHERE connection_id = ${connectionId} ` +
                `AND collected_at > NOW() - INTERVAL '1 hour'`,
            );
        }
        // pg_node_role rows are deleted outright: ageNodeRole replaces
        // them wholesale, so a test that used it leaves a synthetic row.
        await this.store.query(
            `DELETE FROM metrics.pg_node_role WHERE connection_id = ${connectionId}`,
        );
        // Clear any injected connection error.
        await this.setConnectionError(connectionId, null);
    }

    // ---------------------------------------------------------------
    // Private helpers
    // ---------------------------------------------------------------

    /**
     * Metric tables written by the typed seeders, cleaned up by
     * `cleanup`. `pg_node_role` is handled separately (deleted outright).
     */
    private static readonly SEEDED_METRIC_TABLES = [
        'pg_stat_activity',
        'pg_sys_cpu_usage_info',
        'pg_sys_memory_info',
        'pg_sys_disk_info',
        'pg_sys_load_avg_info',
    ];

    /** Format a column value for inline SQL: numbers raw, strings quoted. */
    private static formatValue(value: string | number): string {
        return typeof value === 'number'
            ? value.toString()
            : AlertSetupHelper.sqlString(value);
    }

    /** Quote a string as a SQL literal, doubling embedded single quotes. */
    private static sqlString(value: string): string {
        return `'${value.replace(/'/g, "''")}'`;
    }
}

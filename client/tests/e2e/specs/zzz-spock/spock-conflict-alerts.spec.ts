/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { test, expect, type Page } from '@playwright/test';
import { label } from 'allure-js-commons';
import { ApiHelper, type AlertRuleApiRow } from '../../helpers/api.helper';
import { AuthHelper } from '../../helpers/auth.helper';
import { ReplicationHelper } from '../../helpers/replication.helper';
import { AlertSetupHelper } from '../../helpers/alert-setup.helper';
import { StoreHelper } from '../../helpers/store.helper';
import {
    SpockReplicationHelper,
    type SpockNodeIndex,
} from '../../helpers/spock-replication.helper';
import { StatusPanelAlertsPage } from '../../pages/StatusPanelAlertsPage';
import { ServerNavigatorPage } from '../../pages/ServerNavigatorPage';
import { MonitoringPage } from '../../pages/MonitoringPage';
import {
    ensureSpockClusterAndNodes,
    SPOCK_CLUSTER_NAME,
    SPOCK_NODE1,
    SPOCK_NODE2,
} from '../../helpers/spock-cluster-setup.helper';
import { API_URL } from '../../fixtures/test-data';

// Engine timing constants (shared with the logical alert-lifecycle spec). The
// spock conflict-alert pipeline is driven entirely through REAL conflicts and
// the REAL alerter — nothing here inserts synthetic alert or resolution rows.
const { EVAL_CYCLE_MS, ALERT_CLEANER_MS } = AlertSetupHelper;

// Built-in spock rule names seeded by the collector v3 migration (category
// `replication`, required_extension `spock`, operator `>=`). Verified against
// collector/src/database/schema.go and the alerter metric registry.
const RULE_RES_PRESENT = 'spock_recent_resolutions_present'; // >= 1  warning
const RULE_RES_HIGH = 'spock_recent_resolutions_high';       // >= 25 critical
const RULE_EXC_PRESENT = 'spock_recent_exceptions_present';  // >= 1  warning

// e2e-only speed lever (no product change): lower the COLLECTION INTERVAL of the
// two spock conflict probes so the collector stores fresh samples — including
// the post-delete 0-count sample that clears an alert — every few seconds rather
// than at the seeded default. Mirrors cluster-stats-verification.spec.ts. These
// probe rows are seeded global defaults (connection_id NULL); the datastore
// reset on `docker compose down` restores them, so no explicit restore is
// needed. This does NOT touch the alerter's 5-minute recent_count cutoff or its
// 5-minute re-fire cooldown, which remain product floors.
const SPOCK_CONFLICT_PROBES = ['spock_resolutions', 'spock_exception_log'];
const FAST_PROBE_INTERVAL_SECONDS = 5;

// Friendly-title fragments the StatusPanel renders for these rules (see
// client/src/utils/friendlyNames.ts). Used to scope status-panel rows on the
// shared estate. If those mappings change, update these fragments to match.
const FRAG_RES_WARNING = 'Conflict Resolutions (Warning)';
const FRAG_RES_CRITICAL = 'Conflict Resolutions (Critical)';
const FRAG_EXC_WARNING = 'Spock Exceptions (Warning)';

// Per-test PK ranges in `documents`. Kept disjoint so tests stay independent
// and idempotent; the whole [100000, 400000) band is swept clean in reset.
const RANGE_LO = 100_000;
const RANGE_HI = 400_000;
// Test 1 (resolutions) generates all four real conflict types on node1 in
// disjoint id ranges; Test 2 (exceptions) generates both real apply-error
// triggers. Every range sits inside [RANGE_LO, RANGE_HI), which reset sweeps,
// so no per-range cleanup is needed.
const T1_PK_BASE = 100_000;     // INSERT-INSERT PK conflict ids (documents_pkey)
const T1_UK_A = 100_100;        // secondary-unique conflict (node a id)
const T1_UK_B = 100_101;        // secondary-unique conflict (node b id)
const T3_EXC_ID = 300_000;      // id whose replicated apply violates the CHECK
const T4_UPD_BASE = 350_000;    // UPDATE-UPDATE conflict ids (node1 winner)
const T5_DELDEL_BASE = 360_000; // DELETE-DELETE conflict ids
const T6_UPDDEL_BASE = 370_000; // UPDATE-vs-DELETE apply-error id (exception)

// Exception scenario: a LOCAL CHECK constraint on node2 (DDL does not
// replicate, so it stays local) rejects any row whose `content` equals the
// forbidden marker. An INSERT of that marker on node1 replicates to node2,
// where the apply violates the CHECK and spock (exception_behaviour =
// transdiscard, the default) discards + logs it to spock.exception_log
// WITHOUT stranding the subscription. Fixed name: reset drops it IF EXISTS
// before and after every test, so the mesh never inherits it.
const T3_CHECK_NAME = 'e2e_exc_check';
const T3_MARKER = 'e2e-exc-violation';

/**
 * Phase 4 of the Spock E2E suite: the CONFLICT alert lifecycle driven by REAL
 * spock conflicts on the 3-node full mesh.
 *
 * Two consolidated tests, each: GENERATE different REAL conflict types (all
 * catalog-verified) -> verify the alerts go ACTIVE (API + GUI) -> CLEAR them.
 *
 * - Test 1 (resolutions) generates all four real resolution conflict types
 *   (insert PK, insert secondary-unique, update-update, delete-delete) in one
 *   pass, then escalates warning -> critical and clears both.
 * - Test 2 (exceptions) generates both real apply-error triggers (a local
 *   CHECK violation on node2 and an update-vs-delete on node1) and clears.
 *
 * The conflicts are always REAL — every scenario proves the spock CATALOG
 * recorded the actual conflict/exception before asserting any alert. Only the
 * alert THRESHOLDS are tuned via e2e-only per-connection overrides (there is
 * ZERO product-code change): a LOW override escalates the `_high` rule with the
 * handful of real conflicts already generated (rather than generating 25+), and
 * a HIGH override plus a raised stored threshold clears each alert on the next
 * cleaner cycle (rather than draining the 5-minute recent_count window).
 *
 * Every step is wrapped in a bounded `toPass` sized against the engine cadence.
 */
test.describe('Spock Conflict Alerts', () => {
    test.use({ storageState: '.auth/admin.json' });

    const api = new ApiHelper(API_URL);
    const auth = new AuthHelper(api);
    const alertSetup = new AlertSetupHelper();
    const replication = new ReplicationHelper();
    const spock = new SpockReplicationHelper();
    const store = new StoreHelper();

    let adminCookie: string;
    let nodeConnId: number;
    let node2ConnId: number;
    const rules: Record<string, AlertRuleApiRow> = {};

    // The full mesh links, expressed as (target-on-which-the-sub-lives, from).
    const ALL_LINKS: ReadonlyArray<readonly [SpockNodeIndex, SpockNodeIndex]> = [
        [1, 2], [1, 3], [2, 1], [2, 3], [3, 1], [3, 2],
    ];

    test.beforeAll(async () => {
        test.setTimeout(300_000);
        await spock.start();
        adminCookie = await auth.loginAsAdmin();
        // Speed up the fire/clear cycle by lowering the two conflict probes'
        // collection interval before any conflict is generated. The seeded rows
        // are global (connection_id NULL); set every match to be safe. The
        // collector picks up the new interval on the next restart (each fire
        // point restarts it via collectAndAwaitSample).
        await enableFastSpockConflictProbes();
    });

    test.beforeEach(async ({ page }) => {
        test.setTimeout(240_000);
        await label('package', 'Alert Management (Spock)');

        await ensureSpockClusterAndNodes(page, api, adminCookie);

        const connections = await api.listConnections({ cookie: adminCookie });
        const node = connections.find((c) => c.name === SPOCK_NODE1.name);
        if (!node) {
            throw new Error(
                `No connection named "${SPOCK_NODE1.name}" found. Available: `
                + connections.map((c) => c.name).join(', '),
            );
        }
        nodeConnId = node.id;

        // The exception scenario surfaces on node2 (that is where the CHECK
        // constraint lives and the failing apply logs), so resolve its
        // connection too. Tests 1 & 2 still read node1 only.
        const node2 = connections.find((c) => c.name === SPOCK_NODE2.name);
        if (!node2) {
            throw new Error(
                `No connection named "${SPOCK_NODE2.name}" found. Available: `
                + connections.map((c) => c.name).join(', '),
            );
        }
        node2ConnId = node2.id;

        // Resolve the built-in spock rules by name (they must exist — the
        // collector seeds them on migration).
        for (const name of [RULE_RES_PRESENT, RULE_RES_HIGH, RULE_EXC_PRESENT]) {
            const rule = await api.getAlertRuleByName(adminCookie, name);
            expect(rule, `built-in rule "${name}" should exist`).toBeTruthy();
            rules[name] = rule!;
        }

        // Start every scenario from a clean, healthy mesh with no leftover
        // conflicts, metrics, alerts or overrides on node1 or node2, and no
        // stray exception-test CHECK constraint.
        await resetSpockConflictState();
    });

    test.afterEach(async () => {
        await resetSpockConflictState();
    });

    test.afterAll(async () => {
        await spock.stop().catch(() => undefined);
    });

    // ------------------------------------------------------------------
    // Shared helpers
    // ------------------------------------------------------------------

    /**
     * Return the mesh to a clean, healthy baseline: drop the exception
     * scenario's local CHECK constraint, re-enable every subscription, drop the
     * seeded `documents` rows and the spock conflict catalogs on all three
     * nodes, then clear the stored metric samples, alerts and overrides for
     * BOTH node1 (resolutions scenarios) and node2 (exception scenario) so the
     * next scenario cannot inherit a firing condition.
     *
     * There is nothing to revert for spock.conflict_resolution: spock 5.0.11
     * accepts only `last_update_wins`, so no scenario ever changes it.
     *
     * Everything is best-effort: a scenario may have been interrupted, so a
     * failed statement must not mask the underlying test result.
     */
    async function resetSpockConflictState(): Promise<void> {
        // Drop the exception scenario's LOCAL CHECK constraint on every node
        // (DDL does not replicate, so it only ever lands on node2, but dropping
        // everywhere is harmless and defends against an interrupted test).
        for (const n of [1, 2, 3] as const) {
            await spock
                .execNode(
                    n,
                    `ALTER TABLE documents DROP CONSTRAINT IF EXISTS ${T3_CHECK_NAME}`,
                )
                .catch(() => undefined);
        }

        // Re-enable every link (immediate) so a disabled subscription recovers.
        for (const [target, from] of ALL_LINKS) {
            await spock
                .enableSub(target, spock.subName(target, from))
                .catch(() => undefined);
        }

        // Drop seeded rows first, let deletes settle, then wipe the catalogs
        // last so delete-missing resolutions produced by the cleanup deletes
        // do not repopulate spock.resolutions after we clear it.
        for (const n of [1, 2, 3] as const) {
            await spock
                .execNode(n, `DELETE FROM documents WHERE id >= ${RANGE_LO} AND id < ${RANGE_HI}`)
                .catch(() => undefined);
        }
        await new Promise<void>((r) => setTimeout(r, 3_000));
        for (const n of [1, 2, 3] as const) {
            await spock.execNode(n, 'DELETE FROM spock.resolutions').catch(() => undefined);
            await spock.execNode(n, 'DELETE FROM spock.exception_log').catch(() => undefined);
        }

        // Clear stored metric samples for both surfaced connections so a stale
        // latest-sample cannot keep an alert alive across scenarios.
        for (const connId of [nodeConnId, node2ConnId]) {
            if (!connId) {
                continue;
            }
            await store
                .query(`DELETE FROM metrics.spock_resolutions WHERE connection_id = ${connId}`)
                .catch(() => undefined);
            await store
                .query(`DELETE FROM metrics.spock_exception_log WHERE connection_id = ${connId}`)
                .catch(() => undefined);
            for (const rule of Object.values(rules)) {
                await api
                    .deleteAlertOverride(adminCookie, 'server', connId, rule.id)
                    .catch(() => undefined);
            }
            await alertSetup.cleanup(connId).catch(() => undefined);
        }
    }

    /**
     * Lower the collection interval of the spock conflict probes so fresh
     * samples land every few seconds. Applies to every row returned for each
     * probe name (the seeded rows are global, connection_id NULL). Best-effort
     * per row so a missing probe cannot fail setup.
     */
    async function enableFastSpockConflictProbes(): Promise<void> {
        const configs = await api.getProbeConfigs(adminCookie);
        for (const name of SPOCK_CONFLICT_PROBES) {
            for (const cfg of configs.filter((c) => c.name === name)) {
                await api
                    .updateProbeConfig(adminCookie, cfg.id, {
                        is_enabled: true,
                        collection_interval_seconds: FAST_PROBE_INTERVAL_SECONDS,
                    })
                    .catch(() => undefined);
            }
        }
    }

    /** Disable both directions of the link between nodes a and b (immediate). */
    async function disablePair(a: SpockNodeIndex, b: SpockNodeIndex): Promise<void> {
        await spock.disableSub(a, spock.subName(a, b));
        await spock.disableSub(b, spock.subName(b, a));
    }

    /** Re-enable both directions of the link between nodes a and b. */
    async function enablePair(a: SpockNodeIndex, b: SpockNodeIndex): Promise<void> {
        await spock.enableSub(a, spock.subName(a, b));
        await spock.enableSub(b, spock.subName(b, a));
    }

    /**
     * INSERT-INSERT PK conflict across a link: disable the pair, insert the
     * same id range on both nodes with node-distinct content and node-distinct
     * emails (so the ONLY conflict is on the primary key, not the unique
     * index), then re-enable. When node `a` applies node `b`'s inserts it hits
     * `count` PK conflicts, each auto-resolved into spock.resolutions on `a`.
     */
    async function insertInsertPkConflict(
        a: SpockNodeIndex,
        b: SpockNodeIndex,
        baseId: number,
        count: number,
    ): Promise<void> {
        const lo = baseId;
        const hi = baseId + count - 1;
        await disablePair(a, b);
        for (const n of [a, b]) {
            await spock.execNode(
                n,
                `INSERT INTO documents (id, content, email, metadata) `
                + `SELECT gs, 'n${n}-pk-' || gs, 'pk' || gs || '-n${n}@x.test', `
                + `'{}'::jsonb FROM generate_series(${lo}, ${hi}) AS gs`,
            );
        }
        await enablePair(a, b);
    }

    /**
     * INSERT-INSERT conflict on the secondary UNIQUE index (documents_email_uk):
     * distinct ids, same email. When node `a` applies node `b`'s insert the
     * unique index collides, resolved into spock.resolutions with
     * idxname = documents_email_uk.
     */
    async function insertInsertUniqueConflict(
        a: SpockNodeIndex,
        b: SpockNodeIndex,
        idA: number,
        idB: number,
        email: string,
    ): Promise<void> {
        await disablePair(a, b);
        await spock.execNode(
            a,
            `INSERT INTO documents (id, content, email) `
            + `VALUES (${idA}, 'n${a}-uk', '${email}')`,
        );
        await spock.execNode(
            b,
            `INSERT INTO documents (id, content, email) `
            + `VALUES (${idB}, 'n${b}-uk', '${email}')`,
        );
        await enablePair(a, b);
    }

    /**
     * UPDATE-UPDATE conflict across a link, engineered so the resolution lands
     * on `winner`. In spock 5.0.11 an update_update conflict is recorded ONLY on
     * the node whose UPDATE commits LAST — it keeps its own (newer) value
     * (conflict_resolution = keep_local) and logs a single update_update
     * resolution per id. The other node applies the remote value
     * (apply_remote) and logs nothing. (This differs from insert_exists, which
     * logs on every node.)
     *
     * Recipe: seed `count` rows on `winner` while the mesh is healthy and wait
     * (bounded) for them to propagate to `loser`; cut the link; UPDATE on
     * `loser` FIRST (older commit → loses); after a small delay UPDATE on
     * `winner` SECOND (later commit → wins); reconnect. The loser's older
     * update then replicates to the winner and loses to the winner's newer local
     * value, producing exactly one update_update (keep_local) resolution per id
     * on the winner only.
     */
    async function updateUpdateConflict(
        winner: SpockNodeIndex,
        loser: SpockNodeIndex,
        baseId: number,
        count: number,
    ): Promise<void> {
        const lo = baseId;
        const hi = baseId + count - 1;

        // 1. Seed the baseline rows on the winner while the mesh is healthy, so
        //    both nodes hold the same starting value before we diverge. Emails
        //    are node/id distinct so seeding never trips the unique index.
        await spock.execNode(
            winner,
            `INSERT INTO documents (id, content, email) `
            + `SELECT gs, 'orig', 'upd' || gs || '-n${winner}@x.test' `
            + `FROM generate_series(${lo}, ${hi}) AS gs`,
        );

        // 2. Wait (bounded) until every seeded row has propagated to the loser,
        //    so the divergent UPDATE on the loser targets an existing row.
        await expect(async () => {
            const out = await spock.execNode(
                loser,
                `SELECT count(*) FROM documents `
                + `WHERE id >= ${lo} AND id <= ${hi}`,
            );
            expect(
                Number.parseInt(out.trim() || '0', 10),
                `seeded rows should propagate to node${loser} before diverging`,
            ).toBe(count);
        }).toPass({ timeout: 60_000, intervals: [2_000] });

        // 3. Cut the link so the two UPDATEs commit independently.
        await disablePair(winner, loser);

        // 4. UPDATE on the loser FIRST (older commit → its value loses).
        await spock.execNode(
            loser,
            `UPDATE documents SET content = 'u-n${loser}' `
            + `WHERE id >= ${lo} AND id <= ${hi}`,
        );

        // 5. Small delay to guarantee a strictly later commit timestamp, then
        //    UPDATE on the winner SECOND (later commit → wins, keep_local).
        await new Promise<void>((r) => setTimeout(r, 1_000));
        await spock.execNode(
            winner,
            `UPDATE documents SET content = 'u-n${winner}' `
            + `WHERE id >= ${lo} AND id <= ${hi}`,
        );

        // 6. Reconnect: the loser's older update replicates to the winner and
        //    loses to the winner's newer local value, recording exactly one
        //    update_update (keep_local) resolution per id on the winner only.
        await enablePair(winner, loser);
    }

    /**
     * DELETE-DELETE conflict across a link. In spock 5.0.11 a delete_delete
     * conflict is recorded on EVERY node that receives a remote DELETE for a row
     * it has already deleted locally (conflict_type = delete_delete,
     * conflict_resolution = skip), so reading the winning node (`a`) works just
     * like the insert conflicts.
     *
     * Recipe: seed `count` rows on `a` while the mesh is healthy and wait
     * (bounded) for them to propagate to `b`; cut the link; DELETE the same id
     * range on BOTH nodes so each holds a locally-deleted row; reconnect. Each
     * node then receives the other's DELETE for the now-missing row, producing
     * exactly one delete_delete (skip) resolution per id.
     */
    async function deleteDeleteConflict(
        a: SpockNodeIndex,
        b: SpockNodeIndex,
        baseId: number,
        count: number,
    ): Promise<void> {
        const lo = baseId;
        const hi = baseId + count - 1;

        // 1. Seed the baseline rows on node `a` while the mesh is healthy.
        //    Emails are node/id distinct so seeding never trips the unique index.
        await spock.execNode(
            a,
            `INSERT INTO documents (id, content, email) `
            + `SELECT gs, 'orig', 'del' || gs || '-n${a}@x.test' `
            + `FROM generate_series(${lo}, ${hi}) AS gs`,
        );

        // 2. Wait (bounded) until every seeded row has propagated to node `b`,
        //    so the divergent DELETE on `b` targets an existing row.
        await expect(async () => {
            const out = await spock.execNode(
                b,
                `SELECT count(*) FROM documents `
                + `WHERE id >= ${lo} AND id <= ${hi}`,
            );
            expect(
                Number.parseInt(out.trim() || '0', 10),
                `seeded rows should propagate to node${b} before diverging`,
            ).toBe(count);
        }).toPass({ timeout: 60_000, intervals: [2_000] });

        // 3. Cut the link so the two DELETEs commit independently.
        await disablePair(a, b);

        // 4. DELETE the same id range on BOTH nodes.
        for (const n of [a, b]) {
            await spock.execNode(
                n,
                `DELETE FROM documents WHERE id >= ${lo} AND id <= ${hi}`,
            );
        }

        // 5. Reconnect: each node receives the other's DELETE for an
        //    already-deleted row, recording one delete_delete (skip) resolution
        //    per id on both nodes (and node3, which received both deletes).
        await enablePair(a, b);
    }

    /**
     * UPDATE-vs-DELETE apply error across a link. Unlike the resolution
     * scenarios, this does NOT produce a spock.resolutions row: the node that
     * deleted the row receives a remote UPDATE for a now-missing row, which is a
     * genuine apply ERROR. With spock.exception_behaviour = transdiscard (the
     * default) the offending apply is discarded and logged to
     * spock.exception_log on the DELETING node WITHOUT stranding the
     * subscription — no CHECK constraint is involved.
     *
     * Recipe: seed the row on `deleteNode` while the mesh is healthy and wait
     * (bounded) for it to propagate to `updateNode`; cut the link; DELETE the
     * row on `deleteNode`; UPDATE the SAME id on `updateNode`; reconnect. The
     * remote UPDATE then arrives at `deleteNode` for the missing row and fails
     * to apply, logging one exception on `deleteNode` (also node3; the updating
     * node logs none).
     */
    async function updateDeleteConflict(
        deleteNode: SpockNodeIndex,
        updateNode: SpockNodeIndex,
        id: number,
    ): Promise<void> {
        // 1. Seed the row on the node that will DELETE it, while the mesh is
        //    healthy, so the update node holds the same starting row.
        await spock.execNode(
            deleteNode,
            `INSERT INTO documents (id, content, email) `
            + `VALUES (${id}, 'orig', 'upddel${id}-n${deleteNode}@x.test')`,
        );

        // 2. Wait (bounded) until the seeded row has propagated to the update
        //    node, so its divergent UPDATE targets an existing row.
        await expect(async () => {
            const out = await spock.execNode(
                updateNode,
                `SELECT count(*) FROM documents WHERE id = ${id}`,
            );
            expect(
                Number.parseInt(out.trim() || '0', 10),
                `seeded row should propagate to node${updateNode} before diverging`,
            ).toBe(1);
        }).toPass({ timeout: 60_000, intervals: [2_000] });

        // 3. Cut the link so the DELETE and UPDATE commit independently.
        await disablePair(deleteNode, updateNode);

        // 4. DELETE the row on the delete node.
        await spock.execNode(
            deleteNode,
            `DELETE FROM documents WHERE id = ${id}`,
        );

        // 5. UPDATE the same id on the update node.
        await spock.execNode(
            updateNode,
            `UPDATE documents SET content = 'u-n${updateNode}' WHERE id = ${id}`,
        );

        // 6. Reconnect: the remote UPDATE arrives at the delete node for the
        //    now-missing row and fails to apply. transdiscard discards + logs it
        //    to spock.exception_log on the delete node without stranding the
        //    subscription (no retry storm).
        await enablePair(deleteNode, updateNode);
    }

    /**
     * Total resolution rows currently in spock.resolutions on a node.
     *
     * spock 5.0.11 writes `spock.resolutions.relname` schema-qualified
     * (`public.documents`), unlike `spock.exception_log.table_name`, which is
     * the bare `documents`. Match either form so the count is correct whichever
     * spock build is under test.
     */
    async function countResolutions(node: SpockNodeIndex): Promise<number> {
        const out = await spock.execNode(
            node,
            "SELECT count(*) FROM spock.resolutions "
            + "WHERE relname IN ('documents', 'public.documents')",
        );
        return Number.parseInt(out.trim() || '0', 10);
    }

    /** Distinct conflict_type labels currently recorded on a node. */
    async function distinctConflictTypes(node: SpockNodeIndex): Promise<string[]> {
        const out = await spock.execNode(
            node,
            "SELECT DISTINCT conflict_type FROM spock.resolutions "
            + "WHERE relname IN ('documents', 'public.documents') ORDER BY 1",
        );
        return out === '' ? [] : out.split('\n');
    }

    /**
     * Distinct index names currently recorded on a node. In spock 5.0.11 a PK
     * conflict and a secondary-UNIQUE conflict share the SAME conflict_type
     * (`insert_exists`) and differ ONLY by `idxname` (`documents_pkey` vs
     * `documents_email_uk`), so the index name is what distinguishes two
     * genuinely different real conflict scenarios.
     */
    async function distinctConflictIndexes(node: SpockNodeIndex): Promise<string[]> {
        const out = await spock.execNode(
            node,
            "SELECT DISTINCT idxname FROM spock.resolutions "
            + "WHERE relname IN ('documents', 'public.documents') ORDER BY 1",
        );
        return out === '' ? [] : out.split('\n');
    }

    /** Total exception rows currently in spock.exception_log on a node. */
    async function countExceptions(node: SpockNodeIndex): Promise<number> {
        const out = await spock.execNode(
            node,
            "SELECT count(*) FROM spock.exception_log WHERE table_name = 'documents'",
        );
        return Number.parseInt(out.trim() || '0', 10);
    }

    /** Replicating subscription count on a node (2 == healthy in the mesh). */
    async function replicatingSubs(node: SpockNodeIndex): Promise<number> {
        const out = await spock.execNode(
            node,
            "SELECT count(*) FROM spock.sub_show_status() WHERE status = 'replicating'",
        );
        return Number.parseInt(out.trim() || '0', 10);
    }

    /**
     * COUNT(*) of the latest stored sample for a connection, mirroring the
     * alerter's `*.recent_count` latest CTE (single MAX(collected_at) within
     * the last 5 minutes). Proves the collector stored the conflict.
     */
    async function latestSampleCount(table: string, connId: number): Promise<number> {
        const val = await store.queryScalar(
            `WITH l AS (SELECT MAX(collected_at) AS mc FROM metrics.${table} `
            + `WHERE connection_id = ${connId} `
            + `AND collected_at > NOW() - INTERVAL '5 minutes') `
            + `SELECT COUNT(*) FROM metrics.${table} s, l `
            + `WHERE s.connection_id = ${connId} AND s.collected_at = l.mc`,
        );
        return val === null ? 0 : Number.parseInt(val, 10);
    }

    /**
     * Force a collector cycle and wait until the latest stored sample for
     * `table` on `connId` holds at least `minCount` rows (i.e. the real
     * conflict reached the metrics store). Defaults to node1; the exception
     * scenario passes node2's connection/server, since the exception is
     * collected from the node whose spock.exception_log recorded it.
     */
    async function collectAndAwaitSample(
        table: string,
        minCount: number,
        connId: number = nodeConnId,
        serverName: string = SPOCK_NODE1.name,
    ): Promise<void> {
        await replication.restartCollector([serverName]);
        await expect(async () => {
            const n = await latestSampleCount(table, connId);
            expect(
                n,
                `metrics.${table} latest sample should hold >= ${minCount} rows`,
            ).toBeGreaterThanOrEqual(minCount);
        }).toPass({ timeout: EVAL_CYCLE_MS * 3, intervals: [5_000] });
    }

    /**
     * Wait until the alerter has an ACTIVE alert for `ruleId` on `connId` with
     * the expected severity, and return its id. The rule fires off the REAL
     * stored conflict metric; an override is set beforehand only to pin the
     * threshold and severity deterministically. Defaults to node1; the
     * exception scenario passes node2's connection.
     */
    async function awaitActiveAlert(
        ruleId: number,
        severity: string,
        connId: number = nodeConnId,
    ): Promise<number> {
        let alertId = 0;
        await expect(async () => {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: connId,
                excludeCleared: true,
            });
            const active = alerts.find(
                (a) => a.rule_id === ruleId && a.status === 'active',
            );
            expect(active, `an active alert for rule ${ruleId} should fire`).toBeTruthy();
            expect(active!.severity).toBe(severity);
            alertId = active!.id;
        }).toPass({ timeout: EVAL_CYCLE_MS * 4, intervals: [5_000] });
        return alertId;
    }

    /**
     * Pin a rule's firing condition for a connection via a server-scoped
     * override. Defaults to node1; the exception scenario pins on node2.
     */
    async function pinRule(
        ruleId: number,
        threshold: number,
        severity: string,
        connId: number = nodeConnId,
    ): Promise<void> {
        await api.upsertAlertOverride(adminCookie, 'server', connId, ruleId, {
            operator: '>=',
            threshold,
            severity,
            enabled: true,
        });
    }

    /**
     * Deterministically drain the conflict metric so the REAL cleaner clears
     * the alert(s): stop generating, delete the SOURCE catalog rows on the given
     * node (superuser DELETE on the spock catalog) so the collector stores no
     * further non-empty samples. With the fast probe interval a fresh 0-count
     * sample lands within a few seconds, after which the alerter's recent_count
     * falls to zero and the alert clears.
     *
     * The 5-minute recent_count window is a product floor that still applies:
     * the worst case (no empty sample is stored, so the last non-empty sample
     * must age out of the window) is ~5 minutes plus a couple of eval cycles.
     * The 8-minute budget clears that comfortably while being far tighter than
     * the old collection-interval-bound wait. Defaults to node1; the exception
     * scenario drains node2.
     */
    async function drainAndAwaitClear(
        sourceTable: 'resolutions' | 'exception_log',
        ruleIds: number[],
        node: SpockNodeIndex = 1,
        connId: number = nodeConnId,
    ): Promise<void> {
        await spock.execNode(node, `DELETE FROM spock.${sourceTable}`).catch(() => undefined);
        await expect(async () => {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: connId,
            });
            for (const ruleId of ruleIds) {
                const row = alerts.find((a) => a.rule_id === ruleId);
                expect(row?.status, `alert for rule ${ruleId} should clear`).toBe(
                    'cleared',
                );
            }
        }).toPass({ timeout: 8 * 60_000, intervals: [5_000] });
    }

    /**
     * Clear active conflict alert(s) FAST, without draining the 5-minute
     * recent_count window (the main cost of the old 6-test file).
     *
     * For each active alert: (1) raise a HIGH server-scoped override on its rule
     * so the evaluator stops matching and cannot reset the alert's stored
     * threshold on its next pass, then (2) raise the alert row's OWN stored
     * threshold so the real cleaner (which compares the live metric against the
     * alert's stored threshold_value, see AlertSetupHelper.raiseStoredThreshold)
     * sees the condition resolved and clears it on its next ~30s cycle.
     *
     * Unlike cluster-status.spec — which can simply DELETE the override because
     * its built-in threshold sits above the real metric — the built-in spock
     * conflict rules fire at >= 1 / >= 25, so DELETING the override would let the
     * evaluator re-fire on the still-present real conflicts. Raising the override
     * high is what keeps the alert cleared. The override is removed in teardown
     * (resetSpockConflictState deletes every rule's override on both
     * connections).
     */
    async function raiseAndAwaitClear(
        entries: ReadonlyArray<{ ruleId: number; alertId: number }>,
        connId: number = nodeConnId,
    ): Promise<void> {
        for (const { ruleId, alertId } of entries) {
            // Push the evaluator threshold far above any real recent_count so it
            // cannot match (and therefore cannot reset the stored threshold).
            await pinRule(ruleId, 1_000_000_000, 'warning', connId);
            // Push the alert's own stored threshold above the live metric so the
            // cleaner resolves it on its next cycle.
            await alertSetup.raiseStoredThreshold(alertId);
        }
        await expect(async () => {
            const { alerts } = await api.getAlerts(adminCookie, {
                connectionId: connId,
            });
            for (const { ruleId } of entries) {
                const row = alerts.find((a) => a.rule_id === ruleId);
                expect(row?.status, `alert for rule ${ruleId} should clear`).toBe(
                    'cleared',
                );
            }
        }).toPass({ timeout: ALERT_CLEANER_MS * 8, intervals: [5_000] });
    }

    /** Longest alphabetic word (>= 4 chars) of a title, for GUI row scoping. */
    function longestWord(title: string): string {
        return (title || '')
            .split(/[^a-zA-Z]+/)
            .filter((w) => w.length >= 4)
            .sort((a, b) => b.length - a.length)[0] ?? '';
    }

    /**
     * Navigate to the app and select a spock server row (defaults to node1).
     * The exception scenario selects node2 so the status panel scopes to the
     * connection whose spock.exception_log recorded the apply error.
     */
    async function selectSpockNode(
        page: Page,
        monitoring: MonitoringPage,
        serverNav: ServerNavigatorPage,
        nodeName: string = SPOCK_NODE1.name,
    ): Promise<void> {
        await page.goto('/');
        await monitoring.waitForAppLoad();
        await expect(async () => {
            const row = serverNav.getServerRow(nodeName).first();
            if (!(await row.isVisible().catch(() => false))) {
                await serverNav.expandCluster(SPOCK_CLUSTER_NAME);
            }
            await row.click({ force: true });
            await expect(
                monitoring.getSectionHeader('Monitoring'),
                'Monitoring section should render after selecting the node',
            ).toBeVisible({ timeout: 5_000 });
        }).toPass({ timeout: 60_000, intervals: [3_000] });
    }

    // ------------------------------------------------------------------
    // Test 1: RESOLUTIONS — all four real resolution conflict types fire the
    //         warning, escalate to critical via a LOW `_high` override, and
    //         clear fast (raise the stored threshold, no window drain).
    // ------------------------------------------------------------------

    test('resolutions: all real conflict types fire alerts, then clear', async ({
        page,
    }) => {
        // Ceiling only — the happy path is far shorter now that the clear no
        // longer drains the 5-minute recent_count window. Re-fire after the
        // 5-minute product cooldown is a product floor DELIBERATELY not
        // exercised here: this test is generate -> verify active -> clear, per
        // the fast-runtime goal.
        test.setTimeout(15 * 60_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const alerts = new StatusPanelAlertsPage(page);
        const presentRule = rules[RULE_RES_PRESENT];
        const highRule = rules[RULE_RES_HIGH];

        // Pin the warning (present) rule deterministically. The critical (high)
        // rule is escalated later via a LOW override so the handful of real
        // conflicts below cross it WITHOUT generating 25+.
        await pinRule(presentRule.id, 1, 'warning');

        await test.step('Generate all four real resolution conflict types on node1', async () => {
            // insert_exists / documents_pkey
            await insertInsertPkConflict(1, 2, T1_PK_BASE, 3);
            // insert_exists / documents_email_uk
            await insertInsertUniqueConflict(1, 2, T1_UK_A, T1_UK_B, 'dup@x.test');
            // update_update (node1 is the winner, so it records the resolution)
            await updateUpdateConflict(1, 2, T4_UPD_BASE, 2);
            // delete_delete (recorded on every node, node1 included)
            await deleteDeleteConflict(1, 2, T5_DELDEL_BASE, 2);
        });

        await test.step('Prove the spock CATALOG recorded multiple distinct REAL conflict types on node1', async () => {
            await expect(async () => {
                const types = await distinctConflictTypes(1);
                expect(
                    types,
                    'catalog should record insert_exists, update_update and delete_delete',
                ).toEqual(
                    expect.arrayContaining([
                        'insert_exists',
                        'update_update',
                        'delete_delete',
                    ]),
                );
                const indexes = await distinctConflictIndexes(1);
                expect(
                    indexes,
                    'catalog should record BOTH the PK and the secondary-unique index',
                ).toEqual(
                    expect.arrayContaining([
                        'documents_pkey',
                        'documents_email_uk',
                    ]),
                );
                expect(
                    await countResolutions(1),
                    'node1 should hold several real resolutions',
                ).toBeGreaterThanOrEqual(4);
            }).toPass({ timeout: 120_000, intervals: [5_000] });
        });

        let warningId = 0;
        let criticalId = 0;
        await test.step('Collector stores the metric, then the warning (present) rule fires ACTIVE', async () => {
            await collectAndAwaitSample('spock_resolutions', 4);
            warningId = await awaitActiveAlert(presentRule.id, 'warning');
            expect(warningId).toBeGreaterThan(0);
        });

        await test.step('Escalate: a LOW `_high` override fires the critical rule ACTIVE on the same real conflicts', async () => {
            // No new conflicts and no 25+ generation: pin the high rule to 1 so
            // the real recent_count already crosses it and it escalates.
            await pinRule(highRule.id, 1, 'critical');
            criticalId = await awaitActiveAlert(highRule.id, 'critical');
            expect(criticalId).not.toBe(warningId);
        });

        await test.step('Deduplication: the four real conflict types keep a single alert row per rule', async () => {
            const stored = await alertSetup.readAlertsFromStore(nodeConnId);
            expect(
                stored.filter((a) => a.ruleId === presentRule.id).length,
                'exactly one alert row for the warning rule',
            ).toBe(1);
            expect(
                stored.filter((a) => a.ruleId === highRule.id).length,
                'exactly one alert row for the critical rule',
            ).toBe(1);
        });

        await test.step('Both alerts surface in the GUI status panel (node1) and cross-check the API', async () => {
            const { alerts: fired } = await api.getAlerts(adminCookie, {
                connectionId: nodeConnId,
                excludeCleared: true,
            });
            expect(
                fired.some(
                    (a) => a.rule_id === presentRule.id && a.status === 'active',
                ),
                'API lists the active warning alert',
            ).toBe(true);
            expect(
                fired.some(
                    (a) => a.rule_id === highRule.id && a.status === 'active',
                ),
                'API lists the active critical alert',
            ).toBe(true);

            await selectSpockNode(page, monitoring, serverNav);
            await alerts.expandActive();
            const warnRow = alerts.getActiveAlertRow(FRAG_RES_WARNING);
            const critRow = alerts.getActiveAlertRow(FRAG_RES_CRITICAL);
            await expect(warnRow, 'warning row visible').toBeVisible({
                timeout: 60_000,
            });
            await expect(critRow, 'critical row visible').toBeVisible({
                timeout: 60_000,
            });
            expect(
                (await alerts.readAlertRow(warnRow, false)).severity?.toLowerCase(),
            ).toBe('warning');
            expect(
                (await alerts.readAlertRow(critRow, false)).severity?.toLowerCase(),
            ).toBe('critical');
        });

        await test.step('Clear fast: raise each active alert threshold so BOTH clear', async () => {
            await raiseAndAwaitClear([
                { ruleId: presentRule.id, alertId: warningId },
                { ruleId: highRule.id, alertId: criticalId },
            ]);
        });
    });

    // ------------------------------------------------------------------
    // Test 2: EXCEPTIONS — both real apply-error triggers land in
    //         spock.exception_log (a CHECK violation on node2 and an
    //         update-vs-delete on node1) without stranding the mesh; the node2
    //         alert fires then clears fast.
    // ------------------------------------------------------------------

    test('exceptions: real apply errors fire an alert, then clear', async ({
        page,
    }) => {
        test.setTimeout(15 * 60_000);
        const monitoring = new MonitoringPage(page);
        const serverNav = new ServerNavigatorPage(page);
        const alerts = new StatusPanelAlertsPage(page);
        const excRule = rules[RULE_EXC_PRESENT];

        // The alert lifecycle runs on node2 (the CHECK scenario), so pin the
        // exception rule there.
        await pinRule(excRule.id, 1, 'warning', node2ConnId);

        let alertId = 0;
        try {
            await test.step('Generate BOTH real apply-error triggers and prove the catalog recorded them', async () => {
                // (a) CHECK violation -> exception on node2. DDL replication is
                //     off, so this CHECK stays local to node2; an INSERT of the
                //     forbidden marker on node1 replicates and fails to apply
                //     (transdiscard discards + logs it, no stranding).
                await spock.execNode(
                    2,
                    `ALTER TABLE documents ADD CONSTRAINT ${T3_CHECK_NAME} `
                    + `CHECK (content <> '${T3_MARKER}')`,
                );
                await spock.execNode(
                    1,
                    `INSERT INTO documents (id, content, email) `
                    + `VALUES (${T3_EXC_ID}, '${T3_MARKER}', 'exc-n1@x.test')`,
                );

                // (b) UPDATE-vs-DELETE -> exception on node1 (delete node1,
                //     update node2): the remote UPDATE arrives at node1 for the
                //     now-missing row and fails to apply.
                await updateDeleteConflict(1, 2, T6_UPDDEL_BASE);

                await expect(async () => {
                    expect(
                        await countExceptions(2),
                        'node2 exception_log should record the CHECK violation',
                    ).toBeGreaterThanOrEqual(1);
                    expect(
                        await countExceptions(1),
                        'node1 exception_log should record the update-vs-delete error',
                    ).toBeGreaterThanOrEqual(1);
                }).toPass({ timeout: 150_000, intervals: [5_000] });

                // transdiscard keeps every node replicating (nothing stranded).
                await expect(async () => {
                    expect(await replicatingSubs(1), 'node1 not stranded').toBe(2);
                    expect(await replicatingSubs(2), 'node2 not stranded').toBe(2);
                }).toPass({ timeout: 30_000, intervals: [5_000] });
            });

            await test.step('Collector stores the metric, then the exception warning fires ACTIVE on node2', async () => {
                await collectAndAwaitSample(
                    'spock_exception_log',
                    1,
                    node2ConnId,
                    SPOCK_NODE2.name,
                );
                alertId = await awaitActiveAlert(
                    excRule.id,
                    'warning',
                    node2ConnId,
                );
                expect(alertId).toBeGreaterThan(0);
            });

            await test.step('The active exception alert surfaces in the GUI (node2) and cross-checks the API', async () => {
                const { alerts: apiAlerts } = await api.getAlerts(adminCookie, {
                    connectionId: node2ConnId,
                    excludeCleared: true,
                });
                expect(
                    apiAlerts.some(
                        (a) => a.rule_id === excRule.id && a.status === 'active',
                    ),
                    'API lists the active exception alert',
                ).toBe(true);

                await selectSpockNode(page, monitoring, serverNav, SPOCK_NODE2.name);
                await alerts.expandActive();
                const row = alerts.getActiveAlertRow(FRAG_EXC_WARNING);
                await expect(
                    row,
                    'the exception alert row should render in the status panel',
                ).toBeVisible({ timeout: 60_000 });
                expect(
                    (await alerts.readAlertRow(row, false)).severity?.toLowerCase(),
                ).toBe('warning');
            });

            await test.step('Clear fast: drop the CHECK + offending rows, then raise the threshold to clear', async () => {
                // Stop the source of new exceptions BEFORE clearing so nothing
                // re-triggers, then raise the alert threshold to clear it on the
                // next cleaner cycle (no 5-minute window drain).
                await spock
                    .execNode(
                        2,
                        `ALTER TABLE documents DROP CONSTRAINT IF EXISTS ${T3_CHECK_NAME}`,
                    )
                    .catch(() => undefined);
                await spock
                    .execNode(1, `DELETE FROM documents WHERE id = ${T3_EXC_ID}`)
                    .catch(() => undefined);

                await raiseAndAwaitClear(
                    [{ ruleId: excRule.id, alertId }],
                    node2ConnId,
                );
            });
        } finally {
            // Drop the local CHECK and remove the offending rows on every node so
            // nothing affects later inserts, even if a step above failed. DDL
            // does not replicate, so the DROP must run on each node directly.
            for (const n of [1, 2, 3] as const) {
                await spock
                    .execNode(
                        n,
                        `ALTER TABLE documents DROP CONSTRAINT IF EXISTS ${T3_CHECK_NAME}`,
                    )
                    .catch(() => undefined);
                await spock
                    .execNode(n, `DELETE FROM documents WHERE id = ${T3_EXC_ID}`)
                    .catch(() => undefined);
                await spock
                    .execNode(n, `DELETE FROM documents WHERE id = ${T6_UPDDEL_BASE}`)
                    .catch(() => undefined);
            }
        }
    });
});

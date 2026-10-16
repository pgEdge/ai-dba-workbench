/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Locator, expect } from '@playwright/test';
import { BasePage } from './BasePage';
import { AnalysisDialog, type RunQueryResult } from './AnalysisDialog';

/** Severity chip labels used to classify a row's chips. */
const SEVERITY_LABELS = ['critical', 'warning', 'info'];

/** Alert-type chip labels used to classify a row's chips. */
const TYPE_LABELS = ['Threshold', 'Anomaly'];

/**
 * Fields read from a single alert row (AlertItem) in the StatusPanel.
 */
export interface AlertRowFields {
    title: string;
    severity: string | null;
    type: string | null;
    server: string | null;
    database: string | null;
    falsePositive: boolean;
    ackedBy: string | null;
}

/**
 * Page object for the active/acknowledged alert list rendered in the
 * StatusPanel's AlertsSection. This is distinct from AlertManagementPage,
 * which drives the admin alert-*rules* table.
 *
 * Selector strategy (from `components/StatusPanel/AlertsSection.tsx`,
 * `AlertItem.tsx`, `AcknowledgeDialog.tsx`):
 * - Section headers carry the text "Active Alerts" (with a count Chip)
 *   and "Acknowledged" (collapsible, DEFAULT COLLAPSED). The empty active
 *   state reads "No active alerts".
 * - Each row (AlertItem) shows the friendly title, a severity chip
 *   (critical/warning/info), a type chip (Threshold/Anomaly), optional
 *   server/database/object chips, a "False Positive" chip when set, and an
 *   "Acked by <user>[: <msg>]" line on acknowledged rows.
 * - Row action IconButtons are found via their MUI icon test ids:
 *   Acknowledge = ancestor button of `[data-testid="CheckCircleOutlineIcon"]`,
 *   Restore = `[data-testid="UndoIcon"]`, Analyze = `[data-testid="PsychologyIcon"]`.
 * - The AcknowledgeDialog is a `role="dialog"` titled "Acknowledge alert"
 *   with a "Reason" field, a "Mark as false positive" toggle, and
 *   Acknowledge/Cancel buttons.
 * - Analyze opens the AlertAnalysisDialog (title "Alert analysis"), driven
 *   through the shared {@link AnalysisDialog} helper.
 */
export class StatusPanelAlertsPage extends BasePage {
    // ---------------------------------------------------------------
    // Section headers
    // ---------------------------------------------------------------

    private activeHeader(): Locator {
        return this.page
            .getByText('Active Alerts', { exact: true })
            .locator('xpath=ancestor::div[.//button][1]');
    }

    private acknowledgedHeader(): Locator {
        return this.page
            .getByText('Acknowledged', { exact: true })
            .locator('xpath=ancestor::div[.//button][1]');
    }

    /** Expand the Active Alerts section if it is collapsed. */
    async expandActive(): Promise<void> {
        const collapsed = await this.activeHeader()
            .locator('[data-testid="ExpandMoreIcon"]')
            .isVisible()
            .catch(() => false);
        if (collapsed) {
            await this.page.getByText('Active Alerts', { exact: true }).click();
            await expect(
                this.activeHeader().locator('[data-testid="ExpandLessIcon"]'),
            ).toBeVisible({ timeout: 5_000 });
        }
    }

    /**
     * Expand the Acknowledged section if it is collapsed. The section only
     * renders when at least one acknowledged alert exists and starts
     * collapsed.
     */
    async expandAcknowledged(): Promise<void> {
        const collapsed = await this.acknowledgedHeader()
            .locator('[data-testid="ExpandMoreIcon"]')
            .isVisible()
            .catch(() => false);
        if (collapsed) {
            await this.page.getByText('Acknowledged', { exact: true }).click();
            await expect(
                this.acknowledgedHeader().locator(
                    '[data-testid="ExpandLessIcon"]',
                ),
            ).toBeVisible({ timeout: 5_000 });
        }
    }

    /** The active-alert count shown in the section header chip. */
    async getActiveCount(): Promise<number> {
        const label = await this.activeHeader()
            .locator('.MuiChip-label')
            .first()
            .innerText();
        return Number.parseInt(label.trim(), 10);
    }

    /** True when the "No active alerts" empty state is shown. */
    async isActiveEmpty(): Promise<boolean> {
        return this.page
            .getByText('No active alerts')
            .isVisible()
            .catch(() => false);
    }

    // ---------------------------------------------------------------
    // Rows
    // ---------------------------------------------------------------

    /**
     * Locate an active alert row. An AlertItem is the innermost `div` that
     * carries BOTH the Acknowledge action (CheckCircleOutlineIcon) and a
     * chip — the ack button lives in a `button`, and chips live in sibling
     * boxes, so the smallest common ancestor is the row container itself.
     *
     * We anchor on the action icon rather than the title because the row
     * renders a *friendly-cased* title (via getFriendlyTitle), so matching
     * the raw rule/metric name never resolves. `titleSubstring` is an
     * OPTIONAL case-insensitive narrowing filter (empty = the last active
     * row, which is the single row these tests generate).
     *
     * NOTE: when several alerts share a title and severity they collapse
     * into a GroupedAlertItem with a different structure; expand it first.
     */
    getActiveAlertRow(titleSubstring: string = ''): Locator {
        return this.alertRow('[data-testid="CheckCircleOutlineIcon"]', titleSubstring);
    }

    /**
     * Locate an acknowledged alert row. An acknowledged row carries the
     * Restore (Undo) action instead of the Acknowledge action.
     */
    getAcknowledgedAlertRow(titleSubstring: string = ''): Locator {
        return this.alertRow('[data-testid="UndoIcon"]', titleSubstring);
    }

    /**
     * Shared row locator: the innermost `div` holding both the given action
     * icon and a chip, optionally narrowed by visible text. `.last()` in
     * document order is the last (deepest) matching AlertItem, never one of
     * its ancestors — so it always resolves to a single row.
     */
    private alertRow(actionIconSelector: string, titleSubstring: string): Locator {
        let rows = this.page
            .locator('div')
            .filter({ has: this.page.locator(actionIconSelector) })
            .filter({ has: this.page.locator('.MuiChip-root') });
        if (titleSubstring) {
            rows = rows.filter({ hasText: titleSubstring });
        }
        return rows.last();
    }

    /**
     * Read the fields of an alert row.
     *
     * `showServer` disambiguates the plain-text chips: when true the first
     * non-classified chip is the server and the second is the database;
     * when false the first is the database. This mirrors AlertItem, which
     * only renders the server chip when `showServer` is set (cluster/estate
     * scope). The chips carry no test ids, so this classification is
     * heuristic — verify against the live DOM when a row also carries an
     * object chip.
     */
    async readAlertRow(
        row: Locator,
        showServer: boolean = false,
    ): Promise<AlertRowFields> {
        const chipLabels = await row
            .locator('.MuiChip-label')
            .allTextContents();
        const chips = chipLabels.map((c) => c.trim());

        const severity =
            chips.find((c) => SEVERITY_LABELS.includes(c)) ?? null;
        const type = chips.find((c) => TYPE_LABELS.includes(c)) ?? null;
        const falsePositive = chips.includes('False Positive');

        const remaining = chips.filter(
            (c) =>
                !SEVERITY_LABELS.includes(c) &&
                !TYPE_LABELS.includes(c) &&
                c !== 'False Positive',
        );
        let server: string | null = null;
        let database: string | null = null;
        if (showServer) {
            server = remaining[0] ?? null;
            database = remaining[1] ?? null;
        } else {
            database = remaining[0] ?? null;
        }

        const rawText = await row.innerText();
        const title = (rawText.split('\n')[0] ?? '').trim();

        let ackedBy: string | null = null;
        const ackLine = rawText
            .split('\n')
            .find((line) => line.trim().startsWith('Acked by '));
        if (ackLine) {
            ackedBy = ackLine.trim().replace(/^Acked by\s+/, '');
        }

        return { title, severity, type, server, database, falsePositive, ackedBy };
    }

    // ---------------------------------------------------------------
    // Row actions
    // ---------------------------------------------------------------

    /**
     * Acknowledge an active alert row. Opens the AcknowledgeDialog, fills
     * the optional reason, toggles "Mark as false positive" when
     * requested, and confirms.
     */
    async acknowledge(
        row: Locator,
        options: { reason?: string; falsePositive?: boolean } = {},
    ): Promise<void> {
        await row
            .locator('[data-testid="CheckCircleOutlineIcon"]')
            .locator('xpath=ancestor::button[1]')
            .click();

        const dialog = this.innerDialog;
        await expect(dialog).toBeVisible({ timeout: 5_000 });

        if (options.reason !== undefined) {
            await dialog.getByLabel('Reason').fill(options.reason);
        }
        if (options.falsePositive) {
            await dialog.getByText('Mark as false positive').click();
        }

        await dialog
            .getByRole('button', { name: 'Acknowledge', exact: true })
            .click();
        await this.waitForDialogToClose();
    }

    /** Restore an acknowledged alert row to active. */
    async restore(row: Locator): Promise<void> {
        await row
            .locator('[data-testid="UndoIcon"]')
            .locator('xpath=ancestor::button[1]')
            .click();
    }

    // ---------------------------------------------------------------
    // AI analysis
    // ---------------------------------------------------------------

    /** The shared analysis-dialog helper scoped to "Alert analysis". */
    analysisDialog(): AnalysisDialog {
        return new AnalysisDialog(this.page, 'Alert analysis');
    }

    /**
     * Open the AI analysis for an alert row and return the analysis-dialog
     * helper. Waits for the dialog to appear.
     */
    async openAnalyze(row: Locator): Promise<AnalysisDialog> {
        await row
            .locator('[data-testid="PsychologyIcon"]')
            .locator('xpath=ancestor::button[1]')
            .click();
        const dialog = this.analysisDialog();
        await dialog.waitForOpen();
        return dialog;
    }

    /** Wait until the open Alert analysis has finished generating. */
    async waitForAnalysisReady(timeout: number = 120_000): Promise<void> {
        await this.analysisDialog().waitForReady(timeout);
    }

    /** The rendered Alert analysis markdown text. */
    async analysisText(): Promise<string> {
        return this.analysisDialog().text();
    }

    /**
     * Run the first generated SQL code block in the open Alert analysis,
     * handling any write-confirmation, and report the outcome.
     */
    async runFirstGeneratedQuery(
        timeout: number = 30_000,
    ): Promise<RunQueryResult> {
        return this.analysisDialog().runFirstGeneratedQuery(timeout);
    }
}

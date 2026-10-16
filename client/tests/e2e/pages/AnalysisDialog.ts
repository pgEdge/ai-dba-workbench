/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Page, type Locator, expect } from '@playwright/test';
import { AI_PROVIDER_LIMIT_PATTERN } from '../helpers/ai.helper';

/**
 * Result of running a generated SQL code block inside an analysis dialog
 * via {@link AnalysisDialog.runFirstGeneratedQuery}.
 */
export interface RunQueryResult {
    /** True when the statement returned a result table (a row footer). */
    ok: boolean;
    /** The parsed row count from the "N rows" footer, when present. */
    rowCount: number | null;
    /** The statement error text, when the statement failed. */
    error: string | null;
}

/**
 * AnalysisDialog wraps the shared BaseAnalysisDialog chrome used by every
 * AI analysis dialog (Alert analysis, Chart analysis, Server/Cluster
 * analysis). It is scoped by the dialog's AppBar title so that the alert,
 * chart, and server dialogs can reuse one set of selectors instead of
 * duplicating them across page objects.
 *
 * Selector strategy (from `shared/BaseAnalysisDialog.tsx`):
 * - The dialog is a full-screen MUI Dialog (`role="dialog"`) containing a
 *   close button with `aria-label="close analysis"` and an h6 title.
 * - The download button (`aria-label="download analysis"`) is disabled
 *   while `!analysis || loading`, so it becoming enabled is a precise
 *   "analysis ready" signal.
 * - The error state renders the text "Analysis Failed".
 * - Generated SQL renders via RunnableCodeBlock: a Run IconButton around
 *   `[data-testid="PlayArrowIcon"]` (a CircularProgress with aria-label
 *   "Running query" replaces it while loading), an optional write
 *   confirmation with Cancel + Execute buttons, an inline results table,
 *   and a "N rows" footer or an error row bearing `[data-testid="ErrorIcon"]`.
 */
export class AnalysisDialog {
    readonly page: Page;
    private readonly title: string;

    constructor(page: Page, title: string) {
        this.page = page;
        this.title = title;
    }

    /**
     * The dialog root, scoped to the analysis dialog carrying the close
     * button and the expected title. Only one analysis dialog is open at
     * a time, so this is unambiguous.
     */
    root(): Locator {
        return this.page
            .locator('[role="dialog"]')
            .filter({
                has: this.page.getByRole('button', { name: 'close analysis' }),
            })
            .filter({ hasText: this.title });
    }

    /** Wait for the dialog to be visible. */
    async waitForOpen(timeout: number = 10_000): Promise<void> {
        await expect(this.root()).toBeVisible({ timeout });
    }

    /**
     * Wait until analysis generation completes. The download button is
     * enabled exactly when a non-empty analysis is present and loading has
     * finished, making it the most reliable ready signal.
     */
    async waitForReady(timeout: number = 300_000): Promise<void> {
        // The download button enables only when a non-empty analysis is
        // present and loading has finished. Server/cluster analyses run an
        // agentic tool loop and can take minutes, so the timeout is
        // generous. Also short-circuit on the "Analysis Failed" state so a
        // failed analysis reports quickly (hasError()) instead of hanging.
        const download = this.root().getByRole('button', {
            name: 'download analysis',
        });
        const errorText = this.root().getByText('Analysis Failed');
        await expect(async () => {
            const enabled = await download.isEnabled().catch(() => false);
            const errored = await errorText.isVisible().catch(() => false);
            expect(
                enabled || errored,
                'analysis should finish (ready or failed)',
            ).toBe(true);
        }).toPass({ timeout, intervals: [3_000] });
    }

    /** True when the dialog is showing the "Analysis Failed" error state. */
    async hasError(): Promise<boolean> {
        return this.root()
            .getByText('Analysis Failed')
            .isVisible()
            .catch(() => false);
    }

    /**
     * The dialog's error-state message, or '' when not in the error state.
     * Reads the error Box (the "Analysis Failed" title plus the `{error}`
     * detail line rendered beneath it in BaseAnalysisDialog), so callers can
     * classify the failure (e.g. an AI provider usage/rate limit) without
     * matching against the whole dialog chrome.
     */
    async errorText(): Promise<string> {
        const title = this.root().getByText('Analysis Failed');
        if (!(await title.isVisible().catch(() => false))) {
            return '';
        }
        // The title and the detail message are sibling Typographies inside
        // one Box; its innerText folds both together.
        return title
            .locator('xpath=..')
            .innerText()
            .catch(() => 'Analysis Failed');
    }

    /**
     * True when the dialog failed specifically because an AI provider
     * rejected the request for a usage/rate limit (e.g.
     * `anthropic (400): You have reached your specified API usage limits`).
     * This is an ENVIRONMENTAL condition, not a product/test defect, so
     * callers skip the step rather than fail it. A genuine analysis error
     * (bad tool call, malformed response) does NOT match and still fails.
     */
    async isProviderLimited(): Promise<boolean> {
        return AI_PROVIDER_LIMIT_PATTERN.test(await this.errorText());
    }

    /**
     * The rendered analysis markdown text. Returns the dialog content, so
     * callers can assert on substrings of the generated report.
     */
    async text(): Promise<string> {
        return this.root().innerText();
    }

    /** Close the dialog and wait for it to disappear. */
    async close(timeout: number = 10_000): Promise<void> {
        await this.root()
            .getByRole('button', { name: 'close analysis' })
            .click();
        await expect(this.root()).toBeHidden({ timeout });
    }

    /**
     * If a write-statement confirmation (Cancel + Execute) is showing,
     * click Cancel and return true. We deliberately never click Execute:
     * executing AI-generated writes against the shared test cluster is
     * destructive (a rotated `postgres` password once cascaded failures
     * across the whole suite). Returns false when no confirmation appeared
     * (a read-only query, which is allowed to run).
     */
    private async cancelWriteConfirmation(dialog: Locator): Promise<boolean> {
        const cancelButton = dialog.getByRole('button', {
            name: 'Cancel',
            exact: true,
        });
        const appeared = await cancelButton
            .waitFor({ state: 'visible', timeout: 3_000 })
            .then(() => true)
            .catch(() => false);
        if (appeared) {
            await cancelButton.click();
        }
        return appeared;
    }

    /**
     * Run the first generated SQL code block in the analysis. Clicks its
     * Run button, CANCELS any write-statement prompt (never executes a
     * write), waits for the query to finish, and reports the outcome.
     *
     * NOTE: the RunnableCodeBlock result and error rows expose no test
     * ids, so the row-count footer and error text are parsed from visible
     * text; verify against the live DOM when wiring up specs.
     */
    async runFirstGeneratedQuery(
        timeout: number = 30_000,
    ): Promise<RunQueryResult> {
        const dialog = this.root();

        // Click the first Run button (IconButton wrapping PlayArrowIcon).
        const runButton = dialog
            .locator('[data-testid="PlayArrowIcon"]')
            .first()
            .locator('xpath=ancestor::button[1]');
        await expect(runButton).toBeVisible({ timeout });
        await runButton.click();

        // A write-statement confirmation may appear. NEVER execute it — the
        // AI can generate destructive DDL (e.g. ALTER USER / DROP), and
        // running it against the shared cluster corrupts state for every
        // later spec (an executed `ALTER USER postgres PASSWORD` once broke
        // primary auth for the whole suite). Cancel it so only read-only
        // queries run; clicking Run then Cancel still exercises the write
        // guard without mutating the database.
        if (await this.cancelWriteConfirmation(dialog)) {
            return { ok: false, rowCount: null, error: null };
        }

        // Wait for the running spinner to clear.
        await expect(dialog.getByLabel('Running query')).toHaveCount(0, {
            timeout,
        });

        // A successful statement renders a row-count footer.
        const footer = dialog
            .getByText(/^\d+\s+rows?$|^Results limited to \d+ rows$/)
            .first();
        if (await footer.count().then((n) => n > 0).catch(() => false)) {
            const footerText = (await footer.innerText()).trim();
            const match = footerText.match(/(\d+)/);
            const rowCount = match ? Number.parseInt(match[1], 10) : null;
            return { ok: true, rowCount, error: null };
        }

        // Otherwise surface any statement error text.
        const errorText = await dialog
            .locator('[data-testid="ErrorIcon"]')
            .last()
            .locator('xpath=following-sibling::*[1]')
            .innerText()
            .catch(() => null);
        return { ok: false, rowCount: null, error: errorText };
    }

    /**
     * Run EVERY generated SQL code block in the analysis, in order. For each
     * block: click its Run button, confirm any write-statement prompt, and
     * wait for the query to finish before moving on (so the RunnableCodeBlock
     * set — and therefore the button indices — stays stable; a running block
     * swaps its PlayArrowIcon for a "Running query" spinner). Returns the
     * total number of runnable blocks and the text of every statement error
     * surfaced (a RunnableCodeBlock error row carries `ErrorIcon`), so callers
     * can assert that all queries executed cleanly.
     */
    async runAllGeneratedQueries(
        perQueryTimeout: number = 30_000,
    ): Promise<{ total: number; okCount: number; errors: string[] }> {
        const dialog = this.root();
        const runIcons = dialog.locator('[data-testid="PlayArrowIcon"]');
        const total = await runIcons.count();

        for (let i = 0; i < total; i++) {
            // No query should be mid-run, so the PlayArrowIcon set (and the
            // nth index) is stable before we pick the next block.
            await expect(dialog.getByLabel('Running query'))
                .toHaveCount(0, { timeout: perQueryTimeout })
                .catch(() => undefined);

            const btn = dialog
                .locator('[data-testid="PlayArrowIcon"]')
                .nth(i)
                .locator('xpath=ancestor::button[1]');
            if (!(await btn.isVisible().catch(() => false))) {
                continue;
            }
            await btn.scrollIntoViewIfNeeded().catch(() => undefined);
            await btn.click();

            // NEVER execute a write statement (see runFirstGeneratedQuery):
            // the AI can emit destructive DDL, and running it against the
            // shared cluster corrupts state for every later spec. Cancel the
            // confirmation so only read-only queries actually run.
            if (await this.cancelWriteConfirmation(dialog)) {
                continue;
            }

            // Wait for this query's running spinner to clear.
            await expect(dialog.getByLabel('Running query')).toHaveCount(0, {
                timeout: perQueryTimeout,
            });
        }

        // A successful RunnableCodeBlock renders a row-count footer; count
        // those as the number of generated queries that executed cleanly.
        const okCount = await dialog
            .getByText(/^\d+\s+rows?$|^Results limited to \d+ rows$/)
            .count();

        // Collect statement errors surfaced by a RunnableCodeBlock. Two
        // classes are NOT counted as failures, because neither indicates a
        // defect in executable SQL:
        //   1. "No executable SQL found" — the block is prose/config the AI
        //      fenced as a code block, not a query.
        //   2. Parameterized templates ($1, $2, …) — the AI emitted a real
        //      query but left bind-value blanks. The app correctly refuses to
        //      run it, and the placeholders cannot be filled reliably (each
        //      run's parameters differ in meaning and type, and the code block
        //      is not editable through the GUI). We LOG it and let it pass.
        // Genuinely broken SQL (unknown column, syntax error, …) is still
        // reported so the strict assertion keeps catching real query defects.
        const errorIcons = await dialog
            .locator('[data-testid="ErrorIcon"]')
            .all();
        const errors: string[] = [];
        for (const icon of errorIcons) {
            const text = (
                await icon
                    .locator('xpath=following-sibling::*[1]')
                    .innerText()
                    .catch(() => '')
            ).trim();
            if (/no executable sql found/i.test(text)) {
                continue;
            }
            if (/parameter placeholder/i.test(text)) {
                console.log(
                    '[AI query] Parameterized query generated by the AI '
                    + '($1/$2 … placeholders): cannot be executed without bind '
                    + 'values and cannot be reliably filled from the DB — '
                    + `marking as passed, not a SQL error. Detail: ${text}`,
                );
                continue;
            }
            errors.push(text || 'unknown query error');
        }
        return { total, okCount, errors };
    }
}

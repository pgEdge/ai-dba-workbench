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
import { AnalysisDialog } from './AnalysisDialog';

/** Escape a string for safe embedding in a RegExp. */
const escapeRegExp = (value: string): string =>
    value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Parse an integer that may carry locale thousands separators. */
const parseLocaleInt = (text: string): number =>
    Number.parseInt(text.replace(/,/g, '').trim(), 10);

/**
 * Page object for the monitoring dashboard displayed in the
 * StatusPanel when a server is selected. Encapsulates interactions
 * with CollapsibleSections (Monitoring, System Resources, Top
 * Queries, etc.), the AI Overview panel, the "Hide monitoring
 * queries" toggle, and the Server Info dialog.
 *
 * Selector strategy:
 * - CollapsibleSection headers: `role="button"` with
 *   `aria-label="{Expand|Collapse} {title} section"`
 * - AI Overview toggle: `aria-label="Expand AI Overview"` or
 *   `aria-label="Collapse AI Overview"`
 * - Top Queries loading: `aria-label="Loading queries"`
 * - Hide monitoring switch: FormControlLabel "Hide monitoring queries"
 * - Server info button: Tooltip "Server details" wrapping an
 *   IconButton containing InfoOutlinedIcon
 * - Server Info dialog close: `aria-label="close server info"`
 */
export class MonitoringPage extends BasePage {
    // ---------------------------------------------------------------
    // CollapsibleSection helpers
    // ---------------------------------------------------------------

    /**
     * Returns the CollapsibleSection header button for a given title.
     * The regex matches both "Expand {title} section" and
     * "Collapse {title} section" aria-labels.
     */
    getSectionHeader(title: string): Locator {
        return this.page.getByRole('button', {
            name: new RegExp(`${title} section`, 'i'),
        });
    }

    /**
     * All chart series legend toggle buttons currently rendered (the
     * accessible DOM legend that replaces the ECharts canvas legend on the
     * monitoring charts). Each is a `<button>` with a
     * `data-testid="legend-toggle-<chartKey>-<series>"` and an
     * `aria-pressed` attribute reflecting whether the series is shown.
     */
    legendToggles(): Locator {
        return this.page.locator('[data-testid^="legend-toggle-"]');
    }

    /** A single series legend toggle by its `data-testid`. */
    legendToggle(testId: string): Locator {
        return this.page.getByTestId(testId);
    }

    /**
     * Returns true if the named CollapsibleSection is currently
     * expanded (aria-expanded="true").
     */
    async isSectionExpanded(title: string): Promise<boolean> {
        const header = this.getSectionHeader(title);
        const value = await header.getAttribute('aria-expanded');
        return value === 'true';
    }

    /**
     * Expands a CollapsibleSection if it is currently collapsed.
     * No-op if the section is already expanded.
     */
    async expandSection(title: string): Promise<void> {
        const expanded = await this.isSectionExpanded(title);
        if (!expanded) {
            await this.getSectionHeader(title).click();
            await expect(
                this.getSectionHeader(title),
                `Section "${title}" should be expanded after click`,
            ).toHaveAttribute('aria-expanded', 'true', { timeout: 5_000 });
        }
    }

    /**
     * Collapses a CollapsibleSection if it is currently expanded.
     * No-op if the section is already collapsed.
     */
    async collapseSection(title: string): Promise<void> {
        const expanded = await this.isSectionExpanded(title);
        if (expanded) {
            await this.getSectionHeader(title).click();
            await expect(
                this.getSectionHeader(title),
                `Section "${title}" should be collapsed after click`,
            ).toHaveAttribute('aria-expanded', 'false', { timeout: 5_000 });
        }
    }

    /**
     * Asserts that the named section is expanded
     * (aria-expanded="true").
     */
    async expectSectionExpanded(title: string): Promise<void> {
        await expect(
            this.getSectionHeader(title),
            `Section "${title}" should be expanded`,
        ).toHaveAttribute('aria-expanded', 'true');
    }

    /**
     * Asserts that the named section is collapsed
     * (aria-expanded="false").
     */
    async expectSectionCollapsed(title: string): Promise<void> {
        await expect(
            this.getSectionHeader(title),
            `Section "${title}" should be collapsed`,
        ).toHaveAttribute('aria-expanded', 'false');
    }

    // ---------------------------------------------------------------
    // AI Overview helpers
    // ---------------------------------------------------------------

    /**
     * Returns the AI Overview expand/collapse toggle button.
     * Matches both "Expand AI Overview" and "Collapse AI Overview".
     */
    getAIOverviewToggle(): Locator {
        return this.page.getByRole('button', {
            name: /expand ai overview|collapse ai overview/i,
        });
    }

    /**
     * Returns true if the AI Overview panel is currently collapsed.
     */
    async isAIOverviewCollapsed(): Promise<boolean> {
        const label = await this.getAIOverviewToggle().getAttribute('aria-label');
        return /expand ai overview/i.test(label ?? '');
    }

    /**
     * Expands the AI Overview if it is currently collapsed.
     */
    async expandAIOverview(): Promise<void> {
        const collapsed = await this.isAIOverviewCollapsed();
        if (collapsed) {
            await this.getAIOverviewToggle().click();
            await expect(
                this.getAIOverviewToggle(),
                'AI Overview should be expanded after click',
            ).toHaveAttribute('aria-label', /collapse ai overview/i, {
                timeout: 5_000,
            });
        }
    }

    /**
     * Collapses the AI Overview if it is currently expanded.
     */
    async collapseAIOverview(): Promise<void> {
        const collapsed = await this.isAIOverviewCollapsed();
        if (!collapsed) {
            await this.getAIOverviewToggle().click();
            await expect(
                this.getAIOverviewToggle(),
                'AI Overview should be collapsed after click',
            ).toHaveAttribute('aria-label', /expand ai overview/i, {
                timeout: 5_000,
            });
        }
    }

    // ---------------------------------------------------------------
    // Top Queries helpers
    // ---------------------------------------------------------------

    /**
     * Waits until the Top Queries loading spinner disappears.
     */
    async waitForTopQueriesLoad(): Promise<void> {
        const spinner = this.page.getByLabel('Loading queries');
        // Only wait for hidden if the spinner is currently visible.
        if (await spinner.isVisible().catch(() => false)) {
            await spinner.waitFor({ state: 'hidden', timeout: 30_000 });
        }
    }

    /**
     * Returns the "Hide monitoring queries" checkbox input locator.
     * Use isChecked() on this locator to read state.
     */
    getHideMonitoringQueriesSwitch(): Locator {
        return this.page.getByLabel('Hide monitoring queries');
    }

    /**
     * Clicks the "Hide monitoring queries" switch in a cross-browser
     * safe way. The MUI Switch input is hidden (opacity: 0, absolute
     * position). Clicking it with force:true works in Chromium but
     * WebKit does not fire React's onChange for hidden inputs.
     *
     * Instead, click the visible .MuiFormControlLabel-label span. That
     * click bubbles up to the parent <label>, which the browser
     * translates into a click on the associated checkbox, triggering
     * React's onChange reliably in all browsers.
     */
    async clickHideMonitoringQueriesSwitch(): Promise<void> {
        await this.page
            .locator('.MuiFormControlLabel-label', {
                hasText: 'Hide monitoring queries',
            })
            .first()
            .click();
    }

    /**
     * Locate a Top Queries row by a substring of its query text. Rows
     * are rendered as `role="button"` with an aria-label of the form
     * "View details for query: {truncated query}", so a marker embedded
     * near the start of the query (e.g. a leading SQL block comment
     * containing a unique "e2e-marker-N" token) is matchable here.
     */
    getTopQueryRowByText(substring: string): Locator {
        return this.page.getByRole('button', {
            name: new RegExp(escapeRegExp(substring), 'i'),
        });
    }

    /**
     * Extract the six positional cell values from a Top Queries row in
     * column order: Database, Query, Calls, Total Time, Mean Time,
     * Rows. Numeric cells are parsed back to integers (the GUI formats
     * them with locale separators via formatNumber).
     */
    async readTopQueryRow(row: Locator): Promise<{
        database: string;
        query: string;
        calls: number;
        totalTime: string;
        meanTime: string;
        rows: number;
    }> {
        const cells = row.locator('.MuiTypography-root');
        const texts = await cells.allTextContents();
        return {
            database: (texts[0] ?? '').trim(),
            query: (texts[1] ?? '').trim(),
            calls: parseLocaleInt(texts[2] ?? ''),
            totalTime: (texts[3] ?? '').trim(),
            meanTime: (texts[4] ?? '').trim(),
            rows: parseLocaleInt(texts[5] ?? ''),
        };
    }

    // ---------------------------------------------------------------
    // System Resources KPI tile helpers
    // ---------------------------------------------------------------

    /**
     * Locate a KPI tile by its label. KpiTile always sets
     * aria-label="{label}: {value}[ unit]" on its Paper element,
     * regardless of whether the underlying system_stats data exists, so
     * getByLabel matches it whether or not the tile is clickable.
     */
    getKpiTile(label: string): Locator {
        return this.page.getByLabel(new RegExp(`^${escapeRegExp(label)}:`));
    }

    /**
     * Read a KPI tile's rendered value and unit from its aria-label.
     * Returns `value: null` when the tile shows the "--" placeholder
     * (no data). The numeric value is parsed from the formatted string
     * (locale separators stripped).
     */
    async readKpiTile(label: string): Promise<{
        raw: string;
        value: number | null;
        unit: string | null;
    }> {
        const aria =
            (await this.getKpiTile(label).getAttribute('aria-label')) ?? '';
        const rest = aria
            .replace(new RegExp(`^${escapeRegExp(label)}:\\s*`), '')
            .trim();

        if (rest === '' || rest.startsWith('--')) {
            return { raw: rest, value: null, unit: null };
        }

        // rest looks like "12.3 %", "0.42", or "1,234 MB".
        const match = rest.match(/^(-?[\d,]*\.?\d+)\s*(\S.*)?$/);
        if (!match) {
            return { raw: rest, value: null, unit: null };
        }
        const value = Number.parseFloat(match[1].replace(/,/g, ''));
        const unit = match[2] ? match[2].trim() : null;
        return {
            raw: rest,
            value: Number.isFinite(value) ? value : null,
            unit,
        };
    }

    // ---------------------------------------------------------------
    // Server Info Dialog helpers
    // ---------------------------------------------------------------

    /**
     * Opens the Server Info dialog by clicking the info icon button
     * inside the ServerInfoCard. The button is an MUI IconButton
     * wrapped in a Tooltip with title="Server details".
     */
    async openServerInfoDialog(): Promise<void> {
        // The InfoOutlinedIcon renders with data-testid="InfoOutlinedIcon".
        // Navigate up to its parent IconButton and click.
        const infoButton = this.page.locator(
            '[data-testid="InfoOutlinedIcon"]',
        ).locator('..');
        await expect(
            infoButton,
            'Server info button should be visible',
        ).toBeVisible({ timeout: 10_000 });
        await infoButton.click();
        await expect(
            this.getServerInfoDialog(),
            'Server Info dialog should open',
        ).toBeVisible({ timeout: 10_000 });
    }

    /**
     * Closes the Server Info dialog by clicking the close button.
     */
    async closeServerInfoDialog(): Promise<void> {
        await this.page
            .getByRole('button', { name: 'close server info' })
            .click();
        await expect(
            this.getServerInfoDialog(),
            'Server Info dialog should close',
        ).toBeHidden({ timeout: 10_000 });
    }

    /**
     * Returns a locator for the Server Info dialog (fullscreen
     * MUI Dialog).
     */
    getServerInfoDialog(): Locator {
        return this.page.locator('.MuiDialog-paperFullScreen');
    }

    // ---------------------------------------------------------------
    // Dashboard time-range selector
    // ---------------------------------------------------------------

    /**
     * Select a dashboard time range. Presets are 1h/6h/24h/7d/30d; pass
     * "custom" to open the custom-range popover. The buttons live in the
     * ToggleButtonGroup labelled "Time range selection", each named
     * "Select {label} time range" (or "Select custom time range").
     */
    async selectTimeRange(label: string): Promise<void> {
        const group = this.page.getByRole('group', {
            name: 'Time range selection',
        });
        const name =
            label.toLowerCase() === 'custom'
                ? 'Select custom time range'
                : `Select ${label} time range`;
        await group.getByRole('button', { name }).click();
    }

    /**
     * True when auto-refresh is paused (a custom range is active). The
     * indicator is a `role="img"` element whose accessible name mentions
     * the pause.
     */
    async isAutoRefreshPaused(): Promise<boolean> {
        return this.page
            .getByRole('img', { name: /auto-refresh is paused/i })
            .isVisible()
            .catch(() => false);
    }

    // ---------------------------------------------------------------
    // Top Queries controls
    // ---------------------------------------------------------------

    /**
     * True when the "Hide monitoring queries" switch is on. Reads the
     * checkbox state behind the FormControlLabel.
     */
    async isMonitoringQueriesHidden(): Promise<boolean> {
        return this.getHideMonitoringQueriesSwitch().isChecked();
    }

    /**
     * Select a database in the Top Queries "Database" filter (MUI Select
     * `#top-queries-database-filter`). Pass "All databases" to clear the
     * filter.
     */
    async selectTopQueriesDatabase(name: string): Promise<void> {
        await this.page
            .getByRole('combobox', { name: 'Database' })
            .click();
        await this.page
            .getByRole('option', { name, exact: true })
            .click();
        // Clear any lingering Select/Menu backdrop (WebKit CI).
        await this.page
            .locator('.MuiBackdrop-invisible')
            .waitFor({ state: 'hidden', timeout: 5_000 })
            .catch(() => {});
    }

    /** Advance the Top Queries table to the next page. */
    async nextTopQueriesPage(): Promise<void> {
        await this.page
            .getByRole('button', { name: 'Next page of queries' })
            .click();
    }

    /** Return the Top Queries table to the previous page. */
    async previousTopQueriesPage(): Promise<void> {
        await this.page
            .getByRole('button', { name: 'Previous page of queries' })
            .click();
    }

    /** Set the Top Queries page size (rows per page). */
    async setTopQueriesRowsPerPage(size: number): Promise<void> {
        await this.page
            .getByRole('button', { name: `Show ${size} rows per page` })
            .click();
    }

    // ---------------------------------------------------------------
    // AI analysis dialogs
    // ---------------------------------------------------------------

    /**
     * Open the per-metric AI analysis for a KPI tile. The brain
     * (Psychology) IconButton sits at the tile's top-right and opens the
     * ChartAnalysisDialog (title "Chart analysis"). Returns the shared
     * analysis-dialog helper.
     */
    async openMetricAnalysis(kpiLabel: string): Promise<AnalysisDialog> {
        await this.getKpiTile(kpiLabel)
            .locator('[data-testid="PsychologyIcon"]')
            .locator('xpath=ancestor::button[1]')
            .click();
        const dialog = new AnalysisDialog(this.page, 'Chart analysis');
        await dialog.waitForOpen();
        return dialog;
    }

    /**
     * Trigger the cluster/server "Run full analysis" from the AI Overview
     * panel. Opens the ServerAnalysisDialog (title "Server analysis" or
     * "Cluster analysis"). Pass `isCluster` to target the cluster title.
     */
    async openFullAnalysis(
        isCluster: boolean = false,
    ): Promise<AnalysisDialog> {
        await this.page
            .getByRole('button', { name: 'Run full analysis' })
            .click();
        const dialog = new AnalysisDialog(
            this.page,
            isCluster ? 'Cluster analysis' : 'Server analysis',
        );
        await dialog.waitForOpen();
        return dialog;
    }

    // ---------------------------------------------------------------
    // Selection status
    // ---------------------------------------------------------------

    /**
     * Read the SelectionHeader status. Hovers the selection icon to reveal
     * its tooltip ("{Type} is online/offline" or "N active alerts: ...")
     * and reads the alert-count badge overlaid on the icon.
     *
     * NOTE: the header icon and badge expose no test ids beyond the MUI
     * icon test id, so this hovers the first Storage/Dns/Language icon and
     * reads the tooltip. Verify against the live DOM if several such icons
     * are present.
     */
    async readSelectionStatus(): Promise<{
        status: string;
        alertBadge: string | null;
    }> {
        const icon = this.page
            .locator(
                '[data-testid="StorageIcon"], [data-testid="DnsIcon"], ' +
                '[data-testid="LanguageIcon"]',
            )
            .first();
        const iconBox = icon.locator('xpath=..');
        await icon.hover();
        const status = await this.page
            .getByRole('tooltip')
            .innerText()
            .catch(() => '');
        const badgeText = (await iconBox.innerText().catch(() => '')).trim();
        return {
            status: status.trim(),
            alertBadge: badgeText === '' ? null : badgeText,
        };
    }

    // ---------------------------------------------------------------
    // Chart panels (ECharts canvas — structure only, no point assertions)
    // ---------------------------------------------------------------

    /** The Paper panel that contains the given chart title. */
    private chartPanel(title: string): Locator {
        return this.page
            .locator('.MuiPaper-root')
            .filter({ hasText: title })
            .first();
    }

    /** True when a chart panel with the given title is visible. */
    async chartPanelVisible(title: string): Promise<boolean> {
        return this.chartPanel(title)
            .isVisible()
            .catch(() => false);
    }

    /**
     * The empty-state message shown in a chart panel (the panel text with
     * its title stripped). Empty when the panel has data or is loading.
     */
    async chartPanelEmptyMessage(title: string): Promise<string> {
        const text = await this.chartPanel(title)
            .innerText()
            .catch(() => '');
        return text.replace(title, '').trim();
    }

    /** True when a chart panel with the given title is loading. */
    async chartLoading(title: string): Promise<boolean> {
        return this.chartPanel(title)
            .getByLabel('Loading chart')
            .isVisible()
            .catch(() => false);
    }
}

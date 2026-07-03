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
}

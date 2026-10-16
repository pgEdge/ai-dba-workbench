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
 * A single event's fields read from the open EventDetailPanel.
 */
export interface OpenEventDetail {
    title: string;
    time: string;
    server?: string;
    summary?: string;
}

/**
 * Page object for the Event Timeline that renders inside the StatusPanel.
 *
 * Selector strategy (from `components/EventTimeline/*`):
 * - The header is a Box carrying the title text "Event Timeline", a
 *   count Chip, category filter Chips, a time-range ToggleButtonGroup, and
 *   an expand/collapse IconButton. Clicking the title toggles the body.
 * - Category chips are MUI Chips identified by their text: Config, HBA,
 *   Ident, Restart, Alert, Cleared, Acked, Extension, Blackouts.
 * - Time-range toggles are ToggleButtons named 1h/6h/24h/7d/30d; the
 *   custom toggle carries aria-label "Select custom time range".
 * - The empty state renders "No events in this time range".
 * - Event markers are clickable circles with no role or test id (see the
 *   NOTE on `countMarkers`/`openEventByTitle`). Clicking one opens the
 *   EventDetailPanel, whose header reads "Event Details" (single) or
 *   "N Events" (cluster) and closes via a CloseIcon button.
 */
export class EventTimelinePage extends BasePage {
    /** Category filter chip labels, in render order. */
    static readonly CATEGORY_LABELS = [
        'Config',
        'HBA',
        'Ident',
        'Restart',
        'Alert',
        'Cleared',
        'Acked',
        'Extension',
        'Blackouts',
    ];

    // ---------------------------------------------------------------
    // Structural locators
    // ---------------------------------------------------------------

    /** The "Event Timeline" title text (also the expand/collapse target). */
    private titleText(): Locator {
        return this.page.getByText('Event Timeline', { exact: true });
    }

    /**
     * The timeline header container: the nearest ancestor div of the
     * title that also holds the header's buttons (toggles, expander).
     */
    private header(): Locator {
        return this.titleText().locator('xpath=ancestor::div[.//button][1]');
    }

    /** The Collapse body that holds the canvas, empty state, or skeleton. */
    private body(): Locator {
        return this.header().locator(
            'xpath=following-sibling::div[contains(@class,"MuiCollapse-root")][1]',
        );
    }

    // ---------------------------------------------------------------
    // Expand / collapse
    // ---------------------------------------------------------------

    /**
     * Expand the timeline if it is currently collapsed. The header shows
     * an ExpandMore icon when collapsed and ExpandLess when expanded.
     */
    async expandTimeline(): Promise<void> {
        const collapsed = await this.header()
            .locator('[data-testid="ExpandMoreIcon"]')
            .isVisible()
            .catch(() => false);
        if (collapsed) {
            await this.titleText().click();
            await expect(
                this.header().locator('[data-testid="ExpandLessIcon"]'),
                'Timeline should be expanded after clicking the title',
            ).toBeVisible({ timeout: 5_000 });
        }
    }

    // ---------------------------------------------------------------
    // Filters
    // ---------------------------------------------------------------

    /**
     * Toggle a category filter chip by its visible label (e.g. "Alert",
     * "Config"). Filtering triggers a re-fetch, so callers should await
     * their own follow-up assertions.
     */
    async selectCategory(label: string): Promise<void> {
        await this.header().getByText(label, { exact: true }).click();
    }

    /**
     * Select a preset time range by its label: 1h, 6h, 24h, 7d, or 30d.
     */
    async selectTimeRange(label: string): Promise<void> {
        await this.header()
            .getByRole('button', { name: label, exact: true })
            .click();
    }

    /** Open the custom time-range popover. */
    async openCustomRange(): Promise<void> {
        await this.header()
            .getByRole('button', { name: 'Select custom time range' })
            .click();
    }

    // ---------------------------------------------------------------
    // State reads
    // ---------------------------------------------------------------

    /** True when the "No events in this time range" empty state shows. */
    async isEmpty(): Promise<boolean> {
        return this.page
            .getByText('No events in this time range')
            .isVisible()
            .catch(() => false);
    }

    /**
     * Best-effort count of event markers currently on the track.
     *
     * NOTE: markers expose no role, test id, or text, so this counts the
     * SvgIcons inside the timeline body. It assumes the detail panel is
     * closed (an open panel adds icons) and returns 0 for the empty state.
     * Verify against the live DOM before relying on exact counts.
     */
    async countMarkers(): Promise<number> {
        if (await this.isEmpty()) {
            return 0;
        }
        return this.body().locator('.MuiSvgIcon-root').count();
    }

    /**
     * Best-effort: open the marker whose tooltip title contains the given
     * substring by hovering each marker until a match is found.
     *
     * NOTE: markers expose no stable selector, so this hovers SvgIcons in
     * the timeline body and reads the resulting tooltip. Verify against
     * the live DOM before relying on it.
     */
    async openEventByTitle(substring: string): Promise<void> {
        const markers = this.body().locator('.MuiSvgIcon-root');
        const count = await markers.count();
        for (let i = 0; i < count; i++) {
            const marker = markers.nth(i);
            await marker.hover();
            const tooltip = this.page.getByRole('tooltip');
            const text = await tooltip.innerText().catch(() => '');
            if (text.includes(substring)) {
                await marker.click();
                await expect(
                    this.page.getByText(/^Event Details$|Events$/).first(),
                ).toBeVisible({ timeout: 5_000 });
                return;
            }
        }
        throw new Error(
            `No timeline marker found with a tooltip containing "${substring}"`,
        );
    }

    /**
     * Read the fields of the event shown in the open (single-event)
     * EventDetailPanel.
     *
     * NOTE: the panel's fields expose no test ids, so they are parsed
     * positionally from the panel's visible text (title, time · server,
     * summary). Verify against the live DOM before relying on it.
     */
    async readOpenEventDetail(): Promise<OpenEventDetail> {
        const panel = this.page
            .getByText('Event Details', { exact: true })
            .locator('xpath=ancestor::div[.//*[@data-testid="CloseIcon"]][1]');
        const raw = await panel.innerText();
        const lines = raw
            .split('\n')
            .map((line) => line.trim())
            .filter((line) => line.length > 0);
        // lines[0] === 'Event Details'
        const title = lines[1] ?? '';
        const timeLine = lines[2] ?? '';
        const [timePart, serverPart] = timeLine
            .split('·')
            .map((part) => part.trim());
        const summary = lines[3];
        return {
            title,
            time: timePart ?? '',
            server: serverPart || undefined,
            summary,
        };
    }

    /** Close the open EventDetailPanel via its CloseIcon button. */
    async closeEventDetail(): Promise<void> {
        const panel = this.page
            .getByText(/^Event Details$|Events$/)
            .first()
            .locator('xpath=ancestor::div[.//*[@data-testid="CloseIcon"]][1]');
        await panel.locator('[data-testid="CloseIcon"]').click();
    }
}

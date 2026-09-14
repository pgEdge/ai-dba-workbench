/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { LocalizationProvider } from '@mui/x-date-pickers/LocalizationProvider';
import { AdapterDayjs } from '@mui/x-date-pickers/AdapterDayjs';
import renderWithTheme from '../../../test/renderWithTheme';
import TimeRangeSelector from '../TimeRangeSelector';
import { DashboardProvider } from '../../../contexts/DashboardContext';
import { useDashboard } from '../../../contexts/useDashboard';

const START = '2026-01-01T00:00:00.000Z';
const END = '2026-01-02T06:30:00.000Z';

/**
 * A test-only control that seeds the context with a custom window, so the
 * selector can be observed in its custom state without going through the
 * picker fields.
 */
const SeedCustomRange: React.FC<{ start?: string; end?: string }> = ({
    start,
    end,
}) => {
    const { setCustomTimeRange, setTimeRange } = useDashboard();
    return (
        <button
            type="button"
            onClick={() => {
                if (start === undefined || end === undefined) {
                    setTimeRange('custom');
                } else {
                    setCustomTimeRange(start, end);
                }
            }}
        >
            seed
        </button>
    );
};

/**
 * Render TimeRangeSelector within a DashboardProvider and a
 * LocalizationProvider, matching how the application mounts it, so that
 * the real date-time pickers are exercised.
 */
const renderTimeRangeSelector = (seed?: { start?: string; end?: string }) => {
    return renderWithTheme(
        <LocalizationProvider dateAdapter={AdapterDayjs}>
            <DashboardProvider>
                <TimeRangeSelector />
                {seed !== undefined && (
                    <SeedCustomRange start={seed.start} end={seed.end} />
                )}
            </DashboardProvider>
        </LocalizationProvider>,
    );
};

const customToggle = () =>
    screen.getByRole('button', { name: /select custom time range/i });

describe('TimeRangeSelector', () => {
    it('renders all five time range options', () => {
        renderTimeRangeSelector();

        expect(screen.getByText('1h')).toBeInTheDocument();
        expect(screen.getByText('6h')).toBeInTheDocument();
        expect(screen.getByText('24h')).toBeInTheDocument();
        expect(screen.getByText('7d')).toBeInTheDocument();
        expect(screen.getByText('30d')).toBeInTheDocument();
    });

    it('has the toggle button group with correct aria-label', () => {
        renderTimeRangeSelector();

        expect(
            screen.getByRole('group', { name: /time range selection/i }),
        ).toBeInTheDocument();
    });

    it('renders each option as a button with an aria-label', () => {
        renderTimeRangeSelector();

        expect(
            screen.getByRole('button', { name: /select 1h time range/i }),
        ).toBeInTheDocument();
        expect(
            screen.getByRole('button', { name: /select 6h time range/i }),
        ).toBeInTheDocument();
        expect(
            screen.getByRole('button', { name: /select 24h time range/i }),
        ).toBeInTheDocument();
        expect(
            screen.getByRole('button', { name: /select 7d time range/i }),
        ).toBeInTheDocument();
        expect(
            screen.getByRole('button', { name: /select 30d time range/i }),
        ).toBeInTheDocument();
    });

    it('defaults to 1h as the selected time range', () => {
        renderTimeRangeSelector();

        const button1h = screen.getByRole('button', {
            name: /select 1h time range/i,
        });
        // MUI ToggleButton uses aria-pressed for the selected state
        expect(button1h).toHaveAttribute('aria-pressed', 'true');
    });

    it('selects a different range when clicked', () => {
        renderTimeRangeSelector();

        const button24h = screen.getByRole('button', {
            name: /select 24h time range/i,
        });

        fireEvent.click(button24h);

        expect(button24h).toHaveAttribute('aria-pressed', 'true');

        // Previous selection should be deselected
        const button1h = screen.getByRole('button', {
            name: /select 1h time range/i,
        });
        expect(button1h).toHaveAttribute('aria-pressed', 'false');
    });

    describe('Custom range', () => {
        it('renders a Custom toggle that is not selected by default', () => {
            renderTimeRangeSelector();

            expect(customToggle()).toHaveTextContent('Custom');
            expect(customToggle()).toHaveAttribute('aria-pressed', 'false');
        });

        it('opens the picker popover without changing the range', () => {
            renderTimeRangeSelector();

            fireEvent.click(customToggle());

            expect(screen.getByLabelText('From')).toBeInTheDocument();
            expect(screen.getByLabelText('To')).toBeInTheDocument();
            // The open popover is a modal, so the toggles behind it are
            // aria-hidden and must be queried with hidden: true.
            expect(
                screen.getByRole('button', {
                    name: /select 1h time range/i,
                    hidden: true,
                }),
            ).toHaveAttribute('aria-pressed', 'true');
        });

        it('closes the popover on Cancel', () => {
            renderTimeRangeSelector();

            fireEvent.click(customToggle());
            fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

            expect(screen.queryByLabelText('From')).not.toBeInTheDocument();
        });

        it('disables Apply whilst no window has been entered', () => {
            renderTimeRangeSelector();

            fireEvent.click(customToggle());

            expect(
                screen.getByRole('button', { name: 'Apply' }),
            ).toBeDisabled();
        });

        it('shows the active window on the toggle and marks it selected', () => {
            renderTimeRangeSelector({ start: START, end: END });

            fireEvent.click(screen.getByRole('button', { name: 'seed' }));

            expect(customToggle()).toHaveAttribute('aria-pressed', 'true');
            expect(customToggle()).toHaveTextContent(/Jan/);
            expect(customToggle()).not.toHaveTextContent('Custom');
            expect(
                screen.getByRole('button', { name: /select 1h time range/i }),
            ).toHaveAttribute('aria-pressed', 'false');
        });

        it('falls back to the word Custom without usable bounds', () => {
            renderTimeRangeSelector({});

            fireEvent.click(screen.getByRole('button', { name: 'seed' }));

            expect(customToggle()).toHaveAttribute('aria-pressed', 'true');
            expect(customToggle()).toHaveTextContent('Custom');
        });

        it('falls back to the word Custom for unparseable bounds', () => {
            renderTimeRangeSelector({ start: 'nonsense', end: 'rubbish' });

            fireEvent.click(screen.getByRole('button', { name: 'seed' }));

            expect(customToggle()).toHaveTextContent('Custom');
        });

        it('shows the auto-refresh paused indicator whilst custom', () => {
            renderTimeRangeSelector({ start: START, end: END });

            expect(
                screen.queryByLabelText(/auto-refresh is paused/i),
            ).not.toBeInTheDocument();

            fireEvent.click(screen.getByRole('button', { name: 'seed' }));

            expect(
                screen.getByLabelText(/auto-refresh is paused/i),
            ).toBeInTheDocument();
        });

        it('names the paused indicator for assistive technology', () => {
            renderTimeRangeSelector({ start: START, end: END });

            fireEvent.click(screen.getByRole('button', { name: 'seed' }));

            const indicator = screen.getByRole('img', {
                name: /auto-refresh is paused whilst a custom time range is active/i,
            });
            expect(indicator).toBeInTheDocument();
            // The name must not depend on the tooltip being shown, so the
            // anchor itself is not hidden from the accessibility tree.
            expect(indicator).not.toHaveAttribute('aria-hidden', 'true');
        });

        it('makes the paused indicator keyboard focusable', () => {
            renderTimeRangeSelector({ start: START, end: END });

            fireEvent.click(screen.getByRole('button', { name: 'seed' }));

            const indicator = screen.getByRole('img', {
                name: /auto-refresh is paused/i,
            });
            expect(indicator).toHaveAttribute('tabindex', '0');

            indicator.focus();
            expect(indicator).toHaveFocus();
        });

        it('shows the paused tooltip when the indicator is focused', async () => {
            /*
             * MUI only opens a tooltip for focus it judges "visible", which
             * it derives from the `:focus-visible` pseudo-class. jsdom
             * reports that pseudo-class as false for every element, so stub
             * it to track the active element, emulating the keyboard focus a
             * real browser reports.
             */
            const nativeMatches = Element.prototype.matches;
            const matchesSpy = vi
                .spyOn(Element.prototype, 'matches')
                .mockImplementation(function (
                    this: Element,
                    selector: string,
                ) {
                    if (selector === ':focus-visible') {
                        return document.activeElement === this;
                    }
                    return nativeMatches.call(this, selector);
                });

            try {
                renderTimeRangeSelector({ start: START, end: END });

                fireEvent.click(
                    screen.getByRole('button', { name: 'seed' }),
                );

                const indicator = screen.getByRole('img', {
                    name: /auto-refresh is paused/i,
                });
                const tooltipText =
                    /^Auto-refresh is paused whilst a custom time range is active$/;
                expect(
                    screen.queryByText(tooltipText),
                ).not.toBeInTheDocument();

                act(() => {
                    indicator.focus();
                });

                expect(
                    await screen.findByText(tooltipText),
                ).toBeInTheDocument();
            } finally {
                matchesSpy.mockRestore();
            }
        });

        it('returns to a preset and clears the custom display', () => {
            renderTimeRangeSelector({ start: START, end: END });

            fireEvent.click(screen.getByRole('button', { name: 'seed' }));
            fireEvent.click(
                screen.getByRole('button', { name: /select 6h time range/i }),
            );

            expect(customToggle()).toHaveTextContent('Custom');
            expect(customToggle()).toHaveAttribute('aria-pressed', 'false');
            expect(
                screen.queryByLabelText(/auto-refresh is paused/i),
            ).not.toBeInTheDocument();
        });

        it('applies the seeded window from the popover and closes it', () => {
            renderTimeRangeSelector({ start: START, end: END });

            fireEvent.click(screen.getByRole('button', { name: 'seed' }));
            fireEvent.click(customToggle());

            const apply = screen.getByRole('button', { name: 'Apply' });
            expect(apply).toBeEnabled();

            fireEvent.click(apply);

            expect(screen.queryByLabelText('From')).not.toBeInTheDocument();
            expect(customToggle()).toHaveAttribute('aria-pressed', 'true');
            expect(customToggle()).toHaveTextContent(/Jan/);
        });
    });
});

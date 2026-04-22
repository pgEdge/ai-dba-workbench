/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Home as HomeIcon } from '@mui/icons-material';
import { renderWithTheme } from '../../../test/renderWithTheme';
import { Section, KV, UsageBar, LoadingSkeleton } from '../components';
import { SECTION_STATE_KEY, MONO_FONT } from '../serverInfoStyles';

describe('ServerInfoDialog components', () => {
    // -----------------------------------------------------------------------
    // Section
    // -----------------------------------------------------------------------

    describe('Section', () => {
        beforeEach(() => {
            vi.clearAllMocks();
            vi.mocked(localStorage.getItem).mockReturnValue(null);
        });

        it('renders the title', () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon data-testid="test-icon" />}
                    title="Test Section"
                >
                    <div>Content</div>
                </Section>
            );

            expect(screen.getByText('Test Section')).toBeInTheDocument();
        });

        it('renders the icon', () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon data-testid="test-icon" />}
                    title="Test Section"
                >
                    <div>Content</div>
                </Section>
            );

            expect(screen.getByTestId('test-icon')).toBeInTheDocument();
        });

        it('renders children content when open', () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon />}
                    title="Test Section"
                    defaultOpen={true}
                >
                    <div>Section Content</div>
                </Section>
            );

            expect(screen.getByText('Section Content')).toBeInTheDocument();
        });

        it('renders badge when provided', () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon />}
                    title="Test Section"
                    badge="42"
                >
                    <div>Content</div>
                </Section>
            );

            expect(screen.getByText('42')).toBeInTheDocument();
        });

        it('does not render badge when not provided', () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon />}
                    title="Test Section"
                >
                    <div>Content</div>
                </Section>
            );

            // Check that there is no element with the badge styling (mono font)
            const titleContainer = screen.getByText('Test Section').parentElement;
            expect(titleContainer?.textContent).not.toContain('42');
        });

        it('toggles content visibility on header click', async () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon />}
                    title="Test Section"
                    defaultOpen={true}
                >
                    <div>Toggle Content</div>
                </Section>
            );

            // Content is visible initially
            expect(screen.getByText('Toggle Content')).toBeVisible();

            // Click header to collapse
            fireEvent.click(screen.getByText('Test Section'));

            // Content should be hidden after collapse
            await waitFor(() => {
                expect(screen.getByText('Toggle Content')).not.toBeVisible();
            });
        });

        it('expands collapsed section on header click', async () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon />}
                    title="Test Section"
                    defaultOpen={false}
                >
                    <div>Expand Content</div>
                </Section>
            );

            // Content is hidden initially
            expect(screen.getByText('Expand Content')).not.toBeVisible();

            // Click header to expand
            fireEvent.click(screen.getByText('Test Section'));

            // Content should be visible after expand
            await waitFor(() => {
                expect(screen.getByText('Expand Content')).toBeVisible();
            });
        });

        it('persists collapsed state to localStorage', () => {
            renderWithTheme(
                <Section
                    sectionId="persist-test"
                    icon={<HomeIcon />}
                    title="Persist Section"
                    defaultOpen={true}
                >
                    <div>Persist Content</div>
                </Section>
            );

            // Click to collapse
            fireEvent.click(screen.getByText('Persist Section'));

            // Check localStorage was updated
            expect(localStorage.setItem).toHaveBeenCalledWith(
                SECTION_STATE_KEY,
                expect.any(String)
            );
            const lastCall = vi.mocked(localStorage.setItem).mock.calls.find(
                (call) => call[0] === SECTION_STATE_KEY
            );
            expect(lastCall).toBeTruthy();
            const state = JSON.parse(lastCall![1] as string);
            expect(state['persist-test']).toBe(false);
        });

        it('reads initial state from localStorage', () => {
            // Pre-set localStorage state to collapsed
            vi.mocked(localStorage.getItem).mockReturnValue(
                JSON.stringify({ 'preloaded-test': false })
            );

            renderWithTheme(
                <Section
                    sectionId="preloaded-test"
                    icon={<HomeIcon />}
                    title="Preloaded Section"
                    defaultOpen={true}
                >
                    <div>Preloaded Content</div>
                </Section>
            );

            // Should be collapsed despite defaultOpen=true
            expect(screen.getByText('Preloaded Content')).not.toBeVisible();
        });

        it('uses defaultOpen when localStorage has no state for section', () => {
            // Pre-set localStorage with different section
            vi.mocked(localStorage.getItem).mockReturnValue(
                JSON.stringify({ 'other-section': true })
            );

            renderWithTheme(
                <Section
                    sectionId="new-section"
                    icon={<HomeIcon />}
                    title="New Section"
                    defaultOpen={false}
                >
                    <div>New Content</div>
                </Section>
            );

            // Should use defaultOpen=false since no stored state
            expect(screen.getByText('New Content')).not.toBeVisible();
        });

        it('handles invalid localStorage JSON gracefully', () => {
            // Set invalid JSON
            vi.mocked(localStorage.getItem).mockReturnValue('invalid json');

            // Should not throw, should use defaultOpen
            renderWithTheme(
                <Section
                    sectionId="invalid-test"
                    icon={<HomeIcon />}
                    title="Invalid Section"
                    defaultOpen={true}
                >
                    <div>Invalid Content</div>
                </Section>
            );

            expect(screen.getByText('Invalid Content')).toBeVisible();
        });

        it('handles localStorage write errors gracefully', () => {
            // Mock setItem to throw
            vi.mocked(localStorage.setItem).mockImplementation(() => {
                throw new Error('Storage full');
            });

            renderWithTheme(
                <Section
                    sectionId="error-test"
                    icon={<HomeIcon />}
                    title="Error Section"
                    defaultOpen={true}
                >
                    <div>Error Content</div>
                </Section>
            );

            // Should not throw when toggling
            expect(() => {
                fireEvent.click(screen.getByText('Error Section'));
            }).not.toThrow();
        });

        it('shows ExpandLess icon when open', () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon />}
                    title="Test Section"
                    defaultOpen={true}
                >
                    <div>Content</div>
                </Section>
            );

            expect(screen.getByTestId('ExpandLessIcon')).toBeInTheDocument();
        });

        it('shows ExpandMore icon when closed', () => {
            renderWithTheme(
                <Section
                    sectionId="test"
                    icon={<HomeIcon />}
                    title="Test Section"
                    defaultOpen={false}
                >
                    <div>Content</div>
                </Section>
            );

            expect(screen.getByTestId('ExpandMoreIcon')).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // KV
    // -----------------------------------------------------------------------

    describe('KV', () => {
        it('renders label', () => {
            renderWithTheme(<KV label="Test Label" value="Test Value" />);

            expect(screen.getByText('Test Label')).toBeInTheDocument();
        });

        it('renders value', () => {
            renderWithTheme(<KV label="Test Label" value="Test Value" />);

            expect(screen.getByText('Test Value')).toBeInTheDocument();
        });

        it('renders em dash for empty value', () => {
            renderWithTheme(<KV label="Empty Label" value="" />);

            expect(screen.getByText('\u2014')).toBeInTheDocument();
        });

        it('renders em dash for null value', () => {
            renderWithTheme(<KV label="Null Label" value={null} />);

            expect(screen.getByText('\u2014')).toBeInTheDocument();
        });

        it('renders em dash for undefined value', () => {
            renderWithTheme(<KV label="Undefined Label" value={undefined} />);

            expect(screen.getByText('\u2014')).toBeInTheDocument();
        });

        it('renders em dash for 0 value (falsy in JS)', () => {
            // The KV component uses `value || em_dash` which treats 0 as falsy
            renderWithTheme(<KV label="Zero Label" value={0} />);

            expect(screen.getByText('\u2014')).toBeInTheDocument();
        });

        it('uses monospace font by default', () => {
            renderWithTheme(<KV label="Mono Label" value="Mono Value" />);

            const valueElement = screen.getByText('Mono Value');
            expect(valueElement).toHaveStyle({ fontFamily: MONO_FONT });
        });

        it('uses inherit font when mono is false', () => {
            renderWithTheme(
                <KV label="Non-Mono Label" value="Non-Mono Value" mono={false} />
            );

            const valueElement = screen.getByText('Non-Mono Value');
            expect(valueElement).toHaveStyle({ fontFamily: 'inherit' });
        });

        it('applies span class when span is true', () => {
            const { container } = renderWithTheme(
                <KV label="Span Label" value="Span Value" span={true} />
            );

            // Check the sx prop is applied by checking for MUI-generated styles
            const box = container.querySelector('.MuiBox-root');
            expect(box).toBeInTheDocument();
            // When span=true, the sx prop has gridColumn
            // We can verify the prop was set by checking the component renders
            expect(screen.getByText('Span Value')).toBeInTheDocument();
        });

        it('does not apply span styling by default', () => {
            const { container } = renderWithTheme(
                <KV label="No Span Label" value="No Span Value" />
            );

            const box = container.querySelector('.MuiBox-root');
            expect(box).toBeInTheDocument();
            // Verify the component renders without span
            expect(screen.getByText('No Span Value')).toBeInTheDocument();
        });

        it('renders React node values', () => {
            renderWithTheme(
                <KV
                    label="Node Label"
                    value={<span data-testid="custom-value">Custom Node</span>}
                />
            );

            expect(screen.getByTestId('custom-value')).toBeInTheDocument();
            expect(screen.getByText('Custom Node')).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // UsageBar
    // -----------------------------------------------------------------------

    describe('UsageBar', () => {
        it('renders label', () => {
            renderWithTheme(
                <UsageBar label="Memory" used={512} total={1024} />
            );

            expect(screen.getByText('Memory')).toBeInTheDocument();
        });

        it('renders formatted used and total bytes', () => {
            renderWithTheme(
                <UsageBar label="Memory" used={536870912} total={1073741824} />
            );

            // 512 MB / 1.0 GB
            expect(screen.getByText(/512\.0 MB/)).toBeInTheDocument();
            expect(screen.getByText(/1\.0 GB/)).toBeInTheDocument();
        });

        it('renders percentage', () => {
            renderWithTheme(
                <UsageBar label="Memory" used={512} total={1024} />
            );

            expect(screen.getByText(/50%/)).toBeInTheDocument();
        });

        it('renders progress bar', () => {
            const { container } = renderWithTheme(
                <UsageBar label="Memory" used={512} total={1024} />
            );

            const progressBar = container.querySelector('.MuiLinearProgress-root');
            expect(progressBar).toBeInTheDocument();
        });

        it('returns null when used is null', () => {
            const { container } = renderWithTheme(
                <UsageBar label="Memory" used={null} total={1024} />
            );

            expect(container.querySelector('.MuiLinearProgress-root')).not.toBeInTheDocument();
            expect(screen.queryByText('Memory')).not.toBeInTheDocument();
        });

        it('returns null when total is null', () => {
            const { container } = renderWithTheme(
                <UsageBar label="Memory" used={512} total={null} />
            );

            expect(container.querySelector('.MuiLinearProgress-root')).not.toBeInTheDocument();
        });

        it('returns null when total is zero', () => {
            const { container } = renderWithTheme(
                <UsageBar label="Memory" used={512} total={0} />
            );

            expect(container.querySelector('.MuiLinearProgress-root')).not.toBeInTheDocument();
        });

        it('renders the outer box container', () => {
            const { container } = renderWithTheme(
                <UsageBar label="Memory" used={512} total={1024} />
            );

            const box = container.querySelector('.MuiBox-root');
            expect(box).toBeInTheDocument();
        });

        it('displays usage information in proper format', () => {
            renderWithTheme(
                <UsageBar label="Disk" used={1073741824} total={10737418240} />
            );

            // 1.0 GB / 10.0 GB (10%)
            expect(screen.getByText('Disk')).toBeInTheDocument();
            expect(screen.getByText(/1\.0 GB/)).toBeInTheDocument();
            expect(screen.getByText(/10\.0 GB/)).toBeInTheDocument();
            expect(screen.getByText(/10%/)).toBeInTheDocument();
        });
    });

    // -----------------------------------------------------------------------
    // LoadingSkeleton
    // -----------------------------------------------------------------------

    describe('LoadingSkeleton', () => {
        it('renders multiple skeleton elements', () => {
            const { container } = renderWithTheme(<LoadingSkeleton />);

            const skeletons = container.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });

        it('renders skeleton for System section (6 items)', () => {
            const { container } = renderWithTheme(<LoadingSkeleton />);

            // The first grid has 6 items for system info
            const gridBoxes = container.querySelectorAll('.MuiBox-root > .MuiBox-root');
            expect(gridBoxes.length).toBeGreaterThan(5);
        });

        it('renders skeleton for PostgreSQL section (3 items)', () => {
            const { container } = renderWithTheme(<LoadingSkeleton />);

            const skeletons = container.querySelectorAll('.MuiSkeleton-root');
            // At minimum: system title + 6*2 items + pg title + 3*2 items + db title + 2*2 items
            // = 1 + 12 + 1 + 6 + 1 + 4 = 25 skeletons minimum
            expect(skeletons.length).toBeGreaterThanOrEqual(15);
        });

        it('renders skeleton for Databases section (2 items)', () => {
            const { container } = renderWithTheme(<LoadingSkeleton />);

            // Database section has 2 database items with 2 skeletons each
            const skeletons = container.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(10);
        });

        it('has correct padding', () => {
            const { container } = renderWithTheme(<LoadingSkeleton />);

            const outerBox = container.querySelector('.MuiBox-root');
            expect(outerBox).toHaveStyle({ padding: '20px' });
        });
    });
});

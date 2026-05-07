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
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import CollapsibleSection from '../CollapsibleSection';

describe('CollapsibleSection', () => {
    beforeEach(() => {
        vi.mocked(localStorage.getItem).mockReturnValue(null);
        vi.mocked(localStorage.setItem).mockClear();
        vi.mocked(localStorage.clear).mockClear();
    });
    it('renders the title', () => {
        render(
            <CollapsibleSection title="Performance">
                <p>Content here</p>
            </CollapsibleSection>,
        );

        expect(screen.getByText('Performance')).toBeInTheDocument();
    });

    it('renders children when expanded by default', () => {
        render(
            <CollapsibleSection title="Metrics">
                <p>Metric content</p>
            </CollapsibleSection>,
        );

        expect(screen.getByText('Metric content')).toBeInTheDocument();
    });

    it('hides children when defaultExpanded is false', () => {
        render(
            <CollapsibleSection title="Hidden" defaultExpanded={false}>
                <p>Hidden content</p>
            </CollapsibleSection>,
        );

        // MUI Collapse with unmountOnExit removes children from DOM
        expect(screen.queryByText('Hidden content')).not.toBeInTheDocument();
    });

    it('collapses content when the header is clicked', () => {
        render(
            <CollapsibleSection title="Toggle Me">
                <p>Toggled content</p>
            </CollapsibleSection>,
        );

        // Content should be visible initially
        expect(screen.getByText('Toggled content')).toBeInTheDocument();

        // Click the header to collapse
        const header = screen.getByRole('button', {
            name: /collapse toggle me section/i,
        });
        fireEvent.click(header);

        // After collapse animation, content should be removed (unmountOnExit)
        // We check the aria-expanded attribute instead since animation may be async
        expect(header).toHaveAttribute('aria-expanded', 'false');
    });

    it('expands content when the header is clicked after collapsing', () => {
        render(
            <CollapsibleSection title="Toggle Me" defaultExpanded={false}>
                <p>Toggled content</p>
            </CollapsibleSection>,
        );

        const header = screen.getByRole('button', {
            name: /expand toggle me section/i,
        });
        expect(header).toHaveAttribute('aria-expanded', 'false');

        fireEvent.click(header);

        expect(header).toHaveAttribute('aria-expanded', 'true');
    });

    it('renders headerRight content', () => {
        render(
            <CollapsibleSection
                title="Section"
                headerRight={<button>Action</button>}
            >
                <p>Body</p>
            </CollapsibleSection>,
        );

        expect(screen.getByText('Action')).toBeInTheDocument();
    });

    it('does not toggle when headerRight content is clicked', () => {
        render(
            <CollapsibleSection
                title="Section"
                headerRight={<button>Action</button>}
            >
                <p>Body</p>
            </CollapsibleSection>,
        );

        const header = screen.getByRole('button', {
            name: /collapse section section/i,
        });
        expect(header).toHaveAttribute('aria-expanded', 'true');

        // Click the headerRight button
        fireEvent.click(screen.getByText('Action'));

        // Should still be expanded because click was stopped from propagating
        expect(header).toHaveAttribute('aria-expanded', 'true');
    });

    it('supports keyboard activation with Enter', () => {
        render(
            <CollapsibleSection title="Keyboard Test">
                <p>Content</p>
            </CollapsibleSection>,
        );

        const header = screen.getByRole('button', {
            name: /collapse keyboard test section/i,
        });

        fireEvent.keyDown(header, { key: 'Enter' });

        expect(header).toHaveAttribute('aria-expanded', 'false');
    });

    it('supports keyboard activation with Space', () => {
        render(
            <CollapsibleSection title="Space Test">
                <p>Content</p>
            </CollapsibleSection>,
        );

        const header = screen.getByRole('button', {
            name: /collapse space test section/i,
        });

        fireEvent.keyDown(header, { key: ' ' });

        expect(header).toHaveAttribute('aria-expanded', 'false');
    });

    it('persists collapsed state in localStorage', () => {
        render(
            <CollapsibleSection title="Persist Test">
                <p>Content</p>
            </CollapsibleSection>,
        );

        const header = screen.getByRole('button', {
            name: /collapse persist test section/i,
        });

        // Collapse the section
        fireEvent.click(header);

        expect(localStorage.setItem).toHaveBeenCalledWith(
            'dashboard-section-persist-test-expanded',
            'false',
        );
    });

    it('restores collapsed state from localStorage', () => {
        const key = 'dashboard-section-restore-test-expanded';
        vi.mocked(localStorage.getItem).mockImplementation(
            (k: string) => (k === key ? 'false' : null),
        );

        render(
            <CollapsibleSection title="Restore Test">
                <p>Content</p>
            </CollapsibleSection>,
        );

        const header = screen.getByRole('button', {
            name: /expand restore test section/i,
        });

        expect(header).toHaveAttribute('aria-expanded', 'false');
    });

    it('uses explicit storageKey when provided', () => {
        render(
            <CollapsibleSection
                title="Custom Key"
                storageKey="my-custom-key"
            >
                <p>Content</p>
            </CollapsibleSection>,
        );

        const header = screen.getByRole('button', {
            name: /collapse custom key section/i,
        });

        fireEvent.click(header);

        expect(localStorage.setItem).toHaveBeenCalledWith(
            'my-custom-key',
            'false',
        );
    });

    describe('forceCollapsed prop', () => {
        it('forces section to initially collapse regardless of stored state', () => {
            // Simulate localStorage returning expanded state
            const key = 'dashboard-section-force-test-expanded';
            vi.mocked(localStorage.getItem).mockImplementation(
                (k: string) => (k === key ? 'true' : null),
            );

            render(
                <CollapsibleSection
                    title="Force Test"
                    forceCollapsed
                >
                    <p>Should not be visible</p>
                </CollapsibleSection>,
            );

            // Content should not be visible despite stored state being expanded
            expect(screen.queryByText('Should not be visible')).not.toBeInTheDocument();

            // Header should indicate collapsed state
            const header = screen.getByRole('button', {
                name: /expand force test section/i,
            });
            expect(header).toHaveAttribute('aria-expanded', 'false');
        });

        it('allows user to click to expand when forceCollapsed is true', () => {
            render(
                <CollapsibleSection
                    title="Click Expand"
                    forceCollapsed
                >
                    <p>Now visible content</p>
                </CollapsibleSection>,
            );

            const header = screen.getByRole('button', {
                name: /expand click expand section/i,
            });

            // Initially collapsed
            expect(header).toHaveAttribute('aria-expanded', 'false');
            expect(screen.queryByText('Now visible content')).not.toBeInTheDocument();

            // Click should expand the section
            fireEvent.click(header);
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // localStorage should NOT be called
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });

        it('allows user to use Enter key to expand when forceCollapsed is true', () => {
            render(
                <CollapsibleSection
                    title="Key Expand"
                    forceCollapsed
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            const header = screen.getByRole('button', {
                name: /expand key expand section/i,
            });

            // Initially collapsed
            expect(header).toHaveAttribute('aria-expanded', 'false');

            // Enter key should expand
            fireEvent.keyDown(header, { key: 'Enter' });
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // localStorage should NOT be called
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });

        it('allows user to use Space key to expand when forceCollapsed is true', () => {
            render(
                <CollapsibleSection
                    title="Space Expand"
                    forceCollapsed
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            const header = screen.getByRole('button', {
                name: /expand space expand section/i,
            });

            // Initially collapsed
            expect(header).toHaveAttribute('aria-expanded', 'false');

            // Space key should expand
            fireEvent.keyDown(header, { key: ' ' });
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // localStorage should NOT be called
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });

        it('does not modify localStorage when toggling with forceCollapsed', () => {
            const key = 'dashboard-section-preserve-state-expanded';
            vi.mocked(localStorage.getItem).mockImplementation(
                (k: string) => (k === key ? 'true' : null),
            );

            render(
                <CollapsibleSection
                    title="Preserve State"
                    forceCollapsed
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            const header = screen.getByRole('button', {
                name: /expand preserve state section/i,
            });

            // Expand
            fireEvent.click(header);
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // Collapse again
            fireEvent.click(header);
            expect(header).toHaveAttribute('aria-expanded', 'false');

            // localStorage should NOT be modified at any point
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });

        it('allows multiple toggle cycles without persisting state', () => {
            render(
                <CollapsibleSection
                    title="Multi Toggle"
                    forceCollapsed
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            const header = screen.getByRole('button', {
                name: /expand multi toggle section/i,
            });

            // Initial state: collapsed
            expect(header).toHaveAttribute('aria-expanded', 'false');

            // First click: expand
            fireEvent.click(header);
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // Second click: collapse
            fireEvent.click(header);
            expect(header).toHaveAttribute('aria-expanded', 'false');

            // Third click: expand again
            fireEvent.click(header);
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // localStorage should never be called
            expect(localStorage.setItem).not.toHaveBeenCalled();
        });

        it('reverts to stored state when forceCollapsed becomes false', () => {
            const key = 'dashboard-section-restore-state-expanded';
            vi.mocked(localStorage.getItem).mockImplementation(
                (k: string) => (k === key ? 'true' : null),
            );

            const { rerender } = render(
                <CollapsibleSection
                    title="Restore State"
                    forceCollapsed
                >
                    <p>Content visible again</p>
                </CollapsibleSection>,
            );

            // Content should be hidden due to forceCollapsed
            expect(screen.queryByText('Content visible again')).not.toBeInTheDocument();

            // Remove forceCollapsed
            rerender(
                <CollapsibleSection
                    title="Restore State"
                    forceCollapsed={false}
                >
                    <p>Content visible again</p>
                </CollapsibleSection>,
            );

            // Content should now be visible based on stored state (true)
            expect(screen.getByText('Content visible again')).toBeInTheDocument();
        });

        it('discards temporary override when forceCollapsed becomes false', () => {
            const key = 'dashboard-section-discard-override-expanded';
            // Stored state is collapsed
            vi.mocked(localStorage.getItem).mockImplementation(
                (k: string) => (k === key ? 'false' : null),
            );

            const { rerender } = render(
                <CollapsibleSection
                    title="Discard Override"
                    forceCollapsed
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            const header = screen.getByRole('button', {
                name: /expand discard override section/i,
            });

            // Initially collapsed (forceCollapsed)
            expect(header).toHaveAttribute('aria-expanded', 'false');

            // User manually expands
            fireEvent.click(header);
            expect(header).toHaveAttribute('aria-expanded', 'true');

            // Remove forceCollapsed - should revert to stored state (false)
            rerender(
                <CollapsibleSection
                    title="Discard Override"
                    forceCollapsed={false}
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            // Should be collapsed based on stored state, not the temporary override
            expect(header).toHaveAttribute('aria-expanded', 'false');
        });
    });

    describe('forceCollapsedMessage prop', () => {
        it('displays message in header when forceCollapsed is true and collapsed', () => {
            render(
                <CollapsibleSection
                    title="Message Test"
                    forceCollapsed
                    forceCollapsedMessage="No data available"
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            expect(screen.getByText('No data available')).toBeInTheDocument();
        });

        it('does not display message when forceCollapsed is false', () => {
            render(
                <CollapsibleSection
                    title="No Message"
                    forceCollapsed={false}
                    forceCollapsedMessage="Should not appear"
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            expect(screen.queryByText('Should not appear')).not.toBeInTheDocument();
        });

        it('does not display message when forceCollapsedMessage is not provided', () => {
            render(
                <CollapsibleSection
                    title="Empty Message"
                    forceCollapsed
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            // Only the title and expand icon should be in the header
            const header = screen.getByRole('button', {
                name: /expand empty message section/i,
            });
            expect(header).toBeInTheDocument();
        });

        it('hides message when user manually expands the section', () => {
            render(
                <CollapsibleSection
                    title="Hide Message"
                    forceCollapsed
                    forceCollapsedMessage="Message to hide"
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            // Message should be visible initially
            expect(screen.getByText('Message to hide')).toBeInTheDocument();

            // Click to expand
            const header = screen.getByRole('button', {
                name: /expand hide message section/i,
            });
            fireEvent.click(header);

            // Message should now be hidden
            expect(screen.queryByText('Message to hide')).not.toBeInTheDocument();
        });

        it('shows message again when user manually collapses the section', () => {
            render(
                <CollapsibleSection
                    title="Show Message"
                    forceCollapsed
                    forceCollapsedMessage="Reappearing message"
                >
                    <p>Content</p>
                </CollapsibleSection>,
            );

            const header = screen.getByRole('button', {
                name: /expand show message section/i,
            });

            // Message visible initially
            expect(screen.getByText('Reappearing message')).toBeInTheDocument();

            // Expand - message hidden
            fireEvent.click(header);
            expect(screen.queryByText('Reappearing message')).not.toBeInTheDocument();

            // Collapse - message visible again
            fireEvent.click(header);
            expect(screen.getByText('Reappearing message')).toBeInTheDocument();
        });
    });
});

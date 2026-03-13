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
});

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
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import MetricOverlay from '../MetricOverlay';
import { OverlayEntry } from '../types';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockPopOverlay = vi.fn();
const mockClearOverlays = vi.fn();
let mockCurrentOverlay: OverlayEntry | null = null;
let mockOverlayStack: OverlayEntry[] = [];

vi.mock('../../../contexts/DashboardContext', () => ({
    useDashboard: () => ({
        currentOverlay: mockCurrentOverlay,
        overlayStack: mockOverlayStack,
        popOverlay: mockPopOverlay,
        clearOverlays: mockClearOverlays,
    }),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const theme = createTheme();

const renderMetricOverlay = (children: React.ReactNode = <div>Overlay Content</div>) => {
    return render(
        <ThemeProvider theme={theme}>
            <MetricOverlay>{children}</MetricOverlay>
        </ThemeProvider>,
    );
};

const createOverlayEntry = (overrides: Partial<OverlayEntry> = {}): OverlayEntry => ({
    level: 'server',
    title: 'Test Overlay',
    entityId: 1,
    entityName: 'Test Entity',
    ...overrides,
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MetricOverlay', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockCurrentOverlay = null;
        mockOverlayStack = [];
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders nothing when currentOverlay is null', () => {
        const { container } = renderMetricOverlay();

        expect(container.firstChild).toBeNull();
    });

    it('renders overlay content when currentOverlay exists', () => {
        const overlay = createOverlayEntry({ title: 'Server Details' });
        mockCurrentOverlay = overlay;
        mockOverlayStack = [overlay];

        renderMetricOverlay(<div>Server Content</div>);

        expect(screen.getByText('Server Content')).toBeInTheDocument();
    });

    it('renders the overlay title', () => {
        const overlay = createOverlayEntry({ title: 'Database Performance' });
        mockCurrentOverlay = overlay;
        mockOverlayStack = [overlay];

        renderMetricOverlay();

        expect(screen.getByText('Database Performance')).toBeInTheDocument();
    });

    it('renders close button', () => {
        const overlay = createOverlayEntry();
        mockCurrentOverlay = overlay;
        mockOverlayStack = [overlay];

        renderMetricOverlay();

        expect(screen.getByRole('button', { name: /close overlay/i })).toBeInTheDocument();
    });

    it('calls clearOverlays when close button is clicked', () => {
        const overlay = createOverlayEntry();
        mockCurrentOverlay = overlay;
        mockOverlayStack = [overlay];

        renderMetricOverlay();

        const closeButton = screen.getByRole('button', { name: /close overlay/i });
        fireEvent.click(closeButton);

        expect(mockClearOverlays).toHaveBeenCalledTimes(1);
    });

    it('does not render back button when overlay stack has only one entry', () => {
        const overlay = createOverlayEntry();
        mockCurrentOverlay = overlay;
        mockOverlayStack = [overlay];

        renderMetricOverlay();

        expect(screen.queryByRole('button', { name: /go back/i })).not.toBeInTheDocument();
    });

    it('renders back button when overlay stack has multiple entries', () => {
        const overlay1 = createOverlayEntry({ title: 'First Overlay' });
        const overlay2 = createOverlayEntry({ title: 'Second Overlay' });
        mockCurrentOverlay = overlay2;
        mockOverlayStack = [overlay1, overlay2];

        renderMetricOverlay();

        expect(screen.getByRole('button', { name: /go back/i })).toBeInTheDocument();
    });

    it('calls popOverlay when back button is clicked', () => {
        const overlay1 = createOverlayEntry({ title: 'First Overlay' });
        const overlay2 = createOverlayEntry({ title: 'Second Overlay' });
        mockCurrentOverlay = overlay2;
        mockOverlayStack = [overlay1, overlay2];

        renderMetricOverlay();

        const backButton = screen.getByRole('button', { name: /go back/i });
        fireEvent.click(backButton);

        expect(mockPopOverlay).toHaveBeenCalledTimes(1);
    });

    it('renders children content', () => {
        const overlay = createOverlayEntry();
        mockCurrentOverlay = overlay;
        mockOverlayStack = [overlay];

        renderMetricOverlay(
            <div>
                <h2>Custom Content</h2>
                <p>Some details here</p>
            </div>,
        );

        expect(screen.getByText('Custom Content')).toBeInTheDocument();
        expect(screen.getByText('Some details here')).toBeInTheDocument();
    });

    it('renders backdrop element', () => {
        const overlay = createOverlayEntry();
        mockCurrentOverlay = overlay;
        mockOverlayStack = [overlay];

        const { container } = renderMetricOverlay();

        // MUI Backdrop creates a div with role="presentation"
        const backdrop = container.querySelector('.MuiBackdrop-root');
        expect(backdrop).toBeInTheDocument();
    });
});

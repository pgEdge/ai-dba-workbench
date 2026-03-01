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
import { describe, it, expect } from 'vitest';
import TimeRangeSelector from '../TimeRangeSelector';
import { DashboardProvider } from '../../../contexts/DashboardContext';

/**
 * Render TimeRangeSelector within a DashboardProvider so that
 * useDashboard is available.
 */
const renderTimeRangeSelector = () => {
    return render(
        <DashboardProvider>
            <TimeRangeSelector />
        </DashboardProvider>,
    );
};

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
});

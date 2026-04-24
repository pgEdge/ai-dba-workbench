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
import { describe, it, expect, vi } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import KpiTile from '../KpiTile';

// Mock the Sparkline component to avoid chart rendering complexity
vi.mock('../Sparkline', () => ({
    default: () => <div data-testid="sparkline" />,
}));

// Mock AICapabilitiesContext so KpiTile can render without the provider
vi.mock('../../../contexts/useAICapabilities', () => ({
    useAICapabilities: () => ({ aiEnabled: true, loading: false }),
}));

const theme = createTheme();

const renderKpiTile = (props: Record<string, unknown> = {}) => {
    const defaultProps = {
        label: 'CPU Usage',
        value: '75%',
    };

    return render(
        <ThemeProvider theme={theme}>
            <KpiTile {...defaultProps} {...props} />
        </ThemeProvider>,
    );
};

describe('KpiTile', () => {
    it('renders the label and value', () => {
        renderKpiTile();

        expect(screen.getByText('CPU Usage')).toBeInTheDocument();
        expect(screen.getByText('75%')).toBeInTheDocument();
    });

    it('renders the unit when provided', () => {
        renderKpiTile({ value: 42, unit: 'ms' });

        expect(screen.getByText('42')).toBeInTheDocument();
        expect(screen.getByText('ms')).toBeInTheDocument();
    });

    it('does not render unit when not provided', () => {
        const { container } = renderKpiTile({ value: 42 });

        // Should only have one Typography in the value area (the value itself)
        expect(screen.getByText('42')).toBeInTheDocument();
        expect(container.textContent).not.toContain('ms');
    });

    it('applies the correct aria-label', () => {
        renderKpiTile({ label: 'Latency', value: '12', unit: 'ms' });

        expect(screen.getByLabelText('Latency: 12 ms')).toBeInTheDocument();
    });

    it('applies aria-label without unit when unit is absent', () => {
        renderKpiTile({ label: 'Count', value: 99 });

        expect(screen.getByLabelText('Count: 99')).toBeInTheDocument();
    });

    it('renders trend indicator when trend and trendValue are provided', () => {
        renderKpiTile({ trend: 'up', trendValue: '+5%' });

        expect(screen.getByText('+5%')).toBeInTheDocument();
    });

    it('does not render trend indicator when trend is absent', () => {
        renderKpiTile();

        // No trend text should appear
        expect(screen.queryByText('+5%')).not.toBeInTheDocument();
    });

    it('renders sparkline when sparklineData is provided', () => {
        const sparklineData = [
            { time: '2025-01-01T00:00:00Z', value: 10 },
            { time: '2025-01-01T01:00:00Z', value: 20 },
        ];

        renderKpiTile({ sparklineData });

        expect(screen.getByTestId('sparkline')).toBeInTheDocument();
    });

    it('sets role=button and is clickable when onClick is provided', () => {
        const onClick = vi.fn();
        renderKpiTile({ onClick });

        const tile = screen.getByRole('button');
        fireEvent.click(tile);

        expect(onClick).toHaveBeenCalledTimes(1);
    });

    it('does not set role=button when onClick is absent', () => {
        renderKpiTile();

        expect(screen.queryByRole('button')).not.toBeInTheDocument();
    });

    it('responds to keyboard activation when clickable', () => {
        const onClick = vi.fn();
        renderKpiTile({ onClick });

        const tile = screen.getByRole('button');
        fireEvent.keyDown(tile, { key: 'Enter' });

        expect(onClick).toHaveBeenCalledTimes(1);
    });
});

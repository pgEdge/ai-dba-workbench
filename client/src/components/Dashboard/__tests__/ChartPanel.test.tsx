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
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ChartPanel from '../ChartPanel';

const theme = createTheme();

const renderChartPanel = (props: Partial<React.ComponentProps<typeof ChartPanel>> = {}) => {
    const defaultProps = {
        title: 'Test Chart',
        loading: false,
        hasData: true,
        emptyMessage: 'No data available',
        height: 200,
        children: <div data-testid="chart-content">Chart Content</div>,
    };

    return render(
        <ThemeProvider theme={theme}>
            <ChartPanel {...defaultProps} {...props} />
        </ThemeProvider>,
    );
};

describe('ChartPanel', () => {
    it('renders children when hasData is true and not loading', () => {
        renderChartPanel();

        expect(screen.getByTestId('chart-content')).toBeInTheDocument();
        expect(screen.getByText('Chart Content')).toBeInTheDocument();
    });

    it('does not render title wrapper when hasData is true and not loading', () => {
        renderChartPanel();

        // The title should not be visible since children are rendered directly
        expect(screen.queryByText('Test Chart')).not.toBeInTheDocument();
    });

    it('renders loading spinner when loading is true', () => {
        renderChartPanel({ loading: true, hasData: false });

        expect(screen.getByLabelText('Loading chart')).toBeInTheDocument();
        expect(screen.queryByTestId('chart-content')).not.toBeInTheDocument();
    });

    it('renders title when loading', () => {
        renderChartPanel({ loading: true, hasData: false });

        expect(screen.getByText('Test Chart')).toBeInTheDocument();
    });

    it('renders empty message when hasData is false and not loading', () => {
        renderChartPanel({ hasData: false, loading: false });

        expect(screen.getByText('No data available')).toBeInTheDocument();
        expect(screen.queryByTestId('chart-content')).not.toBeInTheDocument();
    });

    it('renders title when showing empty state', () => {
        renderChartPanel({ hasData: false, loading: false });

        expect(screen.getByText('Test Chart')).toBeInTheDocument();
    });

    it('prioritizes loading state over hasData', () => {
        // Even if hasData is true, loading should show the spinner
        renderChartPanel({ loading: true, hasData: true });

        expect(screen.getByLabelText('Loading chart')).toBeInTheDocument();
        expect(screen.queryByTestId('chart-content')).not.toBeInTheDocument();
    });

    it('renders custom empty message', () => {
        renderChartPanel({
            hasData: false,
            loading: false,
            emptyMessage: 'Custom empty message for testing',
        });

        expect(screen.getByText('Custom empty message for testing')).toBeInTheDocument();
    });

    it('renders custom title', () => {
        renderChartPanel({
            hasData: false,
            loading: false,
            title: 'Custom Chart Title',
        });

        expect(screen.getByText('Custom Chart Title')).toBeInTheDocument();
    });
});

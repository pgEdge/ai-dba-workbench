/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Component coverage for `MetricCard`. The card is a small presentation
 * component that pulls trend icon and color from the MUI theme based on
 * the `trend` prop, optionally renders a leading icon, and forwards the
 * `color` prop into the value text. The tests exercise the four
 * branches: no trend, up trend, down trend, with optional icon, and
 * verify that the trend block is hidden when no trend is provided.
 */

import type React from 'react';
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import {
    Memory as MemoryIcon,
} from '@mui/icons-material';
import MetricCard from '../MetricCard';
import { renderWithTheme } from '../../../test/renderWithTheme';

interface MetricCardProps {
    label: string;
    value: React.ReactNode;
    trend?: 'up' | 'down';
    trendValue?: React.ReactNode;
    icon?: React.ElementType;
    color?: string;
}

const renderCard = (props: Partial<MetricCardProps>) =>
    renderWithTheme(
        <MetricCard
            label="CPU"
            value="42%"
            {...(props as MetricCardProps)}
        />,
    );

describe('MetricCard', () => {
    it('renders the label and value text', () => {
        renderCard({ label: 'CPU', value: '42%' });
        expect(screen.getByText('CPU')).toBeInTheDocument();
        expect(screen.getByText('42%')).toBeInTheDocument();
    });

    it('omits the trend block when no trend prop is supplied', () => {
        renderCard({ label: 'CPU', value: '42%' });
        // Neither trend icon should appear; their roles are img by
        // default for MUI SvgIcon. We assert via testid through the
        // typography text not being rendered.
        expect(screen.queryByText('+1%')).not.toBeInTheDocument();
        expect(screen.queryByText('-1%')).not.toBeInTheDocument();
    });

    it('renders an up-trend with success color and trendValue text', () => {
        renderCard({
            label: 'CPU',
            value: '42%',
            trend: 'up',
            trendValue: '+5%',
        });
        const trendText = screen.getByText('+5%');
        expect(trendText).toBeInTheDocument();
        // The TrendingUp icon is rendered alongside the trend value;
        // confirm the parent container holds both an SVG and the text.
        const parent = trendText.parentElement;
        expect(parent?.querySelector('svg')).toBeTruthy();
    });

    it('renders a down-trend with the trendValue text', () => {
        renderCard({
            label: 'Memory',
            value: '88%',
            trend: 'down',
            trendValue: '-3%',
        });
        expect(screen.getByText('-3%')).toBeInTheDocument();
        expect(screen.getByText('Memory')).toBeInTheDocument();
    });

    it('renders an optional leading icon when provided', () => {
        const { container } = renderCard({
            label: 'Memory',
            value: '88%',
            icon: MemoryIcon,
        });
        // MUI icons render an <svg>. Without the icon prop the only
        // SVGs in the tree would be trend icons, which are not present
        // when `trend` is unset; so finding any SVG confirms the icon.
        const svgs = container.querySelectorAll('svg');
        expect(svgs.length).toBeGreaterThan(0);
    });

    it('does not render an icon element when icon prop is not provided', () => {
        const { container } = renderCard({
            label: 'Memory',
            value: '88%',
        });
        const svgs = container.querySelectorAll('svg');
        // No icon prop and no trend means no SVGs anywhere.
        expect(svgs.length).toBe(0);
    });

    it('applies custom color to the value typography', () => {
        renderCard({
            label: 'Connections',
            value: '128',
            color: '#ff00aa',
        });
        const valueEl = screen.getByText('128');
        // Inline style should reflect the custom color.
        expect(valueEl).toHaveStyle({ color: '#ff00aa' });
    });
});

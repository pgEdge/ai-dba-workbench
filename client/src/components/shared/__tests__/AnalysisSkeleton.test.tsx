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
import { describe, it, expect, vi } from 'vitest';
import AnalysisSkeleton from '../AnalysisSkeleton';
import { renderWithTheme } from '../../../test/renderWithTheme';

// Mock markdownStyles
vi.mock('../markdownStyles', () => ({
    getSkeletonBgSx: () => ({ bgcolor: 'grey.300' }),
    sxSkeletonContainer: { p: 2 },
    sxSkeletonRow: { display: 'flex', alignItems: 'center', gap: 1 },
}));

describe('AnalysisSkeleton', () => {
    describe('rendering', () => {
        it('renders without crashing', () => {
            renderWithTheme(<AnalysisSkeleton />);
            // Should render multiple skeleton elements
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(0);
        });

        it('renders text skeletons for sections', () => {
            renderWithTheme(<AnalysisSkeleton />);
            // Look for text variant skeletons
            const textSkeletons = document.querySelectorAll(
                '.MuiSkeleton-text'
            );
            expect(textSkeletons.length).toBeGreaterThan(0);
        });

        it('renders circular skeletons for bullet points', () => {
            renderWithTheme(<AnalysisSkeleton />);
            // Look for circular variant skeletons
            const circularSkeletons = document.querySelectorAll(
                '.MuiSkeleton-circular'
            );
            expect(circularSkeletons.length).toBe(3);
        });

        it('renders section headers with varying widths', () => {
            renderWithTheme(<AnalysisSkeleton />);
            // Should have multiple width variations
            const skeletons = document.querySelectorAll('.MuiSkeleton-root');
            expect(skeletons.length).toBeGreaterThan(5);
        });
    });

    describe('structure', () => {
        it('contains summary section skeleton', () => {
            const { container } = renderWithTheme(<AnalysisSkeleton />);
            // The component should have a container with skeleton elements
            expect(container.firstChild).toBeInTheDocument();
        });

        it('contains analysis section skeleton', () => {
            const { container } = renderWithTheme(<AnalysisSkeleton />);
            const skeletons = container.querySelectorAll('.MuiSkeleton-root');
            // Should have skeletons for summary, analysis, and remediation
            expect(skeletons.length).toBeGreaterThanOrEqual(10);
        });

        it('contains remediation section with bullet points', () => {
            renderWithTheme(<AnalysisSkeleton />);
            // Should have 3 bullet point rows
            const circularSkeletons = document.querySelectorAll(
                '.MuiSkeleton-circular'
            );
            expect(circularSkeletons.length).toBe(3);
        });
    });
});

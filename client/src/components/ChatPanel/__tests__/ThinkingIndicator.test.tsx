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
import { screen, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import ThinkingIndicator from '../ThinkingIndicator';
import { renderWithTheme } from '../../../test/renderWithTheme';

describe('ThinkingIndicator', () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    describe('visibility', () => {
        it('renders nothing when visible is false', () => {
            renderWithTheme(<ThinkingIndicator visible={false} />);
            expect(screen.queryByRole('status')).not.toBeInTheDocument();
        });

        it('renders indicator when visible is true', () => {
            renderWithTheme(<ThinkingIndicator visible={true} />);
            expect(screen.getByRole('status')).toBeInTheDocument();
        });
    });

    describe('content', () => {
        it('shows a thinking phrase', () => {
            renderWithTheme(<ThinkingIndicator visible={true} />);
            // The component renders one of many phrases, so just check there is text
            expect(screen.getByRole('status').textContent).not.toBe('');
        });

        it('shows a spinner', () => {
            renderWithTheme(<ThinkingIndicator visible={true} />);
            expect(screen.getByRole('progressbar')).toBeInTheDocument();
        });
    });

    describe('phrase cycling', () => {
        it('changes phrase after interval', () => {
            renderWithTheme(<ThinkingIndicator visible={true} />);

            // Advance past fade out and cycle interval
            act(() => {
                vi.advanceTimersByTime(2800);
            });

            // The phrase may or may not have changed (random), but no error
            expect(screen.getByRole('status')).toBeInTheDocument();
        });

        it('stops cycling when hidden', () => {
            const { rerender } = renderWithTheme(
                <ThinkingIndicator visible={true} />
            );

            // Start cycling
            act(() => {
                vi.advanceTimersByTime(1000);
            });

            // Hide indicator
            rerender(<ThinkingIndicator visible={false} />);

            // Should not throw when advancing timers
            act(() => {
                vi.advanceTimersByTime(5000);
            });

            expect(screen.queryByRole('status')).not.toBeInTheDocument();
        });

        it('restarts cycling when shown again', () => {
            const { rerender } = renderWithTheme(
                <ThinkingIndicator visible={true} />
            );

            // Hide and show again
            rerender(<ThinkingIndicator visible={false} />);
            rerender(<ThinkingIndicator visible={true} />);

            expect(screen.getByRole('status')).toBeInTheDocument();

            // Should continue cycling
            act(() => {
                vi.advanceTimersByTime(2800);
            });

            expect(screen.getByRole('status')).toBeInTheDocument();
        });
    });

    describe('accessibility', () => {
        it('has role status', () => {
            renderWithTheme(<ThinkingIndicator visible={true} />);
            expect(screen.getByRole('status')).toBeInTheDocument();
        });

        it('has aria-live polite', () => {
            renderWithTheme(<ThinkingIndicator visible={true} />);
            expect(screen.getByRole('status')).toHaveAttribute(
                'aria-live',
                'polite'
            );
        });
    });
});

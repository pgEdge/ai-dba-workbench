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
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import ErrorBoundary from '../ErrorBoundary';
import { logger } from '../../utils/logger';

/*
 * ThrowingChild is a deterministic helper that throws on first
 * render so the boundary always observes a real error rather than a
 * flaky async failure.
 */
const ThrowingChild: React.FC<{ message?: string }> = ({
    message = 'boom',
}) => {
    throw new Error(message);
};

describe('ErrorBoundary', () => {
    let errorSpy: ReturnType<typeof vi.spyOn>;
    let loggerErrorSpy: ReturnType<typeof vi.spyOn>;

    beforeEach(() => {
        // React intentionally writes the unhandled-error tree to the
        // browser console even when the boundary handles the failure.
        // Silence it so the test output stays readable.
        errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
        loggerErrorSpy = vi
            .spyOn(logger, 'error')
            .mockImplementation(() => {});
    });

    afterEach(() => {
        errorSpy.mockRestore();
        loggerErrorSpy.mockRestore();
        vi.restoreAllMocks();
    });

    describe('Happy path', () => {
        it('renders children unchanged when no error is thrown', () => {
            render(
                <ErrorBoundary>
                    <div data-testid="happy-child">All good</div>
                </ErrorBoundary>,
            );

            expect(screen.getByTestId('happy-child')).toBeInTheDocument();
            expect(screen.getByText('All good')).toBeInTheDocument();
        });
    });

    describe('Error path', () => {
        it('renders the fallback UI when a child component throws', () => {
            render(
                <ErrorBoundary>
                    <ThrowingChild />
                </ErrorBoundary>,
            );

            expect(screen.getByText('Something went wrong')).toBeInTheDocument();
            expect(
                screen.getByText('The application has crashed'),
            ).toBeInTheDocument();
        });

        it('shows the error message inside the details block', () => {
            render(
                <ErrorBoundary>
                    <ThrowingChild message="kaboom-message" />
                </ErrorBoundary>,
            );

            const details = screen.getByTestId('error-boundary-details');
            expect(details.textContent).toContain('kaboom-message');
        });

        it('logs the error and error info via the project logger', () => {
            render(
                <ErrorBoundary>
                    <ThrowingChild message="logged-message" />
                </ErrorBoundary>,
            );

            // Two separate logger.error calls: one for the error, one
            // for the React errorInfo.  Inspect both for the expected
            // markers.
            expect(loggerErrorSpy).toHaveBeenCalled();
            const calls = loggerErrorSpy.mock.calls;
            expect(calls[0][0]).toBe('ErrorBoundary caught an error:');
            expect(String(calls[0][1])).toContain('logged-message');
            expect(calls[1][0]).toBe('Error info:');
        });

        it('invokes the onError callback when provided', () => {
            const onError = vi.fn();
            render(
                <ErrorBoundary onError={onError}>
                    <ThrowingChild message="callback-message" />
                </ErrorBoundary>,
            );

            expect(onError).toHaveBeenCalledTimes(1);
            const [errorArg, infoArg] = onError.mock.calls[0];
            expect(errorArg).toBeInstanceOf(Error);
            expect((errorArg as Error).message).toBe('callback-message');
            expect(infoArg).toBeDefined();
        });

        it('renders the custom fallback when one is supplied', () => {
            render(
                <ErrorBoundary fallback={<div>custom fallback ui</div>}>
                    <ThrowingChild />
                </ErrorBoundary>,
            );

            expect(screen.getByText('custom fallback ui')).toBeInTheDocument();
            expect(
                screen.queryByText('Something went wrong'),
            ).not.toBeInTheDocument();
        });

        it('triggers window.location.reload when the reload button is clicked', () => {
            const reloadMock = vi.fn();
            // jsdom marks `window.location` non-writable; redefine via
            // defineProperty so the test can install the spy without
            // mutating the read-only descriptor.
            const original = window.location;
            Object.defineProperty(window, 'location', {
                configurable: true,
                value: { ...original, reload: reloadMock },
            });

            render(
                <ErrorBoundary>
                    <ThrowingChild />
                </ErrorBoundary>,
            );

            const reloadButton = screen.getByRole('button', { name: /reload/i });
            fireEvent.click(reloadButton);
            expect(reloadMock).toHaveBeenCalledTimes(1);

            Object.defineProperty(window, 'location', {
                configurable: true,
                value: original,
            });
        });

        it('toggles the details panel when the expand button is clicked', () => {
            render(
                <ErrorBoundary>
                    <ThrowingChild message="toggle-message" />
                </ErrorBoundary>,
            );

            const toggle = screen.getByRole('button', {
                name: /hide error details/i,
            });
            // Details start expanded.
            expect(toggle).toHaveAttribute('aria-expanded', 'true');

            fireEvent.click(toggle);

            // After clicking, the same button updates its aria-label
            // to 'Show error details' and aria-expanded becomes false.
            const collapsedToggle = screen.getByRole('button', {
                name: /show error details/i,
            });
            expect(collapsedToggle).toHaveAttribute('aria-expanded', 'false');
        });
    });
});

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
import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import CopyCodeButton from '../shared/CopyCodeButton';

// Mock markdownStyles to avoid pulling in the full style module
vi.mock('../shared/markdownStyles', () => ({
    getCopyButtonSx: () => ({}),
}));

const theme = createTheme();

const renderButton = (code = 'SELECT 1;') =>
    render(
        <ThemeProvider theme={theme}>
            <CopyCodeButton code={code} theme={theme} />
        </ThemeProvider>
    );

describe('CopyCodeButton Component', () => {
    let writeTextMock: ReturnType<typeof vi.fn>;

    beforeEach(() => {
        writeTextMock = vi.fn().mockResolvedValue(undefined);
        Object.assign(navigator, {
            clipboard: { writeText: writeTextMock },
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders the copy icon button with correct aria-label', () => {
        renderButton();
        const button = screen.getByRole('button', {
            name: 'Copy to clipboard',
        });
        expect(button).toBeInTheDocument();
    });

    it('renders ContentCopy icon initially', () => {
        renderButton();
        expect(
            screen.getByTestId('ContentCopyIcon')
        ).toBeInTheDocument();
        expect(screen.queryByTestId('CheckIcon')).not.toBeInTheDocument();
    });

    it('calls navigator.clipboard.writeText with the provided code', async () => {
        renderButton('CREATE TABLE t (id int);');
        const button = screen.getByRole('button', {
            name: 'Copy to clipboard',
        });

        await act(async () => {
            fireEvent.click(button);
        });

        expect(writeTextMock).toHaveBeenCalledWith(
            'CREATE TABLE t (id int);'
        );
    });

    it('shows Check icon after clicking (copied state)', async () => {
        renderButton();
        const button = screen.getByRole('button', {
            name: 'Copy to clipboard',
        });

        await act(async () => {
            fireEvent.click(button);
        });

        expect(screen.getByTestId('CheckIcon')).toBeInTheDocument();
        expect(
            screen.queryByTestId('ContentCopyIcon')
        ).not.toBeInTheDocument();
    });

    it('reverts to ContentCopy icon after 2-second timeout', async () => {
        vi.useFakeTimers();

        renderButton();
        const button = screen.getByRole('button', {
            name: 'Copy to clipboard',
        });

        await act(async () => {
            fireEvent.click(button);
        });

        // Confirm we are in the copied state
        expect(screen.getByTestId('CheckIcon')).toBeInTheDocument();

        // Advance timers past the 2-second reset window
        act(() => {
            vi.advanceTimersByTime(2000);
        });

        expect(
            screen.getByTestId('ContentCopyIcon')
        ).toBeInTheDocument();
        expect(screen.queryByTestId('CheckIcon')).not.toBeInTheDocument();

        vi.useRealTimers();
    });

    it('tooltip shows "Copy to clipboard" initially', async () => {
        renderButton();
        const button = screen.getByRole('button', {
            name: 'Copy to clipboard',
        });

        // Hover to trigger the tooltip
        await act(async () => {
            fireEvent.mouseOver(button);
        });

        expect(
            await screen.findByRole('tooltip')
        ).toHaveTextContent('Copy to clipboard');
    });

    it('tooltip shows "Copied!" after clicking', async () => {
        renderButton();
        const button = screen.getByRole('button', {
            name: 'Copy to clipboard',
        });

        await act(async () => {
            fireEvent.click(button);
        });

        // Hover to ensure tooltip is visible
        await act(async () => {
            fireEvent.mouseOver(button);
        });

        expect(
            await screen.findByRole('tooltip')
        ).toHaveTextContent('Copied!');
    });
});

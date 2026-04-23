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
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import CreatedTokenDialog from '../CreatedTokenDialog';

const theme = createTheme();

const TEST_TOKEN = 'pgedge_test_token_abc123xyz789';

const renderComponent = (props: Partial<React.ComponentProps<typeof CreatedTokenDialog>> = {}) => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
        token: TEST_TOKEN,
        onCopy: vi.fn(),
        copied: false,
    };
    return render(
        <ThemeProvider theme={theme}>
            <CreatedTokenDialog {...defaultProps} {...props} />
        </ThemeProvider>
    );
};

describe('CreatedTokenDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders the dialog title', () => {
        renderComponent();
        expect(screen.getByText('Token created')).toBeInTheDocument();
    });

    it('renders the warning alert', () => {
        renderComponent();
        expect(
            screen.getByText('Save this token securely. It will not be shown again.')
        ).toBeInTheDocument();
    });

    it('displays the token value', () => {
        renderComponent();
        expect(screen.getByText(TEST_TOKEN)).toBeInTheDocument();
    });

    it('renders copy button', () => {
        renderComponent();
        expect(screen.getByRole('button', { name: /copy token/i })).toBeInTheDocument();
    });

    it('calls onCopy when copy button is clicked', () => {
        const onCopy = vi.fn();
        renderComponent({ onCopy });

        fireEvent.click(screen.getByRole('button', { name: /copy token/i }));
        expect(onCopy).toHaveBeenCalled();
    });

    it('shows CopyIcon when copied is false', () => {
        renderComponent({ copied: false });
        expect(screen.getByTestId('ContentCopyIcon')).toBeInTheDocument();
        expect(screen.queryByTestId('CheckIcon')).not.toBeInTheDocument();
    });

    it('shows CheckIcon when copied is true', () => {
        renderComponent({ copied: true });
        expect(screen.getByTestId('CheckIcon')).toBeInTheDocument();
        expect(screen.queryByTestId('ContentCopyIcon')).not.toBeInTheDocument();
    });

    it('renders Close button', () => {
        renderComponent();
        expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument();
    });

    it('calls onClose when Close button is clicked', () => {
        const onClose = vi.fn();
        renderComponent({ onClose });

        fireEvent.click(screen.getByRole('button', { name: 'Close' }));
        expect(onClose).toHaveBeenCalled();
    });

    it('does not render when open is false', () => {
        renderComponent({ open: false });
        expect(screen.queryByText('Token created')).not.toBeInTheDocument();
    });

    it('displays null token gracefully', () => {
        renderComponent({ token: null });
        // The dialog should still render, just without the token text
        expect(screen.getByText('Token created')).toBeInTheDocument();
        // The token value should not appear
        expect(screen.queryByText(TEST_TOKEN)).not.toBeInTheDocument();
    });

    it('accepts a ref for the content element', () => {
        const ref = React.createRef<HTMLDivElement>();
        render(
            <ThemeProvider theme={theme}>
                <CreatedTokenDialog
                    ref={ref}
                    open={true}
                    onClose={vi.fn()}
                    token={TEST_TOKEN}
                    onCopy={vi.fn()}
                    copied={false}
                />
            </ThemeProvider>
        );

        // The ref should be attached to the DialogContent element
        expect(ref.current).toBeInstanceOf(HTMLElement);
    });
});

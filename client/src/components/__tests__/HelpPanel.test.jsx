/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import HelpPanel from '../HelpPanel';

describe('HelpPanel Component', () => {
    it('renders when open is true', () => {
        render(<HelpPanel open={true} onClose={vi.fn()} />);
        expect(screen.getByText('Help & Documentation')).toBeInTheDocument();
    });

    it('does not render content when open is false', () => {
        render(<HelpPanel open={false} onClose={vi.fn()} />);
        // The drawer is closed, so content should not be visible
        expect(screen.queryByText('Help & Documentation')).not.toBeInTheDocument();
    });

    it('displays Getting Started section', () => {
        render(<HelpPanel open={true} onClose={vi.fn()} />);
        expect(screen.getByText('Getting Started')).toBeInTheDocument();
    });

    it('displays Features section', () => {
        render(<HelpPanel open={true} onClose={vi.fn()} />);
        expect(screen.getByText('Features')).toBeInTheDocument();
    });

    it('displays Settings & Options section', () => {
        render(<HelpPanel open={true} onClose={vi.fn()} />);
        expect(screen.getByText('Settings & Options')).toBeInTheDocument();
    });

    it('displays Support section', () => {
        render(<HelpPanel open={true} onClose={vi.fn()} />);
        expect(screen.getByText('Support')).toBeInTheDocument();
    });

    it('displays version information', () => {
        render(<HelpPanel open={true} onClose={vi.fn()} />);
        expect(screen.getByText(/Client: v/)).toBeInTheDocument();
    });

    it('displays copyright notice', () => {
        render(<HelpPanel open={true} onClose={vi.fn()} />);
        expect(screen.getByText(/2025 - 2026, pgEdge, Inc/)).toBeInTheDocument();
    });

    it('calls onClose when close button is clicked', () => {
        const onClose = vi.fn();
        render(<HelpPanel open={true} onClose={onClose} />);

        const closeButton = screen.getByLabelText('close help');
        fireEvent.click(closeButton);

        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('displays feature list items', () => {
        render(<HelpPanel open={true} onClose={vi.fn()} />);

        expect(screen.getByText('Database Monitoring')).toBeInTheDocument();
        expect(screen.getByText('AI-Powered Analysis')).toBeInTheDocument();
        expect(screen.getByText('Query Analysis')).toBeInTheDocument();
        expect(screen.getByText('Alerting')).toBeInTheDocument();
    });
});

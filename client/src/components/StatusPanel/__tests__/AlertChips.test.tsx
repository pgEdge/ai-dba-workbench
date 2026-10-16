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
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ThemeProvider } from '@mui/material';
import { AlertSourceChip, AlertTypeChip } from '../AlertChips';
import { getAlertTypeLabel } from '../styles';
import { createPgedgeTheme } from '../../../theme/pgedgeTheme';

const renderWithTheme = (ui: React.ReactElement, mode: 'light' | 'dark') =>
    render(<ThemeProvider theme={createPgedgeTheme(mode)}>{ui}</ThemeProvider>);

describe('getAlertTypeLabel', () => {
    it('maps each alert type to its label', () => {
        expect(getAlertTypeLabel('anomaly')).toBe('Anomaly');
        expect(getAlertTypeLabel('system')).toBe('System');
        expect(getAlertTypeLabel('threshold')).toBe('Threshold');
        expect(getAlertTypeLabel(undefined)).toBe('Threshold');
    });
});

describe.each(['light', 'dark'] as const)('AlertChips in %s mode', (mode) => {
    it('renders the system type chip in the outlined form', () => {
        const { container } = renderWithTheme(<AlertTypeChip alertType="system" />, mode);
        expect(screen.getByText('System')).toBeInTheDocument();
        expect(container.querySelector('.MuiChip-outlined')).not.toBeNull();
    });

    it('renders the other type chips filled', () => {
        const { container } = renderWithTheme(<AlertTypeChip alertType="anomaly" />, mode);
        expect(screen.getByText('Anomaly')).toBeInTheDocument();
        expect(container.querySelector('.MuiChip-filled')).not.toBeNull();
    });

    it('defaults a missing type to threshold', () => {
        renderWithTheme(<AlertTypeChip />, mode);
        expect(screen.getByText('Threshold')).toBeInTheDocument();
    });

    it('labels a system alert source as the Workbench', () => {
        renderWithTheme(<AlertSourceChip isSystem showServer={false} />, mode);
        expect(screen.getByLabelText('Source: AI DBA Workbench')).toHaveTextContent('AI DBA Workbench');
    });

    it('names the server when server chips are shown', () => {
        renderWithTheme(<AlertSourceChip isSystem={false} server="server-1" showServer />, mode);
        expect(screen.getByText('server-1')).toBeInTheDocument();
    });

    it('renders nothing without a server or when hidden', () => {
        const hidden = renderWithTheme(
            <AlertSourceChip isSystem={false} server="server-1" showServer={false} />, mode,
        );
        expect(hidden.container).toBeEmptyDOMElement();
        hidden.unmount();
        const missing = renderWithTheme(<AlertSourceChip isSystem={false} showServer />, mode);
        expect(missing.container).toBeEmptyDOMElement();
    });
});

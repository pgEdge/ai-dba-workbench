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
import ConnectionScopeTable from '../ConnectionScopeTable';
import type { ScopedConnection } from '../tokenTypes';

const theme = createTheme();

const CONNECTIONS: ScopedConnection[] = [
    { id: 1, name: 'Primary DB', access_level: 'read' },
    { id: 2, name: 'Secondary DB', access_level: 'read_write' },
];

const renderComponent = (props: Partial<React.ComponentProps<typeof ConnectionScopeTable>> = {}) => {
    const defaultProps = {
        connections: CONNECTIONS,
        onAccessLevelChange: vi.fn(),
        onRemove: vi.fn(),
        ownerConnectionLevels: { 1: 'read_write', 2: 'read_write' },
        ownerIsSuperuser: false,
    };
    return render(
        <ThemeProvider theme={theme}>
            <ConnectionScopeTable {...defaultProps} {...props} />
        </ThemeProvider>
    );
};

describe('ConnectionScopeTable', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders nothing when connections array is empty', () => {
        const { container } = renderComponent({ connections: [] });
        expect(container).toBeEmptyDOMElement();
    });

    it('renders table with connection names', () => {
        renderComponent();
        expect(screen.getByText('Primary DB')).toBeInTheDocument();
        expect(screen.getByText('Secondary DB')).toBeInTheDocument();
    });

    it('renders table headers', () => {
        renderComponent();
        expect(screen.getByText('Connection')).toBeInTheDocument();
        expect(screen.getByText('Access Level')).toBeInTheDocument();
    });

    it('shows access level dropdowns with correct values', () => {
        renderComponent();
        const selects = screen.getAllByRole('combobox');
        expect(selects).toHaveLength(2);
    });

    it('calls onAccessLevelChange when access level is changed', () => {
        const onAccessLevelChange = vi.fn();
        renderComponent({ onAccessLevelChange });

        // Find the first select and change it
        const selects = screen.getAllByRole('combobox');
        fireEvent.mouseDown(selects[0]);

        // Find and click the Read/Write option
        const readWriteOption = screen.getByRole('option', { name: 'Read/Write' });
        fireEvent.click(readWriteOption);

        expect(onAccessLevelChange).toHaveBeenCalledWith(1, 'read_write');
    });

    it('calls onRemove when delete button is clicked', () => {
        const onRemove = vi.fn();
        renderComponent({ onRemove });

        const removeButtons = screen.getAllByRole('button', { name: /remove/i });
        fireEvent.click(removeButtons[0]);

        expect(onRemove).toHaveBeenCalledWith(1);
    });

    it('shows only Read Only option when owner has read level', () => {
        renderComponent({
            ownerConnectionLevels: { 1: 'read', 2: 'read' },
        });

        const selects = screen.getAllByRole('combobox');
        fireEvent.mouseDown(selects[0]);

        expect(screen.getByRole('option', { name: 'Read Only' })).toBeInTheDocument();
        expect(screen.queryByRole('option', { name: 'Read/Write' })).not.toBeInTheDocument();
    });

    it('shows Read/Write option when owner is superuser', () => {
        renderComponent({
            ownerConnectionLevels: { 1: 'read', 2: 'read' },
            ownerIsSuperuser: true,
        });

        const selects = screen.getAllByRole('combobox');
        fireEvent.mouseDown(selects[0]);

        expect(screen.getByRole('option', { name: 'Read Only' })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'Read/Write' })).toBeInTheDocument();
    });

    it('shows Read/Write option when owner has read_write level', () => {
        renderComponent({
            ownerConnectionLevels: { 1: 'read_write', 2: 'read_write' },
        });

        const selects = screen.getAllByRole('combobox');
        fireEvent.mouseDown(selects[0]);

        expect(screen.getByRole('option', { name: 'Read Only' })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'Read/Write' })).toBeInTheDocument();
    });

    it('disables controls when disabled prop is true', () => {
        renderComponent({ disabled: true });

        const selects = screen.getAllByRole('combobox');
        selects.forEach((select) => {
            expect(select).toHaveClass('Mui-disabled');
        });

        const removeButtons = screen.getAllByRole('button', { name: /remove/i });
        removeButtons.forEach((button) => {
            expect(button).toBeDisabled();
        });
    });

    it('renders correct aria-labels for remove buttons', () => {
        renderComponent();

        expect(screen.getByRole('button', { name: 'remove Primary DB' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'remove Secondary DB' })).toBeInTheDocument();
    });
});

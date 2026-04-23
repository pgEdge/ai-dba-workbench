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
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ScopeMultiSelect from '../ScopeMultiSelect';

const theme = createTheme();

interface TestOption {
    id: number;
    name: string;
    _isAll?: boolean;
}

const ALL_OPTION: TestOption = { id: -1, name: '*', _isAll: true };

const OPTIONS: TestOption[] = [
    { id: 1, name: 'Option A' },
    { id: 2, name: 'Option B' },
    { id: 3, name: 'Option C' },
];

const renderComponent = (props: Partial<React.ComponentProps<typeof ScopeMultiSelect<TestOption>>> = {}) => {
    const defaultProps = {
        label: 'Test Select',
        options: OPTIONS,
        value: [] as TestOption[],
        onChange: vi.fn(),
        getOptionLabel: (opt: TestOption) => opt._isAll ? 'All Options' : opt.name,
        allOption: ALL_OPTION,
    };
    return render(
        <ThemeProvider theme={theme}>
            <ScopeMultiSelect<TestOption> {...defaultProps} {...props} />
        </ThemeProvider>
    );
};

// Helper to open the autocomplete dropdown
const openDropdown = async () => {
    const input = screen.getByRole('combobox');
    // Focus and then use arrow down to open the dropdown
    fireEvent.focus(input);
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    await waitFor(() => {
        expect(screen.getByRole('listbox')).toBeInTheDocument();
    });
};

describe('ScopeMultiSelect', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders with the correct label', () => {
        renderComponent();
        expect(screen.getByLabelText('Test Select')).toBeInTheDocument();
    });

    it('shows all options including All when no selection is made', async () => {
        renderComponent();
        await openDropdown();

        expect(screen.getByRole('option', { name: 'All Options' })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'Option A' })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'Option B' })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'Option C' })).toBeInTheDocument();
    });

    it('calls onChange when an option is selected', async () => {
        const onChange = vi.fn();
        renderComponent({ onChange });
        await openDropdown();

        fireEvent.click(screen.getByRole('option', { name: 'Option A' }));
        expect(onChange).toHaveBeenCalled();
    });

    it('hides All option when individual items are selected', async () => {
        renderComponent({
            value: [OPTIONS[0]],
        });
        await openDropdown();

        expect(screen.queryByRole('option', { name: 'All Options' })).not.toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'Option B' })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'Option C' })).toBeInTheDocument();
    });

    it('shows only All option when All is selected', async () => {
        renderComponent({
            value: [ALL_OPTION],
        });
        await openDropdown();

        expect(screen.getByRole('option', { name: 'All Options' })).toBeInTheDocument();
        expect(screen.queryByRole('option', { name: 'Option A' })).not.toBeInTheDocument();
        expect(screen.queryByRole('option', { name: 'Option B' })).not.toBeInTheDocument();
        expect(screen.queryByRole('option', { name: 'Option C' })).not.toBeInTheDocument();
    });

    it('is disabled when disabled prop is true', () => {
        renderComponent({ disabled: true });
        const input = screen.getByRole('combobox');
        expect(input).toBeDisabled();
    });

    it('shows no listbox when options array is empty', () => {
        renderComponent({ options: [] });
        const input = screen.getByRole('combobox');
        fireEvent.focus(input);
        fireEvent.keyDown(input, { key: 'ArrowDown' });

        // No listbox should appear since there are no options
        expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    });

    it('replaces selection with All when All is newly selected', async () => {
        const onChange = vi.fn();
        renderComponent({ onChange });
        await openDropdown();

        // Select All
        fireEvent.click(screen.getByRole('option', { name: 'All Options' }));

        // Should call onChange with just the All option
        expect(onChange).toHaveBeenCalledWith([ALL_OPTION]);
    });

    it('displays selected options as chips', () => {
        renderComponent({
            value: [OPTIONS[0], OPTIONS[1]],
        });

        expect(screen.getByText('Option A')).toBeInTheDocument();
        expect(screen.getByText('Option B')).toBeInTheDocument();
    });

    it('displays All Options chip when All is selected', () => {
        renderComponent({
            value: [ALL_OPTION],
        });

        expect(screen.getByText('All Options')).toBeInTheDocument();
    });

    it('removes option when chip delete button is clicked', () => {
        const onChange = vi.fn();
        renderComponent({
            onChange,
            value: [ALL_OPTION],
        });

        // Find the chip's delete button (the X on the "All Options" chip)
        const chip = screen.getByText('All Options').closest('.MuiChip-root');
        expect(chip).toBeInTheDocument();

        const deleteButton = chip?.querySelector('[data-testid="CancelIcon"]');
        if (deleteButton) {
            fireEvent.click(deleteButton);
            expect(onChange).toHaveBeenCalled();
        }
    });
});

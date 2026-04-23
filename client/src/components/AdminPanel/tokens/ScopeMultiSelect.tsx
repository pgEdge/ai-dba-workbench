/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useCallback } from 'react';
import { Autocomplete, TextField } from '@mui/material';
import { SELECT_FIELD_SX } from '../../shared/formStyles';

/**
 * Props for the ScopeMultiSelect component.
 *
 * @template T - The type of options, must have an id and optional _isAll flag.
 */
export interface ScopeMultiSelectProps<T extends { id: string | number; _isAll?: boolean }> {
    /** Label for the autocomplete field. */
    label: string;
    /** All available options (excluding the "All" option if it should be added). */
    options: T[];
    /** Currently selected values. */
    value: T[];
    /** Callback when selection changes. */
    onChange: (value: T[]) => void;
    /** Function to get the display label for an option. */
    getOptionLabel: (option: T) => string;
    /** The "All" sentinel option. */
    allOption: T;
    /** Whether the field is disabled. */
    disabled?: boolean;
}

/**
 * A multi-select autocomplete for scope options (MCP privileges or admin
 * permissions) with special "All" option handling.
 *
 * When "All" is selected, only "All" is shown in options. When individual
 * items exist, "All" is hidden from options. Switching from individual to
 * All replaces the selection with just All.
 */
function ScopeMultiSelect<T extends { id: string | number; _isAll?: boolean }>({
    label,
    options,
    value,
    onChange,
    getOptionLabel,
    allOption,
    disabled = false,
}: ScopeMultiSelectProps<T>): React.ReactElement {
    // Build the filtered options list based on current selection
    const filteredOptions = React.useMemo(() => {
        const baseOptions: T[] = options.length > 0 ? [allOption, ...options] : [];
        return baseOptions.filter((p) => {
            // If "All" is currently selected, only show the All option
            if (value.some((s) => s._isAll)) {
                return p._isAll;
            }
            // If any individual items are selected, hide the All option
            if (value.length > 0 && p._isAll) {
                return false;
            }
            return true;
        });
    }, [options, value, allOption]);

    // Handle selection changes with All option logic
    const handleChange = useCallback(
        (_e: React.SyntheticEvent, newValue: T[]) => {
            const hasAll = newValue.some((v) => v._isAll);
            const hadAll = value.some((v) => v._isAll);

            if (hasAll && !hadAll) {
                // User just selected "All" - replace everything with just All
                onChange([allOption]);
            } else if (!hasAll && hadAll) {
                // User removed "All" - keep remaining individual items
                onChange(newValue.filter((v) => !v._isAll));
            } else {
                // Normal selection change
                onChange(newValue);
            }
        },
        [value, onChange, allOption]
    );

    // Check equality by id
    const isOptionEqualToValue = useCallback(
        (option: T, val: T) => option.id === val.id,
        []
    );

    return (
        <Autocomplete<T, true>
            multiple
            options={filteredOptions}
            getOptionLabel={getOptionLabel}
            isOptionEqualToValue={isOptionEqualToValue}
            value={value}
            onChange={handleChange}
            renderInput={(params) => (
                <TextField
                    {...params}
                    label={label}
                    margin="dense"
                    InputLabelProps={{
                        ...params.InputLabelProps,
                        shrink: true,
                    }}
                    sx={SELECT_FIELD_SX}
                />
            )}
            disabled={disabled}
        />
    );
}

export default ScopeMultiSelect;

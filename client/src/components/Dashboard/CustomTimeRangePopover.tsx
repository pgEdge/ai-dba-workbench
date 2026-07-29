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
import { useCallback, useEffect, useState } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Popover from '@mui/material/Popover';
import Typography from '@mui/material/Typography';
import { DateTimePicker } from '@mui/x-date-pickers/DateTimePicker';
import dayjs, { type Dayjs } from 'dayjs';
import {
    CUSTOM_RANGE_ACTIONS_SX,
    CUSTOM_RANGE_POPOVER_SX,
} from './styles';

export interface CustomTimeRangePopoverProps {
    /** Whether the popover is visible. */
    open: boolean;
    /** Element the popover anchors to. */
    anchorEl: HTMLElement | null;
    /** Current window start as an ISO string, if one is set. */
    startISO?: string;
    /** Current window end as an ISO string, if one is set. */
    endISO?: string;
    /** Called with the chosen window when the user applies it. */
    onApply: (startISO: string, endISO: string) => void;
    /** Called when the popover should close without applying. */
    onClose: () => void;
}

/**
 * Convert an optional ISO string into a Dayjs value, treating an absent
 * or unparseable string as no selection.
 */
const toDayjs = (iso?: string): Dayjs | null => {
    if (iso === undefined || iso === '') {
        return null;
    }
    const parsed = dayjs(iso);
    return parsed.isValid() ? parsed : null;
};

/**
 * A window is applicable only when both bounds are present, both parse,
 * and the end is strictly after the start.
 */
const isWindowValid = (start: Dayjs | null, end: Dayjs | null): boolean => {
    if (start === null || end === null) {
        return false;
    }
    if (!start.isValid() || !end.isValid()) {
        return false;
    }
    return end.isAfter(start);
};

/**
 * A popover offering From and To date-time fields for an arbitrary
 * window, with Cancel and Apply actions. Apply stays disabled whilst the
 * entered window is invalid.
 *
 * The component deliberately reads no context; callers own the range
 * state, so both the dashboard selector and the event timeline can reuse
 * it despite keeping their ranges in different places.
 */
const CustomTimeRangePopover: React.FC<CustomTimeRangePopoverProps> = ({
    open,
    anchorEl,
    startISO,
    endISO,
    onApply,
    onClose,
}) => {
    const [start, setStart] = useState<Dayjs | null>(() => toDayjs(startISO));
    const [end, setEnd] = useState<Dayjs | null>(() => toDayjs(endISO));

    /*
     * Re-seed the fields from the incoming window each time the popover
     * opens, so a cancelled edit does not leak into the next visit.
     */
    useEffect(() => {
        if (open) {
            setStart(toDayjs(startISO));
            setEnd(toDayjs(endISO));
        }
    }, [open, startISO, endISO]);

    const valid = isWindowValid(start, end);

    const handleApply = useCallback((): void => {
        // The Apply button is disabled unless the window is valid; the
        // null check is here only to narrow the types.
        if (start !== null && end !== null) {
            onApply(start.toISOString(), end.toISOString());
        }
    }, [start, end, onApply]);

    return (
        <Popover
            open={open}
            anchorEl={anchorEl}
            onClose={onClose}
            anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
            transformOrigin={{ vertical: 'top', horizontal: 'right' }}
            slotProps={{ paper: { 'aria-label': 'Custom time range' } }}
        >
            <Box sx={CUSTOM_RANGE_POPOVER_SX}>
                <DateTimePicker
                    label="From"
                    value={start}
                    onChange={setStart}
                    slotProps={{ textField: { size: 'small' } }}
                />
                <DateTimePicker
                    label="To"
                    value={end}
                    onChange={setEnd}
                    slotProps={{ textField: { size: 'small' } }}
                />
                {!valid && (
                    <Typography variant="caption" color="error">
                        Choose a start and an end, with the end after the
                        start.
                    </Typography>
                )}
                <Box sx={CUSTOM_RANGE_ACTIONS_SX}>
                    <Button size="small" onClick={onClose}>
                        Cancel
                    </Button>
                    <Button
                        size="small"
                        variant="contained"
                        disabled={!valid}
                        onClick={handleApply}
                    >
                        Apply
                    </Button>
                </Box>
            </Box>
        </Popover>
    );
};

export default CustomTimeRangePopover;

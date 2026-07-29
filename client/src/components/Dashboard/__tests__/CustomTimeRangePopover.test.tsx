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
import { screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import dayjs, { type Dayjs } from 'dayjs';
import renderWithTheme from '../../../test/renderWithTheme';
import CustomTimeRangePopover from '../CustomTimeRangePopover';

/*
 * The real DateTimePicker renders a section-based field whose keyboard
 * editing model is MUI's concern, not ours. Replacing it with a plain
 * text input keeps these tests focused on the popover's own validation
 * and Apply behaviour, and lets a test drive a field change in one step.
 * TimeRangeSelector.test.tsx exercises the component with the real
 * picker mounted.
 */
vi.mock('@mui/x-date-pickers/DateTimePicker', () => ({
    DateTimePicker: ({
        label,
        value,
        onChange,
    }: {
        label: string;
        value: Dayjs | null;
        onChange: (value: Dayjs | null) => void;
    }) => (
        <input
            aria-label={label}
            value={value === null || !value.isValid() ? '' : value.toISOString()}
            onChange={(event) => {
                onChange(
                    event.target.value === ''
                        ? null
                        : dayjs(event.target.value),
                );
            }}
        />
    ),
}));

const START = '2026-01-01T00:00:00.000Z';
const END = '2026-01-01T06:00:00.000Z';

describe('CustomTimeRangePopover', () => {
    const onApply = vi.fn();
    const onClose = vi.fn();

    beforeEach(() => {
        onApply.mockClear();
        onClose.mockClear();
    });

    const renderPopover = (
        props: Partial<React.ComponentProps<typeof CustomTimeRangePopover>> = {},
    ) => {
        const anchorEl = document.createElement('div');
        document.body.appendChild(anchorEl);
        return renderWithTheme(
            <CustomTimeRangePopover
                open
                anchorEl={anchorEl}
                onApply={onApply}
                onClose={onClose}
                {...props}
            />,
        );
    };

    const applyButton = () =>
        screen.getByRole('button', { name: 'Apply' });

    it('renders nothing whilst closed', () => {
        renderPopover({ open: false });

        expect(screen.queryByLabelText('From')).not.toBeInTheDocument();
    });

    it('seeds both fields from the supplied window', () => {
        renderPopover({ startISO: START, endISO: END });

        expect(screen.getByLabelText('From')).toHaveValue(START);
        expect(screen.getByLabelText('To')).toHaveValue(END);
    });

    it('treats an unparseable timestamp as no selection', () => {
        renderPopover({ startISO: 'not-a-date', endISO: END });

        expect(screen.getByLabelText('From')).toHaveValue('');
        expect(applyButton()).toBeDisabled();
    });

    it('disables Apply and explains why when a bound is missing', () => {
        renderPopover({ startISO: START });

        expect(applyButton()).toBeDisabled();
        expect(
            screen.getByText(/end after the/i),
        ).toBeInTheDocument();
    });

    it('disables Apply when the end is not after the start', () => {
        renderPopover({ startISO: END, endISO: START });

        expect(applyButton()).toBeDisabled();
    });

    it('disables Apply when the bounds are equal', () => {
        renderPopover({ startISO: START, endISO: START });

        expect(applyButton()).toBeDisabled();
    });

    it('enables Apply once an edit makes the window valid', () => {
        renderPopover({ startISO: START });

        expect(applyButton()).toBeDisabled();

        fireEvent.change(screen.getByLabelText('To'), {
            target: { value: END },
        });

        expect(applyButton()).toBeEnabled();
        expect(screen.queryByText(/end after the/i)).not.toBeInTheDocument();
    });

    /*
     * A part-entered date reaches onChange as an invalid Dayjs rather
     * than as null, so the validity check has to reject those too.
     */
    it('disables Apply when an edit yields an unparseable date', () => {
        renderPopover({ startISO: START, endISO: END });

        expect(applyButton()).toBeEnabled();

        fireEvent.change(screen.getByLabelText('To'), {
            target: { value: 'the day before yesterday' },
        });

        expect(applyButton()).toBeDisabled();
    });

    it('disables Apply again when a field is cleared', () => {
        renderPopover({ startISO: START, endISO: END });

        expect(applyButton()).toBeEnabled();

        fireEvent.change(screen.getByLabelText('From'), {
            target: { value: '' },
        });

        expect(applyButton()).toBeDisabled();
    });

    it('reports the chosen window as ISO strings on Apply', () => {
        renderPopover({ startISO: START, endISO: END });

        fireEvent.click(applyButton());

        expect(onApply).toHaveBeenCalledTimes(1);
        expect(onApply).toHaveBeenCalledWith(START, END);
    });

    it('reports an edited window on Apply', () => {
        renderPopover({ startISO: START, endISO: END });

        fireEvent.change(screen.getByLabelText('To'), {
            target: { value: '2026-01-02T00:00:00.000Z' },
        });
        fireEvent.click(applyButton());

        expect(onApply).toHaveBeenCalledWith(
            START,
            '2026-01-02T00:00:00.000Z',
        );
    });

    it('closes without applying on Cancel', () => {
        renderPopover({ startISO: START, endISO: END });

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        expect(onClose).toHaveBeenCalledTimes(1);
        expect(onApply).not.toHaveBeenCalled();
    });

    it('discards an abandoned edit when reopened', () => {
        const anchorEl = document.createElement('div');
        document.body.appendChild(anchorEl);

        const element = (open: boolean) => (
            <CustomTimeRangePopover
                open={open}
                anchorEl={anchorEl}
                startISO={START}
                endISO={END}
                onApply={onApply}
                onClose={onClose}
            />
        );

        const { rerender } = renderWithTheme(element(true));

        fireEvent.change(screen.getByLabelText('To'), {
            target: { value: '2026-02-02T00:00:00.000Z' },
        });
        expect(screen.getByLabelText('To')).toHaveValue(
            '2026-02-02T00:00:00.000Z',
        );

        rerender(element(false));
        rerender(element(true));

        expect(screen.getByLabelText('To')).toHaveValue(END);
    });
});

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import dayjs, { type Dayjs } from 'dayjs';
import renderWithTheme from '../../../test/renderWithTheme';
import EventTimeline from '../index';
import { TIME_RANGE_STORAGE_KEY } from '../config';
import * as useTimelineEventsModule from '../../../hooks/useTimelineEvents';

vi.mock('../../../hooks/useTimelineEvents', () => ({
    useTimelineEvents: vi.fn(),
}));

/*
 * The real DateTimePicker renders a section-based field whose keyboard
 * editing model is MUI's concern; a plain text input keeps these tests
 * focused on the timeline's own wiring and removes the need for a
 * LocalizationProvider in this file. TimeRangeSelector.test.tsx already
 * exercises the popover with the real picker mounted.
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

const START = '2026-02-01T09:00:00.000Z';
const END = '2026-02-01T17:30:00.000Z';

const selection = {
    type: 'server' as const,
    id: 1,
    name: 'Test Server',
    serverIds: [1],
};

const mockedHook = useTimelineEventsModule.useTimelineEvents as ReturnType<typeof vi.fn>;

/*
 * src/test/setup.ts replaces localStorage with vi.fn stubs, so the
 * persistence assertions inspect the setItem calls for the timeline key
 * rather than reading a value back out.
 */
const storedRanges = (): unknown[] =>
    (localStorage.setItem as ReturnType<typeof vi.fn>).mock.calls
        .filter((call) => call[0] === TIME_RANGE_STORAGE_KEY)
        .map((call) => call[1]);

const hookCalls = (): Record<string, unknown>[] =>
    mockedHook.mock.calls.map((call) => call[0] as Record<string, unknown>);

const lastTimeRange = (): unknown => {
    const calls = hookCalls();
    return calls[calls.length - 1].timeRange;
};

/** Open the picker and apply the fixed window used throughout this file. */
const applyWindow = (startISO = START, endISO = END): void => {
    fireEvent.click(
        screen.getByRole('button', { name: 'Select custom time range' }),
    );
    fireEvent.change(screen.getByLabelText('From'), {
        target: { value: startISO },
    });
    fireEvent.change(screen.getByLabelText('To'), {
        target: { value: endISO },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
};

describe('EventTimeline custom time range', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockedHook.mockReturnValue({
            events: [],
            loading: false,
            error: null,
            totalCount: 0,
            refetch: vi.fn(),
            retrying: false,
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('opens the picker from the custom toggle', () => {
        renderWithTheme(<EventTimeline selection={selection} />);

        expect(screen.queryByLabelText('From')).not.toBeInTheDocument();

        fireEvent.click(
            screen.getByRole('button', { name: 'Select custom time range' }),
        );

        expect(screen.getByLabelText('From')).toBeInTheDocument();
        expect(screen.getByLabelText('To')).toBeInTheDocument();
    });

    it('passes the applied window to the fetch hook as dates', () => {
        renderWithTheme(<EventTimeline selection={selection} />);

        expect(lastTimeRange()).toBe('24h');

        applyWindow();

        const range = lastTimeRange() as { start: Date; end: Date };
        expect(range.start).toBeInstanceOf(Date);
        expect(range.end).toBeInstanceOf(Date);
        expect(range.start.toISOString()).toBe(START);
        expect(range.end.toISOString()).toBe(END);
    });

    it('marks the custom toggle as selected and describes the window', () => {
        renderWithTheme(<EventTimeline selection={selection} />);

        applyWindow();

        const customButton = screen.getByRole('button', {
            name: 'Select custom time range',
        });
        expect(customButton).toHaveClass('Mui-selected');
        expect(screen.getByRole('button', { name: '24h' })).not.toHaveClass(
            'Mui-selected',
        );

        const title = customButton.getAttribute('title');
        expect(title).toContain(new Date(START).toLocaleString());
        expect(title).toContain(new Date(END).toLocaleString());
    });

    it('closes the picker without applying when cancelled', () => {
        renderWithTheme(<EventTimeline selection={selection} />);

        fireEvent.click(
            screen.getByRole('button', { name: 'Select custom time range' }),
        );
        fireEvent.change(screen.getByLabelText('From'), {
            target: { value: START },
        });
        fireEvent.change(screen.getByLabelText('To'), {
            target: { value: END },
        });
        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        expect(screen.queryByLabelText('From')).not.toBeInTheDocument();
        expect(lastTimeRange()).toBe('24h');
    });

    it('does not persist a custom window, leaving the stored preset intact', () => {
        renderWithTheme(<EventTimeline selection={selection} />);

        fireEvent.click(screen.getByRole('button', { name: '6h' }));
        expect(storedRanges()).toEqual(['24h', '6h']);

        applyWindow();

        // Nothing further is written, so the last stored value is still a
        // preset and a reload cannot resurrect the custom window.
        expect(lastTimeRange()).not.toBe('6h');
        expect(storedRanges()).toEqual(['24h', '6h']);
    });

    it('returns to a preset when one is chosen after a custom window', () => {
        renderWithTheme(<EventTimeline selection={selection} />);

        applyWindow();
        fireEvent.click(screen.getByRole('button', { name: '7d' }));

        expect(lastTimeRange()).toBe('7d');
        expect(storedRanges()).toEqual(['24h', '7d']);
    });

    it('ignores a click on the already selected custom toggle', () => {
        renderWithTheme(<EventTimeline selection={selection} />);

        applyWindow();
        const applied = lastTimeRange();

        /*
         * An exclusive ToggleButtonGroup reports null when its selected
         * button is clicked again; that must not clear the window, and it
         * must reopen the picker seeded with the applied bounds.
         */
        fireEvent.click(
            screen.getByRole('button', { name: 'Select custom time range' }),
        );

        expect(lastTimeRange()).toBe(applied);
        expect(screen.getByLabelText('From')).toHaveValue(START);
        expect(screen.getByLabelText('To')).toHaveValue(END);
    });

    it('keeps the picker closed when the timeline has no selection', () => {
        const { container } = renderWithTheme(<EventTimeline selection={null} />);

        expect(container).toBeEmptyDOMElement();
    });
});

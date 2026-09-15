/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import {
    screen,
    fireEvent,
    waitFor,
    within,
    act,
} from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';

const mockApiFetch = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

import AdminAuditLog from '../AdminAuditLog';
import type { AuditEvent } from '../AdminAuditLog';

const EVENTS: AuditEvent[] = [
    {
        id: 12,
        occurred_at: '2026-09-15T10:22:31.123456Z',
        actor_type: 'user',
        actor_id: 3,
        actor_name: 'alice',
        actor_ip: '192.0.2.5',
        action: 'group.delete',
        target_type: 'group',
        target_id: 7,
        target_name: 'dbas',
        outcome: 'success',
        details: { before: { name: 'dbas', description: '' } },
        prev_hash: 'aaa',
        hash: 'bbb',
    },
    {
        id: 11,
        occurred_at: '2026-09-15T09:01:00Z',
        actor_type: 'token',
        actor_id: null,
        actor_name: 'ci-token',
        action: 'permission.admin.revoke',
        target_id: null,
        outcome: 'denied',
        error: 'requires manage_groups permission',
        prev_hash: 'ccc',
        hash: 'ddd',
    },
];

/**
 * A sparse event: a failure with no error text, no details, no target
 * name and an unparseable timestamp, exercising every fallback the
 * table renders.
 */
const SPARSE_EVENT: AuditEvent = {
    id: 10,
    occurred_at: 'not-a-timestamp',
    actor_type: 'system',
    actor_id: null,
    actor_name: '',
    action: 'user.create',
    target_type: 'user',
    target_id: 42,
    outcome: 'failure',
    prev_hash: 'eee',
    hash: 'fff',
};

/** Build a minimal Response stand-in for a successful audit request. */
const okResponse = (
    events: AuditEvent[],
    total?: string | null,
): Response =>
    ({
        ok: true,
        status: 200,
        headers: {
            get: (name: string) =>
                name.toLowerCase() === 'x-total-count'
                    ? (total ?? null)
                    : null,
        },
        json: () => Promise.resolve(events),
    }) as unknown as Response;

/** Build a Response stand-in for a failing audit request. */
const errorResponse = (status: number, body: string): Response =>
    ({
        ok: false,
        status,
        headers: { get: () => null },
        text: () => Promise.resolve(body),
        json: () => Promise.reject(new Error('not json')),
    }) as unknown as Response;

/** The URL passed to the most recent apiFetch call. */
const lastUrl = (): string => {
    const calls = mockApiFetch.mock.calls;
    return String(calls[calls.length - 1][0]);
};

/** The query parameters of the most recent apiFetch call. */
const lastParams = (): URLSearchParams =>
    new URLSearchParams(lastUrl().split('?')[1] ?? '');

/** Open a MUI Select by its accessible name and choose an option. */
const chooseOption = async (label: string, option: string) => {
    fireEvent.mouseDown(screen.getByLabelText(label));
    const listbox = await screen.findByRole('listbox');
    fireEvent.click(within(listbox).getByRole('option', { name: option }));
};

describe('AdminAuditLog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockApiFetch.mockResolvedValue(okResponse(EVENTS, '2'));
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders a row for each audit event', async () => {
        renderWithTheme(<AdminAuditLog />);

        expect(await screen.findByText('group.delete')).toBeInTheDocument();
        expect(screen.getByText('permission.admin.revoke')).toBeInTheDocument();
        expect(screen.getByText('alice')).toBeInTheDocument();
        expect(screen.getByText('ci-token')).toBeInTheDocument();
        expect(screen.getByText('success')).toBeInTheDocument();
        expect(screen.getByText('denied')).toBeInTheDocument();
        expect(screen.getByText('dbas')).toBeInTheDocument();
    });

    it('requests the first page with the default limit on mount', async () => {
        renderWithTheme(<AdminAuditLog />);

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        expect(lastUrl()).toContain('/api/v1/rbac/audit');
        expect(lastParams().get('limit')).toBe('25');
        expect(lastParams().get('offset')).toBe('0');
    });

    it('sends the filter values when Apply is clicked', async () => {
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        fireEvent.change(screen.getByLabelText('Actor'), {
            target: { value: 'alice' },
        });
        fireEvent.change(screen.getByLabelText('Action'), {
            target: { value: 'group.delete' },
        });
        await chooseOption('Target type', 'group');
        await chooseOption('Outcome', 'success');
        fireEvent.change(screen.getByLabelText('Since'), {
            target: { value: '2026-09-01T00:00' },
        });
        fireEvent.change(screen.getByLabelText('Until'), {
            target: { value: '2026-09-15T12:00' },
        });

        mockApiFetch.mockClear();
        fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        const params = lastParams();
        expect(params.get('actor')).toBe('alice');
        expect(params.get('action')).toBe('group.delete');
        expect(params.get('target_type')).toBe('group');
        expect(params.get('outcome')).toBe('success');
        expect(params.get('since')).toBe(
            new Date('2026-09-01T00:00').toISOString(),
        );
        expect(params.get('until')).toBe(
            new Date('2026-09-15T12:00').toISOString(),
        );
    });

    it('omits filters that are left blank', async () => {
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        mockApiFetch.mockClear();
        fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        const params = lastParams();
        expect(params.has('actor')).toBe(false);
        expect(params.has('outcome')).toBe(false);
        expect(params.has('since')).toBe(false);
    });

    it('advances the offset when the next page is requested', async () => {
        mockApiFetch.mockResolvedValue(okResponse(EVENTS, '60'));
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        mockApiFetch.mockClear();
        fireEvent.click(screen.getByRole('button', { name: /next page/i }));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        expect(lastParams().get('offset')).toBe('25');
        expect(lastParams().get('limit')).toBe('25');
    });

    it('changes the limit and resets the offset when the page size changes', async () => {
        mockApiFetch.mockResolvedValue(okResponse(EVENTS, '600'));
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        fireEvent.click(screen.getByRole('button', { name: /next page/i }));
        await waitFor(() => {
            expect(lastParams().get('offset')).toBe('25');
        });

        mockApiFetch.mockClear();
        await chooseOption('Rows per page:', '100');

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        expect(lastParams().get('limit')).toBe('100');
        expect(lastParams().get('offset')).toBe('0');
    });

    it('falls back to the row count when X-Total-Count is absent', async () => {
        mockApiFetch.mockResolvedValue(okResponse(EVENTS, null));
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        expect(screen.getByText(/1.2 of 2/)).toBeInTheDocument();
    });

    it('reveals the formatted details and error text when expanded', async () => {
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        expect(screen.queryByText(/"before"/)).not.toBeInTheDocument();

        const expanders = screen.getAllByRole('button', {
            name: 'Show details',
        });
        fireEvent.click(expanders[0]);

        const details = await screen.findByTestId('audit-details-12');
        expect(details.textContent).toContain(
            JSON.stringify(EVENTS[0].details, null, 2),
        );

        fireEvent.click(expanders[1]);
        const denied = await screen.findByTestId('audit-details-11');
        expect(denied.textContent).toContain(
            'requires manage_groups permission',
        );
    });

    it('collapses an expanded row when the toggle is clicked again', async () => {
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        fireEvent.click(
            screen.getAllByRole('button', { name: 'Show details' })[0],
        );
        await screen.findByTestId('audit-details-12');

        fireEvent.click(
            screen.getByRole('button', { name: 'Hide details' }),
        );
        await waitFor(() => {
            expect(
                screen.queryByTestId('audit-details-12'),
            ).not.toBeInTheDocument();
        });
    });

    it('shows the actor IP as a tooltip on the actor name', async () => {
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        fireEvent.mouseOver(screen.getByText('alice'));

        expect(await screen.findByRole('tooltip')).toHaveTextContent(
            '192.0.2.5',
        );
    });

    it('renders the empty state when no events match', async () => {
        mockApiFetch.mockResolvedValue(okResponse([], '0'));
        renderWithTheme(<AdminAuditLog />);

        expect(
            await screen.findByText(
                'No audit events match the current filters.',
            ),
        ).toBeInTheDocument();
    });

    it('renders the server error message when the request fails', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(403, '{"error":"superuser access required"}'),
        );
        renderWithTheme(<AdminAuditLog />);

        expect(
            await screen.findByText('superuser access required'),
        ).toBeInTheDocument();
    });

    it('falls back sensibly for a sparse event', async () => {
        mockApiFetch.mockResolvedValue(okResponse([SPARSE_EVENT], '1'));
        renderWithTheme(<AdminAuditLog />);

        // An unparseable timestamp is shown verbatim rather than as
        // "Invalid Date".
        expect(await screen.findByText('not-a-timestamp')).toBeInTheDocument();
        // A target with no name falls back to its numeric id.
        expect(screen.getByText('#42')).toBeInTheDocument();
        expect(screen.getByText('failure')).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', { name: 'Show details' }));

        const details = await screen.findByTestId('audit-details-10');
        expect(details.textContent).toContain(
            'No further detail was recorded.',
        );
    });

    it('ignores a since value that cannot be parsed', async () => {
        renderWithTheme(<AdminAuditLog />);
        await screen.findByText('group.delete');

        fireEvent.change(screen.getByLabelText('Since'), {
            target: { value: 'not-a-date' },
        });
        mockApiFetch.mockClear();
        fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

        await waitFor(() => {
            expect(mockApiFetch).toHaveBeenCalled();
        });
        expect(lastParams().has('since')).toBe(false);
    });

    it('renders a non-JSON error body verbatim', async () => {
        mockApiFetch.mockResolvedValue(
            errorResponse(500, 'upstream failure'),
        );
        renderWithTheme(<AdminAuditLog />);

        expect(
            await screen.findByText('upstream failure'),
        ).toBeInTheDocument();
    });

    it('reports the status when the error body is empty', async () => {
        mockApiFetch.mockResolvedValue(errorResponse(502, ''));
        renderWithTheme(<AdminAuditLog />);

        expect(
            await screen.findByText('Request failed with status 502'),
        ).toBeInTheDocument();
    });

    it('ignores a stale response that resolves after a newer one', async () => {
        let resolveFirst: (response: Response) => void = () => undefined;
        const firstRequest = new Promise<Response>((resolve) => {
            resolveFirst = resolve;
        });
        mockApiFetch
            .mockReturnValueOnce(firstRequest)
            .mockResolvedValue(okResponse([SPARSE_EVENT], '1'));

        renderWithTheme(<AdminAuditLog />);

        // Start a second request whilst the first is still pending.
        fireEvent.change(screen.getByLabelText('Actor'), {
            target: { value: 'alice' },
        });
        fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
        expect(await screen.findByText('user.create')).toBeInTheDocument();

        // The first request now resolves, out of order, and must not
        // overwrite the newer rows or re-enter the loading state.
        resolveFirst(okResponse(EVENTS, '2'));
        await act(async () => {
            await firstRequest;
        });
        expect(mockApiFetch).toHaveBeenCalledTimes(2);

        expect(screen.getByText('user.create')).toBeInTheDocument();
        expect(screen.queryByText('group.delete')).not.toBeInTheDocument();
        expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    });

    it('renders a fallback message when the request throws', async () => {
        mockApiFetch.mockRejectedValue(new Error('network down'));
        renderWithTheme(<AdminAuditLog />);

        expect(await screen.findByText('network down')).toBeInTheDocument();
    });
});

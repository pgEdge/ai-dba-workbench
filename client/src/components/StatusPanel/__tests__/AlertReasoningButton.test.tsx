/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import AlertItem from '../AlertItem';
import GroupedAlertItem from '../GroupedAlertItem';
import AlertReasoningButton from '../AlertReasoningButton';
import { renderWithTheme } from '../../../test/renderWithTheme';
import { clearAlertReasoningCache } from '../../../hooks/useAlertReasoning';
import { ApiError } from '../../../utils/apiClient';
import type { TransformedAlert } from '../types';

const mockApiGet = vi.fn();
vi.mock('../../../utils/apiClient', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../../../utils/apiClient')>();
    return {
        ...actual,
        apiGet: (...args: unknown[]) => mockApiGet(...args),
    };
});

const LABEL = 'Why was this flagged?';

const anomalyAlert: TransformedAlert = {
    id: 42,
    severity: 'warning',
    title: 'Connection count anomaly',
    time: '2 min ago',
    server: 'server-1',
    connectionId: 7,
    alertType: 'anomaly',
};

const openPopover = () => {
    fireEvent.click(screen.getByRole('button', { name: LABEL }));
    return screen.getByRole('dialog', { name: 'Detection reasoning' });
};

describe('AlertReasoningButton', () => {
    beforeEach(() => {
        mockApiGet.mockReset();
        clearAlertReasoningCache();
    });

    it('appears only on non-system anomaly alerts', () => {
        const { unmount } = renderWithTheme(<AlertItem alert={anomalyAlert} />);
        expect(screen.getByRole('button', { name: LABEL })).toBeInTheDocument();
        unmount();

        const threshold = renderWithTheme(
            <AlertItem alert={{ ...anomalyAlert, alertType: 'threshold' }} />,
        );
        expect(screen.queryByRole('button', { name: LABEL })).not.toBeInTheDocument();
        threshold.unmount();

        renderWithTheme(<AlertItem alert={{ ...anomalyAlert, isSystem: true }} />);
        expect(screen.queryByRole('button', { name: LABEL })).not.toBeInTheDocument();
    });

    it('fetches only when clicked, then shows the text and caption', async () => {
        mockApiGet.mockResolvedValue({
            alert_id: 42,
            status: 'available',
            source: 'decision',
            reasoning: 'Connections tripled.\n<b>not bold</b>',
        });
        renderWithTheme(<AlertItem alert={anomalyAlert} />);
        expect(mockApiGet).not.toHaveBeenCalled();

        const button = screen.getByRole('button', { name: LABEL });
        expect(button).toHaveAttribute('aria-expanded', 'false');
        const dialog = openPopover();
        expect(button).toHaveAttribute('aria-expanded', 'true');
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/alerts/reasoning?alert_id=42');
        expect(within(dialog).getByLabelText('Loading detection reasoning')).toBeInTheDocument();

        const text = await within(dialog).findByText(/Connections tripled/);
        // LLM output is rendered as text, never as markup.
        expect(text.textContent).toContain('<b>not bold</b>');
        expect(dialog.querySelector('b')).toBeNull();
        expect(within(dialog).getByText(/generated after the decision/)).toBeInTheDocument();
    });

    it('omits the caption for reasoning-model text and does not refetch on reopen', async () => {
        mockApiGet.mockResolvedValue({
            alert_id: 42,
            status: 'available',
            source: 'reasoning',
            reasoning: 'Stored explanation.',
        });
        renderWithTheme(<AlertItem alert={anomalyAlert} />);
        let dialog = openPopover();
        await within(dialog).findByText('Stored explanation.');
        expect(within(dialog).queryByText(/generated after the decision/)).not.toBeInTheDocument();

        fireEvent.keyDown(dialog, { key: 'Escape' });
        await waitFor(() => {
            expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
        });

        dialog = openPopover();
        expect(within(dialog).getByText('Stored explanation.')).toBeInTheDocument();
        expect(mockApiGet).toHaveBeenCalledTimes(1);
    });

    it('offers a retry when the reasoning is unavailable', async () => {
        mockApiGet
            .mockResolvedValueOnce({ alert_id: 42, status: 'unavailable' })
            .mockResolvedValueOnce({
                alert_id: 42,
                status: 'available',
                reasoning: 'Second time lucky.',
            });
        renderWithTheme(<AlertItem alert={anomalyAlert} />);
        const dialog = openPopover();
        await within(dialog).findByText(/not available right now/);
        expect(within(dialog).getByText(/Try again in a minute, or ask an administrator to configure an LLM/)).toBeInTheDocument();

        fireEvent.click(within(dialog).getByRole('button', { name: 'Retry' }));
        expect(await within(dialog).findByText('Second time lucky.')).toBeInTheDocument();
        expect(mockApiGet).toHaveBeenCalledTimes(2);
    });

    it('offers a retry after a server error', async () => {
        mockApiGet.mockRejectedValue(new ApiError('boom', 500));
        renderWithTheme(<AlertItem alert={anomalyAlert} />);
        const dialog = openPopover();
        await within(dialog).findByText(/could not be loaded/);
        expect(within(dialog).getByRole('button', { name: 'Retry' })).toBeInTheDocument();
    });

    it('offers no retry when the alert is not found', async () => {
        mockApiGet.mockRejectedValue(new ApiError('nope', 404));
        renderWithTheme(<AlertItem alert={anomalyAlert} />);
        const dialog = openPopover();
        await within(dialog).findByText(/no longer exists/);
        expect(within(dialog).queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument();
    });

    it('explains when no reasoning applies', async () => {
        mockApiGet.mockResolvedValue({ alert_id: 42, status: 'not_applicable' });
        renderWithTheme(<AlertItem alert={anomalyAlert} />);
        const dialog = openPopover();
        expect(
            await within(dialog).findByText('No detection reasoning is recorded for this alert.'),
        ).toBeInTheDocument();
    });

    it('keeps clicks inside the popover from reaching the row', async () => {
        mockApiGet.mockResolvedValue({ alert_id: 42, status: 'not_applicable' });
        const onRowClick = vi.fn();
        renderWithTheme(
            <div onClick={onRowClick}>
                <AlertReasoningButton alertId={42} />
            </div>,
        );
        const dialog = openPopover();
        await within(dialog).findByText(/No detection reasoning/);
        fireEvent.click(dialog);
        expect(onRowClick).not.toHaveBeenCalled();
    });

    it('renders a compact button on grouped instance rows', async () => {
        mockApiGet.mockResolvedValue({ alert_id: 43, status: 'not_applicable' });
        renderWithTheme(
            <GroupedAlertItem
                title="Connection count anomaly"
                alerts={[
                    { ...anomalyAlert, id: 43 },
                    { ...anomalyAlert, id: 44, alertType: 'threshold' },
                ]}
            />,
        );
        const buttons = screen.getAllByRole('button', { name: LABEL });
        expect(buttons).toHaveLength(1);
        fireEvent.click(buttons[0]);
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/alerts/reasoning?alert_id=43');
        const dialog = screen.getByRole('dialog', { name: 'Detection reasoning' });
        await within(dialog).findByText(/No detection reasoning/);
    });
});

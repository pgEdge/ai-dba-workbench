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
import { describe, it, expect, vi, beforeEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';
import RemoveServerDialog from '../RemoveServerDialog';
import type { ClusterServerInfo } from '../../ServerDialog/ServerDialog.types';

const createMockServer = (
    overrides: Partial<ClusterServerInfo> = {},
): ClusterServerInfo => ({
    id: 1,
    name: 'server-1',
    host: 'host1.example.com',
    port: 5432,
    status: 'online',
    role: 'binary_primary',
    ...overrides,
});

describe('RemoveServerDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('renders nothing when server is null', () => {
        const { container } = renderWithTheme(
            <RemoveServerDialog
                server={null}
                clusterName="test-cluster"
                removing={false}
                onConfirm={vi.fn()}
                onCancel={vi.fn()}
            />,
        );

        expect(
            screen.queryByText('Remove server from cluster'),
        ).not.toBeInTheDocument();
        expect(container.querySelector('[role="dialog"]')).not.toBeInTheDocument();
    });

    it('renders dialog when server is provided', () => {
        const server = createMockServer();

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="test-cluster"
                removing={false}
                onConfirm={vi.fn()}
                onCancel={vi.fn()}
            />,
        );

        expect(
            screen.getByText('Remove server from cluster'),
        ).toBeInTheDocument();
        expect(screen.getByText('server-1')).toBeInTheDocument();
        expect(screen.getByText('test-cluster')).toBeInTheDocument();
    });

    it('displays server name and cluster name in content', () => {
        const server = createMockServer({ name: 'my-db-server' });

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="production-cluster"
                removing={false}
                onConfirm={vi.fn()}
                onCancel={vi.fn()}
            />,
        );

        expect(screen.getByText('my-db-server')).toBeInTheDocument();
        expect(screen.getByText('production-cluster')).toBeInTheDocument();
        expect(
            screen.getByText(/will become standalone/i),
        ).toBeInTheDocument();
    });

    it('calls onCancel when Cancel button is clicked', () => {
        const onCancel = vi.fn();
        const server = createMockServer();

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="test-cluster"
                removing={false}
                onConfirm={vi.fn()}
                onCancel={onCancel}
            />,
        );

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        expect(onCancel).toHaveBeenCalledTimes(1);
    });

    it('calls onConfirm when Remove button is clicked', () => {
        const onConfirm = vi.fn();
        const server = createMockServer();

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="test-cluster"
                removing={false}
                onConfirm={onConfirm}
                onCancel={vi.fn()}
            />,
        );

        fireEvent.click(screen.getByRole('button', { name: 'Remove' }));

        expect(onConfirm).toHaveBeenCalledTimes(1);
    });

    it('disables Cancel button when removing is true', () => {
        const server = createMockServer();

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="test-cluster"
                removing={true}
                onConfirm={vi.fn()}
                onCancel={vi.fn()}
            />,
        );

        expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    });

    it('disables Remove button when removing is true', () => {
        const server = createMockServer();

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="test-cluster"
                removing={true}
                onConfirm={vi.fn()}
                onCancel={vi.fn()}
            />,
        );

        const removeButton = screen.getByRole('button', { name: '' });
        expect(removeButton).toBeDisabled();
    });

    it('shows loading spinner when removing is true', () => {
        const server = createMockServer();

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="test-cluster"
                removing={true}
                onConfirm={vi.fn()}
                onCancel={vi.fn()}
            />,
        );

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
    });

    it('shows Remove text when not removing', () => {
        const server = createMockServer();

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="test-cluster"
                removing={false}
                onConfirm={vi.fn()}
                onCancel={vi.fn()}
            />,
        );

        expect(
            screen.getByRole('button', { name: 'Remove' }),
        ).toBeInTheDocument();
        expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    });

    it('displays relationship deletion warning', () => {
        const server = createMockServer();

        renderWithTheme(
            <RemoveServerDialog
                server={server}
                clusterName="test-cluster"
                removing={false}
                onConfirm={vi.fn()}
                onCancel={vi.fn()}
            />,
        );

        expect(
            screen.getByText(
                /All relationships involving this server within the cluster will be deleted/i,
            ),
        ).toBeInTheDocument();
    });
});

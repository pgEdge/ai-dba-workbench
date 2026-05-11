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
import renderWithTheme from '../../test/renderWithTheme';
import AddMenu from '../AddMenu';

// Mock useAuth so the test can vary the user's permissions without
// having to mount the full AuthProvider and stub the user info API.
const mockHasPermission = vi.fn<(perm: string) => boolean>(() => true);

vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({
        hasPermission: mockHasPermission,
    }),
}));

interface RenderOptions {
    open?: boolean;
    onAddServer?: () => void;
    onAddCluster?: () => void;
    onAddGroup?: () => void;
    onClose?: () => void;
}

const renderAddMenu = (options: RenderOptions = {}) => {
    const anchor = document.createElement('div');
    document.body.appendChild(anchor);

    const props = {
        anchorEl: anchor,
        open: options.open ?? true,
        onClose: options.onClose ?? vi.fn(),
        onAddServer: options.onAddServer,
        onAddCluster: options.onAddCluster,
        onAddGroup: options.onAddGroup,
    };

    return renderWithTheme(<AddMenu {...props} />);
};

describe('AddMenu', () => {
    beforeEach(() => {
        mockHasPermission.mockReset();
        mockHasPermission.mockReturnValue(true);
    });

    describe('with manage_connections permission', () => {
        beforeEach(() => {
            mockHasPermission.mockImplementation(
                (perm: string) => perm === 'manage_connections',
            );
        });

        it('renders Add Server, Add Cluster, and Add Cluster Group', () => {
            renderAddMenu();

            expect(screen.getByText('Add Server')).toBeInTheDocument();
            expect(screen.getByText('Add Cluster')).toBeInTheDocument();
            expect(screen.getByText('Add Cluster Group')).toBeInTheDocument();
        });

        it('queries for the manage_connections permission', () => {
            renderAddMenu();

            expect(mockHasPermission).toHaveBeenCalledWith('manage_connections');
        });

        it('invokes onAddServer and closes when Add Server is clicked', () => {
            const onAddServer = vi.fn();
            const onClose = vi.fn();
            renderAddMenu({ onAddServer, onClose });

            fireEvent.click(screen.getByText('Add Server'));

            expect(onAddServer).toHaveBeenCalledTimes(1);
            expect(onClose).toHaveBeenCalledTimes(1);
        });

        it('invokes onAddCluster and closes when Add Cluster is clicked', () => {
            const onAddCluster = vi.fn();
            const onClose = vi.fn();
            renderAddMenu({ onAddCluster, onClose });

            fireEvent.click(screen.getByText('Add Cluster'));

            expect(onAddCluster).toHaveBeenCalledTimes(1);
            expect(onClose).toHaveBeenCalledTimes(1);
        });

        it('invokes onAddGroup and closes when Add Cluster Group is clicked', () => {
            const onAddGroup = vi.fn();
            const onClose = vi.fn();
            renderAddMenu({ onAddGroup, onClose });

            fireEvent.click(screen.getByText('Add Cluster Group'));

            expect(onAddGroup).toHaveBeenCalledTimes(1);
            expect(onClose).toHaveBeenCalledTimes(1);
        });

        it('closes even when callbacks are not provided', () => {
            const onClose = vi.fn();
            renderAddMenu({ onClose });

            fireEvent.click(screen.getByText('Add Server'));
            fireEvent.click(screen.getByText('Add Cluster'));
            fireEvent.click(screen.getByText('Add Cluster Group'));

            expect(onClose).toHaveBeenCalledTimes(3);
        });
    });

    describe('without manage_connections permission', () => {
        beforeEach(() => {
            mockHasPermission.mockReturnValue(false);
        });

        it('renders only Add Server', () => {
            renderAddMenu();

            expect(screen.getByText('Add Server')).toBeInTheDocument();
            expect(screen.queryByText('Add Cluster')).not.toBeInTheDocument();
            expect(
                screen.queryByText('Add Cluster Group'),
            ).not.toBeInTheDocument();
            expect(screen.queryByRole('separator')).not.toBeInTheDocument();
        });

        it('still allows Add Server to fire its callback', () => {
            const onAddServer = vi.fn();
            const onClose = vi.fn();
            renderAddMenu({ onAddServer, onClose });

            fireEvent.click(screen.getByText('Add Server'));

            expect(onAddServer).toHaveBeenCalledTimes(1);
            expect(onClose).toHaveBeenCalledTimes(1);
        });
    });

    describe('loading or unauthenticated state', () => {
        it('hides gated items when hasPermission always returns false', () => {
            // Simulates the AuthProvider's initial state where the user
            // has not yet loaded and adminPermissions is the empty array,
            // so hasPermission returns false for every probe.
            mockHasPermission.mockReturnValue(false);

            renderAddMenu();

            expect(screen.getByText('Add Server')).toBeInTheDocument();
            expect(screen.queryByText('Add Cluster')).not.toBeInTheDocument();
            expect(
                screen.queryByText('Add Cluster Group'),
            ).not.toBeInTheDocument();
            expect(screen.queryByRole('separator')).not.toBeInTheDocument();
        });
    });

    describe('open state', () => {
        it('does not render menu items when open is false', () => {
            renderAddMenu({ open: false });

            expect(screen.queryByText('Add Server')).not.toBeInTheDocument();
            expect(screen.queryByText('Add Cluster')).not.toBeInTheDocument();
            expect(
                screen.queryByText('Add Cluster Group'),
            ).not.toBeInTheDocument();
        });
    });
});

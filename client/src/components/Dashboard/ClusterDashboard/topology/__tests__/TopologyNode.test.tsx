/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Component coverage for TopologyNode. The node card is a pure
 * presentation component: it renders a status dot, server name, and
 * RolePill, and forwards click and keyboard events to the parent. The
 * tests exercise the four `getStatusDotColor` branches (online,
 * warning, offline, default), the optional highlight border, and the
 * Enter/Space keyboard handlers that mirror the click behaviour for
 * keyboard users.
 */

import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import TopologyNode from '../TopologyNode';
import type { TopoNode } from '../types';
import { renderWithTheme } from '../../../../../test/renderWithTheme';
import type { ClusterServer } from '../../../../../contexts/ClusterDataContext';

const baseServer = {
    id: 1,
    name: 'srv-1',
    role: 'binary_primary',
    status: 'online',
} as unknown as ClusterServer;

const makeNode = (overrides: Partial<TopoNode> = {}): TopoNode => ({
    id: 1,
    name: 'srv-1',
    role: 'binary_primary',
    status: 'online',
    x: 10,
    y: 20,
    server: baseServer,
    ...overrides,
});

describe('TopologyNode', () => {
    it('renders the server name and a button with the expected aria-label', () => {
        renderWithTheme(
            <TopologyNode
                node={makeNode({ name: 'leader' })}
                nodeWidth={120}
                onClick={vi.fn()}
            />,
        );
        expect(screen.getByText('leader')).toBeInTheDocument();
        expect(
            screen.getByRole('button', { name: /select server leader/i }),
        ).toBeInTheDocument();
    });

    it('positions the card according to node x/y coordinates and width', () => {
        renderWithTheme(
            <TopologyNode
                node={makeNode({ x: 42, y: 99 })}
                nodeWidth={150}
                onClick={vi.fn()}
            />,
        );
        const button = screen.getByRole('button', { name: /select server/i });
        expect(button).toHaveStyle({
            left: '42px',
            top: '99px',
            width: '150px',
        });
    });

    it.each([
        ['online', 'success'],
        ['warning', 'warning'],
        ['offline', 'error'],
        ['unknown', 'grey'],
    ])('renders a status dot for status=%s', (status, _palette) => {
        renderWithTheme(
            <TopologyNode
                node={makeNode({ status, name: 'srv' })}
                nodeWidth={120}
                onClick={vi.fn()}
            />,
        );
        // The dot exposes its status via aria-label.
        expect(
            screen.getByLabelText(`Status: ${status}`),
        ).toBeInTheDocument();
    });

    it('invokes onClick with the node when clicked', async () => {
        const onClick = vi.fn();
        const node = makeNode({ name: 'clicked' });
        const user = userEvent.setup();
        renderWithTheme(
            <TopologyNode node={node} nodeWidth={120} onClick={onClick} />,
        );
        await user.click(
            screen.getByRole('button', { name: /select server clicked/i }),
        );
        expect(onClick).toHaveBeenCalledTimes(1);
        expect(onClick).toHaveBeenCalledWith(node);
    });

    it('triggers onClick when the user presses Enter while focused', async () => {
        const onClick = vi.fn();
        const node = makeNode();
        const user = userEvent.setup();
        renderWithTheme(
            <TopologyNode node={node} nodeWidth={120} onClick={onClick} />,
        );
        const button = screen.getByRole('button', { name: /select server/i });
        button.focus();
        await user.keyboard('{Enter}');
        expect(onClick).toHaveBeenCalledTimes(1);
        expect(onClick).toHaveBeenCalledWith(node);
    });

    it('triggers onClick when the user presses Space while focused', async () => {
        const onClick = vi.fn();
        const node = makeNode();
        const user = userEvent.setup();
        renderWithTheme(
            <TopologyNode node={node} nodeWidth={120} onClick={onClick} />,
        );
        const button = screen.getByRole('button', { name: /select server/i });
        button.focus();
        await user.keyboard(' ');
        expect(onClick).toHaveBeenCalledTimes(1);
    });

    it('ignores other keys (Tab, Escape) for activation', async () => {
        const onClick = vi.fn();
        const user = userEvent.setup();
        renderWithTheme(
            <TopologyNode
                node={makeNode()}
                nodeWidth={120}
                onClick={onClick}
            />,
        );
        const button = screen.getByRole('button', { name: /select server/i });
        button.focus();
        await user.keyboard('{Escape}');
        await user.keyboard('a');
        expect(onClick).not.toHaveBeenCalled();
    });

    it('renders distinct emotion classes for highlight vs non-highlight', () => {
        // jsdom does not parse the CSS that emotion injects into <style>
        // tags, so getComputedStyle would return empty strings for the
        // sx-driven border/box-shadow rules. Comparing the generated
        // class names is enough to confirm the highlight branch took a
        // different code path through the conditional sx expression.
        const { unmount } = renderWithTheme(
            <TopologyNode
                node={makeNode()}
                nodeWidth={120}
                onClick={vi.fn()}
            />,
        );
        const plainClass = screen
            .getByRole('button', { name: /select server/i })
            .className;
        unmount();

        renderWithTheme(
            <TopologyNode
                node={makeNode()}
                nodeWidth={120}
                onClick={vi.fn()}
                highlight={true}
            />,
        );
        const highlightedClass = screen
            .getByRole('button', { name: /select server/i })
            .className;

        expect(highlightedClass).not.toBe(plainClass);
    });
});

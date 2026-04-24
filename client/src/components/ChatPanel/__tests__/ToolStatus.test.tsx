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
import { screen } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ToolStatus, { type ToolActivity } from '../ToolStatus';
import { renderWithTheme } from '../../../test/renderWithTheme';

// Mock toolDisplayNames to have predictable output
vi.mock('../../../utils/toolDisplayNames', () => ({
    getToolDisplayName: (name: string) => {
        const displayNames: Record<string, string> = {
            get_server_info: 'Server Info',
            run_query: 'Run Query',
            analyze_query: 'Analyze Query',
        };
        return displayNames[name] || name;
    },
}));

describe('ToolStatus', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('rendering', () => {
        it('renders nothing when tools array is empty', () => {
            const { container } = renderWithTheme(<ToolStatus tools={[]} />);
            expect(container.firstChild).toBeNull();
        });

        it('renders nothing when tools is undefined', () => {
            const { container } = renderWithTheme(
                <ToolStatus tools={undefined as unknown as ToolActivity[]} />
            );
            expect(container.firstChild).toBeNull();
        });

        it('renders tool chips for each tool', () => {
            const tools: ToolActivity[] = [
                { name: 'get_server_info', status: 'completed' },
                { name: 'run_query', status: 'running' },
            ];
            renderWithTheme(<ToolStatus tools={tools} />);

            expect(screen.getByText('Server Info')).toBeInTheDocument();
            expect(screen.getByText('Run Query')).toBeInTheDocument();
        });
    });

    describe('status icons', () => {
        it('shows spinner for running status', () => {
            const tools: ToolActivity[] = [
                { name: 'get_server_info', status: 'running' },
            ];
            renderWithTheme(<ToolStatus tools={tools} />);

            expect(screen.getByLabelText('Running')).toBeInTheDocument();
        });

        it('shows check icon for completed status', () => {
            const tools: ToolActivity[] = [
                { name: 'get_server_info', status: 'completed' },
            ];
            renderWithTheme(<ToolStatus tools={tools} />);

            expect(screen.getByTestId('CheckCircleIcon')).toBeInTheDocument();
        });

        it('shows warning icon for error status', () => {
            const tools: ToolActivity[] = [
                { name: 'get_server_info', status: 'error' },
            ];
            renderWithTheme(<ToolStatus tools={tools} />);

            expect(screen.getByTestId('WarningIcon')).toBeInTheDocument();
        });
    });

    describe('multiple tools', () => {
        it('renders multiple tools with different statuses', () => {
            const tools: ToolActivity[] = [
                { name: 'get_server_info', status: 'completed' },
                { name: 'run_query', status: 'running' },
                { name: 'analyze_query', status: 'error' },
            ];
            renderWithTheme(<ToolStatus tools={tools} />);

            expect(screen.getByText('Server Info')).toBeInTheDocument();
            expect(screen.getByText('Run Query')).toBeInTheDocument();
            expect(screen.getByText('Analyze Query')).toBeInTheDocument();

            expect(screen.getByTestId('CheckCircleIcon')).toBeInTheDocument();
            expect(screen.getByLabelText('Running')).toBeInTheDocument();
            expect(screen.getByTestId('WarningIcon')).toBeInTheDocument();
        });

        it('handles duplicate tool names', () => {
            const tools: ToolActivity[] = [
                { name: 'run_query', status: 'completed' },
                { name: 'run_query', status: 'running' },
            ];
            renderWithTheme(<ToolStatus tools={tools} />);

            const runQueryChips = screen.getAllByText('Run Query');
            expect(runQueryChips).toHaveLength(2);
        });
    });

    describe('display names', () => {
        it('uses display name from toolDisplayNames', () => {
            const tools: ToolActivity[] = [
                { name: 'get_server_info', status: 'completed' },
            ];
            renderWithTheme(<ToolStatus tools={tools} />);

            expect(screen.getByText('Server Info')).toBeInTheDocument();
        });

        it('falls back to raw name for unknown tools', () => {
            const tools: ToolActivity[] = [
                { name: 'unknown_tool', status: 'completed' },
            ];
            renderWithTheme(<ToolStatus tools={tools} />);

            expect(screen.getByText('unknown_tool')).toBeInTheDocument();
        });
    });
});

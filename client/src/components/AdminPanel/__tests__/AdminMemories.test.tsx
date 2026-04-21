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
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';

const mockApiGet = vi.fn();
const mockApiDelete = vi.fn();
const mockApiPatch = vi.fn();

vi.mock('../../../utils/apiClient', () => ({
    apiGet: (...args: unknown[]) => mockApiGet(...args),
    apiDelete: (...args: unknown[]) => mockApiDelete(...args),
    apiPatch: (...args: unknown[]) => mockApiPatch(...args),
}));

import AdminMemories from '../AdminMemories';

const MOCK_MEMORIES = [
    {
        id: 1,
        username: 'admin',
        scope: 'system',
        category: 'optimization',
        content: 'Always use EXPLAIN ANALYZE for query optimization',
        pinned: true,
        model_name: 'claude-3',
        created_at: '2024-01-15T10:30:00Z',
        updated_at: '2024-01-15T10:30:00Z',
    },
    {
        id: 2,
        username: 'testuser',
        scope: 'user',
        category: 'best-practices',
        content: 'Use connection pooling for high-concurrency applications. This is a very long piece of content that should be truncated in the display to prevent the table from becoming too wide and maintain readability.',
        pinned: false,
        model_name: 'claude-3',
        created_at: '2024-01-16T14:20:00Z',
        updated_at: '2024-01-16T14:20:00Z',
    },
    {
        id: 3,
        username: 'admin',
        scope: 'system',
        category: '',
        content: 'Vacuum tables regularly to prevent bloat',
        pinned: false,
        model_name: 'claude-3',
        created_at: '2024-01-17T09:00:00Z',
        updated_at: '2024-01-17T09:00:00Z',
    },
];

describe('AdminMemories', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('displays loading state initially', () => {
        mockApiGet.mockReturnValue(new Promise(() => {}));

        renderWithTheme(<AdminMemories />);

        expect(screen.getByRole('progressbar')).toBeInTheDocument();
        expect(screen.getByLabelText('Loading memories')).toBeInTheDocument();
    });

    it('renders memories from API', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText('Memories')).toBeInTheDocument();
        });

        // Check table headers
        expect(screen.getByText('Content')).toBeInTheDocument();
        expect(screen.getByText('Category')).toBeInTheDocument();
        expect(screen.getByText('Scope')).toBeInTheDocument();
        expect(screen.getByText('Pinned')).toBeInTheDocument();
        expect(screen.getByText('Created')).toBeInTheDocument();
        expect(screen.getByText('Actions')).toBeInTheDocument();

        // Check that memories are displayed
        expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        expect(screen.getByText(/Vacuum tables regularly/)).toBeInTheDocument();

        // Verify API was called correctly
        expect(mockApiGet).toHaveBeenCalledWith('/api/v1/memories?limit=1000');
    });

    it('truncates long content with ellipsis', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText('Memories')).toBeInTheDocument();
        });

        // The second memory has long content that should be truncated
        const truncatedContent = screen.getByText(/Use connection pooling/);
        expect(truncatedContent.textContent).toContain('\u2026');
    });

    it('displays scope chips correctly', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getAllByText('System').length).toBeGreaterThan(0);
        });

        expect(screen.getByText('User')).toBeInTheDocument();
    });

    it('displays categories in the table', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText('optimization')).toBeInTheDocument();
        });

        expect(screen.getByText('best-practices')).toBeInTheDocument();
        // Empty category shows as dash
        expect(screen.getByText('-')).toBeInTheDocument();
    });

    it('shows empty state when no memories exist', async () => {
        mockApiGet.mockResolvedValue({ memories: [] });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText('No memories found.')).toBeInTheDocument();
        });
    });

    it('displays error message when API fails', async () => {
        mockApiGet.mockRejectedValue(new Error('Network error'));

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText('Network error')).toBeInTheDocument();
        });
    });

    it('toggles pinned state optimistically', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });
        mockApiPatch.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        });

        // Find the pinned switches - first one should be checked (pinned: true)
        const pinnedSwitches = screen.getAllByRole('checkbox', { name: /toggle pinned/i });
        expect(pinnedSwitches[0]).toBeChecked();

        await user.click(pinnedSwitches[0]);

        await waitFor(() => {
            expect(mockApiPatch).toHaveBeenCalledWith(
                '/api/v1/memories/1',
                { pinned: false }
            );
        });
    });

    it('displays error when pin toggle fails', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });
        mockApiPatch.mockRejectedValue(new Error('Patch failed'));
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        });

        const pinnedSwitches = screen.getAllByRole('checkbox', { name: /toggle pinned/i });
        await user.click(pinnedSwitches[0]);

        // Error message should be displayed
        await waitFor(() => {
            expect(screen.getByText('Patch failed')).toBeInTheDocument();
        });
    });

    it('opens delete confirmation dialog when delete button is clicked', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        });

        const deleteButtons = screen.getAllByRole('button', { name: /delete memory/i });
        await user.click(deleteButtons[0]);

        await waitFor(() => {
            expect(screen.getByText('Delete Memory')).toBeInTheDocument();
        });

        expect(screen.getByText('Are you sure you want to delete this memory?')).toBeInTheDocument();
    });

    it('closes delete dialog when Cancel is clicked', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        });

        const deleteButtons = screen.getAllByRole('button', { name: /delete memory/i });
        await user.click(deleteButtons[0]);

        await waitFor(() => {
            expect(screen.getByText('Delete Memory')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /Cancel/i }));

        await waitFor(() => {
            expect(screen.queryByText('Delete Memory')).not.toBeInTheDocument();
        });
    });

    it('deletes memory when confirmed', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });
        mockApiDelete.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        });

        const deleteButtons = screen.getAllByRole('button', { name: /delete memory/i });
        await user.click(deleteButtons[0]);

        await waitFor(() => {
            expect(screen.getByText('Delete Memory')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /^Delete$/i }));

        await waitFor(() => {
            expect(mockApiDelete).toHaveBeenCalledWith('/api/v1/memories/1');
        });
    });

    it('displays error when delete fails', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });
        mockApiDelete.mockRejectedValue(new Error('Delete failed'));
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        });

        const deleteButtons = screen.getAllByRole('button', { name: /delete memory/i });
        await user.click(deleteButtons[0]);

        await waitFor(() => {
            expect(screen.getByText('Delete Memory')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /^Delete$/i }));

        await waitFor(() => {
            expect(screen.getByText('Delete failed')).toBeInTheDocument();
        });
    });

    it('refreshes memories list after successful delete', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });
        mockApiDelete.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        });

        const deleteButtons = screen.getAllByRole('button', { name: /delete memory/i });
        await user.click(deleteButtons[0]);

        await waitFor(() => {
            expect(screen.getByText('Delete Memory')).toBeInTheDocument();
        });

        await user.click(screen.getByRole('button', { name: /^Delete$/i }));

        await waitFor(() => {
            // fetchMemories is called again after delete
            expect(mockApiGet).toHaveBeenCalledTimes(2);
        });
    });

    it('displays delete button for each memory row', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Always use EXPLAIN ANALYZE/)).toBeInTheDocument();
        });

        const deleteButtons = screen.getAllByRole('button', { name: /delete memory/i });
        expect(deleteButtons).toHaveLength(3);
    });

    it('displays formatted dates', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Jan 15, 2024/)).toBeInTheDocument();
        });
    });

    it('handles pinning an unpinned memory', async () => {
        mockApiGet.mockResolvedValue({ memories: MOCK_MEMORIES });
        mockApiPatch.mockResolvedValue({});
        const user = userEvent.setup({ delay: null });

        renderWithTheme(<AdminMemories />);

        await waitFor(() => {
            expect(screen.getByText(/Use connection pooling/)).toBeInTheDocument();
        });

        // Second memory is unpinned
        const pinnedSwitches = screen.getAllByRole('checkbox', { name: /toggle pinned/i });
        expect(pinnedSwitches[1]).not.toBeChecked();

        await user.click(pinnedSwitches[1]);

        await waitFor(() => {
            expect(mockApiPatch).toHaveBeenCalledWith(
                '/api/v1/memories/2',
                { pinned: true }
            );
        });
    });
});

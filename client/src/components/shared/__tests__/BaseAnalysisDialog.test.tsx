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
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import { Psychology as PsychologyIcon } from '@mui/icons-material';
import BaseAnalysisDialog from '../BaseAnalysisDialog';
import { renderWithTheme } from '../../../test/renderWithTheme';

// Mock the MarkdownContent component to simplify testing
vi.mock('../MarkdownContent', () => ({
    MarkdownContent: ({ content }: { content: string }) => (
        <div data-testid="markdown-content">{content}</div>
    ),
    AnalysisSkeleton: () => (
        <div data-testid="analysis-skeleton">Loading skeleton</div>
    ),
}));

// Mock the style functions
vi.mock('../MarkdownExports', () => ({
    sxErrorFlexRow: { display: 'flex', gap: 1 },
    getIconBoxSx: () => ({ display: 'flex' }),
    getIconColorSx: () => ({ color: 'primary.main' }),
    getLoadingBannerSx: () => ({ display: 'flex', p: 2 }),
    getPulseDotSx: () => ({ width: 8, height: 8 }),
    getLoadingTextSx: () => ({ fontSize: '0.875rem' }),
    getErrorBoxSx: () => ({ p: 2, bgcolor: 'error.light' }),
    getErrorTitleSx: () => ({ fontWeight: 600 }),
    getAnalysisBoxSx: () => ({ p: 2 }),
    getDownloadButtonSx: () => ({ color: 'text.secondary' }),
}));

// Mock SlideTransition with forwardRef to avoid MUI warnings
vi.mock('../SlideTransition', () => ({
    default: React.forwardRef(function MockSlideTransition(
        { children }: { children: React.ReactElement },
        _ref: React.Ref<unknown>,
    ) {
        return children;
    }),
}));

describe('BaseAnalysisDialog', () => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
        title: 'Test Analysis',
        icon: <PsychologyIcon data-testid="dialog-icon" />,
        toolLabels: ['Tool A', 'Tool B', 'Tool C'],
        analysis: null as string | null,
        loading: false,
        error: null as string | null,
        progressMessage: 'Analyzing...',
        activeTools: [] as string[],
        onDownload: vi.fn(),
        onReset: vi.fn(),
        toolbarContent: null as React.ReactNode,
        markdownContentProps: {
            isDark: false,
            connectionId: 1,
            databaseName: 'testdb',
            serverName: 'test-server',
        },
    };

    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('rendering', () => {
        it('renders nothing visible when closed', () => {
            renderWithTheme(
                <BaseAnalysisDialog {...defaultProps} open={false} />
            );
            // Dialog should not be visible when closed
            expect(screen.queryByText('Test Analysis')).not.toBeInTheDocument();
        });

        it('shows the title and icon when open', () => {
            renderWithTheme(<BaseAnalysisDialog {...defaultProps} />);
            expect(screen.getByText('Test Analysis')).toBeInTheDocument();
            expect(screen.getByTestId('dialog-icon')).toBeInTheDocument();
        });

        it('renders close button with correct aria-label', () => {
            renderWithTheme(<BaseAnalysisDialog {...defaultProps} />);
            const closeButton = screen.getByRole('button', {
                name: /close analysis/i,
            });
            expect(closeButton).toBeInTheDocument();
        });

        it('renders download button with correct aria-label', () => {
            renderWithTheme(<BaseAnalysisDialog {...defaultProps} />);
            const downloadButton = screen.getByRole('button', {
                name: /download analysis/i,
            });
            expect(downloadButton).toBeInTheDocument();
        });
    });

    describe('loading state', () => {
        it('shows loading skeleton when loading is true', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    progressMessage="Fetching data..."
                />
            );
            expect(screen.getByTestId('analysis-skeleton')).toBeInTheDocument();
        });

        it('shows progress message when loading', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    progressMessage="Processing query..."
                />
            );
            expect(screen.getByText('Processing query...')).toBeInTheDocument();
        });

        it('renders all tool badges during loading', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    toolLabels={['Tool A', 'Tool B', 'Tool C']}
                />
            );
            expect(screen.getByText('Tool A')).toBeInTheDocument();
            expect(screen.getByText('Tool B')).toBeInTheDocument();
            expect(screen.getByText('Tool C')).toBeInTheDocument();
        });

        it('disables download button when loading', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    analysis="Some analysis"
                />
            );
            const downloadButton = screen.getByRole('button', {
                name: /download analysis/i,
            });
            expect(downloadButton).toBeDisabled();
        });
    });

    describe('active tools styling', () => {
        it('renders active tool badges with active styling', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    toolLabels={['Tool A', 'Tool B', 'Tool C']}
                    activeTools={['Tool B']}
                />
            );
            // All tools should be rendered
            expect(screen.getByText('Tool A')).toBeInTheDocument();
            expect(screen.getByText('Tool B')).toBeInTheDocument();
            expect(screen.getByText('Tool C')).toBeInTheDocument();
        });

        it('handles empty activeTools array', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    activeTools={[]}
                />
            );
            // Should render without errors
            expect(screen.getByText('Tool A')).toBeInTheDocument();
        });

        it('handles multiple active tools', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    activeTools={['Tool A', 'Tool C']}
                />
            );
            expect(screen.getByText('Tool A')).toBeInTheDocument();
            expect(screen.getByText('Tool C')).toBeInTheDocument();
        });
    });

    describe('error state', () => {
        it('shows error message when error is present', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    error="Connection failed"
                />
            );
            expect(screen.getByText('Analysis Failed')).toBeInTheDocument();
            expect(screen.getByText('Connection failed')).toBeInTheDocument();
        });

        it('does not show error when loading', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    error="Some error"
                />
            );
            expect(
                screen.queryByText('Analysis Failed')
            ).not.toBeInTheDocument();
        });

        it('shows error icon in error state', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    error="Test error"
                />
            );
            // ErrorIcon is rendered within the error box
            expect(screen.getByText('Analysis Failed')).toBeInTheDocument();
        });
    });

    describe('analysis state', () => {
        it('shows analysis content via MarkdownContent', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    analysis="## Analysis Result - This is the analysis."
                />
            );
            expect(screen.getByTestId('markdown-content')).toBeInTheDocument();
            expect(
                screen.getByText('## Analysis Result - This is the analysis.')
            ).toBeInTheDocument();
        });

        it('uses markdownContent prop when provided', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    analysis="Raw analysis"
                    markdownContent="# Custom Title - Raw analysis"
                />
            );
            expect(
                screen.getByText('# Custom Title - Raw analysis')
            ).toBeInTheDocument();
        });

        it('does not show analysis when loading', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    analysis="Some analysis"
                />
            );
            expect(
                screen.queryByTestId('markdown-content')
            ).not.toBeInTheDocument();
        });

        it('enables download button when analysis exists and not loading', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    analysis="Analysis content"
                />
            );
            const downloadButton = screen.getByRole('button', {
                name: /download analysis/i,
            });
            expect(downloadButton).not.toBeDisabled();
        });
    });

    describe('download button', () => {
        it('is disabled when analysis is null', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    analysis={null}
                />
            );
            const downloadButton = screen.getByRole('button', {
                name: /download analysis/i,
            });
            expect(downloadButton).toBeDisabled();
        });

        it('is disabled when loading even with analysis', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    analysis="Some content"
                />
            );
            const downloadButton = screen.getByRole('button', {
                name: /download analysis/i,
            });
            expect(downloadButton).toBeDisabled();
        });

        it('calls onDownload when clicked and enabled', () => {
            const onDownload = vi.fn();
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    analysis="Analysis content"
                    onDownload={onDownload}
                />
            );
            const downloadButton = screen.getByRole('button', {
                name: /download analysis/i,
            });
            fireEvent.click(downloadButton);
            expect(onDownload).toHaveBeenCalledTimes(1);
        });
    });

    describe('close button', () => {
        it('calls onClose when close button is clicked', () => {
            const onClose = vi.fn();
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    onClose={onClose}
                />
            );
            const closeButton = screen.getByRole('button', {
                name: /close analysis/i,
            });
            fireEvent.click(closeButton);
            expect(onClose).toHaveBeenCalledTimes(1);
        });

        it('calls onReset before onClose when close button is clicked', () => {
            const onClose = vi.fn();
            const onReset = vi.fn();
            const callOrder: string[] = [];
            onReset.mockImplementation(() => callOrder.push('reset'));
            onClose.mockImplementation(() => callOrder.push('close'));

            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    onClose={onClose}
                    onReset={onReset}
                />
            );
            const closeButton = screen.getByRole('button', {
                name: /close analysis/i,
            });
            fireEvent.click(closeButton);
            expect(onReset).toHaveBeenCalledTimes(1);
            expect(onClose).toHaveBeenCalledTimes(1);
            expect(callOrder).toEqual(['reset', 'close']);
        });

        it('works without onReset prop', () => {
            const onClose = vi.fn();
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    onClose={onClose}
                    onReset={undefined}
                />
            );
            const closeButton = screen.getByRole('button', {
                name: /close analysis/i,
            });
            fireEvent.click(closeButton);
            expect(onClose).toHaveBeenCalledTimes(1);
        });
    });

    describe('toolbar content slot', () => {
        it('renders toolbarContent when provided', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    toolbarContent={
                        <span data-testid="custom-toolbar">Custom Badge</span>
                    }
                />
            );
            expect(screen.getByTestId('custom-toolbar')).toBeInTheDocument();
            expect(screen.getByText('Custom Badge')).toBeInTheDocument();
        });

        it('renders multiple toolbar content elements', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    toolbarContent={
                        <>
                            <span data-testid="badge-1">Badge 1</span>
                            <span data-testid="badge-2">Badge 2</span>
                        </>
                    }
                />
            );
            expect(screen.getByTestId('badge-1')).toBeInTheDocument();
            expect(screen.getByTestId('badge-2')).toBeInTheDocument();
        });

        it('renders null toolbarContent without errors', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    toolbarContent={null}
                />
            );
            expect(screen.getByText('Test Analysis')).toBeInTheDocument();
        });
    });

    describe('markdown content props forwarding', () => {
        it('forwards all markdownContentProps to MarkdownContent', () => {
            const connectionMap = new Map([[1, 'Server 1'], [2, 'Server 2']]);
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    analysis="Test analysis"
                    markdownContentProps={{
                        isDark: true,
                        connectionId: 42,
                        databaseName: 'production',
                        serverName: 'prod-server',
                        connectionMap,
                    }}
                />
            );
            // MarkdownContent is mocked, so we just verify it renders
            expect(screen.getByTestId('markdown-content')).toBeInTheDocument();
        });

        it('handles undefined optional props', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    analysis="Test analysis"
                    markdownContentProps={{
                        isDark: false,
                    }}
                />
            );
            expect(screen.getByTestId('markdown-content')).toBeInTheDocument();
        });
    });

    describe('state precedence', () => {
        it('shows loading state over analysis state', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    analysis="Some analysis"
                />
            );
            expect(screen.getByTestId('analysis-skeleton')).toBeInTheDocument();
            expect(
                screen.queryByTestId('markdown-content')
            ).not.toBeInTheDocument();
        });

        it('shows loading state over error state', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    error="Some error"
                />
            );
            expect(screen.getByTestId('analysis-skeleton')).toBeInTheDocument();
            expect(
                screen.queryByText('Analysis Failed')
            ).not.toBeInTheDocument();
        });

        it('shows error state when not loading with error', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={false}
                    error="Network error"
                    analysis={null}
                />
            );
            expect(screen.getByText('Analysis Failed')).toBeInTheDocument();
            expect(screen.getByText('Network error')).toBeInTheDocument();
        });
    });

    describe('empty tool labels', () => {
        it('handles empty toolLabels array', () => {
            renderWithTheme(
                <BaseAnalysisDialog
                    {...defaultProps}
                    loading={true}
                    toolLabels={[]}
                />
            );
            // Should render loading state without tool badges
            expect(screen.getByTestId('analysis-skeleton')).toBeInTheDocument();
        });
    });
});

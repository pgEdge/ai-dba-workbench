/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// The dialog's wiring: it starts the analysis once on opening and tells
// the shared shell where a write run from the analysis comes from. The
// hook and the shell are stubbed so only that wiring is under test.

import type React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import QueryAnalysisDialog from '../QueryAnalysisDialog';

const analyzeMock = vi.fn();
let hookState = {
    analysis: null as string | null,
    loading: false,
    error: null as string | null,
    progressMessage: '',
    activeTools: [] as string[],
    analyze: analyzeMock,
};

vi.mock('../../hooks/useQueryAnalysis', () => ({
    useQueryAnalysis: () => hookState,
}));

vi.mock('../shared/BaseAnalysisDialog', () => ({
    BaseAnalysisDialog: (props: Record<string, unknown>) => {
        if (!props.open) {return null;}
        const md = props.markdownContentProps as Record<string, unknown>;
        return (
            <div data-testid="base-dialog">
                <div data-testid="toolbar">
                    {props.toolbarContent as React.ReactNode}
                </div>
                <div data-testid="md-origin">
                    {JSON.stringify(md.actionOrigin)}
                </div>
                <button type="button" onClick={props.onDownload as () => void}>
                    download
                </button>
            </div>
        );
    },
}));

const downloadMock = vi.fn();
vi.mock('../../utils/downloadMarkdown', () => ({
    downloadAsMarkdown: (...args: unknown[]) => downloadMock(...args),
}));

const stats = {
    calls: 10,
    totalExecTime: 100,
    meanExecTime: 10,
    rows: 5,
    sharedBlksHit: 1,
    sharedBlksRead: 2,
};

const renderDialog = (open = true) =>
    render(
        <QueryAnalysisDialog
            open={open}
            onClose={vi.fn()}
            isDark={false}
            queryText="SELECT 1"
            queryId="42"
            stats={stats}
            connectionId={3}
            connectionName="alpha"
            databaseName="app"
        />,
    );

describe('QueryAnalysisDialog', () => {
    beforeEach(() => {
        analyzeMock.mockReset();
        downloadMock.mockReset();
        hookState = { ...hookState, analysis: null };
    });

    it('starts the analysis when opened', () => {
        renderDialog();
        expect(analyzeMock).toHaveBeenCalledWith(
            expect.objectContaining({ queryId: '42', connectionId: 3 }),
        );
    });

    it('records writes run from the analysis as query analysis', () => {
        renderDialog();
        expect(
            JSON.parse(screen.getByTestId('md-origin').textContent ?? ''),
        ).toMatchObject({
            actionSource: 'query_analysis',
            actionContext: 'Query analysis of query 42',
        });
    });

    it('downloads the finished analysis', () => {
        hookState = { ...hookState, analysis: '## Result' };
        renderDialog();
        fireEvent.click(screen.getByText('download'));
        expect(downloadMock).toHaveBeenCalledTimes(1);
    });
});

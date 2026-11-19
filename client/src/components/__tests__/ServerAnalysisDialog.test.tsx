/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// The dialog's wiring: it analyses the selection on opening and tells the
// shared shell where a write run from the analysis comes from. The hook
// and the shell are stubbed so only that wiring is under test.

import type React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import ServerAnalysisDialog from '../ServerAnalysisDialog';
import { renderWithTheme } from '../../test/renderWithTheme';
import type {
    ClusterSelection,
    ServerSelection,
} from '../../types/selection';

const analyzeMock = vi.fn();
const resetMock = vi.fn();
let hookState = {
    analysis: null as string | null,
    loading: false,
    error: null as string | null,
    progressMessage: '',
    activeTools: [] as string[],
    analyze: analyzeMock,
    reset: resetMock,
};

vi.mock('../../hooks/useServerAnalysis', () => ({
    useServerAnalysis: () => hookState,
}));

vi.mock('../shared/BaseAnalysisDialog', () => ({
    BaseAnalysisDialog: (props: Record<string, unknown>) => {
        if (!props.open) {return null;}
        const md = props.markdownContentProps as Record<string, unknown>;
        const map = md.connectionMap as Map<number, string> | undefined;
        return (
            <div data-testid="base-dialog">
                <div data-testid="toolbar">
                    {props.toolbarContent as React.ReactNode}
                </div>
                <div data-testid="md-origin">
                    {JSON.stringify(md.actionOrigin)}
                </div>
                <div data-testid="md-connection">
                    {String(md.connectionId)}
                </div>
                <div data-testid="md-map">
                    {JSON.stringify(map ? Array.from(map.entries()) : null)}
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

const server = {
    type: 'server',
    id: 4,
    name: 'alpha',
    status: 'online',
    description: '',
    host: 'db.example.com',
    port: 5432,
    role: 'primary',
    version: '17',
    database: 'app',
    username: 'u',
    os: 'linux',
    platform: 'x86_64',
} as ServerSelection;

const cluster = {
    type: 'cluster',
    id: 'c1',
    name: 'ring',
    status: 'online',
    servers: [
        { id: 4, name: 'alpha' },
        { id: 5, name: 'beta' },
    ],
} as unknown as ClusterSelection;

const renderDialog = (selection: ServerSelection | ClusterSelection | null) =>
    renderWithTheme(
        <ServerAnalysisDialog
            open={true}
            selection={selection}
            onClose={vi.fn()}
        />,
    );

describe('ServerAnalysisDialog', () => {
    beforeEach(() => {
        analyzeMock.mockReset();
        downloadMock.mockReset();
        hookState = { ...hookState, analysis: null };
    });

    it('analyses a server and records writes as server analysis', () => {
        renderDialog(server);

        expect(analyzeMock).toHaveBeenCalledWith(server);
        expect(screen.getByTestId('md-connection')).toHaveTextContent('4');
        expect(
            JSON.parse(screen.getByTestId('md-origin').textContent ?? ''),
        ).toMatchObject({
            actionSource: 'server_analysis',
            actionContext: 'Server analysis of alpha',
        });
    });

    it('analyses a cluster with a connection map', () => {
        renderDialog(cluster);

        expect(screen.getByTestId('md-map')).toHaveTextContent(
            '[[4,"alpha"],[5,"beta"]]',
        );
        expect(screen.getByTestId('toolbar')).toHaveTextContent('2 servers');
        expect(
            JSON.parse(screen.getByTestId('md-origin').textContent ?? ''),
        ).toMatchObject({ actionContext: 'Cluster analysis of ring' });
    });

    it('downloads the finished analysis', () => {
        hookState = { ...hookState, analysis: '## Result' };
        renderDialog(server);
        fireEvent.click(screen.getByText('download'));
        expect(downloadMock).toHaveBeenCalledTimes(1);
    });

    it('names an unknown selection', () => {
        renderDialog(null);
        expect(
            JSON.parse(screen.getByTestId('md-origin').textContent ?? ''),
        ).toMatchObject({ actionContext: 'Server analysis of unknown' });
    });
});

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Exercises ChatSqlCodeBlock with the real RunnableCodeBlock and
// ConnectionSelectorCodeBlock, so the tests assert what the user sees:
// the Run button's target label, the selector, and the request that a
// Run sends. Only the syntax highlighter and apiFetch are mocked.

import type React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createTheme } from '@mui/material/styles';
import ChatSqlCodeBlock from '../ChatSqlCodeBlock';
import ReplyTargetsContext, { EMPTY_REPLY_TARGETS } from '../replyTargetsContext';
import type { ReplyTargets } from '../chatSqlTarget';
import ClusterDataContext, {
    type ClusterDataContextValue,
    type ClusterGroup,
} from '../../../contexts/ClusterDataContext';
import { renderWithTheme } from '../../../test/renderWithTheme';

vi.mock('react-syntax-highlighter', () => ({
    Prism: ({ children }: { children: React.ReactNode }) => (
        <div data-testid="syntax-highlighter">{children}</div>
    ),
}));

const mockApiFetch = vi.fn();
vi.mock('../../../utils/apiClient', () => ({
    apiFetch: (...args: unknown[]) => mockApiFetch(...args),
}));

// Validation is RunnableCodeBlock's concern and has its own tests; here
// it only matters which target it is asked to validate against.
const mockUseSqlValidation = vi.fn(
    (_options: unknown) => ({ status: 'skipped', error: '' }),
);
vi.mock('../../../hooks/useSqlValidation', () => ({
    useSqlValidation: (options: unknown) => mockUseSqlValidation(options),
}));

const theme = createTheme();

const clusterData: ClusterGroup[] = [
    {
        id: 'g1',
        name: 'Group',
        clusters: [
            {
                id: 'c1',
                name: 'Cluster',
                servers: [
                    { id: 1, name: 'alpha', database_name: 'app' },
                    { id: 2, name: 'beta', database_name: 'postgres' },
                ],
            },
        ],
    },
];

const renderBlock = (code: string, reply: ReplyTargets = EMPTY_REPLY_TARGETS) => {
    const clusters = { clusterData } as unknown as ClusterDataContextValue;
    return renderWithTheme(
        <ClusterDataContext.Provider value={clusters}>
            <ReplyTargetsContext.Provider value={reply}>
                <ChatSqlCodeBlock
                    code={code}
                    language="sql"
                    isDark={false}
                    syntaxTheme={{}}
                    customBackground="#fff"
                    theme={theme}
                    props={{}}
                />
            </ReplyTargetsContext.Provider>
        </ClusterDataContext.Provider>,
    );
};

const singleTarget: ReplyTargets = {
    targets: [{ connectionId: 1 }],
    unattributed: false,
    usedDatastore: false,
};

describe('ChatSqlCodeBlock', () => {
    beforeEach(() => {
        mockApiFetch.mockReset();
        mockUseSqlValidation.mockClear();
    });

    it('labels the Run button with the single target the reply used', () => {
        renderBlock('SELECT 1', singleTarget);
        expect(screen.getByLabelText('Run on alpha/app')).toBeInTheDocument();
        expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    });

    it('runs the query on that server and database', async () => {
        mockApiFetch.mockResolvedValue({
            ok: true,
            status: 200,
            json: vi.fn().mockResolvedValue({
                results: [{ columns: ['one'], rows: [['1']], row_count: 1 }],
            }),
        });
        const user = userEvent.setup();
        renderBlock('SELECT 1', singleTarget);

        const button = screen.getByLabelText('Run on alpha/app')
            .querySelector('button') as HTMLElement;
        await user.click(button);

        await waitFor(() => expect(mockApiFetch).toHaveBeenCalledTimes(1));
        const [url, init] = mockApiFetch.mock.calls[0] as [string, { body: string }];
        expect(url).toBe('/api/v1/connections/1/query');
        expect(JSON.parse(init.body)).toEqual({
            query: 'SELECT 1;',
            database_name: 'app',
        });
        expect(mockUseSqlValidation).toHaveBeenCalledWith(
            expect.objectContaining({
                connectionId: 1,
                databaseName: 'app',
                enabled: true,
            }),
        );
    });

    it('runs on the server named by a connection_id comment, without the comment', () => {
        renderBlock('-- connection_id: 2\nSELECT 2', singleTarget);
        expect(screen.getByLabelText('Run on beta/postgres')).toBeInTheDocument();
        expect(screen.getByTestId('syntax-highlighter').textContent).toBe('SELECT 2');
    });

    it('asks the user to choose when the target is unclear', async () => {
        const user = userEvent.setup();
        renderBlock('SELECT 1');

        const select = screen.getByRole('combobox', {
            name: 'Server to run this query on',
        });
        expect(select).toHaveTextContent('Choose a server to run on');
        expect(screen.queryByLabelText(/^Run on/)).not.toBeInTheDocument();

        await user.click(select);
        await user.click(screen.getByRole('option', { name: 'beta/postgres (ID: 2)' }));

        expect(screen.getByLabelText('Run on beta/postgres')).toBeInTheDocument();
    });

    it.each([
        'SELECT * FROM t WHERE id = $1',
        'SELECT * FROM <table_name>',
    ])('shows the template notice and no Run button for %s', (sql) => {
        renderBlock(sql, singleTarget);
        expect(screen.getByTestId('sql-template-notice'))
            .toHaveTextContent('query template');
        expect(screen.getByLabelText('Copy to clipboard')).toBeInTheDocument();
        expect(screen.queryByLabelText(/^Run/)).not.toBeInTheDocument();
        expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    });
});

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
 * Component coverage for ConnectionSelectorCodeBlock. The component
 * wraps a RunnableCodeBlock with an MUI Select that lets the user pick
 * which cluster node to run the SQL against; the chosen connection ID
 * and name are forwarded to RunnableCodeBlock as props. To keep these
 * tests isolated from the heavy SQL-runner UI, the underlying
 * RunnableCodeBlock is mocked with a stub that surfaces the props it
 * received as data attributes so the wrapper's behaviour can be
 * asserted in observable terms.
 */

import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createTheme } from '@mui/material/styles';
import ConnectionSelectorCodeBlock from '../ConnectionSelectorCodeBlock';
import { renderWithTheme } from '../../../test/renderWithTheme';

vi.mock('../RunnableCodeBlock', () => ({
    default: (props: Record<string, unknown>) => (
        <div
            data-testid="runnable-code-block"
            data-connection-id={String(props.connectionId)}
            data-server-name={String(props.serverName)}
            data-database-name={String(props.databaseName ?? '')}
            data-language={String(props.language)}
            data-is-sql={String(props.isSql)}
        >
            {String(props.codeContent)}
        </div>
    ),
}));

const theme = createTheme();

const baseProps = {
    codeContent: 'SELECT 1;',
    language: 'sql',
    isDark: false,
    syntaxTheme: {},
    customBackground: '#fff',
    theme,
    props: {},
};

describe('ConnectionSelectorCodeBlock', () => {
    it('initialises with the first connection in the map', () => {
        const map = new Map([
            [10, 'alpha'],
            [20, 'beta'],
        ]);
        renderWithTheme(
            <ConnectionSelectorCodeBlock
                {...baseProps}
                connectionMap={map}
                databaseName="prod"
            />,
        );

        const runnable = screen.getByTestId('runnable-code-block');
        expect(runnable.dataset.connectionId).toBe('10');
        expect(runnable.dataset.serverName).toBe('alpha');
        expect(runnable.dataset.databaseName).toBe('prod');
        expect(runnable.dataset.isSql).toBe('true');
        expect(runnable.dataset.language).toBe('sql');
        expect(runnable.textContent).toBe('SELECT 1;');
    });

    it('renders one MenuItem per connection in the map', async () => {
        const map = new Map([
            [1, 'one'],
            [2, 'two'],
            [3, 'three'],
        ]);
        const user = userEvent.setup();
        renderWithTheme(
            <ConnectionSelectorCodeBlock
                {...baseProps}
                connectionMap={map}
            />,
        );

        // Open the dropdown to check options are present.
        await user.click(screen.getByRole('combobox'));

        expect(screen.getByRole('option', { name: 'one (ID: 1)' })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'two (ID: 2)' })).toBeInTheDocument();
        expect(screen.getByRole('option', { name: 'three (ID: 3)' })).toBeInTheDocument();
    });

    it('forwards the new connection id and name when the user picks a different option', async () => {
        const map = new Map([
            [10, 'alpha'],
            [20, 'beta'],
        ]);
        const user = userEvent.setup();
        renderWithTheme(
            <ConnectionSelectorCodeBlock
                {...baseProps}
                connectionMap={map}
            />,
        );

        await user.click(screen.getByRole('combobox'));
        await user.click(screen.getByRole('option', { name: 'beta (ID: 20)' }));

        const runnable = screen.getByTestId('runnable-code-block');
        expect(runnable.dataset.connectionId).toBe('20');
        expect(runnable.dataset.serverName).toBe('beta');
    });

    it('falls back to id 0 and an empty server name when the map is empty', () => {
        renderWithTheme(
            <ConnectionSelectorCodeBlock
                {...baseProps}
                connectionMap={new Map()}
            />,
        );

        const runnable = screen.getByTestId('runnable-code-block');
        expect(runnable.dataset.connectionId).toBe('0');
        expect(runnable.dataset.serverName).toBe('');
    });

    it('forwards an empty databaseName when none is provided', () => {
        const map = new Map([[7, 'lone']]);
        renderWithTheme(
            <ConnectionSelectorCodeBlock
                {...baseProps}
                connectionMap={map}
            />,
        );

        const runnable = screen.getByTestId('runnable-code-block');
        expect(runnable.dataset.databaseName).toBe('');
    });
});

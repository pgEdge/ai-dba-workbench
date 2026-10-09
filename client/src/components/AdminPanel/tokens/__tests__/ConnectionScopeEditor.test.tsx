/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ConnectionScopeEditor from '../ConnectionScopeEditor';
import {
    ALL_CONNECTIONS_LABEL,
    NO_CONNECTION_RESTRICTION_TEXT,
} from '../tokenTypes';
import type { Connection, ScopedConnection } from '../tokenTypes';

const theme = createTheme();

const CONNECTIONS: Connection[] = [
    { id: 1, name: 'Primary DB' },
    { id: 2, name: 'Replica DB' },
];

const ALL: ScopedConnection = { id: 0, name: ALL_CONNECTIONS_LABEL, access_level: 'read' };
const PRIMARY: ScopedConnection = { id: 1, name: 'Primary DB', access_level: 'read_write' };

type EditorProps = React.ComponentProps<typeof ConnectionScopeEditor>;

/** Renders the editor with its scope held in state, as the dialogs do. */
const Harness: React.FC<Partial<EditorProps> & { initial?: ScopedConnection[] }> = ({
    initial = [],
    ...props
}) => {
    const [scoped, setScoped] = useState<ScopedConnection[]>(initial);
    return (
        <ThemeProvider theme={theme}>
            <ConnectionScopeEditor
                availableConnections={CONNECTIONS}
                scopedConnections={scoped}
                onScopedConnectionsChange={setScoped}
                ownerConnectionLevels={{ 1: 'read_write', 2: 'read' }}
                ownerIsSuperuser={false}
                {...props}
            />
        </ThemeProvider>
    );
};

const openOptions = async () => {
    const input = screen.getByLabelText(/Add Connection/);
    fireEvent.focus(input);
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    await waitFor(() => {
        expect(screen.getAllByRole('option').length).toBeGreaterThan(0);
    });
};

const optionNames = () =>
    screen.getAllByRole('option').map((o) => o.textContent);

const rowNames = () =>
    screen.queryAllByRole('row').slice(1).map(
        (r) => r.querySelector('td')?.textContent,
    );

describe('ConnectionScopeEditor', () => {
    it('explains an empty scope as no restriction', () => {
        render(<Harness />);
        expect(screen.getByText(NO_CONNECTION_RESTRICTION_TEXT)).toBeInTheDocument();
    });

    it('offers all connections to a superuser owner', async () => {
        render(<Harness ownerIsSuperuser ownerConnectionLevels={{}} />);
        await openOptions();
        expect(optionNames()).toEqual([ALL_CONNECTIONS_LABEL, 'Primary DB', 'Replica DB']);
    });

    it('offers all connections to an owner who holds every connection', async () => {
        render(<Harness ownerConnectionLevels={{ 0: 'read', 1: 'read' }} />);
        await openOptions();
        expect(optionNames()).toContain(ALL_CONNECTIONS_LABEL);
    });

    it('does not offer all connections to an owner holding only some', async () => {
        render(<Harness />);
        await openOptions();
        expect(optionNames()).toEqual(['Primary DB', 'Replica DB']);
    });

    it('does not offer a connection already in the scope', async () => {
        render(<Harness initial={[PRIMARY]} />);
        await openOptions();
        expect(optionNames()).toEqual(['Replica DB']);
    });

    it('adds a connection at the owner\'s level for it', async () => {
        render(<Harness />);
        await openOptions();
        fireEvent.click(screen.getByRole('option', { name: 'Replica DB' }));
        expect(rowNames()).toEqual(['Replica DB']);
        expect(screen.getByDisplayValue('read')).toBeInTheDocument();
    });

    it('clears the picker text after a pick, so the next list is whole', async () => {
        render(<Harness ownerIsSuperuser />);
        const input = screen.getByLabelText(/Add Connection/);
        fireEvent.change(input, { target: { value: 'Rep' } });
        await waitFor(() => {
            expect(optionNames()).toEqual(['Replica DB']);
        });
        fireEvent.click(screen.getByRole('option', { name: 'Replica DB' }));
        expect(input).toHaveValue('');

        await openOptions();
        expect(optionNames()).toEqual([ALL_CONNECTIONS_LABEL, 'Primary DB']);
    });

    it('replaces particular connections when all connections is chosen', async () => {
        render(<Harness ownerIsSuperuser initial={[PRIMARY]} />);
        await openOptions();
        fireEvent.click(screen.getByRole('option', { name: ALL_CONNECTIONS_LABEL }));
        expect(rowNames()).toEqual([ALL_CONNECTIONS_LABEL]);
    });

    it('replaces all connections when a particular connection is chosen', async () => {
        render(<Harness ownerIsSuperuser initial={[ALL]} />);
        await openOptions();
        fireEvent.click(screen.getByRole('option', { name: 'Primary DB' }));
        expect(rowNames()).toEqual(['Primary DB']);
    });

    it('explains a mixed scope until the user resolves it', () => {
        render(<Harness initial={[ALL, PRIMARY]} />);
        expect(screen.getByRole('alert')).toHaveTextContent(/cannot be saved/);

        fireEvent.click(screen.getByLabelText(`remove ${ALL_CONNECTIONS_LABEL}`));
        expect(screen.queryByRole('alert')).not.toBeInTheDocument();
        expect(rowNames()).toEqual(['Primary DB']);
    });

    it('resolves a mixed scope when a particular connection is added', async () => {
        render(<Harness initial={[ALL, PRIMARY]} />);
        await openOptions();
        fireEvent.click(screen.getByRole('option', { name: 'Replica DB' }));
        expect(screen.queryByRole('alert')).not.toBeInTheDocument();
        expect(rowNames()).toEqual(['Primary DB', 'Replica DB']);
    });

    it('explains a connection named twice', () => {
        render(<Harness initial={[PRIMARY, { ...PRIMARY }]} />);
        expect(screen.getByRole('alert')).toHaveTextContent(
            /names "Primary DB" more than once/,
        );
    });

    it('changes an entry\'s access level', async () => {
        render(<Harness initial={[PRIMARY]} />);
        fireEvent.mouseDown(screen.getByRole('combobox', { name: '' }));
        fireEvent.click(await screen.findByRole('option', { name: 'Read Only' }));
        expect(screen.getByDisplayValue('read')).toBeInTheDocument();
    });
});

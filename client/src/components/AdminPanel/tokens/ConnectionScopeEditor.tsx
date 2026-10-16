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
import { Autocomplete, TextField, Alert } from '@mui/material';
import { SELECT_FIELD_SX } from '../../shared/formStyles';
import ConnectionScopeTable from './ConnectionScopeTable';
import {
    ALL_CONNECTIONS_ID,
    ALL_CONNECTIONS_OPTION,
    NO_CONNECTION_RESTRICTION_TEXT,
    addScopedConnection,
    connectionScopeProblem,
} from './tokenTypes';
import type { Connection, ScopedConnection } from './tokenTypes';

export interface ConnectionScopeEditorProps {
    /** Connections the owner can reach, which may be added to the scope. */
    availableConnections: Connection[];
    /** The connection scope being edited. */
    scopedConnections: ScopedConnection[];
    /** Handler for changes to the connection scope. */
    onScopedConnectionsChange: (connections: ScopedConnection[]) => void;
    /**
     * Map of connection ID to the owner's maximum access level; an entry
     * for connection 0 means the owner holds every connection.
     */
    ownerConnectionLevels: Record<number, string>;
    /** Whether the owner is a superuser. */
    ownerIsSuperuser: boolean;
    /** Whether the editor is disabled (e.g., during loading). */
    disabled?: boolean;
}

/**
 * Edits a token's connection scope: a picker for adding a connection, or
 * every connection the owner can reach, and a table of the entries. It
 * never builds a scope the server would refuse (see
 * addScopedConnection), and explains inline what is wrong with a stored
 * scope that already breaks the rules, so the dialog can block saving.
 */
const ConnectionScopeEditor: React.FC<ConnectionScopeEditorProps> = ({
    availableConnections,
    scopedConnections,
    onScopedConnectionsChange,
    ownerConnectionLevels,
    ownerIsSuperuser,
    disabled = false,
}) => {
    // The "all connections" entry can be granted only by an owner who
    // holds every connection.
    const canScopeAll =
        ownerIsSuperuser || ALL_CONNECTIONS_ID in ownerConnectionLevels;

    const options = [
        ...(canScopeAll ? [ALL_CONNECTIONS_OPTION] : []),
        ...availableConnections,
    ].filter((c) => !scopedConnections.some((sc) => sc.id === c.id));

    // The picker only adds entries, so its text is cleared after each
    // pick; left in place, it would filter the next list down to nothing.
    const [inputValue, setInputValue] = useState('');

    const handleAdd = (connection: Connection | null) => {
        setInputValue('');
        if (connection) {
            onScopedConnectionsChange(
                addScopedConnection(scopedConnections, {
                    id: connection.id,
                    name: connection.name,
                    access_level:
                        ownerConnectionLevels[connection.id] || 'read_write',
                }),
            );
        }
    };

    const problem = connectionScopeProblem(scopedConnections);

    return (
        <>
            <Autocomplete<Connection>
                options={options}
                getOptionLabel={(option) => option.name || ''}
                value={null}
                onChange={(_e, value) => { handleAdd(value); }}
                inputValue={inputValue}
                onInputChange={(_e, value, reason) => {
                    if (reason !== 'reset') {
                        setInputValue(value);
                    }
                }}
                renderInput={(params) => (
                    <TextField
                        {...params}
                        label="Add Connection"
                        margin="dense"
                        placeholder="Select a connection to add..."
                        helperText={
                            scopedConnections.length === 0
                                ? NO_CONNECTION_RESTRICTION_TEXT
                                : undefined
                        }
                        InputLabelProps={{
                            ...params.InputLabelProps,
                            shrink: true,
                        }}
                        sx={SELECT_FIELD_SX}
                    />
                )}
                disabled={disabled}
            />

            {problem && (
                <Alert severity="warning" sx={{ mt: 1, borderRadius: 1 }}>
                    {problem}
                </Alert>
            )}

            <ConnectionScopeTable
                connections={scopedConnections}
                onAccessLevelChange={(id, level) => {
                    onScopedConnectionsChange(
                        scopedConnections.map((c) =>
                            c.id === id ? { ...c, access_level: level } : c
                        )
                    );
                }}
                onRemove={(id) => {
                    onScopedConnectionsChange(
                        scopedConnections.filter((c) => c.id !== id)
                    );
                }}
                ownerConnectionLevels={ownerConnectionLevels}
                ownerIsSuperuser={ownerIsSuperuser}
                disabled={disabled}
            />
        </>
    );
};

export default ConnectionScopeEditor;

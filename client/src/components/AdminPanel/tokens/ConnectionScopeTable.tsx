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
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableRow,
    TextField,
    MenuItem,
    IconButton,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Delete as DeleteIcon } from '@mui/icons-material';
import { tableHeaderCellSx, getDeleteIconSx } from '../styles';
import { SELECT_FIELD_SX } from '../../shared/formStyles';
import type { ScopedConnection } from './tokenTypes';

export interface ConnectionScopeTableProps {
    /** The list of scoped connections to display. */
    connections: ScopedConnection[];
    /** Handler for when a connection's access level changes. */
    onAccessLevelChange: (connectionId: number, accessLevel: string) => void;
    /** Handler for when a connection is removed. */
    onRemove: (connectionId: number) => void;
    /** Map of connection ID to maximum allowed access level for the owner. */
    ownerConnectionLevels: Record<number, string>;
    /** Whether the owner is a superuser (can always grant read_write). */
    ownerIsSuperuser: boolean;
    /** Whether the table is disabled (e.g., during loading). */
    disabled?: boolean;
}

/**
 * Table displaying scoped connections with access level dropdowns and
 * remove buttons.
 */
const ConnectionScopeTable: React.FC<ConnectionScopeTableProps> = ({
    connections,
    onAccessLevelChange,
    onRemove,
    ownerConnectionLevels,
    ownerIsSuperuser,
    disabled = false,
}) => {
    const theme = useTheme();
    const deleteIconSx = getDeleteIconSx(theme);

    if (connections.length === 0) {
        return null;
    }

    return (
        <Table size="small" sx={{ mt: 1 }}>
            <TableHead>
                <TableRow>
                    <TableCell sx={{ ...tableHeaderCellSx, py: 0.5 }}>
                        Connection
                    </TableCell>
                    <TableCell sx={{ ...tableHeaderCellSx, py: 0.5 }}>
                        Access Level
                    </TableCell>
                    <TableCell
                        sx={{ ...tableHeaderCellSx, py: 0.5 }}
                        align="right"
                    />
                </TableRow>
            </TableHead>
            <TableBody>
                {connections.map((sc) => {
                    const canGrantReadWrite =
                        ownerConnectionLevels[sc.id] === 'read_write' ||
                        ownerIsSuperuser;

                    return (
                        <TableRow key={sc.id}>
                            <TableCell sx={{ py: 0.5 }}>{sc.name}</TableCell>
                            <TableCell sx={{ py: 0.5 }}>
                                <TextField
                                    select
                                    size="small"
                                    value={sc.access_level}
                                    onChange={(e) =>
                                        { onAccessLevelChange(sc.id, e.target.value); }
                                    }
                                    disabled={disabled}
                                    InputLabelProps={{ shrink: true }}
                                    sx={{ minWidth: 130, ...SELECT_FIELD_SX }}
                                >
                                    <MenuItem value="read">Read Only</MenuItem>
                                    {canGrantReadWrite && (
                                        <MenuItem value="read_write">
                                            Read/Write
                                        </MenuItem>
                                    )}
                                </TextField>
                            </TableCell>
                            <TableCell align="right" sx={{ py: 0.5 }}>
                                <IconButton
                                    size="small"
                                    onClick={() => { onRemove(sc.id); }}
                                    disabled={disabled}
                                    sx={deleteIconSx}
                                    aria-label={`remove ${sc.name}`}
                                >
                                    <DeleteIcon fontSize="small" />
                                </IconButton>
                            </TableCell>
                        </TableRow>
                    );
                })}
            </TableBody>
        </Table>
    );
};

export default ConnectionScopeTable;

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import { useContext } from 'react';
import ConnectionStatusContext from './ConnectionStatusContext';
import type { ConnectionStatusValue } from './ConnectionStatusContext';

export const useConnectionStatus = (): ConnectionStatusValue => {
    const context = useContext(ConnectionStatusContext);
    if (!context) {
        throw new Error(
            'useConnectionStatus must be used within a ConnectionStatusProvider',
        );
    }
    return context;
};

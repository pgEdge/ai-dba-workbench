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
import AlertsContext from './AlertsContext';
import type { AlertsContextValue } from './AlertsContext';

export const useAlerts = (): AlertsContextValue => {
    const context = useContext(AlertsContext);
    if (!context) {
        throw new Error('useAlerts must be used within an AlertsProvider');
    }
    return context;
};

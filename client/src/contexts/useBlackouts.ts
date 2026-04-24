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
import BlackoutContext from './BlackoutContext';
import type { BlackoutContextValue } from './BlackoutContext';

export const useBlackouts = (): BlackoutContextValue => {
    const context = useContext(BlackoutContext);
    if (!context) {
        throw new Error('useBlackouts must be used within a BlackoutProvider');
    }
    return context;
};

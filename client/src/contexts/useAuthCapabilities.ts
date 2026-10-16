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
import AuthCapabilitiesContext from './AuthCapabilitiesContext';
import type { AuthCapabilitiesValue } from './AuthCapabilitiesContext';

export const useAuthCapabilities = (): AuthCapabilitiesValue => {
    const context = useContext(AuthCapabilitiesContext);
    if (!context) {
        throw new Error(
            'useAuthCapabilities must be used within an AuthCapabilitiesProvider',
        );
    }
    return context;
};

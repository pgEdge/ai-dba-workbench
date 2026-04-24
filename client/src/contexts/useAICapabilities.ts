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
import AICapabilitiesContext from './AICapabilitiesContext';
import type { AICapabilitiesValue } from './AICapabilitiesContext';

export const useAICapabilities = (): AICapabilitiesValue => {
    const context = useContext(AICapabilitiesContext);
    if (!context) {
        throw new Error(
            'useAICapabilities must be used within an AICapabilitiesProvider',
        );
    }
    return context;
};

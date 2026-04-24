/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { createContext, useState, useEffect, useMemo } from 'react';
import { apiGet } from '../utils/apiClient';

export interface AICapabilitiesValue {
    aiEnabled: boolean;
    maxIterations: number;
    loading: boolean;
}

const AICapabilitiesContext = createContext<AICapabilitiesValue | null>(null);

interface CapabilitiesResponse {
    ai_enabled: boolean;
    max_iterations?: number;
}

export const AICapabilitiesProvider = ({
    children,
}: {
    children: React.ReactNode;
}): React.ReactElement => {
    const [aiEnabled, setAiEnabled] = useState(false);
    const [maxIterations, setMaxIterations] = useState(50);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        const fetchCapabilities = async () => {
            try {
                const data = await apiGet<CapabilitiesResponse>('/api/v1/capabilities');
                setAiEnabled(data.ai_enabled === true);
                setMaxIterations(data.max_iterations ?? 50);
            } catch {
                setAiEnabled(false);
            } finally {
                setLoading(false);
            }
        };

        fetchCapabilities();
    }, []);

    const value = useMemo(
        () => ({ aiEnabled, maxIterations, loading }),
        [aiEnabled, maxIterations, loading],
    );

    return (
        <AICapabilitiesContext.Provider value={value}>
            {children}
        </AICapabilitiesContext.Provider>
    );
};

export default AICapabilitiesContext;

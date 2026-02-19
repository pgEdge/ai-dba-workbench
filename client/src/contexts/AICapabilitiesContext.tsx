/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
/* eslint-disable react-refresh/only-export-components */

import React, { createContext, useContext, useState, useEffect } from 'react';

interface AICapabilitiesValue {
    aiEnabled: boolean;
    loading: boolean;
}

const AICapabilitiesContext = createContext<AICapabilitiesValue | null>(null);

interface CapabilitiesResponse {
    ai_enabled: boolean;
}

export const AICapabilitiesProvider = ({
    children,
}: {
    children: React.ReactNode;
}): React.ReactElement => {
    const [aiEnabled, setAiEnabled] = useState(false);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        const fetchCapabilities = async () => {
            try {
                const response = await fetch('/api/v1/capabilities', {
                    credentials: 'include',
                });
                if (response.ok) {
                    const data: CapabilitiesResponse = await response.json();
                    setAiEnabled(data.ai_enabled === true);
                } else {
                    setAiEnabled(false);
                }
            } catch {
                setAiEnabled(false);
            } finally {
                setLoading(false);
            }
        };

        fetchCapabilities();
    }, []);

    return (
        <AICapabilitiesContext.Provider value={{ aiEnabled, loading }}>
            {children}
        </AICapabilitiesContext.Provider>
    );
};

export const useAICapabilities = (): AICapabilitiesValue => {
    const context = useContext(AICapabilitiesContext);
    if (!context) {
        throw new Error(
            'useAICapabilities must be used within an AICapabilitiesProvider',
        );
    }
    return context;
};

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
import { createContext, useState, useEffect, useMemo } from 'react';
import { apiGet } from '../utils/apiClient';

/**
 * AuthCapabilities describes the sign-in methods the server offers, so
 * that the login screen renders only the ones that will actually work.
 */
export interface AuthCapabilities {
    localEnabled: boolean;
    oidcEnabled: boolean;
    oidcLabel: string;
}

export type AuthCapabilitiesValue = AuthCapabilities & { loading: boolean };

/*
 * The fallback used whenever the capabilities are not available: either
 * the fetch failed, or the server predates the `auth` block. A username
 * and password form that might be rejected is far better than a blank
 * card with nothing to click.
 */
const DEFAULT_AUTH_CAPABILITIES: AuthCapabilities = {
    localEnabled: true,
    oidcEnabled: false,
    oidcLabel: '',
};

const AuthCapabilitiesContext = createContext<AuthCapabilitiesValue | null>(null);

interface AuthCapabilitiesResponse {
    local_enabled?: boolean;
    oidc_enabled?: boolean;
    oidc_label?: string;
}

interface CapabilitiesResponse {
    auth?: AuthCapabilitiesResponse;
}

/**
 * Map the server's `auth` block onto the client shape, defaulting every
 * missing field so that an older server still yields a usable screen.
 */
const mapAuthCapabilities = (
    auth: AuthCapabilitiesResponse | undefined,
): AuthCapabilities => {
    if (!auth) {
        return DEFAULT_AUTH_CAPABILITIES;
    }
    return {
        localEnabled: auth.local_enabled !== false,
        oidcEnabled: auth.oidc_enabled === true,
        oidcLabel: typeof auth.oidc_label === 'string' ? auth.oidc_label : '',
    };
};

export const AuthCapabilitiesProvider = ({
    children,
}: {
    children: React.ReactNode;
}): React.ReactElement => {
    const [capabilities, setCapabilities] = useState<AuthCapabilities>(
        DEFAULT_AUTH_CAPABILITIES,
    );
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;

        const fetchCapabilities = async () => {
            try {
                const data = await apiGet<CapabilitiesResponse>(
                    '/api/v1/capabilities',
                );
                if (!cancelled) {
                    setCapabilities(mapAuthCapabilities(data.auth));
                }
            } catch {
                if (!cancelled) {
                    setCapabilities(DEFAULT_AUTH_CAPABILITIES);
                }
            } finally {
                if (!cancelled) {
                    setLoading(false);
                }
            }
        };

        void fetchCapabilities();

        return () => {
            cancelled = true;
        };
    }, []);

    const value = useMemo(
        () => ({ ...capabilities, loading }),
        [capabilities, loading],
    );

    return (
        <AuthCapabilitiesContext.Provider value={value}>
            {children}
        </AuthCapabilitiesContext.Provider>
    );
};

export default AuthCapabilitiesContext;

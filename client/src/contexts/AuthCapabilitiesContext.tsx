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

/*
 * How long to wait for the capabilities before giving up and applying
 * the defaults. `apiGet` sets no timeout of its own, so without this a
 * request that never settles (a proxy holding the connection open, a
 * half-open socket) would leave the login screen with nothing to sign
 * in with at all.
 */
const CAPABILITIES_TIMEOUT_MS = 5000;

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
        let settled = false;
        const controller = new AbortController();

        /*
         * Apply the defaults if the request has not answered in time,
         * and abort it so that a late answer cannot swap the form out
         * from under whoever is already typing into it.
         */
        const timer = setTimeout(() => {
            // Both the cleanup and the request's own completion clear
            // this timer, so reaching here means neither has happened.
            settled = true;
            controller.abort();
            setCapabilities(DEFAULT_AUTH_CAPABILITIES);
            setLoading(false);
        }, CAPABILITIES_TIMEOUT_MS);

        const fetchCapabilities = async () => {
            try {
                const data = await apiGet<CapabilitiesResponse>(
                    '/api/v1/capabilities',
                    { signal: controller.signal },
                );
                if (!cancelled && !settled) {
                    setCapabilities(mapAuthCapabilities(data.auth));
                }
            } catch {
                if (!cancelled && !settled) {
                    setCapabilities(DEFAULT_AUTH_CAPABILITIES);
                }
            } finally {
                if (!cancelled && !settled) {
                    settled = true;
                    clearTimeout(timer);
                    setLoading(false);
                }
            }
        };

        void fetchCapabilities();

        return () => {
            cancelled = true;
            clearTimeout(timer);
            controller.abort();
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

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
 * What a server that answered, but sent no `auth` block, offers: it
 * predates this feature, so local login is all it has. This is
 * knowledge rather than a guess, which is why it differs from the
 * fallback below.
 */
const LEGACY_AUTH_CAPABILITIES: AuthCapabilities = {
    localEnabled: true,
    oidcEnabled: false,
    oidcLabel: '',
};

/*
 * What to assume when the server did not answer at all. Here we know
 * nothing, so both affordances are offered and the operator's choice
 * decides which one works: signing in by a method that turns out to be
 * disabled costs an error message and a second try, whereas hiding the
 * only method that would have worked is a dead end with nothing on
 * screen to explain it. The label is left empty so that the button
 * falls back to the same generic wording the server itself uses.
 */
const UNKNOWN_AUTH_CAPABILITIES: AuthCapabilities = {
    localEnabled: true,
    oidcEnabled: true,
    oidcLabel: '',
};

/*
 * How long to wait for one attempt at the capabilities. `apiGet` sets
 * no timeout of its own, so without this a request that never settles
 * (a proxy holding the connection open, a half-open socket) would
 * leave the login screen waiting forever.
 *
 * A healthy server answers this endpoint, which serves three fields
 * from memory, in tens of milliseconds, so four seconds is already
 * two orders of magnitude of slack and a stall is the likelier
 * explanation than slowness.
 */
const CAPABILITIES_TIMEOUT_MS = 4000;

/*
 * One retry, so that a single transient stall does not decide what the
 * login screen offers. The worst case stays inside eight seconds,
 * which is short enough that the spinner still reads as waiting rather
 * than as broken; a longer single timeout would cover the same stall
 * less well whilst making every failure slower.
 */
const CAPABILITIES_RETRIES = 1;

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
        return LEGACY_AUTH_CAPABILITIES;
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
        LEGACY_AUTH_CAPABILITIES,
    );
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;
        let timer: ReturnType<typeof setTimeout> | undefined;
        let controller: AbortController | undefined;

        const finish = (value: AuthCapabilities) => {
            if (cancelled) {
                return;
            }
            setCapabilities(value);
            setLoading(false);
        };

        /*
         * One attempt: race the request against a timer, so that the
         * attempt ends on time whether or not the request honours the
         * abort, and abort it either way so that a late answer cannot
         * swap the form out from under whoever is already typing.
         */
        const attempt = async (): Promise<CapabilitiesResponse> => {
            controller = new AbortController();
            const aborter = controller;

            const request = apiGet<CapabilitiesResponse>(
                '/api/v1/capabilities',
                { signal: aborter.signal },
            );
            // The race abandons the request on a timeout, so absorb a
            // later rejection rather than leaving it unhandled.
            request.catch(() => undefined);

            const expiry = new Promise<never>((_, reject) => {
                timer = setTimeout(() => {
                    aborter.abort();
                    reject(new Error('capabilities request timed out'));
                }, CAPABILITIES_TIMEOUT_MS);
            });

            try {
                return await Promise.race([request, expiry]);
            } finally {
                clearTimeout(timer);
            }
        };

        const fetchCapabilities = async () => {
            for (let left = CAPABILITIES_RETRIES; left >= 0; left -= 1) {
                try {
                    const data = await attempt();
                    finish(mapAuthCapabilities(data.auth));
                    return;
                } catch {
                    if (cancelled) {
                        return;
                    }
                }
            }
            finish(UNKNOWN_AUTH_CAPABILITIES);
        };

        void fetchCapabilities();

        return () => {
            cancelled = true;
            clearTimeout(timer);
            controller?.abort();
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

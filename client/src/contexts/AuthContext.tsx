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
import { createContext, useState, useEffect, useCallback, useMemo } from 'react';
import { apiGet, apiPost } from '../utils/apiClient';
import { clearAlertAnalysisCache } from '../hooks/useAlertAnalysis';
import { clearChartAnalysisCache } from '../hooks/useChartAnalysis';
import { clearAnalysisCache as clearServerAnalysisCache } from '../hooks/useServerAnalysis';
import { clearQueryAnalysisCache } from '../hooks/useQueryAnalysis';
import { clearAlertReasoningCache } from '../hooks/useAlertReasoning';
import { clearSqlValidationCache } from '../utils/sqlValidation';
import { logger } from '../utils/logger';

// Wildcard entry in admin_permissions that grants every admin permission.
// Mirrors the server-side constant AdminPermissionWildcard in
// server/src/internal/auth/token_scope.go.
export const ADMIN_PERMISSION_WILDCARD = '*';

export interface User {
    authenticated: boolean;
    username: string;
    isSuperuser?: boolean;
    expiresAt?: string;
    /**
     * Whether the account may own API tokens. False for an account that
     * signs in through an identity provider while the installation keeps
     * API tokens off for federated logins (issue #742); it says nothing
     * about whether the user may manage tokens, which adminPermissions
     * answers. Left undefined when unknown, which counts as allowed.
     */
    apiTokensAllowed?: boolean;
}

export interface AuthContextValue {
    user: User | null;
    loading: boolean;
    login: (username: string, password: string) => Promise<void>;
    logout: () => Promise<void>;
    forceLogout: () => Promise<void>;
    adminPermissions: string[];
    hasPermission: (perm: string) => boolean;
    hasAnyAdminAccess: boolean;
}

interface AuthProviderProps {
    children: React.ReactNode;
}

interface UserInfoResponse {
    authenticated: boolean;
    username: string;
    is_superuser?: boolean;
    admin_permissions?: string[];
    // Absent from servers that predate issue #742, which allow tokens.
    api_tokens_allowed?: boolean;
}

interface LoginResponse {
    success: boolean;
    message?: string;
    error?: string;
    expires_at?: string;
}

const AuthContext = createContext<AuthContextValue | null>(null);

/**
 * Drop every module-level cache that holds data fetched for the
 * signed-in user, so that none of it survives into the next user's
 * session in the same tab.
 */
const clearSessionCaches = (): void => {
    clearAlertAnalysisCache();
    clearChartAnalysisCache();
    clearServerAnalysisCache();
    clearQueryAnalysisCache();
    clearAlertReasoningCache();
    clearSqlValidationCache();
};

export const AuthProvider = ({ children }: AuthProviderProps): React.ReactElement => {
    const [user, setUser] = useState<User | null>(null);
    const [loading, setLoading] = useState<boolean>(true);
    const [adminPermissions, setAdminPermissions] = useState<string[]>([]);

    useEffect(() => {
        void checkAuth();
    }, []);

    const checkAuth = async (): Promise<void> => {
        try {
            // Validate session by calling the user info endpoint
            // The httpOnly cookie will be sent automatically with credentials: 'include'
            const userInfo = await apiGet<UserInfoResponse>('/api/v1/user/info');

            if (userInfo.authenticated) {
                setUser({
                    authenticated: true,
                    username: userInfo.username,
                    isSuperuser: userInfo.is_superuser ?? false,
                    apiTokensAllowed: userInfo.api_tokens_allowed ?? true
                });
                setAdminPermissions(userInfo.admin_permissions ?? []);
            } else {
                // Session is invalid - clear it
                setUser(null);
                setAdminPermissions([]);
            }
        } catch (error) {
            logger.error('Auth check failed:', error);
            // Invalid or expired session - clear it
            setUser(null);
            setAdminPermissions([]);
        } finally {
            setLoading(false);
        }
    };

    const login = useCallback(async (username: string, password: string): Promise<void> => {
        try {
            // Authenticate via REST API
            // The server will set an httpOnly cookie with the session token
            const result = await apiPost<LoginResponse>('/api/v1/auth/login', { username, password });

            if (!result.success) {
                throw new Error(result.message || 'Authentication failed');
            }

            // Fetch full user info including superuser status
            // The httpOnly cookie will be sent automatically
            try {
                const userInfo = await apiGet<UserInfoResponse>('/api/v1/user/info');
                setUser({
                    authenticated: true,
                    username: userInfo.username,
                    isSuperuser: userInfo.is_superuser ?? false,
                    apiTokensAllowed: userInfo.api_tokens_allowed ?? true,
                    expiresAt: result.expires_at
                });
                setAdminPermissions(userInfo.admin_permissions ?? []);
            } catch {
                // Fallback if user info fetch fails
                setUser({
                    authenticated: true,
                    username: username,
                    expiresAt: result.expires_at
                });
                setAdminPermissions([]);
            }
        } catch (error) {
            // Re-throw with user-friendly message
            throw new Error((error as Error).message || 'Login failed');
        }
    }, []);

    // Note: localStorage entries (including chat input history) are
    // intentionally retained across sessions to preserve user preferences
    // and command history between login sessions.
    const logout = useCallback(async (): Promise<void> => {
        try {
            // Call logout endpoint to clear the httpOnly cookie on the server
            await apiPost('/api/v1/auth/logout');
        } catch (error) {
            logger.error('Logout request failed:', error);
            // Continue with local logout even if server request fails
        }
        clearSessionCaches();
        setUser(null);
        setAdminPermissions([]);
    }, []);

    // Check if the user has a specific admin permission
    const hasPermission = useCallback((perm: string): boolean => {
        if (user?.isSuperuser) {return true;}
        if (adminPermissions.includes(ADMIN_PERMISSION_WILDCARD)) {return true;}
        return adminPermissions.includes(perm);
    }, [user?.isSuperuser, adminPermissions]);

    // True if the user is a superuser or has any admin permissions
    const hasAnyAdminAccess = useMemo((): boolean => {
        return !!(user?.isSuperuser || adminPermissions.length > 0);
    }, [user?.isSuperuser, adminPermissions]);

    // Force logout without any cleanup (used when session is invalidated)
    const forceLogout = useCallback(async (): Promise<void> => {
        try {
            await apiPost('/api/v1/auth/logout');
        } catch {
            // Ignore errors during force logout
        }
        clearSessionCaches();
        setUser(null);
        setAdminPermissions([]);
    }, []);

    const value: AuthContextValue = useMemo(() => ({
        user,
        loading,
        login,
        logout,
        forceLogout,
        adminPermissions,
        hasPermission,
        hasAnyAdminAccess,
    }), [user, loading, login, logout, forceLogout, adminPermissions, hasPermission, hasAnyAdminAccess]);

    return (
        <AuthContext.Provider value={value}>
            {children}
        </AuthContext.Provider>
    );
};

export default AuthContext;

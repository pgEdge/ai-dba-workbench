/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { createContext, useState, useEffect, useCallback, useMemo } from 'react';
import { apiGet, apiPost } from '../utils/apiClient';
import { clearAlertAnalysisCache } from '../hooks/useAlertAnalysis';
import { clearChartAnalysisCache } from '../hooks/useChartAnalysis';
import { clearAnalysisCache as clearServerAnalysisCache } from '../hooks/useServerAnalysis';
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
}

interface LoginResponse {
    success: boolean;
    message?: string;
    error?: string;
    expires_at?: string;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export const AuthProvider = ({ children }: AuthProviderProps): React.ReactElement => {
    const [user, setUser] = useState<User | null>(null);
    const [loading, setLoading] = useState<boolean>(true);
    const [adminPermissions, setAdminPermissions] = useState<string[]>([]);

    useEffect(() => {
        checkAuth();
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
                    isSuperuser: userInfo.is_superuser || false
                });
                setAdminPermissions(userInfo.admin_permissions || []);
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
                    isSuperuser: userInfo.is_superuser || false,
                    expiresAt: result.expires_at
                });
                setAdminPermissions(userInfo.admin_permissions || []);
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
        clearAlertAnalysisCache();
        clearChartAnalysisCache();
        clearServerAnalysisCache();
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
        clearAlertAnalysisCache();
        clearChartAnalysisCache();
        clearServerAnalysisCache();
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

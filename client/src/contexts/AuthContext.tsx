/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { createContext, useState, useContext, useEffect, useCallback, useMemo } from 'react';

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

// API base URL for authentication
const API_BASE_URL = '/api/v1';

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
            const response = await fetch(`${API_BASE_URL}/user/info`, {
                credentials: 'include'
            });

            if (!response.ok) {
                throw new Error('Failed to fetch user info');
            }

            const userInfo: UserInfoResponse = await response.json();

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
            console.error('Auth check failed:', error);
            // Invalid or expired session - clear it
            setUser(null);
            setAdminPermissions([]);
        } finally {
            setLoading(false);
        }
    };

    const login = async (username: string, password: string): Promise<void> => {
        try {
            // Authenticate via REST API
            // The server will set an httpOnly cookie with the session token
            const response = await fetch(`${API_BASE_URL}/auth/login`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                credentials: 'include', // Receive the httpOnly cookie
                body: JSON.stringify({ username, password })
            });

            const result: LoginResponse = await response.json();

            if (!response.ok) {
                throw new Error(result.error || 'Login failed');
            }

            if (!result.success) {
                throw new Error(result.message || 'Authentication failed');
            }

            // Fetch full user info including superuser status
            // The httpOnly cookie will be sent automatically
            const userInfoResponse = await fetch(`${API_BASE_URL}/user/info`, {
                credentials: 'include'
            });

            if (userInfoResponse.ok) {
                const userInfo: UserInfoResponse = await userInfoResponse.json();
                setUser({
                    authenticated: true,
                    username: userInfo.username,
                    isSuperuser: userInfo.is_superuser || false,
                    expiresAt: result.expires_at
                });
                setAdminPermissions(userInfo.admin_permissions || []);
            } else {
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
    };

    const logout = async (): Promise<void> => {
        try {
            // Call logout endpoint to clear the httpOnly cookie on the server
            await fetch(`${API_BASE_URL}/auth/logout`, {
                method: 'POST',
                credentials: 'include'
            });
        } catch (error) {
            console.error('Logout request failed:', error);
            // Continue with local logout even if server request fails
        }
        setUser(null);
        setAdminPermissions([]);
    };

    // Check if the user has a specific admin permission
    const hasPermission = useCallback((perm: string): boolean => {
        if (user?.isSuperuser) return true;
        return adminPermissions.includes(perm);
    }, [user?.isSuperuser, adminPermissions]);

    // True if the user is a superuser or has any admin permissions
    const hasAnyAdminAccess = useMemo((): boolean => {
        return !!(user?.isSuperuser || adminPermissions.length > 0);
    }, [user?.isSuperuser, adminPermissions]);

    // Force logout without any cleanup (used when session is invalidated)
    const forceLogout = async (): Promise<void> => {
        try {
            await fetch(`${API_BASE_URL}/auth/logout`, {
                method: 'POST',
                credentials: 'include'
            });
        } catch (_error) {
            // Ignore errors during force logout
        }
        setUser(null);
        setAdminPermissions([]);
    };

    return (
        <AuthContext.Provider value={{
            user,
            loading,
            login,
            logout,
            forceLogout,
            adminPermissions,
            hasPermission,
            hasAnyAdminAccess,
        }}>
            {children}
        </AuthContext.Provider>
    );
};

export const useAuth = (): AuthContextValue => {
    const context = useContext(AuthContext);
    if (!context) {
        throw new Error('useAuth must be used within an AuthProvider');
    }
    return context;
};

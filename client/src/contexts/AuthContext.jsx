/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { createContext, useState, useContext, useEffect } from 'react';

const AuthContext = createContext(null);

// API base URL for authentication
const API_BASE_URL = '/api/v1';

export const AuthProvider = ({ children }) => {
    const [user, setUser] = useState(null);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        checkAuth();
    }, []);

    const checkAuth = async () => {
        try {
            // Validate session by calling the user info endpoint
            // The httpOnly cookie will be sent automatically with credentials: 'include'
            const response = await fetch(`${API_BASE_URL}/user/info`, {
                credentials: 'include'
            });

            if (!response.ok) {
                throw new Error('Failed to fetch user info');
            }

            const userInfo = await response.json();

            if (userInfo.authenticated) {
                setUser({
                    authenticated: true,
                    username: userInfo.username,
                    isSuperuser: userInfo.is_superuser || false
                });
            } else {
                // Session is invalid - clear it
                setUser(null);
            }
        } catch (error) {
            console.error('Auth check failed:', error);
            // Invalid or expired session - clear it
            setUser(null);
        } finally {
            setLoading(false);
        }
    };

    const login = async (username, password) => {
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

            const result = await response.json();

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
                const userInfo = await userInfoResponse.json();
                setUser({
                    authenticated: true,
                    username: userInfo.username,
                    isSuperuser: userInfo.is_superuser || false,
                    expiresAt: result.expires_at
                });
            } else {
                // Fallback if user info fetch fails
                setUser({
                    authenticated: true,
                    username: username,
                    expiresAt: result.expires_at
                });
            }
        } catch (error) {
            // Re-throw with user-friendly message
            throw new Error(error.message || 'Login failed');
        }
    };

    const logout = async () => {
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
    };

    // Force logout without any cleanup (used when session is invalidated)
    const forceLogout = async () => {
        try {
            await fetch(`${API_BASE_URL}/auth/logout`, {
                method: 'POST',
                credentials: 'include'
            });
        } catch (error) {
            // Ignore errors during force logout
        }
        setUser(null);
    };

    return (
        <AuthContext.Provider value={{
            user,
            loading,
            login,
            logout,
            forceLogout
        }}>
            {children}
        </AuthContext.Provider>
    );
};

export const useAuth = () => {
    const context = useContext(AuthContext);
    if (!context) {
        throw new Error('useAuth must be used within an AuthProvider');
    }
    return context;
};

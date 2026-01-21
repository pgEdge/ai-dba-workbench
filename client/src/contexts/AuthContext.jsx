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
const API_BASE_URL = '/api';

export const AuthProvider = ({ children }) => {
    const [user, setUser] = useState(null);
    const [sessionToken, setSessionToken] = useState(() => {
        // Load session token from localStorage on initialization
        return localStorage.getItem('session-token');
    });
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        checkAuth();
    }, []);

    const checkAuth = async () => {
        try {
            if (!sessionToken) {
                setLoading(false);
                return;
            }

            // Validate session by calling the user info endpoint
            const response = await fetch(`${API_BASE_URL}/user/info`, {
                headers: {
                    'Authorization': `Bearer ${sessionToken}`
                }
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
                throw new Error('Session invalid');
            }
        } catch (error) {
            console.error('Auth check failed:', error);
            // Invalid or expired session - clear it
            setSessionToken(null);
            localStorage.removeItem('session-token');
            setUser(null);
        } finally {
            setLoading(false);
        }
    };

    const login = async (username, password) => {
        try {
            // Authenticate via REST API
            const response = await fetch(`${API_BASE_URL}/auth/login`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ username, password })
            });

            const result = await response.json();

            if (!response.ok) {
                throw new Error(result.error || 'Login failed');
            }

            if (!result.success || !result.session_token) {
                throw new Error(result.message || 'Authentication failed');
            }

            // Store session token in state and localStorage
            setSessionToken(result.session_token);
            localStorage.setItem('session-token', result.session_token);

            // Fetch full user info including superuser status
            const userInfoResponse = await fetch(`${API_BASE_URL}/user/info`, {
                headers: {
                    'Authorization': `Bearer ${result.session_token}`
                }
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
                    username: username,
                    expiresAt: result.expires_at
                });
            }
        } catch (error) {
            // Re-throw with user-friendly message
            throw new Error(error.message || 'Login failed');
        }
    };

    const logout = () => {
        // Clear session token
        setSessionToken(null);
        localStorage.removeItem('session-token');
        setUser(null);
    };

    // Force logout without any cleanup (used when session is invalidated)
    const forceLogout = () => {
        setSessionToken(null);
        localStorage.removeItem('session-token');
        setUser(null);
    };

    return (
        <AuthContext.Provider value={{
            user,
            sessionToken,
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

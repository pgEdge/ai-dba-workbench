/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# API Integration Patterns

This document outlines how the pgEdge AI DBA Workbench React client communicates
with the backend MCP server, including authentication, error handling, and
real-time updates via Server-Sent Events (SSE).

## API Service Layer Architecture

### Base API Client

```typescript
// src/services/api.ts
import { getAuthToken } from './auth';

export class APIError extends Error {
    constructor(
        message: string,
        public statusCode: number,
        public code?: string
    ) {
        super(message);
        this.name = 'APIError';
    }
}

interface RequestConfig extends RequestInit {
    requiresAuth?: boolean;
    timeout?: number;
}

class APIClient {
    private baseURL: string;
    private defaultTimeout: number;

    constructor() {
        this.baseURL = import.meta.env.VITE_API_URL || 'http://localhost:8080';
        this.defaultTimeout = parseInt(
            import.meta.env.VITE_API_TIMEOUT || '30000',
            10
        );
    }

    private async request<T>(
        endpoint: string,
        config: RequestConfig = {}
    ): Promise<T> {
        const {
            requiresAuth = true,
            timeout = this.defaultTimeout,
            headers = {},
            ...rest
        } = config;

        // Build headers
        const requestHeaders: HeadersInit = {
            'Content-Type': 'application/json',
            ...headers,
        };

        // Add authentication token if required
        if (requiresAuth) {
            const token = await getAuthToken();
            if (!token) {
                throw new APIError('No authentication token', 401, 'UNAUTHORIZED');
            }
            requestHeaders['Authorization'] = `Bearer ${token}`;
        }

        // Create abort controller for timeout
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), timeout);

        try {
            const response = await fetch(`${this.baseURL}${endpoint}`, {
                ...rest,
                headers: requestHeaders,
                signal: controller.signal,
            });

            clearTimeout(timeoutId);

            // Handle non-2xx responses
            if (!response.ok) {
                const errorData = await response.json().catch(() => ({}));
                throw new APIError(
                    errorData.message || `HTTP ${response.status}`,
                    response.status,
                    errorData.code
                );
            }

            // Handle 204 No Content
            if (response.status === 204) {
                return undefined as T;
            }

            // Parse JSON response
            return await response.json();
        } catch (error) {
            if (error instanceof APIError) {
                throw error;
            }

            if (error instanceof Error) {
                if (error.name === 'AbortError') {
                    throw new APIError('Request timeout', 408, 'TIMEOUT');
                }
                throw new APIError(
                    error.message || 'Network error',
                    0,
                    'NETWORK_ERROR'
                );
            }

            throw new APIError('Unknown error', 0, 'UNKNOWN_ERROR');
        }
    }

    async get<T>(endpoint: string, config?: RequestConfig): Promise<T> {
        return this.request<T>(endpoint, {
            ...config,
            method: 'GET',
        });
    }

    async post<T>(
        endpoint: string,
        data?: any,
        config?: RequestConfig
    ): Promise<T> {
        return this.request<T>(endpoint, {
            ...config,
            method: 'POST',
            body: data ? JSON.stringify(data) : undefined,
        });
    }

    async put<T>(
        endpoint: string,
        data?: any,
        config?: RequestConfig
    ): Promise<T> {
        return this.request<T>(endpoint, {
            ...config,
            method: 'PUT',
            body: data ? JSON.stringify(data) : undefined,
        });
    }

    async patch<T>(
        endpoint: string,
        data?: any,
        config?: RequestConfig
    ): Promise<T> {
        return this.request<T>(endpoint, {
            ...config,
            method: 'PATCH',
            body: data ? JSON.stringify(data) : undefined,
        });
    }

    async delete<T>(endpoint: string, config?: RequestConfig): Promise<T> {
        return this.request<T>(endpoint, {
            ...config,
            method: 'DELETE',
        });
    }
}

export const apiClient = new APIClient();
```

### Authentication Service

```typescript
// src/services/auth.ts
import { apiClient } from './api';
import type { User, AuthToken, LoginResponse } from '../types/models';

// Token storage (in-memory)
let authToken: AuthToken | null = null;
let tokenRefreshTimer: NodeJS.Timeout | null = null;

/**
 * Store authentication token in memory and set up refresh timer
 */
export const setAuthToken = (token: AuthToken) => {
    authToken = token;

    // Clear existing refresh timer
    if (tokenRefreshTimer) {
        clearTimeout(tokenRefreshTimer);
    }

    // Set up token refresh (refresh 5 minutes before expiry)
    const expiresIn = new Date(token.expiresAt).getTime() - Date.now();
    const refreshIn = Math.max(0, expiresIn - 5 * 60 * 1000);

    tokenRefreshTimer = setTimeout(() => {
        refreshAuthToken().catch((error) => {
            console.error('Token refresh failed:', error);
            // Logout on refresh failure
            clearAuthToken();
        });
    }, refreshIn);
};

/**
 * Get current authentication token
 */
export const getAuthToken = async (): Promise<string | null> => {
    if (!authToken) {
        // Try to restore from httpOnly cookie via API call
        try {
            const token = await verifySession();
            if (token) {
                setAuthToken(token);
                return token.value;
            }
        } catch {
            return null;
        }
    }

    return authToken?.value || null;
};

/**
 * Clear authentication token
 */
export const clearAuthToken = () => {
    authToken = null;
    if (tokenRefreshTimer) {
        clearTimeout(tokenRefreshTimer);
        tokenRefreshTimer = null;
    }
};

/**
 * Login with username and password
 */
export const login = async (
    username: string,
    password: string
): Promise<LoginResponse> => {
    const response = await apiClient.post<LoginResponse>(
        '/api/auth/login',
        { username, password },
        { requiresAuth: false }
    );

    setAuthToken(response.token);
    return response;
};

/**
 * Logout and invalidate token
 */
export const logout = async (): Promise<void> => {
    try {
        await apiClient.post('/api/auth/logout');
    } finally {
        clearAuthToken();
    }
};

/**
 * Refresh authentication token
 */
export const refreshAuthToken = async (): Promise<AuthToken> => {
    const response = await apiClient.post<{ token: AuthToken }>(
        '/api/auth/refresh'
    );
    setAuthToken(response.token);
    return response.token;
};

/**
 * Verify current session (restore from httpOnly cookie)
 */
export const verifySession = async (): Promise<AuthToken | null> => {
    try {
        const response = await apiClient.get<{ token: AuthToken }>(
            '/api/auth/verify'
        );
        return response.token;
    } catch {
        return null;
    }
};

/**
 * Change user password
 */
export const changePassword = async (
    currentPassword: string,
    newPassword: string
): Promise<void> => {
    await apiClient.post('/api/auth/change-password', {
        currentPassword,
        newPassword,
    });
};
```

### Connection Service

```typescript
// src/services/connections.ts
import { apiClient } from './api';
import type {
    Connection,
    CreateConnectionInput,
    UpdateConnectionInput,
} from '../types/models';

/**
 * Get all connections accessible to the current user
 */
export const getConnections = async (): Promise<Connection[]> => {
    return apiClient.get<Connection[]>('/api/connections');
};

/**
 * Get a specific connection by ID
 */
export const getConnection = async (id: string): Promise<Connection> => {
    return apiClient.get<Connection>(`/api/connections/${id}`);
};

/**
 * Create a new connection
 */
export const createConnection = async (
    data: CreateConnectionInput
): Promise<Connection> => {
    return apiClient.post<Connection>('/api/connections', data);
};

/**
 * Update an existing connection
 */
export const updateConnection = async (
    id: string,
    data: UpdateConnectionInput
): Promise<Connection> => {
    return apiClient.patch<Connection>(`/api/connections/${id}`, data);
};

/**
 * Delete a connection
 */
export const deleteConnection = async (id: string): Promise<void> => {
    return apiClient.delete(`/api/connections/${id}`);
};

/**
 * Test a connection
 */
export const testConnection = async (
    id: string
): Promise<{ success: boolean; message?: string }> => {
    return apiClient.post(`/api/connections/${id}/test`);
};
```

### Metrics Service

```typescript
// src/services/metrics.ts
import { apiClient } from './api';
import type { Metric, TimeRange } from '../types/models';

/**
 * Get metrics for a connection within a time range
 */
export const getMetrics = async (
    connectionId: string,
    timeRange: TimeRange
): Promise<Metric[]> => {
    const params = new URLSearchParams({ timeRange });
    return apiClient.get<Metric[]>(
        `/api/connections/${connectionId}/metrics?${params}`
    );
};

/**
 * Get paginated metrics
 */
export const getMetricsPage = async (
    connectionId: string,
    page: number,
    limit: number = 50
): Promise<{ metrics: Metric[]; hasMore: boolean }> => {
    const params = new URLSearchParams({
        page: page.toString(),
        limit: limit.toString(),
    });
    return apiClient.get(
        `/api/connections/${connectionId}/metrics?${params}`
    );
};

/**
 * Get real-time snapshot of current metrics
 */
export const getRealtimeMetrics = async (
    connectionId: string
): Promise<Metric[]> => {
    return apiClient.get<Metric[]>(
        `/api/connections/${connectionId}/metrics/realtime`
    );
};
```

## Server-Sent Events (SSE) for Real-time Updates

### SSE Client Utility

```typescript
// src/services/sse.ts
import { getAuthToken } from './auth';

export type SSEEventHandler = (data: any) => void;
export type SSEErrorHandler = (error: Error) => void;

interface SSEClientOptions {
    onMessage: SSEEventHandler;
    onError?: SSEErrorHandler;
    onOpen?: () => void;
    reconnect?: boolean;
    reconnectDelay?: number;
}

export class SSEClient {
    private eventSource: EventSource | null = null;
    private endpoint: string;
    private options: SSEClientOptions;
    private reconnectAttempts = 0;
    private maxReconnectAttempts = 5;
    private reconnectTimer: NodeJS.Timeout | null = null;

    constructor(endpoint: string, options: SSEClientOptions) {
        this.endpoint = endpoint;
        this.options = {
            reconnect: true,
            reconnectDelay: 3000,
            ...options,
        };
    }

    async connect(): Promise<void> {
        try {
            // Get authentication token
            const token = await getAuthToken();
            if (!token) {
                throw new Error('No authentication token');
            }

            // EventSource doesn't support custom headers, so we pass token as query param
            // Note: This is less secure than headers. Consider upgrading to WebSockets for production.
            const url = `${import.meta.env.VITE_API_URL}${this.endpoint}?token=${encodeURIComponent(token)}`;

            this.eventSource = new EventSource(url);

            this.eventSource.onopen = () => {
                this.reconnectAttempts = 0;
                this.options.onOpen?.();
            };

            this.eventSource.onmessage = (event) => {
                try {
                    const data = JSON.parse(event.data);
                    this.options.onMessage(data);
                } catch (error) {
                    console.error('Failed to parse SSE data:', error);
                }
            };

            this.eventSource.onerror = (event) => {
                const error = new Error('SSE connection error');
                this.options.onError?.(error);

                // Attempt reconnection if enabled
                if (
                    this.options.reconnect &&
                    this.reconnectAttempts < this.maxReconnectAttempts
                ) {
                    this.reconnect();
                } else {
                    this.disconnect();
                }
            };
        } catch (error) {
            this.options.onError?.(
                error instanceof Error ? error : new Error('Failed to connect')
            );
        }
    }

    private reconnect(): void {
        this.reconnectAttempts++;
        this.disconnect();

        this.reconnectTimer = setTimeout(() => {
            console.log(
                `Attempting SSE reconnection (${this.reconnectAttempts}/${this.maxReconnectAttempts})`
            );
            this.connect();
        }, this.options.reconnectDelay);
    }

    disconnect(): void {
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = null;
        }

        if (this.eventSource) {
            this.eventSource.close();
            this.eventSource = null;
        }
    }

    isConnected(): boolean {
        return (
            this.eventSource !== null &&
            this.eventSource.readyState === EventSource.OPEN
        );
    }
}
```

### SSE Hook for React Components

```typescript
// src/hooks/useSSE.ts
import { useEffect, useState, useRef } from 'react';
import { SSEClient, SSEEventHandler } from '../services/sse';

interface UseSSEOptions {
    onMessage: SSEEventHandler;
    onError?: (error: Error) => void;
    enabled?: boolean;
}

export const useSSE = (endpoint: string, options: UseSSEOptions) => {
    const { onMessage, onError, enabled = true } = options;
    const [isConnected, setIsConnected] = useState(false);
    const clientRef = useRef<SSEClient | null>(null);

    useEffect(() => {
        if (!enabled) {
            return;
        }

        const client = new SSEClient(endpoint, {
            onMessage,
            onError: (error) => {
                setIsConnected(false);
                onError?.(error);
            },
            onOpen: () => {
                setIsConnected(true);
            },
        });

        clientRef.current = client;
        client.connect();

        return () => {
            client.disconnect();
            clientRef.current = null;
        };
    }, [endpoint, enabled, onMessage, onError]);

    return { isConnected };
};

// Usage example
const MetricsMonitor: React.FC<{ connectionId: string }> = ({
    connectionId,
}) => {
    const [metrics, setMetrics] = useState<Metric[]>([]);

    const { isConnected } = useSSE(
        `/api/connections/${connectionId}/metrics/stream`,
        {
            onMessage: (newMetric: Metric) => {
                setMetrics((prev) => [...prev, newMetric]);
            },
            onError: (error) => {
                console.error('SSE error:', error);
            },
        }
    );

    return (
        <div>
            <Chip
                label={isConnected ? 'Connected' : 'Disconnected'}
                color={isConnected ? 'success' : 'error'}
            />
            <MetricsChart data={metrics} />
        </div>
    );
};
```

## Error Handling Patterns

### Global Error Handler

```typescript
// src/utils/errorHandler.ts
import { APIError } from '../services/api';

export interface ErrorInfo {
    message: string;
    code?: string;
    statusCode?: number;
    retryable: boolean;
}

export const handleAPIError = (error: unknown): ErrorInfo => {
    if (error instanceof APIError) {
        // Determine if error is retryable
        const retryable =
            error.statusCode >= 500 ||
            error.code === 'TIMEOUT' ||
            error.code === 'NETWORK_ERROR';

        // Map error codes to user-friendly messages
        const message = mapErrorMessage(error.code, error.message);

        return {
            message,
            code: error.code,
            statusCode: error.statusCode,
            retryable,
        };
    }

    if (error instanceof Error) {
        return {
            message: error.message,
            retryable: false,
        };
    }

    return {
        message: 'An unexpected error occurred',
        retryable: false,
    };
};

const mapErrorMessage = (
    code: string | undefined,
    defaultMessage: string
): string => {
    const messages: Record<string, string> = {
        UNAUTHORIZED: 'Please log in to continue',
        FORBIDDEN: 'You do not have permission to perform this action',
        NOT_FOUND: 'The requested resource was not found',
        TIMEOUT: 'Request timed out. Please try again.',
        NETWORK_ERROR: 'Network error. Please check your connection.',
        VALIDATION_ERROR: 'Invalid input. Please check your data.',
    };

    return messages[code || ''] || defaultMessage;
};
```

### Error Boundary with Retry

```typescript
// src/components/common/ErrorBoundaryWithRetry.tsx
import React, { Component, ReactNode } from 'react';
import { Alert, Button, Box } from '@mui/material';

interface Props {
    children: ReactNode;
    fallback?: ReactNode;
}

interface State {
    hasError: boolean;
    error: Error | null;
}

export class ErrorBoundaryWithRetry extends Component<Props, State> {
    constructor(props: Props) {
        super(props);
        this.state = { hasError: false, error: null };
    }

    static getDerivedStateFromError(error: Error): State {
        return { hasError: true, error };
    }

    componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
        console.error('Error caught by boundary:', error, errorInfo);
    }

    handleReset = () => {
        this.setState({ hasError: false, error: null });
    };

    render() {
        if (this.state.hasError) {
            if (this.props.fallback) {
                return this.props.fallback;
            }

            return (
                <Box p={4}>
                    <Alert severity="error" sx={{ mb: 2 }}>
                        <strong>Something went wrong</strong>
                        {this.state.error && (
                            <div>{this.state.error.message}</div>
                        )}
                    </Alert>
                    <Button variant="contained" onClick={this.handleReset}>
                        Try Again
                    </Button>
                </Box>
            );
        }

        return this.props.children;
    }
}
```

### Query Error Handling with React Query

```typescript
// src/hooks/useConnections.ts
import { useQuery } from '@tanstack/react-query';
import { getConnections } from '../services/connections';
import { handleAPIError } from '../utils/errorHandler';
import { useNotification } from '../contexts/NotificationContext';

export const useConnections = () => {
    const { showNotification } = useNotification();

    return useQuery({
        queryKey: ['connections'],
        queryFn: getConnections,
        onError: (error) => {
            const errorInfo = handleAPIError(error);
            showNotification(errorInfo.message, 'error');
        },
        retry: (failureCount, error) => {
            const errorInfo = handleAPIError(error);
            // Only retry if error is retryable and we haven't exceeded max retries
            return errorInfo.retryable && failureCount < 3;
        },
        retryDelay: (attemptIndex) => {
            // Exponential backoff: 1s, 2s, 4s
            return Math.min(1000 * 2 ** attemptIndex, 10000);
        },
    });
};
```

## Request Cancellation

### Cancellation with React Query

```typescript
// React Query automatically cancels queries when components unmount
// or when a new query with the same key is started

// For manual cancellation:
import { useQuery, useQueryClient } from '@tanstack/react-query';

const MyComponent: React.FC = () => {
    const queryClient = useQueryClient();

    const { data, isLoading } = useQuery({
        queryKey: ['data'],
        queryFn: fetchData,
    });

    const handleCancel = () => {
        // Cancel all queries with this key
        queryClient.cancelQueries({ queryKey: ['data'] });
    };

    return (
        <div>
            {isLoading && <Button onClick={handleCancel}>Cancel</Button>}
        </div>
    );
};
```

## Input Validation and Sanitization

### Client-Side Validation

```typescript
// src/utils/validation.ts

/**
 * Validate email format
 */
export const isValidEmail = (email: string): boolean => {
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    return emailRegex.test(email);
};

/**
 * Validate hostname (domain or IP)
 */
export const isValidHostname = (hostname: string): boolean => {
    // Domain regex
    const domainRegex =
        /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i;

    // IPv4 regex
    const ipv4Regex =
        /^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$/;

    // IPv6 regex (simplified)
    const ipv6Regex = /^([0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}$/i;

    return (
        domainRegex.test(hostname) ||
        ipv4Regex.test(hostname) ||
        ipv6Regex.test(hostname)
    );
};

/**
 * Validate port number
 */
export const isValidPort = (port: number): boolean => {
    return Number.isInteger(port) && port >= 1 && port <= 65535;
};

/**
 * Validate password strength
 */
export const validatePasswordStrength = (
    password: string
): { valid: boolean; message?: string } => {
    if (password.length < 8) {
        return {
            valid: false,
            message: 'Password must be at least 8 characters',
        };
    }

    if (!/[A-Z]/.test(password)) {
        return {
            valid: false,
            message: 'Password must contain at least one uppercase letter',
        };
    }

    if (!/[a-z]/.test(password)) {
        return {
            valid: false,
            message: 'Password must contain at least one lowercase letter',
        };
    }

    if (!/[0-9]/.test(password)) {
        return {
            valid: false,
            message: 'Password must contain at least one number',
        };
    }

    return { valid: true };
};

/**
 * Sanitize user input (prevent XSS)
 */
export const sanitizeInput = (input: string): string => {
    return input
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#x27;')
        .replace(/\//g, '&#x2F;');
};
```

## Rate Limiting and Throttling

### Throttled API Calls

```typescript
// src/utils/throttle.ts
export const throttle = <T extends (...args: any[]) => any>(
    func: T,
    limit: number
): ((...args: Parameters<T>) => void) => {
    let inThrottle: boolean;

    return function (this: any, ...args: Parameters<T>) {
        if (!inThrottle) {
            func.apply(this, args);
            inThrottle = true;
            setTimeout(() => (inThrottle = false), limit);
        }
    };
};

// Usage
const handleSearch = throttle((query: string) => {
    searchAPI(query);
}, 500); // Limit to once every 500ms
```

### Debounced API Calls

```typescript
// src/utils/debounce.ts
export const debounce = <T extends (...args: any[]) => any>(
    func: T,
    wait: number
): ((...args: Parameters<T>) => void) => {
    let timeout: NodeJS.Timeout | null = null;

    return function (this: any, ...args: Parameters<T>) {
        if (timeout) {
            clearTimeout(timeout);
        }

        timeout = setTimeout(() => {
            func.apply(this, args);
        }, wait);
    };
};

// Usage in component
const SearchInput: React.FC = () => {
    const [query, setQuery] = useState('');

    const debouncedSearch = useMemo(
        () =>
            debounce((searchQuery: string) => {
                searchAPI(searchQuery);
            }, 300),
        []
    );

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const value = e.target.value;
        setQuery(value);
        debouncedSearch(value);
    };

    return <TextField value={query} onChange={handleChange} />;
};
```

## Summary

- Use a centralized API client with authentication and error handling
- Store tokens securely (memory + httpOnly cookies)
- Implement automatic token refresh before expiry
- Use React Query for caching and automatic retry
- Handle SSE connections with automatic reconnection
- Validate inputs on both client and server
- Sanitize all user inputs to prevent XSS
- Implement proper error boundaries and user-friendly error messages
- Use throttling/debouncing for frequent API calls
- Follow defense-in-depth security practices

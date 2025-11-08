/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# State Management Patterns and Practices

This document outlines state management strategies for the pgEdge AI Workbench
React frontend, covering different types of state and when to use each
approach.

## State Categories

### 1. Server State

Data that lives on the server and is synchronized to the client.

**Examples**:
- User data
- Database connections
- Monitoring metrics
- Configuration settings

**Management**: React Query (TanStack Query)

**Characteristics**:
- Asynchronous
- Potentially stale
- Needs caching
- Can be shared across components
- Requires loading and error states

### 2. Global UI State

Application-wide state needed across multiple components.

**Examples**:
- Authentication status and user info
- Theme preferences (light/dark mode)
- Active connection selection
- Global notifications/alerts

**Management**: React Context + useReducer

**Characteristics**:
- Synchronous
- Persists across navigation
- Accessible from any component
- Changes trigger re-renders

### 3. Local Component State

State specific to a single component or feature.

**Examples**:
- Form input values
- Modal open/closed state
- Accordion expanded/collapsed state
- Local filtering or sorting

**Management**: useState hook

**Characteristics**:
- Scoped to component
- Simple and straightforward
- No need for global access
- Ephemeral (lost on unmount)

### 4. URL State

State synchronized with the browser URL.

**Examples**:
- Current route/page
- Query parameters for filters
- Pagination state
- Selected resource IDs

**Management**: React Router

**Characteristics**:
- Shareable via URL
- Bookmarkable
- Browser history integration
- Persists across page refreshes

## React Query for Server State

### Setup

```typescript
// src/main.tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ReactQueryDevtools } from '@tanstack/react-query-devtools';

const queryClient = new QueryClient({
    defaultOptions: {
        queries: {
            staleTime: 1000 * 60 * 5, // 5 minutes
            cacheTime: 1000 * 60 * 30, // 30 minutes
            retry: 3,
            retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, 30000),
            refetchOnWindowFocus: false,
        },
        mutations: {
            retry: 1,
        },
    },
});

ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
        <QueryClientProvider client={queryClient}>
            <App />
            <ReactQueryDevtools initialIsOpen={false} />
        </QueryClientProvider>
    </React.StrictMode>
);
```

### Query Pattern

```typescript
// src/hooks/useConnections.ts
import { useQuery } from '@tanstack/react-query';
import { getConnections } from '../services/connections';
import type { Connection } from '../types/models';

export const useConnections = () => {
    return useQuery<Connection[], Error>({
        queryKey: ['connections'],
        queryFn: getConnections,
        staleTime: 1000 * 60 * 5, // Override default if needed
    });
};

// Usage in component
const ConnectionList: React.FC = () => {
    const { data: connections, isLoading, error, refetch } = useConnections();

    if (isLoading) {
        return <CircularProgress />;
    }

    if (error) {
        return <Alert severity="error">Error: {error.message}</Alert>;
    }

    return (
        <div>
            {connections?.map((conn) => (
                <ConnectionCard key={conn.id} connection={conn} />
            ))}
            <Button onClick={() => refetch()}>Refresh</Button>
        </div>
    );
};
```

### Query with Parameters

```typescript
// src/hooks/useMetrics.ts
import { useQuery } from '@tanstack/react-query';
import { getMetrics } from '../services/metrics';
import type { Metric, TimeRange } from '../types/models';

interface UseMetricsOptions {
    connectionId: string;
    timeRange: TimeRange;
    enabled?: boolean;
}

export const useMetrics = ({
    connectionId,
    timeRange,
    enabled = true,
}: UseMetricsOptions) => {
    return useQuery<Metric[], Error>({
        queryKey: ['metrics', connectionId, timeRange],
        queryFn: () => getMetrics(connectionId, timeRange),
        enabled: enabled && !!connectionId,
        refetchInterval: 1000 * 30, // Refetch every 30 seconds
    });
};

// Usage
const MetricsChart: React.FC<{ connectionId: string }> = ({ connectionId }) => {
    const [timeRange, setTimeRange] = useState<TimeRange>('1h');

    const { data: metrics, isLoading } = useMetrics({
        connectionId,
        timeRange,
    });

    return (
        <div>
            <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
            {isLoading ? <LoadingSpinner /> : <Chart data={metrics} />}
        </div>
    );
};
```

### Mutation Pattern

```typescript
// src/hooks/useCreateConnection.ts
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { createConnection } from '../services/connections';
import type { CreateConnectionInput, Connection } from '../types/models';

export const useCreateConnection = () => {
    const queryClient = useQueryClient();

    return useMutation<Connection, Error, CreateConnectionInput>({
        mutationFn: createConnection,
        onSuccess: (newConnection) => {
            // Invalidate and refetch connections list
            queryClient.invalidateQueries({ queryKey: ['connections'] });

            // Or optimistically update the cache
            queryClient.setQueryData<Connection[]>(['connections'], (old) => {
                return old ? [...old, newConnection] : [newConnection];
            });
        },
        onError: (error) => {
            console.error('Failed to create connection:', error);
        },
    });
};

// Usage
const CreateConnectionForm: React.FC = () => {
    const createConnection = useCreateConnection();

    const handleSubmit = async (values: CreateConnectionInput) => {
        try {
            await createConnection.mutateAsync(values);
            // Handle success (e.g., show notification, close form)
        } catch (error) {
            // Error is already logged by onError
            // Handle UI error state
        }
    };

    return (
        <form onSubmit={handleSubmit}>
            {/* Form fields */}
            <Button
                type="submit"
                disabled={createConnection.isPending}
            >
                {createConnection.isPending ? 'Creating...' : 'Create'}
            </Button>
            {createConnection.isError && (
                <Alert severity="error">
                    {createConnection.error.message}
                </Alert>
            )}
        </form>
    );
};
```

### Optimistic Updates

```typescript
// src/hooks/useUpdateConnection.ts
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { updateConnection } from '../services/connections';
import type { Connection, UpdateConnectionInput } from '../types/models';

export const useUpdateConnection = () => {
    const queryClient = useQueryClient();

    return useMutation<
        Connection,
        Error,
        { id: string; updates: UpdateConnectionInput }
    >({
        mutationFn: ({ id, updates }) => updateConnection(id, updates),

        // Optimistically update the cache before the request completes
        onMutate: async ({ id, updates }) => {
            // Cancel outgoing refetches
            await queryClient.cancelQueries({ queryKey: ['connections'] });

            // Snapshot previous value
            const previousConnections = queryClient.getQueryData<Connection[]>([
                'connections',
            ]);

            // Optimistically update
            queryClient.setQueryData<Connection[]>(['connections'], (old) => {
                return old?.map((conn) =>
                    conn.id === id ? { ...conn, ...updates } : conn
                );
            });

            // Return context with previous value
            return { previousConnections };
        },

        // Rollback on error
        onError: (err, variables, context) => {
            if (context?.previousConnections) {
                queryClient.setQueryData(
                    ['connections'],
                    context.previousConnections
                );
            }
        },

        // Refetch after success or error
        onSettled: () => {
            queryClient.invalidateQueries({ queryKey: ['connections'] });
        },
    });
};
```

### Infinite Queries (Pagination)

```typescript
// src/hooks/useInfiniteMetrics.ts
import { useInfiniteQuery } from '@tanstack/react-query';
import { getMetricsPage } from '../services/metrics';

export const useInfiniteMetrics = (connectionId: string) => {
    return useInfiniteQuery({
        queryKey: ['metrics', 'infinite', connectionId],
        queryFn: ({ pageParam = 0 }) =>
            getMetricsPage(connectionId, pageParam),
        getNextPageParam: (lastPage, allPages) => {
            return lastPage.hasMore ? allPages.length : undefined;
        },
        initialPageParam: 0,
    });
};

// Usage
const MetricsList: React.FC = () => {
    const {
        data,
        fetchNextPage,
        hasNextPage,
        isFetchingNextPage,
    } = useInfiniteMetrics('conn-123');

    return (
        <div>
            {data?.pages.map((page, i) => (
                <React.Fragment key={i}>
                    {page.metrics.map((metric) => (
                        <MetricItem key={metric.id} metric={metric} />
                    ))}
                </React.Fragment>
            ))}
            {hasNextPage && (
                <Button
                    onClick={() => fetchNextPage()}
                    disabled={isFetchingNextPage}
                >
                    {isFetchingNextPage ? 'Loading...' : 'Load More'}
                </Button>
            )}
        </div>
    );
};
```

## Context for Global UI State

### Authentication Context

```typescript
// src/contexts/AuthContext.tsx
import React, { createContext, useContext, useReducer, useEffect } from 'react';
import type { User, AuthToken } from '../types/models';

interface AuthState {
    user: User | null;
    token: AuthToken | null;
    isAuthenticated: boolean;
    isLoading: boolean;
}

type AuthAction =
    | { type: 'LOGIN_SUCCESS'; payload: { user: User; token: AuthToken } }
    | { type: 'LOGOUT' }
    | { type: 'REFRESH_TOKEN'; payload: { token: AuthToken } }
    | { type: 'SET_LOADING'; payload: boolean };

interface AuthContextValue extends AuthState {
    login: (username: string, password: string) => Promise<void>;
    logout: () => void;
    refreshToken: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

const authReducer = (state: AuthState, action: AuthAction): AuthState => {
    switch (action.type) {
        case 'LOGIN_SUCCESS':
            return {
                ...state,
                user: action.payload.user,
                token: action.payload.token,
                isAuthenticated: true,
                isLoading: false,
            };
        case 'LOGOUT':
            return {
                user: null,
                token: null,
                isAuthenticated: false,
                isLoading: false,
            };
        case 'REFRESH_TOKEN':
            return {
                ...state,
                token: action.payload.token,
            };
        case 'SET_LOADING':
            return {
                ...state,
                isLoading: action.payload,
            };
        default:
            return state;
    }
};

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({
    children,
}) => {
    const [state, dispatch] = useReducer(authReducer, {
        user: null,
        token: null,
        isAuthenticated: false,
        isLoading: true,
    });

    // Check for existing session on mount
    useEffect(() => {
        const checkAuth = async () => {
            try {
                // Attempt to restore session
                const token = await getStoredToken();
                if (token) {
                    const user = await verifyToken(token);
                    dispatch({
                        type: 'LOGIN_SUCCESS',
                        payload: { user, token },
                    });
                }
            } catch (error) {
                // Token invalid or expired
                console.error('Auth check failed:', error);
            } finally {
                dispatch({ type: 'SET_LOADING', payload: false });
            }
        };

        checkAuth();
    }, []);

    const login = async (username: string, password: string) => {
        dispatch({ type: 'SET_LOADING', payload: true });
        try {
            const { user, token } = await loginAPI(username, password);
            await storeToken(token);
            dispatch({ type: 'LOGIN_SUCCESS', payload: { user, token } });
        } catch (error) {
            dispatch({ type: 'SET_LOADING', payload: false });
            throw error;
        }
    };

    const logout = async () => {
        try {
            if (state.token) {
                await logoutAPI(state.token);
            }
        } catch (error) {
            console.error('Logout API call failed:', error);
        } finally {
            await clearStoredToken();
            dispatch({ type: 'LOGOUT' });
        }
    };

    const refreshToken = async () => {
        if (!state.token) return;

        try {
            const newToken = await refreshTokenAPI(state.token);
            await storeToken(newToken);
            dispatch({ type: 'REFRESH_TOKEN', payload: { token: newToken } });
        } catch (error) {
            // Refresh failed, logout user
            logout();
            throw error;
        }
    };

    return (
        <AuthContext.Provider
            value={{
                ...state,
                login,
                logout,
                refreshToken,
            }}
        >
            {children}
        </AuthContext.Provider>
    );
};

// Custom hook for consuming auth context
export const useAuth = () => {
    const context = useContext(AuthContext);
    if (!context) {
        throw new Error('useAuth must be used within AuthProvider');
    }
    return context;
};
```

### Theme Context

```typescript
// src/contexts/ThemeContext.tsx
import React, { createContext, useContext, useState, useMemo } from 'react';
import { ThemeProvider as MUIThemeProvider } from '@mui/material/styles';
import { createTheme } from '@mui/material/styles';
import { lightTheme, darkTheme } from '../styles/theme';

type ThemeMode = 'light' | 'dark';

interface ThemeContextValue {
    mode: ThemeMode;
    toggleTheme: () => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export const ThemeProvider: React.FC<{ children: React.ReactNode }> = ({
    children,
}) => {
    const [mode, setMode] = useState<ThemeMode>(() => {
        // Read from localStorage or system preference
        const stored = localStorage.getItem('theme-mode');
        if (stored === 'light' || stored === 'dark') return stored;

        return window.matchMedia('(prefers-color-scheme: dark)').matches
            ? 'dark'
            : 'light';
    });

    const toggleTheme = () => {
        setMode((prevMode) => {
            const newMode = prevMode === 'light' ? 'dark' : 'light';
            localStorage.setItem('theme-mode', newMode);
            return newMode;
        });
    };

    const theme = useMemo(
        () => createTheme(mode === 'light' ? lightTheme : darkTheme),
        [mode]
    );

    return (
        <ThemeContext.Provider value={{ mode, toggleTheme }}>
            <MUIThemeProvider theme={theme}>{children}</MUIThemeProvider>
        </ThemeContext.Provider>
    );
};

export const useTheme = () => {
    const context = useContext(ThemeContext);
    if (!context) {
        throw new Error('useTheme must be used within ThemeProvider');
    }
    return context;
};
```

### Connection Context

```typescript
// src/contexts/ConnectionContext.tsx
import React, { createContext, useContext, useState } from 'react';
import type { Connection } from '../types/models';

interface ConnectionContextValue {
    activeConnection: Connection | null;
    setActiveConnection: (connection: Connection | null) => void;
}

const ConnectionContext = createContext<ConnectionContextValue | null>(null);

export const ConnectionProvider: React.FC<{ children: React.ReactNode }> = ({
    children,
}) => {
    const [activeConnection, setActiveConnection] = useState<Connection | null>(
        null
    );

    return (
        <ConnectionContext.Provider
            value={{ activeConnection, setActiveConnection }}
        >
            {children}
        </ConnectionContext.Provider>
    );
};

export const useActiveConnection = () => {
    const context = useContext(ConnectionContext);
    if (!context) {
        throw new Error(
            'useActiveConnection must be used within ConnectionProvider'
        );
    }
    return context;
};
```

### Notification Context

```typescript
// src/contexts/NotificationContext.tsx
import React, { createContext, useContext, useState, useCallback } from 'react';
import { Snackbar, Alert } from '@mui/material';

type NotificationType = 'success' | 'error' | 'warning' | 'info';

interface Notification {
    id: string;
    message: string;
    type: NotificationType;
}

interface NotificationContextValue {
    showNotification: (message: string, type: NotificationType) => void;
}

const NotificationContext = createContext<NotificationContextValue | null>(null);

export const NotificationProvider: React.FC<{ children: React.ReactNode }> = ({
    children,
}) => {
    const [notifications, setNotifications] = useState<Notification[]>([]);

    const showNotification = useCallback(
        (message: string, type: NotificationType) => {
            const id = Math.random().toString(36).substring(7);
            setNotifications((prev) => [...prev, { id, message, type }]);

            // Auto-dismiss after 5 seconds
            setTimeout(() => {
                setNotifications((prev) => prev.filter((n) => n.id !== id));
            }, 5000);
        },
        []
    );

    const handleClose = (id: string) => {
        setNotifications((prev) => prev.filter((n) => n.id !== id));
    };

    return (
        <NotificationContext.Provider value={{ showNotification }}>
            {children}
            {notifications.map((notification) => (
                <Snackbar
                    key={notification.id}
                    open={true}
                    onClose={() => handleClose(notification.id)}
                    anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
                >
                    <Alert
                        severity={notification.type}
                        onClose={() => handleClose(notification.id)}
                    >
                        {notification.message}
                    </Alert>
                </Snackbar>
            ))}
        </NotificationContext.Provider>
    );
};

export const useNotification = () => {
    const context = useContext(NotificationContext);
    if (!context) {
        throw new Error(
            'useNotification must be used within NotificationProvider'
        );
    }
    return context;
};
```

## Local State with useState

### Simple Component State

```typescript
const ConnectionForm: React.FC = () => {
    const [formData, setFormData] = useState({
        host: '',
        port: 5432,
        database: '',
        username: '',
    });

    const handleChange = (field: string, value: any) => {
        setFormData((prev) => ({
            ...prev,
            [field]: value,
        }));
    };

    return (
        <form>
            <TextField
                label="Host"
                value={formData.host}
                onChange={(e) => handleChange('host', e.target.value)}
            />
            {/* Other fields */}
        </form>
    );
};
```

### Boolean State (Toggles)

```typescript
const ConnectionCard: React.FC = () => {
    const [isExpanded, setIsExpanded] = useState(false);
    const [showPassword, setShowPassword] = useState(false);

    return (
        <Card>
            <CardHeader
                action={
                    <IconButton onClick={() => setIsExpanded(!isExpanded)}>
                        {isExpanded ? <ExpandLess /> : <ExpandMore />}
                    </IconButton>
                }
            />
            <Collapse in={isExpanded}>
                <CardContent>
                    <TextField
                        type={showPassword ? 'text' : 'password'}
                        InputProps={{
                            endAdornment: (
                                <IconButton
                                    onClick={() => setShowPassword(!showPassword)}
                                >
                                    {showPassword ? <VisibilityOff /> : <Visibility />}
                                </IconButton>
                            ),
                        }}
                    />
                </CardContent>
            </Collapse>
        </Card>
    );
};
```

## URL State with React Router

### Query Parameters for Filters

```typescript
// src/pages/ConnectionsPage.tsx
import { useSearchParams } from 'react-router-dom';

const ConnectionsPage: React.FC = () => {
    const [searchParams, setSearchParams] = useSearchParams();

    const filter = searchParams.get('filter') || '';
    const sort = searchParams.get('sort') || 'name';

    const handleFilterChange = (newFilter: string) => {
        setSearchParams({
            filter: newFilter,
            sort,
        });
    };

    const handleSortChange = (newSort: string) => {
        setSearchParams({
            filter,
            sort: newSort,
        });
    };

    return (
        <div>
            <FilterInput value={filter} onChange={handleFilterChange} />
            <SortSelect value={sort} onChange={handleSortChange} />
            <ConnectionList filter={filter} sort={sort} />
        </div>
    );
};
```

### Route Parameters

```typescript
// src/pages/ConnectionDetailPage.tsx
import { useParams, useNavigate } from 'react-router-dom';

const ConnectionDetailPage: React.FC = () => {
    const { connectionId } = useParams<{ connectionId: string }>();
    const navigate = useNavigate();

    const { data: connection, isLoading } = useConnection(connectionId!);

    if (isLoading) return <LoadingSpinner />;
    if (!connection) {
        return <Alert severity="error">Connection not found</Alert>;
    }

    return (
        <div>
            <Button onClick={() => navigate('/connections')}>Back</Button>
            <ConnectionDetails connection={connection} />
        </div>
    );
};
```

## State Management Best Practices

### 1. Colocation

Keep state as close to where it's used as possible.

```typescript
// Good - state is local to component
const SearchBar: React.FC = () => {
    const [query, setQuery] = useState('');
    return <input value={query} onChange={(e) => setQuery(e.target.value)} />;
};

// Avoid - unnecessarily global state
const SearchContext = createContext(null);
```

### 2. Derived State

Calculate values from existing state instead of duplicating.

```typescript
// Good
const UserList: React.FC = () => {
    const [users, setUsers] = useState<User[]>([]);
    const [searchTerm, setSearchTerm] = useState('');

    // Derived state
    const filteredUsers = users.filter((user) =>
        user.name.toLowerCase().includes(searchTerm.toLowerCase())
    );

    return <div>{/* Render filteredUsers */}</div>;
};

// Avoid
const UserList: React.FC = () => {
    const [users, setUsers] = useState<User[]>([]);
    const [filteredUsers, setFilteredUsers] = useState<User[]>([]);
    const [searchTerm, setSearchTerm] = useState('');

    useEffect(() => {
        setFilteredUsers(
            users.filter((user) =>
                user.name.toLowerCase().includes(searchTerm.toLowerCase())
            )
        );
    }, [users, searchTerm]);
};
```

### 3. Immutable Updates

Always create new objects/arrays when updating state.

```typescript
// Good
setUsers((prev) => [...prev, newUser]);
setUser((prev) => ({ ...prev, name: 'New Name' }));

// Avoid
users.push(newUser); // Mutates array
setUsers(users);

user.name = 'New Name'; // Mutates object
setUser(user);
```

### 4. Batch Updates

React automatically batches updates in event handlers and lifecycle methods.

```typescript
// These will be batched into a single re-render
const handleClick = () => {
    setCount((c) => c + 1);
    setFlag((f) => !f);
    setData((d) => [...d, newItem]);
};
```

### 5. Lazy Initialization

For expensive initial state calculations.

```typescript
// Good - only runs once
const [state, setState] = useState(() => {
    return expensiveComputation();
});

// Avoid - runs on every render
const [state, setState] = useState(expensiveComputation());
```

## State Migration Checklist

When deciding where to store state:

1. **Is it server data?** → Use React Query
2. **Is it needed across multiple unrelated components?** → Use Context
3. **Is it tied to a URL?** → Use React Router
4. **Is it local to a component/feature?** → Use useState

## Summary

- Use **React Query** for all server state (API data, caching)
- Use **Context + useReducer** for global UI state (auth, theme)
- Use **useState** for local component state (forms, toggles)
- Use **React Router** for URL state (routes, query params)
- Keep state as local as possible
- Derive values instead of duplicating state
- Always update state immutably
- Use TypeScript for type safety across all state

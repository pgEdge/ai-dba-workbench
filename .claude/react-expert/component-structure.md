/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Component Structure and Organization

This document outlines component architecture patterns, organization
strategies, and best practices for the pgEdge AI DBA Workbench React frontend.

## Component Categories

### 1. Common Components

Reusable, generic UI components with no business logic.

**Location**: `src/components/common/`

**Examples**:
- Button variants (primary, secondary, danger)
- Input fields (text, password, number, select)
- Cards and panels
- Loading indicators
- Error boundaries
- Alerts and notifications
- Modals and dialogs
- Tables and data grids

**Characteristics**:
- Highly reusable across the application
- Prop-driven (controlled components)
- No direct API calls or business logic
- Well-documented with TypeScript interfaces
- Include accessibility attributes
- Fully tested with multiple prop combinations

### 2. Layout Components

Components that define the application structure and navigation.

**Location**: `src/components/layout/`

**Examples**:
- AppLayout (main application wrapper)
- Header / TopBar
- Sidebar / Navigation
- Footer
- Breadcrumbs
- Page containers

**Characteristics**:
- Define the visual structure
- Handle navigation state
- Responsive design considerations
- May use React Router for navigation
- Include common UI patterns (drawer, app bar, etc.)

### 3. Feature Components

Domain-specific components tied to business features.

**Location**: `src/components/features/`

**Examples**:
- ConnectionList
- MetricChart
- QueryEditor
- LogViewer
- UserManagement
- TokenManager

**Characteristics**:
- Business logic encapsulation
- May fetch data via custom hooks
- Feature-specific validation
- Composed of common components
- Moderate reusability within feature domain

### 4. Page Components

Top-level route components that represent entire pages.

**Location**: `src/pages/`

**Examples**:
- LoginPage
- DashboardPage
- ConnectionsPage
- MonitoringPage
- SettingsPage

**Characteristics**:
- One component per route
- Orchestrate feature components
- Handle page-level state
- Include page title and metadata
- Error boundaries for page isolation

## Component Design Patterns

### Pattern 1: Presentational vs. Container

**Presentational Components** (Dumb Components)
- Focus on how things look
- Receive data via props
- No state management (except UI state)
- Highly reusable

```typescript
// Example: UserCard.tsx
interface UserCardProps {
    name: string;
    email: string;
    role: string;
    onEdit: () => void;
    onDelete: () => void;
}

export const UserCard: React.FC<UserCardProps> = ({
    name,
    email,
    role,
    onEdit,
    onDelete,
}) => {
    return (
        <Card>
            <CardContent>
                <Typography variant="h6">{name}</Typography>
                <Typography variant="body2" color="text.secondary">
                    {email}
                </Typography>
                <Chip label={role} size="small" />
            </CardContent>
            <CardActions>
                <Button size="small" onClick={onEdit}>
                    Edit
                </Button>
                <Button size="small" color="error" onClick={onDelete}>
                    Delete
                </Button>
            </CardActions>
        </Card>
    );
};
```

**Container Components** (Smart Components)
- Focus on how things work
- Provide data to presentational components
- Handle state and side effects
- Orchestrate business logic

```typescript
// Example: UserListContainer.tsx
export const UserListContainer: React.FC = () => {
    const { data: users, isLoading, error } = useUsers();
    const [selectedUser, setSelectedUser] = useState<User | null>(null);
    const deleteUser = useDeleteUser();

    const handleEdit = (user: User) => {
        setSelectedUser(user);
    };

    const handleDelete = async (userId: string) => {
        await deleteUser.mutateAsync(userId);
    };

    if (isLoading) return <LoadingSpinner />;
    if (error) return <ErrorAlert message={error.message} />;

    return (
        <>
            <Grid container spacing={2}>
                {users?.map((user) => (
                    <Grid item xs={12} sm={6} md={4} key={user.id}>
                        <UserCard
                            name={user.name}
                            email={user.email}
                            role={user.role}
                            onEdit={() => handleEdit(user)}
                            onDelete={() => handleDelete(user.id)}
                        />
                    </Grid>
                ))}
            </Grid>
            {selectedUser && (
                <EditUserDialog
                    user={selectedUser}
                    onClose={() => setSelectedUser(null)}
                />
            )}
        </>
    );
};
```

### Pattern 2: Compound Components

For complex components with multiple related parts.

```typescript
// Example: ConnectionForm compound component
interface ConnectionFormContextValue {
    values: ConnectionFormValues;
    errors: Record<string, string>;
    updateField: (field: string, value: any) => void;
}

const ConnectionFormContext = createContext<ConnectionFormContextValue | null>(
    null
);

export const ConnectionForm: React.FC<{ onSubmit: (values: any) => void }> & {
    Host: typeof HostField;
    Port: typeof PortField;
    Database: typeof DatabaseField;
    Actions: typeof ActionsField;
} = ({ children, onSubmit }) => {
    const [values, setValues] = useState<ConnectionFormValues>({});
    const [errors, setErrors] = useState<Record<string, string>>({});

    const updateField = (field: string, value: any) => {
        setValues((prev) => ({ ...prev, [field]: value }));
    };

    return (
        <ConnectionFormContext.Provider value={{ values, errors, updateField }}>
            <form onSubmit={onSubmit}>{children}</form>
        </ConnectionFormContext.Provider>
    );
};

// Sub-components
ConnectionForm.Host = () => {
    const { values, errors, updateField } = useContext(ConnectionFormContext)!;
    return (
        <TextField
            label="Host"
            value={values.host || ''}
            onChange={(e) => updateField('host', e.target.value)}
            error={!!errors.host}
            helperText={errors.host}
        />
    );
};

// Usage:
<ConnectionForm onSubmit={handleSubmit}>
    <ConnectionForm.Host />
    <ConnectionForm.Port />
    <ConnectionForm.Database />
    <ConnectionForm.Actions />
</ConnectionForm>
```

### Pattern 3: Render Props

For flexible component composition.

```typescript
// Example: DataFetcher with render prop
interface DataFetcherProps<T> {
    url: string;
    children: (data: T | null, loading: boolean, error: Error | null) => React.ReactNode;
}

export function DataFetcher<T>({ url, children }: DataFetcherProps<T>) {
    const [data, setData] = useState<T | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<Error | null>(null);

    useEffect(() => {
        fetchData(url)
            .then(setData)
            .catch(setError)
            .finally(() => setLoading(false));
    }, [url]);

    return <>{children(data, loading, error)}</>;
}

// Usage:
<DataFetcher<User[]> url="/api/users">
    {(users, loading, error) => {
        if (loading) return <LoadingSpinner />;
        if (error) return <ErrorAlert message={error.message} />;
        return <UserList users={users!} />;
    }}
</DataFetcher>
```

### Pattern 4: Higher-Order Components (HOCs)

For cross-cutting concerns like authentication or authorization.

```typescript
// Example: withAuth HOC
export function withAuth<P extends object>(
    Component: React.ComponentType<P>
): React.FC<P> {
    return (props: P) => {
        const { user, isAuthenticated } = useAuth();
        const navigate = useNavigate();

        useEffect(() => {
            if (!isAuthenticated) {
                navigate('/login');
            }
        }, [isAuthenticated, navigate]);

        if (!isAuthenticated) {
            return <LoadingSpinner />;
        }

        return <Component {...props} />;
    };
}

// Usage:
export const DashboardPage = withAuth(() => {
    return <div>Dashboard content</div>;
});
```

## Component File Structure

Each component should follow this structure:

```typescript
/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

// 1. Imports - external libraries first
import React, { useState, useEffect } from 'react';
import { Button, Card, CardContent } from '@mui/material';

// 2. Imports - internal dependencies
import { useConnections } from '../../hooks/useConnections';
import { ConnectionItem } from './ConnectionItem';
import type { Connection } from '../../types/models';

// 3. Type definitions
interface ConnectionListProps {
    filter?: string;
    onSelect?: (connection: Connection) => void;
}

// 4. Component definition
export const ConnectionList: React.FC<ConnectionListProps> = ({
    filter,
    onSelect,
}) => {
    // 4a. Hooks (in order: context, state, effects, callbacks)
    const { data: connections, isLoading } = useConnections();
    const [selectedId, setSelectedId] = useState<string | null>(null);

    useEffect(() => {
        // Effect logic
    }, []);

    // 4b. Event handlers
    const handleSelect = (connection: Connection) => {
        setSelectedId(connection.id);
        onSelect?.(connection);
    };

    // 4c. Derived state / computed values
    const filteredConnections = filter
        ? connections?.filter((c) => c.name.includes(filter))
        : connections;

    // 4d. Early returns (loading, error states)
    if (isLoading) {
        return <LoadingSpinner />;
    }

    // 4e. Main render
    return (
        <Card>
            <CardContent>
                {filteredConnections?.map((connection) => (
                    <ConnectionItem
                        key={connection.id}
                        connection={connection}
                        selected={connection.id === selectedId}
                        onSelect={handleSelect}
                    />
                ))}
            </CardContent>
        </Card>
    );
};

// 5. Default props (if needed)
ConnectionList.defaultProps = {
    filter: '',
};
```

## Component Naming Conventions

### File Names
- Use PascalCase for component files: `UserCard.tsx`, `LoginForm.tsx`
- Match file name to component name
- One component per file (except for tightly coupled sub-components)
- Use `.tsx` extension for components, `.ts` for utilities

### Component Names
- PascalCase: `UserCard`, `ConnectionList`, `MetricChart`
- Descriptive and specific: `EditUserDialog` not `Dialog`
- Suffix with component type when helpful: `LoginForm`, `UserCard`, `MetricTable`

### Props Interface Names
- Component name + "Props": `UserCardProps`, `ConnectionListProps`
- Export the interface for documentation and reuse

### Event Handler Names
- Prefix with "handle": `handleClick`, `handleSubmit`, `handleChange`
- Props for callbacks: prefix with "on": `onClick`, `onSubmit`, `onChange`

## Component Props Best Practices

### 1. Define Explicit Interfaces

```typescript
// Good
interface ButtonProps {
    variant: 'primary' | 'secondary' | 'danger';
    size: 'small' | 'medium' | 'large';
    disabled?: boolean;
    onClick: () => void;
    children: React.ReactNode;
}

// Avoid
interface ButtonProps {
    [key: string]: any;
}
```

### 2. Use Optional Props Judiciously

```typescript
interface UserCardProps {
    // Required props
    name: string;
    email: string;

    // Optional props with reasonable defaults
    role?: string;
    avatarUrl?: string;

    // Optional callbacks
    onEdit?: () => void;
    onDelete?: () => void;
}
```

### 3. Avoid Props Drilling

When props are passed through multiple levels, consider:
- Using React Context for global state
- Creating intermediate container components
- Using composition instead of props

```typescript
// Instead of:
<Parent>
    <Child userId={userId}>
        <GrandChild userId={userId}>
            <GreatGrandChild userId={userId} />
        </GrandChild>
    </Child>
</Parent>

// Use Context:
<UserProvider userId={userId}>
    <Parent>
        <Child>
            <GrandChild>
                <GreatGrandChild />
            </GrandChild>
        </Child>
    </Parent>
</UserProvider>
```

## Component Composition

Build complex UIs through composition:

```typescript
// Bad - monolithic component
export const Dashboard = () => {
    return (
        <div>
            <div className="header">...</div>
            <div className="sidebar">...</div>
            <div className="content">
                <div className="metrics">...</div>
                <div className="charts">...</div>
                <div className="logs">...</div>
            </div>
        </div>
    );
};

// Good - composed components
export const Dashboard = () => {
    return (
        <DashboardLayout>
            <DashboardHeader />
            <DashboardSidebar />
            <DashboardContent>
                <MetricsPanel />
                <ChartsPanel />
                <LogsPanel />
            </DashboardContent>
        </DashboardLayout>
    );
};
```

## Error Boundaries

Wrap feature areas with error boundaries to prevent full app crashes:

```typescript
// ErrorBoundary.tsx
interface ErrorBoundaryProps {
    fallback?: React.ReactNode;
    children: React.ReactNode;
}

interface ErrorBoundaryState {
    hasError: boolean;
    error: Error | null;
}

export class ErrorBoundary extends React.Component<
    ErrorBoundaryProps,
    ErrorBoundaryState
> {
    constructor(props: ErrorBoundaryProps) {
        super(props);
        this.state = { hasError: false, error: null };
    }

    static getDerivedStateFromError(error: Error): ErrorBoundaryState {
        return { hasError: true, error };
    }

    componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
        console.error('Error caught by boundary:', error, errorInfo);
        // Log to error reporting service
    }

    render() {
        if (this.state.hasError) {
            return (
                this.props.fallback || (
                    <Alert severity="error">
                        Something went wrong. Please refresh the page.
                    </Alert>
                )
            );
        }

        return this.props.children;
    }
}

// Usage:
<ErrorBoundary fallback={<ErrorFallback />}>
    <MonitoringPage />
</ErrorBoundary>
```

## Accessibility Guidelines

### 1. Semantic HTML

```typescript
// Good
<nav>
    <ul>
        <li><a href="/dashboard">Dashboard</a></li>
        <li><a href="/connections">Connections</a></li>
    </ul>
</nav>

// Avoid
<div className="nav">
    <div className="nav-item" onClick={...}>Dashboard</div>
    <div className="nav-item" onClick={...}>Connections</div>
</div>
```

### 2. ARIA Attributes

```typescript
<Button
    aria-label="Delete user"
    aria-describedby="delete-user-description"
    onClick={handleDelete}
>
    <DeleteIcon />
</Button>
<Typography id="delete-user-description" className="sr-only">
    This will permanently delete the user account
</Typography>
```

### 3. Keyboard Navigation

```typescript
const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        handleClick();
    }
};

<div
    role="button"
    tabIndex={0}
    onClick={handleClick}
    onKeyDown={handleKeyDown}
>
    Click me
</div>
```

### 4. Focus Management

```typescript
const dialogRef = useRef<HTMLDivElement>(null);

useEffect(() => {
    if (isOpen) {
        dialogRef.current?.focus();
    }
}, [isOpen]);

<Dialog
    open={isOpen}
    onClose={handleClose}
    ref={dialogRef}
    aria-labelledby="dialog-title"
    aria-describedby="dialog-description"
>
    ...
</Dialog>
```

## Performance Optimization

### 1. React.memo for Expensive Components

```typescript
export const ExpensiveComponent = React.memo<ExpensiveComponentProps>(
    ({ data, onUpdate }) => {
        // Expensive rendering logic
        return <div>...</div>;
    },
    (prevProps, nextProps) => {
        // Custom comparison function
        return prevProps.data.id === nextProps.data.id;
    }
);
```

### 2. useMemo for Expensive Calculations

```typescript
const sortedAndFilteredData = useMemo(() => {
    return data
        .filter((item) => item.status === 'active')
        .sort((a, b) => a.name.localeCompare(b.name));
}, [data]);
```

### 3. useCallback for Event Handlers

```typescript
const handleDelete = useCallback(
    (id: string) => {
        deleteItem(id);
    },
    [deleteItem]
);
```

### 4. Code Splitting

```typescript
// Lazy load heavy components
const MonitoringPage = lazy(() => import('./pages/MonitoringPage'));
const SettingsPage = lazy(() => import('./pages/SettingsPage'));

// Usage in router
<Suspense fallback={<LoadingSpinner />}>
    <Routes>
        <Route path="/monitoring" element={<MonitoringPage />} />
        <Route path="/settings" element={<SettingsPage />} />
    </Routes>
</Suspense>
```

## Testing Components

### 1. Test User Behavior, Not Implementation

```typescript
// Good - tests user interaction
test('submits form when button is clicked', async () => {
    render(<LoginForm />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/username/i), 'testuser');
    await user.type(screen.getByLabelText(/password/i), 'password123');
    await user.click(screen.getByRole('button', { name: /login/i }));

    expect(screen.getByText(/welcome/i)).toBeInTheDocument();
});

// Avoid - tests implementation details
test('updates state when input changes', () => {
    const { result } = renderHook(() => useState(''));
    // Testing internal state instead of user-facing behavior
});
```

### 2. Mock External Dependencies

```typescript
// Mock API calls
import { server } from '../mocks/server';
import { rest } from 'msw';

test('displays error when API fails', async () => {
    server.use(
        rest.get('/api/users', (req, res, ctx) => {
            return res(ctx.status(500), ctx.json({ error: 'Server error' }));
        })
    );

    render(<UserList />);

    expect(await screen.findByText(/error loading users/i)).toBeInTheDocument();
});
```

### 3. Test Accessibility

```typescript
import { axe, toHaveNoViolations } from 'jest-axe';

expect.extend(toHaveNoViolations);

test('should not have accessibility violations', async () => {
    const { container } = render(<UserCard name="John" email="john@example.com" />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
});
```

## Documentation

Document components with JSDoc comments:

```typescript
/**
 * Displays a list of database connections with filtering and selection.
 *
 * @component
 * @example
 * ```tsx
 * <ConnectionList
 *   filter="production"
 *   onSelect={(conn) => console.log(conn.id)}
 * />
 * ```
 */
export const ConnectionList: React.FC<ConnectionListProps> = ({
    filter,
    onSelect,
}) => {
    // ...
};
```

## Summary Checklist

When creating a new component, ensure:

- [ ] Component has a single, clear responsibility
- [ ] Props interface is explicitly defined with TypeScript
- [ ] Accessibility attributes are included (ARIA, keyboard support)
- [ ] Error states are handled gracefully
- [ ] Loading states provide user feedback
- [ ] Component is properly memoized if expensive
- [ ] Event handlers use useCallback when appropriate
- [ ] Component is tested with React Testing Library
- [ ] File structure follows conventions
- [ ] JSDoc documentation is provided
- [ ] Security considerations are addressed (input sanitization)

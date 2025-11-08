/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Frontend Testing Strategy

This document outlines comprehensive testing approaches for the pgEdge AI
Workbench React frontend, covering unit tests, integration tests, and
end-to-end tests.

## Testing Stack

### Core Testing Tools

- **Vitest** - Fast unit test runner (compatible with Vite)
- **React Testing Library** - Component testing with user-centric approach
- **Mock Service Worker (MSW)** - API mocking for tests
- **Testing Library User Event** - Realistic user interaction simulation
- **Playwright** - End-to-end browser testing
- **@testing-library/jest-dom** - Custom matchers for DOM assertions

### Installation

```bash
npm install -D vitest @vitest/ui
npm install -D @testing-library/react @testing-library/jest-dom
npm install -D @testing-library/user-event
npm install -D msw
npm install -D @playwright/test
npm install -D @axe-core/playwright  # Accessibility testing
```

## Test Configuration

### Vitest Configuration

```typescript
// vitest.config.ts
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
    plugins: [react()],
    test: {
        globals: true,
        environment: 'jsdom',
        setupFiles: ['./tests/setup.ts'],
        coverage: {
            provider: 'v8',
            reporter: ['text', 'json', 'html', 'lcov'],
            exclude: [
                'node_modules/',
                'tests/',
                '**/*.d.ts',
                '**/*.config.*',
                '**/mockData',
            ],
            lines: 80,
            functions: 80,
            branches: 80,
            statements: 80,
        },
    },
    resolve: {
        alias: {
            '@': path.resolve(__dirname, './src'),
        },
    },
});
```

### Test Setup File

```typescript
// tests/setup.ts
import '@testing-library/jest-dom';
import { cleanup } from '@testing-library/react';
import { afterEach, vi } from 'vitest';
import { setupServer } from 'msw/node';
import { handlers } from './mocks/handlers';

// Setup MSW server
export const server = setupServer(...handlers);

// Start server before all tests
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));

// Reset handlers after each test
afterEach(() => {
    cleanup();
    server.resetHandlers();
});

// Close server after all tests
afterAll(() => server.close());

// Mock environment variables
vi.stubEnv('VITE_API_URL', 'http://localhost:8080');
vi.stubEnv('VITE_API_TIMEOUT', '30000');

// Mock window.matchMedia (for responsive tests)
Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
    })),
});

// Mock IntersectionObserver
global.IntersectionObserver = class IntersectionObserver {
    constructor() {}
    disconnect() {}
    observe() {}
    takeRecords() {
        return [];
    }
    unobserve() {}
} as any;
```

### MSW Mock Handlers

```typescript
// tests/mocks/handlers.ts
import { http, HttpResponse } from 'msw';
import { mockConnections, mockUsers, mockMetrics } from './mockData';

export const handlers = [
    // Auth endpoints
    http.post('/api/auth/login', async ({ request }) => {
        const body = await request.json();
        const { username, password } = body as any;

        if (username === 'testuser' && password === 'password123') {
            return HttpResponse.json({
                user: {
                    id: '1',
                    username: 'testuser',
                    email: 'test@example.com',
                    fullName: 'Test User',
                },
                token: {
                    value: 'mock-token-123',
                    expiresAt: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
                },
            });
        }

        return HttpResponse.json(
            { message: 'Invalid credentials' },
            { status: 401 }
        );
    }),

    http.post('/api/auth/logout', () => {
        return HttpResponse.json({ success: true });
    }),

    http.get('/api/auth/verify', () => {
        return HttpResponse.json({
            token: {
                value: 'mock-token-123',
                expiresAt: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
            },
        });
    }),

    // Connection endpoints
    http.get('/api/connections', () => {
        return HttpResponse.json(mockConnections);
    }),

    http.get('/api/connections/:id', ({ params }) => {
        const connection = mockConnections.find((c) => c.id === params.id);
        if (!connection) {
            return HttpResponse.json(
                { message: 'Connection not found' },
                { status: 404 }
            );
        }
        return HttpResponse.json(connection);
    }),

    http.post('/api/connections', async ({ request }) => {
        const body = await request.json();
        const newConnection = {
            id: Math.random().toString(36).substring(7),
            ...(body as any),
            isActive: true,
            createdAt: new Date().toISOString(),
        };
        return HttpResponse.json(newConnection, { status: 201 });
    }),

    http.patch('/api/connections/:id', async ({ params, request }) => {
        const body = await request.json();
        const connection = mockConnections.find((c) => c.id === params.id);
        if (!connection) {
            return HttpResponse.json(
                { message: 'Connection not found' },
                { status: 404 }
            );
        }
        const updated = { ...connection, ...(body as any) };
        return HttpResponse.json(updated);
    }),

    http.delete('/api/connections/:id', ({ params }) => {
        const connection = mockConnections.find((c) => c.id === params.id);
        if (!connection) {
            return HttpResponse.json(
                { message: 'Connection not found' },
                { status: 404 }
            );
        }
        return new HttpResponse(null, { status: 204 });
    }),

    // Metrics endpoints
    http.get('/api/connections/:id/metrics', ({ request }) => {
        const url = new URL(request.url);
        const timeRange = url.searchParams.get('timeRange');
        return HttpResponse.json(mockMetrics);
    }),
];
```

### Mock Data

```typescript
// tests/mocks/mockData.ts
import type { Connection, User, Metric } from '../../src/types/models';

export const mockUsers: User[] = [
    {
        id: '1',
        username: 'testuser',
        email: 'test@example.com',
        fullName: 'Test User',
        isSuperuser: false,
        createdAt: '2024-01-01T00:00:00Z',
    },
    {
        id: '2',
        username: 'admin',
        email: 'admin@example.com',
        fullName: 'Admin User',
        isSuperuser: true,
        createdAt: '2024-01-01T00:00:00Z',
    },
];

export const mockConnections: Connection[] = [
    {
        id: 'conn-1',
        name: 'Production DB',
        host: 'prod.example.com',
        port: 5432,
        database: 'production',
        username: 'postgres',
        type: 'production',
        isActive: true,
        isMonitored: true,
        createdAt: '2024-01-01T00:00:00Z',
    },
    {
        id: 'conn-2',
        name: 'Development DB',
        host: 'dev.example.com',
        port: 5432,
        database: 'development',
        username: 'postgres',
        type: 'development',
        isActive: true,
        isMonitored: false,
        createdAt: '2024-01-01T00:00:00Z',
    },
];

export const mockMetrics: Metric[] = [
    {
        id: 'metric-1',
        connectionId: 'conn-1',
        timestamp: '2024-01-01T12:00:00Z',
        cpu: 45.2,
        memory: 62.8,
        connections: 12,
        transactions: 1234,
    },
    {
        id: 'metric-2',
        connectionId: 'conn-1',
        timestamp: '2024-01-01T12:05:00Z',
        cpu: 48.1,
        memory: 63.5,
        connections: 13,
        transactions: 1289,
    },
];
```

## Unit Testing Components

### Testing Presentational Components

```typescript
// tests/components/common/UserCard.test.tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { UserCard } from '@/components/common/UserCard';

describe('UserCard', () => {
    const mockUser = {
        id: '1',
        name: 'John Doe',
        email: 'john@example.com',
        role: 'admin',
    };

    it('renders user information correctly', () => {
        render(
            <UserCard
                name={mockUser.name}
                email={mockUser.email}
                role={mockUser.role}
                onEdit={vi.fn()}
                onDelete={vi.fn()}
            />
        );

        expect(screen.getByText('John Doe')).toBeInTheDocument();
        expect(screen.getByText('john@example.com')).toBeInTheDocument();
        expect(screen.getByText('admin')).toBeInTheDocument();
    });

    it('calls onEdit when edit button is clicked', async () => {
        const handleEdit = vi.fn();
        const user = userEvent.setup();

        render(
            <UserCard
                name={mockUser.name}
                email={mockUser.email}
                role={mockUser.role}
                onEdit={handleEdit}
                onDelete={vi.fn()}
            />
        );

        await user.click(screen.getByRole('button', { name: /edit/i }));

        expect(handleEdit).toHaveBeenCalledTimes(1);
    });

    it('calls onDelete when delete button is clicked', async () => {
        const handleDelete = vi.fn();
        const user = userEvent.setup();

        render(
            <UserCard
                name={mockUser.name}
                email={mockUser.email}
                role={mockUser.role}
                onEdit={vi.fn()}
                onDelete={handleDelete}
            />
        );

        await user.click(screen.getByRole('button', { name: /delete/i }));

        expect(handleDelete).toHaveBeenCalledTimes(1);
    });
});
```

### Testing Container Components

```typescript
// tests/components/features/ConnectionList.test.tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ConnectionList } from '@/components/features/ConnectionList';
import { mockConnections } from '../../mocks/mockData';

const createWrapper = () => {
    const queryClient = new QueryClient({
        defaultOptions: {
            queries: { retry: false },
        },
    });

    return ({ children }: { children: React.ReactNode }) => (
        <QueryClientProvider client={queryClient}>
            {children}
        </QueryClientProvider>
    );
};

describe('ConnectionList', () => {
    it('displays loading state initially', () => {
        render(<ConnectionList />, { wrapper: createWrapper() });

        expect(screen.getByText(/loading/i)).toBeInTheDocument();
    });

    it('displays connections after loading', async () => {
        render(<ConnectionList />, { wrapper: createWrapper() });

        await waitFor(() => {
            expect(screen.getByText('Production DB')).toBeInTheDocument();
            expect(screen.getByText('Development DB')).toBeInTheDocument();
        });
    });

    it('displays error message when API fails', async () => {
        // Override MSW handler to return error
        server.use(
            http.get('/api/connections', () => {
                return HttpResponse.json(
                    { message: 'Server error' },
                    { status: 500 }
                );
            })
        );

        render(<ConnectionList />, { wrapper: createWrapper() });

        await waitFor(() => {
            expect(screen.getByText(/error/i)).toBeInTheDocument();
        });
    });

    it('filters connections based on search query', async () => {
        const user = userEvent.setup();

        render(<ConnectionList />, { wrapper: createWrapper() });

        await waitFor(() => {
            expect(screen.getByText('Production DB')).toBeInTheDocument();
        });

        const searchInput = screen.getByRole('textbox', { name: /search/i });
        await user.type(searchInput, 'Production');

        expect(screen.getByText('Production DB')).toBeInTheDocument();
        expect(screen.queryByText('Development DB')).not.toBeInTheDocument();
    });
});
```

### Testing Forms

```typescript
// tests/components/features/ConnectionForm.test.tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConnectionForm } from '@/components/features/ConnectionForm';

describe('ConnectionForm', () => {
    const defaultProps = {
        open: true,
        onClose: vi.fn(),
        onSubmit: vi.fn(),
    };

    it('renders all form fields', () => {
        render(<ConnectionForm {...defaultProps} />);

        expect(screen.getByLabelText(/connection name/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/host/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/port/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/database/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    });

    it('validates required fields', async () => {
        const user = userEvent.setup();
        render(<ConnectionForm {...defaultProps} />);

        const submitButton = screen.getByRole('button', { name: /create/i });
        await user.click(submitButton);

        await waitFor(() => {
            expect(screen.getByText(/name is required/i)).toBeInTheDocument();
            expect(screen.getByText(/host is required/i)).toBeInTheDocument();
            expect(screen.getByText(/database is required/i)).toBeInTheDocument();
        });

        expect(defaultProps.onSubmit).not.toHaveBeenCalled();
    });

    it('validates port number range', async () => {
        const user = userEvent.setup();
        render(<ConnectionForm {...defaultProps} />);

        const portInput = screen.getByLabelText(/port/i);
        await user.clear(portInput);
        await user.type(portInput, '99999');

        const submitButton = screen.getByRole('button', { name: /create/i });
        await user.click(submitButton);

        await waitFor(() => {
            expect(
                screen.getByText(/port must be between 1 and 65535/i)
            ).toBeInTheDocument();
        });
    });

    it('submits form with valid data', async () => {
        const user = userEvent.setup();
        const handleSubmit = vi.fn().mockResolvedValue(undefined);

        render(<ConnectionForm {...defaultProps} onSubmit={handleSubmit} />);

        // Fill out form
        await user.type(screen.getByLabelText(/connection name/i), 'Test DB');
        await user.type(screen.getByLabelText(/host/i), 'localhost');
        await user.type(screen.getByLabelText(/database/i), 'testdb');
        await user.type(screen.getByLabelText(/username/i), 'postgres');
        await user.type(screen.getByLabelText(/password/i), 'password123');

        await user.click(screen.getByRole('button', { name: /create/i }));

        await waitFor(() => {
            expect(handleSubmit).toHaveBeenCalledWith({
                name: 'Test DB',
                host: 'localhost',
                port: 5432,
                database: 'testdb',
                username: 'postgres',
                password: 'password123',
                sslMode: 'prefer',
            });
        });

        expect(defaultProps.onClose).toHaveBeenCalled();
    });

    it('displays error message when submission fails', async () => {
        const user = userEvent.setup();
        const handleSubmit = vi
            .fn()
            .mockRejectedValue(new Error('Connection failed'));

        render(<ConnectionForm {...defaultProps} onSubmit={handleSubmit} />);

        // Fill out form
        await user.type(screen.getByLabelText(/connection name/i), 'Test DB');
        await user.type(screen.getByLabelText(/host/i), 'localhost');
        await user.type(screen.getByLabelText(/database/i), 'testdb');
        await user.type(screen.getByLabelText(/username/i), 'postgres');

        await user.click(screen.getByRole('button', { name: /create/i }));

        await waitFor(() => {
            expect(screen.getByText(/connection failed/i)).toBeInTheDocument();
        });
    });
});
```

## Testing Custom Hooks

```typescript
// tests/hooks/useAuth.test.tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useAuth } from '@/hooks/useAuth';
import { AuthProvider } from '@/contexts/AuthContext';

const createWrapper = () => {
    const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false } },
    });

    return ({ children }: { children: React.ReactNode }) => (
        <QueryClientProvider client={queryClient}>
            <AuthProvider>{children}</AuthProvider>
        </QueryClientProvider>
    );
};

describe('useAuth', () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it('returns initial unauthenticated state', () => {
        const { result } = renderHook(() => useAuth(), {
            wrapper: createWrapper(),
        });

        expect(result.current.isAuthenticated).toBe(false);
        expect(result.current.user).toBeNull();
    });

    it('logs in user successfully', async () => {
        const { result } = renderHook(() => useAuth(), {
            wrapper: createWrapper(),
        });

        await waitFor(() => {
            result.current.login('testuser', 'password123');
        });

        await waitFor(() => {
            expect(result.current.isAuthenticated).toBe(true);
            expect(result.current.user?.username).toBe('testuser');
        });
    });

    it('handles login failure', async () => {
        const { result } = renderHook(() => useAuth(), {
            wrapper: createWrapper(),
        });

        await expect(
            result.current.login('invalid', 'wrong')
        ).rejects.toThrow();

        expect(result.current.isAuthenticated).toBe(false);
    });

    it('logs out user', async () => {
        const { result } = renderHook(() => useAuth(), {
            wrapper: createWrapper(),
        });

        // Login first
        await waitFor(() => {
            result.current.login('testuser', 'password123');
        });

        await waitFor(() => {
            expect(result.current.isAuthenticated).toBe(true);
        });

        // Logout
        await waitFor(() => {
            result.current.logout();
        });

        await waitFor(() => {
            expect(result.current.isAuthenticated).toBe(false);
            expect(result.current.user).toBeNull();
        });
    });
});
```

## Testing Contexts

```typescript
// tests/contexts/AuthContext.test.tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AuthProvider, useAuth } from '@/contexts/AuthContext';

// Test component that uses auth context
const TestComponent = () => {
    const { user, isAuthenticated, login, logout } = useAuth();

    return (
        <div>
            <div>Status: {isAuthenticated ? 'Authenticated' : 'Not authenticated'}</div>
            {user && <div>User: {user.username}</div>}
            <button onClick={() => login('testuser', 'password123')}>
                Login
            </button>
            <button onClick={logout}>Logout</button>
        </div>
    );
};

describe('AuthContext', () => {
    it('provides initial unauthenticated state', () => {
        render(
            <AuthProvider>
                <TestComponent />
            </AuthProvider>
        );

        expect(screen.getByText('Status: Not authenticated')).toBeInTheDocument();
    });

    it('updates state after login', async () => {
        const user = userEvent.setup();

        render(
            <AuthProvider>
                <TestComponent />
            </AuthProvider>
        );

        await user.click(screen.getByRole('button', { name: /login/i }));

        await waitFor(() => {
            expect(screen.getByText('Status: Authenticated')).toBeInTheDocument();
            expect(screen.getByText('User: testuser')).toBeInTheDocument();
        });
    });

    it('updates state after logout', async () => {
        const user = userEvent.setup();

        render(
            <AuthProvider>
                <TestComponent />
            </AuthProvider>
        );

        // Login
        await user.click(screen.getByRole('button', { name: /login/i }));
        await waitFor(() => {
            expect(screen.getByText('Status: Authenticated')).toBeInTheDocument();
        });

        // Logout
        await user.click(screen.getByRole('button', { name: /logout/i }));
        await waitFor(() => {
            expect(screen.getByText('Status: Not authenticated')).toBeInTheDocument();
        });
    });
});
```

## Accessibility Testing

```typescript
// tests/accessibility/ConnectionForm.a11y.test.tsx
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { ConnectionForm } from '@/components/features/ConnectionForm';

expect.extend(toHaveNoViolations);

describe('ConnectionForm Accessibility', () => {
    it('should not have accessibility violations', async () => {
        const { container } = render(
            <ConnectionForm
                open={true}
                onClose={() => {}}
                onSubmit={async () => {}}
            />
        );

        const results = await axe(container);
        expect(results).toHaveNoViolations();
    });

    it('has proper ARIA labels', () => {
        render(
            <ConnectionForm
                open={true}
                onClose={() => {}}
                onSubmit={async () => {}}
            />
        );

        const nameInput = screen.getByLabelText(/connection name/i);
        expect(nameInput).toHaveAttribute('aria-required', 'true');

        const submitButton = screen.getByRole('button', { name: /create/i });
        expect(submitButton).toBeInTheDocument();
    });

    it('supports keyboard navigation', async () => {
        const user = userEvent.setup();

        render(
            <ConnectionForm
                open={true}
                onClose={() => {}}
                onSubmit={async () => {}}
            />
        );

        // Tab through form fields
        await user.tab();
        expect(screen.getByLabelText(/connection name/i)).toHaveFocus();

        await user.tab();
        expect(screen.getByLabelText(/host/i)).toHaveFocus();

        await user.tab();
        expect(screen.getByLabelText(/port/i)).toHaveFocus();
    });
});
```

## Snapshot Testing

```typescript
// tests/components/common/UserCard.snapshot.test.tsx
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { UserCard } from '@/components/common/UserCard';

describe('UserCard Snapshots', () => {
    it('matches snapshot', () => {
        const { container } = render(
            <UserCard
                name="John Doe"
                email="john@example.com"
                role="admin"
                onEdit={() => {}}
                onDelete={() => {}}
            />
        );

        expect(container).toMatchSnapshot();
    });
});
```

## End-to-End Testing with Playwright

### Playwright Configuration

```typescript
// playwright.config.ts
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
    testDir: './tests/e2e',
    fullyParallel: true,
    forbidOnly: !!process.env.CI,
    retries: process.env.CI ? 2 : 0,
    workers: process.env.CI ? 1 : undefined,
    reporter: 'html',
    use: {
        baseURL: 'http://localhost:5173',
        trace: 'on-first-retry',
        screenshot: 'only-on-failure',
    },
    projects: [
        {
            name: 'chromium',
            use: { ...devices['Desktop Chrome'] },
        },
        {
            name: 'firefox',
            use: { ...devices['Desktop Firefox'] },
        },
        {
            name: 'webkit',
            use: { ...devices['Desktop Safari'] },
        },
        {
            name: 'Mobile Chrome',
            use: { ...devices['Pixel 5'] },
        },
    ],
    webServer: {
        command: 'npm run dev',
        url: 'http://localhost:5173',
        reuseExistingServer: !process.env.CI,
    },
});
```

### E2E Test Example

```typescript
// tests/e2e/login.spec.ts
import { test, expect } from '@playwright/test';

test.describe('Login Flow', () => {
    test('should login successfully', async ({ page }) => {
        await page.goto('/login');

        // Fill login form
        await page.fill('input[name="username"]', 'testuser');
        await page.fill('input[name="password"]', 'password123');

        // Submit form
        await page.click('button[type="submit"]');

        // Wait for navigation to dashboard
        await page.waitForURL('/dashboard');

        // Verify user is logged in
        await expect(page.locator('text=Welcome, testuser')).toBeVisible();
    });

    test('should show error for invalid credentials', async ({ page }) => {
        await page.goto('/login');

        await page.fill('input[name="username"]', 'invalid');
        await page.fill('input[name="password"]', 'wrong');
        await page.click('button[type="submit"]');

        // Verify error message
        await expect(page.locator('text=Invalid credentials')).toBeVisible();

        // Should still be on login page
        expect(page.url()).toContain('/login');
    });
});

// tests/e2e/connections.spec.ts
test.describe('Connection Management', () => {
    test.beforeEach(async ({ page }) => {
        // Login before each test
        await page.goto('/login');
        await page.fill('input[name="username"]', 'testuser');
        await page.fill('input[name="password"]', 'password123');
        await page.click('button[type="submit"]');
        await page.waitForURL('/dashboard');
    });

    test('should create a new connection', async ({ page }) => {
        await page.goto('/connections');

        // Click create button
        await page.click('button:has-text("Create Connection")');

        // Fill form
        await page.fill('input[name="name"]', 'Test Connection');
        await page.fill('input[name="host"]', 'localhost');
        await page.fill('input[name="database"]', 'testdb');
        await page.fill('input[name="username"]', 'postgres');

        // Submit
        await page.click('button:has-text("Create")');

        // Verify success
        await expect(page.locator('text=Test Connection')).toBeVisible();
    });

    test('should delete a connection', async ({ page }) => {
        await page.goto('/connections');

        // Find connection and click more options
        await page.click('[data-testid="connection-menu-1"]');

        // Click delete
        await page.click('text=Delete');

        // Confirm deletion
        await page.click('button:has-text("Confirm")');

        // Verify connection is removed
        await expect(page.locator('[data-testid="connection-1"]')).not.toBeVisible();
    });
});
```

## Test Coverage

### Running Tests

```json
// package.json scripts
{
    "scripts": {
        "test": "vitest",
        "test:ui": "vitest --ui",
        "test:coverage": "vitest --coverage",
        "test:e2e": "playwright test",
        "test:e2e:ui": "playwright test --ui",
        "test:all": "npm run test:coverage && npm run test:e2e"
    }
}
```

### Coverage Thresholds

Configure minimum coverage thresholds in `vitest.config.ts`:

```typescript
coverage: {
    lines: 80,
    functions: 80,
    branches: 80,
    statements: 80,
    include: ['src/**/*.{ts,tsx}'],
    exclude: [
        'src/**/*.d.ts',
        'src/**/*.stories.tsx',
        'src/main.tsx',
    ],
}
```

## Best Practices Summary

1. **Test user behavior, not implementation** - Focus on what users see and do
2. **Use React Testing Library queries** - getByRole, getByLabelText, etc.
3. **Mock external dependencies** - Use MSW for API mocking
4. **Test accessibility** - Use axe-core and keyboard navigation tests
5. **Aim for high coverage** - Target 80%+ code coverage
6. **Write descriptive test names** - Clearly state what is being tested
7. **Keep tests isolated** - Each test should be independent
8. **Test error states** - Don't just test the happy path
9. **Use snapshots sparingly** - Only for truly static UI
10. **Run tests in CI/CD** - Automate test execution on every commit

## Testing Checklist

For each component, ensure:

- [ ] Renders correctly with valid props
- [ ] Handles all user interactions (clicks, typing, etc.)
- [ ] Validates form inputs properly
- [ ] Displays loading states
- [ ] Displays error states
- [ ] Handles API errors gracefully
- [ ] Is accessible (ARIA, keyboard navigation)
- [ ] No accessibility violations (axe)
- [ ] Responsive across breakpoints (if applicable)
- [ ] Snapshot test (if UI is stable)

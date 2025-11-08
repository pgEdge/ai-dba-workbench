/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# React Frontend Architecture Overview

This document provides a comprehensive overview of the recommended architecture
for the pgEdge AI Workbench React frontend client application.

## Technology Stack

### Core Technologies

- **React 18+** - Modern React with concurrent features and automatic batching
- **TypeScript 5+** - Type safety and enhanced developer experience
- **Material-UI (MUI) v5+** - Component library for professional UI
- **React Router v6+** - Client-side routing and navigation
- **Vite** - Fast build tool and development server (recommended over CRA)

### State Management

- **React Context + useReducer** - For global application state
- **React Query (TanStack Query)** - For server state management and caching
- **Local component state (useState)** - For UI-only state

### API Communication

- **Fetch API with custom wrapper** - For HTTP requests to MCP server
- **EventSource API** - For Server-Sent Events (SSE) from MCP server
- **Axios (alternative)** - If more advanced HTTP features are needed

### Testing

- **Vitest** - Fast unit test runner (integrates well with Vite)
- **React Testing Library** - Component testing focused on user behavior
- **Mock Service Worker (MSW)** - API mocking for tests
- **Playwright** - End-to-end testing

### Code Quality

- **ESLint** - JavaScript/TypeScript linting
- **Prettier** - Code formatting
- **TypeScript strict mode** - Enhanced type checking
- **Husky** - Git hooks for pre-commit checks

## Project Structure

```
client/
├── public/                 # Static assets
│   ├── favicon.ico
│   └── index.html
├── src/
│   ├── assets/            # Images, fonts, static files
│   ├── components/        # Reusable UI components
│   │   ├── common/        # Shared components (Button, Input, etc.)
│   │   ├── layout/        # Layout components (Header, Sidebar, etc.)
│   │   └── features/      # Feature-specific components
│   ├── contexts/          # React contexts for global state
│   │   ├── AuthContext.tsx
│   │   ├── ThemeContext.tsx
│   │   └── ConnectionContext.tsx
│   ├── hooks/             # Custom React hooks
│   │   ├── useAuth.ts
│   │   ├── useConnections.ts
│   │   └── useMetrics.ts
│   ├── pages/             # Page-level components (routes)
│   │   ├── Login/
│   │   ├── Dashboard/
│   │   ├── Connections/
│   │   ├── Monitoring/
│   │   └── Settings/
│   ├── services/          # API communication layer
│   │   ├── api.ts         # Base API client
│   │   ├── auth.ts        # Authentication APIs
│   │   ├── connections.ts # Connection management APIs
│   │   └── mcp.ts         # MCP server communication
│   ├── types/             # TypeScript type definitions
│   │   ├── api.ts         # API request/response types
│   │   ├── models.ts      # Domain models
│   │   └── mcp.ts         # MCP protocol types
│   ├── utils/             # Utility functions
│   │   ├── validation.ts  # Input validation
│   │   ├── formatting.ts  # Data formatting
│   │   └── security.ts    # Security utilities
│   ├── styles/            # Global styles and theme
│   │   ├── theme.ts       # MUI theme configuration
│   │   └── global.css     # Global CSS
│   ├── App.tsx            # Root component
│   ├── main.tsx           # Application entry point
│   └── vite-env.d.ts      # Vite type definitions
├── tests/                 # Unit and integration tests
│   ├── components/
│   ├── hooks/
│   ├── services/
│   └── utils/
├── .env.example           # Environment variable template
├── .eslintrc.json         # ESLint configuration
├── .prettierrc            # Prettier configuration
├── index.html             # HTML entry point
├── package.json           # NPM dependencies and scripts
├── tsconfig.json          # TypeScript configuration
├── vite.config.ts         # Vite configuration
└── vitest.config.ts       # Vitest configuration
```

## Architecture Principles

### 1. Separation of Concerns

- **Presentation Components** - Focus solely on rendering UI
- **Container Components** - Handle data fetching and business logic
- **Custom Hooks** - Encapsulate reusable logic
- **Services** - Abstract API communication details

### 2. Single Responsibility

Each component, hook, and module should have one clear purpose. This makes
code easier to test, maintain, and reason about.

### 3. Composition Over Inheritance

Use component composition and higher-order components (HOCs) or custom hooks
rather than class inheritance.

### 4. Type Safety

Leverage TypeScript throughout the application:
- Define interfaces for all data structures
- Use strict null checks
- Avoid `any` type unless absolutely necessary
- Create utility types for common patterns

### 5. Security First

- Sanitize all user inputs before rendering
- Implement proper authentication token management
- Use HTTPS for all production deployments
- Implement CSRF protection
- Follow OWASP security best practices

### 6. Performance Optimization

- Code splitting with React.lazy() and Suspense
- Memoization with React.memo, useMemo, useCallback
- Virtualization for large lists (react-window or react-virtualized)
- Image optimization and lazy loading
- Bundle size monitoring

### 7. Accessibility (WCAG 2.1 AA)

- Semantic HTML elements
- Proper ARIA attributes
- Keyboard navigation support
- Focus management
- Screen reader compatibility
- Color contrast compliance

## Application Flow

### Authentication Flow

1. User lands on login page (unauthenticated)
2. User enters credentials
3. Client sends login request to MCP server
4. Server validates credentials and returns bearer token
5. Client stores token securely (memory + httpOnly cookie pattern)
6. Client redirects to dashboard
7. All subsequent API calls include bearer token in Authorization header
8. Token refresh mechanism before expiry
9. Logout clears token and redirects to login

### Data Flow

1. **Component mounts** - Triggers data fetching via custom hook
2. **Custom hook** - Uses React Query to manage request state
3. **Service layer** - Makes HTTP request to MCP server
4. **Response handling** - Data is cached by React Query
5. **UI update** - Component re-renders with new data
6. **Error handling** - Errors are caught and displayed to user

### Real-time Updates (SSE)

1. Component subscribes to SSE endpoint
2. EventSource connection established to MCP server
3. Server sends events as data changes
4. Client updates local state/cache
5. UI reflects changes in real-time
6. Connection cleanup on component unmount

## Key Design Decisions

### Why Vite Over Create React App?

- Significantly faster development server (ES modules)
- Faster build times (esbuild)
- Better TypeScript support out of the box
- Modern, actively maintained
- Smaller bundle sizes

### Why React Query?

- Automatic caching and deduplication
- Background refetching
- Optimistic updates
- Automatic error retry
- Reduced boilerplate for async operations
- Better UX with loading and error states

### Why Context + useReducer Over Redux?

- Simpler setup and less boilerplate
- Sufficient for moderate complexity
- No external dependencies for state management
- Easier to understand for new developers
- Can always migrate to Redux later if needed

### Why MUI?

- Comprehensive component library
- Professional design out of the box
- Strong TypeScript support
- Extensive theming capabilities
- Active community and documentation
- Accessibility built-in

## Security Considerations

### Token Management

**Never store tokens in localStorage** - vulnerable to XSS attacks

Recommended approach:
1. Store token in memory (React state/context)
2. Use httpOnly cookie for persistence (set by server)
3. Implement refresh token rotation
4. Clear tokens on logout

### Input Validation

- Validate on both client and server (defense in depth)
- Use schema validation (Zod or Yup)
- Sanitize inputs before rendering (prevent XSS)
- Escape SQL parameters (handled by server, but validate format client-side)

### HTTPS Only in Production

- Enforce HTTPS for all production deployments
- Use HSTS headers
- No mixed content (all resources over HTTPS)

### Content Security Policy

Implement strict CSP headers to prevent XSS:
- Restrict script sources
- No inline scripts (use nonces if necessary)
- Restrict style sources

### Session Isolation

- Ensure user sessions are completely isolated
- No data leakage between users
- Verify token scopes on every request
- Implement proper RBAC checks

## Performance Targets

- **First Contentful Paint (FCP)**: < 1.5s
- **Largest Contentful Paint (LCP)**: < 2.5s
- **Time to Interactive (TTI)**: < 3.5s
- **Cumulative Layout Shift (CLS)**: < 0.1
- **First Input Delay (FID)**: < 100ms

## Browser Support

- Chrome (last 2 versions)
- Firefox (last 2 versions)
- Safari (last 2 versions)
- Edge (last 2 versions)
- No IE11 support (use modern JavaScript features)

## Development Workflow

1. **Local Development**
   ```bash
   npm install
   npm run dev
   ```

2. **Type Checking**
   ```bash
   npm run type-check
   ```

3. **Linting**
   ```bash
   npm run lint
   ```

4. **Testing**
   ```bash
   npm test
   npm run test:coverage
   ```

5. **Build**
   ```bash
   npm run build
   ```

6. **Preview Production Build**
   ```bash
   npm run preview
   ```

## Environment Configuration

Use `.env` files for environment-specific configuration:

- `.env.development` - Development settings
- `.env.production` - Production settings
- `.env.test` - Test settings

All environment variables must be prefixed with `VITE_` to be accessible in
the client code.

Example:
```
VITE_API_URL=http://localhost:8080
VITE_API_TIMEOUT=30000
VITE_ENABLE_MOCK_API=false
```

## Next Steps

1. Initialize project with Vite and TypeScript
2. Set up MUI theme and component library
3. Implement authentication flow
4. Create base layout components
5. Implement routing structure
6. Set up API service layer
7. Build core features (dashboard, connections, monitoring)
8. Implement comprehensive testing
9. Optimize performance and bundle size
10. Conduct security audit

## Additional Resources

- [React Official Documentation](https://react.dev/)
- [Material-UI Documentation](https://mui.com/)
- [React Query Documentation](https://tanstack.com/query/latest)
- [Vite Documentation](https://vitejs.dev/)
- [TypeScript Documentation](https://www.typescriptlang.org/)
- [OWASP Security Guide](https://owasp.org/www-project-web-security-testing-guide/)

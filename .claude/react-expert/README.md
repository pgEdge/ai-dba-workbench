/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# React/Material-UI Frontend Expert Documentation

This directory contains comprehensive documentation for building the pgEdge AI
Workbench React frontend application. The documentation is designed to provide
practical guidance for developing new features and maintaining existing code.

## Documentation Overview

### [Architecture Overview](./architecture-overview.md)

High-level architectural decisions and technology stack for the frontend
application.

**Topics Covered**:
- Technology stack (React, TypeScript, MUI, Vite)
- Project structure and organization
- Architecture principles (separation of concerns, type safety, security)
- Application flow (authentication, data flow, SSE)
- Key design decisions and rationale
- Performance targets and browser support
- Development workflow

**When to Read**: Start here to understand the overall architecture before
building any features.

### [Component Structure](./component-structure.md)

Detailed guidance on component architecture, organization, and patterns.

**Topics Covered**:
- Component categories (common, layout, feature, page)
- Design patterns (presentational vs container, compound components, HOCs)
- Component file structure and naming conventions
- Props best practices and composition
- Error boundaries
- Accessibility guidelines
- Performance optimization
- Testing components

**When to Read**: Before creating any new component or when refactoring
existing components.

### [State Management](./state-management.md)

Comprehensive guide to managing different types of state in the application.

**Topics Covered**:
- State categories (server, global UI, local, URL)
- React Query for server state
- Context API for global state
- useState for local state
- React Router for URL state
- Patterns for queries, mutations, and optimistic updates
- Best practices and common patterns

**When to Read**: When implementing data fetching, global state, or form state.

### [MUI Patterns](./mui-patterns.md)

Material-UI component usage, theming, and customization strategies.

**Topics Covered**:
- Theme configuration (light/dark mode)
- Layout patterns (app layout, drawer, responsive grid)
- Common component patterns (tables, forms, dialogs, loading states)
- Responsive design with breakpoints
- Accessibility patterns
- Custom component variants
- Performance optimization

**When to Read**: When styling components, implementing layouts, or creating
custom MUI variants.

### [API Integration](./api-integration.md)

Patterns for communicating with the backend MCP server.

**Topics Covered**:
- API service layer architecture
- Authentication and token management
- Server-Sent Events (SSE) for real-time updates
- Error handling and retry logic
- Request cancellation
- Input validation and sanitization
- Rate limiting and throttling

**When to Read**: When implementing API calls, authentication, or real-time
features.

### [Testing Approach](./testing-approach.md)

Comprehensive testing strategy for frontend code.

**Topics Covered**:
- Testing stack (Vitest, React Testing Library, MSW, Playwright)
- Test configuration and setup
- Unit testing components and hooks
- Integration testing with API mocking
- Accessibility testing
- End-to-end testing
- Coverage requirements and best practices

**When to Read**: Before writing tests for new features or when debugging
test failures.

## Quick Start Guide

### For New Features

1. **Review Architecture** - Read [architecture-overview.md](./architecture-overview.md)
   to understand the overall structure
2. **Plan Component Structure** - Use [component-structure.md](./component-structure.md)
   to design your components
3. **Implement State Management** - Follow [state-management.md](./state-management.md)
   for data handling
4. **Style with MUI** - Apply patterns from [mui-patterns.md](./mui-patterns.md)
5. **Integrate APIs** - Use [api-integration.md](./api-integration.md) for
   backend communication
6. **Write Tests** - Follow [testing-approach.md](./testing-approach.md) to
   ensure quality

### For Bug Fixes

1. **Identify Component Type** - Determine if it's a presentation, container,
   or page component
2. **Check State Management** - Verify correct state handling patterns
3. **Review Error Handling** - Ensure proper error boundaries and user feedback
4. **Add Tests** - Write regression tests to prevent future issues

### For Code Reviews

When reviewing frontend code, check:

- [ ] Component follows single responsibility principle
- [ ] Proper TypeScript types are defined
- [ ] State is managed appropriately (local vs global vs server)
- [ ] Error handling is comprehensive
- [ ] Accessibility attributes are included
- [ ] Security best practices are followed (input validation, XSS prevention)
- [ ] Tests are written and passing
- [ ] Code follows project style guidelines (4-space indentation)
- [ ] No unnecessary re-renders or performance issues

## Project Structure Reference

```
client/
├── src/
│   ├── components/
│   │   ├── common/          # Reusable UI components
│   │   ├── layout/          # Layout components
│   │   └── features/        # Feature-specific components
│   ├── pages/               # Route-level components
│   ├── contexts/            # React contexts for global state
│   ├── hooks/               # Custom React hooks
│   ├── services/            # API communication layer
│   ├── types/               # TypeScript definitions
│   ├── utils/               # Utility functions
│   └── styles/              # Theme and global styles
├── tests/                   # Unit and integration tests
│   ├── components/
│   ├── hooks/
│   ├── services/
│   ├── mocks/              # MSW handlers and mock data
│   └── e2e/                # Playwright E2E tests
└── public/                 # Static assets
```

## Common Patterns Quick Reference

### Creating a New Page

```typescript
// src/pages/MyPage/MyPage.tsx
import { Box, Typography } from '@mui/material';

export const MyPage: React.FC = () => {
    return (
        <Box>
            <Typography variant="h4">My Page</Typography>
            {/* Page content */}
        </Box>
    );
};
```

### Fetching Data with React Query

```typescript
// src/hooks/useMyData.ts
import { useQuery } from '@tanstack/react-query';
import { getMyData } from '../services/myService';

export const useMyData = () => {
    return useQuery({
        queryKey: ['myData'],
        queryFn: getMyData,
    });
};

// In component
const { data, isLoading, error } = useMyData();
```

### Creating a Form

```typescript
// src/components/features/MyForm.tsx
import { useState } from 'react';
import { TextField, Button } from '@mui/material';

export const MyForm: React.FC = () => {
    const [formData, setFormData] = useState({ name: '' });
    const [errors, setErrors] = useState<Record<string, string>>({});

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        // Validate and submit
    };

    return (
        <form onSubmit={handleSubmit}>
            <TextField
                label="Name"
                value={formData.name}
                onChange={(e) =>
                    setFormData({ ...formData, name: e.target.value })
                }
                error={!!errors.name}
                helperText={errors.name}
            />
            <Button type="submit">Submit</Button>
        </form>
    );
};
```

### Testing a Component

```typescript
// tests/components/MyComponent.test.tsx
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MyComponent } from '@/components/MyComponent';

test('renders and handles interaction', async () => {
    const user = userEvent.setup();
    render(<MyComponent />);

    expect(screen.getByText('Hello')).toBeInTheDocument();

    await user.click(screen.getByRole('button'));

    expect(screen.getByText('Clicked')).toBeInTheDocument();
});
```

## Security Checklist

When implementing frontend features, always:

- [ ] Validate all user inputs on both client and server
- [ ] Sanitize inputs before rendering to prevent XSS
- [ ] Store authentication tokens securely (memory + httpOnly cookies)
- [ ] Use HTTPS in production
- [ ] Implement proper CSRF protection
- [ ] Never expose sensitive data in client code
- [ ] Ensure session isolation between users
- [ ] Follow OWASP security guidelines

## Performance Checklist

For optimal performance:

- [ ] Use React.memo for expensive components
- [ ] Implement code splitting with lazy loading
- [ ] Optimize images (compression, lazy loading)
- [ ] Use virtualization for large lists
- [ ] Minimize bundle size (analyze with vite-plugin-visualizer)
- [ ] Implement proper caching with React Query
- [ ] Avoid unnecessary re-renders (useCallback, useMemo)
- [ ] Monitor Core Web Vitals

## Accessibility Checklist

Ensure all features are accessible:

- [ ] Use semantic HTML elements
- [ ] Include proper ARIA attributes
- [ ] Support keyboard navigation
- [ ] Ensure proper focus management
- [ ] Maintain sufficient color contrast
- [ ] Provide alternative text for images
- [ ] Test with screen readers
- [ ] Run axe accessibility tests

## Additional Resources

### Official Documentation

- [React Documentation](https://react.dev/)
- [TypeScript Documentation](https://www.typescriptlang.org/)
- [Material-UI Documentation](https://mui.com/)
- [React Query Documentation](https://tanstack.com/query/latest)
- [Vite Documentation](https://vitejs.dev/)
- [React Testing Library](https://testing-library.com/react)
- [Playwright Documentation](https://playwright.dev/)

### Best Practices

- [React Best Practices](https://react.dev/learn/thinking-in-react)
- [TypeScript Best Practices](https://www.typescriptlang.org/docs/handbook/declaration-files/do-s-and-don-ts.html)
- [Web Accessibility Guidelines (WCAG)](https://www.w3.org/WAI/WCAG21/quickref/)
- [OWASP Security Guidelines](https://owasp.org/www-project-web-security-testing-guide/)

## Getting Help

When you need assistance:

1. **Check this documentation first** - Most common patterns are covered
2. **Review official docs** - React, MUI, and library documentation
3. **Search existing code** - Look for similar patterns in the codebase
4. **Ask for code review** - Get feedback from team members
5. **Test thoroughly** - Write tests to verify your implementation

## Contributing to This Documentation

This documentation should be updated when:

- New patterns are established
- Architecture decisions change
- New libraries or tools are adopted
- Common pitfalls are discovered
- Best practices evolve

Keep documentation:
- **Practical** - Focus on real-world usage
- **Current** - Update when things change
- **Clear** - Write for developers at all levels
- **Comprehensive** - Cover both "how" and "why"

## Project Standards Reminder

All frontend code must:

- Use **4 spaces for indentation** (project standard)
- Include the **copyright notice** at the top of each file
- Follow **TypeScript strict mode** conventions
- Include **comprehensive tests** (80%+ coverage)
- Be **accessible** (WCAG 2.1 AA compliant)
- Be **secure** (follow security best practices)
- Be **documented** (JSDoc comments for complex logic)

---

This documentation is maintained as part of the pgEdge AI Workbench project.
For questions or suggestions, please refer to the main project documentation
or open an issue.

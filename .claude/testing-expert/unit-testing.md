# Unit Testing Patterns - pgEdge AI DBA Workbench

This document covers unit testing patterns and best practices for the AI DBA Workbench project.

## Unit Test Principles

Unit tests should:
- Test one function or method in isolation
- Execute in milliseconds
- Not depend on external systems (database, network, filesystem)
- Use mocks for dependencies
- Be deterministic (same result every time)

## File Organization

### GoLang (Collector and Server)

**Location**: Co-located with source code in same package

```
src/
├── database/
│   ├── datastore.go           # Implementation
│   ├── datastore_test.go      # Tests
│   ├── schema.go
│   └── schema_test.go
└── usermgmt/
    ├── usermgmt.go
    └── usermgmt_test.go
```

**Naming Convention**:
- Test file: `<source_file>_test.go`
- Package: Same as source (e.g., `package database`)
- Test function: `func Test<FunctionName>(t *testing.T)`

### React (Client - Planned)

**Location**: Either co-located or in `/client/tests/unit/`

```
src/
├── components/
│   ├── UserList/
│   │   ├── UserList.tsx
│   │   └── UserList.test.tsx
│   └── LoginForm/
│       ├── LoginForm.tsx
│       └── LoginForm.test.tsx
└── utils/
    ├── validation.ts
    └── validation.test.ts
```

## GoLang Unit Test Patterns

### Basic Test Structure

```go
/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mypackage

import (
    "testing"
)

// TestBasicFunction tests a simple function
func TestBasicFunction(t *testing.T) {
    // Setup
    input := "test input"
    expected := "expected output"

    // Execute
    result := MyFunction(input)

    // Assert
    if result != expected {
        t.Errorf("MyFunction(%v) = %v, want %v", input, result, expected)
    }
}
```

### Using testify for Assertions

```go
package mypackage

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestWithTestify(t *testing.T) {
    result := MyFunction("input")

    // assert: test continues if fails
    assert.Equal(t, "expected", result, "should return expected value")
    assert.NotNil(t, result, "should not be nil")
    assert.Contains(t, result, "exp", "should contain substring")

    // require: test stops if fails (for critical checks)
    require.NotNil(t, result, "result must not be nil to continue")
    require.NoError(t, err, "must not error to continue")
}
```

### Table-Driven Tests

Best practice for testing multiple scenarios:

```go
func TestConnectionString(t *testing.T) {
    tests := []struct {
        name     string
        host     string
        port     int
        database string
        want     string
    }{
        {
            name:     "basic connection",
            host:     "localhost",
            port:     5432,
            database: "testdb",
            want:     "host=localhost port=5432 dbname=testdb",
        },
        {
            name:     "with special characters",
            host:     "db.example.com",
            port:     5433,
            database: "test-db",
            want:     "host=db.example.com port=5433 dbname=test-db",
        },
        {
            name:     "empty database",
            host:     "localhost",
            port:     5432,
            database: "",
            want:     "host=localhost port=5432 dbname=",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := BuildConnectionString(tt.host, tt.port, tt.database)
            if got != tt.want {
                t.Errorf("BuildConnectionString() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Testing Error Cases

```go
func TestValidation(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
        errMsg  string
    }{
        {
            name:    "valid input",
            input:   "valid@example.com",
            wantErr: false,
        },
        {
            name:    "empty input",
            input:   "",
            wantErr: true,
            errMsg:  "input cannot be empty",
        },
        {
            name:    "invalid format",
            input:   "not-an-email",
            wantErr: true,
            errMsg:  "invalid email format",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateEmail(tt.input)

            if tt.wantErr {
                if err == nil {
                    t.Error("expected error but got nil")
                    return
                }
                if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
                    t.Errorf("error = %v, want error containing %v",
                        err, tt.errMsg)
                }
            } else {
                if err != nil {
                    t.Errorf("unexpected error: %v", err)
                }
            }
        })
    }
}
```

### Mocking with Interfaces

Define interfaces for dependencies:

```go
// database.go
type Database interface {
    Query(sql string) ([]Row, error)
    Execute(sql string) error
}

type UserRepository struct {
    db Database
}

func (r *UserRepository) GetUser(id int) (*User, error) {
    rows, err := r.db.Query("SELECT * FROM users WHERE id = $1")
    if err != nil {
        return nil, err
    }
    // Process rows...
}

// database_test.go
type mockDatabase struct {
    queryResult []Row
    queryError  error
    execError   error
}

func (m *mockDatabase) Query(sql string) ([]Row, error) {
    return m.queryResult, m.queryError
}

func (m *mockDatabase) Execute(sql string) error {
    return m.execError
}

func TestUserRepository_GetUser(t *testing.T) {
    mockDB := &mockDatabase{
        queryResult: []Row{
            {ID: 1, Name: "Test User"},
        },
        queryError: nil,
    }

    repo := &UserRepository{db: mockDB}
    user, err := repo.GetUser(1)

    require.NoError(t, err)
    assert.Equal(t, "Test User", user.Name)
}

func TestUserRepository_GetUser_Error(t *testing.T) {
    mockDB := &mockDatabase{
        queryError: errors.New("connection failed"),
    }

    repo := &UserRepository{db: mockDB}
    _, err := repo.GetUser(1)

    require.Error(t, err)
    assert.Contains(t, err.Error(), "connection failed")
}
```

### Testing with Context

```go
func TestOperationWithContext(t *testing.T) {
    tests := []struct {
        name    string
        timeout time.Duration
        wantErr bool
    }{
        {
            name:    "completes within timeout",
            timeout: 5 * time.Second,
            wantErr: false,
        },
        {
            name:    "times out",
            timeout: 1 * time.Millisecond,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
            defer cancel()

            err := LongRunningOperation(ctx)

            if tt.wantErr {
                require.Error(t, err)
                assert.Equal(t, context.DeadlineExceeded, err)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

### Subtests for Complex Scenarios

```go
func TestUserManagement(t *testing.T) {
    t.Run("creation", func(t *testing.T) {
        t.Run("valid user", func(t *testing.T) {
            user, err := CreateUser("test@example.com", "Test User")
            require.NoError(t, err)
            assert.NotNil(t, user)
        })

        t.Run("duplicate email", func(t *testing.T) {
            _, err := CreateUser("duplicate@example.com", "User 1")
            require.NoError(t, err)

            _, err = CreateUser("duplicate@example.com", "User 2")
            require.Error(t, err)
            assert.Contains(t, err.Error(), "already exists")
        })

        t.Run("invalid email", func(t *testing.T) {
            _, err := CreateUser("invalid", "Test User")
            require.Error(t, err)
        })
    })

    t.Run("retrieval", func(t *testing.T) {
        t.Run("existing user", func(t *testing.T) {
            // Test code...
        })

        t.Run("non-existent user", func(t *testing.T) {
            // Test code...
        })
    })
}
```

## React Unit Test Patterns (Planned)

### Component Testing with React Testing Library

```typescript
// UserList.test.tsx
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { UserList } from './UserList';

describe('UserList', () => {
    it('renders user list', () => {
        const users = [
            { id: 1, name: 'John Doe', email: 'john@example.com' },
            { id: 2, name: 'Jane Smith', email: 'jane@example.com' },
        ];

        render(<UserList users={users} />);

        expect(screen.getByText('John Doe')).toBeInTheDocument();
        expect(screen.getByText('Jane Smith')).toBeInTheDocument();
    });

    it('calls onDelete when delete button clicked', async () => {
        const handleDelete = jest.fn();
        const users = [{ id: 1, name: 'John Doe', email: 'john@example.com' }];

        render(<UserList users={users} onDelete={handleDelete} />);

        const deleteButton = screen.getByRole('button', { name: /delete/i });
        await userEvent.click(deleteButton);

        expect(handleDelete).toHaveBeenCalledWith(1);
    });

    it('shows empty state when no users', () => {
        render(<UserList users={[]} />);

        expect(screen.getByText(/no users found/i)).toBeInTheDocument();
    });
});
```

### Hook Testing

```typescript
// useAuth.test.ts
import { renderHook, act } from '@testing-library/react';
import { useAuth } from './useAuth';

describe('useAuth', () => {
    it('initializes with logged out state', () => {
        const { result } = renderHook(() => useAuth());

        expect(result.current.isAuthenticated).toBe(false);
        expect(result.current.user).toBeNull();
    });

    it('logs in user', async () => {
        const { result } = renderHook(() => useAuth());

        await act(async () => {
            await result.current.login('user@example.com', 'password');
        });

        expect(result.current.isAuthenticated).toBe(true);
        expect(result.current.user).toEqual({
            email: 'user@example.com',
        });
    });

    it('handles login error', async () => {
        const { result } = renderHook(() => useAuth());

        await act(async () => {
            await expect(
                result.current.login('invalid', 'wrong')
            ).rejects.toThrow('Invalid credentials');
        });

        expect(result.current.isAuthenticated).toBe(false);
    });
});
```

### Utility Function Testing

```typescript
// validation.test.ts
import { validateEmail, validatePassword } from './validation';

describe('validateEmail', () => {
    it.each([
        ['valid@example.com', true],
        ['test.user@domain.co.uk', true],
        ['invalid', false],
        ['@example.com', false],
        ['test@', false],
        ['', false],
    ])('validates %s as %s', (email, expected) => {
        expect(validateEmail(email)).toBe(expected);
    });
});

describe('validatePassword', () => {
    const validCases = [
        'Password123!',
        'Strong@Pass1',
        'Secure#2023',
    ];

    const invalidCases = [
        'weak',              // too short
        'nouppercaseonly1!', // no uppercase
        'NOLOWERCASE1!',     // no lowercase
        'NoNumbers!',        // no numbers
        'NoSpecial123',      // no special char
    ];

    test.each(validCases)('accepts valid password: %s', (password) => {
        expect(validatePassword(password)).toBe(true);
    });

    test.each(invalidCases)('rejects invalid password: %s', (password) => {
        expect(validatePassword(password)).toBe(false);
    });
});
```

## Mock Strategies

### Interface-Based Mocking (GoLang)

Current pattern used in the project:

```go
// Define interface
type Config interface {
    GetPgHost() string
    GetPgPort() int
    GetPgDatabase() string
    Validate() error
}

// Create mock implementation
type mockConfig struct {
    pgHost      string
    pgPort      int
    pgDatabase  string
    validateErr error
}

func (m *mockConfig) GetPgHost() string     { return m.pgHost }
func (m *mockConfig) GetPgPort() int        { return m.pgPort }
func (m *mockConfig) GetPgDatabase() string { return m.pgDatabase }
func (m *mockConfig) Validate() error       { return m.validateErr }

// Use in tests
func TestDatastore(t *testing.T) {
    config := &mockConfig{
        pgHost:     "testhost",
        pgPort:     5432,
        pgDatabase: "testdb",
    }

    ds := NewDatastore(config)
    // Test datastore...
}
```

### Dependency Injection for Testability

Make dependencies explicit:

```go
// Bad: hard to test
type Service struct {
    db *sql.DB
}

func NewService() *Service {
    db, _ := sql.Open("postgres", "connection-string")
    return &Service{db: db}
}

// Good: testable
type Service struct {
    db Database // interface
}

func NewService(db Database) *Service {
    return &Service{db: db}
}

// Now easy to test with mock
func TestService(t *testing.T) {
    mockDB := &mockDatabase{}
    service := NewService(mockDB)
    // Test service...
}
```

## Testing Private Functions

### Option 1: Test Through Public API (Preferred)

```go
// private function
func calculateTotal(items []Item) float64 {
    total := 0.0
    for _, item := range items {
        total += item.Price
    }
    return total
}

// public function
func ProcessOrder(order Order) (float64, error) {
    total := calculateTotal(order.Items)
    // more logic...
    return total, nil
}

// Test private function through public API
func TestProcessOrder(t *testing.T) {
    order := Order{
        Items: []Item{
            {Price: 10.0},
            {Price: 20.0},
        },
    }

    total, err := ProcessOrder(order)

    require.NoError(t, err)
    assert.Equal(t, 30.0, total)
}
```

### Option 2: Test File in Same Package

```go
// myfile.go
package mypackage

func privateFunction() int {
    return 42
}

// myfile_test.go
package mypackage  // Same package, can access private functions

func TestPrivateFunction(t *testing.T) {
    result := privateFunction()
    assert.Equal(t, 42, result)
}
```

## Test Helpers and Utilities

### Setup and Teardown

```go
func TestMain(m *testing.M) {
    // Setup before all tests
    setup()

    // Run tests
    code := m.Run()

    // Teardown after all tests
    teardown()

    os.Exit(code)
}

func setup() {
    // Initialize test resources
}

func teardown() {
    // Clean up test resources
}
```

### Helper Functions

```go
// Test helpers (unexported, for use within test package)
func createTestUser(t *testing.T, name, email string) *User {
    t.Helper() // Marks this as helper, better error reporting

    user := &User{
        Name:  name,
        Email: email,
    }

    if err := user.Validate(); err != nil {
        t.Fatalf("invalid test user: %v", err)
    }

    return user
}

func TestUserCreation(t *testing.T) {
    user := createTestUser(t, "Test User", "test@example.com")
    assert.NotNil(t, user)
}
```

## Performance Testing

### Benchmarks

```go
func BenchmarkEncryption(b *testing.B) {
    password := "test-password"
    secret := "test-secret"

    b.ResetTimer() // Reset timer after setup

    for i := 0; i < b.N; i++ {
        _, err := EncryptPassword(password, secret)
        if err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkEncryptionParallel(b *testing.B) {
    password := "test-password"
    secret := "test-secret"

    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, err := EncryptPassword(password, secret)
            if err != nil {
                b.Fatal(err)
            }
        }
    })
}
```

Run benchmarks:
```bash
go test -bench=. ./...
go test -bench=BenchmarkEncryption -benchmem ./package/
```

## Common Pitfalls

### 1. Testing Implementation Instead of Behavior

```go
// Bad: tests implementation
func TestCalculatePrice_Bad(t *testing.T) {
    product := &Product{BasePrice: 100}

    // Testing internal calculation details
    discount := product.getDiscount()
    tax := product.calculateTax(100 - discount)

    assert.Equal(t, 10, discount)
    assert.Equal(t, 9, tax)
}

// Good: tests behavior
func TestCalculatePrice_Good(t *testing.T) {
    product := &Product{BasePrice: 100}

    finalPrice := product.CalculatePrice()

    assert.Equal(t, 99.0, finalPrice)
}
```

### 2. Not Using Table-Driven Tests

```go
// Bad: repetitive tests
func TestValidEmail1(t *testing.T) {
    assert.True(t, ValidateEmail("test@example.com"))
}
func TestValidEmail2(t *testing.T) {
    assert.True(t, ValidateEmail("user@domain.org"))
}
func TestInvalidEmail1(t *testing.T) {
    assert.False(t, ValidateEmail("invalid"))
}

// Good: table-driven
func TestValidateEmail(t *testing.T) {
    tests := []struct {
        email string
        valid bool
    }{
        {"test@example.com", true},
        {"user@domain.org", true},
        {"invalid", false},
    }

    for _, tt := range tests {
        t.Run(tt.email, func(t *testing.T) {
            assert.Equal(t, tt.valid, ValidateEmail(tt.email))
        })
    }
}
```

### 3. Not Cleaning Up Resources

```go
// Bad: no cleanup
func TestFile(t *testing.T) {
    f, _ := os.Create("/tmp/test.txt")
    f.WriteString("test")
    // File never closed, remains on disk
}

// Good: proper cleanup
func TestFile(t *testing.T) {
    f, err := os.Create("/tmp/test.txt")
    require.NoError(t, err)
    defer os.Remove("/tmp/test.txt")
    defer f.Close()

    _, err = f.WriteString("test")
    require.NoError(t, err)
}
```

### 4. Ignoring Errors in Tests

```go
// Bad
func TestOperation(t *testing.T) {
    result, _ := DoSomething()  // Ignoring error
    assert.Equal(t, "expected", result)
}

// Good
func TestOperation(t *testing.T) {
    result, err := DoSomething()
    require.NoError(t, err, "DoSomething should not error")
    assert.Equal(t, "expected", result)
}
```

## Running Tests

### All Tests
```bash
go test ./...
```

### Verbose Output
```bash
go test -v ./...
```

### Specific Package
```bash
go test ./database/
```

### Specific Test
```bash
go test -run TestConnectionString ./database/
```

### With Coverage
```bash
go test -cover ./...
```

### Race Detection
```bash
go test -race ./...
```

### Short Mode (Skip Long Tests)
```bash
go test -short ./...

// In test code:
if testing.Short() {
    t.Skip("skipping long test in short mode")
}
```

## Related Documents

- `testing-overview.md` - Testing strategy overview
- `integration-testing.md` - Integration test patterns
- `test-utilities.md` - Shared test utilities
- `database-testing.md` - Database testing approach
- `writing-tests.md` - Practical guide for new tests

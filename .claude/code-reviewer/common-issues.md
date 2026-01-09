# Common Issues

This document catalogs frequently found issues during code review.

## Go Anti-Patterns

### Error Handling

**Ignored errors:**

```go
// BAD
result, _ := riskyOperation()
json.Unmarshal(data, &obj)  // Error ignored

// GOOD
result, err := riskyOperation()
if err != nil {
    return fmt.Errorf("risky operation failed: %w", err)
}
```

**Empty error checks:**

```go
// BAD
if err != nil {
    // Nothing here - error silently swallowed
}

// GOOD
if err != nil {
    return err  // Or handle appropriately
}
```

**Lost error context:**

```go
// BAD
if err != nil {
    return errors.New("failed")  // Original error lost
}

// GOOD
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}
```

### Resource Management

**Missing defer for cleanup:**

```go
// BAD
file, err := os.Open(path)
// ... code that might return early ...
file.Close()  // May never be called

// GOOD
file, err := os.Open(path)
if err != nil {
    return err
}
defer file.Close()
```

**Defer in loop:**

```go
// BAD - Defers accumulate until function returns
for _, file := range files {
    f, _ := os.Open(file)
    defer f.Close()  // All close at function end!
}

// GOOD - Close in loop or use function
for _, file := range files {
    if err := processFile(file); err != nil {
        return err
    }
}

func processFile(path string) error {
    f, err := os.Open(path)
    if err != nil {
        return err
    }
    defer f.Close()
    // ...
}
```

### Concurrency

**Data race with shared state:**

```go
// BAD - Race condition
var counter int
go func() { counter++ }()
go func() { counter++ }()

// GOOD - Use sync
var counter int64
go func() { atomic.AddInt64(&counter, 1) }()
go func() { atomic.AddInt64(&counter, 1) }()
```

**Goroutine leak:**

```go
// BAD - Goroutine may block forever
go func() {
    ch <- value  // If no one reads, goroutine leaks
}()

// GOOD - Use context or select with timeout
go func() {
    select {
    case ch <- value:
    case <-ctx.Done():
    }
}()
```

### Context Misuse

**Not passing context:**

```go
// BAD
func DoWork() {
    db.Query("SELECT ...")  // No context
}

// GOOD
func DoWork(ctx context.Context) {
    db.QueryContext(ctx, "SELECT ...")
}
```

**Creating unnecessary context:**

```go
// BAD
func Handler(ctx context.Context) {
    newCtx := context.Background()  // Ignores parent context
    doWork(newCtx)
}

// GOOD
func Handler(ctx context.Context) {
    doWork(ctx)  // Propagate parent context
}
```

## React Anti-Patterns

### Hook Issues

**Missing dependencies:**

```typescript
// BAD - userId not in deps, stale closure
useEffect(() => {
    fetchUser(userId);
}, []);

// GOOD
useEffect(() => {
    fetchUser(userId);
}, [userId]);
```

**Infinite loops:**

```typescript
// BAD - Object in deps always changes
useEffect(() => {
    doSomething(config);
}, [{ key: 'value' }]);  // New object every render!

// GOOD - Stable reference or individual values
const config = useMemo(() => ({ key: 'value' }), []);
useEffect(() => {
    doSomething(config);
}, [config]);
```

### State Management

**State in render:**

```typescript
// BAD - Expensive computation every render
const Component = () => {
    const data = expensiveComputation();  // Runs every render
    return <div>{data}</div>;
};

// GOOD - Use useMemo
const Component = () => {
    const data = useMemo(() => expensiveComputation(), []);
    return <div>{data}</div>;
};
```

**Mutating state directly:**

```typescript
// BAD - Direct mutation
const [items, setItems] = useState([]);
items.push(newItem);  // Mutates existing array
setItems(items);  // React may not detect change

// GOOD - Create new array
setItems([...items, newItem]);
```

### Performance

**Inline objects/functions in props:**

```typescript
// BAD - New object every render causes child re-render
<Child style={{ margin: 10 }} onClick={() => handleClick()} />

// GOOD - Stable references
const style = useMemo(() => ({ margin: 10 }), []);
const handleClickCb = useCallback(() => handleClick(), []);
<Child style={style} onClick={handleClickCb} />
```

**Missing key or index as key:**

```typescript
// BAD - Index as key causes issues with reordering
{items.map((item, i) => <Item key={i} {...item} />)}

// GOOD - Stable unique key
{items.map(item => <Item key={item.id} {...item} />)}
```

## Database Query Issues

### SQL Injection

See security-auditor knowledge base for details.

```go
// BAD - Never do this
query := "SELECT * FROM users WHERE name = '" + name + "'"

// GOOD - Always parameterize
query := "SELECT * FROM users WHERE name = $1"
db.Query(ctx, query, name)
```

### N+1 Queries

```go
// BAD - Query in loop
users := getUsers()
for _, user := range users {
    orders := getOrdersForUser(user.ID)  // N queries!
}

// GOOD - Single query with JOIN or IN
query := `
    SELECT u.*, o.*
    FROM users u
    LEFT JOIN orders o ON o.user_id = u.id
`
```

### Missing Error Check on Rows

```go
// BAD - Error check missing
rows, _ := db.Query(ctx, query)
for rows.Next() {
    // ...
}

// GOOD - Check all errors
rows, err := db.Query(ctx, query)
if err != nil {
    return err
}
defer rows.Close()
for rows.Next() {
    // ...
}
if err := rows.Err(); err != nil {  // Check iteration errors
    return err
}
```

## API Design Issues

### Inconsistent Error Responses

```go
// BAD - Inconsistent formats
return errors.New("user not found")
return fmt.Errorf("error: %v", err)
return nil  // Silent failure

// GOOD - Consistent error handling
return fmt.Errorf("user not found: %w", ErrNotFound)
```

### Missing Input Validation

```go
// BAD - No validation
func CreateUser(username string) error {
    return db.Insert(username)
}

// GOOD - Validate first
func CreateUser(username string) error {
    if username == "" {
        return errors.New("username required")
    }
    if len(username) > 100 {
        return errors.New("username too long")
    }
    return db.Insert(username)
}
```

## Testing Issues

### Test Without Assertions

```go
// BAD - Test doesn't verify anything
func TestCreate(t *testing.T) {
    Create()  // No assertion
}

// GOOD - Assert expected behavior
func TestCreate(t *testing.T) {
    result, err := Create()
    require.NoError(t, err)
    assert.NotNil(t, result)
    assert.Equal(t, expected, result.Value)
}
```

### Test Depends on Order

```go
// BAD - Tests share state
var globalCounter int

func TestA(t *testing.T) {
    globalCounter++
}

func TestB(t *testing.T) {
    assert.Equal(t, 1, globalCounter)  // Depends on TestA running first
}

// GOOD - Independent tests
func TestA(t *testing.T) {
    counter := 0
    counter++
    assert.Equal(t, 1, counter)
}
```

### Flaky Tests

Common causes:

- Time-dependent assertions
- Race conditions
- External service dependencies
- Uncontrolled random data

Solutions:

- Use test fixtures
- Mock external services
- Seed random generators
- Use relative time comparisons

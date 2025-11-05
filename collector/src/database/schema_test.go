/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testDBName holds the name of the temporary test database
var testDBName string

// TestMain sets up and tears down the test database for all tests
func TestMain(m *testing.M) {
	// Skip if PostgreSQL tests are disabled
	if os.Getenv("SKIP_DB_TESTS") != "" {
		fmt.Println("Skipping database tests (SKIP_DB_TESTS is set)")
		os.Exit(0)
	}

	// Setup test database
	if err := setupTestDatabase(); err != nil {
		fmt.Printf("Failed to setup test database: %v\n", err)
		fmt.Println("Skipping database tests")
		os.Exit(0)
	}

	// Run tests
	exitCode := m.Run()

	// Teardown test database
	teardownTestDatabase()

	os.Exit(exitCode)
}

// setupTestDatabase creates a temporary test database
func setupTestDatabase() error {
	ctx := context.Background()

	// Generate test database name with timestamp
	now := time.Now()
	timestamp := now.Format("20060102_150405")
	microseconds := now.Nanosecond() / 1000
	testDBName = fmt.Sprintf("ai_workbench_test_%s_%06d", timestamp, microseconds)

	// Get connection string for admin database (postgres)
	adminConnStr := getAdminConnectionString()

	// Connect to admin database
	adminPool, err := pgxpool.New(ctx, adminConnStr)
	if err != nil {
		return fmt.Errorf("failed to connect to admin database: %w", err)
	}
	defer adminPool.Close()

	if err := adminPool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping admin database: %w", err)
	}

	// Create test database
	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", testDBName))
	if err != nil {
		return fmt.Errorf("failed to create test database: %w", err)
	}

	fmt.Printf("Created test database: %s\n", testDBName)
	return nil
}

// teardownTestDatabase drops the temporary test database
func teardownTestDatabase() {
	ctx := context.Background()

	if testDBName == "" {
		return
	}

	// Check if we should keep the test database
	if keep := os.Getenv("TEST_DB_KEEP"); keep == "1" || keep == "true" {
		fmt.Printf("Keeping test database: %s (TEST_DB_KEEP is set)\n", testDBName)
		return
	}

	// Get connection string for admin database
	adminConnStr := getAdminConnectionString()

	// Connect to admin database
	adminPool, err := pgxpool.New(ctx, adminConnStr)
	if err != nil {
		fmt.Printf("Failed to connect to admin database for cleanup: %v\n", err)
		return
	}
	defer adminPool.Close()

	// Drop test database
	_, err = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", testDBName))
	if err != nil {
		fmt.Printf("Warning: failed to drop test database %s: %v\n", testDBName, err)
	} else {
		fmt.Printf("Dropped test database: %s\n", testDBName)
	}
}

// getAdminConnectionString returns the connection string for the admin database (postgres)
func getAdminConnectionString() string {
	// Check for postgres:// URL format first
	if testURL := os.Getenv("TEST_DB_URL"); testURL != "" {
		return replaceDatabase(testURL, "postgres")
	}

	// Check for connection string format (backward compatibility)
	if testConnStr := os.Getenv("TEST_DB_CONN"); testConnStr != "" {
		return replaceDatabase(testConnStr, "postgres")
	}

	// Default connection to postgres database
	return "host=localhost port=5432 user=postgres dbname=postgres sslmode=disable"
}

// replaceDatabase replaces the database name in a connection string or URL
func replaceDatabase(connStr, dbName string) string {
	// Check if it's a postgres:// or postgresql:// URL
	if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
		// Parse URL: postgres://user:pass@host:port/dbname?params
		parts := strings.SplitN(connStr, "?", 2)
		baseURL := parts[0]
		queryString := ""
		if len(parts) > 1 {
			queryString = "?" + parts[1]
		}

		// Find the database name part (after last /)
		lastSlash := strings.LastIndex(baseURL, "/")
		if lastSlash != -1 {
			// Replace or add database name
			baseURL = baseURL[:lastSlash+1] + dbName
		} else {
			// No database specified, add it
			baseURL = baseURL + "/" + dbName
		}

		return baseURL + queryString
	}

	// Handle connection string format (key=value pairs)
	parts := strings.Fields(connStr)
	var newParts []string
	found := false

	for _, part := range parts {
		if strings.HasPrefix(part, "dbname=") {
			newParts = append(newParts, "dbname="+dbName)
			found = true
		} else {
			newParts = append(newParts, part)
		}
	}

	if !found {
		newParts = append(newParts, "dbname="+dbName)
	}

	return strings.Join(newParts, " ")
}

// getTestConnection creates a connection for testing
func getTestConnection(t *testing.T) (*pgxpool.Pool, *pgxpool.Conn) {
	ctx := context.Background()

	// Get base connection string
	var baseConnStr string
	if testURL := os.Getenv("TEST_DB_URL"); testURL != "" {
		baseConnStr = testURL
	} else if testConnStr := os.Getenv("TEST_DB_CONN"); testConnStr != "" {
		baseConnStr = testConnStr
	} else {
		baseConnStr = "host=localhost port=5432 user=postgres sslmode=disable"
	}

	// Replace with test database name
	connStr := replaceDatabase(baseConnStr, testDBName)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
		return nil, nil
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("Failed to ping test database: %v", err)
		return nil, nil
	}

	// Acquire a connection from the pool for schema operations
	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatalf("Failed to acquire connection from pool: %v", err)
		return nil, nil
	}

	return pool, conn
}

// cleanupTestSchema drops all test tables
func cleanupTestSchema(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()

	tables := []string{
		"user_tokens",
		"service_tokens",
		"user_accounts",
		"connections",
		"schema_version",
	}

	for _, table := range tables {
		_, err := pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
		if err != nil {
			t.Logf("Warning: failed to drop table %s: %v", table, err)
		}
	}
}

func TestNewSchemaManager(t *testing.T) {
	sm := NewSchemaManager()

	if sm == nil {
		t.Fatal("NewSchemaManager returned nil")
	}

	if len(sm.migrations) == 0 {
		t.Fatal("NewSchemaManager created manager with no migrations")
	}

	// Verify migrations are registered in order
	expectedVersions := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21}
	if len(sm.migrations) != len(expectedVersions) {
		t.Fatalf("Expected %d migrations, got %d", len(expectedVersions), len(sm.migrations))
	}

	for i, expectedVersion := range expectedVersions {
		if sm.migrations[i].Version != expectedVersion {
			t.Errorf("Migration %d: expected version %d, got %d",
				i, expectedVersion, sm.migrations[i].Version)
		}

		if sm.migrations[i].Description == "" {
			t.Errorf("Migration %d has empty description", i)
		}

		if sm.migrations[i].Up == nil {
			t.Errorf("Migration %d has nil Up function", i)
		}
	}
}

func TestMigrateFromScratch(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up any existing schema
	cleanupTestSchema(t, pool)

	// Create schema manager and migrate
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify schema_version table exists and has correct structure
	var count int
	err := pool.QueryRow(ctx, `
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'schema_version'
    `).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for schema_version table: %v", err)
	}
	if count != 1 {
		t.Fatal("schema_version table was not created")
	}

	// Verify all migrations were applied
	err = pool.QueryRow(ctx, `
        SELECT COUNT(*)
        FROM schema_version
    `).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count migrations: %v", err)
	}
	if count != len(sm.migrations) {
		t.Fatalf("Expected %d migrations to be applied, got %d", len(sm.migrations), count)
	}

	// Verify connections table exists
	err = pool.QueryRow(ctx, `
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'connections'
    `).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for connections table: %v", err)
	}
	if count != 1 {
		t.Fatal("connections table was not created")
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

func TestMigrateIdempotency(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up any existing schema
	cleanupTestSchema(t, pool)

	// Create schema manager
	sm := NewSchemaManager()

	// Run migrations first time
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("First migration failed: %v", err)
	}

	// Get version after first migration
	var version1 int
	err := pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version1)
	if err != nil {
		t.Fatalf("Failed to get version after first migration: %v", err)
	}

	// Run migrations second time (should be idempotent)
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Second migration failed: %v", err)
	}

	// Get version after second migration
	var version2 int
	err = pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version2)
	if err != nil {
		t.Fatalf("Failed to get version after second migration: %v", err)
	}

	// Versions should be the same
	if version1 != version2 {
		t.Errorf("Migrations not idempotent: version changed from %d to %d",
			version1, version2)
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

func TestGetCurrentVersion(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up any existing schema
	cleanupTestSchema(t, pool)

	sm := NewSchemaManager()

	// Test when schema_version table doesn't exist
	version, err := sm.getCurrentVersion(conn)
	if err != nil {
		t.Fatalf("Failed to get current version when table doesn't exist: %v", err)
	}
	if version != 0 {
		t.Errorf("Expected version 0 when table doesn't exist, got %d", version)
	}

	// Apply migrations
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Test when schema_version table exists
	version, err = sm.getCurrentVersion(conn)
	if err != nil {
		t.Fatalf("Failed to get current version: %v", err)
	}
	if version != len(sm.migrations) {
		t.Errorf("Expected version %d, got %d", len(sm.migrations), version)
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

func TestGetMigrationStatus(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up any existing schema
	cleanupTestSchema(t, pool)

	sm := NewSchemaManager()

	// Test status before any migrations
	statuses, err := sm.GetMigrationStatus(conn)
	if err != nil {
		t.Fatalf("Failed to get migration status: %v", err)
	}

	for _, status := range statuses {
		if status.Applied {
			t.Errorf("Migration %d should not be applied yet", status.Version)
		}
		if status.AppliedAt != nil {
			t.Errorf("Migration %d should not have AppliedAt timestamp yet", status.Version)
		}
	}

	// Apply migrations
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Test status after migrations
	statuses, err = sm.GetMigrationStatus(conn)
	if err != nil {
		t.Fatalf("Failed to get migration status: %v", err)
	}

	for _, status := range statuses {
		if !status.Applied {
			t.Errorf("Migration %d should be applied", status.Version)
		}
		if status.AppliedAt == nil {
			t.Errorf("Migration %d should have AppliedAt timestamp", status.Version)
		}
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

func TestMonitoredConnectionsConstraints(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up and migrate
	cleanupTestSchema(t, pool)
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Test port constraint
	_, err := pool.Exec(ctx, `
        INSERT INTO connections
        (name, host, port, database_name, username, owner_username)
        VALUES ('test', 'localhost', 0, 'testdb', 'testuser', 'token')
    `)
	if err == nil {
		t.Error("Expected error for invalid port, got nil")
	}

	_, err = pool.Exec(ctx, `
        INSERT INTO connections
        (name, host, port, database_name, username, owner_username)
        VALUES ('test', 'localhost', 70000, 'testdb', 'testuser', 'token')
    `)
	if err == nil {
		t.Error("Expected error for invalid port, got nil")
	}

	// Test owner_username constraint for non-shared connections
	_, err = pool.Exec(ctx, `
        INSERT INTO connections
        (name, host, port, database_name, username, is_shared)
        VALUES ('test', 'localhost', 5432, 'testdb', 'testuser', FALSE)
    `)
	if err == nil {
		t.Error("Expected error for missing owner_username on non-shared connection")
	}

	// Create a test user account for the valid insertion test
	_, err = pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testowner', 'testowner@example.com', 'Test Owner', 'hash123')
    `)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Test valid insertion with owner_username
	_, err = pool.Exec(ctx, `
        INSERT INTO connections
        (name, host, port, database_name, username, owner_username)
        VALUES ('test', 'localhost', 5432, 'testdb', 'testuser', 'testowner')
    `)
	if err != nil {
		t.Errorf("Failed to insert valid connection with owner_username: %v", err)
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

func TestIndexesCreated(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up and migrate
	cleanupTestSchema(t, pool)
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Check for indexes on connections
	expectedIndexes := []string{
		"idx_connections_owner_username",
		"idx_connections_owner_token",
		"idx_connections_is_monitored",
		"idx_connections_name",
	}

	for _, indexName := range expectedIndexes {
		var count int
		err := pool.QueryRow(ctx, `
            SELECT COUNT(*)
            FROM pg_indexes
            WHERE tablename = 'connections'
            AND indexname = $1
        `, indexName).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for index %s: %v", indexName, err)
		}
		if count != 1 {
			t.Errorf("Index %s not found", indexName)
		}
	}

	// Clean up
	cleanupTestSchema(t, pool)
}
func TestUserAccountsTable(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up and migrate
	cleanupTestSchema(t, pool)
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify table exists
	var count int
	err := pool.QueryRow(ctx, `
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'user_accounts'
    `).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for user_accounts table: %v", err)
	}
	if count != 1 {
		t.Fatal("user_accounts table was not created")
	}

	// Test unique constraint on username
	_, err = pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser', 'test@example.com', 'Test User', 'hash123')
    `)
	if err != nil {
		t.Errorf("Failed to insert first user: %v", err)
	}

	_, err = pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser', 'test2@example.com', 'Test User 2', 'hash456')
    `)
	if err == nil {
		t.Error("Expected error for duplicate username, got nil")
	}

	// Test empty username constraint
	_, err = pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('', 'test3@example.com', 'Test User 3', 'hash789')
    `)
	if err == nil {
		t.Error("Expected error for empty username, got nil")
	}

	// Test empty email constraint
	_, err = pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser2', '', 'Test User 2', 'hash789')
    `)
	if err == nil {
		t.Error("Expected error for empty email, got nil")
	}

	// Test empty password_hash constraint
	_, err = pool.Exec(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser2', 'test2@example.com', 'Test User 2', '')
    `)
	if err == nil {
		t.Error("Expected error for empty password_hash, got nil")
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

func TestUserTokensTable(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up and migrate
	cleanupTestSchema(t, pool)
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify table exists
	var count int
	err := pool.QueryRow(ctx, `
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'user_tokens'
    `).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for user_tokens table: %v", err)
	}
	if count != 1 {
		t.Fatal("user_tokens table was not created")
	}

	// Create a test user first
	var userID int
	err = pool.QueryRow(ctx, `
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser', 'test@example.com', 'Test User', 'hash123')
        RETURNING id
    `).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Test foreign key constraint
	_, err = pool.Exec(ctx, `
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES (99999, 'token123', NOW() + INTERVAL '24 hours')
    `)
	if err == nil {
		t.Error("Expected error for invalid user_id foreign key, got nil")
	}

	// Test valid token insertion
	_, err = pool.Exec(ctx, `
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES ($1, 'token123', NOW() + INTERVAL '24 hours')
    `, userID)
	if err != nil {
		t.Errorf("Failed to insert valid token: %v", err)
	}

	// Test unique constraint on token_hash
	_, err = pool.Exec(ctx, `
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES ($1, 'token123', NOW() + INTERVAL '24 hours')
    `, userID)
	if err == nil {
		t.Error("Expected error for duplicate token_hash, got nil")
	}

	// Test empty token_hash constraint
	_, err = pool.Exec(ctx, `
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES ($1, '', NOW() + INTERVAL '24 hours')
    `, userID)
	if err == nil {
		t.Error("Expected error for empty token_hash, got nil")
	}

	// Test expires_at constraint (must be in future)
	_, err = pool.Exec(ctx, `
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES ($1, 'token456', NOW() - INTERVAL '1 hour')
    `, userID)
	if err == nil {
		t.Error("Expected error for expires_at in the past, got nil")
	}

	// Test cascade delete
	_, err = pool.Exec(ctx, `DELETE FROM user_accounts WHERE id = $1`, userID)
	if err != nil {
		t.Errorf("Failed to delete user: %v", err)
	}

	err = pool.QueryRow(ctx, `
        SELECT COUNT(*) FROM user_tokens WHERE user_id = $1
    `, userID).Scan(&count)
	if err != nil {
		t.Errorf("Failed to count tokens: %v", err)
	}
	if count != 0 {
		t.Error("Expected cascade delete to remove tokens")
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

func TestServiceTokensTable(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up and migrate
	cleanupTestSchema(t, pool)
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify table exists
	var count int
	err := pool.QueryRow(ctx, `
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'service_tokens'
    `).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for service_tokens table: %v", err)
	}
	if count != 1 {
		t.Fatal("service_tokens table was not created")
	}

	// Test valid token insertion
	_, err = pool.Exec(ctx, `
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('service1', 'stoken123', 'Test service token')
    `)
	if err != nil {
		t.Errorf("Failed to insert valid service token: %v", err)
	}

	// Test unique constraint on name
	_, err = pool.Exec(ctx, `
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('service1', 'stoken456', 'Another test token')
    `)
	if err == nil {
		t.Error("Expected error for duplicate service name, got nil")
	}

	// Test unique constraint on token_hash
	_, err = pool.Exec(ctx, `
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('service2', 'stoken123', 'Another test token')
    `)
	if err == nil {
		t.Error("Expected error for duplicate token_hash, got nil")
	}

	// Test empty name constraint
	_, err = pool.Exec(ctx, `
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('', 'stoken789', 'Test token')
    `)
	if err == nil {
		t.Error("Expected error for empty name, got nil")
	}

	// Test empty token_hash constraint
	_, err = pool.Exec(ctx, `
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('service2', '', 'Test token')
    `)
	if err == nil {
		t.Error("Expected error for empty token_hash, got nil")
	}

	// Test nullable expires_at (service tokens can be permanent)
	_, err = pool.Exec(ctx, `
        INSERT INTO service_tokens (name, token_hash, expires_at, note)
        VALUES ('service2', 'stoken456', NULL, 'Permanent token')
    `)
	if err != nil {
		t.Errorf("Failed to insert service token with null expires_at: %v", err)
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

func TestAuthenticationIndexes(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up and migrate
	cleanupTestSchema(t, pool)
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Check for indexes on user_accounts
	expectedIndexes := []string{
		"idx_user_accounts_username",
		"idx_user_accounts_email",
	}

	for _, indexName := range expectedIndexes {
		var count int
		err := pool.QueryRow(ctx, `
            SELECT COUNT(*)
            FROM pg_indexes
            WHERE tablename = 'user_accounts'
            AND indexname = $1
        `, indexName).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for index %s: %v", indexName, err)
		}
		if count != 1 {
			t.Errorf("Index %s not found", indexName)
		}
	}

	// Check for indexes on user_tokens
	expectedIndexes = []string{
		"idx_user_tokens_user_id",
		"idx_user_tokens_token_hash",
		"idx_user_tokens_expires_at",
	}

	for _, indexName := range expectedIndexes {
		var count int
		err := pool.QueryRow(ctx, `
            SELECT COUNT(*)
            FROM pg_indexes
            WHERE tablename = 'user_tokens'
            AND indexname = $1
        `, indexName).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for index %s: %v", indexName, err)
		}
		if count != 1 {
			t.Errorf("Index %s not found", indexName)
		}
	}

	// Check for indexes on service_tokens
	expectedIndexes = []string{
		"idx_service_tokens_name",
		"idx_service_tokens_token_hash",
		"idx_service_tokens_expires_at",
	}

	for _, indexName := range expectedIndexes {
		var count int
		err := pool.QueryRow(ctx, `
            SELECT COUNT(*)
            FROM pg_indexes
            WHERE tablename = 'service_tokens'
            AND indexname = $1
        `, indexName).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for index %s: %v", indexName, err)
		}
		if count != 1 {
			t.Errorf("Index %s not found", indexName)
		}
	}

	// Clean up
	cleanupTestSchema(t, pool)
}

// TestZZZ_FullSchemaForInspection creates the full schema and leaves it in
// place for inspection. This test runs last (due to ZZZ prefix) and does not
// clean up, allowing users to inspect the schema when TEST_DB_KEEP=1 is set.
func TestZZZ_FullSchemaForInspection(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Clean up any existing schema from previous tests
	cleanupTestSchema(t, pool)

	// Create the full schema
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to create full schema: %v", err)
	}

	// Verify all tables exist
	expectedTables := []string{
		"schema_version",
		"connections",
		"user_accounts",
		"user_tokens",
		"service_tokens",
		"probe_configs",
	}

	for _, tableName := range expectedTables {
		var count int
		err := pool.QueryRow(ctx, `
            SELECT COUNT(*)
            FROM information_schema.tables
            WHERE table_name = $1
        `, tableName).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for table %s: %v", tableName, err)
		}
		if count != 1 {
			t.Errorf("Table %s not found", tableName)
		}
	}

	// Log message about schema inspection
	if keep := os.Getenv("TEST_DB_KEEP"); keep == "1" || keep == "true" {
		t.Logf("Full schema created in test database: %s", testDBName)
		t.Logf("Database will be kept for inspection (TEST_DB_KEEP is set)")
		t.Logf("To inspect: psql -d %s", testDBName)
		t.Logf("To clean up manually: DROP DATABASE %s", testDBName)
	}

	// DO NOT call cleanupTestSchema here - leave schema in place for inspection
}

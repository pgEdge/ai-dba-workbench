/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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
	if keep := os.Getenv("TEST_AI_WORKBENCH_KEEP_DB"); keep == "1" || keep == "true" {
		fmt.Printf("Keeping test database: %s (TEST_AI_WORKBENCH_KEEP_DB is set)\n", testDBName)
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
	if testURL := os.Getenv("TEST_AI_WORKBENCH_SERVER"); testURL != "" {
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
	if testURL := os.Getenv("TEST_AI_WORKBENCH_SERVER"); testURL != "" {
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
		// Alerter tables (migrations 7-10)
		"anomaly_candidates",
		"correlation_groups",
		"metric_baselines",
		"metric_definitions",
		"blackout_schedules",
		"blackouts",
		"alert_acknowledgments",
		"alerts",
		"alert_thresholds",
		"alert_rules",
		"probe_availability",
		"alerter_settings",
		// Core tables (migrations 1-5)
		"probe_configs",
		"connections",
		"schema_version",
	}

	// Drop metrics schema first
	_, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS metrics CASCADE")
	if err != nil {
		t.Logf("Warning: failed to drop metrics schema: %v", err)
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
	// All migrations have been squashed into a single migration at version 1
	// that creates the complete schema with all tables, indexes, and seed data
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
	// The version should be the highest migration version (4), not the count of migrations
	version, err = sm.getCurrentVersion(conn)
	if err != nil {
		t.Fatalf("Failed to get current version: %v", err)
	}
	// Get the highest version from migrations
	highestVersion := 0
	for _, m := range sm.migrations {
		if m.Version > highestVersion {
			highestVersion = m.Version
		}
	}
	if version != highestVersion {
		t.Errorf("Expected version %d, got %d", highestVersion, version)
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

	// Test port constraint - port must be > 0
	_, err := pool.Exec(ctx, `
        INSERT INTO connections
        (name, host, port, database_name, username, owner_username)
        VALUES ('test', 'localhost', 0, 'testdb', 'testuser', 'testowner')
    `)
	if err == nil {
		t.Error("Expected error for invalid port, got nil")
	}

	// Test port constraint - port must be <= 65535
	_, err = pool.Exec(ctx, `
        INSERT INTO connections
        (name, host, port, database_name, username, owner_username)
        VALUES ('test', 'localhost', 70000, 'testdb', 'testuser', 'testowner')
    `)
	if err == nil {
		t.Error("Expected error for invalid port, got nil")
	}

	// Test chk_owner constraint - must have either owner_username or owner_token
	_, err = pool.Exec(ctx, `
        INSERT INTO connections
        (name, host, port, database_name, username, is_shared)
        VALUES ('test', 'localhost', 5432, 'testdb', 'testuser', FALSE)
    `)
	if err == nil {
		t.Error("Expected error for missing owner_username/owner_token on connection")
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

	// Test valid insertion with owner_token
	_, err = pool.Exec(ctx, `
        INSERT INTO connections
        (name, host, port, database_name, username, owner_token)
        VALUES ('test2', 'localhost', 5432, 'testdb', 'testuser', 'service-token-123')
    `)
	if err != nil {
		t.Errorf("Failed to insert valid connection with owner_token: %v", err)
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

// TestZZZ_FullSchemaForInspection creates the full schema and leaves it in
// place for inspection. This test runs last (due to ZZZ prefix) and does not
// clean up, allowing users to inspect the schema when TEST_AI_WORKBENCH_KEEP_DB=1 is set.
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
	// Note: user_accounts, user_tokens, service_tokens moved to SQLite auth store
	expectedTables := []string{
		"schema_version",
		"connections",
		"probe_configs",
		// Alerter tables (migrations 7-10)
		"alerter_settings",
		"probe_availability",
		"alert_rules",
		"alert_thresholds",
		"alerts",
		"alert_acknowledgments",
		"blackouts",
		"blackout_schedules",
		"metric_definitions",
		"metric_baselines",
		"correlation_groups",
		"anomaly_candidates",
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

	// Verify metrics schema tables exist
	expectedMetricsTables := []string{
		"pg_connectivity",
	}

	for _, tableName := range expectedMetricsTables {
		var count int
		err := pool.QueryRow(ctx, `
            SELECT COUNT(*)
            FROM information_schema.tables
            WHERE table_schema = 'metrics'
            AND table_name = $1
        `, tableName).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for metrics table %s: %v", tableName, err)
		}
		if count != 1 {
			t.Errorf("Metrics table metrics.%s not found", tableName)
		}
	}

	// Log message about schema inspection
	if keep := os.Getenv("TEST_AI_WORKBENCH_KEEP_DB"); keep == "1" || keep == "true" {
		t.Logf("Full schema created in test database: %s", testDBName)
		t.Logf("Database will be kept for inspection (TEST_AI_WORKBENCH_KEEP_DB is set)")
		t.Logf("To inspect: psql -d %s", testDBName)
		t.Logf("To clean up manually: DROP DATABASE %s", testDBName)
	}

	// DO NOT call cleanupTestSchema here - leave schema in place for inspection
}

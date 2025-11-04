/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// getTestConnection creates a connection for testing
func getTestConnection(t *testing.T) *sql.DB {
	// Skip if PostgreSQL is not available
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database tests (SKIP_DB_TESTS is set)")
	}

	// Use test database
	connStr := "host=localhost port=5432 user=postgres dbname=postgres sslmode=disable"
	if testConnStr := os.Getenv("TEST_DB_CONN"); testConnStr != "" {
		connStr = testConnStr
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Skip("PostgreSQL not available: " + err.Error())
		return nil
	}

	if err := db.Ping(); err != nil {
		if cerr := db.Close(); cerr != nil {
			t.Logf("Error closing database: %v", cerr)
		}
		t.Skip("PostgreSQL not available: " + err.Error())
		return nil
	}

	return db
}

// cleanupTestSchema drops all test tables
func cleanupTestSchema(t *testing.T, db *sql.DB) {
	tables := []string{
		"user_tokens",
		"service_tokens",
		"user_accounts",
		"monitored_connections",
		"schema_version",
	}

	for _, table := range tables {
		_, err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
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
	expectedVersions := []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
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
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up any existing schema
	cleanupTestSchema(t, db)

	// Create schema manager and migrate
	sm := NewSchemaManager()
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify schema_version table exists and has correct structure
	var count int
	err := db.QueryRow(`
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
	err = db.QueryRow(`
        SELECT COUNT(*)
        FROM schema_version
    `).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count migrations: %v", err)
	}
	if count != len(sm.migrations) {
		t.Fatalf("Expected %d migrations to be applied, got %d", len(sm.migrations), count)
	}

	// Verify monitored_connections table exists
	err = db.QueryRow(`
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'monitored_connections'
    `).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for monitored_connections table: %v", err)
	}
	if count != 1 {
		t.Fatal("monitored_connections table was not created")
	}

	// Clean up
	cleanupTestSchema(t, db)
}

func TestMigrateIdempotency(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up any existing schema
	cleanupTestSchema(t, db)

	// Create schema manager
	sm := NewSchemaManager()

	// Run migrations first time
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("First migration failed: %v", err)
	}

	// Get version after first migration
	var version1 int
	err := db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version1)
	if err != nil {
		t.Fatalf("Failed to get version after first migration: %v", err)
	}

	// Run migrations second time (should be idempotent)
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Second migration failed: %v", err)
	}

	// Get version after second migration
	var version2 int
	err = db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version2)
	if err != nil {
		t.Fatalf("Failed to get version after second migration: %v", err)
	}

	// Versions should be the same
	if version1 != version2 {
		t.Errorf("Migrations not idempotent: version changed from %d to %d",
			version1, version2)
	}

	// Clean up
	cleanupTestSchema(t, db)
}

func TestGetCurrentVersion(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up any existing schema
	cleanupTestSchema(t, db)

	sm := NewSchemaManager()

	// Test when schema_version table doesn't exist
	version, err := sm.getCurrentVersion(db)
	if err != nil {
		t.Fatalf("Failed to get current version when table doesn't exist: %v", err)
	}
	if version != 0 {
		t.Errorf("Expected version 0 when table doesn't exist, got %d", version)
	}

	// Apply migrations
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Test when schema_version table exists
	version, err = sm.getCurrentVersion(db)
	if err != nil {
		t.Fatalf("Failed to get current version: %v", err)
	}
	if version != len(sm.migrations) {
		t.Errorf("Expected version %d, got %d", len(sm.migrations), version)
	}

	// Clean up
	cleanupTestSchema(t, db)
}

func TestGetMigrationStatus(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up any existing schema
	cleanupTestSchema(t, db)

	sm := NewSchemaManager()

	// Test status before any migrations
	statuses, err := sm.GetMigrationStatus(db)
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
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Test status after migrations
	statuses, err = sm.GetMigrationStatus(db)
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
	cleanupTestSchema(t, db)
}

func TestMonitoredConnectionsConstraints(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up and migrate
	cleanupTestSchema(t, db)
	sm := NewSchemaManager()
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Test port constraint
	_, err := db.Exec(`
        INSERT INTO monitored_connections
        (name, host, port, database_name, username, owner_token)
        VALUES ('test', 'localhost', 0, 'testdb', 'testuser', 'token')
    `)
	if err == nil {
		t.Error("Expected error for invalid port, got nil")
	}

	_, err = db.Exec(`
        INSERT INTO monitored_connections
        (name, host, port, database_name, username, owner_token)
        VALUES ('test', 'localhost', 70000, 'testdb', 'testuser', 'token')
    `)
	if err == nil {
		t.Error("Expected error for invalid port, got nil")
	}

	// Test owner_token constraint for non-shared connections
	_, err = db.Exec(`
        INSERT INTO monitored_connections
        (name, host, port, database_name, username, is_shared)
        VALUES ('test', 'localhost', 5432, 'testdb', 'testuser', FALSE)
    `)
	if err == nil {
		t.Error("Expected error for missing owner_token on non-shared connection")
	}

	// Test valid insertion
	_, err = db.Exec(`
        INSERT INTO monitored_connections
        (name, host, port, database_name, username, owner_token)
        VALUES ('test', 'localhost', 5432, 'testdb', 'testuser', 'token')
    `)
	if err != nil {
		t.Errorf("Failed to insert valid connection: %v", err)
	}

	// Clean up
	cleanupTestSchema(t, db)
}

func TestIndexesCreated(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up and migrate
	cleanupTestSchema(t, db)
	sm := NewSchemaManager()
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Check for indexes on monitored_connections
	expectedIndexes := []string{
		"idx_monitored_connections_owner_token",
		"idx_monitored_connections_is_monitored",
		"idx_monitored_connections_name",
	}

	for _, indexName := range expectedIndexes {
		var count int
		err := db.QueryRow(`
            SELECT COUNT(*)
            FROM pg_indexes
            WHERE tablename = 'monitored_connections'
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
	cleanupTestSchema(t, db)
}
func TestUserAccountsTable(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up and migrate
	cleanupTestSchema(t, db)
	sm := NewSchemaManager()
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify table exists
	var count int
	err := db.QueryRow(`
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
	_, err = db.Exec(`
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser', 'test@example.com', 'Test User', 'hash123')
    `)
	if err != nil {
		t.Errorf("Failed to insert first user: %v", err)
	}

	_, err = db.Exec(`
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser', 'test2@example.com', 'Test User 2', 'hash456')
    `)
	if err == nil {
		t.Error("Expected error for duplicate username, got nil")
	}

	// Test empty username constraint
	_, err = db.Exec(`
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('', 'test3@example.com', 'Test User 3', 'hash789')
    `)
	if err == nil {
		t.Error("Expected error for empty username, got nil")
	}

	// Test empty email constraint
	_, err = db.Exec(`
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser2', '', 'Test User 2', 'hash789')
    `)
	if err == nil {
		t.Error("Expected error for empty email, got nil")
	}

	// Test empty password_hash constraint
	_, err = db.Exec(`
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser2', 'test2@example.com', 'Test User 2', '')
    `)
	if err == nil {
		t.Error("Expected error for empty password_hash, got nil")
	}

	// Clean up
	cleanupTestSchema(t, db)
}

func TestUserTokensTable(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up and migrate
	cleanupTestSchema(t, db)
	sm := NewSchemaManager()
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify table exists
	var count int
	err := db.QueryRow(`
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
	err = db.QueryRow(`
        INSERT INTO user_accounts (username, email, full_name, password_hash)
        VALUES ('testuser', 'test@example.com', 'Test User', 'hash123')
        RETURNING id
    `).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Test foreign key constraint
	_, err = db.Exec(`
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES (99999, 'token123', NOW() + INTERVAL '24 hours')
    `)
	if err == nil {
		t.Error("Expected error for invalid user_id foreign key, got nil")
	}

	// Test valid token insertion
	_, err = db.Exec(`
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES ($1, 'token123', NOW() + INTERVAL '24 hours')
    `, userID)
	if err != nil {
		t.Errorf("Failed to insert valid token: %v", err)
	}

	// Test unique constraint on token_hash
	_, err = db.Exec(`
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES ($1, 'token123', NOW() + INTERVAL '24 hours')
    `, userID)
	if err == nil {
		t.Error("Expected error for duplicate token_hash, got nil")
	}

	// Test empty token_hash constraint
	_, err = db.Exec(`
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES ($1, '', NOW() + INTERVAL '24 hours')
    `, userID)
	if err == nil {
		t.Error("Expected error for empty token_hash, got nil")
	}

	// Test expires_at constraint (must be in future)
	_, err = db.Exec(`
        INSERT INTO user_tokens (user_id, token_hash, expires_at)
        VALUES ($1, 'token456', NOW() - INTERVAL '1 hour')
    `, userID)
	if err == nil {
		t.Error("Expected error for expires_at in the past, got nil")
	}

	// Test cascade delete
	_, err = db.Exec(`DELETE FROM user_accounts WHERE id = $1`, userID)
	if err != nil {
		t.Errorf("Failed to delete user: %v", err)
	}

	err = db.QueryRow(`
        SELECT COUNT(*) FROM user_tokens WHERE user_id = $1
    `, userID).Scan(&count)
	if err != nil {
		t.Errorf("Failed to count tokens: %v", err)
	}
	if count != 0 {
		t.Error("Expected cascade delete to remove tokens")
	}

	// Clean up
	cleanupTestSchema(t, db)
}

func TestServiceTokensTable(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up and migrate
	cleanupTestSchema(t, db)
	sm := NewSchemaManager()
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Verify table exists
	var count int
	err := db.QueryRow(`
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
	_, err = db.Exec(`
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('service1', 'stoken123', 'Test service token')
    `)
	if err != nil {
		t.Errorf("Failed to insert valid service token: %v", err)
	}

	// Test unique constraint on name
	_, err = db.Exec(`
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('service1', 'stoken456', 'Another test token')
    `)
	if err == nil {
		t.Error("Expected error for duplicate service name, got nil")
	}

	// Test unique constraint on token_hash
	_, err = db.Exec(`
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('service2', 'stoken123', 'Another test token')
    `)
	if err == nil {
		t.Error("Expected error for duplicate token_hash, got nil")
	}

	// Test empty name constraint
	_, err = db.Exec(`
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('', 'stoken789', 'Test token')
    `)
	if err == nil {
		t.Error("Expected error for empty name, got nil")
	}

	// Test empty token_hash constraint
	_, err = db.Exec(`
        INSERT INTO service_tokens (name, token_hash, note)
        VALUES ('service2', '', 'Test token')
    `)
	if err == nil {
		t.Error("Expected error for empty token_hash, got nil")
	}

	// Test nullable expires_at (service tokens can be permanent)
	_, err = db.Exec(`
        INSERT INTO service_tokens (name, token_hash, expires_at, note)
        VALUES ('service2', 'stoken456', NULL, 'Permanent token')
    `)
	if err != nil {
		t.Errorf("Failed to insert service token with null expires_at: %v", err)
	}

	// Clean up
	cleanupTestSchema(t, db)
}

func TestAuthenticationIndexes(t *testing.T) {
	db := getTestConnection(t)
	if db == nil {
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Error closing database: %v", err)
		}
	}()

	// Clean up and migrate
	cleanupTestSchema(t, db)
	sm := NewSchemaManager()
	if err := sm.Migrate(db); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Check for indexes on user_accounts
	expectedIndexes := []string{
		"idx_user_accounts_username",
		"idx_user_accounts_email",
	}

	for _, indexName := range expectedIndexes {
		var count int
		err := db.QueryRow(`
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
		err := db.QueryRow(`
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
		err := db.QueryRow(`
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
	cleanupTestSchema(t, db)
}

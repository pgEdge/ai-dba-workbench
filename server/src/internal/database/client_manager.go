/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package database

import (
	"fmt"
	"os"
	"sync"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// ClientManager manages per-token database clients for connection isolation
// Each authenticated token gets its own database connection
type ClientManager struct {
	mu       sync.RWMutex
	clients  map[string]*Client     // tokenHash -> client
	dbConfig *config.DatabaseConfig // single database configuration
}

// NewClientManager creates a new client manager with database configuration
func NewClientManager(dbConfig *config.DatabaseConfig) *ClientManager {
	return &ClientManager{
		clients:  make(map[string]*Client),
		dbConfig: dbConfig,
	}
}

// GetClient returns a database client for the given token hash
// Creates a new client if one doesn't exist for this token
func (cm *ClientManager) GetClient(tokenHash string) (*Client, error) {
	if tokenHash == "" {
		return nil, fmt.Errorf("token hash is required for authenticated requests")
	}

	// Try to get existing client (read lock)
	cm.mu.RLock()
	if client, exists := cm.clients[tokenHash]; exists {
		cm.mu.RUnlock()
		return client, nil
	}
	dbConfig := cm.dbConfig
	cm.mu.RUnlock()

	if dbConfig == nil {
		return nil, fmt.Errorf("no database configured")
	}

	// Create new client (write lock)
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := cm.clients[tokenHash]; exists {
		return client, nil
	}

	// Create and initialize new client with database configuration
	client := NewClient(dbConfig)
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := client.LoadMetadata(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	cm.clients[tokenHash] = client
	return client, nil
}

// GetDatabaseConfig returns the database configuration
func (cm *ClientManager) GetDatabaseConfig() *config.DatabaseConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.dbConfig
}

// UpdateDatabaseConfig updates the database configuration
// Used for SIGHUP config reload
// Note: Existing connections are NOT closed - they will be reused if config matches
func (cm *ClientManager) UpdateDatabaseConfig(dbConfig *config.DatabaseConfig) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.dbConfig = dbConfig

	if dbConfig != nil {
		fmt.Fprintf(os.Stderr, "Updated database configuration: %s@%s:%d/%s\n",
			dbConfig.User, dbConfig.Host, dbConfig.Port, dbConfig.Database)
	} else {
		fmt.Fprintf(os.Stderr, "Database configuration cleared\n")
	}
}

// RemoveClient removes and closes the database client for a given token hash
// This should be called when a token is removed or expires
func (cm *ClientManager) RemoveClient(tokenHash string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	client, exists := cm.clients[tokenHash]
	if !exists {
		return nil // Already removed
	}

	client.Close()
	delete(cm.clients, tokenHash)

	// Log with truncated hash for security
	hashPreview := tokenHash
	if len(tokenHash) > 12 {
		hashPreview = tokenHash[:12]
	}
	fmt.Fprintf(os.Stderr, "Removed database connection for token hash: %s...\n", hashPreview)

	return nil
}

// RemoveClients removes and closes database clients for multiple token hashes
// This is useful for bulk cleanup when multiple tokens expire
func (cm *ClientManager) RemoveClients(tokenHashes []string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	removedCount := 0
	for _, tokenHash := range tokenHashes {
		if client, exists := cm.clients[tokenHash]; exists {
			client.Close()
			delete(cm.clients, tokenHash)
			removedCount++
		}
	}

	if removedCount > 0 {
		fmt.Fprintf(os.Stderr, "Removed connections for %d token(s)\n", removedCount)
	}

	return nil
}

// CloseAll closes all managed database clients
// This should be called on server shutdown
func (cm *ClientManager) CloseAll() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, client := range cm.clients {
		client.Close()
	}

	cm.clients = make(map[string]*Client)
	return nil
}

// GetClientCount returns the number of active database client connections
// Useful for monitoring and testing
func (cm *ClientManager) GetClientCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.clients)
}

// SetClient sets a database client for the given key (token hash)
// This allows runtime configuration of database connections
func (cm *ClientManager) SetClient(key string, client *Client) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if client == nil {
		return fmt.Errorf("client cannot be nil")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Close existing client if it exists
	if existingClient, exists := cm.clients[key]; exists {
		existingClient.Close()
	}

	cm.clients[key] = client
	return nil
}

// GetOrCreateClient returns a database client for the given key
// If no client exists and autoConnect is true, creates and connects a new client
// If no client exists and autoConnect is false, returns an error
func (cm *ClientManager) GetOrCreateClient(key string, autoConnect bool) (*Client, error) {
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}

	// Try to get existing client (read lock)
	cm.mu.RLock()
	if client, exists := cm.clients[key]; exists {
		cm.mu.RUnlock()
		return client, nil
	}
	dbConfig := cm.dbConfig
	cm.mu.RUnlock()

	if !autoConnect {
		return nil, fmt.Errorf("no database connection configured")
	}

	if dbConfig == nil {
		return nil, fmt.Errorf("no database configured")
	}

	// Create new client (write lock)
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := cm.clients[key]; exists {
		return client, nil
	}

	// Create and initialize new client with database configuration
	client := NewClient(dbConfig)
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := client.LoadMetadata(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	cm.clients[key] = client
	return client, nil
}

// SessionInfo contains the information needed to create a client for a session
type SessionInfo struct {
	TokenHash    string
	ConnectionID int
	DatabaseName *string
}

// GetClientForSession returns a database client for a connection session
// This is a helper that builds the appropriate client key and connection string
func (cm *ClientManager) GetClientForSession(session *SessionInfo, connStr string) (*Client, error) {
	if session == nil {
		return nil, fmt.Errorf("session info is required")
	}
	if connStr == "" {
		return nil, fmt.Errorf("connection string is required")
	}

	// Build unique key combining token hash and connection ID
	clientKey := fmt.Sprintf("%s:conn:%d", session.TokenHash, session.ConnectionID)
	if session.DatabaseName != nil {
		clientKey = fmt.Sprintf("%s:db:%s", clientKey, *session.DatabaseName)
	}

	return cm.GetOrCreateClientWithConnString(clientKey, connStr)
}

// GetOrCreateClientWithConnString returns a database client for the given key
// Creates a client using the provided connection string if one doesn't exist
func (cm *ClientManager) GetOrCreateClientWithConnString(key string, connStr string) (*Client, error) {
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if connStr == "" {
		return nil, fmt.Errorf("connection string is required")
	}

	// Try to get existing client (read lock)
	cm.mu.RLock()
	if client, exists := cm.clients[key]; exists {
		// Check if it's connected to the same connection string
		if client.GetDefaultConnection() == connStr {
			cm.mu.RUnlock()
			return client, nil
		}
		// Different connection string - need to replace the client
		cm.mu.RUnlock()
	} else {
		cm.mu.RUnlock()
	}

	// Create new client (write lock)
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Close existing client if it exists and has different connection
	if existingClient, exists := cm.clients[key]; exists {
		if existingClient.GetDefaultConnection() == connStr {
			return existingClient, nil
		}
		existingClient.Close()
		delete(cm.clients, key)
	}

	// Create and initialize new client with connection string
	client := NewClientWithConnectionString(connStr, cm.dbConfig)
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := client.LoadMetadata(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	cm.clients[key] = client
	return client, nil
}

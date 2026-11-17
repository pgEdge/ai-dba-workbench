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
	"errors"
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

	// monitoredGuard restricts the addresses the clients made for
	// monitored connections (GetOrCreateClientWithConnString) may dial.
	// Nil selects the always-blocked floor; see SetMonitoredDialGuard.
	monitoredGuard DialGuard

	// inflight holds the monitored-client connects under way, by key
	// and connection string; see GetOrCreateClientWithConnString.
	inflight map[string]*clientCall

	// lastFlightGen numbers the connects started for inflight, so that
	// runClientCall can tell an older connect from a newer one.
	lastFlightGen uint64

	// closeGen counts CloseAll calls, so that a connect which started
	// before one and finishes after it closes its client rather than
	// storing it.
	closeGen uint64
}

// clientCall is one monitored-client connect in flight. done closes
// once client and err are set. gen is the connect's number (see
// lastFlightGen) and closeGen the CloseAll count when it started.
// waiters counts the callers who joined it after it started; nothing
// in the manager depends on it, but it lets tests see that a caller
// has joined without waiting a fixed time.
type clientCall struct {
	done     chan struct{}
	client   *Client
	err      error
	gen      uint64
	closeGen uint64
	waiters  int
}

// NewClientManager creates a new client manager with database configuration
func NewClientManager(dbConfig *config.DatabaseConfig) *ClientManager {
	return &ClientManager{
		clients:  make(map[string]*Client),
		dbConfig: dbConfig,
		inflight: make(map[string]*clientCall),
	}
}

// SetMonitoredDialGuard sets the guard applied to every client made for
// a monitored connection, normally Datastore.GuardMonitoredConnConfig.
// Clients already open keep the guard they were made with.
func (cm *ClientManager) SetMonitoredDialGuard(guard DialGuard) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.monitoredGuard = guard
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
// This should be called on server shutdown. A monitored-client connect
// still in flight closes its client when it finishes rather than
// storing it.
func (cm *ClientManager) CloseAll() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.closeGen++
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
// This is a helper that builds the appropriate client key and connection string.
// A failed connect is held to the floor, so that it takes the same time
// to report whatever its cause (issue #850). The hold is measured from
// when this caller asked, not from when the connect it may share with
// other callers started, so that a caller who joins a connect late is
// held as long as one who started it. No lock is held during the
// connect or the hold; see GetOrCreateClientWithConnString.
func (cm *ClientManager) GetClientForSession(ctx context.Context, session *SessionInfo, connStr string) (*Client, error) {
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

	start := connectTiming.Load().Now()
	client, err := cm.GetOrCreateClientWithConnString(ctx, clientKey, connStr)
	var failed *failedConnectError
	if errors.As(err, &failed) {
		holdFailedConnect(ctx, connectTiming.Load().Now().Sub(start))
	}
	if err != nil {
		return nil, err
	}
	return client, nil
}

// GetOrCreateClientWithConnString returns a database client for the given key
// Creates a client using the provided connection string if one doesn't exist,
// opening its first connection with ctx's values.
//
// The connect and metadata load run without cm.mu held, so that a request
// for another key, or a cached client, never waits on this connect: if it
// did, its latency would follow this connect's raw outcome and give away
// what the floor hides (issue #850). Callers for the same key and
// connection string while a connect is in flight share it rather than
// connecting again. The shared connect is detached from every caller's
// cancellation, so that one caller giving up does not fail the others;
// it is still bounded by the connect deadline, and a caller whose ctx
// ends stops waiting for it.
func (cm *ClientManager) GetOrCreateClientWithConnString(ctx context.Context, key string, connStr string) (*Client, error) {
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if connStr == "" {
		return nil, fmt.Errorf("connection string is required")
	}

	flightKey := key + "\x00" + connStr
	cm.mu.Lock()
	if client, exists := cm.clients[key]; exists && client.GetDefaultConnection() == connStr {
		cm.mu.Unlock()
		return client, nil
	}
	call, inFlight := cm.inflight[flightKey]
	if inFlight {
		call.waiters++
	} else {
		cm.lastFlightGen++
		call = &clientCall{done: make(chan struct{}), gen: cm.lastFlightGen, closeGen: cm.closeGen}
		cm.inflight[flightKey] = call
		go cm.runClientCall(context.WithoutCancel(ctx), call, flightKey, key, connStr,
			cm.monitoredGuard, cm.dbConfig)
	}
	cm.mu.Unlock()

	select {
	case <-call.done:
		return call.client, call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// errClientManagerClosed is returned to the callers of a connect that
// finished after CloseAll; the client it made is closed, not stored.
var errClientManagerClosed = errors.New("client manager is closed")

// runClientCall performs the shared connect for call, stores the client
// on success and then releases the callers waiting on call.done.
//
// Connects for one key under different connection strings (say, before
// and after the stored connection is edited) can finish in either
// order. The client from the most recently started connect wins: an
// older connect finishing last neither replaces nor closes the newer
// client, which its callers may already be using, and its own callers
// are given that newer client instead.
func (cm *ClientManager) runClientCall(
	ctx context.Context,
	call *clientCall,
	flightKey, key, connStr string,
	guard DialGuard,
	dbConfig *config.DatabaseConfig,
) {
	client, err := connectMonitoredClient(ctx, connStr, guard, dbConfig)
	cm.finishClientCall(call, flightKey, key, client, err)
}

// finishClientCall installs the outcome of call's connect, by the rules
// in runClientCall, and releases its callers.
func (cm *ClientManager) finishClientCall(call *clientCall, flightKey, key string, client *Client, err error) {
	cm.mu.Lock()
	delete(cm.inflight, flightKey)
	if err == nil {
		existing, exists := cm.clients[key]
		switch {
		case cm.closeGen != call.closeGen:
			client.Close()
			client, err = nil, errClientManagerClosed
		case exists && existing.flightGen > call.gen:
			client.Close()
			client = existing
		default:
			// A client for another connection string under this key is
			// replaced only once its successor is ready.
			if exists {
				existing.Close()
			}
			client.flightGen = call.gen
			cm.clients[key] = client
		}
	}
	call.client, call.err = client, err
	cm.mu.Unlock()
	close(call.done)
}

// connectMonitoredClient makes, connects and loads the metadata of a
// client for a monitored connection. The connect runs under the connect
// deadline, and a failed connect is reported as a failedConnectError so
// that GetClientForSession holds it.
func connectMonitoredClient(
	ctx context.Context,
	connStr string,
	guard DialGuard,
	dbConfig *config.DatabaseConfig,
) (*Client, error) {
	// Every connection string reaching here was built from a stored
	// monitored connection (see GetClientForSession), so the addresses it
	// connects to are checked.
	client := NewClientWithConnectionString(connStr, dbConfig)
	client.dialGuard = guard
	if client.dialGuard == nil {
		client.dialGuard = (*Datastore)(nil).GuardMonitoredConnConfig
	}
	if _, err := boundedConnect(ctx, client.connect); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", &failedConnectError{err: err})
	}

	if err := LoadMonitoredMetadata(ctx, client, connStr); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}
	return client, nil
}

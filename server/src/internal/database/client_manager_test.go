/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package database

import (
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

func TestNewClientManager(t *testing.T) {
	// Test with nil config
	cm := NewClientManager(nil)
	if cm == nil {
		t.Fatal("Expected non-nil ClientManager")
	}
	defer cm.CloseAll()

	if cm.GetClientCount() != 0 {
		t.Error("Expected 0 clients for new manager")
	}

	// Test GetDatabaseConfig returns nil when no config
	cfg := cm.GetDatabaseConfig()
	if cfg != nil {
		t.Error("Expected nil database config")
	}
}

func TestNewClientManager_WithConfig(t *testing.T) {
	dbConfig := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		User:     "testuser",
	}

	cm := NewClientManager(dbConfig)
	if cm == nil {
		t.Fatal("Expected non-nil ClientManager")
	}
	defer cm.CloseAll()

	// Test GetDatabaseConfig returns the config
	cfg := cm.GetDatabaseConfig()
	if cfg == nil {
		t.Fatal("Expected database config to be set")
	}
	if cfg.Host != "localhost" {
		t.Errorf("Expected host 'localhost', got %q", cfg.Host)
	}
}

func TestClientManager_GetOrCreateClient_NoConfig(t *testing.T) {
	cm := NewClientManager(nil)
	defer cm.CloseAll()

	// Should error when no database configured
	_, err := cm.GetOrCreateClient("test-token", true)
	if err == nil {
		t.Error("Expected error when no database configured")
	}
}

func TestClientManager_GetOrCreateClient_AutoConnectFalse(t *testing.T) {
	dbConfig := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		User:     "testuser",
	}

	cm := NewClientManager(dbConfig)
	defer cm.CloseAll()

	// Should error when autoConnect is false and no client exists
	_, err := cm.GetOrCreateClient("test-token", false)
	if err == nil {
		t.Error("Expected error when autoConnect is false and no client exists")
	}
}

func TestClientManager_SetClient(t *testing.T) {
	cm := NewClientManager(nil)
	defer cm.CloseAll()

	// Create a dummy client
	client := NewClient(nil)

	// Set client
	err := cm.SetClient("test-key", client)
	if err != nil {
		t.Fatalf("Failed to set client: %v", err)
	}

	if cm.GetClientCount() != 1 {
		t.Errorf("Expected 1 client, got %d", cm.GetClientCount())
	}

	// Test empty key error
	err = cm.SetClient("", client)
	if err == nil {
		t.Error("Expected error for empty key")
	}

	// Test nil client error
	err = cm.SetClient("another-key", nil)
	if err == nil {
		t.Error("Expected error for nil client")
	}
}

func TestClientManager_RemoveClient(t *testing.T) {
	cm := NewClientManager(nil)
	defer cm.CloseAll()

	// Create and add a client
	client := NewClient(nil)
	_ = cm.SetClient("test-key", client)

	if cm.GetClientCount() != 1 {
		t.Fatalf("Expected 1 client, got %d", cm.GetClientCount())
	}

	// Remove the client
	err := cm.RemoveClient("test-key")
	if err != nil {
		t.Fatalf("Failed to remove client: %v", err)
	}

	if cm.GetClientCount() != 0 {
		t.Errorf("Expected 0 clients after removal, got %d", cm.GetClientCount())
	}

	// Remove non-existent client (should not error)
	err = cm.RemoveClient("non-existent")
	if err != nil {
		t.Errorf("Unexpected error removing non-existent client: %v", err)
	}
}

func TestClientManager_RemoveClients(t *testing.T) {
	cm := NewClientManager(nil)
	defer cm.CloseAll()

	// Create and add multiple clients
	client1 := NewClient(nil)
	client2 := NewClient(nil)
	client3 := NewClient(nil)
	_ = cm.SetClient("key1", client1)
	_ = cm.SetClient("key2", client2)
	_ = cm.SetClient("key3", client3)

	if cm.GetClientCount() != 3 {
		t.Fatalf("Expected 3 clients, got %d", cm.GetClientCount())
	}

	// Remove two clients
	err := cm.RemoveClients([]string{"key1", "key3"})
	if err != nil {
		t.Fatalf("Failed to remove clients: %v", err)
	}

	if cm.GetClientCount() != 1 {
		t.Errorf("Expected 1 client after removal, got %d", cm.GetClientCount())
	}
}

func TestClientManager_CloseAll(t *testing.T) {
	cm := NewClientManager(nil)

	// Add some clients
	client1 := NewClient(nil)
	client2 := NewClient(nil)
	_ = cm.SetClient("key1", client1)
	_ = cm.SetClient("key2", client2)

	if cm.GetClientCount() != 2 {
		t.Fatalf("Expected 2 clients, got %d", cm.GetClientCount())
	}

	// Close all
	err := cm.CloseAll()
	if err != nil {
		t.Fatalf("Failed to close all: %v", err)
	}

	if cm.GetClientCount() != 0 {
		t.Errorf("Expected 0 clients after CloseAll, got %d", cm.GetClientCount())
	}
}

func TestClientManager_UpdateDatabaseConfig(t *testing.T) {
	cm := NewClientManager(nil)
	defer cm.CloseAll()

	if cm.GetDatabaseConfig() != nil {
		t.Error("Expected nil initial config")
	}

	// Update config
	newConfig := &config.DatabaseConfig{
		Host:     "newhost",
		Port:     5433,
		Database: "newdb",
		User:     "newuser",
	}
	cm.UpdateDatabaseConfig(newConfig)

	cfg := cm.GetDatabaseConfig()
	if cfg == nil {
		t.Fatal("Expected database config to be set")
	}
	if cfg.Host != "newhost" {
		t.Errorf("Expected host 'newhost', got %q", cfg.Host)
	}

	// Update to nil
	cm.UpdateDatabaseConfig(nil)
	if cm.GetDatabaseConfig() != nil {
		t.Error("Expected nil config after update")
	}
}

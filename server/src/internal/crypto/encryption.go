/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	pkgcrypto "github.com/pgedge/ai-workbench/pkg/crypto"
)

const (
	// KeySize is the size of the encryption key in bytes (256 bits)
	KeySize = 32
)

// EncryptionKey represents an AES-256 encryption key
type EncryptionKey struct {
	key []byte
}

// Package-level function variables wrap external dependencies so
// tests can swap them to exercise otherwise-unreachable error paths.
// Production callers see the standard library / pkgcrypto behavior.
var (
	randRead   = io.ReadFull
	encryptGCM = pkgcrypto.EncryptGCM
)

// GenerateKey creates a new random 256-bit encryption key
func GenerateKey() (*EncryptionKey, error) {
	key := make([]byte, KeySize)
	if _, err := randRead(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}
	return &EncryptionKey{key: key}, nil
}

// LoadKeyFromFile loads an encryption key from a file
func LoadKeyFromFile(path string) (*EncryptionKey, error) {
	// Check file permissions before loading
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat key file: %w", err)
	}

	// Verify file has 0600 permissions (owner read/write only)
	mode := fileInfo.Mode().Perm()
	if mode != 0600 {
		return nil, fmt.Errorf("insecure permissions on key file %s: %04o (expected 0600). Please run: chmod 600 %s", path, mode, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	// Decode base64
	key, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}

	if len(key) != KeySize {
		return nil, fmt.Errorf("invalid key size: expected %d bytes, got %d", KeySize, len(key))
	}

	return &EncryptionKey{key: key}, nil
}

// SaveToFile saves the encryption key to a file with restricted permissions
func (k *EncryptionKey) SaveToFile(path string) error {
	// Encode key as base64
	encoded := base64.StdEncoding.EncodeToString(k.key)

	// Write with restrictive permissions (owner read/write only)
	if err := os.WriteFile(path, []byte(encoded), 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	return nil
}

// Encrypt encrypts plaintext using AES-256-GCM
// Returns base64-encoded ciphertext with nonce prepended
func (k *EncryptionKey) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	nonceCiphertext, err := encryptGCM(k.key, []byte(plaintext))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(nonceCiphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext using AES-256-GCM
func (k *EncryptionKey) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	plaintext, err := pkgcrypto.DecryptGCM(k.key, data)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package notifications

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	pkgcrypto "github.com/pgedge/ai-workbench/pkg/crypto"
)

// secretManager implements SecretManager using AES-256-GCM
type secretManager struct {
	key []byte // 32-byte key for AES-256
}

// NewSecretManager creates a new SecretManager with the given 32-byte key
func NewSecretManager(key []byte) (SecretManager, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(key))
	}
	return &secretManager{key: key}, nil
}

// LoadSecretKey loads a secret key from a file.
// The file should contain a hex-encoded 32-byte key (64 hex characters).
// The file must have 0600 permissions (owner read/write only).
func LoadSecretKey(path string) ([]byte, error) {
	// Check file permissions before loading
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat secret key file: %w", err)
	}

	mode := fileInfo.Mode().Perm()
	if mode != 0600 {
		return nil, fmt.Errorf(
			"insecure permissions on secret key file %s: %04o (expected 0600). "+
				"Please run: chmod 600 %s", path, mode, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret key file: %w", err)
	}

	// Trim whitespace (newlines, spaces, etc.)
	hexKey := strings.TrimSpace(string(data))

	// Decode hex to bytes
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex key: %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf(
			"invalid key length: expected 32 bytes (64 hex characters), got %d bytes",
			len(key),
		)
	}

	return key, nil
}

// Encrypt implements SecretManager.Encrypt using AES-256-GCM.
// Returns base64-encoded ciphertext (nonce prepended to ciphertext).
func (s *secretManager) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	nonceCiphertext, err := pkgcrypto.EncryptGCM(s.key, []byte(plaintext))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(nonceCiphertext), nil
}

// Decrypt implements SecretManager.Decrypt.
// Expects base64-encoded ciphertext with prepended nonce.
func (s *secretManager) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 ciphertext: %w", err)
	}

	plaintext, err := pkgcrypto.DecryptGCM(s.key, data)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

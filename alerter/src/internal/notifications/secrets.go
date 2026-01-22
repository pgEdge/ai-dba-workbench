/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package notifications

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
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
func LoadSecretKey(path string) ([]byte, error) {
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

	// Create AES cipher with key
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal plaintext with nonce (prepends nonce to ciphertext)
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Return base64(nonce + ciphertext)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt implements SecretManager.Decrypt.
// Expects base64-encoded ciphertext with prepended nonce.
func (s *secretManager) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Base64 decode
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 ciphertext: %w", err)
	}

	// Create AES cipher with key
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce and ciphertext
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]

	// Open/decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	// Return plaintext string
	return string(plaintext), nil
}

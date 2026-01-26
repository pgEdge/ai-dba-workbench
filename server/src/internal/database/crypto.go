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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// saltSize is the size of the random salt in bytes (128 bits)
	saltSize = 16

	// pbkdf2Iterations is the number of PBKDF2 iterations for key derivation
	pbkdf2Iterations = 100000
)

// deriveKey derives a 32-byte AES key from the server secret and salt
// using PBKDF2 with SHA256 and 100,000 iterations for brute-force resistance.
func deriveKey(serverSecret string, salt []byte) []byte {
	// Use PBKDF2 with SHA256 for secure key derivation
	return pbkdf2.Key(
		[]byte(serverSecret),
		salt,
		pbkdf2Iterations,
		32,
		sha256.New,
	)
}

// deriveKeyFromString is kept for backward compatibility with existing encrypted data
// that used username as salt. New encryptions use random salt.
// Deprecated: Use deriveKey with random salt instead.
func deriveKeyFromString(serverSecret string, salt string) []byte {
	return pbkdf2.Key(
		[]byte(serverSecret),
		[]byte(salt),
		pbkdf2Iterations,
		32,
		sha256.New,
	)
}

// EncryptPassword encrypts a password using AES-256-GCM with a random salt.
// The key is derived from the server secret and a cryptographically random salt.
// The salt is prepended to the ciphertext for storage.
// The username parameter is ignored (kept for API compatibility) - random salt is used instead.
func EncryptPassword(password string, serverSecret string, _ string) (string, error) {
	if serverSecret == "" {
		return "", fmt.Errorf("server secret is required for encryption")
	}

	// Generate cryptographically random salt (much better than username)
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key from server secret and random salt
	key := deriveKey(serverSecret, salt)

	// Create cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the password
	ciphertext := gcm.Seal(nonce, nonce, []byte(password), nil)

	// Prepend salt to ciphertext: [salt (16 bytes)][nonce + ciphertext]
	result := make([]byte, saltSize+len(ciphertext))
	copy(result[:saltSize], salt)
	copy(result[saltSize:], ciphertext)

	// Encode to base64 for storage
	return base64.StdEncoding.EncodeToString(result), nil
}

// DecryptPassword decrypts a password using AES-256-GCM.
// It supports two formats:
// 1. New format: [salt (16 bytes)][nonce][ciphertext] - uses random salt
// 2. Legacy format: [nonce][ciphertext] - uses username as salt (deprecated)
// The function automatically detects the format based on whether decryption succeeds.
func DecryptPassword(encryptedPassword string, serverSecret string, username string) (string, error) {
	if serverSecret == "" {
		return "", fmt.Errorf("server secret is required for decryption")
	}

	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(encryptedPassword)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// Try new format first (with random salt prepended)
	if len(data) > saltSize {
		plaintext, err := decryptWithSalt(data, serverSecret)
		if err == nil {
			return plaintext, nil
		}
		// If decryption failed, fall through to try legacy format
	}

	// Fall back to legacy format (username as salt)
	return decryptLegacy(data, serverSecret, username)
}

// decryptWithSalt decrypts data in the new format: [salt (16 bytes)][nonce][ciphertext]
func decryptWithSalt(data []byte, serverSecret string) (string, error) {
	if len(data) < saltSize {
		return "", fmt.Errorf("data too short for salt")
	}

	// Extract salt
	salt := data[:saltSize]
	ciphertext := data[saltSize:]

	// Derive key from server secret and extracted salt
	key := deriveKey(serverSecret, salt)

	// Create cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// decryptLegacy decrypts data in the legacy format using username as salt.
// Deprecated: This exists only for backward compatibility with existing encrypted data.
func decryptLegacy(data []byte, serverSecret string, username string) (string, error) {
	// Derive key from server secret and username (legacy approach)
	key := deriveKeyFromString(serverSecret, username)

	// Create cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

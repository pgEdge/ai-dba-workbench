/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
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

// deriveKey derives a 32-byte AES key from the server secret and optional salt
// using PBKDF2 with SHA256 and 100,000 iterations for brute-force resistance.
//
// BREAKING CHANGE: This function was updated from a simple SHA256 hash to PBKDF2.
// Existing encrypted passwords created with the previous implementation will no
// longer decrypt correctly and must be re-encrypted.
func deriveKey(serverSecret string, salt string) []byte {
	// Use PBKDF2 with SHA256, 100,000 iterations for secure key derivation
	return pbkdf2.Key(
		[]byte(serverSecret),
		[]byte(salt),
		100000,
		32,
		sha256.New,
	)
}

// EncryptPassword encrypts a password using AES-256-GCM
// The key is derived from the server secret and username
func EncryptPassword(password string, serverSecret string, username string) (string, error) {
	if serverSecret == "" {
		return "", fmt.Errorf("server secret is required for encryption")
	}

	// Derive key from server secret and username
	key := deriveKey(serverSecret, username)

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

	// Encode to base64 for storage
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptPassword decrypts a password using AES-256-GCM.
// Supports two formats:
// 1. New format (server): [salt (16 bytes)][nonce][ciphertext] - random salt
// 2. Legacy format (collector): [nonce][ciphertext] - username as salt
func DecryptPassword(encryptedPassword string, serverSecret string, username string) (string, error) {
	if serverSecret == "" {
		return "", fmt.Errorf("server secret is required for decryption")
	}

	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(encryptedPassword)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// Try new format first (server uses 16-byte random salt prepended)
	const saltSize = 16
	if len(data) > saltSize+12 { // salt + minimum nonce size
		if plaintext, err := decryptWithRandomSalt(data, serverSecret, saltSize); err == nil {
			return plaintext, nil
		}
	}

	// Fall back to legacy format (username as salt)
	return decryptWithUsernameSalt(data, serverSecret, username)
}

// decryptWithRandomSalt decrypts data in the server's format: [salt][nonce][ciphertext]
func decryptWithRandomSalt(data []byte, serverSecret string, saltSize int) (string, error) {
	salt := data[:saltSize]
	ciphertext := data[saltSize:]

	// Derive key using the extracted salt (as bytes, not string)
	key := pbkdf2.Key([]byte(serverSecret), salt, 100000, 32, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// decryptWithUsernameSalt decrypts data in the legacy format: [nonce][ciphertext]
func decryptWithUsernameSalt(data []byte, serverSecret string, username string) (string, error) {
	key := deriveKey(serverSecret, username)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

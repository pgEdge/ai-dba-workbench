/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// deriveKey derives a 32-byte AES key from the server secret and optional salt
func deriveKey(serverSecret string, salt string) []byte {
	// Combine server secret with salt
	combined := serverSecret + salt
	hash := sha256.Sum256([]byte(combined))
	return hash[:]
}

// DecryptPassword decrypts a password using AES-256-GCM
// The key is derived from the server secret and username
func DecryptPassword(encryptedPassword string, serverSecret string, username string) (string, error) {
	if serverSecret == "" {
		return "", fmt.Errorf("server secret is required for decryption")
	}

	// Derive key from server secret and username
	key := deriveKey(serverSecret, username)

	// Decode from base64
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedPassword)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

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

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

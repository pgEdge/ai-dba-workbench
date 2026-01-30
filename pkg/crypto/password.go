/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package crypto provides cryptographic utilities for password encryption.
package crypto

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

    // gcmNonceSize is the standard nonce size for AES-GCM in bytes
    gcmNonceSize = 12

    // pbkdf2Iterations is the number of PBKDF2 iterations for key derivation.
    // This is key derivation for encryption (not password hashing); an attacker
    // needs both the ciphertext and the server secret to attempt brute force,
    // so 100,000 iterations provides adequate protection for this threat model.
    pbkdf2Iterations = 100000

    // keySize is the size of the derived AES key in bytes (256 bits)
    keySize = 32
)

// deriveKey derives a 32-byte AES key from the server secret and salt
// using PBKDF2 with SHA256 and 100,000 iterations for brute-force resistance.
func deriveKey(serverSecret string, salt []byte) []byte {
    return pbkdf2.Key(
        []byte(serverSecret),
        salt,
        pbkdf2Iterations,
        keySize,
        sha256.New,
    )
}

// EncryptPassword encrypts a password using AES-256-GCM with a random salt.
// The key is derived from the server secret and a cryptographically random salt.
// The salt is prepended to the ciphertext for storage.
//
// Format: base64([salt (16 bytes)][nonce (12 bytes)][ciphertext + auth tag])
func EncryptPassword(password string, serverSecret string) (string, error) {
    if serverSecret == "" {
        return "", fmt.Errorf("server secret is required for encryption")
    }

    // Generate cryptographically random salt
    salt := make([]byte, saltSize)
    if _, err := io.ReadFull(rand.Reader, salt); err != nil {
        return "", fmt.Errorf("failed to generate salt: %w", err)
    }

    // Derive key from server secret and random salt
    key := deriveKey(serverSecret, salt)
    defer func() {
        for i := range key {
            key[i] = 0
        }
    }()

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
// Expects format: base64([salt (16 bytes)][nonce (12 bytes)][ciphertext + auth tag])
func DecryptPassword(encryptedPassword string, serverSecret string) (string, error) {
    if serverSecret == "" {
        return "", fmt.Errorf("failed to decrypt password: %w", fmt.Errorf("server secret is required"))
    }

    // Decode from base64
    data, err := base64.StdEncoding.DecodeString(encryptedPassword)
    if err != nil {
        return "", fmt.Errorf("failed to decrypt password: %w", err)
    }

    // Minimum size: salt (16) + nonce (12) + at least some ciphertext
    if len(data) < saltSize+gcmNonceSize {
        return "", fmt.Errorf("failed to decrypt password: %w", fmt.Errorf("encrypted data too short"))
    }

    // Extract salt
    salt := data[:saltSize]
    ciphertext := data[saltSize:]

    // Derive key from server secret and extracted salt
    key := deriveKey(serverSecret, salt)
    defer func() {
        for i := range key {
            key[i] = 0
        }
    }()

    // Create cipher block
    block, err := aes.NewCipher(key)
    if err != nil {
        return "", fmt.Errorf("failed to decrypt password: %w", err)
    }

    // Create GCM mode
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("failed to decrypt password: %w", err)
    }

    // Extract nonce
    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return "", fmt.Errorf("failed to decrypt password: %w", fmt.Errorf("ciphertext too short"))
    }

    nonce, ciphertextBytes := ciphertext[:nonceSize], ciphertext[nonceSize:]

    // Decrypt
    plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
    if err != nil {
        return "", fmt.Errorf("failed to decrypt password: %w", err)
    }

    return string(plaintext), nil
}

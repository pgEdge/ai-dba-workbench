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
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSecretManager_ValidKey(t *testing.T) {
	key := make([]byte, 32) // 256-bit key
	for i := range key {
		key[i] = byte(i)
	}

	sm, err := NewSecretManager(key)
	if err != nil {
		t.Errorf("NewSecretManager() unexpected error: %v", err)
	}

	if sm == nil {
		t.Error("NewSecretManager() returned nil")
	}
}

func TestNewSecretManager_InvalidKeyLength(t *testing.T) {
	tests := []struct {
		name    string
		keyLen  int
		wantErr bool
	}{
		{"too short - 16 bytes", 16, true},
		{"too short - 31 bytes", 31, true},
		{"correct - 32 bytes", 32, false},
		{"too long - 33 bytes", 33, true},
		{"too long - 64 bytes", 64, true},
		{"empty", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keyLen)
			_, err := NewSecretManager(key)

			if tt.wantErr {
				if err == nil {
					t.Error("NewSecretManager() expected error, got nil")
				} else if !strings.Contains(err.Error(), "32 bytes") {
					t.Errorf("Error should mention 32 bytes: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("NewSecretManager() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestSecretManager_EncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	sm, err := NewSecretManager(key)
	if err != nil {
		t.Fatalf("NewSecretManager() error: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"simple text", "hello world"},
		{"password", "my-secret-password-123!"},
		{"url", "https://hooks.slack.com/services/T00/B00/XXX"},
		{"unicode", "Hello, world"},
		{"special chars", "p@$$w0rd!#$%^&*()"},
		{"long text", strings.Repeat("a", 1000)},
		{"json", `{"key": "value", "nested": {"a": 1}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := sm.Encrypt(tt.plaintext)
			if err != nil {
				t.Errorf("Encrypt() error: %v", err)
				return
			}

			// Ciphertext should be different from plaintext
			if ciphertext == tt.plaintext {
				t.Error("Encrypt() ciphertext should differ from plaintext")
			}

			// Decrypt
			decrypted, err := sm.Decrypt(ciphertext)
			if err != nil {
				t.Errorf("Decrypt() error: %v", err)
				return
			}

			if decrypted != tt.plaintext {
				t.Errorf("Decrypt() = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestSecretManager_EncryptEmpty(t *testing.T) {
	key := make([]byte, 32)
	sm, err := NewSecretManager(key)
	if err != nil {
		t.Fatalf("NewSecretManager() error: %v", err)
	}

	ciphertext, err := sm.Encrypt("")
	if err != nil {
		t.Errorf("Encrypt() unexpected error for empty string: %v", err)
	}

	if ciphertext != "" {
		t.Errorf("Encrypt() empty string should return empty, got %q", ciphertext)
	}
}

func TestSecretManager_DecryptEmpty(t *testing.T) {
	key := make([]byte, 32)
	sm, err := NewSecretManager(key)
	if err != nil {
		t.Fatalf("NewSecretManager() error: %v", err)
	}

	plaintext, err := sm.Decrypt("")
	if err != nil {
		t.Errorf("Decrypt() unexpected error for empty string: %v", err)
	}

	if plaintext != "" {
		t.Errorf("Decrypt() empty string should return empty, got %q", plaintext)
	}
}

func TestSecretManager_EncryptProducesUniqueResults(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	sm, err := NewSecretManager(key)
	if err != nil {
		t.Fatalf("NewSecretManager() error: %v", err)
	}

	plaintext := "same-text"

	// Encrypt the same text multiple times
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		ciphertext, err := sm.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt() error: %v", err)
		}

		if results[ciphertext] {
			t.Error("Encrypt() should produce unique ciphertexts due to random nonce")
		}
		results[ciphertext] = true

		// All should decrypt to the same value
		decrypted, err := sm.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("Decrypt() error: %v", err)
		}
		if decrypted != plaintext {
			t.Errorf("Decrypt() = %q, want %q", decrypted, plaintext)
		}
	}
}

func TestSecretManager_DecryptInvalidBase64(t *testing.T) {
	key := make([]byte, 32)
	sm, smErr := NewSecretManager(key)
	if smErr != nil {
		t.Fatalf("NewSecretManager() error: %v", smErr)
	}

	_, err := sm.Decrypt("not-valid-base64!!!")
	if err == nil {
		t.Error("Decrypt() expected error for invalid base64")
	}

	if !strings.Contains(err.Error(), "decode base64") {
		t.Errorf("Error should mention base64 decoding: %v", err)
	}
}

func TestSecretManager_DecryptTooShort(t *testing.T) {
	key := make([]byte, 32)
	sm, smErr := NewSecretManager(key)
	if smErr != nil {
		t.Fatalf("NewSecretManager() error: %v", smErr)
	}

	// Base64 encoded data that's too short for nonce
	shortData := "YWJj" // "abc" in base64

	_, err := sm.Decrypt(shortData)
	if err == nil {
		t.Error("Decrypt() expected error for data too short")
	}

	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("Error should mention ciphertext too short: %v", err)
	}
}

func TestSecretManager_DecryptTamperedData(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	sm, smErr := NewSecretManager(key)
	if smErr != nil {
		t.Fatalf("NewSecretManager() error: %v", smErr)
	}

	// Encrypt valid data
	ciphertext, err := sm.Encrypt("secret data")
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	// Tamper with the ciphertext (change a character)
	runes := []rune(ciphertext)
	if len(runes) > 10 {
		if runes[10] == 'A' {
			runes[10] = 'B'
		} else {
			runes[10] = 'A'
		}
	}
	tampered := string(runes)

	// Decryption should fail with authentication error
	_, err = sm.Decrypt(tampered)
	if err == nil {
		t.Error("Decrypt() expected error for tampered data")
	}
}

func TestSecretManager_DifferentKeysCannotDecrypt(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i)
		key2[i] = byte(i + 1) // Different key
	}

	sm1, err1 := NewSecretManager(key1)
	if err1 != nil {
		t.Fatalf("NewSecretManager() error: %v", err1)
	}
	sm2, err2 := NewSecretManager(key2)
	if err2 != nil {
		t.Fatalf("NewSecretManager() error: %v", err2)
	}

	// Encrypt with key1
	ciphertext, err := sm1.Encrypt("secret message")
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	// Try to decrypt with key2
	_, err = sm2.Decrypt(ciphertext)
	if err == nil {
		t.Error("Decrypt() with wrong key should fail")
	}
}

func TestLoadSecretKey_ValidFile(t *testing.T) {
	// Create a temporary file with a valid hex key
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	hexKey := hex.EncodeToString(key)

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "secret.key")

	err := os.WriteFile(keyFile, []byte(hexKey), 0600)
	if err != nil {
		t.Fatalf("Failed to write test key file: %v", err)
	}

	loadedKey, err := LoadSecretKey(keyFile)
	if err != nil {
		t.Errorf("LoadSecretKey() unexpected error: %v", err)
	}

	if len(loadedKey) != 32 {
		t.Errorf("LoadSecretKey() returned key of length %d, want 32", len(loadedKey))
	}

	for i := range loadedKey {
		if loadedKey[i] != key[i] {
			t.Errorf("LoadSecretKey() key mismatch at position %d", i)
			break
		}
	}
}

func TestLoadSecretKey_WithWhitespace(t *testing.T) {
	key := make([]byte, 32)
	hexKey := hex.EncodeToString(key)

	// Add whitespace (newlines, spaces)
	contentWithWhitespace := "  " + hexKey + "\n\n"

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "secret.key")

	err := os.WriteFile(keyFile, []byte(contentWithWhitespace), 0600)
	if err != nil {
		t.Fatalf("Failed to write test key file: %v", err)
	}

	loadedKey, err := LoadSecretKey(keyFile)
	if err != nil {
		t.Errorf("LoadSecretKey() unexpected error: %v", err)
	}

	if len(loadedKey) != 32 {
		t.Errorf("LoadSecretKey() returned key of length %d, want 32", len(loadedKey))
	}
}

func TestLoadSecretKey_FileNotFound(t *testing.T) {
	_, err := LoadSecretKey("/nonexistent/path/to/key")
	if err == nil {
		t.Error("LoadSecretKey() expected error for nonexistent file")
	}

	if !strings.Contains(err.Error(), "read secret key file") {
		t.Errorf("Error should mention reading file: %v", err)
	}
}

func TestLoadSecretKey_InvalidHex(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "secret.key")

	err := os.WriteFile(keyFile, []byte("not-valid-hex-zzz"), 0600)
	if err != nil {
		t.Fatalf("Failed to write test key file: %v", err)
	}

	_, err = LoadSecretKey(keyFile)
	if err == nil {
		t.Error("LoadSecretKey() expected error for invalid hex")
	}

	if !strings.Contains(err.Error(), "decode hex") {
		t.Errorf("Error should mention hex decoding: %v", err)
	}
}

func TestLoadSecretKey_WrongLength(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "secret.key")

	// 16 bytes = 32 hex chars (too short)
	shortKey := strings.Repeat("ab", 16)
	err := os.WriteFile(keyFile, []byte(shortKey), 0600)
	if err != nil {
		t.Fatalf("Failed to write test key file: %v", err)
	}

	_, err = LoadSecretKey(keyFile)
	if err == nil {
		t.Error("LoadSecretKey() expected error for wrong key length")
	}

	if !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("Error should mention 32 bytes: %v", err)
	}
}

func TestLoadSecretKey_TooLong(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "secret.key")

	// 64 bytes = 128 hex chars (too long)
	longKey := strings.Repeat("ab", 64)
	err := os.WriteFile(keyFile, []byte(longKey), 0600)
	if err != nil {
		t.Fatalf("Failed to write test key file: %v", err)
	}

	_, err = LoadSecretKey(keyFile)
	if err == nil {
		t.Error("LoadSecretKey() expected error for key too long")
	}

	if !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("Error should mention 32 bytes: %v", err)
	}
}

func TestSecretManager_RealWorldScenario(t *testing.T) {
	// Test a realistic scenario: storing and retrieving webhook URLs
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i * 2)
	}
	sm, smErr := NewSecretManager(key)
	if smErr != nil {
		t.Fatalf("NewSecretManager() error: %v", smErr)
	}

	secrets := []string{
		"https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
		"smtp_password_123!@#",
		"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"user:password",
	}

	for _, secret := range secrets {
		// Encrypt
		encrypted, err := sm.Encrypt(secret)
		if err != nil {
			t.Errorf("Encrypt(%q) error: %v", secret[:20], err)
			continue
		}

		// Verify encrypted is base64
		if encrypted == secret {
			t.Errorf("Encrypted should differ from plaintext")
		}

		// Decrypt
		decrypted, err := sm.Decrypt(encrypted)
		if err != nil {
			t.Errorf("Decrypt() error: %v", err)
			continue
		}

		if decrypted != secret {
			t.Errorf("Round-trip failed: got %q, want %q", decrypted[:20], secret[:20])
		}
	}
}

func TestSecretManager_ConcurrentAccess(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	sm, smErr := NewSecretManager(key)
	if smErr != nil {
		t.Fatalf("NewSecretManager() error: %v", smErr)
	}

	// Run multiple goroutines encrypting/decrypting
	done := make(chan bool)
	for i := range 10 {
		go func(id int) {
			for range 100 {
				plaintext := "secret-data-" + string(rune('A'+id))

				encrypted, err := sm.Encrypt(plaintext)
				if err != nil {
					t.Errorf("Goroutine %d: Encrypt() error: %v", id, err)
					continue
				}

				decrypted, err := sm.Decrypt(encrypted)
				if err != nil {
					t.Errorf("Goroutine %d: Decrypt() error: %v", id, err)
					continue
				}

				if decrypted != plaintext {
					t.Errorf("Goroutine %d: round-trip failed", id)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range 10 {
		<-done
	}
}

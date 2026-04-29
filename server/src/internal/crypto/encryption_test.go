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
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if key == nil {
		t.Fatal("Expected non-nil key")
	}

	if len(key.key) != KeySize {
		t.Errorf("Expected key size %d, got %d", KeySize, len(key.key))
	}

	// Generate another key and verify they're different
	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if string(key.key) == string(key2.key) {
		t.Error("Expected different keys, got identical keys")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	testCases := []struct {
		name      string
		plaintext string
	}{
		{"simple password", "mypassword1234"},
		{"complex password", "P@ssw0rd!@#$%^&*()"},
		{"unicode password", "пароль密码"},
		{"empty string", ""},
		{"long password", "this is a very long password with many characters to test larger plaintexts"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Encrypt
			ciphertext, err := key.Encrypt(tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			if tc.plaintext == "" {
				if ciphertext != "" {
					t.Error("Expected empty ciphertext for empty plaintext")
				}
				return
			}

			if ciphertext == "" {
				t.Error("Expected non-empty ciphertext")
			}

			// Ciphertext should be different from plaintext
			if ciphertext == tc.plaintext {
				t.Error("Ciphertext should not match plaintext")
			}

			// Decrypt
			decrypted, err := key.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			if decrypted != tc.plaintext {
				t.Errorf("Decryption mismatch: expected %q, got %q", tc.plaintext, decrypted)
			}
		})
	}
}

func TestEncryptionNonDeterministic(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	plaintext := "test password"

	// Encrypt the same plaintext twice
	ciphertext1, err := key.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	ciphertext2, err := key.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Ciphertexts should be different due to random nonces
	if ciphertext1 == ciphertext2 {
		t.Error("Expected different ciphertexts for same plaintext (nonce should be random)")
	}

	// But both should decrypt to the same plaintext
	decrypted1, err := key.Decrypt(ciphertext1)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	decrypted2, err := key.Decrypt(ciphertext2)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted1 != plaintext || decrypted2 != plaintext {
		t.Error("Both ciphertexts should decrypt to the same plaintext")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	plaintext := "secret password"

	// Encrypt with key1
	ciphertext, err := key1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Try to decrypt with key2 (should fail)
	_, err = key2.Decrypt(ciphertext)
	if err == nil {
		t.Error("Expected decryption to fail with wrong key")
	}
}

func TestSaveAndLoadKey(t *testing.T) {
	// Create temporary directory for test
	tmpDir, err := os.MkdirTemp("", "pgedge-crypto-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	keyPath := filepath.Join(tmpDir, "test.key")

	// Generate and save key
	originalKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if err := originalKey.SaveToFile(keyPath); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Verify file exists and has correct permissions
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("Failed to stat key file: %v", err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("Expected file permissions 0600, got %o", info.Mode().Perm())
	}

	// Load key from file
	loadedKey, err := LoadKeyFromFile(keyPath)
	if err != nil {
		t.Fatalf("LoadKeyFromFile failed: %v", err)
	}

	// Verify keys are identical
	if string(originalKey.key) != string(loadedKey.key) {
		t.Error("Loaded key does not match original key")
	}

	// Test encryption/decryption with loaded key
	plaintext := "test password"
	ciphertext, err := originalKey.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := loadedKey.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt with loaded key failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Expected %q, got %q", plaintext, decrypted)
	}
}

func TestLoadKeyFromInvalidFile(t *testing.T) {
	testCases := []struct {
		name     string
		content  string
		expected string
	}{
		{"nonexistent file", "", "failed to read key file"},
		{"invalid base64", "not-valid-base64!", "failed to decode key"},
		{"wrong size", "YWJjZGVm", "invalid key size"}, // "abcdef" in base64, too short
	}

	tmpDir, err := os.MkdirTemp("", "pgedge-crypto-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			keyPath := filepath.Join(tmpDir, tc.name+".key")

			if tc.content != "" {
				if err := os.WriteFile(keyPath, []byte(tc.content), 0600); err != nil {
					t.Fatalf("Failed to write test file: %v", err)
				}
			}

			_, err := LoadKeyFromFile(keyPath)
			if err == nil {
				t.Error("Expected error, got nil")
			}
		})
	}
}

func TestLoadKeyWithInsecurePermissions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pgedge-crypto-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	keyPath := filepath.Join(tmpDir, "insecure.key")

	// Generate a valid key file
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Save with correct permissions first
	if err := key.SaveToFile(keyPath); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Test various insecure permission modes
	insecurePermissions := []os.FileMode{
		0644, // world-readable
		0666, // world-readable and writable
		0640, // group-readable
		0660, // group-readable and writable
		0604, // world-readable
	}

	for _, perm := range insecurePermissions {
		t.Run(fmt.Sprintf("mode_%04o", perm), func(t *testing.T) {
			// Change to insecure permissions
			if err := os.Chmod(keyPath, perm); err != nil {
				t.Fatalf("Failed to change permissions: %v", err)
			}

			// Try to load - should fail
			_, err := LoadKeyFromFile(keyPath)
			if err == nil {
				t.Errorf("Expected error for permissions %04o, got nil", perm)
			}

			// Verify error message mentions permissions
			if err != nil && !strings.Contains(err.Error(), "insecure permissions") {
				t.Errorf("Expected 'insecure permissions' error, got: %v", err)
			}
		})
	}
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	testCases := []struct {
		name       string
		ciphertext string
	}{
		{"invalid base64", "not-valid-base64!"},
		{"too short", "YWJj"}, // "abc" in base64, too short for nonce
		{"corrupted", "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo="},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := key.Decrypt(tc.ciphertext)
			if err == nil {
				t.Error("Expected error for invalid ciphertext, got nil")
			}
		})
	}
}

// TestGenerateKeyRandFailure verifies that GenerateKey wraps and
// returns an error when the underlying random reader fails. This
// exercises the io.ReadFull error branch by swapping the package-level
// randRead hook.
func TestGenerateKeyRandFailure(t *testing.T) {
	original := randRead
	t.Cleanup(func() { randRead = original })

	wantErr := errors.New("simulated rand failure")
	randRead = func(_ io.Reader, _ []byte) (int, error) {
		return 0, wantErr
	}

	key, err := GenerateKey()
	if err == nil {
		t.Fatal("Expected error from GenerateKey, got nil")
	}
	if key != nil {
		t.Errorf("Expected nil key on failure, got %v", key)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Expected wrapped %q, got %v", wantErr, err)
	}
	if !strings.Contains(err.Error(), "failed to generate random key") {
		t.Errorf("Expected wrap prefix in error, got %v", err)
	}
}

// TestLoadKeyFromFileInvalidSize verifies that LoadKeyFromFile rejects
// a base64 payload that decodes to a byte count other than KeySize.
// The existing "wrong size" subtest in TestLoadKeyFromInvalidFile
// covers a 6-byte payload; this test uses a 16-byte payload to make
// the intent explicit and to keep the assertion specific to the
// invalid-size branch.
func TestLoadKeyFromFileInvalidSize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pgedge-crypto-invalid-size-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	keyPath := filepath.Join(tmpDir, "short.key")
	short := make([]byte, 16) // half the expected key size
	encoded := base64.StdEncoding.EncodeToString(short)
	if err := os.WriteFile(keyPath, []byte(encoded), 0600); err != nil {
		t.Fatalf("Failed to write key file: %v", err)
	}

	_, err = LoadKeyFromFile(keyPath)
	if err == nil {
		t.Fatal("Expected invalid key size error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid key size") {
		t.Errorf("Expected 'invalid key size' in error, got %v", err)
	}
}

// TestLoadKeyFromFileReadFailure verifies that LoadKeyFromFile
// surfaces an os.ReadFile error after a successful stat. We achieve a
// passing-stat / failing-read combination by creating a directory at
// the target path with mode 0600 — Stat reports the right permissions
// for a regular-file check, but ReadFile rejects directories.
func TestLoadKeyFromFileReadFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pgedge-crypto-read-fail-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	dirAsKey := filepath.Join(tmpDir, "dir.key")
	if err := os.Mkdir(dirAsKey, 0600); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	_, err = LoadKeyFromFile(dirAsKey)
	if err == nil {
		t.Fatal("Expected read error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read key file") {
		t.Errorf("Expected 'failed to read key file' in error, got %v", err)
	}
}

// TestSaveToFileWriteFailure verifies that SaveToFile wraps and
// returns errors from os.WriteFile. Writing into a path whose parent
// is a regular file (not a directory) reliably fails with ENOTDIR on
// Linux without requiring privileged operations.
func TestSaveToFileWriteFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pgedge-crypto-write-fail-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	parentFile := filepath.Join(tmpDir, "parent")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0600); err != nil {
		t.Fatalf("Failed to create parent file: %v", err)
	}

	// Using parentFile as a directory component yields ENOTDIR.
	target := filepath.Join(parentFile, "key")

	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	err = key.SaveToFile(target)
	if err == nil {
		t.Fatal("Expected write error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to write key file") {
		t.Errorf("Expected 'failed to write key file' in error, got %v", err)
	}
}

// TestDecryptEmptyCiphertext verifies the empty-input fast path for
// Decrypt: an empty ciphertext returns an empty plaintext without
// invoking base64 decoding or AES-GCM. This mirrors the matching
// short-circuit in Encrypt.
func TestDecryptEmptyCiphertext(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	plaintext, err := key.Decrypt("")
	if err != nil {
		t.Fatalf("Decrypt(\"\") returned error: %v", err)
	}
	if plaintext != "" {
		t.Errorf("Expected empty plaintext, got %q", plaintext)
	}
}

// TestEncryptGCMFailure verifies that Encrypt surfaces errors from
// the underlying GCM helper. We swap the package-level encryptGCM
// hook to inject a deterministic failure; the production code path
// continues to use pkgcrypto.EncryptGCM.
func TestEncryptGCMFailure(t *testing.T) {
	original := encryptGCM
	t.Cleanup(func() { encryptGCM = original })

	wantErr := errors.New("simulated GCM failure")
	encryptGCM = func(_ []byte, _ []byte) ([]byte, error) {
		return nil, wantErr
	}

	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	ciphertext, err := key.Encrypt("plaintext")
	if err == nil {
		t.Fatal("Expected error from Encrypt, got nil")
	}
	if ciphertext != "" {
		t.Errorf("Expected empty ciphertext on failure, got %q", ciphertext)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Expected error to wrap %q, got %v", wantErr, err)
	}
}

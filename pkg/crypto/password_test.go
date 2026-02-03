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
	"strings"
	"testing"
)

const testSecret = "test-server-secret-key"

func TestRoundTrip(t *testing.T) {
	password := "my-database-password"

	encrypted, err := EncryptPassword(password, testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	decrypted, err := DecryptPassword(encrypted, testSecret)
	if err != nil {
		t.Fatalf("DecryptPassword failed: %v", err)
	}

	if decrypted != password {
		t.Errorf("round-trip failed: got %q, want %q", decrypted, password)
	}
}

func TestEmptyPassword(t *testing.T) {
	encrypted, err := EncryptPassword("", testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword with empty password failed: %v", err)
	}

	decrypted, err := DecryptPassword(encrypted, testSecret)
	if err != nil {
		t.Fatalf("DecryptPassword with empty password failed: %v", err)
	}

	if decrypted != "" {
		t.Errorf("expected empty string, got %q", decrypted)
	}
}

func TestEmptyServerSecretRejection(t *testing.T) {
	_, err := EncryptPassword("password", "")
	if err == nil {
		t.Error("EncryptPassword should reject empty server secret")
	}

	_, err = DecryptPassword("dGVzdA==", "")
	if err == nil {
		t.Error("DecryptPassword should reject empty server secret")
	}
}

func TestDifferentCiphertexts(t *testing.T) {
	password := "same-password"

	enc1, err := EncryptPassword(password, testSecret)
	if err != nil {
		t.Fatalf("first encryption failed: %v", err)
	}

	enc2, err := EncryptPassword(password, testSecret)
	if err != nil {
		t.Fatalf("second encryption failed: %v", err)
	}

	if enc1 == enc2 {
		t.Error("encrypting the same password twice should produce different ciphertexts")
	}
}

func TestCorruptedBase64Ciphertext(t *testing.T) {
	encrypted, err := EncryptPassword("password", testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	// Corrupt the base64 string by replacing characters
	corrupted := "!!invalid-base64!!" + encrypted[18:]

	_, err = DecryptPassword(corrupted, testSecret)
	if err == nil {
		t.Error("DecryptPassword should fail on corrupted base64 input")
	}
}

func TestTamperedCiphertext(t *testing.T) {
	encrypted, err := EncryptPassword("password", testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	// Flip a byte in the ciphertext portion (after salt and nonce)
	idx := saltSize + gcmNonceSize + 1
	if idx < len(data) {
		data[idx] ^= 0xFF
	}

	tampered := base64.StdEncoding.EncodeToString(data)

	_, err = DecryptPassword(tampered, testSecret)
	if err == nil {
		t.Error("DecryptPassword should fail on tampered ciphertext")
	}
}

func TestTruncatedCiphertext(t *testing.T) {
	// Data shorter than salt + nonce minimum
	short := make([]byte, saltSize+gcmNonceSize-1)
	encoded := base64.StdEncoding.EncodeToString(short)

	_, err := DecryptPassword(encoded, testSecret)
	if err == nil {
		t.Error("DecryptPassword should fail on truncated ciphertext")
	}

	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("expected 'too short' in error, got: %v", err)
	}
}

func TestWrongServerSecret(t *testing.T) {
	encrypted, err := EncryptPassword("password", testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	_, err = DecryptPassword(encrypted, "wrong-secret")
	if err == nil {
		t.Error("DecryptPassword should fail with wrong server secret")
	}
}

func TestVariousPasswordLengths(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{"short", "ab"},
		{"medium", "a-medium-length-password-1234"},
		{"long", strings.Repeat("long-password-segment-", 50)},
		{"unicode", "p\u00e4ssw\u00f6rd-\u2603-\U0001F512"},
		{"special_chars", "p@$$w0rd!#%^&*(){}[]|\\:\";<>?,./~`"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encrypted, err := EncryptPassword(tc.password, testSecret)
			if err != nil {
				t.Fatalf("EncryptPassword failed: %v", err)
			}

			decrypted, err := DecryptPassword(encrypted, testSecret)
			if err != nil {
				t.Fatalf("DecryptPassword failed: %v", err)
			}

			if decrypted != tc.password {
				t.Errorf("got %q, want %q", decrypted, tc.password)
			}
		})
	}
}

func TestEncryptedOutputFormat(t *testing.T) {
	// Test that encrypted output is valid base64 and has expected minimum length
	encrypted, err := EncryptPassword("test", testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	// Should be valid base64
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("encrypted output is not valid base64: %v", err)
	}

	// Minimum length: salt (16) + nonce (12) + ciphertext (at least 1) + auth tag (16)
	minLen := saltSize + gcmNonceSize + 1 + 16
	if len(data) < minLen {
		t.Errorf("encrypted data too short: got %d bytes, want at least %d", len(data), minLen)
	}
}

func TestCiphertextTooShortAfterNonce(t *testing.T) {
	// Create data that passes the first length check but fails the nonce check
	// salt (16) + minimal data that won't satisfy nonce extraction
	data := make([]byte, saltSize+gcmNonceSize)
	encoded := base64.StdEncoding.EncodeToString(data)

	_, err := DecryptPassword(encoded, testSecret)
	if err == nil {
		t.Error("DecryptPassword should fail when ciphertext is empty after nonce")
	}
}

func TestVariousServerSecretLengths(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{"single_char", "a"},
		{"short", "short"},
		{"medium", "medium-length-secret-key"},
		{"long", strings.Repeat("long-secret-", 100)},
		{"unicode_secret", "\u00e4\u00f6\u00fc-secret-\U0001F511"},
	}

	password := "test-password"

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encrypted, err := EncryptPassword(password, tc.secret)
			if err != nil {
				t.Fatalf("EncryptPassword failed: %v", err)
			}

			decrypted, err := DecryptPassword(encrypted, tc.secret)
			if err != nil {
				t.Fatalf("DecryptPassword failed: %v", err)
			}

			if decrypted != password {
				t.Errorf("got %q, want %q", decrypted, password)
			}
		})
	}
}

func TestSaltExtraction(t *testing.T) {
	// Encrypt the same password twice
	password := "test-password"

	enc1, err := EncryptPassword(password, testSecret)
	if err != nil {
		t.Fatalf("first encryption failed: %v", err)
	}

	enc2, err := EncryptPassword(password, testSecret)
	if err != nil {
		t.Fatalf("second encryption failed: %v", err)
	}

	// Decode and extract salts
	data1, _ := base64.StdEncoding.DecodeString(enc1)
	data2, _ := base64.StdEncoding.DecodeString(enc2)

	salt1 := data1[:saltSize]
	salt2 := data2[:saltSize]

	// Salts should be different (random)
	if string(salt1) == string(salt2) {
		t.Error("different encryptions should use different salts")
	}
}

func TestDecryptWithModifiedSalt(t *testing.T) {
	encrypted, err := EncryptPassword("password", testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	// Modify the salt (first 16 bytes)
	data[0] ^= 0xFF

	modified := base64.StdEncoding.EncodeToString(data)

	// Should fail because the derived key will be wrong
	_, err = DecryptPassword(modified, testSecret)
	if err == nil {
		t.Error("DecryptPassword should fail with modified salt")
	}
}

func TestDecryptWithModifiedNonce(t *testing.T) {
	encrypted, err := EncryptPassword("password", testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	// Modify the nonce (bytes 16-27)
	data[saltSize] ^= 0xFF

	modified := base64.StdEncoding.EncodeToString(data)

	// Should fail because GCM authentication will fail
	_, err = DecryptPassword(modified, testSecret)
	if err == nil {
		t.Error("DecryptPassword should fail with modified nonce")
	}
}

func TestNullBytesInPassword(t *testing.T) {
	// Test that passwords containing null bytes work correctly
	password := "pass\x00word\x00with\x00nulls"

	encrypted, err := EncryptPassword(password, testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	decrypted, err := DecryptPassword(encrypted, testSecret)
	if err != nil {
		t.Fatalf("DecryptPassword failed: %v", err)
	}

	if decrypted != password {
		t.Errorf("got %q, want %q", decrypted, password)
	}
}

func TestBinaryDataAsPassword(t *testing.T) {
	// Test with binary data that might contain problematic bytes
	password := string([]byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD})

	encrypted, err := EncryptPassword(password, testSecret)
	if err != nil {
		t.Fatalf("EncryptPassword failed: %v", err)
	}

	decrypted, err := DecryptPassword(encrypted, testSecret)
	if err != nil {
		t.Fatalf("DecryptPassword failed: %v", err)
	}

	if decrypted != password {
		t.Errorf("binary data round-trip failed")
	}
}

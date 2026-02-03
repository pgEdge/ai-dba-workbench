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

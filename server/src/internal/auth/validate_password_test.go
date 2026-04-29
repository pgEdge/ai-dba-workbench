/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "valid passphrase exactly at minimum length",
			password: "correcthorse",
			wantErr:  false,
		},
		{
			name:     "valid 12-char mixed passphrase",
			password: "Secure1passXY",
			wantErr:  false,
		},
		{
			name:     "valid long passphrase with spaces",
			password: "correct horse battery staple",
			wantErr:  false,
		},
		{
			name:     "valid all-lowercase passphrase (no composition rules)",
			password: "abcdefghijkl",
			wantErr:  false,
		},
		{
			name:     "valid all-digit passphrase",
			password: "918273645091",
			wantErr:  false,
		},
		{
			name:     "valid unicode passphrase",
			password: "παράδειγμα-2026",
			wantErr:  false,
		},
		{
			name:     "valid near-max byte length",
			password: strings.Repeat("a", 72),
			wantErr:  false,
		},
		{
			name:     "valid passphrase that previously failed composition rules",
			password: "alllowercaseletters",
			wantErr:  false,
		},
		{
			name:      "empty string is rejected",
			password:  "",
			wantErr:   true,
			errSubstr: "at least 12 characters",
		},
		{
			name:      "11 characters is just under minimum",
			password:  "abcdefghijk",
			wantErr:   true,
			errSubstr: "at least 12 characters",
		},
		{
			name:      "single byte over the bcrypt 72-byte ceiling",
			password:  strings.Repeat("a", 73),
			wantErr:   true,
			errSubstr: "at most 72 bytes",
		},
		{
			name:      "multi-byte runes that exceed 72 bytes",
			password:  strings.Repeat("é", 40), // 40 runes, 80 bytes
			wantErr:   true,
			errSubstr: "at most 72 bytes",
		},
		{
			name:     "passphrase not in dictionary is accepted",
			password: "passwordpass",
			wantErr:  false, // not in the embedded common-password list
		},
		{
			name:      "common password from dictionary",
			password:  "unbelievable",
			wantErr:   true,
			errSubstr: "too common",
		},
		{
			name:      "common password is rejected case-insensitively",
			password:  "UnBelievable",
			wantErr:   true,
			errSubstr: "too common",
		},
		{
			name:      "common password with surrounding whitespace is rejected",
			password:  "  unbelievable  ",
			wantErr:   true,
			errSubstr: "too common",
		},
		{
			name:      "another common password from dictionary",
			password:  "contortionist",
			wantErr:   true,
			errSubstr: "too common",
		},
		{
			// NIST SP 800-63B does not require rejecting control
			// characters, and bcrypt safely accepts arbitrary bytes
			// within its 72-byte limit. The NUL byte counts as one
			// rune, so this 13-rune string is accepted as long as it
			// is not in the common-password dictionary.
			name:     "NUL byte embedded in password is accepted",
			password: "abcdefgh\x00ijkl",
			wantErr:  false,
		},
		{
			// Pre-composed "é" (U+00E9) repeated 12 times is exactly
			// 12 runes, satisfying the rune-count minimum.
			name:     "12 runes of pre-composed e-acute is accepted",
			password: strings.Repeat("é", 12),
			wantErr:  false,
		},
		{
			// Decomposed "é" ("e" + COMBINING ACUTE ACCENT, U+0301)
			// is two runes; 12 occurrences is 24 runes total. The
			// rune-count minimum and dictionary check both succeed.
			name:     "decomposed e-acute repeated 12 times is accepted",
			password: strings.Repeat("é", 12),
			wantErr:  false,
		},
		{
			// Cyrillic "о" (U+043E) at position 1 maps to ASCII "o"
			// after normalization, so the dictionary check finds the
			// underlying entry "contortionist" and rejects the input.
			name:      "cyrillic confusable maps to common password",
			password:  "cоntortionist",
			wantErr:   true,
			errSubstr: "too common",
		},
		{
			// Fullwidth Latin form of "unbelievable" (12 runes). NFKC
			// normalization folds fullwidth characters back to ASCII
			// so the entry is found in the dictionary.
			name:      "fullwidth latin maps to common password",
			password:  "ｕｎｂｅｌｉｅｖａｂｌｅ",
			wantErr:   true,
			errSubstr: "too common",
		},
		{
			// Each U+1F600 emoji encodes to four UTF-8 bytes, so 24
			// repetitions yield 24 runes / 96 bytes. The rune-count
			// minimum of 12 is satisfied, but the byte-length cap of
			// 72 fires with a clear "at most 72 bytes" message. This
			// guards against silent bcrypt truncation when a password
			// uses multi-byte runes.
			name:      "multi-byte runes that exceed 72 bytes",
			password:  strings.Repeat("\U0001F600", 24),
			wantErr:   true,
			errSubstr: "at most 72 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for password %q, got nil", tt.password)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error to contain %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error for password %q, got %v", tt.password, err)
				}
			}
		})
	}
}

// TestCommonPasswordDictionaryLoaded asserts that the embedded common-
// password dictionary parsed correctly at package init time. A corrupt or
// empty file would silently weaken the policy, so this test acts as a
// guardrail.
func TestCommonPasswordDictionaryLoaded(t *testing.T) {
	n := commonPasswordCount()
	if n < 5000 {
		t.Fatalf("expected at least 5000 common passwords, got %d", n)
	}

	// Spot-check a few entries we know must be present.
	for _, p := range []string{"password", "qwerty", "123456", "iloveyou"} {
		if !isCommonPassword(p) {
			t.Errorf("expected %q to be flagged as a common password", p)
		}
	}

	// A clearly-uncommon passphrase must not be flagged.
	if isCommonPassword("correct horse battery staple") {
		t.Errorf("did not expect long passphrase to be flagged as common")
	}
}

// TestIsCommonPasswordEmpty ensures the lookup returns false for the empty
// string regardless of dictionary state, so empty-input handling cannot be
// confused with the dictionary check.
func TestIsCommonPasswordEmpty(t *testing.T) {
	if isCommonPassword("") {
		t.Fatalf("empty string must not be reported as a common password")
	}
	if isCommonPassword("    ") {
		t.Fatalf("whitespace-only string must not be reported as a common password")
	}
}

// TestNormalizeForDictionary exercises the Unicode-confusable folder used
// by the common-password lookup. The cases cover NFKC compatibility folds
// (fullwidth, combining sequences), the explicit ASCII-confusable map for
// Cyrillic and Greek look-alikes, and the case-folding/whitespace-trim
// step. Leet-style substitutions are deliberately not folded; the final
// case verifies that they survive normalization.
func TestNormalizeForDictionary(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty string is returned unchanged",
			in:   "",
			want: "",
		},
		{
			name: "ascii is lowercased and trimmed",
			in:   "  Password  ",
			want: "password",
		},
		{
			name: "cyrillic confusable o is folded to ascii o",
			in:   "passwоrd1234",
			want: "password1234",
		},
		{
			name: "fullwidth latin folds via NFKC to ascii",
			in:   "ｐａｓｓｗｏｒｄ",
			want: "password",
		},
		{
			name: "fullwidth digits fold via NFKC to ascii digits",
			in:   "１２３４５６７８",
			want: "12345678",
		},
		{
			name: "decomposed e-acute composes to single rune via NFKC",
			in:   "éclair",
			want: "éclair",
		},
		{
			name: "greek alpha folds to ascii a",
			in:   "αlphαbet1234",
			want: "alphabet1234",
		},
		{
			name: "leet substitutions are not folded",
			in:   "p@ssw0rd",
			want: "p@ssw0rd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeForDictionary(tt.in)
			if got != tt.want {
				t.Errorf("normalizeForDictionary(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestFoldFullwidth covers the explicit fullwidth-to-ASCII fallback. NFKC
// normalization handles fullwidth forms in normal lookup paths, so the
// helper itself is rarely exercised end-to-end. This unit test pins the
// behavior to guard against future divergence between NFKC and the
// fallback table.
func TestFoldFullwidth(t *testing.T) {
	tests := []struct {
		name string
		in   rune
		want rune
	}{
		{name: "fullwidth digit zero", in: '０', want: '0'},
		{name: "fullwidth digit nine", in: '９', want: '9'},
		{name: "fullwidth uppercase A", in: 'Ａ', want: 'A'},
		{name: "fullwidth uppercase Z", in: 'Ｚ', want: 'Z'},
		{name: "fullwidth lowercase a", in: 'ａ', want: 'a'},
		{name: "fullwidth lowercase z", in: 'ｚ', want: 'z'},
		{name: "non-fullwidth ascii passes through", in: 'm', want: 'm'},
		{name: "unrelated non-ascii passes through", in: 'π', want: 'π'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := foldFullwidth(tt.in); got != tt.want {
				t.Errorf("foldFullwidth(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCommonPasswordConfusables confirms that script-confusable variants
// of dictionary entries are caught after normalization. A user attempting
// to bypass the dictionary by typing Cyrillic look-alikes or fullwidth
// Latin forms must still be rejected.
func TestCommonPasswordConfusables(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "cyrillic o in unbelievable", in: "unbeliеvable"}, // е is U+0435
		{name: "fullwidth contortionist", in: "ｃｏｎｔｏｒｔｉｏｎｉｓｔ"},
		{name: "greek alpha in masturbation", in: "mαsturbation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !isCommonPassword(tc.in) {
				t.Errorf("expected %q to fold to a common password", tc.in)
			}
		})
	}
}

// TestDecomposedRuneCount confirms that a decomposed accented character
// counts as two runes for the rune-count minimum, matching the contract
// documented on ValidatePassword. NFKC handles the dictionary check
// separately, so combining sequences must not be silently re-composed
// before the length check.
func TestDecomposedRuneCount(t *testing.T) {
	decomposed := "é"               // 2 runes, 3 bytes
	composed := norm.NFC.String("é") // 1 rune, 2 bytes
	if utf8.RuneCountInString(decomposed) != 2 {
		t.Fatalf("expected decomposed e-acute to be 2 runes, got %d", utf8.RuneCountInString(decomposed))
	}
	if utf8.RuneCountInString(composed) != 1 {
		t.Fatalf("expected composed e-acute to be 1 rune, got %d", utf8.RuneCountInString(composed))
	}
	// 12 decomposed sequences = 24 runes / 36 bytes; passes both bounds.
	pwd := strings.Repeat(decomposed, 12)
	if err := ValidatePassword(pwd); err != nil {
		t.Errorf("expected decomposed passphrase of 24 runes to pass, got %v", err)
	}
	// 6 decomposed sequences = 12 runes / 18 bytes; satisfies minimum.
	pwd = strings.Repeat(decomposed, 6)
	if err := ValidatePassword(pwd); err != nil {
		t.Errorf("expected decomposed passphrase of 12 runes to pass, got %v", err)
	}
}

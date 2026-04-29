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
	_ "embed"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"
)

// commonPasswordsRaw is the embedded common-password dictionary used to
// reject easily-guessed passwords. The file is sourced from the SecLists
// project (10k-most-common.txt) and contains one lowercase entry per line.
//
//go:embed data/common-passwords.txt
var commonPasswordsRaw string

var (
	commonPasswordsOnce sync.Once
	commonPasswords     map[string]struct{}
)

// confusableFolds maps script-confusable code points (Cyrillic, Greek,
// fullwidth Latin/digit forms) to their ASCII visual equivalents. The fold
// is intentionally narrow: only characters that share a glyph with an
// ASCII letter or digit are mapped. Leet-style folds (for example, 0->o or
// 3->e) are deliberately excluded because they would reject too many
// legitimate passphrases. NFKC handles fullwidth forms in most cases, but
// the explicit entries below act as a fallback for any code points NFKC
// leaves unchanged.
var confusableFolds = map[rune]rune{
	// Cyrillic lowercase look-alikes.
	'а': 'a', // CYRILLIC SMALL LETTER A
	'е': 'e', // CYRILLIC SMALL LETTER IE
	'о': 'o', // CYRILLIC SMALL LETTER O
	'р': 'p', // CYRILLIC SMALL LETTER ER
	'с': 'c', // CYRILLIC SMALL LETTER ES
	'у': 'y', // CYRILLIC SMALL LETTER U
	'х': 'x', // CYRILLIC SMALL LETTER HA
	// Cyrillic uppercase look-alikes (covered for safety prior to lower-
	// casing). The lookup runs after normalization but before strings.
	// ToLower, so include both cases here.
	'А': 'a', // CYRILLIC CAPITAL LETTER A
	'Е': 'e', // CYRILLIC CAPITAL LETTER IE
	'О': 'o', // CYRILLIC CAPITAL LETTER O
	'Р': 'p', // CYRILLIC CAPITAL LETTER ER
	'С': 'c', // CYRILLIC CAPITAL LETTER ES
	'У': 'y', // CYRILLIC CAPITAL LETTER U
	'Х': 'x', // CYRILLIC CAPITAL LETTER HA
	// Greek look-alikes.
	'Α': 'a', // GREEK CAPITAL LETTER ALPHA
	'α': 'a', // GREEK SMALL LETTER ALPHA
	'Ε': 'e', // GREEK CAPITAL LETTER EPSILON
	'ε': 'e', // GREEK SMALL LETTER EPSILON
	'Ο': 'o', // GREEK CAPITAL LETTER OMICRON
	'ο': 'o', // GREEK SMALL LETTER OMICRON
	'Ρ': 'p', // GREEK CAPITAL LETTER RHO
	'ρ': 'p', // GREEK SMALL LETTER RHO
	'Τ': 't', // GREEK CAPITAL LETTER TAU
	'τ': 't', // GREEK SMALL LETTER TAU
}

// foldFullwidth maps fullwidth ASCII characters (digits and Latin
// letters) to their ASCII counterparts. NFKC normalization typically
// performs this fold, but the explicit fallback guards against any
// future divergence.
func foldFullwidth(r rune) rune {
	switch {
	case r >= '０' && r <= '９':
		return '0' + (r - '０')
	case r >= 'Ａ' && r <= 'Ｚ':
		return 'A' + (r - 'Ａ')
	case r >= 'ａ' && r <= 'ｚ':
		return 'a' + (r - 'ａ')
	}
	return r
}

// normalizeForDictionary canonicalizes the supplied string for use as a
// key in the common-password dictionary. The transformation order is:
//  1. Apply NFKC Unicode normalization to collapse compatibility variants
//     and combining sequences (for example, fullwidth digits and
//     pre-composed accented letters).
//  2. Fold a small set of script-confusable characters (Cyrillic, Greek)
//     and any fullwidth ASCII forms NFKC may have missed onto their
//     ASCII visual equivalents.
//  3. Trim surrounding whitespace and lowercase the result so the
//     comparison is case-insensitive.
//
// The function is unexported because it is an implementation detail of
// the dictionary check.
func normalizeForDictionary(s string) string {
	if s == "" {
		return s
	}
	s = norm.NFKC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if folded, ok := confusableFolds[r]; ok {
			b.WriteRune(folded)
			continue
		}
		b.WriteRune(foldFullwidth(r))
	}
	return strings.ToLower(strings.TrimSpace(b.String()))
}

// loadCommonPasswords parses the embedded common-password dictionary into a
// set keyed by the normalized form of each entry. The work is performed
// exactly once and cached for the lifetime of the process. Storing the
// canonical form means the lookup side only normalizes the user input,
// keeping each call cheap.
func loadCommonPasswords() {
	commonPasswordsOnce.Do(func() {
		// Pre-size the map roughly; the SecLists 10k file has ~10000 entries.
		m := make(map[string]struct{}, 10000)
		for _, line := range strings.Split(commonPasswordsRaw, "\n") {
			entry := normalizeForDictionary(line)
			if entry == "" {
				continue
			}
			m[entry] = struct{}{}
		}
		commonPasswords = m
	})
}

// commonPasswordCount returns the number of entries in the common-password
// dictionary. It is primarily useful for tests that assert the embedded
// file loaded successfully and for the defense-in-depth load assertion in
// the auth store constructor.
func commonPasswordCount() int {
	loadCommonPasswords()
	return len(commonPasswords)
}

// isCommonPassword reports whether the supplied password appears in the
// embedded common-password dictionary. The password is normalized with
// normalizeForDictionary before lookup so Unicode-confusable variants
// (for example, Cyrillic "о" in place of ASCII "o") cannot bypass the
// check.
func isCommonPassword(password string) bool {
	loadCommonPasswords()
	key := normalizeForDictionary(password)
	if key == "" {
		return false
	}
	_, ok := commonPasswords[key]
	return ok
}

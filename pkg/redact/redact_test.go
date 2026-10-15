/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package redact_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/pkg/redact"
	"github.com/pgedge/ai-workbench/pkg/redact/redacttest"
)

func TestText(t *testing.T) {
	for _, tc := range redacttest.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := redact.Text(tc.In)
			if got != tc.Want {
				t.Errorf("got  %q\nwant %q", got, tc.Want)
			}
			if redacttest.SecretLeft(got) {
				t.Errorf("a secret survived: %q", got)
			}
		})
	}
}

// TestTextRedactionPatterns checks each pattern on its own, so removing
// one fails a test.
func TestTextRedactionPatterns(t *testing.T) {
	samples := map[string]string{
		"URI userinfo":           "postgresql://app:example-secret@192.0.2.1/db",
		"JSON key":               `{"password": "example-secret"}`,
		"SET credential setting": "SET restore_command TO 'example-secret'",
		"quoted name, value":     "('password', 'example-secret')",
		"keyword=value":          "password=example-secret",
		"PASSWORD 'value'":       "PASSWORD 'example-secret'",
	}
	redactions := redact.TextRedactions()
	if len(samples) != len(redactions) {
		t.Fatalf("%d samples for %d patterns", len(samples), len(redactions))
	}
	for _, r := range redactions {
		sample, ok := samples[r.Name]
		if !ok {
			t.Fatalf("no sample for %q", r.Name)
		}
		re := regexp.MustCompile("(?is)" + r.Pattern)
		re.Longest()
		got := re.ReplaceAllString(sample, strings.ReplaceAll(r.Replacement, `\1`, "${1}"))
		if strings.Contains(got, "example-secret") || !strings.Contains(got, redact.RedactedValue) {
			t.Errorf("%s: %q became %q", r.Name, sample, got)
		}
		// Only group 1 may capture, so PostgreSQL and Go cannot differ
		// on which group a replacement names.
		if n := re.NumSubexp(); n != 1 {
			t.Errorf("%s has %d capturing groups, want 1", r.Name, n)
		}
	}
}

// TestTextAdversarial masks inputs built to make a regular expression
// engine backtrack. Go's engine is linear in the input, about a second a
// megabyte without the race detector, so 64 KiB inputs keep the test
// quick under -race whilst quadratic behaviour would still take minutes.
// The server's integration tests run 1 MB inputs through PostgreSQL.
func TestTextAdversarial(t *testing.T) {
	for name, in := range redacttest.AdversarialInputs(1 << 16) {
		start := time.Now()
		redact.Text(in)
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("%s took %s", name, elapsed)
		}
	}
}

// TestAccessorsReturnCopies checks a caller cannot change the shared
// lists through what the accessors return.
func TestAccessorsReturnCopies(t *testing.T) {
	frags := redact.SecretColumnFragments()
	names := redact.TextSecretNamePatterns()
	settings := redact.CredentialSettings()
	redactions := redact.TextRedactions()
	if len(frags) == 0 || len(names) <= len(frags) || len(settings) == 0 || len(redactions) == 0 {
		t.Fatalf("unexpected lengths %d, %d, %d, %d", len(frags), len(names), len(settings), len(redactions))
	}
	frags[0], names[0], settings[0], redactions[0].Pattern = "x", "x", "x", "x"
	if redact.SecretColumnFragments()[0] == "x" || redact.TextSecretNamePatterns()[0] == "x" ||
		redact.CredentialSettings()[0] == "x" || redact.TextRedactions()[0].Pattern == "x" {
		t.Error("an accessor returned the shared slice")
	}
	if got := redact.Text("password=example-secret"); got != "password="+redact.RedactedValue {
		t.Errorf("Text changed after editing a copy: %q", got)
	}
}

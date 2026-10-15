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
	"unicode/utf8"

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

// TestTextInJSON masks every case as it appears in a row image, where
// every backslash is doubled and every double quote escaped.
func TestTextInJSON(t *testing.T) {
	for _, tc := range redacttest.Cases {
		if got := redact.Text(redacttest.JSONText(tc.In)); !redacttest.JSONFormOK(tc, got) {
			t.Errorf("%s: JSON form\ngot  %q\nwant %q", tc.Name, got, redacttest.JSONText(tc.Want))
		}
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
		"concatenated PASSWORD":  "PASSWORD ' || '''example-secret'''",
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

// TestKnownLimits pins what Text returns for the forms the patterns do
// not mask, so a change in what they do is noticed.
func TestKnownLimits(t *testing.T) {
	for _, tc := range redacttest.KnownLimits {
		if got := redact.Text(tc.In); got != tc.Want {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.Name, got, tc.Want)
		}
	}
}

// TestTextTruncates checks Text cuts a value to MaxTextLength
// characters, not bytes, before masking it.
func TestTextTruncates(t *testing.T) {
	in := "password=example-secret " + strings.Repeat("é", redact.MaxTextLength)
	got := redact.Text(in)
	if n := utf8.RuneCountInString(got); n != redact.MaxTextLength-len("example-secret")+len(redact.RedactedValue) {
		t.Errorf("got %d characters", n)
	}
	if !utf8.ValidString(got) || !strings.HasPrefix(got, "password=******** é") {
		t.Errorf("got %q...", got[:30])
	}
	if short := "password=x"; redact.Text(short) != "password=********" {
		t.Errorf("short text: %q", redact.Text(short))
	}
	if exact := strings.Repeat("a", redact.MaxTextLength); redact.Text(exact) != exact {
		t.Error("text of exactly MaxTextLength characters was cut")
	}
	// More bytes than MaxTextLength but fewer characters is kept whole.
	if wide := strings.Repeat("é", redact.MaxTextLength-1); redact.Text(wide) != wide {
		t.Error("text under MaxTextLength characters was cut")
	}
}

// TestTextAdversarial masks 1 MiB inputs built to make a regular
// expression engine backtrack. Text cuts them to MaxTextLength first,
// and Go's engine is linear in the input, so each takes well under a
// second; the server's integration tests run the same inputs through
// PostgreSQL.
func TestTextAdversarial(t *testing.T) {
	for name, in := range redacttest.AdversarialInputs(1 << 20) {
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

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

	"github.com/pgedge/ai-workbench/pkg/pgsettings"
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

// notIdempotent names the cases whose masked text Text masks further on
// a second pass, almost always by taking the closing quote after a
// masked value into the next unquoted value. Settled exists for them.
var notIdempotent = map[string]bool{
	"named argument":                                    true,
	"doubled quote conninfo":                            true,
	"positional node_create":                            true,
	"nested role password":                              true,
	"backslash quoted conninfo":                         true,
	"backslash quoted role password":                    true,
	"json escape string conninfo":                       true,
	"C22: PASSWORD after quote char":                    true,
	"C23: tab sep in E-string":                          true,
	"quoted role password after a comment":              true,
	"quoted role password after concatenation":          true,
	"backslash quoted with escaped backslash and quote": true,
	"backslash quoted with a lone escaped backslash":    true,
}

// TestSettled pins which cases Text does not leave as it found them on a
// second pass, and checks that Settled masks every case, and the one
// fuzzed input found to need a third pass, to text Text leaves alone.
func TestSettled(t *testing.T) {
	seen := 0
	for _, tc := range redacttest.Cases {
		once := redact.Text(tc.In)
		if changed := redact.Text(once) != once; changed != notIdempotent[tc.Name] {
			t.Errorf("%s: masking the masked text changes it: %v, pinned %v", tc.Name, changed, notIdempotent[tc.Name])
		}
		if notIdempotent[tc.Name] {
			seen++
		}
		settled := redact.Settled(tc.In)
		if redact.Text(settled) != settled || redacttest.SecretLeft(settled) {
			t.Errorf("%s: Settled gave %q", tc.Name, settled)
		}
	}
	if seen != len(notIdempotent) {
		t.Errorf("notIdempotent names %d cases, %d of them in redacttest.Cases", len(notIdempotent), seen)
	}

	third := `{"stmt": "CONNECTION E'host=192.0.2.1 password=\\'s3cret a b\\''"}` +
		`E app sword=_key":AWS_SECRET_ACCESS_KEY=example-secret aws s3 cp %p s3://bucket/%f`
	twice := redact.Text(redact.Text(third))
	if redact.Text(twice) == twice {
		t.Error("the third-pass input settles in two passes")
	}
	if settled := redact.Settled(third); redact.Text(settled) != settled {
		t.Errorf("the third-pass input did not settle: %q", settled)
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
	redactions := redact.TextRedactions()
	if len(frags) == 0 || len(names) <= len(frags) || len(redactions) == 0 {
		t.Fatalf("unexpected lengths %d, %d, %d", len(frags), len(names), len(redactions))
	}
	frags[0], names[0], redactions[0].Pattern = "x", "x", "x"
	if redact.SecretColumnFragments()[0] == "x" || redact.TextSecretNamePatterns()[0] == "x" ||
		redact.TextRedactions()[0].Pattern == "x" {
		t.Error("an accessor returned the shared slice")
	}
	if got := redact.Text("password=example-secret"); got != "password="+redact.RedactedValue {
		t.Errorf("Text changed after editing a copy: %q", got)
	}
}

// TestTextMasksEveryCredentialSetting checks that ALTER SYSTEM SET masks
// the value of every setting pkg/pgsettings lists as holding a
// credential, so that the list there is the only one to change. A name
// such as primary_conninfo also matches the keyword=value pattern, which
// can take the quotes with the value, so only the secret's absence is
// checked.
func TestTextMasksEveryCredentialSetting(t *testing.T) {
	settings := pgsettings.CredentialSettings()
	if len(settings) == 0 {
		t.Fatal("pgsettings.CredentialSettings() is empty")
	}
	var setPattern *regexp.Regexp
	for _, r := range redact.TextRedactions() {
		if r.Name == "SET credential setting" {
			setPattern = regexp.MustCompile("(?is)" + r.Pattern)
		}
	}
	if setPattern == nil {
		t.Fatal("no SET credential setting pattern")
	}
	for _, name := range settings {
		in := "ALTER SYSTEM SET " + name + " TO 'example secret'"
		if !setPattern.MatchString(in) {
			t.Errorf("the SET credential setting pattern does not match %q", in)
		}
		got := redact.Text(in)
		if strings.Contains(got, "example") || strings.Contains(got, "secret'") ||
			!strings.Contains(got, name) || !strings.Contains(got, redact.RedactedValue) {
			t.Errorf("Text(%q) = %q, want the value masked", in, got)
		}
	}
}

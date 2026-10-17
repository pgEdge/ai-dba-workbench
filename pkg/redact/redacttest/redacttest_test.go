/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package redacttest

import "testing"

func TestSecretLeft(t *testing.T) {
	for s, want := range map[string]bool{
		"password=example-secret":    true,
		"password=s3cret":            true,
		`{"token": 31337}`:           true,
		"PASSWORD 'hunter2'":         true,
		"password=--Secret123":       true,
		`correct\ horse\ battery`:    true,
		"password=********":          false,
		"https://example.com/secret": false,
	} {
		if got := SecretLeft(s); got != want {
			t.Errorf("SecretLeft(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestCasesHoldSecrets(t *testing.T) {
	leaking := 0
	for _, tc := range Cases {
		if tc.Name == "" || tc.In == "" {
			t.Errorf("incomplete case %+v", tc)
		}
		if SecretLeft(tc.In) {
			leaking++
		}
		if SecretLeft(tc.Want) {
			t.Errorf("%s: the wanted text holds a secret: %q", tc.Name, tc.Want)
		}
	}
	if leaking == 0 {
		t.Error("no case holds a secret")
	}
}

func TestJSONText(t *testing.T) {
	if got, want := JSONText(`a "b" \c <d>`), `"a \"b\" \\c <d>"`; got != want {
		t.Errorf("JSONText = %s, want %s", got, want)
	}
}

func TestJSONFormOK(t *testing.T) {
	c := Case{"c", "password='example-secret' x", "password=******** x"}
	for got, want := range map[string]bool{
		`"password=******** x"`:       true,
		`"password=******** x`:        true,
		`"password=******** y"`:       false,
		`"password=example-secret x"`: false,
		`"password='secret a b' x"`:   false,
	} {
		if JSONFormOK(c, got) != want {
			t.Errorf("JSONFormOK(%q) = %v, want %v", got, !want, want)
		}
	}
	j := Case{"j", `{"password": "example-secret"}`, ""}
	if !JSONFormOK(j, "anything masked") {
		t.Error("a JSON input with no secret left should pass")
	}
	if JSONFormOK(j, `"{\"password\": \"example-secret\"}"`) {
		t.Error("a JSON input with a secret left should fail")
	}
	for name, want := range JSONWants {
		var c *Case
		for i := range Cases {
			if Cases[i].Name == name {
				c = &Cases[i]
			}
		}
		if c == nil {
			t.Errorf("JSONWants names no case: %q", name)
			continue
		}
		if !JSONFormOK(*c, want) || SecretLeft(want) {
			t.Errorf("%s: JSONWants entry rejected or holds a secret", name)
		}
	}
}

func TestKnownLimitsComplete(t *testing.T) {
	if len(KnownLimits) == 0 {
		t.Fatal("no known limits")
	}
	for _, c := range KnownLimits {
		if c.Name == "" || c.In == "" || c.Want == "" {
			t.Errorf("incomplete known limit %+v", c)
		}
	}
}

func TestAdversarialInputs(t *testing.T) {
	const size = 1 << 12
	inputs := AdversarialInputs(size)
	if len(inputs) == 0 {
		t.Fatal("no inputs")
	}
	for name, in := range inputs {
		if len(in) < size/2 || len(in) > size*2 {
			t.Errorf("%s is %d bytes, want about %d", name, len(in), size)
		}
	}
}

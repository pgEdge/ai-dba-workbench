/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package incident

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixturesDir is the repository's incident fixtures directory.
const fixturesDir = "../../../../../evaluation/incidents"

const validYAML = `
id: sample-one
title: Sample
setup: SELECT 1;
workload:
  - name: holder
    sql: BEGIN;
metrics:
  - table: pg_stat_checkpointer
    samples:
      - ago: 30m
        values: {num_requested: 4}
alert:
  title: Something fired
  description: It is bad.
  severity: warning
  triggered_ago: 5m
expect:
  diagnosis:
    all_of: ['bad']
reference: It is bad.
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	t.Parallel()
	p := writeFile(t, t.TempDir(), "a.yaml", validYAML)
	inc, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if inc.ID != "sample-one" || inc.Path != p {
		t.Errorf("got id %q path %q", inc.ID, inc.Path)
	}
	if got := time.Duration(inc.Alert.TriggeredAgo); got != 5*time.Minute {
		t.Errorf("triggered_ago = %v", got)
	}
	if got := time.Duration(inc.Metrics[0].Samples[0].Ago); got != 30*time.Minute {
		t.Errorf("ago = %v", got)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"unknown field", validYAML + "hue: blue\n", "field hue not found"},
		{"bad duration", strings.Replace(validYAML, "triggered_ago: 5m", "triggered_ago: soon", 1), "invalid duration"},
		{"non-scalar duration", strings.Replace(validYAML, "triggered_ago: 5m", "triggered_ago: [1]", 1), "cannot unmarshal"},
		{"invalid", strings.Replace(validYAML, "id: sample-one", "id: Bad_ID", 1), "id \"Bad_ID\""},
		{"two documents", validYAML + "---\n" + validYAML, "more than one YAML document"},
		{"bad second document", validYAML + "---\nid: [\n", "parse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := writeFile(t, dir, strings.ReplaceAll(tc.name, " ", "-")+".yaml", tc.content)
			_, err := Load(p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("Load of a missing file succeeded")
	}
}

func validIncident() Incident {
	return Incident{
		ID: "ok", Title: "t", Setup: "SELECT 1",
		Workload:  []Session{{Name: "s", SQL: "SELECT 1"}},
		Metrics:   []MetricSeries{{Table: "pg_x", Samples: []Sample{{Values: map[string]any{"a": 1}}}}},
		Alert:     Alert{Title: "t", Description: "d", Severity: "critical"},
		Prompt:    "look at {{connection_id}}",
		Expect:    Expectations{Diagnosis: Diagnosis{AnyOf: []string{"x"}}},
		Reference: "x",
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*Incident)
		want   string
	}{
		{"valid", func(*Incident) {}, ""},
		{"bad id", func(i *Incident) { i.ID = "9lives" }, "id \"9lives\""},
		{"no title", func(i *Incident) { i.Title = " " }, "title is required"},
		{"no setup", func(i *Incident) { i.Setup = "" }, "setup is required"},
		{"workload without sql", func(i *Incident) { i.Workload[0].SQL = "" }, "workload[0] needs a name and sql"},
		{"bad table", func(i *Incident) { i.Metrics[0].Table = "metrics.x" }, "not a plain table name"},
		{"no samples", func(i *Incident) { i.Metrics[0].Samples = nil }, "has no samples"},
		{"empty sample", func(i *Incident) { i.Metrics[0].Samples[0].Values = nil }, "has no values"},
		{"negative ago", func(i *Incident) { i.Metrics[0].Samples[0].Ago = Duration(-time.Minute) }, "ago must not be negative"},
		{"negative triggered_ago", func(i *Incident) { i.Alert.TriggeredAgo = Duration(-time.Minute) }, "triggered_ago must not be negative"},
		{"reserved column", func(i *Incident) { i.Metrics[0].Samples[0].Values["collected_at"] = "x" }, "sets collected_at"},
		{"alert without description", func(i *Incident) { i.Alert.Description = "" }, "alert needs a title and a description"},
		{"bad severity", func(i *Incident) { i.Alert.Severity = "fatal" }, "alert.severity \"fatal\""},
		{"unknown placeholder", func(i *Incident) { i.Prompt = "{{host}}" }, "unknown placeholder"},
		{"no diagnosis", func(i *Incident) { i.Expect.Diagnosis.AnyOf = nil }, "at least one all_of or any_of"},
		{"bad pattern", func(i *Incident) { i.Expect.Actions.Forbidden = []string{"("} }, "pattern \"(\""},
		{"no reference", func(i *Incident) { i.Reference = "" }, "reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inc := validIncident()
			tc.mutate(&inc)
			err := inc.Validate()
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("Validate: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("Validate error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestLoadDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "b.yaml", validYAML)
	writeFile(t, dir, "a.yaml", strings.Replace(validYAML, "sample-one", "sample-two", 1))
	writeFile(t, dir, "notes.txt", "ignored")
	got, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(got) != 2 || got[0].ID != "sample-one" || got[1].ID != "sample-two" {
		t.Fatalf("LoadDir returned %d incidents, first %q", len(got), got[0].ID)
	}

	dup := t.TempDir()
	writeFile(t, dup, "a.yaml", validYAML)
	writeFile(t, dup, "b.yaml", validYAML)
	if _, err := LoadDir(dup); err == nil || !strings.Contains(err.Error(), "used by both") {
		t.Errorf("duplicate ids: err = %v", err)
	}

	bad := t.TempDir()
	writeFile(t, bad, "a.yaml", "id: [")
	if _, err := LoadDir(bad); err == nil {
		t.Error("LoadDir accepted an unparsable file")
	}

	if _, err := LoadDir(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no incident files") {
		t.Errorf("empty dir: err = %v", err)
	}
	if _, err := LoadDir(filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "list incidents") {
		t.Errorf("missing dir: err = %v", err)
	}

	// Glob metacharacters in the directory name are taken literally.
	literal := filepath.Join(t.TempDir(), "cases[1]")
	if err := os.Mkdir(literal, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, literal, "a.yaml", validYAML)
	if err := os.Mkdir(filepath.Join(literal, "sub.yaml"), 0o750); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadDir(literal); err != nil || len(got) != 1 {
		t.Errorf("LoadDir(%q) = %d incidents, %v", literal, len(got), err)
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()
	all := []*Incident{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got, err := Select(all, nil)
	if err != nil || len(got) != 3 {
		t.Fatalf("Select(nil) = %d, %v", len(got), err)
	}
	got, err = Select(all, []string{"c", "a"})
	if err != nil || len(got) != 2 || got[0].ID != "c" || got[1].ID != "a" {
		t.Fatalf("Select(c, a) = %v, %v", got, err)
	}
	if _, err := Select(all, []string{"z"}); err == nil {
		t.Error("Select accepted an unknown id")
	}
}

func TestRenderPrompt(t *testing.T) {
	t.Parallel()
	inc := validIncident()
	inc.Prompt = ""
	inc.Alert.Description = "  d  \n"
	got := inc.RenderPrompt(PromptData{ConnectionID: 7, ConnectionName: "eval-x", Database: "eval_x"})
	for _, want := range []string{`"eval-x" (connection_id 7, database "eval_x")`, "t\nd\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("default prompt %q lacks %q", got, want)
		}
	}
	inc.Prompt = "{{connection_id}}/{{connection_name}}/{{database}}/{{alert_title}}/{{alert_description}}"
	if got := inc.RenderPrompt(PromptData{ConnectionID: 3, ConnectionName: "n", Database: "d"}); got != "3/n/d/t/d" {
		t.Errorf("custom prompt = %q", got)
	}
}

// TestRepositoryFixtures checks that every fixture shipped with the
// repository loads and validates.
func TestRepositoryFixtures(t *testing.T) {
	t.Parallel()
	all, err := LoadDir(fixturesDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(all) < 5 {
		t.Errorf("expected at least five fixtures, found %d", len(all))
	}
}

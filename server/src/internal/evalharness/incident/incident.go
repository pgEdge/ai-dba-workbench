/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package incident defines the incident fixture format used by the
// evaluation harness, and loads and validates fixture files.
//
// An incident fixture is a YAML file describing a synthetic database
// problem: the SQL that recreates it on a disposable PostgreSQL
// instance, any sessions that must stay open while it is investigated,
// the alert and recorded metric history the Workbench datastore should
// hold, and the checks a good investigation must pass. The fields are
// documented in docs/developer-guide/evaluation/index.md.
package incident

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Incident is one fixture.
type Incident struct {
	// ID uniquely names the incident; it must match idPattern and is
	// used in database, connection and report names.
	ID string `yaml:"id"`
	// Title is a one-line human-readable name.
	Title string `yaml:"title"`
	// Category groups related incidents in reports (locking, bloat...).
	Category string `yaml:"category"`
	// Summary explains, for maintainers, what the incident recreates and
	// what a good investigation finds.
	Summary string `yaml:"summary"`

	// Setup is SQL run, as the instance's administrative role, in the
	// incident's own freshly created database.
	Setup string `yaml:"setup"`
	// Workload lists sessions opened after Setup and held open while the
	// investigation runs, for problems that only exist while a session
	// is active (held locks, idle transactions).
	Workload []Session `yaml:"workload"`
	// Teardown is SQL run in the administrative database after each run,
	// to undo instance-wide changes made by Setup (replication slots,
	// ALTER SYSTEM settings). The incident database itself is always
	// dropped, so Teardown need not clean up inside it.
	Teardown string `yaml:"teardown"`

	// Metrics holds recorded metric history inserted into the datastore
	// for the incident's connection.
	Metrics []MetricSeries `yaml:"metrics"`
	// Alert is the alert that "fired" and prompted the investigation.
	Alert Alert `yaml:"alert"`

	// Prompt optionally replaces the default investigation request sent
	// to Ellie. It may use the placeholders listed in PromptPlaceholders.
	Prompt string `yaml:"prompt"`

	// Expect holds the scoring checks.
	Expect Expectations `yaml:"expect"`
	// Reference is an example of a good final report. Unit tests check
	// that it passes the incident's own diagnosis and action checks,
	// which keeps the expectations honest.
	Reference string `yaml:"reference"`

	// Path is the file the incident was loaded from.
	Path string `yaml:"-"`
}

// Session is one workload session.
type Session struct {
	// Name labels the session in errors.
	Name string `yaml:"name"`
	// SQL is run on the session's own connection.
	SQL string `yaml:"sql"`
	// Blocks marks a session whose SQL is expected to wait on a lock.
	// It is started in the background and the harness waits until it is
	// seen waiting before the investigation starts. Sessions without it
	// run their SQL to completion and then stay connected, so an open
	// transaction they began stays open.
	Blocks bool `yaml:"blocks"`
}

// MetricSeries is recorded history for one datastore metrics table.
type MetricSeries struct {
	// Table is the table name under the metrics schema, such as
	// pg_stat_checkpointer.
	Table string `yaml:"table"`
	// Samples are the rows to insert.
	Samples []Sample `yaml:"samples"`
}

// Sample is one recorded metrics row.
type Sample struct {
	// Ago is how long before the run started the sample was collected,
	// as a Go duration such as 45m.
	Ago Duration `yaml:"ago"`
	// Values maps column names to values. connection_id and
	// collected_at are filled in by the harness.
	Values map[string]any `yaml:"values"`
}

// Alert describes the alert row inserted into the datastore.
type Alert struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Severity    string   `yaml:"severity"`
	Probe       string   `yaml:"probe"`
	Metric      string   `yaml:"metric"`
	Value       *float64 `yaml:"value"`
	Threshold   *float64 `yaml:"threshold"`
	Operator    string   `yaml:"operator"`
	Object      string   `yaml:"object"`
	// TriggeredAgo is how long before the run the alert fired.
	TriggeredAgo Duration `yaml:"triggered_ago"`
}

// Expectations are the deterministic checks applied to a run.
type Expectations struct {
	Diagnosis Diagnosis `yaml:"diagnosis"`
	Actions   Actions   `yaml:"actions"`
	// Tools lists tools the investigation must call at least once.
	Tools []string `yaml:"tools"`
}

// Diagnosis checks the final report's text with case-insensitive
// regular expressions.
type Diagnosis struct {
	// AllOf must every one match.
	AllOf []string `yaml:"all_of"`
	// AnyOf needs at least one match, when present.
	AnyOf []string `yaml:"any_of"`
	// NoneOf must none match; use it for known wrong diagnoses.
	NoneOf []string `yaml:"none_of"`
}

// Actions checks the SQL the report proposes, taken from its fenced sql
// code blocks with comments removed.
type Actions struct {
	// Allowed needs at least one proposed statement matching one of
	// these patterns, when present.
	Allowed []string `yaml:"allowed"`
	// Forbidden must match no proposed statement.
	Forbidden []string `yaml:"forbidden"`
}

// Duration is a time.Duration read from YAML as a Go duration string.
type Duration time.Duration

// UnmarshalYAML parses a duration string such as "30m".
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: %w", node.Line, err)
	}
	*d = Duration(v)
	return nil
}

// idPattern restricts incident IDs to names that are safe in database
// and connection names.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

// tablePattern restricts metrics table names.
var tablePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

var severities = map[string]bool{"info": true, "warning": true, "critical": true}

// PromptPlaceholders are the placeholders a fixture's Prompt may use.
var PromptPlaceholders = []string{
	"{{connection_id}}", "{{connection_name}}", "{{database}}",
	"{{alert_title}}", "{{alert_description}}",
}

// Load reads and validates one fixture file.
func Load(path string) (*Incident, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: fixture paths come from the operator
	if err != nil {
		return nil, fmt.Errorf("read incident: %w", err)
	}
	var inc Incident
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&inc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	inc.Path = path
	if err := inc.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &inc, nil
}

// LoadDir loads every *.yaml file in dir, sorted by ID, and rejects
// duplicate IDs.
func LoadDir(dir string) ([]*Incident, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no incident files (*.yaml) in %s", dir)
	}
	seen := map[string]string{}
	incidents := make([]*Incident, 0, len(paths))
	for _, p := range paths {
		inc, err := Load(p)
		if err != nil {
			return nil, err
		}
		if prev, ok := seen[inc.ID]; ok {
			return nil, fmt.Errorf("incident id %q is used by both %s and %s", inc.ID, prev, p)
		}
		seen[inc.ID] = p
		incidents = append(incidents, inc)
	}
	sort.Slice(incidents, func(i, j int) bool { return incidents[i].ID < incidents[j].ID })
	return incidents, nil
}

// Select returns the incidents whose IDs are listed in ids, in the
// order given, or all of them when ids is empty.
func Select(all []*Incident, ids []string) ([]*Incident, error) {
	if len(ids) == 0 {
		return all, nil
	}
	byID := make(map[string]*Incident, len(all))
	for _, inc := range all {
		byID[inc.ID] = inc
	}
	out := make([]*Incident, 0, len(ids))
	for _, id := range ids {
		inc, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown incident %q", id)
		}
		out = append(out, inc)
	}
	return out, nil
}

// Validate checks the fixture's required fields and patterns.
func (inc *Incident) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if !idPattern.MatchString(inc.ID) {
		add("id %q must be lower-case letters, digits and hyphens, starting with a letter (at most 41 characters)", inc.ID)
	}
	if strings.TrimSpace(inc.Title) == "" {
		add("title is required")
	}
	if strings.TrimSpace(inc.Setup) == "" {
		add("setup is required")
	}
	for i, s := range inc.Workload {
		if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.SQL) == "" {
			add("workload[%d] needs a name and sql", i)
		}
	}
	for i, m := range inc.Metrics {
		if !tablePattern.MatchString(m.Table) {
			add("metrics[%d].table %q is not a plain table name", i, m.Table)
		}
		if len(m.Samples) == 0 {
			add("metrics[%d] has no samples", i)
		}
		for j, s := range m.Samples {
			if len(s.Values) == 0 {
				add("metrics[%d].samples[%d] has no values", i, j)
			}
			for col := range s.Values {
				if col == "connection_id" || col == "collected_at" {
					add("metrics[%d].samples[%d] sets %s, which the harness fills in", i, j, col)
				}
			}
		}
	}
	if strings.TrimSpace(inc.Alert.Title) == "" || strings.TrimSpace(inc.Alert.Description) == "" {
		add("alert needs a title and a description")
	}
	if !severities[inc.Alert.Severity] {
		add("alert.severity %q must be info, warning or critical", inc.Alert.Severity)
	}
	if prompt := inc.Prompt; prompt != "" {
		rest := prompt
		for _, p := range PromptPlaceholders {
			rest = strings.ReplaceAll(rest, p, "")
		}
		if strings.Contains(rest, "{{") {
			add("prompt uses an unknown placeholder; known ones are %s", strings.Join(PromptPlaceholders, ", "))
		}
	}
	if err := inc.Expect.validate(); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(inc.Reference) == "" {
		add("reference (an example of a good report) is required")
	}
	return errors.Join(errs...)
}

func (e Expectations) validate() error {
	d := e.Diagnosis
	if len(d.AllOf)+len(d.AnyOf) == 0 {
		return errors.New("expect.diagnosis needs at least one all_of or any_of pattern")
	}
	var errs []error
	for _, group := range [][]string{d.AllOf, d.AnyOf, d.NoneOf, e.Actions.Allowed, e.Actions.Forbidden} {
		for _, p := range group {
			if _, err := Compile(p); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Compile compiles a fixture pattern; matching is case-insensitive.
func Compile(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", pattern, err)
	}
	return re, nil
}

// PromptData is the run-specific information substituted into the
// investigation prompt.
type PromptData struct {
	ConnectionID   int
	ConnectionName string
	Database       string
}

// defaultPrompt is the request sent to Ellie when a fixture has no
// Prompt of its own. It reads like an on-call engineer handing over an
// alert, and deliberately gives no hint of the cause.
const defaultPrompt = `An alert has fired on the server "{{connection_name}}" (connection_id {{connection_id}}, database "{{database}}"):

{{alert_title}}
{{alert_description}}

Please investigate what is causing this, explain the root cause, and recommend what we should do about it.`

// RenderPrompt returns the investigation request for one run.
func (inc *Incident) RenderPrompt(d PromptData) string {
	tmpl := inc.Prompt
	if tmpl == "" {
		tmpl = defaultPrompt
	}
	return strings.NewReplacer(
		"{{connection_id}}", fmt.Sprint(d.ConnectionID),
		"{{connection_name}}", d.ConnectionName,
		"{{database}}", d.Database,
		"{{alert_title}}", inc.Alert.Title,
		"{{alert_description}}", strings.TrimSpace(inc.Alert.Description),
	).Replace(tmpl)
}

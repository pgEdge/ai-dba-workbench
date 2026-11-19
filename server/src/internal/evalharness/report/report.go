/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package report records evaluation runs and summarizes them as pass
// rates and score variance, in JSON for machines and Markdown for
// people.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/investigate"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/scoring"
)

// Run is one investigation of one incident with one target.
type Run struct {
	Incident   string                 `json:"incident"`
	Title      string                 `json:"title"`
	Target     string                 `json:"target"`
	Index      int                    `json:"run"`
	Transcript investigate.Transcript `json:"transcript"`
	Result     scoring.Result         `json:"result"`
}

// Results is everything one invocation of the harness produced; it is
// saved as results.json.
type Results struct {
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	PromptSource string    `json:"prompt_source"`
	Runs         []Run     `json:"runs"`
}

// Stat is the mean and sample standard deviation of a measurement
// across repeated runs.
type Stat struct {
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
}

// CheckRate is how often one check passed.
type CheckRate struct {
	Name   string `json:"name"`
	Passed int    `json:"passed"`
	Runs   int    `json:"runs"`
}

// Summary aggregates the runs of one incident with one target.
type Summary struct {
	Incident   string      `json:"incident"`
	Title      string      `json:"title"`
	Target     string      `json:"target"`
	Runs       int         `json:"runs"`
	Passed     int         `json:"passed"`
	Score      Stat        `json:"score"`
	Iterations Stat        `json:"iterations"`
	ToolCalls  Stat        `json:"tool_calls"`
	Tokens     Stat        `json:"tokens"`
	Seconds    Stat        `json:"seconds"`
	Checks     []CheckRate `json:"checks"`
}

// PassRate is the fraction of runs that passed every check.
func (s Summary) PassRate() float64 {
	if s.Runs == 0 {
		return 0
	}
	return float64(s.Passed) / float64(s.Runs)
}

// Summarize groups runs by incident and target, in the order each pair
// first appears.
func Summarize(runs []Run) []Summary {
	type key struct{ incident, target string }
	var order []key
	groups := map[key][]Run{}
	for i := range runs {
		k := key{runs[i].Incident, runs[i].Target}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], runs[i])
	}
	out := make([]Summary, 0, len(order))
	for _, k := range order {
		out = append(out, summarizeGroup(groups[k]))
	}
	return out
}

func summarizeGroup(runs []Run) Summary {
	s := Summary{Incident: runs[0].Incident, Title: runs[0].Title, Target: runs[0].Target, Runs: len(runs)}
	var score, iters, calls, tokens, secs []float64
	var checkOrder []string
	checks := map[string]*CheckRate{}
	for i := range runs {
		r := &runs[i]
		if r.Result.Passed {
			s.Passed++
		}
		score = append(score, r.Result.Score)
		iters = append(iters, float64(r.Transcript.Iterations))
		calls = append(calls, float64(len(r.Transcript.ToolCalls)))
		tokens = append(tokens, float64(r.Transcript.Usage.TotalTokens))
		secs = append(secs, r.Transcript.Duration.Seconds())
		for _, c := range r.Result.Checks {
			cr, ok := checks[c.Name]
			if !ok {
				cr = &CheckRate{Name: c.Name}
				checks[c.Name] = cr
				checkOrder = append(checkOrder, c.Name)
			}
			cr.Runs++
			if c.Passed {
				cr.Passed++
			}
		}
	}
	s.Score, s.Iterations, s.ToolCalls, s.Tokens, s.Seconds = stat(score), stat(iters), stat(calls), stat(tokens), stat(secs)
	for _, name := range checkOrder {
		s.Checks = append(s.Checks, *checks[name])
	}
	return s
}

func stat(xs []float64) Stat {
	if len(xs) == 0 {
		return Stat{}
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	if len(xs) < 2 {
		return Stat{Mean: mean}
	}
	var sq float64
	for _, x := range xs {
		sq += (x - mean) * (x - mean)
	}
	return Stat{Mean: mean, StdDev: math.Sqrt(sq / float64(len(xs)-1))}
}

// Save writes results as indented JSON.
func Save(path string, res Results) error {
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	return nil
}

// Load reads results saved by Save.
func Load(path string) (Results, error) {
	var res Results
	data, err := os.ReadFile(path) //nolint:gosec // G304: results paths come from the operator
	if err != nil {
		return res, fmt.Errorf("read results: %w", err)
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return res, fmt.Errorf("parse %s: %w", path, err)
	}
	return res, nil
}

// maxExcerpt caps the final report quoted for a failed run.
const maxExcerpt = 3000

// WriteMarkdown writes the report for one set of results.
func WriteMarkdown(w io.Writer, res Results) error {
	p := &printer{w: w}
	summaries := Summarize(res.Runs)

	p.line("# Ellie evaluation report")
	p.line("")
	p.line("Started %s, finished %s; system prompt from `%s`.",
		res.StartedAt.UTC().Format(time.RFC3339), res.FinishedAt.UTC().Format(time.RFC3339), res.PromptSource)
	p.line("")
	p.line("A run passes when every check passes. Score is the fraction of checks passed;")
	p.line("figures after ± are the sample standard deviation across repeated runs.")
	p.line("")

	p.line("## Overall")
	p.line("")
	p.line("| Target | Runs | Passed | Pass rate | Mean score |")
	p.line("|--------|-----:|-------:|----------:|-----------:|")
	totals := targetTotals(res.Runs)
	for i := range totals {
		t := &totals[i]
		p.line("| %s | %d | %d | %s | %.2f ± %.2f |", t.Target, t.Runs, t.Passed, percent(t.PassRate()), t.Score.Mean, t.Score.StdDev)
	}
	p.line("")

	p.line("## By incident")
	p.line("")
	p.line("| Incident | Target | Runs | Pass rate | Score | Iterations | Tool calls | Tokens | Seconds |")
	p.line("|----------|--------|-----:|----------:|------:|-----------:|-----------:|-------:|--------:|")
	for i := range summaries {
		s := &summaries[i]
		p.line("| %s | %s | %d | %s | %.2f ± %.2f | %.1f ± %.1f | %.1f ± %.1f | %.0f ± %.0f | %.0f ± %.0f |",
			s.Incident, s.Target, s.Runs, percent(s.PassRate()), s.Score.Mean, s.Score.StdDev,
			s.Iterations.Mean, s.Iterations.StdDev, s.ToolCalls.Mean, s.ToolCalls.StdDev,
			s.Tokens.Mean, s.Tokens.StdDev, s.Seconds.Mean, s.Seconds.StdDev)
	}
	p.line("")

	p.line("## Checks")
	for i := range summaries {
		s := &summaries[i]
		p.line("")
		p.line("### %s (%s)", s.Incident, s.Target)
		p.line("")
		if s.Title != "" {
			p.line("%s.", strings.TrimSuffix(s.Title, "."))
			p.line("")
		}
		p.line("| Check | Pass rate |")
		p.line("|-------|----------:|")
		for _, c := range s.Checks {
			p.line("| %s | %s (%d/%d) |", escapeCell(c.Name), percent(float64(c.Passed)/float64(c.Runs)), c.Passed, c.Runs)
		}
	}
	p.line("")

	writeFailedRuns(p, res.Runs)
	return p.err
}

// writeFailedRuns lists each failed run with the checks it failed and an
// excerpt of its final report.
func writeFailedRuns(p *printer, runs []Run) {
	p.line("## Failed runs")
	p.line("")
	failed := 0
	for i := range runs {
		r := &runs[i]
		if r.Result.Passed {
			continue
		}
		failed++
		p.line("### %s (%s), run %d", r.Incident, r.Target, r.Index)
		p.line("")
		p.line("Outcome `%s` after %d iterations and %d tool calls.", r.Transcript.Outcome, r.Transcript.Iterations, len(r.Transcript.ToolCalls))
		if r.Transcript.Error != "" {
			p.line("Error: %s", oneLine(r.Transcript.Error))
		}
		p.line("")
		for _, c := range r.Result.Checks {
			if !c.Passed {
				p.line("- Failed: %s (%s)", c.Name, oneLine(c.Detail))
			}
		}
		if text := strings.TrimSpace(r.Transcript.FinalText); text != "" {
			p.line("")
			p.line("<details><summary>Final report</summary>")
			p.line("")
			p.line("%s", fence(excerpt(text, maxExcerpt)))
			p.line("")
			p.line("</details>")
		}
		p.line("")
	}
	if failed == 0 {
		p.line("None.")
	}
}

// Labeled is one results file in a comparison.
type Labeled struct {
	Label   string
	Results Results
}

// WriteComparison writes a table of pass rates per incident for several
// results files, such as a baseline and a candidate prompt.
func WriteComparison(w io.Writer, sets []Labeled) error {
	p := &printer{w: w}
	type column struct{ label, target string }
	var columns []column
	cells := map[column]map[string]Summary{}
	incidentSet := map[string]bool{}
	for _, set := range sets {
		summaries := Summarize(set.Results.Runs)
		for i := range summaries {
			s := summaries[i]
			c := column{set.Label, s.Target}
			if _, ok := cells[c]; !ok {
				cells[c] = map[string]Summary{}
				columns = append(columns, c)
			}
			cells[c][s.Incident] = s
			incidentSet[s.Incident] = true
		}
	}
	incidents := make([]string, 0, len(incidentSet))
	for id := range incidentSet {
		incidents = append(incidents, id)
	}
	sort.Strings(incidents)

	p.line("# Ellie evaluation comparison")
	p.line("")
	p.line("Each cell is the pass rate, then the mean score ± its standard deviation.")
	p.line("")
	header, rule := "| Incident |", "|----------|"
	for _, c := range columns {
		header += fmt.Sprintf(" %s: %s |", escapeCell(c.label), c.target)
		rule += "------|"
	}
	p.line("%s", header)
	p.line("%s", rule)
	for _, id := range incidents {
		row := "| " + id + " |"
		for _, c := range columns {
			if s, ok := cells[c][id]; ok {
				row += fmt.Sprintf(" %s, %.2f ± %.2f |", percent(s.PassRate()), s.Score.Mean, s.Score.StdDev)
			} else {
				row += " - |"
			}
		}
		p.line("%s", row)
	}
	return p.err
}

// targetTotals summarizes all runs of each target, in the order the
// targets first appear.
func targetTotals(runs []Run) []Summary {
	all := make([]Run, len(runs))
	copy(all, runs)
	for i := range all {
		all[i].Incident = ""
		all[i].Title = ""
	}
	return Summarize(all)
}

type printer struct {
	w   io.Writer
	err error
}

func (p *printer) line(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format+"\n", args...)
}

func percent(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }

func escapeCell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func excerpt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "\n[truncated]"
}

// fence wraps text in a code fence longer than any backtick run inside
// it, so a model's own code blocks cannot break the report's layout.
func fence(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", max(3, longest+1))
	return f + "markdown\n" + text + "\n" + f
}

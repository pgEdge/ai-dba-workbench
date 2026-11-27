/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package scoring applies an incident's deterministic checks to one
// investigation.
//
// Every check is a pass or fail decided by pattern matching, so the same
// transcript always gets the same score and changes in pass rate come
// from the model, the prompt or the tools rather than from the grader.
package scoring

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/incident"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/investigate"
	"github.com/pgedge/ai-workbench/server/internal/sqllex"
)

// Check is the result of one check.
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	// Detail explains a failure, such as the forbidden statement found.
	Detail string `json:"detail,omitempty"`
}

// Result is the score for one investigation.
type Result struct {
	Checks []Check `json:"checks"`
	// Score is the fraction of checks passed, from 0 to 1; it is 0
	// when the investigation produced no report.
	Score float64 `json:"score"`
	// Passed is true when every check passed.
	Passed bool `json:"passed"`
}

// Score applies every check: that the investigation completed, the
// diagnosis and action checks on its final report, and the required
// tools.
func Score(exp incident.Expectations, t investigate.Transcript) (Result, error) {
	checks := []Check{{
		Name:   "investigation completed",
		Passed: t.Outcome == investigate.OutcomeCompleted,
		Detail: outcomeDetail(t),
	}}
	report, err := reportChecks(exp, t.FinalText)
	if err != nil {
		return Result{}, err
	}
	checks = append(checks, report...)

	called := map[string]bool{}
	for _, n := range t.ToolNames() {
		called[n] = true
	}
	for _, tool := range exp.Tools {
		c := Check{Name: "used tool " + tool, Passed: called[tool]}
		if !c.Passed {
			c.Detail = "never called"
		}
		checks = append(checks, c)
	}
	r := summarize(checks)
	if t.Outcome != investigate.OutcomeCompleted || strings.TrimSpace(t.FinalText) == "" {
		// Only a completed investigation holds a report the model wrote;
		// otherwise the final text is a canned message or an unfinished
		// draft, and the negative checks (none_of, forbidden) would pass
		// vacuously. Neither earns partial credit.
		r.Score = 0
	}
	return r, nil
}

// ScoreReport applies only the diagnosis and action checks to a report.
// Tests use it to confirm each fixture's reference report passes.
func ScoreReport(exp incident.Expectations, report string) (Result, error) {
	checks, err := reportChecks(exp, report)
	if err != nil {
		return Result{}, err
	}
	return summarize(checks), nil
}

func outcomeDetail(t investigate.Transcript) string {
	switch {
	case t.Outcome == investigate.OutcomeCompleted:
		return ""
	case t.Error != "":
		return fmt.Sprintf("%s: %s", t.Outcome, t.Error)
	default:
		return string(t.Outcome)
	}
}

func reportChecks(exp incident.Expectations, report string) ([]Check, error) {
	var checks []Check
	d := exp.Diagnosis

	for _, p := range d.AllOf {
		re, err := incident.Compile(p)
		if err != nil {
			return nil, err
		}
		c := Check{Name: "diagnosis mentions /" + p + "/", Passed: re.MatchString(report)}
		if !c.Passed {
			c.Detail = "no match in the final report"
		}
		checks = append(checks, c)
	}
	if len(d.AnyOf) > 0 {
		c := Check{Name: "diagnosis mentions one of /" + strings.Join(d.AnyOf, "/, /") + "/"}
		for _, p := range d.AnyOf {
			re, err := incident.Compile(p)
			if err != nil {
				return nil, err
			}
			c.Passed = c.Passed || re.MatchString(report)
		}
		if !c.Passed {
			c.Detail = "none matched the final report"
		}
		checks = append(checks, c)
	}
	for _, p := range d.NoneOf {
		re, err := incident.Compile(p)
		if err != nil {
			return nil, err
		}
		c := Check{Name: "diagnosis avoids /" + p + "/"}
		m := re.FindString(report)
		c.Passed = m == ""
		if !c.Passed {
			c.Detail = fmt.Sprintf("found %q", m)
		}
		checks = append(checks, c)
	}

	blocks := fencedBlocks(report)
	statements := sqlStatements(blocks)
	if len(exp.Actions.Allowed) > 0 {
		c := Check{Name: "proposes a fix matching /" + strings.Join(exp.Actions.Allowed, "/, /") + "/"}
		for _, p := range exp.Actions.Allowed {
			re, err := incident.Compile(p)
			if err != nil {
				return nil, err
			}
			c.Passed = c.Passed || anyMatch(re, statements) != ""
		}
		if !c.Passed {
			c.Detail = fmt.Sprintf("no matching statement among %d proposed", len(statements))
		}
		checks = append(checks, c)
	}
	// A destructive command is forbidden whatever language its block is
	// in, such as pg_resetwal in a shell block, so forbidden patterns
	// see the commands in every block, not only the SQL statements.
	commands := slices.Concat(statements, otherCommands(blocks))
	for _, p := range exp.Actions.Forbidden {
		re, err := incident.Compile(p)
		if err != nil {
			return nil, err
		}
		c := Check{Name: "does not propose /" + p + "/"}
		if m := anyMatch(re, commands); m != "" {
			c.Detail = fmt.Sprintf("proposed %q", m)
		} else {
			c.Passed = true
		}
		checks = append(checks, c)
	}
	return checks, nil
}

func anyMatch(re *regexp.Regexp, statements []string) string {
	for _, s := range statements {
		if re.MatchString(s) {
			return s
		}
	}
	return ""
}

func summarize(checks []Check) Result {
	r := Result{Checks: checks, Passed: true}
	passed := 0
	for _, c := range checks {
		if c.Passed {
			passed++
		} else {
			r.Passed = false
		}
	}
	if len(checks) > 0 {
		r.Score = float64(passed) / float64(len(checks))
	}
	return r
}

// fencePattern finds the opening line of a fenced code block and its
// info string.
var fencePattern = regexp.MustCompile("(?m)^[ \t]*(```+|~~~+)[ \t]*([A-Za-z0-9_+-]*)[^\n]*\n")

// sqlLanguages are the info strings treated as SQL.
var sqlLanguages = map[string]bool{"sql": true, "postgresql": true, "postgres": true, "pgsql": true, "psql": true, "plpgsql": true}

// block is one fenced code block: its info string, lower-cased, and
// its contents.
type block struct {
	lang string
	body string
}

// fencedBlocks returns a report's fenced code blocks in order. A block
// left open runs to the end of the report.
func fencedBlocks(report string) []block {
	var blocks []block
	rest := report
	for {
		loc := fencePattern.FindStringSubmatchIndex(rest)
		if loc == nil {
			return blocks
		}
		fence := rest[loc[2]:loc[3]]
		lang := strings.ToLower(rest[loc[4]:loc[5]])
		body := rest[loc[1]:]
		end := closingFence(body, fence)
		if end < 0 {
			return append(blocks, block{lang: lang, body: body})
		}
		blocks = append(blocks, block{lang: lang, body: body[:end]})
		// Skip the whole closing line: a closing fence may be longer than
		// the opening one, and its extra characters must not open a block.
		nl := strings.IndexByte(body[end:], '\n')
		if nl < 0 {
			return blocks
		}
		rest = body[end+nl+1:]
	}
}

// ProposedStatements returns the SQL statements in a report's fenced
// sql code blocks, with comments removed, so that a statement the model
// merely mentions in a comment, or warns against there, does not count
// as proposed. These are the actions the report recommends running.
func ProposedStatements(report string) []string {
	return sqlStatements(fencedBlocks(report))
}

func sqlStatements(blocks []block) []string {
	var statements []string
	for _, b := range blocks {
		if sqlLanguages[b.lang] {
			statements = append(statements, sqllex.SplitStatements(stripComments(b.body), sqllex.StandardStrings)...)
		}
	}
	return statements
}

// commentLine matches a line that is wholly a comment in a shell, SQL
// or C-like language. A "--" counts only when a space or the end of the
// line follows it, so that a shell line starting with a long option,
// such as psql's --command, is kept.
var commentLine = regexp.MustCompile(`^\s*(#|//|--(\s|$))`)

// otherCommands returns the contents of the blocks that are not SQL,
// one entry per block with its whole-line comments removed, so that a
// command spread over continuation lines is still seen whole.
func otherCommands(blocks []block) []string {
	var commands []string
	for _, b := range blocks {
		if sqlLanguages[b.lang] {
			continue
		}
		var kept []string
		for _, line := range strings.Split(b.body, "\n") {
			if !commentLine.MatchString(line) {
				kept = append(kept, line)
			}
		}
		if text := strings.TrimSpace(strings.Join(kept, "\n")); text != "" {
			commands = append(commands, text)
		}
	}
	return commands
}

// closingFence returns the offset of the line that closes a block
// opened with fence, or -1 when the block runs to the end.
func closingFence(body, fence string) int {
	offset := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
			return offset + strings.Index(line, fence)
		}
		offset += len(line)
	}
	return -1
}

// stripComments replaces each SQL comment with a space, leaving quoted
// literals and identifiers intact.
func stripComments(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); {
		j := sqllex.SkipNonCode(sql, i, sqllex.StandardStrings)
		switch {
		case j == i:
			b.WriteByte(sql[i])
			i++
		case strings.HasPrefix(sql[i:], "--") || strings.HasPrefix(sql[i:], "/*"):
			b.WriteByte(' ')
			i = j
		default:
			b.WriteString(sql[i:j])
			i = j
		}
	}
	return b.String()
}

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package report

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/investigate"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/scoring"
)

func run(incident, target string, index int, passed bool, score float64, iters int) Run {
	r := Run{
		Incident: incident, Title: "Title of " + incident, Target: target, Index: index,
		Transcript: investigate.Transcript{
			Outcome: investigate.OutcomeCompleted, Iterations: iters,
			ToolCalls: make([]investigate.ToolCall, iters),
			Usage:     llm.TokenUsage{TotalTokens: 1000 * iters},
			Duration:  time.Duration(iters) * time.Second,
			FinalText: "The cause is `x`.\n\n```sql\nSELECT 1;\n```",
		},
		Result: scoring.Result{Passed: passed, Score: score, Checks: []scoring.Check{
			{Name: "investigation completed", Passed: true},
			{Name: "diagnosis mentions /a|b/", Passed: passed, Detail: "no match\nin the report"},
		}},
	}
	if !passed {
		r.Transcript.Outcome, r.Transcript.Error = investigate.OutcomeError, "LLM request\nfailed"
	}
	return r
}

func sampleResults() Results {
	return Results{
		StartedAt:    time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		FinishedAt:   time.Date(2026, 3, 1, 12, 30, 0, 0, time.UTC),
		PromptSource: "client/src/hooks/chat/chatConstants.ts",
		Runs: []Run{
			run("lock-pileup", "anthropic", 1, true, 1, 4),
			run("lock-pileup", "anthropic", 2, false, 0.5, 6),
			run("lock-pileup", "openai:gpt-x", 1, true, 1, 5),
			run("table-bloat", "anthropic", 1, true, 1, 3),
		},
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	got := Summarize(sampleResults().Runs)
	if len(got) != 3 {
		t.Fatalf("got %d summaries", len(got))
	}
	s := got[0]
	if s.Incident != "lock-pileup" || s.Target != "anthropic" || s.Runs != 2 || s.Passed != 1 || s.PassRate() != 0.5 {
		t.Fatalf("first summary %+v", s)
	}
	if s.Score.Mean != 0.75 || math.Abs(s.Score.StdDev-math.Sqrt(0.125)) > 1e-9 {
		t.Errorf("score stat %+v", s.Score)
	}
	if s.Iterations.Mean != 5 || s.Tokens.Mean != 5000 || s.Seconds.Mean != 5 || s.ToolCalls.Mean != 5 {
		t.Errorf("stats %+v", s)
	}
	if len(s.Checks) != 2 || s.Checks[1].Passed != 1 || s.Checks[1].Runs != 2 {
		t.Errorf("checks %+v", s.Checks)
	}
	if got[1].Target != "openai:gpt-x" || got[1].Score.StdDev != 0 {
		t.Errorf("second summary %+v", got[1])
	}
	if (Summary{}).PassRate() != 0 || stat(nil) != (Stat{}) {
		t.Error("empty summary or stat is not zero")
	}
}

func TestWriteMarkdown(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := WriteMarkdown(&b, sampleResults()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"# Ellie evaluation report",
		"system prompt from `client/src/hooks/chat/chatConstants.ts`",
		"| anthropic | 3 | 2 | 67% | 0.83 ± 0.29 |",
		"| lock-pileup | anthropic | 2 | 50% | 0.75 ± 0.35 | 5.0 ± 1.4 |",
		"### table-bloat (anthropic)",
		"| diagnosis mentions /a\\|b/ | 50% (1/2) |",
		"### lock-pileup (anthropic), run 2",
		"Outcome `error` after 6 iterations and 6 tool calls.",
		"Error: LLM request failed",
		"- Failed: diagnosis mentions /a|b/ (no match in the report)",
		"````markdown\nThe cause is `x`.\n\n```sql",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q\n%s", want, out)
		}
	}

	b.Reset()
	res := sampleResults()
	res.Runs = res.Runs[:1]
	if err := WriteMarkdown(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "## Failed runs\n\nNone.") {
		t.Errorf("report without failures:\n%s", b.String())
	}
}

func TestWriteComparison(t *testing.T) {
	t.Parallel()
	base := sampleResults()
	cand := sampleResults()
	cand.Runs = cand.Runs[:1]
	var b bytes.Buffer
	if err := WriteComparison(&b, []Labeled{{"base", base}, {"new|prompt", cand}}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"| Incident | base: anthropic | base: openai:gpt-x | new\\|prompt: anthropic |",
		"| lock-pileup | 50%, 0.75 ± 0.35 | 100%, 1.00 ± 0.00 | 100%, 1.00 ± 0.00 |",
		"| table-bloat | 100%, 1.00 ± 0.00 | - | - |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("comparison lacks %q\n%s", want, out)
		}
	}
}

func TestSaveAndLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "results.json")
	want := sampleResults()
	if err := Save(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 4 || got.Runs[1].Transcript.Iterations != 6 || !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("round trip lost data: %+v", got)
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("Load of a missing file succeeded")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("Load accepted malformed JSON")
	}
	if err := Save(filepath.Join(dir, "no", "such", "dir.json"), want); err == nil {
		t.Error("Save into a missing directory succeeded")
	}
	nan := want
	nan.Runs = []Run{{Result: scoring.Result{Score: math.NaN()}}}
	if err := Save(filepath.Join(dir, "nan.json"), nan); err == nil {
		t.Error("Save encoded NaN")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteErrors(t *testing.T) {
	t.Parallel()
	if err := WriteMarkdown(failingWriter{}, sampleResults()); err == nil {
		t.Error("WriteMarkdown ignored a write error")
	}
	if err := WriteComparison(failingWriter{}, []Labeled{{"a", sampleResults()}}); err == nil {
		t.Error("WriteComparison ignored a write error")
	}
}

func TestExcerpt(t *testing.T) {
	t.Parallel()
	if got := excerpt("short", 10); got != "short" {
		t.Errorf("excerpt = %q", got)
	}
	if got := excerpt("aé", 2); got != "a\n[truncated]" {
		t.Errorf("excerpt split a rune: %q", got)
	}
}

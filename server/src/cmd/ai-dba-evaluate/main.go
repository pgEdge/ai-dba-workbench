/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Command ai-dba-evaluate replays synthetic incidents against Ellie, the
// Workbench's AI assistant, and reports how reliably each model
// diagnoses them. See docs/developer-guide/evaluation/index.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/incident"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/investigate"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/report"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/runner"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/stage"
	"github.com/pgedge/ai-workbench/server/internal/evalharness/wbclient"
)

// Environment variables read by the run command. Credentials come from
// the environment so they never appear in a process listing.
const (
	envToken        = "AI_DBA_EVALUATE_TOKEN"
	envAdminDSN     = "AI_DBA_EVALUATE_ADMIN_DSN"
	envDatastoreDSN = "AI_DBA_EVALUATE_DATASTORE_DSN"
)

const usage = `Usage: ai-dba-evaluate <command> [flags]

Commands:
  run      Replay incidents against one or more models and write
           results.json and report.md
  report   Print the Markdown report for a results.json file
  compare  Compare results files side by side: compare [label=]FILE...

Run "ai-dba-evaluate <command> -h" for the flags of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

// run executes one command and returns the process exit status: 0 on
// success, 1 when the command failed and 2 for a usage error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "run":
		err = runCommand(ctx, args[1:], stdout, stderr, getenv)
	case "report":
		err = reportCommand(args[1:], stdout, stderr)
	case "compare":
		err = compareCommand(args[1:], stdout, stderr)
	case "help", "-h", "-help", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	var ue usageError
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &ue):
		_, _ = fmt.Fprintf(stderr, "ai-dba-evaluate %s: %v\n", args[0], err)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "ai-dba-evaluate %s: %v\n", args[0], err)
		return 1
	}
}

// usageError marks a mistake in the command line.
type usageError struct{ error }

func usagef(format string, a ...any) error {
	return usageError{fmt.Errorf(format, a...)}
}

// listFlag collects a repeatable flag, also splitting each value on
// commas.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(v string) error {
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			*l = append(*l, s)
		}
	}
	return nil
}

type runOptions struct {
	serverURL      string
	token          string
	adminDSN       string
	datastoreDSN   string
	incidentsDir   string
	only           listFlag
	targets        listFlag
	runs           int
	maxIterations  int
	promptSource   string
	out            string
	requestTimeout time.Duration
}

func parseRunFlags(args []string, stderr io.Writer, getenv func(string) string) (*runOptions, error) {
	o := &runOptions{}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.serverURL, "server-url", "http://127.0.0.1:8080", "base URL of the Workbench server")
	fs.StringVar(&o.incidentsDir, "incidents", "evaluation/incidents", "directory of incident fixtures")
	fs.Var(&o.only, "only", "incident IDs to run, comma-separated or repeated (default all)")
	fs.Var(&o.targets, "target", "provider[:model] to evaluate, repeatable (default: the server's default model)")
	fs.IntVar(&o.runs, "runs", 3, "repetitions of each incident per target")
	fs.IntVar(&o.maxIterations, "max-iterations", 50, "maximum LLM calls per investigation")
	fs.StringVar(&o.promptSource, "system-prompt", "client/src/hooks/chat/chatConstants.ts",
		"Ellie's system prompt: the client's chatConstants.ts, or a text file holding a candidate prompt")
	fs.StringVar(&o.out, "out", "evaluation/results", "directory for results.json and report.md")
	fs.DurationVar(&o.requestTimeout, "request-timeout", 5*time.Minute, "timeout for each request to the server")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: ai-dba-evaluate run [flags]\n\n"+
			"The server token and DSNs are read from %s, %s and %s.\n\nFlags:\n",
			envToken, envAdminDSN, envDatastoreDSN)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		return nil, usageError{err}
	}
	if fs.NArg() > 0 {
		return nil, usagef("unexpected arguments %q", fs.Args())
	}
	o.token, o.adminDSN, o.datastoreDSN = getenv(envToken), getenv(envAdminDSN), getenv(envDatastoreDSN)
	for name, v := range map[string]string{envToken: o.token, envAdminDSN: o.adminDSN, envDatastoreDSN: o.datastoreDSN} {
		if v == "" {
			return nil, usagef("%s is not set", name)
		}
	}
	if o.runs < 1 || o.maxIterations < 1 {
		return nil, usagef("-runs and -max-iterations must be at least 1")
	}
	if len(o.targets) == 0 {
		o.targets = listFlag{"default"}
	}
	return o, nil
}

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	o, err := parseRunFlags(args, stderr, getenv)
	if err != nil {
		return err
	}
	targets := make([]investigate.Target, 0, len(o.targets))
	for _, s := range o.targets {
		t, err := investigate.ParseTarget(s)
		if err != nil {
			return usageError{err}
		}
		targets = append(targets, t)
	}
	all, err := incident.LoadDir(o.incidentsDir)
	if err != nil {
		return err
	}
	incidents, err := incident.Select(all, o.only)
	if err != nil {
		return usageError{err}
	}
	prompt, err := investigate.LoadSystemPrompt(o.promptSource)
	if err != nil {
		return err
	}

	client := wbclient.New(o.serverURL, o.token, o.requestTimeout)
	// Fail fast on a wrong URL or token rather than once per run.
	if err := client.Do(ctx, http.MethodGet, "/api/v1/mcp/tools", nil, nil); err != nil {
		return fmt.Errorf("checking the server: %w", err)
	}
	if err := os.MkdirAll(o.out, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	r := &runner.Runner{
		Stage: runner.FromStager(&stage.Stager{
			AdminDSN:     o.adminDSN,
			DatastoreDSN: o.datastoreDSN,
			Registry:     &stage.APIRegistry{Client: client},
		}),
		Investigator:  &investigate.ChatLoop{Client: client, SystemPrompt: prompt},
		Targets:       targets,
		Runs:          o.runs,
		MaxIterations: o.maxIterations,
		Progress:      stdout,
	}
	res := report.Results{StartedAt: time.Now().UTC(), PromptSource: o.promptSource}
	runs, runErr := r.Run(ctx, incidents)
	res.FinishedAt = time.Now().UTC()
	res.Runs = runs

	// Keep whatever completed, even when the run stopped early.
	if err := writeResults(o.out, res); err != nil {
		return errors.Join(runErr, err)
	}
	_, _ = fmt.Fprintf(stdout, "Wrote %s and %s\n",
		filepath.Join(o.out, "results.json"), filepath.Join(o.out, "report.md"))
	return runErr
}

func writeResults(dir string, res report.Results) error {
	if err := report.Save(filepath.Join(dir, "results.json"), res); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "report.md")) //nolint:gosec // G304: the output directory comes from the operator
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	if err := report.WriteMarkdown(f, res); err != nil {
		_ = f.Close()
		return fmt.Errorf("write report: %w", err)
	}
	return f.Close()
}

func reportCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: ai-dba-evaluate report RESULTS.json")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
	}
	if fs.NArg() != 1 {
		return usagef("expected one results file")
	}
	res, err := report.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	return report.WriteMarkdown(stdout, res)
}

func compareCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: ai-dba-evaluate compare [label=]RESULTS.json...\n\n"+
			"Each file's columns are headed by its label, or by its path when no label is given.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
	}
	if fs.NArg() == 0 {
		return usagef("expected at least one results file")
	}
	sets := make([]report.Labeled, 0, fs.NArg())
	for _, arg := range fs.Args() {
		label, path, ok := strings.Cut(arg, "=")
		if !ok {
			label, path = arg, arg
		}
		res, err := report.Load(path)
		if err != nil {
			return err
		}
		sets = append(sets, report.Labeled{Label: label, Results: res})
	}
	return report.WriteComparison(stdout, sets)
}

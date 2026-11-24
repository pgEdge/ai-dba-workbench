# Ellie Evaluation Harness

The evaluation harness measures how reliably Ellie, the Workbench's AI
assistant, diagnoses database incidents. The harness stages each
synthetic incident on a disposable PostgreSQL instance, asks Ellie to
investigate it through the server's LLM proxy and tools, and scores the
final report with deterministic checks. Each incident runs several times
per model, so the report shows pass rates and their variance rather than
a single result that could be luck.

Use the harness to compare models, to check a change to Ellie's system
prompt before it ships, and to watch for regressions over time.

## How a Run Works

The `ai-dba-evaluate` command drives each run through five steps. For
every incident, every target model and every repetition, the harness:

1. creates a fresh database for the incident, runs the fixture's setup
   SQL and registers the database as a Workbench connection that is
   neither shared nor monitored; the connection logs in as a temporary
   role with `pg_monitor` and `pg_read_all_data` rather than as a
   superuser, so Ellie works with a monitoring user's privileges;
2. inserts the fixture's alert and recorded metric history into the
   datastore, and starts any workload sessions, waiting until blocking
   sessions are seen waiting on their locks;
3. sends the incident prompt to Ellie and runs the same agentic loop as
   the web client, calling the server's tools until the model writes its
   final report or reaches the iteration limit;
4. scores the transcript against the fixture's expectations;
5. stops the workload, removes the connection (and with it the alert
   and metric history), drops the database and the role, and runs the
   fixture's teardown SQL.

Runs are sequential, because some incidents change instance-wide state
such as replication slots or `ALTER SYSTEM` settings. The harness reads
Ellie's system prompt from `client/src/hooks/chat/chatConstants.ts` by
default, so it always tests the prompt the chat panel ships.

Fixtures change settings with `ALTER SYSTEM`, drop replication slots
and add a default partition to datastore metrics tables, so the harness
refuses to stage an incident on an instance that might matter. Both
`AI_DBA_EVALUATE_ADMIN_DSN` and `AI_DBA_EVALUATE_DATASTORE_DSN` must
name only `localhost` or loopback addresses, and each instance must
hold a database named `ai_dba_evaluate_disposable`; the
`run-evaluation.sh` script creates that database in its throwaway
container.

## Running the Harness

The harness needs Docker, Go and the `pg_isready` client tool, plus an
API key for each provider you evaluate. Name the models to evaluate in
`EVALUATE_TARGETS` as space-separated `provider:model` values; the first
target is also the server's default model. Supply the matching
credentials in `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`
or `OLLAMA_URL`.

In the following example, the harness compares two models with five
runs of each incident:

```bash
export EVALUATE_TARGETS="anthropic:<model> openai:<model>"
export ANTHROPIC_API_KEY=... OPENAI_API_KEY=...
make evaluate EVALUATE_ARGS="-runs 5"
```

The `make evaluate` target runs `evaluation/scripts/run-evaluation.sh`,
which starts PostgreSQL 17 in Docker, builds and starts the server
against it, creates a temporary user and service token, replays the
incidents and tears everything down. The script writes each API key to
a file that only the current user can read, and deletes the files when
it exits. The target is not part of `make test-all`, because a run is
slow, costs money and gives different results each time.

The script accepts these environment variables besides the credentials:

- `EVALUATE_OUT` sets the output directory; the default is
  `evaluation/results`.
- `EVALUATE_DB_PORT` and `EVALUATE_SERVER_PORT` set the PostgreSQL and
  server ports; the defaults are `55433` and `8090`.
- `EVALUATE_KEEP_STACK=1` leaves the stack running after the run so
  that you can inspect it; the API key files are still deleted.

`EVALUATE_ARGS` passes flags to `ai-dba-evaluate run`. Use `-only` with
a comma-separated list of incident IDs to replay a subset, `-runs` to
set the repetitions (the default is 3), `-max-iterations` to bound the
LLM calls in one investigation (the default is 50) and `-system-prompt`
to evaluate a candidate prompt held in a plain text file.

In the following example, the harness evaluates a candidate prompt
against one incident:

```bash
make evaluate EVALUATE_ARGS="-only lock-pileup -system-prompt /tmp/prompt.txt"
```

## Reading the Results

Each run writes `results.json`, which holds every transcript with its
tool calls, token usage and check results, and `report.md`. The report
summarises the pass rate and mean score per target, then per incident
and target with the standard deviation across runs, then the pass rate
of each individual check. A final section lists each failed run with
the checks it failed and an excerpt of its report.

A run passes only when every check passes; its score is the fraction of
checks that passed, and is 0 when the investigation did not finish with a
report, for example when it failed or reached the iteration limit.

Two commands work with saved results. `ai-dba-evaluate report` prints
the Markdown report for a `results.json` file again, and
`ai-dba-evaluate compare` puts several results files side by side, one
column per file and target:

```bash
./bin/ai-dba-evaluate compare before=old/results.json after=evaluation/results/results.json
```

## Running in CI

The [`.github/workflows/evaluation.yml`][evaluation-yml] workflow runs
the harness weekly and on demand; it never runs on pushes or pull
requests. The scheduled run happens only once a maintainer sets the
`EVALUATE_TARGETS` repository variable, and a manual run can override
the targets, the number of runs and the incidents. The workflow reads
the API keys from the `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`GEMINI_API_KEY` and `OLLAMA_URL` repository secrets, publishes the
report to the job summary and uploads the results as an artifact.

[evaluation-yml]: https://github.com/pgEdge/ai-dba-workbench/blob/main/.github/workflows/evaluation.yml

## Writing an Incident Fixture

Each incident is a YAML file in `evaluation/incidents`. Fixtures must be
entirely synthetic: invent every name, value and log line, and use
reserved names such as `example.com` for anything that looks like an
address. Never copy data, hostnames or logs from a real system.

A fixture contains the following fields:

- `id`, `title`, `category` and `summary` identify the incident; the
  summary explains the cause and what a good investigation finds, for
  the benefit of reviewers, and is never shown to the model.
- `setup` is SQL that builds the incident in its fresh database; the
  harness runs each statement separately, so statements that cannot run
  inside a transaction block, such as `VACUUM`, work.
- `workload` lists sessions that stay connected during the
  investigation; each has a `name`, its `sql`, and `blocks: true` when
  the SQL is expected to wait on a lock.
- `teardown` is SQL that undoes instance-wide changes, such as
  `ALTER SYSTEM RESET` or dropping a replication slot; it runs in the
  instance's maintenance database after the incident database is
  dropped.
- `metrics` lists recorded history for datastore tables under the
  `metrics` schema; each sample gives its age (`ago`, such as `45m`) and
  its column values, and the harness fills in `connection_id` and
  `collected_at`.
- `alert` describes the alert that opens the investigation: its
  `title`, `description`, `severity`, `probe`, `metric`, `value`,
  `threshold`, `operator`, `object` and `triggered_ago`.
- `prompt` optionally replaces the default investigation request; it
  can refer to `{{connection_id}}`, `{{connection_name}}`,
  `{{database}}`, `{{alert_title}}` and `{{alert_description}}`.
- `expect` holds the checks, described below.
- `reference` is a model answer that must pass the fixture's own
  checks; a unit test enforces this.

The `expect` field holds three kinds of check. `diagnosis` applies
case-insensitive regular expressions to the final report: every
`all_of` pattern must match, at least one `any_of` pattern must match,
and no `none_of` pattern may match. `actions` applies patterns to the
commands the report proposes in fenced code blocks. At least one SQL
statement, taken from the `sql` blocks with comments removed, must
match an `allowed` pattern. No `forbidden` pattern may match either a
SQL statement or the contents of any other block, whatever its
language, with whole-line comments removed; this catches a destructive
command such as `pg_resetwal` in a shell block. `tools` lists the tools
the investigation must call at least once.

Write patterns that accept every reasonable phrasing. Models wrap and
hyphenate freely, so match whitespace with `\s+` rather than a literal
space, and use forms such as `idle[\s-]+in[\s-]+transaction`. Use
`none_of` for known wrong diagnoses and `forbidden` for destructive
actions such as `\bdrop\s+table\b`. Make an `allowed` pattern specific
enough that a fix aimed at the wrong object fails, such as terminating
the waiting sessions rather than the blocker. Add a plausible wrong
report for the fixture to `wrongReports` in
`server/src/internal/evalharness/scoring/scoring_test.go`, which checks
that each one fails the check it names.

Before opening a pull request with a new fixture, run the unit tests,
which load every fixture and check its reference report, then replay
the new incident a few times against at least one model:

```bash
(cd server/src && go test ./internal/evalharness/...)
make evaluate EVALUATE_ARGS="-only <id> -runs 3"
```

## Code Layout

The harness lives in the server module. The command is in
`server/src/cmd/ai-dba-evaluate`, and its packages are under
`server/src/internal/evalharness`:

- `incident` loads and validates fixtures and renders the prompt.
- `stage` stages and tears down an incident on PostgreSQL.
- `investigate` runs the investigation; `ChatLoop` mirrors the web
  client's agentic loop.
- `scoring` applies the deterministic checks.
- `runner` replays incidents across targets and repetitions.
- `report` aggregates results and writes the Markdown report.

The `investigate.Investigator` interface is the extension point for
other ways of running Ellie. Today the only implementation is
`ChatLoop`, which drives the loop from the harness the same way the web
client does; when Ellie's loop moves to the server, a second
implementation can call it, and the fixtures, scoring and reports stay
as they are.

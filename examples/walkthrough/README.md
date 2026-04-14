<!--
  pgEdge AI DBA Workbench

  Copyright (c) 2025 - 2026, pgEdge, Inc.
  This software is released under The PostgreSQL License
-->

# AI DBA Workbench — Guided Walkthrough

This walkthrough launches the AI DBA Workbench with a
pre-seeded demo database and guides you through every
feature. One command gets you from zero to a working
monitoring dashboard in under three minutes. An in-browser
tour walks you through 24 steps covering monitoring, AI
analysis, administration, and alerting in 15 to 18 minutes.

At the end of the tour, you can connect your own PostgreSQL
database and keep the workbench running. You can also clean
up all containers and data with a single command.

## Table of Contents

- [Quick Start](#quick-start)
- [Prerequisites](#prerequisites)
- [What You Will Experience](#what-you-will-experience)
- [Login Credentials](#login-credentials)
- [Tour Sections](#tour-sections)
- [Cleaning Up](#cleaning-up)
- [Development](#development)
- [File Structure](#file-structure)
- [Troubleshooting](#troubleshooting)

## Quick Start

Run the following command in Terminal (macOS/Linux) or
Git Bash (Windows).

```bash
curl -fsSL \
  https://raw.githubusercontent.com/pgEdge/ai-dba-workbench/main/examples/walkthrough/install.sh \
  | bash
```

## Prerequisites

The walkthrough requires the following tools and resources:

- [Docker Engine 24.0+](https://docs.docker.com/get-docker/)
  or Docker Desktop provides container runtime support.
- On Windows,
  [Git for Windows](https://gitforwindows.org/) provides
  Git Bash; Docker Desktop for Windows provides the
  container runtime.
- Approximately 4 GB of available RAM allows Docker to run
  all services.
- Ports 3000 and 8080 are used by default; the setup script
  finds available alternatives automatically if they are busy.
- An API key for Anthropic, OpenAI, or Google Gemini is
  optional; the script prompts you during setup. Ollama
  works locally without a key.

## What You Will Experience

The walkthrough consists of three phases.

1. Install (terminal, approximately 2-3 minutes): The script
   downloads files, starts 7 Docker containers, and opens
   the browser.
2. Guided Tour (browser, approximately 15-18 minutes): A
   Driver.js overlay walks you through every feature.
3. Make It Yours (optional): Add your own database, keep
   exploring, or clean up all resources.

## Login Credentials

The walkthrough stack creates a default administrator
account with the following credentials:

- Username: `admin`
- Password: `DemoPass2026`

Enter these credentials on the login page. The guided tour
starts automatically after you log in.

## Tour Sections

The in-browser tour covers six sections across 24 steps.

- Welcome and Login introduces the workbench and
  authenticates with the demo account.
- Monitoring Dashboard explores real-time metrics, charts,
  and server health indicators.
- AI Analysis demonstrates intelligent query analysis and
  optimization recommendations.
- Database Administration covers connection management,
  configuration, and cluster settings.
- Alerting and Notifications walks through threshold
  configuration, alert history, and blackout windows.
- Make It Yours offers the option to add a real database
  or clean up the demo environment.

## Cleaning Up

The following commands stop all containers and remove the
walkthrough data from your machine.

```bash
cd pgedge-workbench-walkthrough/examples/walkthrough
docker compose down -v
cd ../../..
rm -rf pgedge-workbench-walkthrough
```

## Development

This section explains how to modify the guided tour.

### Editing Tour Steps

The Driver.js tour definition lives in the `nginx/walkthrough/`
directory. Edit `tour.js` to add, remove, or reorder steps.
Custom styles for the tour overlay live in `tour.css`.

### Regenerating the Datastore Seed

The seed directory contains tiered snapshot files
(`datastore-seed-4h.sql`, `datastore-seed-8h.sql`,
`datastore-seed-16h.sql`, `datastore-seed-24h.sql`) with
collector metrics and alert history. To regenerate the seed
data, run the full stack for the desired duration and then
dump the datastore with `pg_dump`.

### Rebasing Timestamps

The `seed/rebase-timestamps.sh` script shifts all pre-baked
metric timestamps so the data appears to have been collected
recently. The `guide.sh` script runs the rebase automatically
after starting the stack.

### LLM Configuration

The `guide.sh` script prompts for an LLM provider and API
key during initial setup. It supports Anthropic, OpenAI,
Google Gemini, and Ollama. Run `guide.sh` again to change
the LLM configuration on an existing stack.

## File Structure

The walkthrough directory contains the following files.

```text
examples/walkthrough/
├── README.md
├── docker-compose.yml
├── install.sh
├── runner.sh
├── setup.sh
├── config/
│   ├── ai-dba-alerter.yaml
│   ├── ai-dba-collector.yaml
│   └── ai-dba-server.yaml
├── nginx/
│   ├── nginx.conf
│   └── walkthrough/
│       ├── driver.min.css
│       ├── driver.min.js
│       ├── images/
│       ├── loader.js
│       ├── tour.css
│       └── tour.js
├── secret/
│   └── .gitkeep
└── seed/
    ├── datastore-seed-4h.sql
    ├── datastore-seed-8h.sql
    ├── datastore-seed-16h.sql
    ├── datastore-seed-24h.sql
    ├── demo-schema.sql
    ├── rebase-timestamps.sh
    └── workload.sh
```

## Troubleshooting

This section covers common issues and their solutions.

### Port Conflict

The `guide.sh` script detects port conflicts automatically
and selects available alternatives. No manual configuration
is required.

### Docker Memory

The stack requires approximately 4 GB of memory. Open
Docker Desktop and navigate to Settings, then Resources,
to allocate at least 4 GB.

### LLM Provider Issues

An LLM API key is not required to complete the tour. Run
`guide.sh` again and choose option 4 to change the LLM
configuration on a running stack.

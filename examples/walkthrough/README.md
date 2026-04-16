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
tour walks you through 33 steps covering monitoring, AI
analysis, administration, alerting, and blackout scheduling
in 15 to 20 minutes.

A database with known problems and several hours of
pre-seeded runtime metrics are included for illustrative
purposes. At the end of the tour, you can connect your own
PostgreSQL database and keep the workbench running.

## Table of Contents

- [Quick Start](#quick-start)
- [Running from a Clone](#running-from-a-clone)
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
  https://raw.githubusercontent.com/AntTheLimey/ai-dba-walkthrough/main/install.sh \
  | bash
```

## Running from a Clone

You can also clone this repository and run the walkthrough
directly. This is useful for development or when you want
to modify the tour.

```bash
git clone https://github.com/AntTheLimey/ai-dba-walkthrough.git
cd ai-dba-walkthrough
bash guide.sh
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
  finds available alternatives automatically if they are
  busy.
- An API key for Anthropic, OpenAI, or Google Gemini is
  optional; the script prompts you during setup. AI features
  work without a key; you just will not see live AI
  analysis.

## What You Will Experience

The walkthrough consists of three phases.

1. Install (terminal, approximately 2-3 minutes): The script
   downloads files, pulls pre-built Docker images, starts 7
   containers, and opens the browser.
2. Guided Tour (browser, approximately 15-20 minutes): A
   Driver.js overlay walks you through 33 steps covering
   every major feature.
3. Make It Yours (optional): Add your own database using
   the built-in Add Server dialog, keep exploring the demo,
   or clean up all resources.

## Login Credentials

The walkthrough stack creates a default administrator
account with the following credentials:

- Username: `admin`
- Password: `DemoPass2026`

The login fields are pre-filled automatically. Click
Sign In to start the tour.

## Tour Sections

The in-browser tour covers seven sections.

- **The Big Picture** introduces the estate dashboard,
  navigator, and server selection.
- **Diagnosing a Problem** explores the AI overview, event
  timeline, server metrics, active alerts, and AI alert
  analysis.
- **Ask Ellie** demonstrates the AI chat assistant with
  natural language queries, SQL execution, and follow-up
  questions.
- **How It's Configured** covers probe defaults, alert
  rules, email and Slack notification channels.
- **Server Settings** shows per-server configuration for
  alert overrides, probe intervals, and notification
  channels.
- **Blackout Windows** walks through blackout scheduling
  with one-time and recurring maintenance windows.
- **Who Can Access What** covers user management, API
  tokens, and AI memories.

## Cleaning Up

Run `guide.sh` again and choose the clean-up option. Or
run the following commands to stop all containers and
remove the walkthrough data.

```bash
cd pgedge-workbench-walkthrough/examples/walkthrough
docker compose down -v
cd ../../..
rm -rf pgedge-workbench-walkthrough
```

## Development

This section explains how to modify the guided tour.

### Editing Tour Steps

The Driver.js tour definition lives in the
`nginx/walkthrough/` directory. Edit `tour.js` to add,
remove, or reorder steps. Custom styles for the tour
overlay live in `tour.css`.

### Regenerating the Datastore Seed

The seed directory contains tiered snapshot files
(`datastore-seed-4h.sql`, `datastore-seed-8h.sql`,
`datastore-seed-16h.sql`, `datastore-seed-24h.sql`) with
collector metrics and alert history. To regenerate the
seed data, run the full stack for the desired duration
and then dump the datastore with `pg_dump`.

### Rebasing Timestamps

The `seed/rebase-timestamps.sh` script shifts all
pre-baked metric timestamps so the data appears to have
been collected recently. The `guide.sh` script runs the
rebase automatically after starting the stack.

### LLM Configuration

The `guide.sh` script prompts for an LLM provider and
API key during initial setup. It supports Anthropic,
OpenAI, and Google Gemini. Run `guide.sh` again to change
the LLM configuration on an existing stack.

## File Structure

```text
├── README.md
├── docker-compose.yml
├── guide.sh
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

An LLM API key is not required to complete the tour. AI
features like Ask Ellie and alert analysis will be
unavailable without a key. Run `guide.sh` again and
choose option 4 to change the LLM configuration on a
running stack.

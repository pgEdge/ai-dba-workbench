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

In the following example, the `install.sh` script downloads
the repository, starts the Docker stack, and opens the
walkthrough in your browser.

```bash
curl -fsSL \
  https://raw.githubusercontent.com/pgEdge/ai-dba-workbench/main/examples/walkthrough/install.sh \
  | bash
```

## Prerequisites

The walkthrough requires the following tools and resources:

- [Docker Engine 24.0+](https://docs.docker.com/get-docker/)
  or Docker Desktop provides container runtime support.
- Approximately 4 GB of available RAM allows Docker to run
  all services.
- Ports 3000 and 8080 must be available on the host machine.
- An [Anthropic API key](https://console.anthropic.com/) is
  optional; the tour prompts you to add the key later.

## What You Will Experience

The walkthrough consists of three phases.

1. Install (terminal, approximately 2-3 minutes): The script
   downloads files, starts the Docker stack, and opens the
   browser.
2. Guided Tour (browser, approximately 15-18 minutes): A
   Driver.js overlay walks you through every feature.
3. Make It Yours (optional): Connect your own database or
   clean up all resources.

## Login Credentials

The walkthrough stack creates a default administrator
account with the following credentials:

- Username: `admin`
- Password: `Demo2026!`

The guided tour enters these credentials for you during the
login step.

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
- Make It Yours offers the option to connect a real
  database or clean up the demo environment.

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

The `seed/datastore-seed.sql` file contains a snapshot of
collector metrics and alert history. To regenerate the seed
data, run the full stack for two to three hours and then dump
the datastore with `pg_dump`.

### Helper Sidecar

The `helper/` directory contains a Python sidecar API that
handles API key injection and connection management during
the tour. The helper communicates with the workbench server
over the internal Docker network.

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
├── helper/
│   ├── Dockerfile
│   └── server.py
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
    ├── datastore-seed.sql
    ├── demo-schema.sql
    └── workload.sh
```

## Troubleshooting

This section covers common issues and their solutions.

### Port Conflict

If ports 3000 or 8080 are already in use, set the
`WT_CLIENT_PORT` and `WT_SERVER_PORT` environment variables
before running the install script.

```bash
export WT_CLIENT_PORT=3001
export WT_SERVER_PORT=8081
```

### Docker Memory

The stack requires approximately 4 GB of memory. Open
Docker Desktop and navigate to Settings, then Resources,
to allocate at least 4 GB.

### API Key Issues

An Anthropic API key is not required to complete the tour.
The tour prompts you to add a key during the AI Analysis
section. You can also add the key later through the
workbench settings page.

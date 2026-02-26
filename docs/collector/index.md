# pgEdge AI DBA Workbench Collector Documentation

Welcome to the pgEdge AI DBA Workbench Collector documentation. The Collector
is a standalone monitoring service that continuously collects metrics from
PostgreSQL servers and stores them in a centralized datastore for analysis by
the AI DBA Workbench system.

## Table of Contents

### Getting Started

- [Overview](overview.md) - High-level architecture and key concepts
- [Quick Start Guide](quickstart.md) - Get up and running quickly
- [Configuration Guide](configuration.md) - Configuration with examples

### Architecture and Design

- [System Architecture](architecture.md) - Detailed system design
- [Database Schema](schema.md) - Schema structure and design
- [Schema Management](schema-management.md) - Migration system
- [Probes System](probes.md) - How probes work
- [Scheduler](scheduler.md) - Probe scheduling and execution
- [Node Role Probe Design](node-role-probe-design.md) - Cluster topology
    detection

### Development

- [Development Guide](development.md) - Setting up for development
- [Testing Guide](testing.md) - Running and writing tests
- [Adding New Probes](adding-probes.md) - Creating custom probes

### Reference

- [Configuration Reference](config-reference.md) - All configuration options
- [Probe Reference](probe-reference.md) - List of available probes
- [pg_settings Usage Guide](pg-settings-usage.md) - Examples and best
    practices for configuration tracking

## Quick Links

- [Main Documentation](../index.md) returns to the main documentation index.
- [Main README][readme] provides quick start and basic information.
- [Example Configuration][example] shows a sample collector configuration file.

[readme]: https://github.com/pgEdge/ai-dba-workbench/blob/main/README.md
[example]: https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-collector.yaml

## Key Features

The Collector provides the following capabilities:

- Multi-server monitoring collects metrics from multiple PostgreSQL servers
  simultaneously with independent connection pools.
- The system includes 34 built-in probes that provide comprehensive coverage
  of PostgreSQL system views and statistics.
- Flexible scheduling allows configurable collection intervals per probe.
- Automated data management handles weekly partitioning and retention-based
  garbage collection.
- Secure connections protect passwords using AES-256-GCM encryption with
  PBKDF2 key derivation and support SSL/TLS connections.
- Efficient connection pooling manages connections for both the datastore
  and monitored servers.
- Graceful shutdown ensures proper cleanup of all connections and resources.

## Getting Help

- Check the documentation in this directory
- Review the sample configuration file
- Examine the test files for examples

## Version

This documentation corresponds to Collector version 0.1.0.

# pgEdge AI DBA Workbench Alerter Documentation

Welcome to the pgEdge AI DBA Workbench Alerter documentation. The Alerter is
a standalone background service that monitors collected metrics for threshold
violations and uses AI-powered anomaly detection to generate alerts.

## Table of Contents

### Getting Started

- [Overview](overview.md) - High-level architecture and key concepts.
- [Quick Start Guide](quickstart.md) - Get up and running quickly.
- [Configuration Reference](configuration.md) - All configuration options.

### Architecture and Design

- [System Architecture](architecture.md) - Detailed system design.
- [Alert Rules](alert-rules.md) - Built-in and custom alert rules.
- [Anomaly Detection](anomaly-detection.md) - AI-powered anomaly detection.

### Scheduling

- [Cron Expressions](cron-expressions.md) - Cron syntax for blackout schedules.

### Development

- [Development Guide](development.md) - Setting up for development.
- [Testing Guide](testing.md) - Running and writing tests.
- [Adding Alert Rules](adding-rules.md) - Creating custom alert rules.

### Reference

- [Alert Rule Reference](rule-reference.md) - List of built-in alert rules.

## Quick Links

- [Main Documentation](../index.md) returns to the main documentation index.
- [Main README][readme] provides quick start and basic information.
- [Example Configuration][example] shows a sample alerter configuration file.

[readme]: https://github.com/pgEdge/ai-dba-workbench/blob/main/README.md
[example]: https://github.com/pgEdge/ai-dba-workbench/blob/main/examples/ai-dba-alerter.yaml

## Key Features

The Alerter provides the following capabilities:

- Threshold-based alerts evaluate collected metrics against configurable
  limits and trigger alerts when thresholds are exceeded.
- Per-connection overrides allow you to customize thresholds for specific
  monitored connections.
- AI-powered anomaly detection uses a tiered approach with statistical
  analysis, embedding similarity, and LLM classification.
- Blackout periods suppress alerts during scheduled maintenance windows
  using manual or cron-based scheduling.
- Automatic alert clearing resolves alerts when conditions return to normal.
- Alert lifecycle management tracks alert states including triggered,
  acknowledged, and cleared.
- Retention management automatically purges old alerts based on configurable
  retention policies.
- Built-in alert rules provide 24 pre-configured rules for common PostgreSQL
  monitoring scenarios.

## Anomaly Detection Tiers

The Alerter implements a tiered anomaly detection system:

- Tier 1 uses statistical analysis with z-score calculations to detect
  deviations from baseline metrics.
- Tier 2 performs embedding similarity search using pgvector to identify
  patterns matching known anomalies.
- Tier 3 employs LLM classification through Ollama, OpenAI, Anthropic, or
  Voyage providers for complex anomaly analysis.

## Getting Help

- Check the documentation in this directory.
- Review the sample configuration file.
- Examine the test files for examples.

## Version

This documentation corresponds to Alerter version 0.1.0.

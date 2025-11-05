# pgEdge AI Workbench Collector Documentation

Welcome to the pgEdge AI Workbench Collector documentation. The Collector is
a standalone monitoring service that continuously collects metrics from
PostgreSQL servers and stores them in a centralized datastore for analysis by
the AI Workbench system.

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

### Development

- [Development Guide](development.md) - Setting up for development
- [Testing Guide](testing.md) - Running and writing tests
- [Adding New Probes](adding-probes.md) - Creating custom probes

### Reference

- [Configuration Reference](config-reference.md) - All configuration options
- [Probe Reference](probe-reference.md) - List of available probes

## Quick Links

- [Main README](../README.md) - Quick start and basic information
- [Sample Configuration](../../configs/ai-workbench.conf.sample)
- [Project Design Document](../../DESIGN.md)

## Key Features

The Collector provides:

- **Multi-Server Monitoring**: Monitor multiple PostgreSQL servers
  simultaneously with independent connection pools
- **24 Built-in Probes**: Comprehensive coverage of PostgreSQL system views
  and statistics
- **Flexible Scheduling**: Configurable collection intervals per probe
- **Automated Data Management**: Weekly partitioning and retention-based
  garbage collection
- **Secure Connections**: Password encryption using AES-256-GCM, SSL/TLS
  support
- **Connection Pooling**: Efficient connection management for both datastore
  and monitored servers
- **Graceful Shutdown**: Proper cleanup of all connections and resources

## Getting Help

- Check the documentation in this directory
- Review the sample configuration file
- Examine the test files for examples
- Consult the main DESIGN.md for system-wide architecture

## Version

This documentation corresponds to Collector version 0.1.0.

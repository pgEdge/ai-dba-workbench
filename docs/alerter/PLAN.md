# Alerter Component Implementation Plan

> Implementation plan for the AI DBA Workbench alerter component.
> This document is for review before implementation begins.

## Overview

The alerter is a **standalone service** (`ai-dba-alerter`) that provides
continuous monitoring of collected metrics with two complementary detection
mechanisms:

1. Traditional threshold-based alerts provide deterministic rules for common
   conditions (server down, disk space low, replication lag, etc.).

2. AI-powered anomaly detection provides tiered statistical and LLM-based
   detection for complex patterns that threshold rules cannot capture.

The alerter exposes a **REST API** for management operations with **RBAC**
permission checks. It is designed as a separate binary to enable independent
scaling and deployment flexibility.

## Architecture

```
┌───────────────────────────────────────────────────────────────────────────┐
│                         Alerter Binary (ai-dba-alerter)                    │
│                                                                            │
│  ┌──────────────────────────────────────────────────────────────────────┐ │
│  │                           REST API Layer                              │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐   │ │
│  │  │ /api/alerts  │  │/api/blackouts│  │ /api/alert-rules         │   │ │
│  │  │              │  │              │  │ /api/metric-definitions  │   │ │
│  │  └──────────────┘  └──────────────┘  └──────────────────────────┘   │ │
│  │                         │                                            │ │
│  │  ┌──────────────────────▼────────────────────────────────────────┐  │ │
│  │  │              RBAC Authorization (via auth.db)                  │  │ │
│  │  └────────────────────────────────────────────────────────────────┘  │ │
│  └──────────────────────────────────────────────────────────────────────┘ │
│                                                                            │
│  ┌──────────────────────────────────────────────────────────────────────┐ │
│  │                        Alert Processing Engine                        │ │
│  │  ┌─────────────────────┐  ┌─────────────────────────────────────┐   │ │
│  │  │  Threshold Engine   │  │       Anomaly Detection             │   │ │
│  │  │  (deterministic)    │  │  ┌───────────────────────────────┐  │   │ │
│  │  │                     │  │  │ Tier 1: Statistical           │  │   │ │
│  │  │  - Server health    │  │  │ (z-score, EWMA)              │  │   │ │
│  │  │  - Disk space       │  │  └────────────┬──────────────────┘  │   │ │
│  │  │  - Replication lag  │  │               │ candidates          │   │ │
│  │  │  - Connection count │  │  ┌────────────▼──────────────────┐  │   │ │
│  │  │  - Lock waits       │  │  │ Tier 2: Embedding/RAG         │  │   │ │
│  │  │  - ...              │  │  │ (pgvector similarity)         │  │   │ │
│  │  └──────────┬──────────┘  │  └────────────┬──────────────────┘  │   │ │
│  │             │             │               │ suspicious          │   │ │
│  │             │             │  ┌────────────▼──────────────────┐  │   │ │
│  │             │             │  │ Tier 3: LLM Classification    │  │   │ │
│  │             │             │  │ (Ollama/OpenAI/Anthropic)     │  │   │ │
│  │             │             │  │ (alert on timeout, log fail)  │  │   │ │
│  │             │             │  └────────────┬──────────────────┘  │   │ │
│  │             │             └───────────────┼─────────────────────┘   │ │
│  │             │                             │                         │ │
│  │             └──────────────┬──────────────┘                         │ │
│  │                            │ alerts                                 │ │
│  │             ┌──────────────▼──────────────┐                         │ │
│  │             │   Blackout Filter           │                         │ │
│  │             │   (check active blackouts)  │                         │ │
│  │             └──────────────┬──────────────┘                         │ │
│  │                            │                                        │ │
│  │             ┌──────────────▼──────────────┐                         │ │
│  │             │   Alert State Manager       │                         │ │
│  │             │   (create, update, clear)   │                         │ │
│  │             └─────────────────────────────┘                         │ │
│  └──────────────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────┬───────────────────────────────────┘
                                        │
                                        ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                          PostgreSQL Datastore                              │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐   │
│  │ metrics.*       │  │ alert_rules     │  │ alerts                  │   │
│  │ (from collector)│  │ alert_thresholds│  │ alert_acknowledgments   │   │
│  └─────────────────┘  │ metric_baselines│  │ blackouts               │   │
│                       │ metric_defs     │  │ blackout_schedules      │   │
│                       │ anomaly_cands   │  │ alerter_settings        │   │
│                       └─────────────────┘  └─────────────────────────┘   │
└───────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                           SQLite Auth Store                                │
│                    (shared with server: data/auth.db)                      │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐   │
│  │ users           │  │ groups          │  │ mcp_privileges          │   │
│  │ tokens          │  │ group_members   │  │ group_privileges        │   │
│  └─────────────────┘  └─────────────────┘  └─────────────────────────┘   │
└───────────────────────────────────────────────────────────────────────────┘
```

## Project Structure

The alerter is a new sub-project following existing patterns:

```
alerter/
├── src/
│   ├── cmd/
│   │   └── ai-dba-alerter/
│   │       └── main.go           # Entry point
│   ├── internal/
│   │   ├── api/
│   │   │   ├── server.go         # HTTP server setup
│   │   │   ├── middleware.go     # Auth, logging, CORS
│   │   │   ├── alerts.go         # Alert CRUD endpoints
│   │   │   ├── blackouts.go      # Blackout endpoints
│   │   │   ├── rules.go          # Alert rule endpoints
│   │   │   └── metrics.go        # Metric definition endpoints
│   │   ├── auth/
│   │   │   └── rbac.go           # RBAC integration (uses server's auth.db)
│   │   ├── config/
│   │   │   └── config.go         # Configuration handling
│   │   ├── database/
│   │   │   ├── datastore.go      # Datastore connection
│   │   │   ├── schema.go         # Migrations
│   │   │   └── types.go          # Data types
│   │   ├── engine/
│   │   │   ├── engine.go         # Main processing loop
│   │   │   ├── threshold/
│   │   │   │   ├── engine.go     # Threshold evaluation
│   │   │   │   ├── rules.go      # Built-in rule definitions
│   │   │   │   └── evaluator.go  # Rule evaluation logic
│   │   │   ├── anomaly/
│   │   │   │   ├── detector.go   # Anomaly detection orchestrator
│   │   │   │   ├── tier1.go      # Statistical detection
│   │   │   │   ├── tier2.go      # Embedding similarity
│   │   │   │   ├── tier3.go      # LLM classification
│   │   │   │   ├── correlation.go# Multi-metric correlation
│   │   │   │   └── coldstart.go  # Cold start handling
│   │   │   ├── blackout/
│   │   │   │   ├── manager.go    # Blackout period management
│   │   │   │   └── scheduler.go  # Scheduled blackout handling
│   │   │   └── state/
│   │   │       ├── manager.go    # Alert lifecycle management
│   │   │       └── types.go      # Alert state types
│   │   └── llm/
│   │       ├── provider.go       # Provider interface
│   │       ├── factory.go        # Provider factory
│   │       ├── embedding.go      # Embedding normalization
│   │       ├── ollama/
│   │       │   └── client.go
│   │       ├── anthropic/
│   │       │   └── client.go
│   │       ├── openai/
│   │       │   └── client.go
│   │       └── voyage/
│   │           └── client.go
│   └── go.mod
├── tests/
│   └── ...
├── docs/
│   └── alerter/
│       ├── index.md
│       ├── configuration.md
│       ├── api-reference.md
│       └── alert-rules.md
└── README.md
```

## Database Schema

### Migration 7: Core Alert Tables

```sql
-- Alerter settings (global configuration stored in DB)
CREATE TABLE alerter_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE alerter_settings IS
    'Global alerter configuration settings';

-- Insert default settings
INSERT INTO alerter_settings (key, value, description) VALUES
    ('alert_retention_days', '90', 'Days to retain cleared/acknowledged alerts'),
    ('correlation_window_seconds', '120', 'Time window for grouping correlated anomalies'),
    ('llm_timeout_seconds', '30', 'Timeout for LLM classification requests');

-- Alert definitions for threshold-based alerts
CREATE TABLE alert_rules (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    category TEXT NOT NULL,       -- 'availability', 'performance', 'capacity',
                                  -- 'replication', 'security', 'maintenance'
    severity TEXT NOT NULL,       -- 'info', 'warning', 'critical'
    probe_name TEXT NOT NULL,     -- Source probe for metrics
    metric_column TEXT NOT NULL,  -- Column to evaluate

    -- Evaluation configuration
    comparison TEXT NOT NULL,     -- 'gt', 'lt', 'gte', 'lte', 'eq', 'neq',
                                  -- 'absent', 'present', 'changed'
    default_threshold DOUBLE PRECISION,
    default_duration_seconds INT DEFAULT 0,  -- Sustained threshold before alert

    -- Grouping/aggregation
    group_by_columns TEXT[],      -- Additional columns to include in alert key
    aggregation TEXT,             -- 'avg', 'max', 'min', 'sum', 'count', NULL

    -- Built-in vs user-defined
    is_builtin BOOLEAN DEFAULT false,
    is_enabled BOOLEAN DEFAULT true,

    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE alert_rules IS
    'Definitions for threshold-based alert rules';

-- Per-connection threshold overrides
CREATE TABLE alert_thresholds (
    id SERIAL PRIMARY KEY,
    rule_id INT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    connection_id INT REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,           -- NULL = all databases on connection

    -- Override values (NULL = use rule default)
    threshold DOUBLE PRECISION,
    duration_seconds INT,
    severity TEXT,
    is_enabled BOOLEAN DEFAULT true,

    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),

    UNIQUE(rule_id, connection_id, database_name)
);

COMMENT ON TABLE alert_thresholds IS
    'Per-connection/database threshold overrides for alert rules';

CREATE INDEX idx_alert_thresholds_rule ON alert_thresholds(rule_id);
CREATE INDEX idx_alert_thresholds_connection ON alert_thresholds(connection_id);

-- Active and historical alerts
CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,

    -- Alert identification
    alert_type TEXT NOT NULL,     -- 'threshold', 'anomaly'
    rule_id INT REFERENCES alert_rules(id),  -- For threshold alerts
    anomaly_candidate_id BIGINT,  -- For anomaly alerts (FK added later)

    -- Source identification
    connection_id INT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,           -- NULL for server-level alerts
    probe_name TEXT NOT NULL,
    metric_column TEXT,

    -- Additional context keys (table name, index name, PID, etc.)
    context_keys JSONB DEFAULT '{}',

    -- Alert state
    status TEXT NOT NULL DEFAULT 'active',  -- 'active', 'cleared', 'acknowledged'
    severity TEXT NOT NULL,

    -- Alert details
    message TEXT NOT NULL,
    current_value DOUBLE PRECISION,
    threshold_value DOUBLE PRECISION,
    details JSONB DEFAULT '{}',   -- Additional context (z-score, LLM explanation)

    -- LLM processing status (for anomaly alerts)
    llm_processed BOOLEAN DEFAULT false,
    llm_failed BOOLEAN DEFAULT false,
    llm_failure_reason TEXT,

    -- Timestamps
    first_triggered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_triggered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    cleared_at TIMESTAMPTZ,
    trigger_count INT DEFAULT 1,  -- How many times condition met while active

    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE alerts IS
    'Active and historical alerts from threshold and anomaly detection';

CREATE INDEX idx_alerts_status ON alerts(status) WHERE status = 'active';
CREATE INDEX idx_alerts_connection ON alerts(connection_id);
CREATE INDEX idx_alerts_type ON alerts(alert_type);
CREATE INDEX idx_alerts_rule ON alerts(rule_id) WHERE rule_id IS NOT NULL;
CREATE INDEX idx_alerts_first_triggered ON alerts(first_triggered_at);

-- Alert acknowledgments (separate table for audit trail)
CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,

    acknowledged_by TEXT NOT NULL,  -- Username
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reason TEXT NOT NULL,           -- User explanation

    -- For learning from acks
    is_false_positive BOOLEAN DEFAULT false,  -- User indicates not a real issue
    suppress_similar BOOLEAN DEFAULT false,   -- Use for future suppression

    created_at TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE alert_acknowledgments IS
    'Audit trail of alert acknowledgments with user-provided reasons';

CREATE INDEX idx_ack_alert ON alert_acknowledgments(alert_id);
CREATE INDEX idx_ack_user ON alert_acknowledgments(acknowledged_by);
```

### Migration 8: Blackout Tables

```sql
-- Active and historical blackout periods
CREATE TABLE blackouts (
    id SERIAL PRIMARY KEY,

    -- Scope (connection_id NULL = global blackout)
    connection_id INT REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,           -- NULL = all databases on connection

    -- Blackout details
    reason TEXT,

    -- State
    is_active BOOLEAN DEFAULT true,

    -- Timestamps
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_by TEXT NOT NULL,     -- Username
    ended_at TIMESTAMPTZ,
    ended_by TEXT,

    -- If created from a schedule
    schedule_id INT,              -- FK added after schedule table

    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE blackouts IS
    'Manual and scheduled blackout periods that suppress alert processing';

CREATE INDEX idx_blackouts_active ON blackouts(is_active) WHERE is_active = true;
CREATE INDEX idx_blackouts_connection ON blackouts(connection_id);

-- Scheduled recurring blackouts
CREATE TABLE blackout_schedules (
    id SERIAL PRIMARY KEY,

    -- Scope
    connection_id INT REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,

    -- Schedule details
    name TEXT NOT NULL,
    reason TEXT,

    -- Recurrence (cron-style or simple)
    schedule_type TEXT NOT NULL,  -- 'once', 'daily', 'weekly', 'monthly', 'cron'

    -- For 'once' type
    start_time TIMESTAMPTZ,
    end_time TIMESTAMPTZ,

    -- For recurring types
    cron_expression TEXT,         -- For 'cron' type
    day_of_week INT,              -- 0-6 for 'weekly'
    day_of_month INT,             -- 1-31 for 'monthly'
    start_hour INT,               -- 0-23 local time
    start_minute INT DEFAULT 0,
    duration_minutes INT NOT NULL,
    timezone TEXT DEFAULT 'UTC',

    -- State
    is_enabled BOOLEAN DEFAULT true,

    -- Audit
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE blackout_schedules IS
    'Scheduled maintenance windows for recurring blackout periods';

ALTER TABLE blackouts
    ADD CONSTRAINT fk_blackouts_schedule
    FOREIGN KEY (schedule_id) REFERENCES blackout_schedules(id) ON DELETE SET NULL;

CREATE INDEX idx_schedules_enabled ON blackout_schedules(is_enabled)
    WHERE is_enabled = true;
CREATE INDEX idx_schedules_connection ON blackout_schedules(connection_id);
```

### Migration 9: Anomaly Detection Tables

```sql
-- Metric definitions for anomaly detection baselines
CREATE TABLE metric_definitions (
    id SERIAL PRIMARY KEY,
    metric_name TEXT NOT NULL,
    probe_name TEXT NOT NULL,

    -- Baseline configuration
    baseline_window INTERVAL DEFAULT '7 days',
    sensitivity DOUBLE PRECISION DEFAULT 3.0,  -- Z-score threshold

    -- Time-aware baselines
    use_hourly_baseline BOOLEAN DEFAULT false,
    use_dow_baseline BOOLEAN DEFAULT false,

    -- Cold start configuration
    cold_start_strategy TEXT DEFAULT 'grace_period',
    cold_start_grace_period INTERVAL DEFAULT '24 hours',
    cold_start_bootstrap_pattern TEXT,

    is_enabled BOOLEAN DEFAULT true,

    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),

    UNIQUE(metric_name, probe_name)
);

COMMENT ON TABLE metric_definitions IS
    'Configuration for anomaly detection per metric';

-- Rolling baseline statistics
CREATE TABLE metric_baselines (
    id BIGSERIAL PRIMARY KEY,
    metric_name TEXT NOT NULL,
    probe_name TEXT NOT NULL,
    connection_id INT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,           -- NULL for server-level metrics

    -- Time segmentation
    hour_of_day INT,              -- 0-23, NULL if not using hourly
    day_of_week INT,              -- 0-6, NULL if not using DOW

    -- Statistics
    sample_count BIGINT NOT NULL,
    mean DOUBLE PRECISION NOT NULL,
    stddev DOUBLE PRECISION NOT NULL,
    min_val DOUBLE PRECISION,
    max_val DOUBLE PRECISION,
    p50 DOUBLE PRECISION,
    p95 DOUBLE PRECISION,
    p99 DOUBLE PRECISION,

    last_updated TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(metric_name, probe_name, connection_id,
           COALESCE(database_name, ''),
           COALESCE(hour_of_day, -1),
           COALESCE(day_of_week, -1))
);

COMMENT ON TABLE metric_baselines IS
    'Rolling statistical baselines for anomaly detection';

CREATE INDEX idx_baselines_lookup ON metric_baselines(
    metric_name, probe_name, connection_id, database_name
);

-- Anomaly candidates (Tier 1 output)
CREATE TABLE anomaly_candidates (
    id BIGSERIAL PRIMARY KEY,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Source identification
    connection_id INT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    probe_name TEXT NOT NULL,
    metric_name TEXT NOT NULL,

    -- Additional context keys
    context_keys JSONB DEFAULT '{}',

    -- Detection values
    current_value DOUBLE PRECISION NOT NULL,
    baseline_mean DOUBLE PRECISION NOT NULL,
    baseline_stddev DOUBLE PRECISION NOT NULL,
    z_score DOUBLE PRECISION NOT NULL,

    -- Context window
    preceding_values DOUBLE PRECISION[],

    -- Tier 2 processing
    embedding VECTOR(1536),
    tier2_score DOUBLE PRECISION,
    similar_alerts JSONB,         -- Past alerts used for comparison

    -- Tier 3 processing
    tier3_result JSONB,           -- LLM classification result
    tier3_failed BOOLEAN DEFAULT false,
    tier3_failure_reason TEXT,

    -- Correlation
    correlation_group_id UUID,

    -- Final disposition
    alert_id BIGINT REFERENCES alerts(id),
    suppressed BOOLEAN DEFAULT false,
    suppression_reason TEXT,

    created_at TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE anomaly_candidates IS
    'Statistical anomalies detected by Tier 1, processed by Tiers 2 and 3';

CREATE INDEX idx_candidates_connection ON anomaly_candidates(connection_id);
CREATE INDEX idx_candidates_detected ON anomaly_candidates(detected_at);
CREATE INDEX idx_candidates_unprocessed ON anomaly_candidates(id)
    WHERE tier3_result IS NULL AND NOT suppressed AND NOT tier3_failed;

-- Correlation groups
CREATE TABLE correlation_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    correlation_type TEXT NOT NULL,  -- 'same_server', 'same_metric', 'cascade'
    connection_id INT REFERENCES connections(id) ON DELETE CASCADE,
    metric_name TEXT,

    -- Tier 3 result for the group
    tier3_result JSONB,

    -- Final disposition
    alert_id BIGINT REFERENCES alerts(id),

    created_at TIMESTAMPTZ DEFAULT now()
);

COMMENT ON TABLE correlation_groups IS
    'Groups of correlated anomalies detected together';

ALTER TABLE anomaly_candidates
    ADD CONSTRAINT fk_candidates_correlation
    FOREIGN KEY (correlation_group_id) REFERENCES correlation_groups(id);

-- Add pgvector index for embedding similarity search
CREATE INDEX idx_candidates_embedding ON anomaly_candidates
    USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100)
    WHERE embedding IS NOT NULL;
```

### Migration 10: Seed Built-in Alert Rules

```sql
-- Built-in threshold alert rules
INSERT INTO alert_rules (
    name, description, category, severity, probe_name, metric_column,
    comparison, default_threshold, default_duration_seconds,
    group_by_columns, aggregation, is_builtin
) VALUES
-- Availability alerts
('server_unreachable',
 'Server is not responding to connection attempts',
 'availability', 'critical', 'pg_stat_activity', 'backend_start',
 'absent', NULL, 60, NULL, NULL, true),

('database_accepting_connections',
 'Database is not accepting connections (max_connections reached or pg_hba deny)',
 'availability', 'critical', 'pg_stat_database', 'numbackends',
 'absent', NULL, 30, ARRAY['database_name'], NULL, true),

-- Capacity alerts
('disk_space_low',
 'Available disk space is below threshold',
 'capacity', 'warning', 'pg_sys_disk_info', 'percent_used',
 'gt', 85.0, 0, ARRAY['mount_point'], NULL, true),

('disk_space_critical',
 'Available disk space is critically low',
 'capacity', 'critical', 'pg_sys_disk_info', 'percent_used',
 'gt', 95.0, 0, ARRAY['mount_point'], NULL, true),

('connection_count_high',
 'Active connection count approaching max_connections',
 'capacity', 'warning', 'pg_stat_activity', 'pid',
 'gt', 0.8, 60, NULL, 'count', true),

('connection_count_critical',
 'Active connection count at or near max_connections',
 'capacity', 'critical', 'pg_stat_activity', 'pid',
 'gt', 0.95, 30, NULL, 'count', true),

('table_bloat_high',
 'Table bloat exceeds threshold',
 'capacity', 'warning', 'pg_stat_all_tables', 'n_dead_tup',
 'gt', 10000000, 0, ARRAY['database_name', 'schemaname', 'relname'], NULL, true),

-- Performance alerts
('long_running_queries',
 'Queries running longer than threshold',
 'performance', 'warning', 'pg_stat_activity', 'query_start',
 'lt', 3600, 0, ARRAY['pid', 'query'], NULL, true),

('lock_waits',
 'Queries waiting for locks longer than threshold',
 'performance', 'warning', 'pg_stat_activity', 'wait_event_type',
 'eq', NULL, 300, ARRAY['pid'], NULL, true),

('high_cpu_usage',
 'CPU usage exceeds threshold',
 'performance', 'warning', 'pg_sys_cpu_usage_info', 'idle_percent',
 'lt', 20.0, 60, NULL, 'avg', true),

('high_memory_usage',
 'Memory usage exceeds threshold',
 'performance', 'warning', 'pg_sys_memory_info', 'percent_used',
 'gt', 90.0, 60, NULL, NULL, true),

('checkpoint_frequency_high',
 'Checkpoints occurring more frequently than expected',
 'performance', 'info', 'pg_stat_bgwriter', 'checkpoints_req',
 'gt', 10, 3600, NULL, 'count', true),

('temp_file_usage_high',
 'Temporary file usage is high (work_mem may need tuning)',
 'performance', 'warning', 'pg_stat_database', 'temp_bytes',
 'gt', 1073741824, 3600, ARRAY['database_name'], 'sum', true),

-- Replication alerts
('replication_lag_high',
 'Replication lag exceeds threshold',
 'replication', 'warning', 'pg_stat_replication', 'replay_lag',
 'gt', 60, 0, ARRAY['application_name', 'client_addr'], NULL, true),

('replication_lag_critical',
 'Replication lag is critically high',
 'replication', 'critical', 'pg_stat_replication', 'replay_lag',
 'gt', 300, 0, ARRAY['application_name', 'client_addr'], NULL, true),

('replication_slot_inactive',
 'Replication slot is inactive',
 'replication', 'warning', 'pg_replication_slots', 'active',
 'eq', 0, 300, ARRAY['slot_name'], NULL, true),

('wal_receiver_disconnected',
 'Standby WAL receiver is disconnected',
 'replication', 'critical', 'pg_stat_wal_receiver', 'status',
 'neq', NULL, 60, NULL, NULL, true),

-- Security alerts
('superuser_connections',
 'Superuser connection count exceeds threshold',
 'security', 'info', 'pg_stat_activity', 'usename',
 'gt', 5, 0, NULL, 'count', true),

('failed_authentication_spike',
 'Spike in failed authentication attempts',
 'security', 'warning', 'pg_stat_database', 'sessions_fatal',
 'gt', 10, 300, ARRAY['database_name'], 'sum', true),

-- Maintenance alerts
('autovacuum_not_running',
 'Autovacuum has not run recently on table',
 'maintenance', 'warning', 'pg_stat_all_tables', 'last_autovacuum',
 'lt', NULL, 0, ARRAY['database_name', 'schemaname', 'relname'], NULL, true),

('transaction_id_wraparound',
 'Database approaching transaction ID wraparound',
 'maintenance', 'critical', 'pg_database', 'age_datfrozenxid',
 'gt', 1500000000, 0, ARRAY['datname'], NULL, true),

('wal_archive_failing',
 'WAL archiving is failing',
 'maintenance', 'critical', 'pg_stat_archiver', 'failed_count',
 'gt', 0, 300, NULL, NULL, true),

('index_scans_low',
 'Sequential scans significantly outnumber index scans',
 'maintenance', 'info', 'pg_stat_all_tables', 'seq_scan',
 'gt', 1000, 86400, ARRAY['database_name', 'schemaname', 'relname'], NULL, true);
```

## REST API Endpoints

All endpoints require authentication via Bearer token and respect RBAC
permissions. Users can only access alerts for connections they have permission
to view.

### Alerts

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/alerts` | List alerts (query params: status, severity, connection_id, limit, offset) |
| GET | `/api/alerts/{id}` | Get alert details including acknowledgments |
| POST | `/api/alerts/{id}/acknowledge` | Acknowledge alert with reason |
| DELETE | `/api/alerts/{id}` | Delete alert (superuser only) |

### Alert Rules

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/alert-rules` | List all alert rules |
| GET | `/api/alert-rules/{id}` | Get rule details with thresholds |
| PUT | `/api/alert-rules/{id}` | Update rule (enable/disable, defaults) |
| POST | `/api/alert-rules` | Create custom rule |
| DELETE | `/api/alert-rules/{id}` | Delete custom rule (not built-in) |

### Alert Thresholds (per-connection overrides)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/alert-thresholds` | List thresholds (query: rule_id, connection_id) |
| POST | `/api/alert-thresholds` | Create threshold override |
| PUT | `/api/alert-thresholds/{id}` | Update threshold |
| DELETE | `/api/alert-thresholds/{id}` | Delete threshold override |

### Blackouts

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/blackouts` | List blackouts (query: active, connection_id) |
| POST | `/api/blackouts` | Create manual blackout |
| PUT | `/api/blackouts/{id}/end` | End active blackout |
| DELETE | `/api/blackouts/{id}` | Delete blackout record |

### Blackout Schedules

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/blackout-schedules` | List schedules |
| POST | `/api/blackout-schedules` | Create schedule |
| PUT | `/api/blackout-schedules/{id}` | Update schedule |
| DELETE | `/api/blackout-schedules/{id}` | Delete schedule |

### Metric Definitions (anomaly detection config)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/metric-definitions` | List metric definitions |
| PUT | `/api/metric-definitions/{id}` | Update definition |

### Settings

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/settings` | Get alerter settings |
| PUT | `/api/settings` | Update settings (superuser only) |

## Configuration

### Configuration File

`/etc/pgedge/ai-dba-alerter.yaml` (or `./ai-dba-alerter.yaml`)

```yaml
# Datastore connection (same as collector/server)
datastore:
  host: localhost
  hostaddr: ""
  database: ai_workbench
  username: postgres
  password_file: /etc/pgedge/ai-dba.password
  port: 5432
  sslmode: prefer
  sslcert: ""
  sslkey: ""
  sslrootcert: ""

# Connection pool settings
pool:
  max_connections: 10
  max_idle_seconds: 300

# HTTP server settings
http:
  addr: localhost:8001
  tls: false
  cert: ""
  key: ""
  chain: ""

# Auth settings (shared with server)
auth:
  data_dir: /var/lib/pgedge/ai-dba/data  # Location of auth.db

# Server secret for decrypting connection passwords
secret_file: /etc/pgedge/ai-dba-collector.secret

# Threshold engine settings
threshold:
  evaluation_interval_seconds: 60

# Anomaly detection settings
anomaly:
  enabled: true
  tier1:
    enabled: true
    default_sensitivity: 3.0
    evaluation_interval_seconds: 60
  tier2:
    enabled: true
    suppression_threshold: 0.85
    similarity_threshold: 0.3
  tier3:
    enabled: true
    timeout_seconds: 30           # Alert after this timeout

# Baseline calculation
baselines:
  refresh_interval_seconds: 3600

# Correlation settings
correlation:
  window_seconds: 120             # Configurable, default 2 minutes

# LLM providers
llm:
  embedding_provider: ollama
  reasoning_provider: ollama

  ollama:
    base_url: http://localhost:11434
    embedding_model: nomic-embed-text
    reasoning_model: qwen2.5:7b-instruct

  openai:
    api_key_file: /etc/pgedge/openai.key
    embedding_model: text-embedding-3-small
    reasoning_model: gpt-4o-mini

  anthropic:
    api_key_file: /etc/pgedge/anthropic.key
    reasoning_model: claude-3-5-haiku-20241022

  voyage:
    api_key_file: /etc/pgedge/voyage.key
    embedding_model: voyage-3-lite
```

### Command Line Options

Following the same pattern as collector and server:

```
Usage: ai-dba-alerter [options]

Options:
  -config string
        Path to configuration file (default: auto-detect)
  -addr string
        HTTP server address (overrides config)
  -tls
        Enable TLS/HTTPS
  -cert string
        Path to TLS certificate file
  -key string
        Path to TLS key file
  -chain string
        Path to TLS certificate chain file
  -data-dir string
        Data directory for auth database
  -db-host string
        Database host (overrides config)
  -db-port int
        Database port (overrides config)
  -db-name string
        Database name (overrides config)
  -db-user string
        Database user (overrides config)
  -db-password string
        Database password (overrides config)
  -db-sslmode string
        Database SSL mode (overrides config)
  -debug
        Enable debug logging
```

### Environment Variables

```
AI_DBA_PG_HOST
AI_DBA_PG_HOSTADDR
AI_DBA_PG_DATABASE
AI_DBA_PG_USERNAME
AI_DBA_PG_PASSWORD
AI_DBA_PG_PORT
AI_DBA_PG_SSLMODE
AI_DBA_PG_SSLCERT
AI_DBA_PG_SSLKEY
AI_DBA_PG_SSLROOTCERT
```

## Traditional Threshold Alert Rules

The following 22 built-in alert rules will be provided:

### Availability

| Alert | Severity | Condition | Default |
|-------|----------|-----------|---------|
| Server Unreachable | Critical | Connection fails | 60s duration |
| Database Not Accepting | Critical | Cannot connect to database | 30s duration |

### Capacity

| Alert | Severity | Condition | Default |
|-------|----------|-----------|---------|
| Disk Space Low | Warning | Used > threshold | 85% |
| Disk Space Critical | Critical | Used > threshold | 95% |
| Connection Count High | Warning | Active > % of max | 80% |
| Connection Count Critical | Critical | Active > % of max | 95% |
| Table Bloat High | Warning | Dead tuples > threshold | 10M tuples |

### Performance

| Alert | Severity | Condition | Default |
|-------|----------|-----------|---------|
| Long Running Queries | Warning | Duration > threshold | 1 hour |
| Lock Waits | Warning | Waiting > threshold | 5 minutes |
| High CPU Usage | Warning | CPU usage > threshold | 80% |
| High Memory Usage | Warning | Memory > threshold | 90% |
| Checkpoint Frequency | Info | Requested checkpoints/hour | > 10 |
| Temp File Usage | Warning | Temp bytes/hour | > 1 GB |

### Replication

| Alert | Severity | Condition | Default |
|-------|----------|-----------|---------|
| Replication Lag High | Warning | Lag > threshold | 60 seconds |
| Replication Lag Critical | Critical | Lag > threshold | 5 minutes |
| Replication Slot Inactive | Warning | Slot inactive | 5 minutes |
| WAL Receiver Disconnected | Critical | Not streaming | 60s duration |

### Security

| Alert | Severity | Condition | Default |
|-------|----------|-----------|---------|
| Superuser Connections | Info | Count > threshold | 5 |
| Authentication Failures | Warning | Failed attempts spike | 10/5min |

### Maintenance

| Alert | Severity | Condition | Default |
|-------|----------|-----------|---------|
| Autovacuum Not Running | Warning | Last run > threshold | Configurable |
| Transaction ID Wraparound | Critical | Age > threshold | 75% of 2B |
| WAL Archive Failing | Critical | Failed count > 0 | 5 min duration |
| Index Scans Low | Info | Seq scans >> idx scans | 1000/day |

## Implementation Phases

### Phase 1: Core Infrastructure

**Goal**: Separate binary, database schema, REST API skeleton, basic RBAC.

1. Create alerter sub-project structure
2. Implement configuration handling (following collector/server patterns)
3. Create database migrations 7-10
4. Implement RBAC integration (use server's auth.db)
5. Implement HTTP server with authentication middleware
6. Implement alert state manager
7. Implement blackout manager
8. Create REST API endpoints (CRUD operations)

**Deliverables**:

- Standalone `ai-dba-alerter` binary
- Database schema in place
- REST API working with RBAC
- Manual blackouts working

### Phase 2: Threshold Engine

**Goal**: Deterministic threshold-based alerting.

1. Implement threshold engine
   - Rule evaluation logic
   - Threshold override resolution
   - Duration tracking for sustained conditions
2. Implement metric querying from datastore
3. Implement alert lifecycle (trigger, update, clear)
4. Seed built-in alert rules
5. Add garbage collection for old alerts (retention configurable, default 90d)

**Deliverables**:

- Threshold alerts firing and clearing
- Per-connection threshold overrides working
- All 22 built-in rules available
- Alert retention working

### Phase 3: Anomaly Detection - Tier 1

**Goal**: Statistical anomaly detection.

1. Implement baseline manager
   - Calculate rolling statistics per metric
   - Time-aware baselines (hour/day)
   - Baseline refresh scheduling
2. Implement Tier 1 detector
   - Z-score calculation
   - EWMA for trend detection
   - Candidate generation
3. Implement cold start handling
   - Grace period strategy
   - Bootstrap from similar servers
   - Alert anyway strategy
4. Add metric definition management endpoints

**Deliverables**:

- Baselines calculated and maintained
- Statistical anomalies detected
- Candidates stored for further processing
- Cold start handled gracefully

### Phase 4: LLM Provider Integration

**Goal**: LLM provider abstraction layer.

1. Implement provider interfaces
   - Embedding provider interface
   - Reasoning provider interface
2. Implement providers
   - Ollama
   - Anthropic
   - OpenAI
   - Voyage
3. Implement embedding normalization (to 1536 dimensions)
4. Implement provider factory and configuration
5. Add timeout handling with fallback to alert

**Deliverables**:

- All four providers working
- Embeddings can be generated
- Classifications can be requested
- Timeout results in alert with `llm_failed=true`

### Phase 5: Anomaly Detection - Tiers 2 & 3

**Goal**: AI-powered anomaly classification.

1. Verify pgvector extension available
2. Implement Tier 2
   - Generate embeddings for candidates
   - Similarity search against past alerts
   - Calculate suppression score
   - Auto-suppress high-confidence matches
3. Implement Tier 3
   - Build context prompt
   - Include similar past alerts (RAG)
   - Get LLM classification
   - Handle timeout (alert anyway, mark `llm_failed`)
4. Implement correlation detection
   - Same-server correlation
   - Same-metric correlation
   - Cascade detection
   - Group-level classification

**Deliverables**:

- Full tiered detection pipeline working
- Past acknowledgments influence future alerts
- Correlated anomalies grouped
- LLM failures tracked but alerts still fire

### Phase 6: Scheduled Blackouts & Polish

**Goal**: Complete blackout scheduling and refinements.

1. Implement blackout scheduler
   - Parse cron expressions
   - Handle recurring schedules
   - Auto-create blackout instances
2. Implement SIGHUP config reload
3. Documentation
   - Alerter configuration guide
   - API reference
   - Alert rule reference
   - Anomaly detection tuning guide
4. Testing
   - Unit tests for all components
   - Integration tests with test database
   - End-to-end alert lifecycle tests

**Deliverables**:

- Scheduled maintenance windows working
- Config reload on SIGHUP
- Full documentation
- Comprehensive test coverage
- Production-ready alerter

## Dependencies

### Required PostgreSQL Extensions

- pgvector provides embedding similarity search in Tier 2.
  - Must be installed on the datastore database
  - Will be checked during migration 9

### Go Dependencies

```
github.com/jackc/pgx/v5          # PostgreSQL driver (existing)
github.com/pgvector/pgvector-go  # pgvector support
github.com/robfig/cron/v3        # Cron expression parsing
gopkg.in/yaml.v3                 # YAML config (existing)
```

## Security Considerations

1. Alert access control means alerts inherit connection ownership; users see
   only alerts for connections they have permission to access.

2. Blackout permissions mean only connection owners (or superusers) can create
   blackouts for connections.

3. LLM API keys are stored in files with restricted permissions, never in
   config or database.

4. Acknowledgment audit ensures all acknowledgments are logged with username
   and timestamp for accountability.

5. No metric data goes to LLM; raw metric values are not sent to external
   LLMs; only aggregated context and patterns.

6. Shared auth store uses the same auth.db as the server for consistent RBAC
   enforcement.

## Testing Strategy

### Unit Tests

- Threshold evaluation logic
- Z-score calculations
- Blackout period checking
- Schedule parsing
- Provider API mocking
- RBAC permission checking

### Integration Tests

- Alert lifecycle (trigger, update, clear, acknowledge)
- Threshold override resolution
- Blackout filtering
- Baseline calculation
- Embedding storage and search
- REST API endpoints

### End-to-End Tests

- Full alert pipeline from metric to alert
- Anomaly detection tiers
- Scheduled blackout creation
- LLM timeout handling

## Decisions Made

Based on review feedback:

1. Alert retention is 90 days default, configurable via `alerter_settings`.
2. The pgvector extension is required for anomaly detection Tier 2.
3. Multi-tenancy uses global rules with per-connection threshold overrides.
4. LLM fallback alerts after timeout; the system tracks that LLM failed via
   `llm_failed` and `llm_failure_reason` columns.
5. Correlation window is configurable via `alerter_settings`, default 120s.

---

*Document version: 2.0*
*Created: 2026-01-10*
*Updated: 2026-01-10 - Separate binary, REST API, RBAC*

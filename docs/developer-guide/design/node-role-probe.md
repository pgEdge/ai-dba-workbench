# Node Role Probe Design

This document describes the design for probes that detect
and track PostgreSQL node roles within various cluster
topologies.

## Overview

PostgreSQL servers can participate in various replication
configurations:

- A standalone server runs without replication.
- Binary (physical/streaming) replication clusters use
  WAL streaming between nodes.
- Native logical replication setups use publications and
  subscriptions.
- Spock multi-master clusters enable bidirectional
  replication.

A single node may hold multiple roles simultaneously. For
example, a Spock node might also have a binary standby
for high availability. The probe system must detect these
configurations accurately and store the results for
analysis.

## Node Role Taxonomy

### Primary Roles

The following table lists the primary roles that a node
can assume.

| Role | Description | Detection Method |
|------|-------------|------------------|
| `standalone` | No replication configured | No standbys, not in recovery, no publications or subscriptions |
| `binary_primary` | Source for physical replication | Has entries in `pg_stat_replication` (type=physical) |
| `binary_standby` | Physical replication target | `pg_is_in_recovery()` = true, has WAL receiver |
| `binary_cascading` | Standby that is also a primary | In recovery AND has standbys |
| `logical_publisher` | Native logical replication source | Has publications |
| `logical_subscriber` | Native logical replication target | Has subscriptions |
| `spock_node` | Active Spock multi-master node | Spock extension active, node in `spock.node` |
| `spock_standby` | Binary standby of a Spock node | In recovery, primary is a Spock node |

### Role Flags

Since roles can combine, the system tracks individual
capability flags. These flags are non-exclusive:

- `is_in_recovery` indicates the node operates in standby
  mode.
- `has_binary_standbys` indicates the node has physical
  replication standbys.
- `has_publications` indicates the node has logical
  replication publications.
- `has_subscriptions` indicates the node has logical
  replication subscriptions.
- `has_spock` indicates the Spock extension is installed
  and active.

## Database Schema

### Table: metrics.pg_server_info

This table stores relatively static server identification
information. The data changes rarely and only on upgrades
or major configuration changes.

```sql
CREATE TABLE IF NOT EXISTS metrics.pg_server_info (
    connection_id INTEGER NOT NULL,

    -- Server Identification
    server_version TEXT,
    server_version_num INTEGER,
    system_identifier BIGINT,
    cluster_name TEXT,
    data_directory TEXT,

    -- Configuration
    max_connections INTEGER,
    max_wal_senders INTEGER,
    max_replication_slots INTEGER,
    wal_level TEXT,

    -- Extensions (for role detection)
    installed_extensions TEXT[],

    collected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (connection_id, collected_at)
) PARTITION BY RANGE (collected_at);

COMMENT ON TABLE metrics.pg_server_info IS
    'Server identification and configuration'
    ' - only stores snapshots when changes'
    ' detected';
```

### Table: metrics.pg_node_role

This table stores node role detection results. The data
may change more dynamically as replication configurations
evolve.

```sql
CREATE TABLE IF NOT EXISTS metrics.pg_node_role (
    connection_id INTEGER NOT NULL,

    -- Fundamental Status
    is_in_recovery BOOLEAN NOT NULL,
    timeline_id INTEGER,

    -- Binary Replication Status
    has_binary_standbys BOOLEAN NOT NULL DEFAULT FALSE,
    binary_standby_count INTEGER DEFAULT 0,
    is_streaming_standby BOOLEAN NOT NULL DEFAULT FALSE,
    upstream_host TEXT,
    upstream_port INTEGER,
    received_lsn TEXT,
    replayed_lsn TEXT,

    -- Logical Replication Status
    publication_count INTEGER DEFAULT 0,
    subscription_count INTEGER DEFAULT 0,
    active_subscription_count INTEGER DEFAULT 0,

    -- Spock Status
    has_spock BOOLEAN NOT NULL DEFAULT FALSE,
    spock_node_id BIGINT,
    spock_node_name TEXT,
    spock_subscription_count INTEGER DEFAULT 0,

    -- Computed Primary Role
    primary_role TEXT NOT NULL,

    -- Role Flags (non-exclusive capabilities)
    role_flags TEXT[] NOT NULL DEFAULT '{}',

    -- Extended Information (JSON for flexibility)
    role_details JSONB,

    collected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (connection_id, collected_at)
) PARTITION BY RANGE (collected_at);

COMMENT ON TABLE metrics.pg_node_role IS
    'Node role detection for cluster topology'
    ' analysis';

CREATE INDEX IF NOT EXISTS
    idx_pg_node_role_primary_role
    ON metrics.pg_node_role(
        connection_id, primary_role,
        collected_at DESC
    );

CREATE INDEX IF NOT EXISTS
    idx_pg_node_role_collected_at
    ON metrics.pg_node_role(collected_at DESC);
```

## Detection Logic

### Query: Server Info

The following query retrieves server identification and
configuration data.

```sql
SELECT
    current_setting('server_version')
        AS server_version,
    current_setting('server_version_num')::integer
        AS server_version_num,
    (SELECT system_identifier
     FROM pg_control_system())
        AS system_identifier,
    current_setting('cluster_name', true)
        AS cluster_name,
    current_setting('data_directory')
        AS data_directory,
    current_setting('max_connections')::integer
        AS max_connections,
    current_setting('max_wal_senders')::integer
        AS max_wal_senders,
    current_setting('max_replication_slots')::integer
        AS max_replication_slots,
    current_setting('wal_level')
        AS wal_level,
    (SELECT array_agg(extname)
     FROM pg_extension)
        AS installed_extensions
```

### Query: Basic Role Detection

The following query detects the fundamental replication
role of a node.

```sql
SELECT
    pg_is_in_recovery() AS is_in_recovery,
    (SELECT timeline_id
     FROM pg_control_checkpoint())
        AS timeline_id,
    (SELECT count(*)
     FROM pg_stat_replication
     WHERE state = 'streaming')
        AS binary_standby_count,
    (SELECT count(*)
     FROM pg_publication)
        AS publication_count,
    (SELECT count(*)
     FROM pg_subscription)
        AS subscription_count,
    (SELECT count(*)
     FROM pg_stat_subscription
     WHERE relid IS NULL
       AND pid IS NOT NULL)
        AS active_subscription_count
```

### Query: Standby Info

The following query runs only when `is_in_recovery` is
true and retrieves upstream connection details.

```sql
SELECT
    sender_host AS upstream_host,
    sender_port AS upstream_port,
    received_lsn::text,
    (SELECT replay_lsn::text
     FROM pg_stat_get_wal_receiver())
FROM pg_stat_wal_receiver
LIMIT 1
```

### Query: Spock Detection

The following queries detect whether Spock is installed
and retrieve node information.

```sql
-- Check if Spock is installed
SELECT EXISTS (
    SELECT 1 FROM pg_extension
    WHERE extname = 'spock'
) AS has_spock;

-- If Spock exists, get node info
SELECT
    node_id AS spock_node_id,
    node_name AS spock_node_name,
    (SELECT count(*)
     FROM spock.subscription
     WHERE sub_enabled = true)
        AS spock_subscription_count
FROM spock.local_node
LIMIT 1;
```

## Role Determination Algorithm

The following Go function determines the primary role
and capability flags for a node.

```go
func determineNodeRole(
    info *NodeRoleInfo,
) (string, []string) {
    var flags []string

    if info.HasBinaryStandbys {
        flags = append(flags, "binary_primary")
    }
    if info.IsStreamingStandby {
        flags = append(flags, "binary_standby")
    }
    if info.PublicationCount > 0 {
        flags = append(flags, "logical_publisher")
    }
    if info.SubscriptionCount > 0 {
        flags = append(flags, "logical_subscriber")
    }
    if info.HasSpock && info.SpockNodeName != "" {
        flags = append(flags, "spock_node")
    }

    var primaryRole string
    switch {
    case info.HasSpock && info.SpockNodeName != "":
        if info.IsInRecovery {
            primaryRole = "spock_standby"
        } else {
            primaryRole = "spock_node"
        }
    case info.IsInRecovery:
        if info.HasBinaryStandbys {
            primaryRole = "binary_cascading"
        } else {
            primaryRole = "binary_standby"
        }
    case info.HasBinaryStandbys:
        primaryRole = "binary_primary"
    case info.PublicationCount > 0 &&
        info.SubscriptionCount > 0:
        primaryRole = "logical_bidirectional"
    case info.PublicationCount > 0:
        primaryRole = "logical_publisher"
    case info.SubscriptionCount > 0:
        primaryRole = "logical_subscriber"
    default:
        primaryRole = "standalone"
    }

    return primaryRole, flags
}
```

## Probe Configuration

The following YAML shows the default probe configuration
for the node role probes.

```yaml
- name: pg_server_info
  description: >-
    Server identification and configuration
    (change-tracked)
  collection_interval_seconds: 3600
  retention_days: 365

- name: pg_node_role
  description: >-
    Node role detection for cluster topology
  collection_interval_seconds: 300
  retention_days: 30
```

## Extensibility Considerations

### Adding New Cluster Types

Developers can add support for new cluster types by
following these steps:

1. Add a detection query for the new extension.
2. Add columns to `pg_node_role` for type-specific data.
3. Update the role determination algorithm.
4. Add new `primary_role` values.

### Role Details JSON

The `role_details` JSONB column provides flexibility for
extension-specific data without schema changes.

```json
{
    "spock": {
        "replication_sets": [
            "default",
            "ddl_sql"
        ],
        "conflict_resolution": "last_update_wins"
    },
    "logical": {
        "publications": ["pub1", "pub2"],
        "subscriptions": ["sub1"]
    }
}
```

## Implementation Notes

### Change Detection

Both probes use change detection to avoid storing
duplicate data:

- The `pg_server_info` probe stores a row only when any
  value changes.
- The `pg_node_role` probe stores a row only when the
  role or key metrics change.

### Error Handling

The probe implementation follows these error handling
rules:

- Extension queries gracefully handle missing extensions.
- Standby-specific queries run only when
  `is_in_recovery` is true.
- Permission errors are logged but do not fail the probe.

### Performance

The probe queries are lightweight and efficient:

- All queries access only system catalogs.
- No queries scan user data tables.
- The probes can run on standbys without affecting
  replication.

## Usage Examples

### Find All Spock Nodes

The following query retrieves the most recent role data
for all Spock-enabled nodes.

```sql
SELECT c.name, c.host,
       r.spock_node_name, r.primary_role
FROM metrics.pg_node_role r
JOIN connections c ON c.id = r.connection_id
WHERE r.has_spock = true
  AND r.collected_at = (
      SELECT MAX(collected_at)
      FROM metrics.pg_node_role
      WHERE connection_id = r.connection_id
  );
```

### Cluster Topology Overview

The following query provides a snapshot of the current
cluster topology.

```sql
SELECT
    c.name,
    r.primary_role,
    r.role_flags,
    r.is_in_recovery,
    r.has_binary_standbys,
    r.binary_standby_count
FROM metrics.pg_node_role r
JOIN connections c ON c.id = r.connection_id
WHERE r.collected_at > NOW() - INTERVAL '1 hour'
ORDER BY r.primary_role, c.name;
```

### Detect Role Changes

The following query identifies nodes that changed roles
within the past seven days.

```sql
SELECT
    c.name,
    r.primary_role,
    r.collected_at,
    LAG(r.primary_role) OVER (
        PARTITION BY r.connection_id
        ORDER BY r.collected_at
    ) AS previous_role
FROM metrics.pg_node_role r
JOIN connections c ON c.id = r.connection_id
WHERE r.collected_at > NOW() - INTERVAL '7 days'
HAVING r.primary_role != LAG(r.primary_role)
    OVER (
        PARTITION BY r.connection_id
        ORDER BY r.collected_at
    );
```

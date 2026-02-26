# Node Role Probe Design

This document describes the design for probes that detect and track PostgreSQL
node roles within various cluster topologies.

## Overview

PostgreSQL servers can participate in various replication configurations:

- Standalone (no replication)
- Binary (physical/streaming) replication clusters
- Native logical replication setups
- Spock multi-master clusters

A single node may have multiple roles simultaneously (e.g., a Spock node that
also has a binary standby for HA). The probe system must accurately detect
these configurations and store them for analysis.

## Node Role Taxonomy

### Primary Roles

| Role | Description | Detection Method |
|------|-------------|------------------|
| `standalone` | No replication configured | No standbys, not in recovery, no publications/subscriptions |
| `binary_primary` | Source for physical replication | Has entries in pg_stat_replication (type=physical) |
| `binary_standby` | Physical replication target | pg_is_in_recovery() = true, has wal_receiver |
| `binary_cascading` | Standby that is also a primary | In recovery AND has standbys |
| `logical_publisher` | Native logical replication source | Has publications |
| `logical_subscriber` | Native logical replication target | Has subscriptions |
| `spock_node` | Active Spock multi-master node | Spock extension, node in spock.node |
| `spock_standby` | Binary standby of Spock node | In recovery, primary is Spock node |

### Role Flags (Non-Exclusive)

Since roles can combine, we track individual capability flags:

- `is_in_recovery` - Fundamental: in standby mode
- `has_binary_standbys` - Has physical replication standbys
- `has_publications` - Has logical replication publications
- `has_subscriptions` - Has logical replication subscriptions
- `has_spock` - Spock extension installed and active

## Database Schema

### Table: metrics.pg_server_info

Stores relatively static server identification information. This data changes
rarely (only on upgrades or major configuration changes).

```sql
CREATE TABLE IF NOT EXISTS metrics.pg_server_info (
    connection_id INTEGER NOT NULL,

    -- Server Identification
    server_version TEXT,                    -- e.g., "17.2"
    server_version_num INTEGER,             -- e.g., 170200
    system_identifier BIGINT,               -- Unique cluster identifier
    cluster_name TEXT,                      -- postgresql.conf cluster_name
    data_directory TEXT,                    -- PGDATA path

    -- Configuration
    max_connections INTEGER,
    max_wal_senders INTEGER,
    max_replication_slots INTEGER,
    wal_level TEXT,                         -- minimal, replica, logical

    -- Extensions (for role detection)
    installed_extensions TEXT[],            -- Array of installed extensions

    collected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (connection_id, collected_at)
) PARTITION BY RANGE (collected_at);

COMMENT ON TABLE metrics.pg_server_info IS
    'Server identification and configuration - only stores snapshots when changes detected';
```

### Table: metrics.pg_node_role

Stores node role detection results. This may change more dynamically as
replication configurations evolve.

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
    upstream_host TEXT,                     -- For standbys: primary host
    upstream_port INTEGER,                  -- For standbys: primary port
    received_lsn TEXT,                      -- Last received LSN
    replayed_lsn TEXT,                      -- Last replayed LSN

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
    primary_role TEXT NOT NULL,             -- Main role for filtering

    -- Role Flags (non-exclusive capabilities)
    role_flags TEXT[] NOT NULL DEFAULT '{}', -- Array: ['binary_primary', 'logical_publisher', ...]

    -- Extended Information (JSON for flexibility)
    role_details JSONB,                     -- Additional role-specific details

    collected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (connection_id, collected_at)
) PARTITION BY RANGE (collected_at);

COMMENT ON TABLE metrics.pg_node_role IS
    'Node role detection for cluster topology analysis';

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_pg_node_role_primary_role
    ON metrics.pg_node_role(connection_id, primary_role, collected_at DESC);
CREATE INDEX IF NOT EXISTS idx_pg_node_role_collected_at
    ON metrics.pg_node_role(collected_at DESC);
```

## Detection Logic

### Query: Server Info

```sql
SELECT
    current_setting('server_version') as server_version,
    current_setting('server_version_num')::integer as server_version_num,
    (SELECT system_identifier FROM pg_control_system()) as system_identifier,
    current_setting('cluster_name', true) as cluster_name,
    current_setting('data_directory') as data_directory,
    current_setting('max_connections')::integer as max_connections,
    current_setting('max_wal_senders')::integer as max_wal_senders,
    current_setting('max_replication_slots')::integer as max_replication_slots,
    current_setting('wal_level') as wal_level,
    (SELECT array_agg(extname) FROM pg_extension) as installed_extensions
```

### Query: Basic Role Detection

```sql
SELECT
    pg_is_in_recovery() as is_in_recovery,
    (SELECT timeline_id FROM pg_control_checkpoint()) as timeline_id,

    -- Binary replication
    (SELECT count(*) FROM pg_stat_replication
     WHERE state = 'streaming') as binary_standby_count,

    -- Logical replication
    (SELECT count(*) FROM pg_publication) as publication_count,
    (SELECT count(*) FROM pg_subscription) as subscription_count,
    (SELECT count(*) FROM pg_stat_subscription
     WHERE subrelid IS NULL AND pid IS NOT NULL) as active_subscription_count
```

### Query: Standby Info (when is_in_recovery = true)

```sql
SELECT
    sender_host as upstream_host,
    sender_port as upstream_port,
    received_lsn::text,
    (SELECT replay_lsn::text FROM pg_stat_get_wal_receiver())
FROM pg_stat_wal_receiver
LIMIT 1
```

### Query: Spock Detection

```sql
-- Check if Spock is installed
SELECT EXISTS (
    SELECT 1 FROM pg_extension WHERE extname = 'spock'
) as has_spock;

-- If Spock exists, get node info
SELECT
    node_id as spock_node_id,
    node_name as spock_node_name,
    (SELECT count(*) FROM spock.subscription
     WHERE sub_enabled = true) as spock_subscription_count
FROM spock.local_node
LIMIT 1;
```

## Role Determination Algorithm

```go
func determineNodeRole(info *NodeRoleInfo) (string, []string) {
    var flags []string

    // Detect individual capabilities
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

    // Determine primary role (most specific applicable role)
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
    case info.PublicationCount > 0 && info.SubscriptionCount > 0:
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

```yaml
# Default probe configuration
- name: pg_server_info
  description: "Server identification and configuration (change-tracked)"
  collection_interval_seconds: 3600  # Check hourly
  retention_days: 365                # Keep for a year

- name: pg_node_role
  description: "Node role detection for cluster topology"
  collection_interval_seconds: 300   # Check every 5 minutes
  retention_days: 30                 # Keep for a month
```

## Extensibility Considerations

### Adding New Cluster Types

1. Add detection query for the new extension (e.g., `has_newtype`)
2. Add columns to `pg_node_role` for type-specific info
3. Update role determination algorithm
4. Add new `primary_role` values

### Role Details JSON

The `role_details` JSONB column provides flexibility for extension-specific
data without schema changes:

```json
{
    "spock": {
        "replication_sets": ["default", "ddl_sql"],
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

Both probes should use change detection to avoid storing duplicate data:

- `pg_server_info`: Store only when any value changes
- `pg_node_role`: Store only when role or key metrics change

### Error Handling

- Extension queries should gracefully handle missing extensions
- Standby-specific queries should only run when `is_in_recovery = true`
- Permission errors should be logged but not fail the probe

### Performance

- Queries are lightweight (system catalogs only)
- No table scans on user data
- Can run on standbys without impacting replication

## Usage Examples

### Find All Spock Nodes

```sql
SELECT c.name, c.host, r.spock_node_name, r.primary_role
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

```sql
SELECT
    c.name,
    r.primary_role,
    r.collected_at,
    LAG(r.primary_role) OVER (
        PARTITION BY r.connection_id
        ORDER BY r.collected_at
    ) as previous_role
FROM metrics.pg_node_role r
JOIN connections c ON c.id = r.connection_id
WHERE r.collected_at > NOW() - INTERVAL '7 days'
HAVING r.primary_role != LAG(r.primary_role) OVER (
    PARTITION BY r.connection_id
    ORDER BY r.collected_at
);
```

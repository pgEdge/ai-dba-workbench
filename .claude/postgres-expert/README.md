# PostgreSQL Database Expert Documentation

This directory contains comprehensive documentation of the pgEdge AI DBA Workbench
PostgreSQL database implementation. This documentation is designed to enable
expert-level guidance on future database development, optimization, and
troubleshooting.

## Documentation Files

### [schema-overview.md](schema-overview.md)
**Purpose:** High-level overview of the database architecture

**Contents:**
- Database purpose and role in the system
- Schema organization (core tables, metrics schema, RBAC tables)
- Key design patterns (partitioning, ownership model, hierarchical groups)
- Performance considerations
- PostgreSQL version requirements
- Security model
- Data retention strategies
- Common queries

**When to use:**
- Getting started with the database architecture
- Understanding the overall system design
- Quick reference for table organization
- Security and access control overview

### [migration-history.md](migration-history.md)
**Purpose:** Complete changelog of all database schema migrations

**Contents:**
- Detailed description of each migration (1-6)
- Migration system mechanics
- Tables created in each migration
- Migration best practices
- Rollback procedures
- Version compatibility matrix

**When to use:**
- Planning new migrations
- Understanding why tables are structured a certain way
- Troubleshooting migration issues
- Reviewing what changed between versions
- Planning upgrades

### [privilege-system.md](privilege-system.md)
**Purpose:** In-depth documentation of the RBAC system (Migration 6)

**Contents:**
- Complete RBAC architecture
- Entity hierarchy and relationships
- Detailed table descriptions (user_groups, group_memberships, etc.)
- Authorization flow diagrams
- Privilege resolution algorithms
- Token scoping mechanics
- Common privilege patterns (read-only, DBA, nested teams)
- Performance considerations for recursive queries
- Security best practices
- Troubleshooting privilege issues

**When to use:**
- Implementing new privilege features
- Debugging authorization failures
- Designing group hierarchies
- Implementing token scoping
- Performance tuning authorization queries
- Understanding the 29 privilege management tools

### [performance-notes.md](performance-notes.md)
**Purpose:** Performance tuning, optimization, and operational best practices

**Contents:**
- Workload characteristics (OLTP + time-series)
- Complete indexing strategy for all tables
- Index bloat management
- Partitioning strategy and management
- PostgreSQL configuration tuning (memory, autovacuum, checkpoints, WAL)
- Connection pooling recommendations
- Monitoring queries and critical metrics
- Backup and recovery procedures
- Performance tuning checklist
- Troubleshooting common performance issues

**When to use:**
- Production deployment planning
- Performance troubleshooting
- Capacity planning
- Index design decisions
- Configuring PostgreSQL parameters
- Setting up monitoring
- Planning backup strategies
- Diagnosing slow queries

### [relationships.md](relationships-md)
**Purpose:** Entity relationships, foreign keys, and referential integrity

**Contents:**
- Complete ER diagrams (ASCII art)
- All foreign key relationships with CASCADE/RESTRICT rules
- Unique constraints and their purposes
- Check constraints and validation rules
- Referential integrity patterns
- Orphan prevention strategies
- Relationship cardinality (1:N, M:N)
- Data integrity rules summary

**When to use:**
- Understanding table relationships
- Planning schema changes
- Debugging referential integrity errors
- Understanding cascade behavior
- Designing new relationships
- Preventing orphaned data
- Generating ER diagrams

## Quick Reference

### Database Statistics

- **Total Tables**: ~45 (6 core, ~35 metrics, 9 RBAC, 4 token scoping)
- **Schemas**: `public` (core), `metrics` (time-series data)
- **Migrations**: 6 (consolidated from original 43)
- **Partitioned Tables**: All tables in `metrics` schema
- **Foreign Keys**: 25+ relationships with CASCADE/RESTRICT rules
- **Indexes**: 70+ indexes (primary keys, foreign keys, lookups, partials)

### Key Tables by Function

**Authentication:**
- user_accounts
- service_tokens
- user_tokens
- user_sessions

**Authorization (RBAC):**
- user_groups
- group_memberships
- mcp_privilege_identifiers
- group_mcp_privileges
- group_connection_privileges

**Token Scoping:**
- user_token_connection_scope
- user_token_mcp_scope
- service_token_connection_scope
- service_token_mcp_scope

**Connection Management:**
- connections
- probe_configs

**Metrics Collection:**
- metrics.pg_stat_activity
- metrics.pg_stat_database
- metrics.pg_stat_replication
- ... (35 total metrics tables)

### Common Tasks

#### Check Current Schema Version
```sql
SELECT * FROM schema_version ORDER BY version DESC;
```

#### View All Tables and Sizes
```sql
SELECT schemaname, tablename,
       pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as
size
FROM pg_tables
WHERE schemaname IN ('public', 'metrics')
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

#### Check User's Effective Privileges
```sql
-- See privilege-system.md for complete queries
WITH RECURSIVE user_groups AS (...)
SELECT * FROM group_mcp_privileges WHERE group_id IN (...);
```

#### Monitor Authentication Performance
```sql
SELECT query, mean_exec_time, calls
FROM pg_stat_statements
WHERE query LIKE '%user_accounts%'
ORDER BY mean_exec_time DESC
LIMIT 10;
```

#### List Active Monitoring
```sql
SELECT c.name, c.is_monitored, COUNT(pc.id) as enabled_probes
FROM connections c
LEFT JOIN probe_configs pc ON pc.connection_id = c.id AND pc.is_enabled
GROUP BY c.id
ORDER BY c.name;
```

## Development Guidelines

### Before Making Schema Changes

1. **Read migration-history.md** - Understand migration patterns
2. **Review relationships.md** - Check foreign key impacts
3. **Check performance-notes.md** - Consider index requirements
4. **Design for backward compatibility** - Use IF NOT EXISTS
5. **Test migration on copy** - Never test on production first

### Adding New Migrations

1. Create new migration in `collector/src/database/schema.go`
2. Use next sequential version number (7, 8, 9...)
3. Include comprehensive COMMENT ON statements
4. Add appropriate indexes for new columns
5. Update this documentation:
   - migration-history.md (add migration details)
   - schema-overview.md (if architecture changes)
   - relationships.md (if new FKs added)
   - performance-notes.md (if new indexes)
   - privilege-system.md (if RBAC changes)

### Adding New Metrics Tables

1. Follow partitioned table pattern from Migration 1
2. Include connection_id and collected_at columns
3. Create indexes on (collected_at) and (connection_id, collected_at)
4. Add to probe_configs default inserts
5. Consider partition sizing (daily vs weekly vs monthly)
6. Do NOT add foreign key to connections (performance)
7. Document in schema-overview.md metrics section

### Modifying Privilege System

1. **Understand privilege flow** - Read privilege-system.md thoroughly
2. **Test cascade effects** - Deleting groups cascades widely
3. **Prevent circular groups** - Application must validate
4. **Cache consideration** - Changes may require cache invalidation
5. **API impacts** - Update MCP tools if needed

## PostgreSQL Version Requirements

**Minimum Version:** PostgreSQL 13

**Recommended Version:** PostgreSQL 14+

**Key Features Used:**
- GENERATED ALWAYS AS IDENTITY (PG 10+)
- Declarative partitioning (PG 10+, improved in 11+)
- pg_stat_* views (some added in PG 13+)
- Recursive CTEs (PG 8.4+, but optimized in recent versions)

**Upgrade Considerations:**
- All migrations compatible with PG 13+
- No version-specific SQL syntax used
- Some pg_stat_* views may not exist on monitored servers < PG 13
  (collector handles gracefully)

## Production Deployment Checklist

### Database Setup
- [ ] PostgreSQL 14+ installed
- [ ] Dedicated database created: `ai_workbench`
- [ ] Collector user created with appropriate permissions
- [ ] Connection pooling configured (application or pgBouncer)

### Configuration
- [ ] Memory settings tuned (see performance-notes.md)
- [ ] Autovacuum configured for high-churn tables
- [ ] Checkpoint and WAL settings optimized
- [ ] max_connections set appropriately

### Monitoring
- [ ] pg_stat_statements extension enabled
- [ ] Monitoring queries scheduled (see performance-notes.md)
- [ ] Disk usage alerts configured
- [ ] Slow query logging enabled

### Backup
- [ ] WAL archiving configured
- [ ] Base backup schedule (weekly recommended)
- [ ] Logical backup schedule (daily recommended)
- [ ] Recovery tested on non-production server

### Partitioning
- [ ] Partition creation automated (daily cron or pg_cron)
- [ ] Partition cleanup automated (retention-based)
- [ ] Partition monitoring alerts configured

### Security
- [ ] TLS/SSL enabled for connections
- [ ] Password encryption at rest configured
- [ ] Superuser accounts limited
- [ ] Regular privilege audits scheduled

## Troubleshooting Guide

### Schema Issues

**Problem:** Migration fails to apply

**Check:**
1. Current schema version: `SELECT * FROM schema_version;`
2. PostgreSQL version: `SELECT version();`
3. Migration log output from collector
4. Check for conflicting objects (tables, indexes)

**Solution:**
- Review migration-history.md for that migration
- Check if migration already partially applied
- Verify PostgreSQL version compatibility

### Performance Issues

**Problem:** Slow authentication

**Check:**
1. Query performance: `EXPLAIN ANALYZE SELECT * FROM user_accounts WHERE username = '...';`
2. Index usage: `SELECT * FROM pg_stat_user_indexes WHERE tablename = 'user_accounts';`
3. Table bloat: `SELECT n_dead_tup FROM pg_stat_user_tables WHERE tablename = 'user_accounts';`

**Solution:**
- See performance-notes.md "Troubleshooting Common Issues"
- Run VACUUM ANALYZE on affected tables
- Verify indexes exist and are being used

### Privilege Issues

**Problem:** User cannot access tool/connection

**Check:**
1. User's group memberships (see privilege-system.md troubleshooting section)
2. Group's privileges
3. Token scoping restrictions (if using token)

**Solution:**
- Use queries from privilege-system.md "Troubleshooting" section
- Verify group hierarchy is correct
- Check token scope hasn't over-restricted access

### Disk Space Issues

**Problem:** Disk filling up rapidly

**Check:**
1. Table sizes: `SELECT ... pg_total_relation_size ...`
2. Partition count: `SELECT COUNT(*) FROM pg_tables WHERE schemaname = 'metrics';`
3. Dead tuples: `SELECT SUM(n_dead_tup) FROM pg_stat_user_tables WHERE schemaname = 'metrics';`

**Solution:**
- Drop old partitions (see performance-notes.md)
- Run VACUUM FULL during maintenance window (locks table)
- Adjust retention periods in probe_configs

## Additional Resources

### PostgreSQL Official Documentation
- [PostgreSQL 14 Documentation](https://www.postgresql.org/docs/14/)
- [Partitioning](https://www.postgresql.org/docs/14/ddl-partitioning.html)
- [Indexes](https://www.postgresql.org/docs/14/indexes.html)
- [pg_stat_statements](https://www.postgresql.org/docs/14/pgstatstatements.html)

### Project Documentation
- `/Users/dpage/git/ai-workbench/docs/authentication.md` - Authentication system
- `/Users/dpage/git/ai-workbench/docs/api-reference-privilege-tools.md` - 29 privilege management tools
- `/Users/dpage/git/ai-workbench/collector/src/database/schema.go` - Source of truth for migrations
- `/Users/dpage/git/ai-workbench/collector/src/database/migrations/README.md` - Migration system overview

### Tools
- **psql** - Interactive PostgreSQL client
- **pgAdmin** - GUI for PostgreSQL administration
- **pg_dump/pg_restore** - Backup and restore utilities
- **EXPLAIN/EXPLAIN ANALYZE** - Query plan analysis
- **pg_stat_statements** - Query performance tracking

## Maintenance Schedule

### Daily
- Monitor authentication performance
- Check disk usage
- Verify collector is running and inserting metrics
- Review slow query log

### Weekly
- Review table bloat statistics
- Check partition creation (ensure future partitions exist)
- Drop old partitions (retention cleanup)
- Review backup success

### Monthly
- Analyze slow queries from pg_stat_statements
- Review and optimize indexes
- Capacity planning review
- Privilege audit (review group memberships and grants)

### Quarterly
- Test backup restore procedure
- Review PostgreSQL configuration
- Plan for PostgreSQL minor version upgrade
- Performance tuning review

## Getting Help

### Internal Resources
1. Review this documentation first
2. Check collector logs for errors
3. Use EXPLAIN ANALYZE for query issues
4. Check PostgreSQL logs for database errors

### External Resources
1. PostgreSQL mailing lists
2. Stack Overflow (postgresql tag)
3. PostgreSQL Slack community
4. #postgresql on Libera.Chat IRC

### Escalation
For critical production issues:
1. Check recent migrations (may need rollback)
2. Review recent configuration changes
3. Check for OS/hardware issues
4. Consider PostgreSQL bug reports (if suspected)

## Contributing to This Documentation

When making changes to the database schema:

1. **Update affected documentation files** in this directory
2. **Keep examples current** - Test all SQL examples
3. **Update version compatibility** - Note any new PostgreSQL features used
4. **Add troubleshooting entries** - Document common issues encountered
5. **Review completeness** - Ensure all new tables/columns documented

### Documentation Standards

- Use clear, concise language
- Include practical examples
- Explain the "why" not just the "what"
- Keep SQL examples tested and working
- Use consistent formatting
- Include performance implications
- Note security considerations

## Document Revision History

| Date | Changes | Author |
|------|---------|--------|
| 2025-01-08 | Initial comprehensive documentation created | PostgreSQL Expert Agent |

## License

This documentation is part of the pgEdge AI DBA Workbench project.

Copyright (c) 2025 - 2026, pgEdge, Inc.
This documentation is released under The PostgreSQL License.

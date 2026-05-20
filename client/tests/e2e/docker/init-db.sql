-- PostgreSQL initialization script for the RPM-mode E2E test stack.
--
-- Runs automatically after server migrations complete via rpm-entrypoint.sh.
-- Executed as the postgres superuser against the ai_workbench database.
--
-- Sets up:
--   - pgvector extension
--   - metrics schema
--   - dba_collector, dba_server, dba_alerter roles with appropriate grants

-- ---------------------------------------------------------------------------
-- Extensions
-- ---------------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS vector;

-- ---------------------------------------------------------------------------
-- Schemas
-- ---------------------------------------------------------------------------
CREATE SCHEMA IF NOT EXISTS metrics;

-- ---------------------------------------------------------------------------
-- dba_collector role
-- ---------------------------------------------------------------------------
CREATE ROLE dba_collector WITH LOGIN PASSWORD 'test';

ALTER SCHEMA metrics OWNER TO dba_collector;

GRANT CREATE ON DATABASE ai_workbench TO dba_collector;
GRANT ALL ON SCHEMA public TO dba_collector;
GRANT ALL ON SCHEMA metrics TO dba_collector;
GRANT ALL ON ALL TABLES IN SCHEMA public TO dba_collector;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO dba_collector;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO dba_collector;
ALTER DEFAULT PRIVILEGES IN SCHEMA metrics GRANT ALL ON TABLES TO dba_collector;

-- ---------------------------------------------------------------------------
-- dba_server role
-- ---------------------------------------------------------------------------
CREATE USER dba_server WITH PASSWORD 'test';

GRANT USAGE ON SCHEMA public TO dba_server;
GRANT USAGE ON SCHEMA metrics TO dba_server;

-- Public schema — full CRUD tables
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE connections TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE clusters TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE cluster_groups TO dba_server;
GRANT SELECT, INSERT, DELETE ON TABLE cluster_node_relationships TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE probe_configs TO dba_server;
GRANT SELECT, UPDATE ON TABLE alert_rules TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE alert_thresholds TO dba_server;
GRANT SELECT, UPDATE ON TABLE alerts TO dba_server;
GRANT SELECT, INSERT ON TABLE alert_acknowledgments TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE blackouts TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE blackout_schedules TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE notification_channels TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE notification_channel_overrides TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE email_recipients TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE conversations TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE chat_memories TO dba_server;

-- Public schema — read-only tables
GRANT SELECT ON TABLE schema_version TO dba_server;
GRANT SELECT ON TABLE alerter_settings TO dba_server;
GRANT SELECT ON TABLE probe_availability TO dba_server;
GRANT SELECT ON TABLE metric_definitions TO dba_server;
GRANT SELECT ON TABLE metric_baselines TO dba_server;
GRANT SELECT ON TABLE correlation_groups TO dba_server;
GRANT SELECT ON TABLE anomaly_candidates TO dba_server;
GRANT SELECT ON TABLE connection_notification_channels TO dba_server;
GRANT SELECT ON TABLE notification_history TO dba_server;
GRANT SELECT ON TABLE notification_reminder_state TO dba_server;

-- Metrics schema — read-only
GRANT SELECT ON ALL TABLES IN SCHEMA metrics TO dba_server;
ALTER DEFAULT PRIVILEGES IN SCHEMA metrics GRANT SELECT ON TABLES TO dba_server;

-- Sequences
GRANT USAGE ON SEQUENCE connections_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE clusters_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE cluster_groups_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE cluster_node_relationships_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE probe_configs_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE alert_thresholds_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE alert_acknowledgments_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE blackouts_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE blackout_schedules_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE notification_channels_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE notification_channel_overrides_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE email_recipients_id_seq TO dba_server;
GRANT USAGE ON SEQUENCE chat_memories_id_seq TO dba_server;
GRANT SELECT, INSERT, UPDATE, DELETE ON alert_acknowledgments TO dba_server;

-- ---------------------------------------------------------------------------
-- dba_alerter role
-- ---------------------------------------------------------------------------
CREATE USER dba_alerter WITH PASSWORD 'test';

GRANT USAGE ON SCHEMA public TO dba_alerter;
GRANT USAGE ON SCHEMA metrics TO dba_alerter;

-- Metrics schema — read-only
GRANT SELECT ON ALL TABLES IN SCHEMA metrics TO dba_alerter;
ALTER DEFAULT PRIVILEGES IN SCHEMA metrics GRANT SELECT ON TABLES TO dba_alerter;

-- Public schema — read-only tables
GRANT SELECT ON TABLE connections TO dba_alerter;
GRANT SELECT ON TABLE clusters TO dba_alerter;
GRANT SELECT ON TABLE alerter_settings TO dba_alerter;
GRANT SELECT ON TABLE alert_rules TO dba_alerter;
GRANT SELECT ON TABLE alert_thresholds TO dba_alerter;
GRANT SELECT ON TABLE blackout_schedules TO dba_alerter;
GRANT SELECT ON TABLE probe_availability TO dba_alerter;
GRANT SELECT ON TABLE probe_configs TO dba_alerter;
GRANT SELECT ON TABLE notification_channel_overrides TO dba_alerter;
GRANT SELECT ON TABLE alert_acknowledgments TO dba_alerter;
GRANT SELECT ON TABLE blackouts TO dba_alerter;

-- Public schema — read/write tables
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE alerts TO dba_alerter;
GRANT USAGE, SELECT ON SEQUENCE alerts_id_seq TO dba_alerter;

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE anomaly_candidates TO dba_alerter;
GRANT USAGE, SELECT ON SEQUENCE anomaly_candidates_id_seq TO dba_alerter;

GRANT SELECT, INSERT, UPDATE ON TABLE anomaly_embeddings TO dba_alerter;
GRANT USAGE, SELECT ON SEQUENCE anomaly_embeddings_id_seq TO dba_alerter;

GRANT SELECT, INSERT, UPDATE ON TABLE metric_baselines TO dba_alerter;
GRANT USAGE, SELECT ON SEQUENCE metric_baselines_id_seq TO dba_alerter;

GRANT SELECT, INSERT ON TABLE blackouts TO dba_alerter;
GRANT USAGE, SELECT ON SEQUENCE blackouts_id_seq TO dba_alerter;

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE notification_channels TO dba_alerter;
GRANT USAGE, SELECT ON SEQUENCE notification_channels_id_seq TO dba_alerter;

GRANT SELECT, INSERT, UPDATE, DELETE ON alert_acknowledgments TO dba_alerter;

# Verifying the Health of Individual Components

After starting all components, you can verify the state of each component in
the installation with the following commands.

## Checking the Collector

The collector logs probe executions to `stderr`. Use the following
command to confirm the collector is running:

```bash
sudo systemctl status pgedge-ai-dba-collector
```

A successful response confirms the collector is running:

```bash
pgedge-ai-dba-collector.service - pgEdge AI DBA Workbench Collector
 Loaded: loaded (/etc/systemd/system/pgedge-ai-dba-collector.service; enabled; vendor preset: enabled)
 Active: active (running) since Tue 2026-05-12 14:45:17 UTC; 24h ago
Main PID: 59722 (ai-dba-collecto)
   Tasks: 12 (limit: 4527)
  Memory: 16.3M
     CPU: 4.233s
  CGroup: /system.slice/pgedge-ai-dba-collector.service
          └─59722 /opt/ai-workbench/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml

May 12 14:45:17 n1 systemd[1]: Started pgEdge AI DBA Workbench Collector.
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 pgEdge AI DBA Workbench Collector v1.0.0-beta1 starting...
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Configuration loaded from: /etc/pgedge/ai-dba-collector.yaml
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Schema is up to date
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Datastore connection established
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Probe scheduler started
May 12 14:45:17 n1 ai-dba-collector[59722]: 2026/05/12 14:45:17 Collector is running. Press Ctrl+C to stop.
```

## Checking the Server

The server listens on the configured HTTP port. Use the following
command to test connectivity:

```bash
curl -s http://localhost:8080/health
```

A successful response confirms the server is running and accepting
requests:

```bash
curl -s http://localhost:8080/health
{"status":"ok","server":"pgedge-postgres-mcp","version":"1.0.0-beta1"}
```

## Checking the Alerter

The alerter logs rule evaluations to `stderr`. Use the following
command to confirm the alerter is running:

```bash
sudo systemctl status pgedge-ai-dba-alerter
```

A successful response confirms the alerter is running:

```bash
pgedge-ai-dba-alerter.service - pgEdge AI DBA Workbench Alerter
 Loaded: loaded (/etc/systemd/system/pgedge-ai-dba-alerter.service; enabled; vendor preset: enabled)
 Active: active (running) since Tue 2026-05-12 15:32:55 UTC; 23h ago
Main PID: 63620 (ai-dba-alerter)
   Tasks: 12 (limit: 4527)
  Memory: 6.6M
     CPU: 6.382s
  CGroup: /system.slice/pgedge-ai-dba-alerter.service
          └─63620 /opt/ai-workbench/ai-dba-alerter -config /etc/pgedge/ai-dba-alerter.yaml

May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Baseline calculator started (interval: 1h0m0s)
May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Alert cleaner started
May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
May 12 15:32:55 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 12 16:32:55 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
May 12 16:32:55 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 13 11:54:51 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
May 13 11:54:51 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
May 13 12:54:51 n1 ai-dba-alerter[63620]: [alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
May 13 12:54:51 n1 ai-dba-alerter[63620]: [alerter] Baseline calculation complete
```

## Checking Metrics Collection

Connect to the datastore and run the following query to verify that
metrics tables contain recent data.

In the following example, the `psql` command connects to the datastore:

```bash
sudo -u postgres psql -d ai_workbench
psql (18.3 (Ubuntu 18.3-1.pgdg22.04+1))
Type "help" for help.
```

In the following example, the `SELECT` statement queries the
`metrics.pg_stat_activity` table for a row count and the most recent
collection timestamp:

```sql
SELECT COUNT(*), MAX(collected_at) FROM metrics.pg_stat_activity;
 count |              max
-------+-------------------------------
  1014 | 2026-05-13 14:56:37.453882+00
(1 row)
```

A non-zero count with a recent timestamp confirms the collector is
gathering metrics.

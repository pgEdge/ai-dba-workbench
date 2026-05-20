# Configuring systemd Services

The following sections provide details about creating systemd service
files to run each component as a background service.

## Configuring the Collector Service

The collector service file configures the collector to start
automatically and restart on failure.

In the following example, the collector service file starts the
collector automatically and restarts the service on failure; replace
`user_name` with the name of the operating system user account that
owns the `/opt/ai-workbench/data` directory. Create the service file
at `/etc/systemd/system/pgedge-ai-dba-collector.service`:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Collector
After=network.target postgresql.service

[Service]
Type=simple
User=user_name
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-collector \
    -config /etc/pgedge/ai-dba-collector.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

## Configuring the Server Service

The server service file configures the server to start automatically
and restart on failure.

In the following example, the server service file starts the server
automatically and restarts the service on failure; replace `user_name`
with the name of the operating system user account that owns the
`/opt/ai-workbench/data` directory. Create the service file at
`/etc/systemd/system/pgedge-ai-dba-server.service`:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Server
After=network.target postgresql.service

[Service]
Type=simple
User=user_name
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-server \
    -config /etc/pgedge/ai-dba-server.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### Configuring the Alerter Service

The alerter service file configures the alerter to start automatically
and restart if the process exits.

In the following example, the alerter service file starts the alerter
automatically and restarts the service when the process exits; replace
`user_name` with the name of the operating system user account that
owns the `/opt/ai-workbench/data` directory. Create the service file
at `/etc/systemd/system/pgedge-ai-dba-alerter.service`:

```ini
[Unit]
Description=pgEdge AI DBA Workbench Alerter
After=network.target postgresql.service

[Service]
Type=simple
User=user_name
WorkingDirectory=/opt/ai-workbench
ExecStart=/opt/ai-workbench/ai-dba-alerter \
    -config /etc/pgedge/ai-dba-alerter.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

## Enabling and Starting the Services

Use `systemctl` to reload the daemon and enable each service.

In the following example, the `systemctl` commands reload the daemon,
enable all services, and start each one:

```bash
sudo systemctl daemon-reload
sudo systemctl enable pgedge-ai-dba-collector
sudo systemctl enable pgedge-ai-dba-server
sudo systemctl enable pgedge-ai-dba-alerter
sudo systemctl start pgedge-ai-dba-collector
sudo systemctl start pgedge-ai-dba-server
sudo systemctl start pgedge-ai-dba-alerter
```

In the following example, the `systemctl status` command checks the
status of each service:

```bash
sudo systemctl status pgedge-ai-dba-collector
sudo systemctl status pgedge-ai-dba-server
sudo systemctl status pgedge-ai-dba-alerter
```
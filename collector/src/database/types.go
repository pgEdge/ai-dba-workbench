/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import "database/sql"

// MonitoredConnection represents a PostgreSQL connection to monitor
type MonitoredConnection struct {
    ID                int
    Name              string
    Host              string
    HostAddr          sql.NullString
    Port              int
    DatabaseName      string
    Username          string
    PasswordEncrypted sql.NullString
    SSLMode           sql.NullString
    SSLCert           sql.NullString
    SSLKey            sql.NullString
    SSLRootCert       sql.NullString
    OwnerUsername     sql.NullString
    OwnerToken        sql.NullString
}

// Application identifiers
const ApplicationName = "pgEdge AI Workbench - Monitoring"

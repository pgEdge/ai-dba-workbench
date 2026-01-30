/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"database/sql"
	"time"
)

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
	UpdatedAt         time.Time
}

// ApplicationName identifies monitoring connections
const ApplicationName = "pgEdge AI DBA Workbench - Monitoring"

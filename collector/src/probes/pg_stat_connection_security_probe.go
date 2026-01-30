/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
)

// PgStatConnectionSecurityProbe collects combined SSL and GSSAPI connection security metrics
// This probe consolidates pg_stat_ssl and pg_stat_gssapi into a single collection
type PgStatConnectionSecurityProbe struct {
	BaseMetricsProbe
}

// NewPgStatConnectionSecurityProbe creates a new pg_stat_connection_security probe
func NewPgStatConnectionSecurityProbe(config *ProbeConfig) *PgStatConnectionSecurityProbe {
	return &PgStatConnectionSecurityProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatConnectionSecurityProbe) GetName() string {
	return ProbeNamePgStatConnectionSecurity
}

// GetTableName returns the metrics table name
func (p *PgStatConnectionSecurityProbe) GetTableName() string {
	return ProbeNamePgStatConnectionSecurity
}

// IsDatabaseScoped returns false as this is server-scoped
func (p *PgStatConnectionSecurityProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatConnectionSecurityProbe) GetQuery() string {
	return ""
}

// checkGSSAPIAvailable checks if pg_stat_gssapi view exists (PG 12+)
func (p *PgStatConnectionSecurityProbe) checkGSSAPIAvailable(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_gssapi'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for pg_stat_gssapi view: %w", err)
	}

	return exists, nil
}

// checkCredentialsDelegatedColumn checks if credentials_delegated column exists (PG 16+)
func (p *PgStatConnectionSecurityProbe) checkCredentialsDelegatedColumn(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'pg_catalog'
            AND table_name = 'pg_stat_gssapi'
            AND column_name = 'credentials_delegated'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for credentials_delegated column: %w", err)
	}

	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatConnectionSecurityProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if GSSAPI view is available
	gssapiAvailable, err := cachedCheck(connectionName, "gssapi_available", func() (bool, error) {
		return p.checkGSSAPIAvailable(ctx, monitoredConn)
	})
	if err != nil {
		return nil, err
	}

	var query string
	if gssapiAvailable {
		// Check for credentials_delegated column (PG 16+)
		hasCredentialsDelegated, err := cachedCheck(connectionName, "credentials_delegated_column", func() (bool, error) {
			return p.checkCredentialsDelegatedColumn(ctx, monitoredConn)
		})
		if err != nil {
			return nil, err
		}

		if hasCredentialsDelegated {
			// PG 16+ with credentials_delegated
			query = `
                SELECT
                    ssl.pid,
                    ssl.ssl,
                    ssl.version AS ssl_version,
                    ssl.cipher,
                    ssl.bits,
                    ssl.client_dn,
                    ssl.client_serial::text,
                    ssl.issuer_dn,
                    gss.gss_authenticated,
                    gss.principal,
                    gss.encrypted AS gss_encrypted,
                    gss.credentials_delegated
                FROM pg_stat_ssl ssl
                LEFT JOIN pg_stat_gssapi gss ON ssl.pid = gss.pid
            `
		} else {
			// PG 12-15 without credentials_delegated
			query = `
                SELECT
                    ssl.pid,
                    ssl.ssl,
                    ssl.version AS ssl_version,
                    ssl.cipher,
                    ssl.bits,
                    ssl.client_dn,
                    ssl.client_serial::text,
                    ssl.issuer_dn,
                    gss.gss_authenticated,
                    gss.principal,
                    gss.encrypted AS gss_encrypted,
                    NULL::boolean AS credentials_delegated
                FROM pg_stat_ssl ssl
                LEFT JOIN pg_stat_gssapi gss ON ssl.pid = gss.pid
            `
		}
	} else {
		// No GSSAPI view available, just query SSL
		query = `
            SELECT
                pid,
                ssl,
                version AS ssl_version,
                cipher,
                bits,
                client_dn,
                client_serial::text,
                issuer_dn,
                NULL::boolean AS gss_authenticated,
                NULL::text AS principal,
                NULL::boolean AS gss_encrypted,
                NULL::boolean AS credentials_delegated
            FROM pg_stat_ssl
        `
	}

	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatConnectionSecurityProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"pid", "ssl", "ssl_version", "cipher", "bits",
		"client_dn", "client_serial", "issuer_dn",
		"gss_authenticated", "principal", "gss_encrypted", "credentials_delegated",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["pid"],
			metric["ssl"],
			metric["ssl_version"],
			metric["cipher"],
			metric["bits"],
			metric["client_dn"],
			metric["client_serial"],
			metric["issuer_dn"],
			metric["gss_authenticated"],
			metric["principal"],
			metric["gss_encrypted"],
			metric["credentials_delegated"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgStatConnectionSecurityProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}

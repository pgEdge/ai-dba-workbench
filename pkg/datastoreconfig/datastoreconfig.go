/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package datastoreconfig provides the shared PostgreSQL connection
// configuration used by the collector and alerter sub-projects.
package datastoreconfig

// DatastoreConfig holds PostgreSQL connection settings for the
// monitoring datastore.
type DatastoreConfig struct {
	Host         string `yaml:"host"`          // PostgreSQL server hostname or IP address
	HostAddr     string `yaml:"hostaddr"`      // PostgreSQL server IP address (bypasses DNS)
	Database     string `yaml:"database"`      // Database name
	Username     string `yaml:"username"`      // Username for connection
	Password     string `yaml:"password"`      // Password (discouraged - use password_file or env var)
	PasswordFile string `yaml:"password_file"` // Path to file containing password
	Port         int    `yaml:"port"`          // PostgreSQL server port
	SSLMode      string `yaml:"sslmode"`       // SSL mode (disable, allow, prefer, require, verify-ca, verify-full)
	SSLCert      string `yaml:"sslcert"`       // Path to client SSL certificate
	SSLKey       string `yaml:"sslkey"`        // Path to client SSL private key
	SSLRootCert  string `yaml:"sslrootcert"`   // Path to root CA certificate
}

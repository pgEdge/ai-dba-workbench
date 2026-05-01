/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/datastoreconfig"
	"github.com/pgedge/ai-workbench/pkg/fileutil"
)

func TestNewConfig(t *testing.T) {
	config := NewConfig()

	if config.Datastore.Host != "localhost" {
		t.Errorf("Expected default Datastore.Host to be 'localhost', got '%s'", config.Datastore.Host)
	}

	if config.Datastore.Port != 5432 {
		t.Errorf("Expected default Datastore.Port to be 5432, got %d", config.Datastore.Port)
	}

	if config.Datastore.Database != "ai_workbench" {
		t.Errorf("Expected default Datastore.Database to be 'ai_workbench', got '%s'", config.Datastore.Database)
	}

	if config.Pool.DatastoreMaxWaitSeconds != 60 {
		t.Errorf("Expected default Pool.DatastoreMaxWaitSeconds to be 60, got %d", config.Pool.DatastoreMaxWaitSeconds)
	}

	if config.Pool.MonitoredMaxWaitSeconds != 60 {
		t.Errorf("Expected default Pool.MonitoredMaxWaitSeconds to be 60, got %d", config.Pool.MonitoredMaxWaitSeconds)
	}

	if config.Pool.MaxConnectionsPerServer != 3 {
		t.Errorf("Expected default Pool.MaxConnectionsPerServer to be 3, got %d", config.Pool.MaxConnectionsPerServer)
	}
}

func TestConfigLoadFromFile(t *testing.T) {
	// Create a temporary config file in YAML format
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")
	secretPath := filepath.Join(tmpDir, "test.secret")

	// Create secret file
	testSecret := "test-secret-from-file"
	if err := os.WriteFile(secretPath, []byte(testSecret+"\n"), 0600); err != nil {
		t.Fatalf("Failed to write test secret file: %v", err)
	}

	configContent := `# Test configuration
datastore:
  host: testhost
  port: 5433
  database: testdb
  username: testuser
secret_file: "` + secretPath + `"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	config := NewConfig()
	if err := config.LoadFromFile(configPath); err != nil {
		t.Fatalf("Failed to load config from file: %v", err)
	}

	if config.Datastore.Host != "testhost" {
		t.Errorf("Expected Datastore.Host to be 'testhost', got '%s'", config.Datastore.Host)
	}

	if config.Datastore.Port != 5433 {
		t.Errorf("Expected Datastore.Port to be 5433, got %d", config.Datastore.Port)
	}

	if config.Datastore.Database != "testdb" {
		t.Errorf("Expected Datastore.Database to be 'testdb', got '%s'", config.Datastore.Database)
	}

	if config.Datastore.Username != "testuser" {
		t.Errorf("Expected Datastore.Username to be 'testuser', got '%s'", config.Datastore.Username)
	}

	if config.SecretFile != secretPath {
		t.Errorf("Expected SecretFile to be '%s', got '%s'", secretPath, config.SecretFile)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: 60,
					DatastoreMaxWaitSeconds: 60,
					MaxConnectionsPerServer: 3,
					MonitoredMaxWaitSeconds: 60,
				},
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
			},
			wantErr: true,
		},
		{
			name: "missing database",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Database: "",
					Username: "testuser",
					Port:     5432,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     -1,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid pool_max_connections",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 0,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid pool_max_idle_seconds",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: -1,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid datastore_pool_max_wait_seconds",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: 60,
					DatastoreMaxWaitSeconds: 0,
					MonitoredMaxWaitSeconds: 60,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid monitored_pool_max_wait_seconds",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: 60,
					DatastoreMaxWaitSeconds: 60,
					MonitoredMaxWaitSeconds: -1,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid max_connections_per_server",
			config: &Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: 60,
					DatastoreMaxWaitSeconds: 60,
					MonitoredMaxWaitSeconds: 60,
					MaxConnectionsPerServer: 0,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadPasswordFromSecretFile(t *testing.T) {
	tmpDir := t.TempDir()
	passwordFile := filepath.Join(tmpDir, "password.txt")

	testPassword := "test-password-123"
	if err := os.WriteFile(passwordFile, []byte(testPassword+"\n"), 0600); err != nil {
		t.Fatalf("Failed to write test password file: %v", err)
	}

	password, err := fileutil.ReadTrimmedFileWithTilde(passwordFile)
	if err != nil {
		t.Fatalf("fileutil.ReadTrimmedFileWithTilde() error = %v", err)
	}

	if password != testPassword {
		t.Errorf("Expected password to be '%s', got '%s'", testPassword, password)
	}
}

func TestGetDefaultConfigPath(t *testing.T) {
	// With nothing set up the helper returns "" so the caller can
	// fall through to compiled-in defaults.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())

	binaryPath := "/usr/local/bin/ai-dba-collector"
	configPath := GetDefaultConfigPath(binaryPath)

	// Since neither the per-user config dir nor /etc/pgedge holds a
	// matching file, the helper should signal "no config found".
	// We accept the existing /etc/pgedge file if the developer
	// happens to have one installed locally.
	if configPath != "" && configPath != "/etc/pgedge/ai-dba-collector.yaml" {
		t.Errorf("Expected empty path or /etc/pgedge path, got %q", configPath)
	}
}

// TestGetDefaultConfigPath_UserDirHit confirms the wrapper returns
// the per-user config path when one exists. We use t.Setenv to
// redirect os.UserConfigDir() into a writable temp directory so
// the test does not depend on the developer's environment.
func TestGetDefaultConfigPath_UserDirHit(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error: %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	expected := filepath.Join(pgedgeDir, "ai-dba-collector.yaml")
	if err := os.WriteFile(expected, []byte("datastore:\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := GetDefaultConfigPath(""); got != expected {
		t.Errorf("GetDefaultConfigPath() = %q, want %q", got, expected)
	}
}

// TestGetDefaultSecretPath_UserDirHit mirrors the config test for
// the secret-file lookup wrapper.
func TestGetDefaultSecretPath_UserDirHit(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error: %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	expected := filepath.Join(pgedgeDir, "ai-dba-collector.secret")
	if err := os.WriteFile(expected, []byte("s3cr3t"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := GetDefaultSecretPath(""); got != expected {
		t.Errorf("GetDefaultSecretPath() = %q, want %q", got, expected)
	}
}

func TestConfigGetters(t *testing.T) {
	config := &Config{
		Datastore: datastoreconfig.DatastoreConfig{
			Host:        "testhost",
			HostAddr:    "192.168.1.1",
			Database:    "testdb",
			Username:    "testuser",
			Password:    "testpass",
			Port:        5433,
			SSLMode:     "require",
			SSLCert:     "/path/to/cert",
			SSLKey:      "/path/to/key",
			SSLRootCert: "/path/to/root",
		},
		Pool: PoolConfig{
			DatastoreMaxConnections: 20,
			DatastoreMaxIdleSeconds: 120,
			DatastoreMaxWaitSeconds: 30,
			MonitoredMaxWaitSeconds: 45,
		},
	}

	if config.GetPgHost() != "testhost" {
		t.Errorf("GetPgHost() = %s, want testhost", config.GetPgHost())
	}
	if config.GetPgHostAddr() != "192.168.1.1" {
		t.Errorf("GetPgHostAddr() = %s, want 192.168.1.1", config.GetPgHostAddr())
	}
	if config.GetPgDatabase() != "testdb" {
		t.Errorf("GetPgDatabase() = %s, want testdb", config.GetPgDatabase())
	}
	if config.GetPgUsername() != "testuser" {
		t.Errorf("GetPgUsername() = %s, want testuser", config.GetPgUsername())
	}
	if config.GetPgPassword() != "testpass" {
		t.Errorf("GetPgPassword() = %s, want testpass", config.GetPgPassword())
	}
	if config.GetPgPort() != 5433 {
		t.Errorf("GetPgPort() = %d, want 5433", config.GetPgPort())
	}
	if config.GetPgSSLMode() != "require" {
		t.Errorf("GetPgSSLMode() = %s, want require", config.GetPgSSLMode())
	}
	if config.GetPgSSLCert() != "/path/to/cert" {
		t.Errorf("GetPgSSLCert() = %s, want /path/to/cert", config.GetPgSSLCert())
	}
	if config.GetPgSSLKey() != "/path/to/key" {
		t.Errorf("GetPgSSLKey() = %s, want /path/to/key", config.GetPgSSLKey())
	}
	if config.GetPgSSLRootCert() != "/path/to/root" {
		t.Errorf("GetPgSSLRootCert() = %s, want /path/to/root", config.GetPgSSLRootCert())
	}
	if config.GetDatastorePoolMaxConnections() != 20 {
		t.Errorf("GetDatastorePoolMaxConnections() = %d, want 20", config.GetDatastorePoolMaxConnections())
	}
	if config.GetDatastorePoolMaxIdleSeconds() != 120 {
		t.Errorf("GetDatastorePoolMaxIdleSeconds() = %d, want 120", config.GetDatastorePoolMaxIdleSeconds())
	}
	if config.GetDatastorePoolMaxWaitSeconds() != 30 {
		t.Errorf("GetDatastorePoolMaxWaitSeconds() = %d, want 30", config.GetDatastorePoolMaxWaitSeconds())
	}
	if config.GetMonitoredPoolMaxWaitSeconds() != 45 {
		t.Errorf("GetMonitoredPoolMaxWaitSeconds() = %d, want 45", config.GetMonitoredPoolMaxWaitSeconds())
	}
}

func TestReadSecretFile(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "secret.txt")

	testSecret := "test-secret-value-123"
	if err := os.WriteFile(secretFile, []byte(testSecret+"\n"), 0600); err != nil {
		t.Fatalf("Failed to write test secret file: %v", err)
	}

	secret, err := fileutil.ReadTrimmedFileWithTilde(secretFile)
	if err != nil {
		t.Fatalf("fileutil.ReadTrimmedFileWithTilde() error = %v", err)
	}

	if secret != testSecret {
		t.Errorf("Expected secret to be '%s', got '%s'", testSecret, secret)
	}
}

func TestReadSecretFile_NotFound(t *testing.T) {
	_, err := fileutil.ReadTrimmedFileWithTilde("/nonexistent/path/to/secret.txt")
	if err == nil {
		t.Error("Expected error when reading non-existent secret file")
	}
}

func TestConfigLoadFromFile_NotFound(t *testing.T) {
	config := NewConfig()
	err := config.LoadFromFile("/nonexistent/path/to/config.yaml")
	if err == nil {
		t.Error("Expected error when loading non-existent config file")
	}
}

func TestConfigLoadFromFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad.yaml")

	// Malformed YAML: unterminated mapping block
	badYAML := "datastore:\n  host: [unterminated\n"
	if err := os.WriteFile(configPath, []byte(badYAML), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	config := NewConfig()
	err := config.LoadFromFile(configPath)
	if err == nil {
		t.Error("Expected error when parsing malformed YAML")
	}
}

func TestConfigApplyFlags(t *testing.T) {
	// Save and restore the package-level flag pointers so this test is
	// isolated from other tests in the file.
	origHost, origHostAddr := *pgHost, *pgHostAddr
	origDB, origUser := *pgDatabase, *pgUsername
	origPwdFile, origPort := *pgPasswordFile, *pgPort
	origSSLMode, origSSLCert := *pgSSLMode, *pgSSLCert
	origSSLKey, origSSLRootCert := *pgSSLKey, *pgSSLRootCert
	t.Cleanup(func() {
		*pgHost = origHost
		*pgHostAddr = origHostAddr
		*pgDatabase = origDB
		*pgUsername = origUser
		*pgPasswordFile = origPwdFile
		*pgPort = origPort
		*pgSSLMode = origSSLMode
		*pgSSLCert = origSSLCert
		*pgSSLKey = origSSLKey
		*pgSSLRootCert = origSSLRootCert
	})

	// Set override values on every flag.
	*pgHost = "flag-host"
	*pgHostAddr = "10.0.0.5"
	*pgDatabase = "flag-db"
	*pgUsername = "flag-user"
	*pgPasswordFile = "/flag/password"
	*pgPort = 6543
	*pgSSLMode = "require"
	*pgSSLCert = "/flag/cert"
	*pgSSLKey = "/flag/key"
	*pgSSLRootCert = "/flag/root"

	config := NewConfig()
	config.ApplyFlags()

	if config.Datastore.Host != "flag-host" {
		t.Errorf("Host: got %q, want flag-host", config.Datastore.Host)
	}
	if config.Datastore.HostAddr != "10.0.0.5" {
		t.Errorf("HostAddr: got %q, want 10.0.0.5", config.Datastore.HostAddr)
	}
	if config.Datastore.Database != "flag-db" {
		t.Errorf("Database: got %q, want flag-db", config.Datastore.Database)
	}
	if config.Datastore.Username != "flag-user" {
		t.Errorf("Username: got %q, want flag-user", config.Datastore.Username)
	}
	if config.Datastore.PasswordFile != "/flag/password" {
		t.Errorf("PasswordFile: got %q, want /flag/password", config.Datastore.PasswordFile)
	}
	if config.Datastore.Port != 6543 {
		t.Errorf("Port: got %d, want 6543", config.Datastore.Port)
	}
	if config.Datastore.SSLMode != "require" {
		t.Errorf("SSLMode: got %q, want require", config.Datastore.SSLMode)
	}
	if config.Datastore.SSLCert != "/flag/cert" {
		t.Errorf("SSLCert: got %q, want /flag/cert", config.Datastore.SSLCert)
	}
	if config.Datastore.SSLKey != "/flag/key" {
		t.Errorf("SSLKey: got %q, want /flag/key", config.Datastore.SSLKey)
	}
	if config.Datastore.SSLRootCert != "/flag/root" {
		t.Errorf("SSLRootCert: got %q, want /flag/root", config.Datastore.SSLRootCert)
	}
}

func TestConfigApplyFlags_EmptyFlagsPreserveDefaults(t *testing.T) {
	// Save and restore flag pointers.
	origHost, origHostAddr := *pgHost, *pgHostAddr
	origDB, origUser := *pgDatabase, *pgUsername
	origPwdFile, origPort := *pgPasswordFile, *pgPort
	origSSLMode, origSSLCert := *pgSSLMode, *pgSSLCert
	origSSLKey, origSSLRootCert := *pgSSLKey, *pgSSLRootCert
	t.Cleanup(func() {
		*pgHost = origHost
		*pgHostAddr = origHostAddr
		*pgDatabase = origDB
		*pgUsername = origUser
		*pgPasswordFile = origPwdFile
		*pgPort = origPort
		*pgSSLMode = origSSLMode
		*pgSSLCert = origSSLCert
		*pgSSLKey = origSSLKey
		*pgSSLRootCert = origSSLRootCert
	})

	// Set all flags to their default values: ApplyFlags should not
	// overwrite any config field in this case.
	*pgHost = ""
	*pgHostAddr = ""
	*pgDatabase = ""
	*pgUsername = ""
	*pgPasswordFile = ""
	*pgPort = 5432
	*pgSSLMode = "prefer"
	*pgSSLCert = ""
	*pgSSLKey = ""
	*pgSSLRootCert = ""

	config := NewConfig()
	// Pre-set all fields so we can detect any unwanted overwrite.
	config.Datastore.Host = "preserved"
	config.Datastore.HostAddr = "preserved-addr"
	config.Datastore.Database = "preserved-db"
	config.Datastore.Username = "preserved-user"
	config.Datastore.PasswordFile = "/preserved/password"
	config.Datastore.Port = 1234
	config.Datastore.SSLMode = "preserved-mode"
	config.Datastore.SSLCert = "/preserved/cert"
	config.Datastore.SSLKey = "/preserved/key"
	config.Datastore.SSLRootCert = "/preserved/root"

	config.ApplyFlags()

	if config.Datastore.Host != "preserved" {
		t.Errorf("Host should not have changed, got %q", config.Datastore.Host)
	}
	if config.Datastore.HostAddr != "preserved-addr" {
		t.Errorf("HostAddr should not have changed, got %q", config.Datastore.HostAddr)
	}
	if config.Datastore.Database != "preserved-db" {
		t.Errorf("Database should not have changed, got %q", config.Datastore.Database)
	}
	if config.Datastore.Username != "preserved-user" {
		t.Errorf("Username should not have changed, got %q", config.Datastore.Username)
	}
	if config.Datastore.PasswordFile != "/preserved/password" {
		t.Errorf("PasswordFile should not have changed, got %q", config.Datastore.PasswordFile)
	}
	if config.Datastore.Port != 1234 {
		t.Errorf("Port should not have changed, got %d", config.Datastore.Port)
	}
	if config.Datastore.SSLMode != "preserved-mode" {
		t.Errorf("SSLMode should not have changed, got %q", config.Datastore.SSLMode)
	}
	if config.Datastore.SSLCert != "/preserved/cert" {
		t.Errorf("SSLCert should not have changed, got %q", config.Datastore.SSLCert)
	}
	if config.Datastore.SSLKey != "/preserved/key" {
		t.Errorf("SSLKey should not have changed, got %q", config.Datastore.SSLKey)
	}
	if config.Datastore.SSLRootCert != "/preserved/root" {
		t.Errorf("SSLRootCert should not have changed, got %q", config.Datastore.SSLRootCert)
	}
}

func TestConfigLoadPassword_AlreadySet(t *testing.T) {
	config := NewConfig()
	config.Datastore.Password = "already-set"
	config.Datastore.PasswordFile = "/should/not/be/read"

	if err := config.LoadPassword(); err != nil {
		t.Fatalf("LoadPassword() error = %v", err)
	}
	if config.Datastore.Password != "already-set" {
		t.Errorf("Password should not have changed, got %q", config.Datastore.Password)
	}
}

func TestConfigLoadPassword_FromFile(t *testing.T) {
	tmpDir := t.TempDir()
	pwFile := filepath.Join(tmpDir, "pw.txt")

	if err := os.WriteFile(pwFile, []byte("file-password\n"), 0600); err != nil {
		t.Fatalf("failed to write password file: %v", err)
	}

	config := NewConfig()
	config.Datastore.PasswordFile = pwFile

	if err := config.LoadPassword(); err != nil {
		t.Fatalf("LoadPassword() error = %v", err)
	}
	if config.Datastore.Password != "file-password" {
		t.Errorf("Password: got %q, want file-password", config.Datastore.Password)
	}
}

func TestConfigLoadPassword_FileMissing(t *testing.T) {
	config := NewConfig()
	config.Datastore.PasswordFile = "/nonexistent/password/file"

	err := config.LoadPassword()
	if err == nil {
		t.Error("expected error when password file is missing")
	}
}

func TestConfigLoadPassword_NoFileNoDirect(t *testing.T) {
	config := NewConfig()
	// Neither Password nor PasswordFile set: LoadPassword returns nil
	// and leaves the field empty for the driver to pick up .pgpass.
	if err := config.LoadPassword(); err != nil {
		t.Errorf("LoadPassword() unexpected error: %v", err)
	}
	if config.Datastore.Password != "" {
		t.Errorf("Password: got %q, want empty", config.Datastore.Password)
	}
}

func TestConfigValidate_AllBranches(t *testing.T) {
	// Exhaustively cover every Validate() branch, including those where
	// Port > 65535 and MonitoredMaxIdleSeconds < 0 which are absent from
	// the existing TestConfigValidate cases.
	baseOK := func() *Config {
		return &Config{
			Datastore: datastoreconfig.DatastoreConfig{
				Host:     "h",
				Database: "d",
				Username: "u",
				Port:     5432,
			},
			Pool: PoolConfig{
				DatastoreMaxConnections: 5,
				DatastoreMaxIdleSeconds: 30,
				DatastoreMaxWaitSeconds: 30,
				MaxConnectionsPerServer: 3,
				MonitoredMaxIdleSeconds: 30,
				MonitoredMaxWaitSeconds: 30,
			},
		}
	}

	tests := []struct {
		name   string
		mutate func(c *Config)
	}{
		{"port too high", func(c *Config) { c.Datastore.Port = 70000 }},
		{"missing username", func(c *Config) { c.Datastore.Username = "" }},
		{"negative monitored idle seconds", func(c *Config) { c.Pool.MonitoredMaxIdleSeconds = -5 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := baseOK()
			tc.mutate(c)
			if err := c.Validate(); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}

	// Sanity: the base is valid.
	if err := baseOK().Validate(); err != nil {
		t.Errorf("base config should be valid, got error: %v", err)
	}
}

func TestLoadSecret_ExplicitPath(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "explicit.secret")

	testSecret := "explicit-secret-value"
	if err := os.WriteFile(secretFile, []byte(testSecret), 0600); err != nil {
		t.Fatalf("Failed to write test secret file: %v", err)
	}

	config := NewConfig()
	config.SecretFile = secretFile

	if err := config.LoadSecret(""); err != nil {
		t.Fatalf("LoadSecret() error = %v", err)
	}

	if config.GetServerSecret() != testSecret {
		t.Errorf("Expected secret to be '%s', got '%s'", testSecret, config.GetServerSecret())
	}
}

func TestLoadSecret_DefaultPath(t *testing.T) {
	// Redirect os.UserConfigDir() into a temp directory so the
	// helper finds our test secret without relying on host state.
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error: %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	secretFile := filepath.Join(pgedgeDir, "ai-dba-collector.secret")
	testSecret := "default-path-secret"
	if err := os.WriteFile(secretFile, []byte(testSecret), 0600); err != nil {
		t.Fatalf("Failed to write test secret file: %v", err)
	}

	config := NewConfig()
	// Don't set SecretFile - let it search default paths.
	if err := config.LoadSecret(""); err != nil {
		t.Fatalf("LoadSecret() error = %v", err)
	}

	if config.GetServerSecret() != testSecret {
		t.Errorf("Expected secret to be '%s', got '%s'", testSecret, config.GetServerSecret())
	}
}

func TestLoadSecret_NotFound(t *testing.T) {
	// Redirect os.UserConfigDir() at an empty temp dir so neither
	// the user nor the system path resolves.
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	config := NewConfig()
	err := config.LoadSecret("")
	if err == nil {
		t.Error("Expected error when no secret file is found")
	}
}

// TestLoadSecret_DefaultPathReadError exercises the rare branch
// where the helper finds a file at the default location but the
// subsequent read fails. We simulate that by placing a directory
// (rather than a regular file) at the discovered path.
func TestLoadSecret_DefaultPathReadError(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir(): %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Create a subdirectory at the would-be secret path so os.Stat
	// in the helper succeeds but ReadFile fails.
	bogusPath := filepath.Join(pgedgeDir, "ai-dba-collector.secret")
	if err := os.Mkdir(bogusPath, 0700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	config := NewConfig()
	if err := config.LoadSecret(""); err == nil {
		t.Error("Expected error when secret path is unreadable")
	}
}

func TestGetServerSecret(t *testing.T) {
	config := NewConfig()

	// Initially should be empty
	if config.GetServerSecret() != "" {
		t.Errorf("Expected initial secret to be empty, got '%s'", config.GetServerSecret())
	}

	// Set via LoadSecret
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "test.secret")
	testSecret := "getter-test-secret"
	if err := os.WriteFile(secretFile, []byte(testSecret), 0600); err != nil {
		t.Fatalf("Failed to write test secret file: %v", err)
	}

	config.SecretFile = secretFile
	if err := config.LoadSecret(""); err != nil {
		t.Fatalf("LoadSecret() error = %v", err)
	}

	if config.GetServerSecret() != testSecret {
		t.Errorf("Expected secret to be '%s', got '%s'", testSecret, config.GetServerSecret())
	}
}

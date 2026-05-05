/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/collector/src/probes"
)

// testConfig satisfies the scheduler.Config interface for unit tests.
type testConfig struct {
	datastoreMaxWait int
	monitoredMaxWait int
}

func (c testConfig) GetDatastorePoolMaxWaitSeconds() int { return c.datastoreMaxWait }
func (c testConfig) GetMonitoredPoolMaxWaitSeconds() int { return c.monitoredMaxWait }

func TestIsClosedPoolError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"generic error", errors.New("something broke"), false},
		{"empty message", errors.New(""), false},
		{"exact match", errors.New("closed pool"), true},
		{"wrapped closed pool", fmt.Errorf("wrapping: %w", errors.New("acquire: closed pool")), true},
		{"substring in middle", errors.New("pool: closed pool detected"), true},
		{"case sensitive", errors.New("Closed Pool"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isClosedPoolError(tc.err)
			if got != tc.want {
				t.Errorf("isClosedPoolError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestNewProbeScheduler verifies the constructor wires up internal state
// correctly without touching any database code.
func TestNewProbeScheduler(t *testing.T) {
	cfg := testConfig{datastoreMaxWait: 30, monitoredMaxWait: 15}
	ps := NewProbeScheduler(nil, nil, cfg, "secret")

	if ps == nil {
		t.Fatal("NewProbeScheduler returned nil")
	}
	if ps.serverSecret != "secret" {
		t.Errorf("serverSecret: got %q, want 'secret'", ps.serverSecret)
	}
	if ps.config.GetDatastorePoolMaxWaitSeconds() != 30 {
		t.Errorf("datastore wait: got %d, want 30", ps.config.GetDatastorePoolMaxWaitSeconds())
	}
	if ps.config.GetMonitoredPoolMaxWaitSeconds() != 15 {
		t.Errorf("monitored wait: got %d, want 15", ps.config.GetMonitoredPoolMaxWaitSeconds())
	}
	if ps.probesByConn == nil {
		t.Error("probesByConn should be initialized")
	}
	if ps.shutdownChan == nil {
		t.Error("shutdownChan should be initialized")
	}
	if ps.ctx == nil {
		t.Error("ctx should be initialized")
	}
	if ps.cancel == nil {
		t.Error("cancel should be initialized")
	}
}

// TestCreateProbe_AllKnownNames iterates every probe-name constant listed
// in probes/constants.go and confirms createProbe returns a non-nil
// MetricsProbe whose name matches. This drives every case in the
// createProbe switch statement.
func TestCreateProbe_AllKnownNames(t *testing.T) {
	ps := NewProbeScheduler(nil, nil, testConfig{}, "")

	knownNames := []string{
		probes.ProbeNamePgStatActivity,
		probes.ProbeNamePgStatReplication,
		probes.ProbeNamePgReplicationSlots,
		probes.ProbeNamePgStatRecoveryPrefetch,
		probes.ProbeNamePgStatSubscription,
		probes.ProbeNamePgStatConnectionSecurity,
		probes.ProbeNamePgStatIO,
		probes.ProbeNamePgStatCheckpointer,
		probes.ProbeNamePgStatWAL,
		probes.ProbeNamePgSettings,
		probes.ProbeNamePgHbaFileRules,
		probes.ProbeNamePgIdentFileMappings,
		probes.ProbeNamePgServerInfo,
		probes.ProbeNamePgNodeRole,
		probes.ProbeNamePgConnectivity,
		probes.ProbeNamePgDatabase,
		probes.ProbeNamePgStatDatabase,
		probes.ProbeNamePgStatDatabaseConflicts,
		probes.ProbeNamePgStatAllTables,
		probes.ProbeNamePgStatAllIndexes,
		probes.ProbeNamePgStatioAllSequences,
		probes.ProbeNamePgStatUserFunctions,
		probes.ProbeNamePgStatStatements,
		probes.ProbeNamePgExtension,
		probes.ProbeNameSpockExceptionLog,
		probes.ProbeNameSpockResolutions,
		probes.ProbeNamePgSysOsInfo,
		probes.ProbeNamePgSysCPUInfo,
		probes.ProbeNamePgSysCPUUsageInfo,
		probes.ProbeNamePgSysMemoryInfo,
		probes.ProbeNamePgSysIoAnalysisInfo,
		probes.ProbeNamePgSysDiskInfo,
		probes.ProbeNamePgSysLoadAvgInfo,
		probes.ProbeNamePgSysProcessInfo,
		probes.ProbeNamePgSysNetworkInfo,
		probes.ProbeNamePgSysCPUMemoryByProcess,
	}

	for _, name := range knownNames {
		t.Run(name, func(t *testing.T) {
			cfg := &probes.ProbeConfig{
				Name:                      name,
				CollectionIntervalSeconds: 60,
				RetentionDays:             7,
			}
			probe := ps.createProbe(cfg)
			if probe == nil {
				t.Fatalf("createProbe(%q) returned nil", name)
			}
			if probe.GetName() != name {
				t.Errorf("probe name mismatch: got %q, want %q", probe.GetName(), name)
			}
		})
	}
}

func TestCreateProbe_UnknownName(t *testing.T) {
	ps := NewProbeScheduler(nil, nil, testConfig{}, "")
	cfg := &probes.ProbeConfig{Name: "not_a_real_probe"}
	if got := ps.createProbe(cfg); got != nil {
		t.Errorf("expected nil for unknown probe, got %v", got)
	}
}

// TestStop_NoStart verifies Stop() can be called on a freshly constructed
// scheduler (Start() was never called, so configReloader is nil and there
// are no goroutines to wait on).
func TestStop_NoStart(t *testing.T) {
	ps := NewProbeScheduler(nil, nil, testConfig{}, "")

	done := make(chan struct{})
	go func() {
		ps.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return promptly when Start() was not called")
	}
}

// TestStop_ClosesShutdownChan verifies that after Stop() the shutdownChan
// is closed and the context has been canceled.
func TestStop_ClosesShutdownChan(t *testing.T) {
	ps := NewProbeScheduler(nil, nil, testConfig{}, "")
	ps.Stop()

	select {
	case _, ok := <-ps.shutdownChan:
		if ok {
			t.Error("expected shutdownChan to be closed")
		}
	default:
		t.Error("shutdownChan was not closed")
	}

	if ps.ctx.Err() == nil {
		t.Error("expected ctx to be canceled after Stop()")
	}
}

// TestConfigReloadLoop_ExitsOnShutdown exercises the configReloadLoop
// goroutine's shutdown path without touching any database code: the loop
// exits on shutdownChan close.
func TestConfigReloadLoop_ExitsOnShutdown(t *testing.T) {
	ps := NewProbeScheduler(nil, nil, testConfig{}, "")
	ps.configReloader = time.NewTicker(time.Hour) // never fires in test window

	ps.wg.Add(1)
	go ps.configReloadLoop()

	close(ps.shutdownChan)

	done := make(chan struct{})
	go func() {
		ps.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("configReloadLoop did not exit after shutdownChan close")
	}

	// Reset shutdownChan before calling Stop() so Stop doesn't panic
	// closing an already-closed channel. Stop is also cleaning up the
	// ticker so we don't leak a goroutine.
	ps.shutdownChan = make(chan struct{})
	ps.Stop()
}

// TestConfigReloadLoop_ExitsOnContextCancel exercises the alternate exit
// path: ctx.Done() instead of shutdownChan.
func TestConfigReloadLoop_ExitsOnContextCancel(t *testing.T) {
	ps := NewProbeScheduler(nil, nil, testConfig{}, "")
	ps.configReloader = time.NewTicker(time.Hour)
	t.Cleanup(func() { ps.configReloader.Stop() })

	ps.wg.Add(1)
	go ps.configReloadLoop()

	ps.cancel()

	done := make(chan struct{})
	go func() {
		ps.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("configReloadLoop did not exit after ctx cancel")
	}
}

// TestProbeScheduler_ProbesMapConcurrency exercises concurrent access to
// the probesByConn map under the sync.RWMutex. It ensures our
// assumptions about the state container hold (no data race detected when
// tests run under `go test -race`).
func TestProbeScheduler_ProbesMapConcurrency(t *testing.T) {
	ps := NewProbeScheduler(nil, nil, testConfig{}, "")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ps.probesMutex.Lock()
			if _, ok := ps.probesByConn[id]; !ok {
				ps.probesByConn[id] = make(map[string]probes.MetricsProbe)
			}
			ps.probesMutex.Unlock()

			ps.probesMutex.RLock()
			_ = ps.probesByConn[id]
			ps.probesMutex.RUnlock()
		}(i)
	}
	wg.Wait()

	if len(ps.probesByConn) != 20 {
		t.Errorf("expected 20 entries, got %d", len(ps.probesByConn))
	}
}

// TestTestConfigInterface ensures the test's local testConfig really
// satisfies the scheduler.Config interface. This is a compile-time check
// promoted to runtime so it registers in coverage.
func TestTestConfigInterface(t *testing.T) {
	var _ Config = testConfig{}
	_ = context.Background // keep import
}

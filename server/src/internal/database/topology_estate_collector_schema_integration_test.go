/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests drive the topology, estate and relationship readers against
// the collector's real schema, with a fixture that holds every kind of
// cluster the auto-detection builds and a connection marked for deletion
// whose metric, alert and relationship rows were written after the mark
// (as a collector or alerter cycle already in flight would). Issue #668
// requires that no reader returns the marked connection.

// csRole is one metrics.pg_node_role row; nil fields are stored as NULL.
type csRole struct {
	primary   string
	streaming bool
	upHost    any
	upPort    any
	hasSpock  bool
	spockName any
	pubHost   any
	pubPort   any
	started   any
}

// csEnv bundles the scratch datastore and its pool for the helpers.
type csEnv struct {
	t    *testing.T
	ctx  context.Context
	ds   *Datastore
	pool *pgxpool.Pool
}

func newCSEnv(t *testing.T) *csEnv {
	t.Helper()
	ds, pool := newCollectorSchemaDatastore(t)
	return &csEnv{t: t, ctx: context.Background(), ds: ds, pool: pool}
}

func (e *csEnv) exec(sql string, args ...any) {
	e.t.Helper()
	execCollectorSchema(e.t, e.pool, sql, args...)
}

func (e *csEnv) queryInt(sql string, args ...any) int {
	e.t.Helper()
	var v int
	if err := e.pool.QueryRow(e.ctx, sql, args...).Scan(&v); err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return v
}

// conn inserts a monitored connection owned by 'test-owner'. connErr and
// role may be nil.
func (e *csEnv) conn(name, host, description string, connErr, role any) int {
	e.t.Helper()
	return e.queryInt(`
        INSERT INTO connections (name, host, port, database_name, username,
                                 owner_username, description, connection_error,
                                 role, is_monitored)
        VALUES ($1, $2, 5432, 'postgres', 'postgres', 'test-owner', $3, $4, $5, TRUE)
        RETURNING id`, name, host, description, connErr, role)
}

func (e *csEnv) connectivity(id int, ago string) {
	e.t.Helper()
	e.exec(`INSERT INTO metrics.pg_connectivity (connection_id, response_time_ms, collected_at)
            VALUES ($1, 1.5, now() - $2::text::interval)`, id, ago)
}

func (e *csEnv) role(id int, ago string, r csRole) {
	e.t.Helper()
	e.exec(`
        INSERT INTO metrics.pg_node_role (
            connection_id, is_in_recovery, is_streaming_standby, upstream_host,
            upstream_port, has_spock, spock_node_name, primary_role,
            publisher_host, publisher_port, postmaster_start_time, collected_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now() - $12::text::interval)`,
		id, r.primary == "binary_standby", r.streaming, r.upHost, r.upPort,
		r.hasSpock, r.spockName, r.primary, r.pubHost, r.pubPort, r.started, ago)
}

func (e *csEnv) serverInfo(id int, ago, version string, sysid any) {
	e.t.Helper()
	e.exec(`
        INSERT INTO metrics.pg_server_info (
            connection_id, server_version, server_version_num, system_identifier,
            data_directory, max_connections, installed_extensions, collected_at)
        VALUES ($1, $2, 170002, $3, '/var/lib/postgresql/data', 100,
                ARRAY['plpgsql', 'pg_stat_statements'], now() - $4::text::interval)`,
		id, version, sysid, ago)
}

func (e *csEnv) osInfo(id int, ago string) {
	e.t.Helper()
	e.exec(`INSERT INTO metrics.pg_sys_os_info (connection_id, name, version, architecture, host_name, collected_at)
            VALUES ($1, 'Linux', '6.1', 'x86_64', 'db.example.com', now() - $2::text::interval)`, id, ago)
}

func (e *csEnv) extension(id int, ago, name, version string) {
	e.t.Helper()
	e.exec(`INSERT INTO metrics.pg_extension (connection_id, database_name, extname, extversion, collected_at)
            VALUES ($1, 'postgres', $2, $3, now() - $4::text::interval)`, id, name, version, ago)
}

// live seeds the recent rows that make a connection online with a role.
func (e *csEnv) live(id int, r csRole, version string, sysid any) {
	e.t.Helper()
	e.connectivity(id, "1 minute")
	e.role(id, "1 minute", r)
	e.serverInfo(id, "1 minute", version, sysid)
	e.osInfo(id, "1 minute")
}

// systemInfo seeds the rows GetConnectionContext reads for the host.
func (e *csEnv) systemInfo(id int) {
	e.t.Helper()
	e.exec(`INSERT INTO metrics.pg_sys_cpu_info (connection_id, model_name, no_of_cores, logical_processor, collected_at)
            VALUES ($1, 'Test CPU', 4, 8, now() - interval '1 minute')`, id)
	e.exec(`INSERT INTO metrics.pg_sys_memory_info (connection_id, total_memory, free_memory, collected_at)
            VALUES ($1, 8589934592, 4294967296, now() - interval '1 minute')`, id)
	for _, mount := range []string{"/", "/var/lib/postgresql"} {
		e.exec(`INSERT INTO metrics.pg_sys_disk_info (connection_id, mount_point, file_system_type,
                    total_space, used_space, free_space, collected_at)
                VALUES ($1, $2, 'ext4', 1000, 400, 600, date_trunc('minute', now()) - interval '1 minute')`, id, mount)
	}
}

// alert inserts an alert and returns its ID; clearedAgo may be nil.
func (e *csEnv) alert(id any, severity, status, title, triggeredAgo string, clearedAgo any) int {
	e.t.Helper()
	return e.queryInt(`
        INSERT INTO alerts (alert_type, connection_id, severity, title, description,
                            status, triggered_at, cleared_at)
        VALUES ('threshold', $1, $2, $3, 'Test alert', $4,
                now() - $5::text::interval, now() - $6::text::interval)
        RETURNING id`, id, severity, title, status, triggeredAgo, clearedAgo)
}

// history seeds two snapshots of every timeline source an hour apart,
// each differing from the last, plus one alert of each lifecycle stage.
func (e *csEnv) history(id int, primaryRole string) {
	e.t.Helper()
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	second := first.Add(48 * time.Hour)
	for i, ago := range []string{"2 hours", "1 hour"} {
		e.exec(`INSERT INTO metrics.pg_settings (connection_id, name, setting, collected_at)
                VALUES ($1, 'work_mem', $2, now() - $3::text::interval)`,
			id, []string{"4MB", "8MB"}[i], ago)
		e.exec(`INSERT INTO metrics.pg_hba_file_rules (connection_id, rule_number, type, database,
                    user_name, address, auth_method, collected_at)
                VALUES ($1, 1, 'host', ARRAY['all'], ARRAY['all'], '192.0.2.0/24', $2,
                        now() - $3::text::interval)`,
			id, []string{"md5", "scram-sha-256"}[i], ago)
		e.exec(`INSERT INTO metrics.pg_ident_file_mappings (connection_id, map_number, map_name,
                    sys_name, pg_username, collected_at)
                VALUES ($1, 1, 'testmap', 'testuser', 'postgres', now() - $2::text::interval)`, id, ago)
		e.role(id, ago, csRole{primary: primaryRole, started: []time.Time{first, second}[i]})
		e.extension(id, ago, "pg_stat_statements", []string{"1.10", "1.11"}[i])
	}
	e.alert(id, "critical", "active", "High CPU", "30 minutes", nil)
	e.alert(id, "warning", "cleared", "Disk filling", "50 minutes", "40 minutes")
	acked := e.alert(id, "warning", "acknowledged", "Slow queries", "45 minutes", nil)
	e.exec(`INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, acknowledged_at,
                acknowledge_type, message)
            VALUES ($1, 'test-owner', now() - interval '20 minutes', 'acknowledge', 'Looking into it')`, acked)
}

// autoCluster inserts a clusters row keyed for auto-detection and returns
// its ID; groupID may be nil.
func (e *csEnv) autoCluster(name, key string, groupID any, description any, dismissed bool) int {
	e.t.Helper()
	return e.queryInt(`
        INSERT INTO clusters (name, auto_cluster_key, group_id, description, dismissed)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id`, name, key, groupID, description, dismissed)
}

func (e *csEnv) clusterIDForKey(key string) int {
	e.t.Helper()
	return e.queryInt(`SELECT id FROM clusters WHERE auto_cluster_key = $1`, key)
}

// markLate marks a connection the way DeleteConnection does, without the
// dependent deletes, leaving the rows that a late write would leave.
func (e *csEnv) markLate(id int) {
	e.t.Helper()
	e.exec(`UPDATE connections SET deleted_at = now(), cluster_id = NULL,
                is_monitored = FALSE, enabled = FALSE WHERE id = $1`, id)
}

// csFixture holds the IDs the assertions need.
type csFixture struct {
	defaultGroup, manualGroup                             int
	binPrimary, binStandby, binStandby2, binStandby3      int
	spkNode1, spkNode2, spkStandby                        int
	pub, sub, sub2                                        int
	standalone, hidden                                    int
	manA, manB, defX                                      int
	ghostA, ghostB, ghostC, ghostD, ghostE, ghostF, ghost int
	gone                                                  int
	binCluster, manualCluster, defCluster, ghostCluster   int
}

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

// buildCSFixture creates the estate described at the top of this file.
func buildCSFixture(e *csEnv) *csFixture {
	e.t.Helper()
	f := &csFixture{}
	f.defaultGroup = e.queryInt(`SELECT id FROM cluster_groups WHERE is_default`)

	// Binary replication: one standby linked by IP, one by system
	// identifier, one by name.
	f.binPrimary = e.conn("bin-primary", "192.0.2.10", "Primary server", nil, "primary")
	e.live(f.binPrimary, csRole{primary: "binary_primary"}, "17.2", int64(111))
	e.systemInfo(f.binPrimary)
	e.history(f.binPrimary, "binary_primary")
	f.binStandby = e.conn("bin-standby", "192.0.2.11", "", nil, "standby")
	e.live(f.binStandby, csRole{primary: "binary_standby", streaming: true, upHost: "192.0.2.10", upPort: 5432}, "17.2", int64(111))
	f.binStandby2 = e.conn("bin-standby2", "192.0.2.12", "", nil, "standby")
	e.live(f.binStandby2, csRole{primary: "binary_standby"}, "17.2", int64(111))
	f.binStandby3 = e.conn("bin-standby3", "192.0.2.13", "", nil, "standby")
	e.live(f.binStandby3, csRole{primary: "binary_standby", streaming: true, upHost: "bin-primary", upPort: 5432}, "17.2", nil)

	// Spock with a hot standby, so the cluster is spock_ha.
	f.spkNode1 = e.conn("spk-node1", "192.0.2.20", "", nil, nil)
	e.live(f.spkNode1, csRole{primary: "spock_node", hasSpock: true, spockName: "n1"}, "17.2", int64(201))
	e.extension(f.spkNode1, "1 minute", "spock", "4.0")
	e.alert(f.spkNode1, "warning", "active", "Replication lag", "10 minutes", nil)
	f.spkNode2 = e.conn("spk-node2", "192.0.2.21", "", nil, nil)
	e.live(f.spkNode2, csRole{primary: "spock_node", hasSpock: true, spockName: "n2"}, "17.2", int64(202))
	e.extension(f.spkNode2, "1 minute", "spock", "4.0")
	f.spkStandby = e.conn("spk-standby", "192.0.2.22", "", nil, nil)
	e.live(f.spkStandby, csRole{primary: "binary_standby", streaming: true, hasSpock: true, upHost: "192.0.2.20", upPort: 5432}, "17.2", int64(201))

	// Logical replication: one subscriber matched by IP, one by name.
	f.pub = e.conn("pub", "192.0.2.30", "Publisher", nil, nil)
	e.live(f.pub, csRole{primary: "logical_publisher", spockName: "pubnode"}, "16.4", nil)
	e.extension(f.pub, "1 minute", "spock", "4.0")
	e.alert(f.pub, "info", "active", "Informational", "5 minutes", nil)
	f.sub = e.conn("sub", "192.0.2.31", "Subscriber", nil, nil)
	e.live(f.sub, csRole{primary: "logical_subscriber", spockName: "subnode", pubHost: "192.0.2.30", pubPort: 5432}, "16.4", nil)
	e.extension(f.sub, "1 minute", "spock", "4.0")
	f.sub2 = e.conn("sub2", "192.0.2.32", "", nil, nil)
	e.live(f.sub2, csRole{primary: "logical_subscriber", pubHost: "pub", pubPort: 5432}, "16.4", nil)

	// A standalone server with a description override, and one dismissed.
	f.standalone = e.conn("standalone", "192.0.2.40", "", "connection refused", nil)
	e.autoCluster("standalone", "standalone:"+itoa(f.standalone), f.defaultGroup, "Lonely", false)
	f.hidden = e.conn("hidden", "192.0.2.41", "", nil, nil)
	e.autoCluster("hidden", "standalone:"+itoa(f.hidden), f.defaultGroup, nil, true)

	// Dismissed binary, spock and logical clusters.
	disPrimary := e.conn("dis-primary", "192.0.2.80", "", nil, nil)
	e.live(disPrimary, csRole{primary: "binary_primary"}, "17.2", int64(222))
	disStandby := e.conn("dis-standby", "192.0.2.81", "", nil, nil)
	e.live(disStandby, csRole{primary: "binary_standby", streaming: true, upHost: "192.0.2.80", upPort: 5432}, "17.2", int64(222))
	e.autoCluster("dis", "binary:"+itoa(disPrimary), f.defaultGroup, nil, true)
	dspNode := e.conn("dsp-node1", "192.0.2.82", "", nil, nil)
	e.live(dspNode, csRole{primary: "spock_node", hasSpock: true, spockName: "d1"}, "17.2", nil)
	e.autoCluster("dsp", "spock:dsp", f.defaultGroup, nil, true)
	dpub := e.conn("dpub", "192.0.2.83", "", nil, nil)
	e.live(dpub, csRole{primary: "logical_publisher"}, "16.4", nil)
	dsub := e.conn("dsub", "192.0.2.84", "", nil, nil)
	e.live(dsub, csRole{primary: "logical_subscriber", pubHost: "192.0.2.83", pubPort: 5432}, "16.4", nil)
	e.autoCluster("dpub", "logical:"+itoa(dpub), f.defaultGroup, nil, true)

	// A manual group holding a manual cluster whose second member is a
	// manually pinned standby of bin-primary.
	group, err := e.ds.CreateClusterGroup(e.ctx, "Manual estate", sp("Hand built"))
	if err != nil {
		e.t.Fatalf("CreateClusterGroup: %v", err)
	}
	f.manualGroup = group.ID
	f.manualCluster, err = e.ds.CreateManualCluster(e.ctx, "Manual cluster", "Hand built", "custom", &f.manualGroup)
	if err != nil {
		e.t.Fatalf("CreateManualCluster: %v", err)
	}
	f.manA = e.conn("man-a", "192.0.2.50", "", nil, nil)
	e.live(f.manA, csRole{primary: "standalone"}, "17.2", nil)
	f.manB = e.conn("man-b", "192.0.2.51", "", nil, nil)
	e.live(f.manB, csRole{primary: "binary_standby", streaming: true, upHost: "192.0.2.10", upPort: 5432}, "17.2", int64(111))
	if err := e.ds.AddServerToCluster(e.ctx, f.manualCluster, f.manA, sp("primary")); err != nil {
		e.t.Fatalf("AddServerToCluster man-a: %v", err)
	}
	if err := e.ds.AddServerToCluster(e.ctx, f.manualCluster, f.manB, nil); err != nil {
		e.t.Fatalf("AddServerToCluster man-b: %v", err)
	}
	if err := e.ds.SetNodeRelationships(e.ctx, f.manualCluster, f.manB,
		[]RelationshipInput{{TargetConnectionID: f.manA, RelationshipType: "streams_from"}}); err != nil {
		e.t.Fatalf("SetNodeRelationships man-b: %v", err)
	}

	// A manual cluster in the default group.
	f.defCluster, err = e.ds.CreateManualCluster(e.ctx, "Default manual", "", "", nil)
	if err != nil {
		e.t.Fatalf("CreateManualCluster default: %v", err)
	}
	f.defX = e.conn("def-x", "192.0.2.60", "", nil, nil)
	if err := e.ds.AddServerToCluster(e.ctx, f.defCluster, f.defX, sp("primary")); err != nil {
		e.t.Fatalf("AddServerToCluster def-x: %v", err)
	}

	// The connection that is marked for deletion; first it is a live
	// standby of bin-primary with history of every timeline kind.
	f.gone = e.conn("gone", "192.0.2.90", "", nil, "standby")
	e.live(f.gone, csRole{primary: "binary_standby", streaming: true, upHost: "192.0.2.10", upPort: 5432}, "17.2", int64(111))
	e.history(f.gone, "binary_standby")

	e.exec(`INSERT INTO blackouts (scope, reason, start_time, end_time, created_by)
            VALUES ('estate', 'Patch window', now() - interval '1 hour', now() + interval '1 hour', 'test-owner'),
                   ('estate', 'Upgrade', now() + interval '2 hours', now() + interval '3 hours', 'test-owner')`)

	if err := e.ds.RefreshClusterAssignments(e.ctx); err != nil {
		e.t.Fatalf("RefreshClusterAssignments: %v", err)
	}
	f.binCluster = e.clusterIDForKey("binary:" + itoa(f.binPrimary))
	e.exec(`UPDATE clusters SET group_id = $1, description = 'Logical estate', replication_type = 'logical'
            WHERE auto_cluster_key = $2`, f.manualGroup, "logical:"+itoa(f.pub))

	// Members persisted in the database but not detected this cycle:
	// two in a cluster auto-detection no longer finds (one a standby of
	// the other, which streams from bin-primary outside the set), two
	// in the live binary cluster (one a standby of bin-standby), one in
	// a claimed cluster, one in a cluster with no group and one under a
	// key that has no type prefix.
	f.ghostCluster = e.autoCluster("ghost", "binary:99999", f.defaultGroup, "Vanished", false)
	claimedGhost := e.autoCluster("claimed ghost", "binary:88888", f.manualGroup, nil, false)
	noGroup := e.autoCluster("no group", "binary:77777", nil, nil, false)
	orphan := e.autoCluster("orphan", "orphan", f.defaultGroup, nil, false)
	f.ghostA = e.conn("ghost-a", "192.0.2.70", "Ghost", "timeout", nil)
	e.live(f.ghostA, csRole{primary: "standalone", spockName: "ghost_a"}, "15.8", nil)
	f.ghostB = e.conn("ghost-b", "192.0.2.71", "", nil, "standby")
	f.ghostC = e.conn("ghost-c", "192.0.2.72", "", nil, "standby")
	f.ghostD = e.conn("ghost-d", "192.0.2.73", "", nil, "standby")
	f.ghostE = e.conn("ghost-e", "192.0.2.74", "", nil, nil)
	f.ghostF = e.conn("ghost-f", "192.0.2.75", "", nil, nil)
	f.ghost = e.conn("ghost-g", "192.0.2.76", "", nil, nil)
	for id, cluster := range map[int]int{
		f.ghostA: f.ghostCluster, f.ghostB: f.ghostCluster,
		f.ghostC: f.binCluster, f.ghostD: f.binCluster,
		f.ghostE: claimedGhost, f.ghostF: noGroup, f.ghost: orphan,
	} {
		e.exec(`UPDATE connections SET cluster_id = $1 WHERE id = $2`, cluster, id)
	}
	for _, rel := range []struct{ cluster, source, target int }{
		{f.ghostCluster, f.ghostB, f.ghostA},
		{f.ghostCluster, f.ghostA, f.binPrimary},
		{f.binCluster, f.ghostC, f.binStandby},
	} {
		if err := e.ds.SetNodeRelationships(e.ctx, rel.cluster, rel.source,
			[]RelationshipInput{{TargetConnectionID: rel.target, RelationshipType: "streams_from"}}); err != nil {
			e.t.Fatalf("SetNodeRelationships %d: %v", rel.source, err)
		}
	}

	// Mark gone, then write the rows a late cycle would: fresh metrics,
	// an alert, and manual relationships in both directions.
	e.markLate(f.gone)
	e.live(f.gone, csRole{primary: "binary_standby", streaming: true, upHost: "192.0.2.10", upPort: 5432}, "17.2", int64(111))
	e.alert(f.gone, "critical", "active", "Late alert", "1 minute", nil)
	if err := e.ds.SetNodeRelationships(e.ctx, f.binCluster, f.gone,
		[]RelationshipInput{{TargetConnectionID: f.binPrimary, RelationshipType: "streams_from"}}); err != nil {
		e.t.Fatalf("SetNodeRelationships gone: %v", err)
	}
	if err := e.ds.SetNodeRelationships(e.ctx, f.binCluster, f.binStandby2,
		[]RelationshipInput{{TargetConnectionID: f.gone, RelationshipType: "replicates_with"}}); err != nil {
		e.t.Fatalf("SetNodeRelationships to gone: %v", err)
	}

	if err := e.ds.RefreshClusterAssignments(e.ctx); err != nil {
		e.t.Fatalf("second RefreshClusterAssignments: %v", err)
	}
	if n := e.queryInt(`SELECT count(*) FROM connections WHERE id = $1 AND cluster_id IS NOT NULL`, f.gone); n != 0 {
		e.t.Fatalf("refresh reassigned the marked connection to a cluster")
	}
	return f
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// walkTopology calls fn for every server at any depth.
func walkTopology(groups []TopologyGroup, fn func(TopologyCluster, TopologyServerInfo)) {
	var walk func(TopologyCluster, []TopologyServerInfo)
	walk = func(c TopologyCluster, servers []TopologyServerInfo) {
		for _, s := range servers {
			fn(c, s)
			walk(c, s.Children)
		}
	}
	for _, g := range groups {
		for _, c := range g.Clusters {
			walk(c, c.Servers)
		}
	}
}

func findCluster(groups []TopologyGroup, pred func(TopologyCluster) bool) (TopologyGroup, TopologyCluster, bool) {
	for _, g := range groups {
		for _, c := range g.Clusters {
			if pred(c) {
				return g, c, true
			}
		}
	}
	return TopologyGroup{}, TopologyCluster{}, false
}

func childIDs(s TopologyServerInfo) []int {
	ids := make([]int, 0, len(s.Children))
	for _, c := range s.Children {
		ids = append(ids, c.ID)
	}
	sort.Ints(ids)
	return ids
}

func containsInt(ids []int, id int) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func TestCollectorSchema_TopologyEstateAndRelationships(t *testing.T) {
	e := newCSEnv(t)
	f := buildCSFixture(e)
	ctx := e.ctx
	ds := e.ds

	t.Run("topology", func(t *testing.T) {
		groups, err := ds.GetClusterTopology(ctx, nil)
		if err != nil {
			t.Fatalf("GetClusterTopology: %v", err)
		}
		walkTopology(groups, func(c TopologyCluster, s TopologyServerInfo) {
			if s.ID == f.gone || s.ID == f.hidden {
				t.Errorf("topology cluster %q holds server %d", c.Name, s.ID)
			}
			for _, r := range s.Relationships {
				if r.TargetServerID == f.gone {
					t.Errorf("server %d has a relationship to the marked connection", s.ID)
				}
			}
		})

		g, bin, ok := findCluster(groups, func(c TopologyCluster) bool {
			return c.AutoClusterKey == "binary:"+itoa(f.binPrimary)
		})
		if !ok || !g.IsDefault || bin.ClusterType != "binary" {
			t.Fatalf("binary cluster missing or misplaced: %+v", bin)
		}
		var primary TopologyServerInfo
		var topIDs []int
		for _, s := range bin.Servers {
			topIDs = append(topIDs, s.ID)
			if s.ID == f.binPrimary {
				primary = s
			}
		}
		if primary.Description != "Primary server" || primary.Version != "17.2" || primary.OS != "Linux" {
			t.Errorf("primary fields = %+v", primary)
		}
		wantChildren := []int{f.binStandby, f.binStandby2, f.binStandby3}
		if got := childIDs(primary); !equalInts(got, wantChildren) {
			t.Errorf("primary children = %v, want %v", got, wantChildren)
		}
		if !containsInt(topIDs, f.ghostD) {
			t.Errorf("persisted member ghost-d not merged at the top level: %v", topIDs)
		}
		for _, c := range primary.Children {
			if c.ID == f.binStandby && !containsInt(childIDs(c), f.ghostC) {
				t.Errorf("ghost-c not nested under bin-standby: %v", childIDs(c))
			}
		}

		if _, spk, ok := findCluster(groups, func(c TopologyCluster) bool { return c.AutoClusterKey == "spock:spk" }); !ok || spk.ClusterType != "spock_ha" || len(spk.Servers) != 2 {
			t.Errorf("spock cluster = %+v", spk)
		}

		g, logical, ok := findCluster(groups, func(c TopologyCluster) bool {
			return c.AutoClusterKey == "logical:"+itoa(f.pub)
		})
		if !ok || g.IsDefault || logical.Description != "Logical estate" || logical.ReplicationType != "logical" {
			t.Fatalf("claimed logical cluster = %+v in %q", logical, g.Name)
		}
		if got := childIDs(logical.Servers[0]); !equalInts(got, sortedInts(f.sub, f.sub2)) {
			t.Errorf("publisher children = %v", got)
		}
		if logical.Servers[0].SpockVersion != "4.0" || logical.Servers[0].Description != "Publisher" {
			t.Errorf("publisher fields = %+v", logical.Servers[0])
		}

		_, manual, ok := findCluster(groups, func(c TopologyCluster) bool { return c.Name == "Manual cluster" })
		if !ok || manual.ClusterType != "manual" || len(manual.Servers) != 1 ||
			manual.Servers[0].ID != f.manA || !equalInts(childIDs(manual.Servers[0]), []int{f.manB}) {
			t.Errorf("manual cluster = %+v", manual)
		}
		g, def, ok := findCluster(groups, func(c TopologyCluster) bool { return c.Name == "Default manual" })
		if !ok || !g.IsDefault || len(def.Servers) != 1 || def.Servers[0].ID != f.defX {
			t.Errorf("default-group manual cluster = %+v", def)
		}

		_, shell, ok := findCluster(groups, func(c TopologyCluster) bool { return c.AutoClusterKey == "binary:99999" })
		if !ok || shell.Description != "Vanished" || len(shell.Servers) != 1 ||
			shell.Servers[0].ID != f.ghostA || !equalInts(childIDs(shell.Servers[0]), []int{f.ghostB}) {
			t.Errorf("shell cluster = %+v", shell)
		}
		if shell.Servers[0].ConnectionError != "timeout" || shell.Servers[0].SpockNodeName != "ghost_a" {
			t.Errorf("persisted member fields = %+v", shell.Servers[0])
		}
		g, claimed, ok := findCluster(groups, func(c TopologyCluster) bool { return c.AutoClusterKey == "binary:88888" })
		if !ok || g.IsDefault || len(claimed.Servers) != 1 || claimed.Servers[0].ID != f.ghostE {
			t.Errorf("claimed shell cluster = %+v", claimed)
		}
		if _, orphan, ok := findCluster(groups, func(c TopologyCluster) bool { return c.AutoClusterKey == "orphan" }); !ok || orphan.ClusterType != "server" {
			t.Errorf("orphan shell cluster = %+v", orphan)
		}
		// A cluster with no group has nowhere to go; it must not stop
		// the shell clusters listed after it from appearing.
		if _, noGroup, ok := findCluster(groups, func(c TopologyCluster) bool { return c.AutoClusterKey == "binary:77777" }); ok {
			t.Errorf("group-less shell cluster placed in topology: %+v", noGroup)
		}
		if _, lone, ok := findCluster(groups, func(c TopologyCluster) bool {
			return c.AutoClusterKey == "standalone:"+itoa(f.standalone)
		}); !ok || lone.Description != "Lonely" || lone.Servers[0].ConnectionError != "connection refused" {
			t.Errorf("standalone cluster = %+v", lone)
		}

		visible, err := ds.GetClusterTopology(ctx, []int{f.binPrimary, f.gone})
		if err != nil {
			t.Fatalf("GetClusterTopology visible: %v", err)
		}
		walkTopology(visible, func(_ TopologyCluster, s TopologyServerInfo) {
			if s.ID != f.binPrimary {
				t.Errorf("visibility filter let server %d through", s.ID)
			}
		})
		if none, err := ds.GetClusterTopology(ctx, []int{}); err != nil || len(none) != 0 {
			t.Errorf("empty allow-list = %v, %v", none, err)
		}
	})

	t.Run("estate snapshot", func(t *testing.T) {
		snap, err := ds.GetEstateSnapshot(ctx)
		if err != nil {
			t.Fatalf("GetEstateSnapshot: %v", err)
		}
		for _, s := range snap.Servers {
			if s.ID == f.gone {
				t.Errorf("estate snapshot lists the marked connection")
			}
		}
		if snap.ServerOffline == 0 || snap.ServerWarning == 0 || snap.ServerOnline == 0 {
			t.Errorf("server status counts = %d/%d/%d", snap.ServerOnline, snap.ServerWarning, snap.ServerOffline)
		}
		if snap.AlertTotal != 3 || snap.AlertCritical != 1 || snap.AlertWarning != 1 || snap.AlertInfo != 1 {
			t.Errorf("alert counts = %d (%d/%d/%d), want 3 (1/1/1)",
				snap.AlertTotal, snap.AlertCritical, snap.AlertWarning, snap.AlertInfo)
		}
		if len(snap.ActiveBlackouts) != 1 || len(snap.UpcomingBlackouts) != 1 {
			t.Errorf("blackouts active=%d upcoming=%d", len(snap.ActiveBlackouts), len(snap.UpcomingBlackouts))
		}
		if len(snap.RecentEvents) == 0 {
			t.Errorf("no recent events")
		}
		for _, ev := range snap.RecentEvents {
			if ev.ServerName == "gone" {
				t.Errorf("recent event from the marked connection: %+v", ev)
			}
		}
	})

	t.Run("scoped snapshots", func(t *testing.T) {
		snap, name, err := ds.GetServerSnapshot(ctx, f.binPrimary)
		if err != nil || name != "bin-primary" || snap.ServerTotal != 1 || snap.AlertCritical != 1 {
			t.Errorf("GetServerSnapshot = %+v, %q, %v", snap, name, err)
		}
		if _, _, err := ds.GetServerSnapshot(ctx, f.gone); err == nil {
			t.Errorf("GetServerSnapshot returned the marked connection")
		}

		snap, name, err = ds.GetClusterSnapshot(ctx, f.binCluster)
		if err != nil || snap.ServerTotal == 0 {
			t.Fatalf("GetClusterSnapshot = %+v, %q, %v", snap, name, err)
		}
		for _, s := range snap.Servers {
			if s.ID == f.gone {
				t.Errorf("cluster snapshot lists the marked connection")
			}
		}
		if _, _, err := ds.GetClusterSnapshot(ctx, 999999); err == nil {
			t.Errorf("GetClusterSnapshot of a missing cluster succeeded")
		}

		snap, name, err = ds.GetGroupSnapshot(ctx, f.manualGroup)
		if err != nil || name != "Manual estate" || snap.ServerTotal == 0 || snap.AlertInfo != 1 {
			t.Errorf("GetGroupSnapshot = %+v, %q, %v", snap, name, err)
		}
		if _, _, err := ds.GetGroupSnapshot(ctx, 999999); err == nil {
			t.Errorf("GetGroupSnapshot of a missing group succeeded")
		}

		if empty := ds.GetConnectionsSnapshot(ctx, nil); empty.ServerTotal != 0 || len(empty.Servers) != 0 {
			t.Errorf("empty connections snapshot = %+v", empty)
		}
		snap = ds.GetConnectionsSnapshot(ctx, []int{f.binPrimary, f.standalone, f.spkNode1, f.gone})
		if snap.ServerTotal != 3 || snap.ServerOffline != 1 || snap.ServerWarning != 2 {
			t.Errorf("connections snapshot = %+v", snap)
		}
		if snap.AlertTotal != 2 {
			t.Errorf("connections snapshot alert total = %d, want 2", snap.AlertTotal)
		}
		for _, ev := range snap.RecentEvents {
			if ev.ServerName == "gone" {
				t.Errorf("scoped recent event from the marked connection")
			}
		}
	})

	t.Run("connection context", func(t *testing.T) {
		cc, err := ds.GetConnectionContext(ctx, f.binPrimary)
		if err != nil {
			t.Fatalf("GetConnectionContext: %v", err)
		}
		if cc.ServerName != "bin-primary" || cc.PostgreSQL == nil || cc.PostgreSQL.Version != "17.2" ||
			cc.PostgreSQL.VersionNum != 170002 || cc.PostgreSQL.MaxConnections != 100 ||
			cc.PostgreSQL.DataDirectory == "" || len(cc.PostgreSQL.InstalledExtensions) != 2 ||
			cc.PostgreSQL.Settings["work_mem"] != "8MB" {
			t.Errorf("PostgreSQL context = %+v", cc.PostgreSQL)
		}
		if cc.System == nil || cc.System.OSName != "Linux" || cc.System.Hostname != "db.example.com" ||
			cc.System.CPU == nil || cc.System.CPU.Cores != 4 || cc.System.Memory == nil ||
			len(cc.System.Disks) != 2 {
			t.Errorf("system context = %+v", cc.System)
		}
		bare, err := ds.GetConnectionContext(ctx, f.defX)
		if err != nil || bare.PostgreSQL != nil || bare.System != nil {
			t.Errorf("context without metrics = %+v, %v", bare, err)
		}
		if _, err := ds.GetConnectionContext(ctx, f.gone); err == nil {
			t.Errorf("GetConnectionContext returned the marked connection")
		}
	})

	t.Run("connection IDs", func(t *testing.T) {
		ids, err := ds.GetConnectionIDsForCluster(ctx, f.binCluster)
		want := sortedInts(f.binPrimary, f.binStandby, f.binStandby2, f.binStandby3, f.ghostC, f.ghostD)
		if err != nil || !equalInts(ids, want) {
			t.Errorf("GetConnectionIDsForCluster = %v, %v; want %v", ids, err, want)
		}
		ids, err = ds.GetConnectionIDsForGroup(ctx, f.manualGroup)
		want = sortedInts(f.manA, f.manB, f.pub, f.sub, f.sub2, f.ghostE)
		if err != nil || !equalInts(ids, want) {
			t.Errorf("GetConnectionIDsForGroup = %v, %v; want %v", ids, err, want)
		}
	})

	t.Run("alerts", func(t *testing.T) {
		result, err := ds.GetAlerts(ctx, AlertListFilter{Limit: 100})
		if err != nil {
			t.Fatalf("GetAlerts: %v", err)
		}
		for _, a := range result.Alerts {
			if a.ConnectionID != nil && *a.ConnectionID == f.gone {
				t.Errorf("GetAlerts returned an alert for the marked connection")
			}
		}
		if int(result.Total) != len(result.Alerts) {
			t.Errorf("GetAlerts total %d != %d rows", result.Total, len(result.Alerts))
		}
		counts, err := ds.GetAlertCounts(ctx, nil, true)
		if err != nil {
			t.Fatalf("GetAlertCounts: %v", err)
		}
		if _, ok := counts.ByServer[f.gone]; ok {
			t.Errorf("GetAlertCounts counted the marked connection")
		}
		if counts.ByServer[f.binPrimary] == 0 {
			t.Errorf("GetAlertCounts missed bin-primary: %v", counts.ByServer)
		}
	})

	t.Run("timeline", func(t *testing.T) {
		types := []string{
			EventTypeConfigChange, EventTypeHBAChange, EventTypeIdentChange,
			EventTypeRestart, EventTypeAlertFired, EventTypeAlertCleared,
			EventTypeAlertAcknowledged, EventTypeExtensionChange,
		}
		window := TimelineFilter{StartTime: time.Now().Add(-24 * time.Hour), EndTime: time.Now(), EventTypes: types, Limit: 1000}

		own := window
		own.ConnectionID = &f.binPrimary
		result, err := ds.GetTimelineEvents(ctx, own)
		if err != nil {
			t.Fatalf("GetTimelineEvents: %v", err)
		}
		seen := map[string]bool{}
		for _, ev := range result.Events {
			seen[ev.EventType] = true
		}
		for _, typ := range types {
			if !seen[typ] {
				t.Errorf("bin-primary timeline has no %s event", typ)
			}
		}
		if result.TotalCount != len(result.Events) {
			t.Errorf("bin-primary TotalCount = %d, events = %d", result.TotalCount, len(result.Events))
		}

		gone := window
		gone.ConnectionID = &f.gone
		result, err = ds.GetTimelineEvents(ctx, gone)
		if err != nil || len(result.Events) != 0 || result.TotalCount != 0 {
			t.Errorf("marked connection timeline = %d events, total %d, %v", len(result.Events), result.TotalCount, err)
		}

		result, err = ds.GetTimelineEvents(ctx, window)
		if err != nil {
			t.Fatalf("GetTimelineEvents estate: %v", err)
		}
		if result.TotalCount != len(result.Events) {
			t.Errorf("estate TotalCount = %d, events = %d", result.TotalCount, len(result.Events))
		}
		for _, ev := range result.Events {
			if ev.ConnectionID == f.gone {
				t.Errorf("estate timeline has a %s event from the marked connection", ev.EventType)
			}
		}
	})

	t.Run("hierarchy and overrides", func(t *testing.T) {
		hierarchy, err := ds.GetClusterHierarchy(ctx)
		if err != nil {
			t.Fatalf("GetClusterHierarchy: %v", err)
		}
		for _, g := range hierarchy {
			for _, c := range g.Clusters {
				for _, s := range c.Servers {
					if s.ID == f.gone {
						t.Errorf("GetClusterHierarchy lists the marked connection in %q", c.Name)
					}
				}
			}
		}
		if err := ds.AssignConnectionToCluster(ctx, f.gone, &f.binCluster, sp("standby"), "manual"); !errors.Is(err, ErrConnectionNotFound) {
			t.Errorf("AssignConnectionToCluster(marked) = %v, want ErrConnectionNotFound", err)
		}
		if _, err := ds.GetOverrideContext(ctx, f.gone, 1); err == nil {
			t.Errorf("GetOverrideContext returned the marked connection")
		}
	})

	t.Run("relationships", func(t *testing.T) {
		info, err := ds.GetConnectionClusterInfo(ctx, f.binStandby)
		if err != nil || info.ClusterID == nil || *info.ClusterID != f.binCluster ||
			info.AutoClusterKey == nil || *info.AutoClusterKey != "binary:"+itoa(f.binPrimary) {
			t.Errorf("GetConnectionClusterInfo = %+v, %v", info, err)
		}
		for _, id := range []int{f.gone, 999999} {
			if _, err := ds.GetConnectionClusterInfo(ctx, id); !errors.Is(err, ErrConnectionNotFound) {
				t.Errorf("GetConnectionClusterInfo(%d) = %v, want ErrConnectionNotFound", id, err)
			}
		}

		list, err := ds.ListClustersForAutocomplete(ctx)
		if err != nil {
			t.Fatalf("ListClustersForAutocomplete: %v", err)
		}
		for _, c := range list {
			if c.Name == "hidden" || c.Name == "dis" {
				t.Errorf("autocomplete lists dismissed cluster %q", c.Name)
			}
		}

		rels, err := ds.GetClusterRelationships(ctx, f.binCluster)
		if err != nil || len(rels) == 0 {
			t.Fatalf("GetClusterRelationships = %v, %v", rels, err)
		}
		for _, r := range rels {
			if r.SourceConnectionID == f.gone || r.TargetConnectionID == f.gone {
				t.Errorf("GetClusterRelationships returned %+v", r)
			}
		}

		if err := ds.ResetMembershipSource(ctx, f.gone); !errors.Is(err, ErrConnectionNotFound) {
			t.Errorf("ResetMembershipSource(marked) = %v", err)
		}
		if err := ds.ResetMembershipSource(ctx, f.defX); err != nil {
			t.Errorf("ResetMembershipSource: %v", err)
		}

		if err := ds.SetNodeRelationships(ctx, f.defCluster, f.defX,
			[]RelationshipInput{{TargetConnectionID: f.defX, RelationshipType: "streams_from"}}); err == nil {
			t.Errorf("SetNodeRelationships accepted a self-relationship")
		}
		if err := ds.SyncAutoDetectedRelationships(ctx, 999999, nil); !errors.Is(err, ErrClusterNotFound) {
			t.Errorf("SyncAutoDetectedRelationships(missing) = %v", err)
		}
		if err := ds.SyncAutoDetectedRelationships(ctx, f.defCluster,
			[]AutoRelationshipInput{{SourceConnectionID: f.defX, TargetConnectionID: f.defX, RelationshipType: "streams_from"}}); err == nil {
			t.Errorf("SyncAutoDetectedRelationships accepted a self-relationship")
		}

		if err := ds.ClearNodeRelationships(ctx, f.manualCluster, f.manB); err != nil {
			t.Errorf("ClearNodeRelationships: %v", err)
		}
		if err := ds.RemoveNodeRelationship(ctx, rels[0].ID); err != nil {
			t.Errorf("RemoveNodeRelationship: %v", err)
		}
		if err := ds.RemoveNodeRelationship(ctx, rels[0].ID); err == nil {
			t.Errorf("RemoveNodeRelationship of a missing row succeeded")
		}

		if err := ds.AddServerToCluster(ctx, 999999, f.defX, nil); !errors.Is(err, ErrClusterNotFound) {
			t.Errorf("AddServerToCluster(missing cluster) = %v", err)
		}
		if err := ds.AddServerToCluster(ctx, f.defCluster, f.gone, nil); !errors.Is(err, ErrConnectionNotFound) {
			t.Errorf("AddServerToCluster(marked) = %v", err)
		}
		if in, err := ds.IsConnectionInCluster(ctx, f.defCluster, f.defX); err != nil || !in {
			t.Errorf("IsConnectionInCluster = %v, %v", in, err)
		}
		if err := ds.RemoveServerFromCluster(ctx, f.defCluster, f.gone); !errors.Is(err, ErrConnectionNotFound) {
			t.Errorf("RemoveServerFromCluster(marked) = %v", err)
		}
		if err := ds.RemoveServerFromCluster(ctx, f.defCluster, f.defX); err != nil {
			t.Errorf("RemoveServerFromCluster: %v", err)
		}
		if in, err := ds.IsConnectionInCluster(ctx, f.defCluster, f.defX); err != nil || in {
			t.Errorf("IsConnectionInCluster after removal = %v, %v", in, err)
		}

		if _, err := ds.CreateManualCluster(ctx, "Bad group", "", "", ip(999999)); err == nil {
			t.Errorf("CreateManualCluster with a missing group succeeded")
		}
	})

	// Last, because it leaves man-b marked but still in its cluster: the
	// ID lookups must not trust the cluster_id invariant alone.
	t.Run("second safeguard", func(t *testing.T) {
		e.exec(`UPDATE connections SET deleted_at = now() WHERE id = $1`, f.manB)
		ids, err := ds.GetConnectionIDsForCluster(ctx, f.manualCluster)
		if err != nil || !equalInts(ids, []int{f.manA}) {
			t.Errorf("GetConnectionIDsForCluster = %v, %v", ids, err)
		}
		ids, err = ds.GetConnectionIDsForGroup(ctx, f.manualGroup)
		if err != nil || containsInt(ids, f.manB) {
			t.Errorf("GetConnectionIDsForGroup = %v, %v", ids, err)
		}
	})
}

func sortedInts(ids ...int) []int {
	out := append([]int(nil), ids...)
	sort.Ints(out)
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCollectorSchema_ClosedPoolErrors drives every reader in the files
// above against a closed pool, so each returns or logs its query error.
func TestCollectorSchema_ClosedPoolErrors(t *testing.T) {
	e := newCSEnv(t)
	closed, err := pgxpool.NewWithConfig(e.ctx, e.pool.Config())
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	closed.Close()
	ds := NewTestDatastore(closed)
	ctx := e.ctx

	if _, err := ds.GetClusterTopology(ctx, nil); err == nil {
		t.Error("GetClusterTopology succeeded")
	}
	if err := ds.RefreshClusterAssignments(ctx); err == nil {
		t.Error("RefreshClusterAssignments succeeded")
	}
	if snap, err := ds.GetEstateSnapshot(ctx); err != nil || snap.ServerTotal != 0 {
		t.Errorf("GetEstateSnapshot = %+v, %v", snap, err)
	}
	if snap := ds.GetConnectionsSnapshot(ctx, []int{1}); snap.ServerTotal != 0 {
		t.Errorf("GetConnectionsSnapshot = %+v", snap)
	}
	if _, _, err := ds.GetServerSnapshot(ctx, 1); err == nil {
		t.Error("GetServerSnapshot succeeded")
	}
	if _, _, err := ds.GetClusterSnapshot(ctx, 1); err == nil {
		t.Error("GetClusterSnapshot succeeded")
	}
	if _, _, err := ds.GetGroupSnapshot(ctx, 1); err == nil {
		t.Error("GetGroupSnapshot succeeded")
	}
	if _, err := ds.GetConnectionIDsForCluster(ctx, 1); err == nil {
		t.Error("GetConnectionIDsForCluster succeeded")
	}
	if _, err := ds.GetConnectionIDsForGroup(ctx, 1); err == nil {
		t.Error("GetConnectionIDsForGroup succeeded")
	}
	if _, err := ds.GetConnectionContext(ctx, 1); err == nil {
		t.Error("GetConnectionContext succeeded")
	}
	if _, err := ds.CreateManualCluster(ctx, "x", "", "", nil); err == nil {
		t.Error("CreateManualCluster(default group) succeeded")
	}
	if _, err := ds.CreateManualCluster(ctx, "x", "", "", ip(1)); err == nil {
		t.Error("CreateManualCluster succeeded")
	}
	if _, err := ds.GetConnectionClusterInfo(ctx, 1); err == nil || errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("GetConnectionClusterInfo = %v", err)
	}
	if _, err := ds.ListClustersForAutocomplete(ctx); err == nil {
		t.Error("ListClustersForAutocomplete succeeded")
	}
	if err := ds.ResetMembershipSource(ctx, 1); err == nil || errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("ResetMembershipSource = %v", err)
	}
	if _, err := ds.GetClusterRelationships(ctx, 1); err == nil {
		t.Error("GetClusterRelationships succeeded")
	}
	if err := ds.SetNodeRelationships(ctx, 1, 1, nil); err == nil {
		t.Error("SetNodeRelationships succeeded")
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, 1, nil); err == nil {
		t.Error("SyncAutoDetectedRelationships succeeded")
	}
	if err := ds.RemoveNodeRelationship(ctx, 1); err == nil {
		t.Error("RemoveNodeRelationship succeeded")
	}
	if err := ds.ClearNodeRelationships(ctx, 1, 1); err == nil {
		t.Error("ClearNodeRelationships succeeded")
	}
	if err := ds.AddServerToCluster(ctx, 1, 1, nil); err == nil {
		t.Error("AddServerToCluster succeeded")
	}
	if err := ds.RemoveServerFromCluster(ctx, 1, 1); err == nil {
		t.Error("RemoveServerFromCluster succeeded")
	}
	if _, err := ds.IsConnectionInCluster(ctx, 1, 1); err == nil {
		t.Error("IsConnectionInCluster succeeded")
	}
}

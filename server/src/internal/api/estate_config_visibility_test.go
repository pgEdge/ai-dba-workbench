/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Estate-wide configuration needs every connection visible (issue #744)
//
// An alert rule's defaults, a global probe config, and a notification
// channel and its recipients apply to every connection, so only a caller
// who sees every connection may change them: a superuser, an
// all-connections grant, or grants naming every connection. Anyone else
// gets the audited missing-permission refusal; a narrowed token keeps
// the token-scope refusal, which runs first.
// =============================================================================

// estateConfigFixture holds the estate-wide handlers over the coverage
// fixture, seeded with an estate default email channel that has one
// recipient and with a probe config on the visible connection.
type estateConfigFixture struct {
	*covFixture
	store         *auth.AuthStore
	alertRule     *AlertRuleHandler
	probeConfig   *ProbeConfigHandler
	channels      *NotificationChannelHandler
	globalProbeID int64
	serverProbeID int64
	channelID     int64
	recipientID   int64
	audited       []string
}

func newEstateConfigFixture(t *testing.T) *estateConfigFixture {
	t.Helper()
	cf := newCovFixture(t)
	store := newTestAuthStore(t)
	checker := auth.NewRBACChecker(store)
	f := &estateConfigFixture{
		covFixture:  cf,
		store:       store,
		alertRule:   NewAlertRuleHandler(cf.ds, store, checker),
		probeConfig: NewProbeConfigHandler(cf.ds, store, checker),
		channels:    NewNotificationChannelHandlerWithSecurity(cf.ds, store, checker, false, nil, nil),
	}
	record := func(_ *http.Request, reason string) { f.audited = append(f.audited, reason) }
	f.alertRule.SetDenialRecorder(record)
	f.probeConfig.SetDenialRecorder(record)
	f.channels.SetDenialRecorder(record)

	scan := func(dst any, q string, args ...any) {
		t.Helper()
		if err := cf.pool.QueryRow(context.Background(), q, args...).Scan(dst); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	scan(&f.globalProbeID, `SELECT id FROM probe_configs WHERE scope = 'global'`)
	scan(&f.serverProbeID, `INSERT INTO probe_configs (name, description, scope, connection_id)
        VALUES ($1, 'Activity probe', 'server', $2) RETURNING id`, cf.probeName, cf.connID)
	scan(&f.channelID, `INSERT INTO notification_channels
        (channel_type, name, smtp_host, from_address, is_estate_default)
        VALUES ('email', 'estate default', 'smtp.example.com', 'alerts@example.com', TRUE)
        RETURNING id`)
	scan(&f.recipientID, `INSERT INTO email_recipients (channel_id, email_address)
        VALUES ($1, 'jane.doe@example.com') RETURNING id`, f.channelID)
	return f
}

// every lists the fixture's connections, and allButOne all of them bar
// the hidden one.
func (f *estateConfigFixture) every() []int     { return []int{f.connID, f.hiddenConnID} }
func (f *estateConfigFixture) allButOne() []int { return []int{f.connID} }

// state renders everything an estate-wide write could change, so that a
// refusal can be shown to have changed nothing.
func (f *estateConfigFixture) state(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.pool.QueryRow(context.Background(), `SELECT concat_ws('|',
            (SELECT default_threshold::text FROM alert_rules WHERE id = $1),
            (SELECT is_enabled::text FROM probe_configs WHERE id = $2),
            (SELECT string_agg(name || enabled::text || is_estate_default::text, ',' ORDER BY id)
               FROM notification_channels),
            (SELECT coalesce(string_agg(email_address || enabled::text, ',' ORDER BY id), '')
               FROM email_recipients))`, f.ruleID, f.globalProbeID).Scan(&s); err != nil {
		t.Fatalf("read state: %v", err)
	}
	return s
}

// estateCaller creates a user holding every permission the estate-wide
// writes need, granted the given connections, and returns a decorator
// acting as that user: through a session when scope is nil, otherwise
// through a token with that connection scope (an empty scope leaves the
// token unrestricted).
func (f *estateConfigFixture) estateCaller(t *testing.T, name string, grants []int,
	scope []auth.ScopedConnection) func(*http.Request) *http.Request {

	t.Helper()
	userID := setupUserWithPermissions(t, f.store, name, []string{
		auth.PermManageAlertRules, auth.PermManageProbes, auth.PermManageNotificationChannels,
	})
	groups, err := f.store.GetUserGroups(userID)
	if err != nil || len(groups) == 0 {
		t.Fatalf("GetUserGroups: %v %v", groups, err)
	}
	for _, id := range grants {
		if err := f.store.GrantConnectionPrivilege(groups[0], id, auth.AccessLevelReadWrite); err != nil {
			t.Fatalf("GrantConnectionPrivilege: %v", err)
		}
	}
	if scope == nil {
		return func(r *http.Request) *http.Request { return withUsername(withUser(r, userID), name) }
	}
	_, token, err := f.store.AsActor(auth.SystemActor()).CreateToken(name, "issue 744", nil, false)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if len(scope) > 0 {
		if err := f.store.AsActor(auth.SystemActor()).SetTokenConnectionScope(token.ID, scope, true); err != nil {
			t.Fatalf("SetTokenConnectionScope: %v", err)
		}
	}
	return func(r *http.Request) *http.Request {
		return withUsername(withToken(r, userID, token.ID), name)
	}
}

// estateWrite is one estate-wide write and the status it answers when
// allowed.
type estateWrite struct {
	name string
	ok   int
	do   func(f *estateConfigFixture, caller func(*http.Request) *http.Request) *httptest.ResponseRecorder
}

func channelPath(id int64, suffix string) string {
	return fmt.Sprintf("/api/v1/notification-channels/%d%s", id, suffix)
}

func recipientPath(f *estateConfigFixture) string {
	return channelPath(f.channelID, fmt.Sprintf("/recipients/%d", f.recipientID))
}

var estateWrites = []estateWrite{
	{"alert rule", http.StatusOK, func(f *estateConfigFixture, c func(*http.Request) *http.Request) *httptest.ResponseRecorder {
		return covDo(f.alertRule.handleAlertRuleSubpath, http.MethodPut,
			fmt.Sprintf("/api/v1/alert-rules/%d", f.ruleID), `{"default_threshold":20}`, c)
	}},
	{"global probe config", http.StatusOK, func(f *estateConfigFixture, c func(*http.Request) *http.Request) *httptest.ResponseRecorder {
		return covDo(f.probeConfig.handleProbeConfigSubpath, http.MethodPut,
			fmt.Sprintf("/api/v1/probe-configs/%d", f.globalProbeID), `{"is_enabled":false}`, c)
	}},
	{"create estate default channel", http.StatusCreated, func(f *estateConfigFixture, c func(*http.Request) *http.Request) *httptest.ResponseRecorder {
		return covDo(f.channels.handleChannels, http.MethodPost, "/api/v1/notification-channels",
			`{"channel_type":"webhook","name":"mine","endpoint_url":"https://example.com/wh","is_estate_default":true}`, c)
	}},
	{"disable estate default channel", http.StatusOK, func(f *estateConfigFixture, c func(*http.Request) *http.Request) *httptest.ResponseRecorder {
		return covDo(f.channels.handleChannelSubpath, http.MethodPut, channelPath(f.channelID, ""),
			`{"enabled":false}`, c)
	}},
	{"delete estate default channel", http.StatusOK, func(f *estateConfigFixture, c func(*http.Request) *http.Request) *httptest.ResponseRecorder {
		return covDo(f.channels.handleChannelSubpath, http.MethodDelete, channelPath(f.channelID, ""), "", c)
	}},
	{"add recipient", http.StatusCreated, func(f *estateConfigFixture, c func(*http.Request) *http.Request) *httptest.ResponseRecorder {
		return covDo(f.channels.handleChannelSubpath, http.MethodPost, channelPath(f.channelID, "/recipients"),
			`{"email_address":"john.doe@example.com"}`, c)
	}},
	{"disable recipient", http.StatusOK, func(f *estateConfigFixture, c func(*http.Request) *http.Request) *httptest.ResponseRecorder {
		return covDo(f.channels.handleChannelSubpath, http.MethodPut, recipientPath(f),
			`{"email_address":"jane.doe@example.com","enabled":false}`, c)
	}},
	{"delete recipient", http.StatusOK, func(f *estateConfigFixture, c func(*http.Request) *http.Request) *httptest.ResponseRecorder {
		return covDo(f.channels.handleChannelSubpath, http.MethodDelete, recipientPath(f), "", c)
	}},
}

// estateConfigCaller is one kind of caller and how the estate-wide
// writes treat it.
type estateConfigCaller struct {
	name    string
	make    func(t *testing.T, f *estateConfigFixture) func(*http.Request) *http.Request
	allowed bool
	// tokenScope marks a caller the token-scope check refuses first.
	tokenScope bool
}

var estateConfigCallers = []estateConfigCaller{
	{"superuser", func(*testing.T, *estateConfigFixture) func(*http.Request) *http.Request {
		return withSuperuser
	}, true, false},
	{"all-connections grant", func(t *testing.T, f *estateConfigFixture) func(*http.Request) *http.Request {
		return f.estateCaller(t, "wildcard_grant", []int{auth.ConnectionIDAll}, nil)
	}, true, false},
	{"every connection by ID", func(t *testing.T, f *estateConfigFixture) func(*http.Request) *http.Request {
		return f.estateCaller(t, "every_by_id", f.every(), nil)
	}, true, false},
	{"every connection by ID, unscoped token", func(t *testing.T, f *estateConfigFixture) func(*http.Request) *http.Request {
		return f.estateCaller(t, "every_by_id_token", f.every(), []auth.ScopedConnection{})
	}, true, false},
	{"all but one connection", func(t *testing.T, f *estateConfigFixture) func(*http.Request) *http.Request {
		return f.estateCaller(t, "all_but_one", f.allButOne(), nil)
	}, false, false},
	{"all but one connection, unscoped token", func(t *testing.T, f *estateConfigFixture) func(*http.Request) *http.Request {
		return f.estateCaller(t, "all_but_one_token", f.allButOne(), []auth.ScopedConnection{})
	}, false, false},
	{"every connection by ID, narrowed token", func(t *testing.T, f *estateConfigFixture) func(*http.Request) *http.Request {
		return f.estateCaller(t, "narrowed_token", f.every(), []auth.ScopedConnection{
			{ConnectionID: f.connID, AccessLevel: auth.AccessLevelReadWrite},
		})
	}, false, true},
}

// TestEstateConfigWritesNeedEveryConnectionVisible drives every
// estate-wide write as each kind of caller.
func TestEstateConfigWritesNeedEveryConnectionVisible(t *testing.T) {
	for _, wr := range estateWrites {
		for _, c := range estateConfigCallers {
			t.Run(wr.name+"/"+c.name, func(t *testing.T) {
				f := newEstateConfigFixture(t)
				caller := c.make(t, f)
				// The refusal a caller without the permission gets.
				noPerm := wr.do(f, func(r *http.Request) *http.Request { return withUser(r, 424242) })
				before := f.state(t)
				f.audited = nil

				rec := wr.do(f, caller)
				switch {
				case c.allowed:
					if rec.Code != wr.ok {
						t.Fatalf("status %d, want %d; body=%s", rec.Code, wr.ok, rec.Body.String())
					}
					if f.state(t) == before {
						t.Errorf("the write changed nothing")
					}
					return
				case c.tokenScope:
					assertOutOfTokenScope(t, rec)
				default:
					if rec.Code != http.StatusForbidden || rec.Body.String() != noPerm.Body.String() {
						t.Errorf("status %d body %q, want the missing-permission refusal %q",
							rec.Code, rec.Body.String(), noPerm.Body.String())
					}
				}
				if len(f.audited) != 1 {
					t.Errorf("audited %v, want one refusal", f.audited)
				}
				if after := f.state(t); after != before {
					t.Errorf("state changed from %s to %s", before, after)
				}
			})
		}
	}
}

// TestEstateConfigRestrictedCallerKeepsScopedAccess checks that a caller
// who cannot see every connection may still read the estate-wide
// objects and change a probe config on a connection they can see.
func TestEstateConfigRestrictedCallerKeepsScopedAccess(t *testing.T) {
	f := newEstateConfigFixture(t)
	caller := f.estateCaller(t, "all_but_one", f.allButOne(), nil)

	reads := []struct {
		h    http.HandlerFunc
		path string
	}{
		{f.alertRule.handleAlertRules, "/api/v1/alert-rules"},
		{f.alertRule.handleAlertRuleSubpath, fmt.Sprintf("/api/v1/alert-rules/%d", f.ruleID)},
		{f.probeConfig.handleProbeConfigSubpath, fmt.Sprintf("/api/v1/probe-configs/%d", f.globalProbeID)},
		{f.channels.handleChannels, "/api/v1/notification-channels"},
		{f.channels.handleChannelSubpath, channelPath(f.channelID, "")},
		{f.channels.handleChannelSubpath, channelPath(f.channelID, "/recipients")},
	}
	for _, rd := range reads {
		covExpect(t, covDo(rd.h, http.MethodGet, rd.path, "", caller), http.StatusOK)
	}

	covExpect(t, covDo(f.probeConfig.handleProbeConfigSubpath, http.MethodPut,
		fmt.Sprintf("/api/v1/probe-configs/%d", f.serverProbeID), `{"is_enabled":false}`, caller),
		http.StatusOK)
}

// TestEstateConfigWritesFailClosed covers the 500s an estate-wide write
// gives when the caller's view of the estate cannot be worked out, and
// the refusal when a connection appears between the two reads.
func TestEstateConfigWritesFailClosed(t *testing.T) {
	failing := func(context.Context) ([]int, error) { return nil, errors.New("boom") }

	for _, wr := range estateWrites {
		t.Run(wr.name+"/list fails", func(t *testing.T) {
			f := newEstateConfigFixture(t)
			f.alertRule.allConnectionIDs = failing
			f.probeConfig.allConnectionIDs = failing
			f.channels.allConnectionIDs = failing
			caller := f.estateCaller(t, "every_by_id", f.every(), nil)
			assertError(t, wr.do(f, caller), http.StatusInternalServerError, "Failed to check access")
		})
	}

	// A restricted caller's visible set is listed from the connections
	// table, so without it their access cannot be resolved. The global
	// probe config's own visibility check answers first, with its own
	// message, so it is not driven here.
	t.Run("visible set fails", func(t *testing.T) {
		f := newEstateConfigFixture(t)
		caller := f.estateCaller(t, "every_by_id", f.every(), nil)
		mustExec(t, f.pool, `DROP TABLE connections CASCADE`)
		for _, wr := range estateWrites {
			if wr.name == "global probe config" {
				continue
			}
			assertError(t, wr.do(f, caller), http.StatusInternalServerError, "Failed to check access")
		}
	})

	// The connections are listed after the visible set is resolved, so a
	// connection added in between is one the caller cannot see.
	t.Run("connection added mid-check", func(t *testing.T) {
		f := newEstateConfigFixture(t)
		caller := f.estateCaller(t, "every_by_id", f.every(), nil)
		list := f.alertRule.allConnectionIDs
		f.alertRule.allConnectionIDs = func(ctx context.Context) ([]int, error) {
			if _, err := f.pool.Exec(ctx, `INSERT INTO connections (name, owner_username)
                VALUES ('late', 'late_owner')`); err != nil {
				return nil, err
			}
			return list(ctx)
		}
		before := f.state(t)
		f.audited = nil
		rec := estateWrites[0].do(f, caller)
		if rec.Code != http.StatusForbidden || len(f.audited) != 1 {
			t.Errorf("status %d audited %v, want an audited 403", rec.Code, f.audited)
		}
		if after := f.state(t); after != before {
			t.Errorf("state changed from %s to %s", before, after)
		}
	})
}

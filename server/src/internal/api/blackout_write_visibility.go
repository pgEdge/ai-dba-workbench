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
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// blackoutTarget is what a blackout or blackout schedule silences: one
// connection, a cluster, a cluster group, or, with no ID set, the whole
// estate.
type blackoutTarget struct {
	groupID, clusterID, connectionID *int
}

func newBlackoutTarget(groupID, clusterID, connectionID *int) blackoutTarget {
	return blackoutTarget{groupID: groupID, clusterID: clusterID, connectionID: connectionID}
}

func targetOfBlackout(b *database.Blackout) blackoutTarget {
	return newBlackoutTarget(b.GroupID, b.ClusterID, b.ConnectionID)
}

func targetOfSchedule(s *database.BlackoutSchedule) blackoutTarget {
	return newBlackoutTarget(s.GroupID, s.ClusterID, s.ConnectionID)
}

// notFoundMessage is the 404 a restricted caller gets when a request
// body names a target they cannot see.
func (t blackoutTarget) notFoundMessage() string {
	switch {
	case t.connectionID != nil:
		return "Connection not found"
	case t.clusterID != nil:
		return "Cluster not found"
	default:
		return "Cluster group not found"
	}
}

// blackoutTargetReach returns the connections a target silences and
// whether at least one of them is in the visible set. An estate-wide
// target reaches no listed member and is visible to everyone; a cluster
// or group with no members is not visible, as on the read routes.
func blackoutTargetReach(ctx context.Context, ds rbacVisibilityDatastore,
	t blackoutTarget, visible map[int]bool) ([]int, bool, error) {

	switch {
	case t.connectionID != nil:
		return []int{*t.connectionID}, visible[*t.connectionID], nil
	case t.clusterID != nil:
		ids, err := ds.GetConnectionIDsForCluster(ctx, *t.clusterID)
		return ids, clusterMembersVisible(ids, visible), err
	case t.groupID != nil:
		ids, err := ds.GetConnectionIDsForGroup(ctx, *t.groupID)
		return ids, clusterMembersVisible(ids, visible), err
	}
	return nil, true, nil
}

// blackoutWriteAccess carries what a blackout write learned about the
// caller's visibility from one gate to the next.
type blackoutWriteAccess struct {
	all     bool
	visible map[int]bool
	members []int
}

// resolveBlackoutWriteAccess resolves the caller's visible connections,
// answering 500 and returning false when they cannot be read.
func (h *BlackoutHandler) resolveBlackoutWriteAccess(w http.ResponseWriter,
	r *http.Request) (blackoutWriteAccess, bool) {

	visible, all, err := resolveVisibleConnectionSet(r.Context(), h.rbacChecker, h.datastore)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to check access")
		return blackoutWriteAccess{}, false
	}
	return blackoutWriteAccess{all: all, visible: visible}, true
}

// requireBlackoutTargetVisible answers 404 with notFoundMsg, and returns
// false, when a restricted caller cannot see the target at all, so that
// a hidden target answers as a missing one does. It records the
// target's members in a for requireBlackoutMembersVisible.
func (h *BlackoutHandler) requireBlackoutTargetVisible(w http.ResponseWriter,
	r *http.Request, a *blackoutWriteAccess, t blackoutTarget,
	notFoundMsg string) bool {

	if a.all {
		return true
	}
	members, seen, err := blackoutTargetReach(r.Context(), h.datastore, t, a.visible)
	if err != nil {
		log.Printf("[ERROR] Failed to check blackout target visibility: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to check access")
		return false
	}
	if !seen {
		RespondError(w, http.StatusNotFound, notFoundMsg)
		return false
	}
	a.members = members
	return true
}

// requireBlackoutMembersVisible refuses a blackout write unless a
// restricted caller can see every connection the target reaches (issue
// #732), since the blackout silences alerting on all of them. The
// refusal is the missing-permission refusal, audited, so that it says
// nothing about which members are hidden or how many, as for overrides
// (issue #716). It runs after the token-scope check, so a narrowed
// token keeps the token-scope refusal.
func (h *BlackoutHandler) requireBlackoutMembersVisible(w http.ResponseWriter,
	r *http.Request, a blackoutWriteAccess) bool {

	if a.all || allMembersVisible(a.members, a.visible) {
		return true
	}
	h.denialRecorder.refuse(w, r, adminPermissionRefusal(auth.PermManageBlackouts))
	return false
}

// requireBlackoutTargetWritable applies the gates every blackout write
// passes, in order: the target must be visible to the caller (404 with
// notFoundMsg, as though missing), in the token's scope (audited 403)
// and have every member visible (audited 403). A stored blackout or
// schedule passes the message its GET answers for a missing ID; an
// empty notFoundMsg, for a target named in a request body, takes the
// target's own notFoundMessage.
func (h *BlackoutHandler) requireBlackoutTargetWritable(w http.ResponseWriter,
	r *http.Request, a blackoutWriteAccess, scope string, t blackoutTarget,
	notFoundMsg string) bool {

	if notFoundMsg == "" {
		notFoundMsg = t.notFoundMessage()
	}
	return h.requireBlackoutTargetVisible(w, r, &a, t, notFoundMsg) &&
		requireBlackoutTargetInTokenScope(w, r, h.rbacChecker, h.denialRecorder, scope, t.connectionID) &&
		h.requireBlackoutMembersVisible(w, r, a)
}

// requireRequestedBlackoutTargetWritable applies
// requireBlackoutTargetWritable to the target a create request names.
func (h *BlackoutHandler) requireRequestedBlackoutTargetWritable(w http.ResponseWriter,
	r *http.Request, scope string, groupID, clusterID, connectionID *int) bool {

	a, ok := h.resolveBlackoutWriteAccess(w, r)
	return ok && h.requireBlackoutTargetWritable(w, r, a, scope,
		newBlackoutTarget(groupID, clusterID, connectionID), "")
}

// unrestrictedBlackoutWriter reports whether the caller sees every
// connection and holds a token scope covering every connection, so that
// no stored record needs loading to allow a write by ID.
func (h *BlackoutHandler) unrestrictedBlackoutWriter(r *http.Request, a blackoutWriteAccess) bool {
	return a.all && h.rbacChecker.ConnectionReadableInTokenScope(r.Context(), auth.ConnectionIDAll)
}

// requireBlackoutWritable checks a write to the stored blackout with
// the given ID: 404 when it is missing or its target is hidden from the
// caller, then the token-scope and every-member checks.
func (h *BlackoutHandler) requireBlackoutWritable(w http.ResponseWriter,
	r *http.Request, id int64) bool {

	a, ok := h.resolveBlackoutWriteAccess(w, r)
	if !ok {
		return false
	}
	if h.unrestrictedBlackoutWriter(r, a) {
		return true
	}
	existing, err := h.datastore.GetBlackout(r.Context(), id)
	if respondDBError(w, err, "fetch blackout",
		notFound(database.ErrBlackoutNotFound, "Blackout not found")) {
		return false
	}
	return h.requireBlackoutTargetWritable(w, r, a, existing.Scope,
		targetOfBlackout(existing), "Blackout not found")
}

// requireFetchedBlackoutWritable is requireBlackoutWritable for a
// blackout the handler has already loaded.
func (h *BlackoutHandler) requireFetchedBlackoutWritable(w http.ResponseWriter,
	r *http.Request, b *database.Blackout) bool {

	a, ok := h.resolveBlackoutWriteAccess(w, r)
	if !ok {
		return false
	}
	return h.requireBlackoutTargetWritable(w, r, a, b.Scope,
		targetOfBlackout(b), "Blackout not found")
}

// requireBlackoutScheduleWritable is requireBlackoutWritable for a
// blackout schedule. It also returns the resolved access, so that an
// update can check the schedule's new target without resolving it
// again.
func (h *BlackoutHandler) requireBlackoutScheduleWritable(w http.ResponseWriter,
	r *http.Request, id int64) (blackoutWriteAccess, bool) {

	a, ok := h.resolveBlackoutWriteAccess(w, r)
	if !ok {
		return a, false
	}
	if h.unrestrictedBlackoutWriter(r, a) {
		return a, true
	}
	existing, err := h.datastore.GetBlackoutSchedule(r.Context(), id)
	if respondDBError(w, err, "fetch blackout schedule",
		notFound(database.ErrBlackoutScheduleNotFound, "Blackout schedule not found")) {
		return a, false
	}
	return a, h.requireBlackoutTargetWritable(w, r, a, existing.Scope,
		targetOfSchedule(existing), "Blackout schedule not found")
}

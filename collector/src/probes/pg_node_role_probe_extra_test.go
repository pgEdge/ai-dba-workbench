/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"strings"
	"testing"
)

// TestPgNodeRoleProbe_InfoToMap_LogicalAndSpock covers the conditional
// JSON branches of infoToMap that the basic tests skip: a node with
// publications/subscriptions emits a "logical" key, and a Spock-enabled
// node emits a "spock" key with the discovered node identity.
func TestPgNodeRoleProbe_InfoToMap_LogicalAndSpock(t *testing.T) {
	p := NewPgNodeRoleProbe(&ProbeConfig{Name: ProbeNamePgNodeRole})

	t.Run("logical branch only", func(t *testing.T) {
		info := &NodeRoleInfo{
			PublicationCount:  2,
			SubscriptionCount: 1,
			RoleDetails:       map[string]any{},
		}
		out := p.infoToMap(info)
		details, ok := out["role_details"].(string)
		if !ok {
			t.Fatalf("role_details is not string: %T",
				out["role_details"])
		}
		if !strings.Contains(details, `"logical"`) {
			t.Errorf("expected logical key, got %q", details)
		}
		if strings.Contains(details, `"spock"`) {
			t.Errorf("did not expect spock key, got %q", details)
		}
	})

	t.Run("spock branch only", func(t *testing.T) {
		nodeID := int64(7)
		nodeName := "node1"
		info := &NodeRoleInfo{
			HasSpock: true, SpockNodeID: &nodeID,
			SpockNodeName:          &nodeName,
			SpockSubscriptionCount: 3,
			RoleDetails:            map[string]any{},
		}
		out := p.infoToMap(info)
		details, ok := out["role_details"].(string)
		if !ok {
			t.Fatalf("role_details is not string: %T",
				out["role_details"])
		}
		if !strings.Contains(details, `"spock"`) {
			t.Errorf("expected spock key, got %q", details)
		}
		if !strings.Contains(details, `"node_name"`) {
			t.Errorf("expected node_name in spock JSON, got %q",
				details)
		}
	})

	t.Run("logical and spock together", func(t *testing.T) {
		nodeName := "node2"
		info := &NodeRoleInfo{
			PublicationCount: 1, SubscriptionCount: 0,
			HasSpock: true, SpockNodeName: &nodeName,
			RoleDetails: map[string]any{},
		}
		out := p.infoToMap(info)
		details, ok := out["role_details"].(string)
		if !ok {
			t.Fatalf("role_details is not string: %T",
				out["role_details"])
		}
		if !strings.Contains(details, `"logical"`) ||
			!strings.Contains(details, `"spock"`) {
			t.Errorf("expected both logical and spock, got %q",
				details)
		}
	})
}

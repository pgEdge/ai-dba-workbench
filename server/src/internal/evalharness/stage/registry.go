/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package stage

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/wbclient"
)

const connectionsPath = "/api/v1/connections"

// APIRegistry registers connections through the Workbench REST API, so
// the server stores and encrypts the credentials exactly as it does for
// a connection added in the web client.
type APIRegistry struct {
	Client *wbclient.Client
}

// createConnectionRequest is the body of POST /api/v1/connections.
// The connection is private to the harness's user and is not monitored,
// so no collector polls it.
type createConnectionRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"database_name"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	SSLMode      string `json:"ssl_mode"`
	IsShared     bool   `json:"is_shared"`
	IsMonitored  bool   `json:"is_monitored"`
}

// Create implements Registry.
func (r *APIRegistry) Create(ctx context.Context, spec ConnectionSpec) (int, error) {
	req := createConnectionRequest{
		Name:         spec.Name,
		Description:  "Created by the evaluation harness; removed when the run ends.",
		Host:         spec.Host,
		Port:         spec.Port,
		DatabaseName: spec.Database,
		Username:     spec.Username,
		Password:     spec.Password,
		SSLMode:      spec.SSLMode,
	}
	var resp struct {
		ID int `json:"id"`
	}
	if err := r.Client.Do(ctx, http.MethodPost, connectionsPath, req, &resp); err != nil {
		return 0, err
	}
	if resp.ID == 0 {
		return 0, errors.New("the server did not return a connection id")
	}
	return resp.ID, nil
}

// Delete implements Registry.
func (r *APIRegistry) Delete(ctx context.Context, id int) error {
	return r.Client.Do(ctx, http.MethodDelete, fmt.Sprintf("%s/%d", connectionsPath, id), nil, nil)
}

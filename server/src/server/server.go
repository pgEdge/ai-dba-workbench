/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package server implements the HTTP/HTTPS server with SSE support
package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/pgEdge/ai-workbench/server/src/config"
	"github.com/pgEdge/ai-workbench/server/src/logger"
	"github.com/pgEdge/ai-workbench/server/src/mcp"
)

// Server represents the MCP HTTP/HTTPS server
type Server struct {
	config  *config.Config
	handler *mcp.Handler
	server  *http.Server
	mu      sync.RWMutex
}

// New creates a new server instance
func New(cfg *config.Config, mcpHandler *mcp.Handler) *Server {
	return &Server{
		config:  cfg,
		handler: mcpHandler,
	}
}

// Start starts the HTTP or HTTPS server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/health", s.handleHealth)

	addr := fmt.Sprintf(":%d", s.config.GetPort())
	s.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if s.config.GetTLS() {
		logger.Startupf("Starting HTTPS server on port %d", s.config.GetPort())
		if err := s.server.ListenAndServeTLS(s.config.GetTLSCert(),
			s.config.GetTLSKey()); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("HTTPS server error: %w", err)
		}
	} else {
		logger.Startupf("Starting HTTP server on port %d", s.config.GetPort())
		if err := s.server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			return fmt.Errorf("HTTP server error: %w", err)
		}
	}

	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	logger.Info("Shutting down server...")
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleSSE handles Server-Sent Events connections for MCP
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	logger.Infof("New SSE connection from %s", r.RemoteAddr)

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get flusher for sending data
	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Error("SSE not supported on this connection")
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Send initial connection success message
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
	flusher.Flush()

	// Create a context that will be canceled when the client disconnects
	ctx := r.Context()

	// Read requests from the client
	reader := bufio.NewReader(r.Body)
	for {
		select {
		case <-ctx.Done():
			logger.Infof("SSE connection closed for %s", r.RemoteAddr)
			return
		default:
			// Set read deadline
			// Note: We can't set deadline on r.Body directly in HTTP/2
			// This is a simplified implementation

			// Read a line (JSON-RPC request)
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					logger.Infof("Client closed connection: %s", r.RemoteAddr)
					return
				}
				logger.Errorf("Error reading from client: %v", err)
				return
			}

			// Process the request
			resp, err := s.handler.HandleRequest(line)
			if err != nil {
				logger.Errorf("Error handling request: %v", err)
				continue
			}

			// Format response
			respData, err := mcp.FormatResponse(resp)
			if err != nil {
				logger.Errorf("Error formatting response: %v", err)
				continue
			}

			// Send response via SSE
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", respData)
			flusher.Flush()

			logger.Infof("Sent response to %s", r.RemoteAddr)
		}
	}
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "{\"status\":\"ok\",\"initialized\":%t}\n",
		s.handler.IsInitialized())
}

// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// Server implements an MCP server that exposes performance tools over HTTP
type Server struct {
	logger      logr.Logger
	tools       map[string]ToolHandler
	toolsMux    sync.RWMutex
	sessions    map[string]*Session
	sessionsMux sync.RWMutex
	httpServer  *http.Server
}

// Session represents an MCP session
type Session struct {
	ID        string
	CreatedAt time.Time
	LastUsed  time.Time
}

// NewServer creates a new MCP server
func NewServer(logger logr.Logger) *Server {
	return &Server{
		logger:   logger.WithName("mcp-server"),
		tools:    make(map[string]ToolHandler),
		sessions: make(map[string]*Session),
	}
}

// RegisterTool registers a tool handler with the server
func (s *Server) RegisterTool(handler ToolHandler) {
	s.toolsMux.Lock()
	defer s.toolsMux.Unlock()
	s.tools[handler.Name()] = handler
	s.logger.Info("registered tool", "name", handler.Name(), "description", handler.Description())
}

// ListenAndServe starts the MCP HTTP server on the specified address
func (s *Server) ListenAndServe(address string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCPEndpoint)

	s.httpServer = &http.Server{
		Addr:         address,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.logger.Info("MCP HTTP server listening", "address", address, "endpoint", "/mcp")
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// handleMCPEndpoint handles HTTP requests to the MCP endpoint
func (s *Server) handleMCPEndpoint(w http.ResponseWriter, r *http.Request) {
	// Validate Origin header to prevent DNS rebinding attacks
	if origin := r.Header.Get("Origin"); origin != "" {
		// For development, allow localhost origins
		if !strings.Contains(origin, "localhost") && !strings.Contains(origin, "127.0.0.1") {
			s.logger.V(1).Info("rejecting request with invalid origin", "origin", origin)
			http.Error(w, "Invalid origin", http.StatusForbidden)
			return
		}
	}

	switch r.Method {
	case http.MethodOptions:
		s.handleOPTIONS(w, r)
	case http.MethodPost:
		s.handlePOST(w, r)
	case http.MethodGet:
		s.handleGET(w, r)
	case http.MethodDelete:
		s.handleDELETE(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleOPTIONS handles CORS preflight requests
func (s *Server) handleOPTIONS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id, Accept")
	w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
	w.WriteHeader(http.StatusNoContent)
}

// handlePOST handles JSON-RPC message sending via POST
func (s *Server) handlePOST(w http.ResponseWriter, r *http.Request) {
	// Get or create session
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = s.createSession()
		w.Header().Set("Mcp-Session-Id", sessionID)
	} else {
		s.updateSession(sessionID)
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error(err, "failed to read request body")
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id")

	// Try to parse as batch first, then single request
	var responses []Response

	// Check if it's a batch request (starts with '[')
	if len(body) > 0 && body[0] == '[' {
		var batchReq []Request
		if err := json.Unmarshal(body, &batchReq); err != nil {
			s.sendJSONError(w, nil, ErrorCodeParseError, "Invalid JSON batch request", err)
			return
		}

		for _, req := range batchReq {
			resp := s.handleRequest(r.Context(), &req)
			responses = append(responses, *resp)
		}
	} else {
		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			s.sendJSONError(w, nil, ErrorCodeParseError, "Invalid JSON request", err)
			return
		}

		resp := s.handleRequest(r.Context(), &req)
		responses = append(responses, *resp)
	}

	// Send response(s)
	if len(responses) == 1 {
		json.NewEncoder(w).Encode(responses[0])
	} else {
		json.NewEncoder(w).Encode(responses)
	}
}

// handleGET handles Server-Sent Events streaming via GET
func (s *Server) handleGET(w http.ResponseWriter, r *http.Request) {
	// Check if client supports SSE
	accept := r.Header.Get("Accept")
	if !strings.Contains(accept, "text/event-stream") {
		http.Error(w, "SSE not supported by client", http.StatusNotAcceptable)
		return
	}

	// Get session ID
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Session ID required for SSE", http.StatusBadRequest)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// For now, just send a simple connection acknowledgment
	// In a full implementation, this would handle streaming responses
	fmt.Fprintf(w, "data: {\"type\":\"connection\",\"sessionId\":\"%s\"}\n\n", sessionID)

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// handleDELETE handles session termination via DELETE
func (s *Server) handleDELETE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	s.deleteSession(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// HandleRequest processes a single MCP request (public method for stdio server)
func (s *Server) HandleRequest(req Request) Response {
	ctx := context.Background()
	resp := s.handleRequest(ctx, &req)
	return *resp
}

// handleRequest processes a single MCP request
func (s *Server) handleRequest(ctx context.Context, req *Request) *Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleCallTool(ctx, req)
	case "prompts/list":
		return s.handleListPrompts(req)
	case "resources/list":
		return s.handleListResources(req)
	default:
		return s.createErrorResponse(req.ID, ErrorCodeMethodNotFound,
			fmt.Sprintf("Method not found: %s", req.Method), nil)
	}
}

// handleInitialize handles the initialize request for MCP protocol handshake
func (s *Server) handleInitialize(req *Request) *Response {
	// Return server capabilities
	result := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{
				"listChanged": false,
			},
			"prompts":   map[string]interface{}{},
			"resources": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "system-agent-mcp",
			"version": "1.0.0",
		},
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleListTools handles the tools/list request
func (s *Server) handleListTools(req *Request) *Response {
	var params ListToolsRequest
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.createErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid parameters", err)
		}
	}

	s.toolsMux.RLock()
	tools := make([]Tool, 0, len(s.tools))
	for _, handler := range s.tools {
		tools = append(tools, Tool{
			Name:        handler.Name(),
			Description: handler.Description(),
			InputSchema: handler.InputSchema(),
		})
	}
	s.toolsMux.RUnlock()

	response := ListToolsResponse{
		Tools: tools,
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  response,
	}
}

// handleCallTool handles the tools/call request
func (s *Server) handleCallTool(ctx context.Context, req *Request) *Response {
	var params CallToolRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.createErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid parameters", err)
	}

	s.toolsMux.RLock()
	handler, exists := s.tools[params.Name]
	s.toolsMux.RUnlock()

	if !exists {
		return s.createErrorResponse(req.ID, ErrorCodeToolNotFound,
			fmt.Sprintf("Tool not found: %s", params.Name), nil)
	}

	result, err := handler.Execute(ctx, params.Arguments)
	if err != nil {
		return s.createErrorResponse(req.ID, ErrorCodeToolError,
			fmt.Sprintf("Tool execution failed: %s", err.Error()), err)
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleListPrompts handles the prompts/list request
func (s *Server) handleListPrompts(req *Request) *Response {
	// Return empty prompts list since we don't support prompts
	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"prompts": []interface{}{},
		},
	}
}

// handleListResources handles the resources/list request
func (s *Server) handleListResources(req *Request) *Response {
	// Return empty resources list since we don't support resources
	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"resources": []interface{}{},
		},
	}
}

// createErrorResponse creates an error response
func (s *Server) createErrorResponse(id interface{}, code int, message string, data interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// sendJSONError sends an error response via HTTP
func (s *Server) sendJSONError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	resp := s.createErrorResponse(id, code, message, data)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error(err, "failed to send error response")
	}
}

// createSession creates a new session with a unique ID
func (s *Server) createSession() string {
	// Generate a cryptographically secure session ID
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		s.logger.Error(err, "failed to generate session ID")
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	sessionID := hex.EncodeToString(bytes)

	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	s.sessions[sessionID] = &Session{
		ID:        sessionID,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	s.logger.V(1).Info("created session", "sessionId", sessionID)
	return sessionID
}

// updateSession updates the last used time for a session
func (s *Server) updateSession(sessionID string) {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	if session, exists := s.sessions[sessionID]; exists {
		session.LastUsed = time.Now()
	}
}

// deleteSession removes a session
func (s *Server) deleteSession(sessionID string) {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	delete(s.sessions, sessionID)
	s.logger.V(1).Info("deleted session", "sessionId", sessionID)
}

// cleanupSessions removes expired sessions (could be called periodically)
func (s *Server) cleanupSessions(maxAge time.Duration) {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	now := time.Now()
	for id, session := range s.sessions {
		if now.Sub(session.LastUsed) > maxAge {
			delete(s.sessions, id)
			s.logger.V(1).Info("cleaned up expired session", "sessionId", id)
		}
	}
}

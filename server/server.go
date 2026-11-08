package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"agent/agent"
	"agent/config"
	"agent/llm"
)

// session holds a conversation-scoped agent instance.
type session struct {
	agent *agent.Agent
}

// Server exposes the agent over HTTP.
type Server struct {
	cfg      *config.Config
	mu       sync.Mutex
	sessions map[string]*session
}

// New creates a new HTTP server for the given config.
func New(cfg *config.Config) *Server {
	return &Server{
		cfg:      cfg,
		sessions: make(map[string]*session),
	}
}

// sessionFor returns (creating if needed) the session with the given ID.
func (s *Server) sessionFor(id string) *session {
	if id == "" {
		id = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess
	}
	ag, err := agent.New(s.cfg)
	if err != nil {
		// Fall back to a shared default agent is not possible here;
		// report the failure via a nil session handled by callers.
		return nil
	}
	sess := &session{agent: ag}
	s.sessions[id] = sess
	return sess
}

// Handler returns the HTTP routes of the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/chat", s.handleChat)
	mux.HandleFunc("POST /v1/chat/stream", s.handleChatStream)
	mux.HandleFunc("GET /v1/usage", s.handleUsage)
	return mux
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

// handleHealth reports server liveness.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"model":    s.cfg.LLM.Model,
		"sessions": s.sessionCount(),
	})
}

// handleChat processes a chat request.
// Request:  {"message": "...", "session_id": "..."}
// Response: {"session_id": "...", "response": "...", "usage": {...}}
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	sess := s.sessionFor(req.SessionID)
	if sess == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}

	resp, err := sess.agent.Process(r.Context(), req.Message)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	usage := sess.agent.Usage()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sessionID(req.SessionID),
		"response":   resp,
		"usage":      usage,
	})
}

// handleChatStream processes a streaming chat request over SSE.
// Events: {"delta": "..."} per chunk, {"done": true, "response": "...", "usage": {...}} at the end,
// and {"error": "..."} on failure.
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSSEError(w, "invalid JSON body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeSSEError(w, "message is required")
		return
	}

	sess := s.sessionFor(req.SessionID)
	if sess == nil {
		writeSSEError(w, "failed to create session")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeSSEError(w, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	resp, err := sess.agent.ProcessStream(r.Context(), req.Message, func(chunk string) {
		writeSSE(w, map[string]string{"delta": chunk})
		flusher.Flush()
	})
	if err != nil {
		writeSSE(w, map[string]string{"error": err.Error()})
		flusher.Flush()
		return
	}

	usage := sess.agent.Usage()
	writeSSE(w, map[string]interface{}{
		"done":     true,
		"response": resp,
		"usage":    usage,
	})
	flusher.Flush()
}

// handleUsage reports the token usage of a session.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session_id")
	sess := s.sessionFor(id)
	if sess == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sessionID(id),
		"usage":      sess.agent.Usage(),
	})
}

// sessionCount returns the number of active sessions.
func (s *Server) sessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// sessionID normalizes an empty session id.
func sessionID(id string) string {
	if id == "" {
		return "default"
	}
	return id
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeSSE writes a single SSE event.
func writeSSE(w http.ResponseWriter, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// writeSSEError writes an SSE error event.
func writeSSEError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/event-stream")
	writeSSE(w, map[string]string{"error": msg})
}

// ensure llm import is used (usage types flow through agent API).
var _ = llm.Usage{}

// Package assistanttest provides a scripted OpenAI-compatible server for
// tests that exercise the assistant over real HTTP.
package assistanttest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Reply is one scripted model turn: either Text or a ToolCall.
type Reply struct {
	Text     string
	ToolName string
	ToolArgs string // JSON object
}

// Server answers /chat/completions with the scripted replies in order and
// records every request body.
type Server struct {
	URL string // base URL to use as Config.BaseURL

	mu       sync.Mutex
	replies  []Reply
	requests []map[string]any
	auth     []string
}

// NewServer starts a Server closed on test cleanup.
func NewServer(t *testing.T, replies ...Reply) *Server {
	t.Helper()
	s := &Server{replies: replies}
	ts := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(ts.Close)
	s.URL = ts.URL + "/v1"
	return s
}

// AuthHeaders returns the Authorization header of every request so far.
func (s *Server) AuthHeaders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...)
}

// Requests returns the decoded request bodies received so far.
func (s *Server) Requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.requests...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/chat/completions" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	if len(s.replies) == 0 {
		s.mu.Unlock()
		http.Error(w, `{"error":{"message":"no scripted reply"}}`, http.StatusInternalServerError)
		return
	}
	reply := s.replies[0]
	s.replies = s.replies[1:]
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(completion(reply)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func completion(r Reply) map[string]any {
	msg := map[string]any{"role": "assistant", "content": r.Text}
	finish := "stop"
	if r.ToolName != "" {
		msg["content"] = nil
		msg["tool_calls"] = []map[string]any{{
			"id":       "call_1",
			"type":     "function",
			"function": map[string]any{"name": r.ToolName, "arguments": r.ToolArgs},
		}}
		finish = "tool_calls"
	}
	return map[string]any{
		"id":      "cmpl-test",
		"object":  "chat.completion",
		"created": 0,
		"model":   "test",
		"choices": []map[string]any{{"index": 0, "finish_reason": finish, "message": msg}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
}

// Command fake-openai is a deterministic, OpenAI-compatible chat-completions
// server for the Playwright E2E stack (and for any manual run of the backend
// against a predictable model).
//
// It is deliberately small: one endpoint the backend actually calls, plus two
// control endpoints the E2E fixtures use to switch behaviour between specs.
// Responses come from embedded fixtures (see the scenario package) and the
// prompt-to-output mapping lives in the matcher package.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/tools/fake-openai/matcher"
	"github.com/jamespud/magi/backend/tools/fake-openai/scenario"
)

const maxRequestBody = 4 << 20

type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

func main() {
	addr := flag.String("addr", envOr("FAKE_OPENAI_ADDR", "127.0.0.1:9000"), "listen address")
	flag.Parse()

	store, err := scenario.New()
	if err != nil {
		log.Fatalf("fake-openai: %v", err)
	}
	server := &fakeServer{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.health)
	mux.HandleFunc("/_control/scenario", server.controlScenario)
	mux.HandleFunc("/_control/reset", server.controlReset)
	mux.HandleFunc("/v1/chat/completions", server.chatCompletions)

	log.Printf("fake-openai listening on %s (deterministic fixtures; POST /_control/scenario to switch modes)", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("fake-openai: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

type fakeServer struct {
	store *scenario.Store
}

func (s *fakeServer) health(w http.ResponseWriter, _ *http.Request) {
	mode, _ := s.store.Mode()
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "mode": mode})
}

func (s *fakeServer) controlScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		mode, mod := s.store.Mode()
		writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "modifier": mod})
		return
	}
	var body struct {
		Name    string `json:"name"`
		DelayMS *int   `json:"delay_ms,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid control body: " + err.Error()})
		return
	}
	if err := s.store.SetMode(body.Name, body.DelayMS); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	mode, mod := s.store.Mode()
	log.Printf("control: mode=%s delay_ms=%d", mode, mod.DelayMS)
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "modifier": mod})
}

func (s *fakeServer) controlReset(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.SetMode("normal", nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	log.Print("control: mode=normal")
	writeJSON(w, http.StatusOK, map[string]any{"mode": "normal"})
}

func (s *fakeServer) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST only"})
		return
	}
	var req chatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid chat body: " + err.Error()})
		return
	}

	kind, role := matcher.Classify(matcher.Request{
		System: systemText(req.Messages),
		User:   lastUserText(req.Messages),
	})
	mode, mod := s.store.Mode()
	log.Printf("chat.completions model=%s kind=%s role=%q mode=%s stream=%t", req.Model, kind, role, mode, req.Stream)

	if mod.DelayMS > 0 {
		time.Sleep(time.Duration(mod.DelayMS) * time.Millisecond)
	}
	if mod.Status >= 400 {
		writeJSON(w, mod.Status, map[string]any{"error": map[string]any{
			"message": fmt.Sprintf("fake-openai scenario %q", mode), "type": "fake_openai_scenario",
		}})
		return
	}

	raw, err := s.store.Response(string(kind), role)
	if err != nil {
		// Loud by design: an unclassifiable request must fail the run visibly
		// rather than produce a plausible-but-wrong state.
		log.Printf("chat.completions: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{
			"message": err.Error(), "type": "fake_openai_no_fixture",
		}})
		return
	}
	content := renderContent(kind, raw, mod.Malformed)
	writeJSON(w, http.StatusOK, chatCompletionResponse(req.Model, content))
}

// renderContent turns a fixture into the assistant message content: plain text
// for the compaction summarizer, the raw JSON text otherwise.
func renderContent(kind matcher.Kind, raw json.RawMessage, malformed bool) string {
	if kind == matcher.KindCompaction {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			return text
		}
		return string(raw)
	}
	if malformed {
		return `{"this is not valid json`
	}
	return string(raw)
}

func chatCompletionResponse(model, content string) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-fake-openai",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
}

func systemText(messages []chatMessage) string {
	for _, m := range messages {
		if m.Role == "system" {
			return messageText(m.Content)
		}
	}
	return ""
}

func lastUserText(messages []chatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messageText(messages[i].Content)
		}
	}
	return ""
}

// messageText accepts either a plain string or OpenAI's content-parts array.
func messageText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

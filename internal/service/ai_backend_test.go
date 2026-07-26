package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The two backends speak different wire formats. These pin both shapes, since
// a mismatch fails at runtime against a real server rather than at compile time.

func TestUsesOpenAIFormatOnlyWhenBaseURLSet(t *testing.T) {
	if NewAIItineraryService("key", "m", "").usesOpenAIFormat() {
		t.Error("no base URL should mean the Anthropic format")
	}
	if !NewAIItineraryService("", "m", "http://h:11434/v1").usesOpenAIFormat() {
		t.Error("a base URL should switch to the OpenAI format")
	}
}

func TestConfiguredRulesDifferPerBackend(t *testing.T) {
	// Anthropic needs a key.
	if NewAIItineraryService("", "m", "").Configured() {
		t.Error("Anthropic without a key must not be configured")
	}
	if !NewAIItineraryService("k", "m", "").Configured() {
		t.Error("Anthropic with a key should be configured")
	}
	// A local server usually wants no key, so a model name is what matters.
	if !NewAIItineraryService("", "qwen3-coder:30b", "http://h:11434/v1").Configured() {
		t.Error("local endpoint with a model should be configured without a key")
	}
	if NewAIItineraryService("", "", "http://h:11434/v1").Configured() {
		t.Error("local endpoint without a model name must not be configured")
	}
}

func TestBaseURLTrailingSlashTrimmed(t *testing.T) {
	s := NewAIItineraryService("", "m", "http://h:11434/v1/")
	if s.baseURL != "http://h:11434/v1" {
		t.Errorf("baseURL = %q, want the trailing slash trimmed", s.baseURL)
	}
}

func TestChatTextOpenAIRequestShape(t *testing.T) {
	var gotPath, gotAuth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello from local"}}]}`))
	}))
	defer srv.Close()

	s := NewAIItineraryService("", "qwen3-coder:30b", srv.URL+"/v1")
	out, err := s.chatText(context.Background(), "SYS", "USER", 1234)
	if err != nil {
		t.Fatalf("chatText: %v", err)
	}
	if out != "hello from local" {
		t.Errorf("reply = %q", out)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	// No key configured => no Authorization header at all, which is what local
	// servers expect.
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none when no key is set", gotAuth)
	}
	if body["model"] != "qwen3-coder:30b" {
		t.Errorf("model = %v", body["model"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", body["messages"])
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "SYS" {
		t.Errorf("system message = %v", first)
	}
	second := msgs[1].(map[string]any)
	if second["role"] != "user" || second["content"] != "USER" {
		t.Errorf("user message = %v", second)
	}
	// Anthropic-only fields must not leak into an OpenAI request.
	if _, present := body["system"]; present {
		t.Error("OpenAI request must not carry a top-level system block")
	}
}

func TestChatTextOpenAISendsKeyWhenPresent(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer srv.Close()
	s := NewAIItineraryService("secret", "m", srv.URL+"/v1")
	if _, err := s.chatText(context.Background(), "s", "u", 10); err != nil {
		t.Fatalf("chatText: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestChatTextSurfacesUpstreamStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // e.g. model not pulled on ollama
	}))
	defer srv.Close()
	s := NewAIItineraryService("", "missing-model", srv.URL+"/v1")
	_, err := s.chatText(context.Background(), "s", "u", 10)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want the upstream status surfaced", err)
	}
}

func TestChatTextUnconfiguredFailsFast(t *testing.T) {
	s := NewAIItineraryService("", "", "")
	if _, err := s.chatText(context.Background(), "s", "u", 10); err != ErrAIItineraryNotConfigured {
		t.Errorf("err = %v, want ErrAIItineraryNotConfigured", err)
	}
}

// decodeChatReply must handle both envelopes.
func TestDecodeChatReply(t *testing.T) {
	openai := `{"choices":[{"message":{"content":"from openai"}}]}`
	got, err := decodeChatReply(strings.NewReader(openai), true)
	if err != nil || got != "from openai" {
		t.Errorf("openai decode = %q, %v", got, err)
	}

	// Anthropic: non-text blocks (thinking) must be skipped, text concatenated.
	anthropic := `{"content":[{"type":"thinking","text":"ignore me"},{"type":"text","text":"part one "},{"type":"text","text":"part two"}]}`
	got, err = decodeChatReply(strings.NewReader(anthropic), false)
	if err != nil || got != "part one part two" {
		t.Errorf("anthropic decode = %q, %v", got, err)
	}
}

func TestDecodeChatReplyEmptyChoicesIsAnError(t *testing.T) {
	if _, err := decodeChatReply(strings.NewReader(`{"choices":[]}`), true); err == nil {
		t.Error("an empty choices array should be an error, not an empty string")
	}
}

// Receipt OCR sends Anthropic-shaped image blocks and needs vision; against a
// local text model it must say so rather than fail obscurely.
func TestReceiptOCRRejectsOpenAIBackend(t *testing.T) {
	ai := NewAIItineraryService("", "qwen3-coder:30b", "http://h:11434/v1")
	svc := NewReceiptOCRService(ai)
	_, err := svc.run(context.Background(), map[string]any{"type": "image"})
	if err == nil {
		t.Fatal("expected receipt OCR to refuse a non-vision backend")
	}
	if !strings.Contains(err.Error(), "vision") {
		t.Errorf("err = %v, want it to name the vision requirement", err)
	}
}

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
)

func TestGenerateReplyDemo(t *testing.T) {
	r := New()
	got, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello world"}},
		}},
	}, GenerateConfig{ProviderID: ProviderDemo, Model: "demo-chat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Echo: hello world" {
		t.Fatalf("unexpected reply: %s", got)
	}
}

func TestNewRunnerUsesNoGlobalHTTPTimeout(t *testing.T) {
	t.Parallel()
	r := New()
	if r.httpClient == nil {
		t.Fatal("httpClient should not be nil")
	}
	if r.httpClient.Timeout != 0 {
		t.Fatalf("expected no global timeout for streaming, got=%s", r.httpClient.Timeout)
	}
}

func TestGenerateReplyOpenAISuccess(t *testing.T) {
	t.Parallel()
	var auth string
	var model string
	var reasoningEffort string

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		model, _ = req["model"].(string)
		reasoningEffort, _ = req["reasoning_effort"].(string)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello from provider"}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	got, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID:      ProviderOpenAI,
		Model:           "gpt-4o-mini",
		APIKey:          "sk-test",
		BaseURL:         mock.URL,
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello from provider" {
		t.Fatalf("unexpected reply: %s", got)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("unexpected auth header: %s", auth)
	}
	if model != "gpt-4o-mini" {
		t.Fatalf("unexpected model: %s", model)
	}
	if reasoningEffort != "low" {
		t.Fatalf("unexpected reasoning_effort: %q", reasoningEffort)
	}
}

func TestGenerateReplyOpenAIMissingAPIKey(t *testing.T) {
	t.Parallel()
	r := New()
	_, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
	})
	assertRunnerCode(t, err, ErrorCodeProviderNotConfigured)
}

func TestGenerateReplyOpenAIUpstreamFailure(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	_, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	})
	assertRunnerCode(t, err, ErrorCodeProviderRequestFailed)
}

func TestGenerateReplyUnsupportedProvider(t *testing.T) {
	t.Parallel()
	r := New()
	_, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: "unknown-provider",
		Model:      "demo-chat",
	})
	assertRunnerCode(t, err, ErrorCodeProviderNotSupported)
}

func TestGenerateReplyCustomProviderWithAdapter(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello from custom adapter"}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	got, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: "custom-provider",
		Model:      "custom-model",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
		AdapterID:  provider.AdapterOpenAICompatible,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello from custom adapter" {
		t.Fatalf("unexpected reply: %s", got)
	}
}

func TestGenerateTurnOpenAICompatibleWithStoreIncludesCacheFields(t *testing.T) {
	t.Parallel()
	var store bool
	var promptCacheKey string
	var previousResponseID string

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		store, _ = req["store"].(bool)
		promptCacheKey, _ = req["prompt_cache_key"].(string)
		previousResponseID, _ = req["previous_response_id"].(string)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","choices":[{"message":{"content":"hello from compat cache"}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID:         "openai-compatible",
		Model:              "ark-code-latest",
		APIKey:             "sk-test",
		BaseURL:            mock.URL,
		Store:              true,
		PromptCacheKey:     "session-1",
		PreviousResponseID: "resp_prev",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(turn.Text) != "hello from compat cache" {
		t.Fatalf("unexpected reply: %q", turn.Text)
	}
	if turn.ResponseID != "chatcmpl_1" {
		t.Fatalf("unexpected response id: %q", turn.ResponseID)
	}
	if !store {
		t.Fatalf("expected store=true for openai-compatible request")
	}
	if promptCacheKey != "session-1" {
		t.Fatalf("unexpected prompt_cache_key: %q", promptCacheKey)
	}
	if previousResponseID != "resp_prev" {
		t.Fatalf("unexpected previous_response_id: %q", previousResponseID)
	}
}

func TestGenerateTurnOpenAIBuiltinSkipsCacheFields(t *testing.T) {
	t.Parallel()
	var req map[string]interface{}

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl_openai","choices":[{"message":{"content":"hello openai"}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID:         ProviderOpenAI,
		Model:              "gpt-4o-mini",
		APIKey:             "sk-test",
		BaseURL:            mock.URL,
		Store:              true,
		PromptCacheKey:     "session-1",
		PreviousResponseID: "resp_prev",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.ResponseID != "chatcmpl_openai" {
		t.Fatalf("unexpected response id: %q", turn.ResponseID)
	}
	if _, ok := req["store"]; ok {
		t.Fatalf("builtin openai request should not carry store, got=%#v", req["store"])
	}
	if _, ok := req["prompt_cache_key"]; ok {
		t.Fatalf("builtin openai request should not carry prompt_cache_key, got=%#v", req["prompt_cache_key"])
	}
	if _, ok := req["previous_response_id"]; ok {
		t.Fatalf("builtin openai request should not carry previous_response_id, got=%#v", req["previous_response_id"])
	}
}

func TestGenerateTurnStreamOpenAICompatibleWithStoreCapturesResponseID(t *testing.T) {
	t.Parallel()
	var requestBody map[string]interface{}

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl_stream_1\",\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl_stream_1\",\"choices\":[{\"delta\":{\"content\":\"stream\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurnStream(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID:         "openai-compatible",
		Model:              "ark-code-latest",
		APIKey:             "sk-test",
		BaseURL:            mock.URL,
		Store:              true,
		PromptCacheKey:     "session-stream",
		PreviousResponseID: "resp_prev",
	}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.ResponseID != "chatcmpl_stream_1" {
		t.Fatalf("unexpected response id: %q", turn.ResponseID)
	}
	if strings.TrimSpace(turn.Text) != "hello stream" {
		t.Fatalf("unexpected reply: %q", turn.Text)
	}
	if store, _ := requestBody["store"].(bool); !store {
		t.Fatalf("expected stream request store=true, got=%#v", requestBody["store"])
	}
	if got, _ := requestBody["prompt_cache_key"].(string); got != "session-stream" {
		t.Fatalf("unexpected stream prompt_cache_key: %q", got)
	}
	if got, _ := requestBody["previous_response_id"].(string); got != "resp_prev" {
		t.Fatalf("unexpected stream previous_response_id: %q", got)
	}
}

func TestGenerateReplyCodexCompatibleSuccess(t *testing.T) {
	t.Parallel()
	var auth string
	var model string
	var stream bool
	var store bool
	var reasoningEffort string
	var promptCacheKey string
	var previousResponseID string

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		model, _ = req["model"].(string)
		stream, _ = req["stream"].(bool)
		store, _ = req["store"].(bool)
		if rawReasoning, ok := req["reasoning"].(map[string]interface{}); ok {
			reasoningEffort, _ = rawReasoning["effort"].(string)
		}
		promptCacheKey, _ = req["prompt_cache_key"].(string)
		previousResponseID, _ = req["previous_response_id"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello \"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"from codex\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"}}\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	got, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID:         ProviderCodex,
		Model:              "gpt-5-codex",
		APIKey:             "sk-test",
		BaseURL:            mock.URL,
		ReasoningEffort:    "high",
		Store:              true,
		PromptCacheKey:     "session-1",
		PreviousResponseID: "resp_prev",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello from codex" {
		t.Fatalf("unexpected reply: %s", got)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("unexpected auth header: %s", auth)
	}
	if model != "gpt-5-codex" {
		t.Fatalf("unexpected model: %s", model)
	}
	if !stream {
		t.Fatalf("expected stream=true for codex-compatible request")
	}
	if !store {
		t.Fatalf("expected store=true for codex-compatible request")
	}
	if reasoningEffort != "high" {
		t.Fatalf("expected reasoning.effort=high for codex-compatible request, got=%q", reasoningEffort)
	}
	if promptCacheKey != "session-1" {
		t.Fatalf("unexpected prompt_cache_key: %q", promptCacheKey)
	}
	if previousResponseID != "resp_prev" {
		t.Fatalf("unexpected previous_response_id: %q", previousResponseID)
	}
}

func TestGenerateTurnCodexCompatibleCapturesResponseID(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_2\"}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_2\"}}\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderCodex,
		Model:      "gpt-5-codex",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.ResponseID != "resp_2" {
		t.Fatalf("expected response id resp_2, got=%q", turn.ResponseID)
	}
	if strings.TrimSpace(turn.Text) != "hello" {
		t.Fatalf("unexpected text: %q", turn.Text)
	}
}

func TestGenerateTurnOpenAIToolCalls(t *testing.T) {
	t.Parallel()
	var requestBody map[string]interface{}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"view","arguments":"{\"path\":\"docs/contracts.md\",\"start\":1,\"end\":5}"}}]}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "view docs/contracts.md lines 1-5"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, []ToolDefinition{
		{
			Name: "view",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
					"start": map[string]interface{}{
						"type": "integer",
					},
					"end": map[string]interface{}{
						"type": "integer",
					},
				},
				"required": []string{"path", "start", "end"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got=%d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "view" {
		t.Fatalf("unexpected tool name: %q", turn.ToolCalls[0].Name)
	}
	if got := turn.ToolCalls[0].Arguments["path"]; got != "docs/contracts.md" {
		t.Fatalf("unexpected tool argument path: %#v", got)
	}
	if got := turn.ToolCalls[0].Arguments["start"]; got != float64(1) {
		t.Fatalf("unexpected tool argument start: %#v", got)
	}

	rawTools, ok := requestBody["tools"].([]interface{})
	if !ok || len(rawTools) != 1 {
		t.Fatalf("expected one tool definition in request, got=%#v", requestBody["tools"])
	}
}

func TestGenerateTurnCodexCompatibleParsesFunctionCalls(t *testing.T) {
	t.Parallel()
	var requestBody map[string]interface{}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"view\",\"arguments\":\"{\\\"path\\\":\\\"docs/contracts.md\\\",\\\"start\\\":1}\"}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"}}\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "view docs/contracts.md"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderCodex,
		Model:      "gpt-5-codex",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, []ToolDefinition{
		{
			Name: "view",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
					"start": map[string]interface{}{
						"type": "integer",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got=%d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "view" {
		t.Fatalf("unexpected tool name: %q", turn.ToolCalls[0].Name)
	}
	if got := turn.ToolCalls[0].Arguments["path"]; got != "docs/contracts.md" {
		t.Fatalf("unexpected tool argument path: %#v", got)
	}
	if got := turn.ToolCalls[0].Arguments["start"]; got != float64(1) {
		t.Fatalf("unexpected tool argument start: %#v", got)
	}

	rawTools, ok := requestBody["tools"].([]interface{})
	if !ok || len(rawTools) != 1 {
		t.Fatalf("expected one tool definition in request, got=%#v", requestBody["tools"])
	}
	firstTool, _ := rawTools[0].(map[string]interface{})
	if firstTool["name"] != "view" {
		t.Fatalf("expected codex tool name=view, got=%#v", firstTool["name"])
	}
}

func TestGenerateTurnOpenAIInvalidToolArgumentsReturnsRecoverableError(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_view","type":"function","function":{"name":"view","arguments":"{\"items\":[{\"path\":\"/tmp/a\",\"start\":1,\"end\":3}]"}}]}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	_, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "view /tmp/a"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, []ToolDefinition{{Name: "view"}})
	assertRunnerCode(t, err, ErrorCodeProviderInvalidReply)

	invalid, ok := InvalidToolCallFromError(err)
	if !ok {
		t.Fatalf("expected InvalidToolCallError, got=%T (%v)", err, err)
	}
	if invalid.Name != "view" {
		t.Fatalf("unexpected tool name: %q", invalid.Name)
	}
	if invalid.CallID != "call_view" {
		t.Fatalf("unexpected call id: %q", invalid.CallID)
	}
	if !strings.Contains(invalid.ArgumentsRaw, `{"items":[{"path":"/tmp/a","start":1,"end":3}]`) {
		t.Fatalf("unexpected raw arguments: %q", invalid.ArgumentsRaw)
	}
	if invalid.Err == nil || !strings.Contains(invalid.Err.Error(), "unexpected end of JSON input") {
		t.Fatalf("unexpected parse error: %#v", invalid.Err)
	}
}

func TestGenerateTurnSerializesAssistantToolMessages(t *testing.T) {
	t.Parallel()
	payloadCh := make(chan map[string]interface{}, 1)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		payloadCh <- req
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	_, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role:    "user",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
			},
			{
				Role:    "assistant",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: "calling tool"}},
				Metadata: map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{
							"id":   "call_abc",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "shell",
								"arguments": "{\"command\":\"pwd\"}",
							},
						},
					},
				},
			},
			{
				Role:    "tool",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: "ok"}},
				Metadata: map[string]interface{}{
					"tool_call_id": "call_abc",
					"name":         "shell",
				},
			},
		},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, []ToolDefinition{{Name: "shell"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]interface{}
	select {
	case payload = <-payloadCh:
	default:
		t.Fatal("provider payload not captured")
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) < 3 {
		t.Fatalf("unexpected request messages: %#v", payload["messages"])
	}
	assistant, _ := messages[1].(map[string]interface{})
	if _, ok := assistant["tool_calls"]; !ok {
		t.Fatalf("assistant tool_calls missing: %#v", assistant)
	}
	toolMsg, _ := messages[2].(map[string]interface{})
	if toolMsg["tool_call_id"] != "call_abc" {
		t.Fatalf("unexpected tool_call_id: %#v", toolMsg["tool_call_id"])
	}
}

func TestGenerateTurnStreamOpenAISendsNativeDeltas(t *testing.T) {
	t.Parallel()
	var requestBody map[string]interface{}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	var streamed []string
	turn, err := r.GenerateTurnStream(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, nil, func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.Text != "hello" {
		t.Fatalf("unexpected turn text: %q", turn.Text)
	}
	if got := strings.Join(streamed, ""); got != "hello" {
		t.Fatalf("unexpected streamed deltas: %q", got)
	}
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got=%d", len(turn.ToolCalls))
	}
	if got, ok := requestBody["stream"].(bool); !ok || !got {
		t.Fatalf("expected stream=true in request, got=%#v", requestBody["stream"])
	}
}

func TestGenerateTurnStreamOpenAIIgnoresEmptyDataHeartbeat(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data:\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurnStream(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "heartbeat test"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.Text != "ok" {
		t.Fatalf("unexpected turn text: %q", turn.Text)
	}
}

func TestGenerateTurnStreamOpenAIIgnoresBracketControlToken(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: [PING]\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurnStream(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "control token test"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.Text != "ok" {
		t.Fatalf("unexpected turn text: %q", turn.Text)
	}
}

func TestGenerateTurnStreamOpenAIAggregatesToolCalls(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"shell\",\"arguments\":\"{\\\"command\\\":\\\"ec\"}}]}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"ho hi\\\"}\"}}]}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	turn, err := r.GenerateTurnStream(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "say hi"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, []ToolDefinition{{Name: "shell"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.Text != "" {
		t.Fatalf("expected empty text, got=%q", turn.Text)
	}
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got=%d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "shell" {
		t.Fatalf("unexpected tool name: %q", turn.ToolCalls[0].Name)
	}
	if got := turn.ToolCalls[0].Arguments["command"]; got != "echo hi" {
		t.Fatalf("unexpected tool argument command: %#v", got)
	}
}

func TestGenerateTurnStreamOpenAITimeoutMappedToRequestFailed(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		time.Sleep(120 * time.Millisecond)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond})
	_, err := r.GenerateTurnStream(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "timeout test"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, nil, nil)
	assertRunnerCode(t, err, ErrorCodeProviderRequestFailed)
}

func TestGenerateTurnStreamCodexCompatibleFallsBackToMessageOutputItem(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello from done item\"}]}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"}}\n\n")
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	var streamed []string
	turn, err := r.GenerateTurnStream(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderCodex,
		Model:      "gpt-5-codex",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	}, nil, func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.Text != "hello from done item" {
		t.Fatalf("unexpected turn text: %q", turn.Text)
	}
	if got := strings.Join(streamed, ""); got != "hello from done item" {
		t.Fatalf("unexpected streamed deltas: %q", got)
	}
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got=%d", len(turn.ToolCalls))
	}
}

func assertRunnerCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %s, got nil", want)
	}
	var rerr *RunnerError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected RunnerError, got: %T (%v)", err, err)
	}
	if rerr.Code != want {
		t.Fatalf("unexpected error code: got=%s want=%s", rerr.Code, want)
	}
}

func TestGenerateTurnFiltersToolCallsAndReasoningByCapability(t *testing.T) {
	t.Parallel()

	adapter := &capabilityProbeAdapter{
		id: "cap-probe-no-tool",
		capabilities: ProviderCapabilities{
			Stream:      false,
			ToolCall:    false,
			Attachments: false,
			Reasoning:   false,
		},
		reply: "ok",
	}

	r := New()
	r.registerAdapter(adapter)

	_, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID:      "custom-no-tool",
		Model:           "m1",
		AdapterID:       adapter.id,
		ReasoningEffort: "high",
	}, []ToolDefinition{{Name: "view"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adapter.lastTools) != 0 {
		t.Fatalf("expected tool definitions to be filtered, got=%d", len(adapter.lastTools))
	}
	if adapter.lastCfg.ReasoningEffort != "" {
		t.Fatalf("expected reasoning effort stripped by capability, got=%q", adapter.lastCfg.ReasoningEffort)
	}
}

func TestGenerateTurnStreamFallsBackWhenStreamCapabilityDisabled(t *testing.T) {
	t.Parallel()

	adapter := &capabilityProbeAdapter{
		id: "cap-probe-no-stream",
		capabilities: ProviderCapabilities{
			Stream:      false,
			ToolCall:    true,
			Attachments: false,
			Reasoning:   true,
		},
		reply: "stream fallback",
	}

	r := New()
	r.registerAdapter(adapter)

	streamed := make([]string, 0, 1)
	turn, err := r.GenerateTurnStream(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: "custom-no-stream",
		Model:      "m1",
		AdapterID:  adapter.id,
	}, nil, func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.Text != "stream fallback" {
		t.Fatalf("unexpected turn text: %q", turn.Text)
	}
	if got := strings.Join(streamed, ""); got != "stream fallback" {
		t.Fatalf("unexpected fallback streamed delta: %q", got)
	}
}

func TestGenerateTurnRejectsAttachmentsWhenCapabilityDisabled(t *testing.T) {
	t.Parallel()

	r := New()
	_, err := r.GenerateTurn(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "image", Text: "inline-image"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderDemo,
		Model:      "demo-chat",
	}, nil)
	assertRunnerCode(t, err, ErrorCodeProviderNotSupported)
}

type capabilityProbeAdapter struct {
	id           string
	capabilities ProviderCapabilities
	reply        string

	lastCfg   GenerateConfig
	lastTools []ToolDefinition
}

func (a *capabilityProbeAdapter) ID() string {
	return a.id
}

func (a *capabilityProbeAdapter) Capabilities() ProviderCapabilities {
	return a.capabilities
}

func (a *capabilityProbeAdapter) GenerateTurn(
	_ context.Context,
	_ domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	_ *Runner,
) (TurnResult, error) {
	a.lastCfg = cfg
	if len(tools) == 0 {
		a.lastTools = nil
	} else {
		a.lastTools = append([]ToolDefinition{}, tools...)
	}
	return TurnResult{Text: a.reply}, nil
}

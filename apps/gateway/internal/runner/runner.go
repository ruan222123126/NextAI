package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
)

const (
	ProviderDemo   = "demo"
	ProviderOpenAI = "openai"
	ProviderCodex  = "codex-compatible"

	defaultOpenAIBaseURL = "https://api.openai.com/v1"

	ErrorCodeProviderNotConfigured = "provider_not_configured"
	ErrorCodeProviderNotSupported  = "provider_not_supported"
	ErrorCodeProviderRequestFailed = "provider_request_failed"
	ErrorCodeProviderInvalidReply  = "provider_invalid_reply"
)

type RunnerError struct {
	Code    string
	Message string
	Err     error
}

type InvalidToolCallError struct {
	Index        int
	CallID       string
	Name         string
	ArgumentsRaw string
	Err          error
}

func (e *RunnerError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e *RunnerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *InvalidToolCallError) Error() string {
	if e == nil {
		return ""
	}
	name := strings.TrimSpace(e.Name)
	detail := "invalid arguments"
	if e.Err != nil {
		detail = e.Err.Error()
	}
	if name != "" {
		return fmt.Sprintf("provider tool call %q has invalid arguments: %s", name, detail)
	}
	return fmt.Sprintf("provider tool call[%d] has invalid arguments: %s", e.Index, detail)
}

func (e *InvalidToolCallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func InvalidToolCallFromError(err error) (*InvalidToolCallError, bool) {
	var invalidErr *InvalidToolCallError
	if !errors.As(err, &invalidErr) || invalidErr == nil {
		return nil, false
	}
	return invalidErr, true
}

type GenerateConfig struct {
	ProviderID         string
	Model              string
	APIKey             string
	BaseURL            string
	AdapterID          string
	Headers            map[string]string
	TimeoutMS          int
	ReasoningEffort    string
	Store              bool
	PromptCacheKey     string
	PreviousResponseID string
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
}

type TurnResult struct {
	Text       string
	ToolCalls  []ToolCall
	ResponseID string
}

type ProviderAdapter interface {
	ID() string
	GenerateTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition, runner *Runner) (TurnResult, error)
}

type StreamProviderAdapter interface {
	ProviderAdapter
	GenerateTurnStream(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition, runner *Runner, onDelta func(string)) (TurnResult, error)
}

type Runner struct {
	httpClient *http.Client
	adapters   map[string]ProviderAdapter
}

func New() *Runner {
	return NewWithHTTPClient(&http.Client{})
}

func NewWithHTTPClient(client *http.Client) *Runner {
	if client == nil {
		client = &http.Client{}
	}
	r := &Runner{
		httpClient: client,
		adapters:   map[string]ProviderAdapter{},
	}
	r.registerAdapter(&demoAdapter{})
	r.registerAdapter(&openAICompatibleAdapter{})
	r.registerAdapter(&codexCompatibleAdapter{})
	return r
}

func (r *Runner) registerAdapter(adapter ProviderAdapter) {
	if adapter == nil {
		return
	}
	id := strings.TrimSpace(adapter.ID())
	if id == "" {
		return
	}
	r.adapters[id] = adapter
}

func (r *Runner) GenerateTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition) (TurnResult, error) {
	providerID := strings.ToLower(strings.TrimSpace(cfg.ProviderID))
	if providerID == "" {
		providerID = ProviderDemo
	}

	adapterID := strings.TrimSpace(cfg.AdapterID)
	if adapterID == "" {
		adapterID = defaultAdapterForProvider(providerID)
	}
	if adapterID == "" {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("provider %q is not supported", providerID),
		}
	}

	if adapterID != provider.AdapterDemo && strings.TrimSpace(cfg.Model) == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "model is required for active provider"}
	}

	adapter, ok := r.adapters[adapterID]
	if !ok {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("adapter %q is not supported", adapterID),
		}
	}
	return adapter.GenerateTurn(ctx, req, cfg, tools, r)
}

func (r *Runner) GenerateReply(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig) (string, error) {
	turn, err := r.GenerateTurn(ctx, req, cfg, nil)
	if err != nil {
		return "", err
	}
	if len(turn.ToolCalls) > 0 {
		return "", &RunnerError{Code: ErrorCodeProviderInvalidReply, Message: "provider response contains tool calls but tool support is disabled"}
	}
	text := strings.TrimSpace(turn.Text)
	if text == "" {
		return "", &RunnerError{Code: ErrorCodeProviderInvalidReply, Message: "provider response has empty content"}
	}
	return text, nil
}

func (r *Runner) GenerateTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	onDelta func(string),
) (TurnResult, error) {
	providerID := strings.ToLower(strings.TrimSpace(cfg.ProviderID))
	if providerID == "" {
		providerID = ProviderDemo
	}

	adapterID := strings.TrimSpace(cfg.AdapterID)
	if adapterID == "" {
		adapterID = defaultAdapterForProvider(providerID)
	}
	if adapterID == "" {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("provider %q is not supported", providerID),
		}
	}

	if adapterID != provider.AdapterDemo && strings.TrimSpace(cfg.Model) == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "model is required for active provider"}
	}

	adapter, ok := r.adapters[adapterID]
	if !ok {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("adapter %q is not supported", adapterID),
		}
	}

	if streamAdapter, ok := adapter.(StreamProviderAdapter); ok {
		return streamAdapter.GenerateTurnStream(ctx, req, cfg, tools, r, onDelta)
	}

	turn, err := adapter.GenerateTurn(ctx, req, cfg, tools, r)
	if err != nil {
		return TurnResult{}, err
	}
	if onDelta != nil && turn.Text != "" {
		onDelta(turn.Text)
	}
	return turn, nil
}

type demoAdapter struct{}

func (a *demoAdapter) ID() string {
	return provider.AdapterDemo
}

func (a *demoAdapter) GenerateTurn(_ context.Context, req domain.AgentProcessRequest, _ GenerateConfig, _ []ToolDefinition, _ *Runner) (TurnResult, error) {
	return TurnResult{Text: generateDemoReply(req)}, nil
}

type openAICompatibleAdapter struct{}

func (a *openAICompatibleAdapter) ID() string {
	return provider.AdapterOpenAICompatible
}

func (a *openAICompatibleAdapter) GenerateTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition, runner *Runner) (TurnResult, error) {
	return runner.generateOpenAICompatibleTurn(ctx, req, cfg, tools)
}

func (a *openAICompatibleAdapter) GenerateTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	runner *Runner,
	onDelta func(string),
) (TurnResult, error) {
	return runner.generateOpenAICompatibleTurnStream(ctx, req, cfg, tools, onDelta)
}

type codexCompatibleAdapter struct{}

func (a *codexCompatibleAdapter) ID() string {
	return provider.AdapterCodexCompatible
}

func (a *codexCompatibleAdapter) GenerateTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition, runner *Runner) (TurnResult, error) {
	return runner.generateCodexCompatibleTurn(ctx, req, cfg, tools)
}

func (a *codexCompatibleAdapter) GenerateTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	runner *Runner,
	onDelta func(string),
) (TurnResult, error) {
	return runner.generateCodexCompatibleTurnStream(ctx, req, cfg, tools, onDelta)
}

func defaultAdapterForProvider(providerID string) string {
	switch providerID {
	case "", ProviderDemo:
		return provider.AdapterDemo
	case ProviderOpenAI:
		return provider.AdapterOpenAICompatible
	case ProviderCodex:
		return provider.AdapterCodexCompatible
	}
	if provider.IsCodexCompatibleProviderID(providerID) {
		return provider.AdapterCodexCompatible
	}
	if strings.HasPrefix(providerID, provider.AdapterOpenAICompatible) {
		return provider.AdapterOpenAICompatible
	}
	return ""
}

func generateDemoReply(req domain.AgentProcessRequest) string {
	parts := make([]string, 0, len(req.Input))
	for _, msg := range req.Input {
		if msg.Role != "user" {
			continue
		}
		for _, c := range msg.Content {
			if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, strings.TrimSpace(c.Text))
			}
		}
	}
	if len(parts) == 0 {
		return "Echo: (empty input)"
	}
	return "Echo: " + strings.Join(parts, " ")
}

func shouldApplyOpenAICompatibleCache(cfg GenerateConfig) bool {
	if !cfg.Store {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(cfg.ProviderID), ProviderOpenAI)
}

func applyOpenAICompatibleCacheConfig(payload *openAIChatRequest, cfg GenerateConfig) {
	if payload == nil || !shouldApplyOpenAICompatibleCache(cfg) {
		return
	}
	payload.Store = true
	payload.PromptCacheKey = strings.TrimSpace(cfg.PromptCacheKey)
	payload.PreviousResponseID = strings.TrimSpace(cfg.PreviousResponseID)
}

func applyReasoningEffort(payload *openAIChatRequest, cfg GenerateConfig) {
	if payload == nil {
		return
	}
	payload.ReasoningEffort = normalizeReasoningEffort(cfg.ReasoningEffort)
}

func (r *Runner) generateOpenAICompatibleTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition) (TurnResult, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "provider api_key is required"}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}

	payload := openAIChatRequest{
		Model:    cfg.Model,
		Messages: toOpenAIMessages(req.Input),
		Tools:    toOpenAITools(tools),
	}
	applyReasoningEffort(&payload, cfg)
	applyOpenAICompatibleCacheConfig(&payload, cfg)
	if len(payload.Messages) == 0 {
		return TurnResult{Text: generateDemoReply(req)}, nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to encode provider request",
			Err:     err,
		}
	}

	requestCtx := ctx
	cancel := func() {}
	if cfg.TimeoutMS > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to create provider request",
			Err:     err,
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range cfg.Headers {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "provider request failed",
			Err:     err,
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to read provider response",
			Err:     err,
		}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: fmt.Sprintf("provider returned status %d", resp.StatusCode),
		}
	}

	var completion openAIChatResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response is not valid json",
			Err:     err,
		}
	}
	if len(completion.Choices) == 0 {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has no choices",
		}
	}

	message := completion.Choices[0].Message
	text := strings.TrimSpace(extractOpenAIContent(message.Content))
	toolCalls, err := parseOpenAIToolCalls(message.ToolCalls)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: err.Error(),
			Err:     err,
		}
	}
	if text == "" && len(toolCalls) == 0 {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has empty content",
		}
	}

	return TurnResult{
		Text:       text,
		ToolCalls:  toolCalls,
		ResponseID: strings.TrimSpace(completion.ID),
	}, nil
}

func (r *Runner) generateOpenAICompatibleTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	onDelta func(string),
) (TurnResult, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "provider api_key is required"}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}

	payload := openAIChatRequest{
		Model:    cfg.Model,
		Messages: toOpenAIMessages(req.Input),
		Tools:    toOpenAITools(tools),
		Stream:   true,
	}
	applyReasoningEffort(&payload, cfg)
	applyOpenAICompatibleCacheConfig(&payload, cfg)
	if len(payload.Messages) == 0 {
		return TurnResult{Text: generateDemoReply(req)}, nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to encode provider request",
			Err:     err,
		}
	}

	requestCtx := ctx
	cancel := func() {}
	if cfg.TimeoutMS > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to create provider request",
			Err:     err,
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for key, value := range cfg.Headers {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "provider request failed",
			Err:     err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: fmt.Sprintf("provider returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))),
		}
	}

	var replyBuilder strings.Builder
	toolCalls := map[int]*openAIToolCall{}
	responseID := ""
	processData := func(data string) error {
		if isSSEControlToken(data) {
			return nil
		}
		var chunk openAIChatStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("provider stream chunk is not valid json: %w; payload=%q", err, truncateText(data, 512))
		}
		if id := strings.TrimSpace(chunk.ID); id != "" {
			responseID = id
		}
		if len(chunk.Choices) == 0 {
			return nil
		}
		for _, choice := range chunk.Choices {
			delta := extractOpenAIDeltaContent(choice.Delta.Content)
			if delta != "" {
				replyBuilder.WriteString(delta)
				if onDelta != nil {
					onDelta(delta)
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				idx := tc.Index
				if idx < 0 {
					idx = 0
				}
				current, ok := toolCalls[idx]
				if !ok {
					current = &openAIToolCall{}
					toolCalls[idx] = current
				}
				if strings.TrimSpace(tc.ID) != "" {
					current.ID = strings.TrimSpace(tc.ID)
				}
				if strings.TrimSpace(tc.Type) != "" {
					current.Type = strings.TrimSpace(tc.Type)
				}
				if strings.TrimSpace(tc.Function.Name) != "" {
					current.Function.Name = strings.TrimSpace(tc.Function.Name)
				}
				if tc.Function.Arguments != "" {
					current.Function.Arguments += tc.Function.Arguments
				}
			}
		}
		return nil
	}

	if err := consumeSSEData(resp.Body, processData); err != nil {
		return TurnResult{}, mapStreamConsumeError(err)
	}

	orderedIndexes := make([]int, 0, len(toolCalls))
	for idx := range toolCalls {
		orderedIndexes = append(orderedIndexes, idx)
	}
	sort.Ints(orderedIndexes)
	aggregatedToolCalls := make([]openAIToolCall, 0, len(orderedIndexes))
	for _, idx := range orderedIndexes {
		tc := toolCalls[idx]
		if tc == nil {
			continue
		}
		aggregatedToolCalls = append(aggregatedToolCalls, *tc)
	}

	parsedToolCalls, err := parseOpenAIToolCalls(aggregatedToolCalls)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: err.Error(),
			Err:     err,
		}
	}

	reply := replyBuilder.String()
	if strings.TrimSpace(reply) == "" && len(parsedToolCalls) == 0 {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has empty content",
		}
	}

	return TurnResult{
		Text:       reply,
		ToolCalls:  parsedToolCalls,
		ResponseID: responseID,
	}, nil
}

func (r *Runner) generateCodexCompatibleTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition) (TurnResult, error) {
	return r.generateCodexCompatibleTurnStream(ctx, req, cfg, tools, nil)
}

func (r *Runner) generateCodexCompatibleTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	onDelta func(string),
) (TurnResult, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "provider api_key is required"}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}

	instructions, inputItems := toCodexResponsesInput(req.Input)
	if len(inputItems) == 0 && strings.TrimSpace(instructions) == "" {
		return TurnResult{Text: generateDemoReply(req)}, nil
	}

	payload := codexResponsesRequest{
		Model:              cfg.Model,
		Instructions:       strings.TrimSpace(instructions),
		PreviousResponseID: strings.TrimSpace(cfg.PreviousResponseID),
		Input:              inputItems,
		Tools:              toCodexTools(tools),
		ToolChoice:         "auto",
		ParallelToolCalls:  false,
		Store:              cfg.Store,
		Stream:             true,
		PromptCacheKey:     strings.TrimSpace(cfg.PromptCacheKey),
	}
	if effort := normalizeReasoningEffort(cfg.ReasoningEffort); effort != "" {
		payload.Reasoning = &codexReasoningConfig{Effort: effort}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to encode provider request",
			Err:     err,
		}
	}

	requestCtx := ctx
	cancel := func() {}
	if cfg.TimeoutMS > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to create provider request",
			Err:     err,
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for key, value := range cfg.Headers {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "provider request failed",
			Err:     err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: fmt.Sprintf("provider returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))),
		}
	}

	var replyBuilder strings.Builder
	messageTexts := make([]string, 0, 2)
	sawDelta := false
	rawToolCalls := make([]codexResponseFunctionCall, 0, 1)
	responseID := ""

	processData := func(data string) error {
		if isSSEControlToken(data) {
			return nil
		}
		var event codexResponsesStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("provider stream chunk is not valid json: %w; payload=%q", err, truncateText(data, 512))
		}

		switch event.Type {
		case "response.created", "response.completed":
			if event.Response != nil {
				if id := strings.TrimSpace(event.Response.ID); id != "" {
					responseID = id
				}
			}
		case "response.output_text.delta":
			delta := event.Delta
			if delta == "" {
				return nil
			}
			sawDelta = true
			replyBuilder.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		case "response.output_item.done":
			switch strings.TrimSpace(event.Item.Type) {
			case "message":
				if sawDelta {
					return nil
				}
				text := extractCodexMessageContent(event.Item.Content)
				if text != "" {
					messageTexts = append(messageTexts, text)
				}
			case "function_call":
				rawToolCalls = append(rawToolCalls, codexResponseFunctionCall{
					CallID:    strings.TrimSpace(event.Item.CallID),
					Name:      strings.TrimSpace(event.Item.Name),
					Arguments: strings.TrimSpace(event.Item.Arguments),
				})
			}
		case "response.failed":
			message := ""
			if event.Response != nil && event.Response.Error != nil {
				message = strings.TrimSpace(event.Response.Error.Message)
			}
			if message == "" {
				message = "provider returned response.failed"
			}
			return errors.New(message)
		}
		return nil
	}

	if err := consumeSSEData(resp.Body, processData); err != nil {
		return TurnResult{}, mapStreamConsumeError(err)
	}

	toolCalls, err := parseCodexToolCalls(rawToolCalls)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: err.Error(),
			Err:     err,
		}
	}

	reply := replyBuilder.String()
	if strings.TrimSpace(reply) == "" && len(messageTexts) > 0 {
		reply = strings.Join(messageTexts, "\n")
		if onDelta != nil && !sawDelta {
			onDelta(reply)
		}
	}

	if strings.TrimSpace(reply) == "" && len(toolCalls) == 0 {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has empty content",
		}
	}

	return TurnResult{Text: reply, ToolCalls: toolCalls, ResponseID: responseID}, nil
}

func toCodexResponsesInput(input []domain.AgentInputMessage) (string, []codexResponsesInputItem) {
	instructions := make([]string, 0, 1)
	out := make([]codexResponsesInputItem, 0, len(input))
	for _, msg := range input {
		role := normalizeRole(msg.Role)
		content := strings.TrimSpace(flattenText(msg.Content))
		switch role {
		case "system":
			if content != "" {
				instructions = append(instructions, content)
			}
		case "assistant":
			if content != "" {
				out = append(out, codexResponsesInputItem{
					Type:    "message",
					Role:    role,
					Content: []codexResponseContentItem{{Type: "output_text", Text: content}},
				})
			}
			toolCalls := parseToolCallsFromMetadata(msg.Metadata)
			for _, call := range toolCalls {
				arguments := strings.TrimSpace(call.Function.Arguments)
				if arguments == "" {
					arguments = "{}"
				}
				out = append(out, codexResponsesInputItem{
					Type:      "function_call",
					CallID:    strings.TrimSpace(call.ID),
					Name:      strings.TrimSpace(call.Function.Name),
					Arguments: arguments,
				})
			}
		case "tool":
			callID := metadataString(msg.Metadata, "tool_call_id")
			if callID == "" {
				continue
			}
			output := content
			out = append(out, codexResponsesInputItem{
				Type:   "function_call_output",
				CallID: callID,
				Output: &output,
			})
		default:
			if content == "" {
				continue
			}
			out = append(out, codexResponsesInputItem{
				Type:    "message",
				Role:    role,
				Content: []codexResponseContentItem{{Type: "input_text", Text: content}},
			})
		}
	}
	return strings.Join(instructions, "\n\n"), out
}

func toCodexTools(tools []ToolDefinition) []codexToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	out := make([]codexToolDefinition, 0, len(tools))
	for _, item := range tools {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		out = append(out, codexToolDefinition{
			Type:        "function",
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			Parameters:  normalizeToolParameters(item.Parameters),
		})
	}
	return out
}

func parseCodexToolCalls(in []codexResponseFunctionCall) ([]ToolCall, error) {
	if len(in) == 0 {
		return nil, nil
	}
	calls := make([]openAIToolCall, 0, len(in))
	for _, item := range in {
		calls = append(calls, openAIToolCall{
			ID:   item.CallID,
			Type: "function",
			Function: openAIFunctionCall{
				Name:      item.Name,
				Arguments: item.Arguments,
			},
		})
	}
	return parseOpenAIToolCalls(calls)
}

func extractCodexMessageContent(content []codexResponseContentItem) string {
	if len(content) == 0 {
		return ""
	}
	parts := make([]string, 0, len(content))
	for _, item := range content {
		switch strings.TrimSpace(item.Type) {
		case "output_text", "input_text", "text":
			text := strings.TrimSpace(item.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

type codexResponsesRequest struct {
	Model              string                    `json:"model"`
	Instructions       string                    `json:"instructions,omitempty"`
	PreviousResponseID string                    `json:"previous_response_id,omitempty"`
	Input              []codexResponsesInputItem `json:"input"`
	Tools              []codexToolDefinition     `json:"tools,omitempty"`
	Reasoning          *codexReasoningConfig     `json:"reasoning,omitempty"`
	ToolChoice         string                    `json:"tool_choice,omitempty"`
	ParallelToolCalls  bool                      `json:"parallel_tool_calls"`
	Store              bool                      `json:"store"`
	Stream             bool                      `json:"stream"`
	PromptCacheKey     string                    `json:"prompt_cache_key,omitempty"`
}

type codexReasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

type codexResponsesInputItem struct {
	Type      string                     `json:"type"`
	Role      string                     `json:"role,omitempty"`
	Content   []codexResponseContentItem `json:"content,omitempty"`
	CallID    string                     `json:"call_id,omitempty"`
	Name      string                     `json:"name,omitempty"`
	Arguments string                     `json:"arguments,omitempty"`
	Output    *string                    `json:"output,omitempty"`
}

type codexToolDefinition struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type codexResponsesStreamEvent struct {
	Type     string                    `json:"type"`
	Delta    string                    `json:"delta,omitempty"`
	Item     codexResponseOutputItem   `json:"item,omitempty"`
	Response *codexResponseEventStatus `json:"response,omitempty"`
}

type codexResponseEventStatus struct {
	ID    string                   `json:"id,omitempty"`
	Error *codexResponseEventError `json:"error,omitempty"`
}

type codexResponseEventError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type codexResponseOutputItem struct {
	Type      string                     `json:"type"`
	Role      string                     `json:"role,omitempty"`
	Content   []codexResponseContentItem `json:"content,omitempty"`
	CallID    string                     `json:"call_id,omitempty"`
	Name      string                     `json:"name,omitempty"`
	Arguments string                     `json:"arguments,omitempty"`
}

type codexResponseContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type codexResponseFunctionCall struct {
	CallID    string
	Name      string
	Arguments string
}

type openAIChatRequest struct {
	Model              string                 `json:"model"`
	Messages           []openAIMessage        `json:"messages"`
	Tools              []openAIToolDefinition `json:"tools,omitempty"`
	ReasoningEffort    string                 `json:"reasoning_effort,omitempty"`
	Stream             bool                   `json:"stream,omitempty"`
	Store              bool                   `json:"store,omitempty"`
	PromptCacheKey     string                 `json:"prompt_cache_key,omitempty"`
	PreviousResponseID string                 `json:"previous_response_id,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    interface{}      `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolDefinition struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type openAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

type openAIChatResponse struct {
	ID      string `json:"id,omitempty"`
	Choices []struct {
		Message struct {
			Content   json.RawMessage  `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
}

type openAIChatStreamResponse struct {
	ID      string `json:"id,omitempty"`
	Choices []struct {
		Delta struct {
			Content   json.RawMessage        `json:"content"`
			ToolCalls []openAIStreamToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
	} `json:"choices"`
}

type openAIStreamToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

func toOpenAIMessages(input []domain.AgentInputMessage) []openAIMessage {
	out := make([]openAIMessage, 0, len(input))
	for _, msg := range input {
		role := normalizeRole(msg.Role)
		content := strings.TrimSpace(flattenText(msg.Content))

		switch role {
		case "assistant":
			toolCalls := parseToolCallsFromMetadata(msg.Metadata)
			item := openAIMessage{Role: role}
			if content != "" {
				item.Content = content
			}
			if len(toolCalls) > 0 {
				item.ToolCalls = toolCalls
			}
			if item.Content == nil && len(item.ToolCalls) == 0 {
				continue
			}
			out = append(out, item)
		case "tool":
			item := openAIMessage{
				Role:    role,
				Content: content,
			}
			if item.Content == nil {
				item.Content = ""
			}
			if toolCallID := metadataString(msg.Metadata, "tool_call_id"); toolCallID != "" {
				item.ToolCallID = toolCallID
			}
			if name := metadataString(msg.Metadata, "name"); name != "" {
				item.Name = name
			}
			out = append(out, item)
		default:
			if content == "" {
				continue
			}
			out = append(out, openAIMessage{Role: role, Content: content})
		}
	}
	return out
}

func toOpenAITools(tools []ToolDefinition) []openAIToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openAIToolDefinition, 0, len(tools))
	for _, item := range tools {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		params := normalizeToolParameters(item.Parameters)
		out = append(out, openAIToolDefinition{
			Type: "function",
			Function: openAIToolFunction{
				Name:        name,
				Description: strings.TrimSpace(item.Description),
				Parameters:  params,
			},
		})
	}
	return out
}

func parseOpenAIToolCalls(in []openAIToolCall) ([]ToolCall, error) {
	if len(in) == 0 {
		return nil, nil
	}
	calls := make([]ToolCall, 0, len(in))
	for i, item := range in {
		name := strings.TrimSpace(item.Function.Name)
		if name == "" {
			return nil, fmt.Errorf("provider tool call[%d] name is empty", i)
		}
		callID := strings.TrimSpace(item.ID)
		if callID == "" {
			callID = fmt.Sprintf("call_%d", i+1)
		}
		argumentsRaw := strings.TrimSpace(item.Function.Arguments)
		if argumentsRaw == "" {
			argumentsRaw = "{}"
		}
		var arguments map[string]interface{}
		if err := json.Unmarshal([]byte(argumentsRaw), &arguments); err != nil {
			return nil, &InvalidToolCallError{
				Index:        i,
				CallID:       callID,
				Name:         name,
				ArgumentsRaw: argumentsRaw,
				Err:          err,
			}
		}
		if arguments == nil {
			arguments = map[string]interface{}{}
		}
		calls = append(calls, ToolCall{ID: callID, Name: name, Arguments: arguments})
	}
	return calls, nil
}

func parseToolCallsFromMetadata(metadata map[string]interface{}) []openAIToolCall {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["tool_calls"]
	if !ok || raw == nil {
		return nil
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []openAIToolCall
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil
	}
	valid := make([]openAIToolCall, 0, len(out))
	for _, call := range out {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		if strings.TrimSpace(call.ID) == "" {
			continue
		}
		args := strings.TrimSpace(call.Function.Arguments)
		if args == "" {
			call.Function.Arguments = "{}"
		}
		if strings.TrimSpace(call.Type) == "" {
			call.Type = "function"
		}
		valid = append(valid, call)
	}
	return valid
}

func metadataString(metadata map[string]interface{}, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func normalizeReasoningEffort(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func truncateText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "...(truncated)"
}

func normalizeToolParameters(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf, &out); err != nil {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	return out
}

func flattenText(content []domain.RuntimeContent) string {
	parts := make([]string, 0, len(content))
	for _, c := range content {
		if c.Type != "text" {
			continue
		}
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "assistant", "user", "tool":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return "user"
	}
}

func extractOpenAIContent(raw json.RawMessage) string {
	var direct string
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct
	}
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		parts := make([]string, 0, len(arr))
		for _, item := range arr {
			if item.Type != "text" {
				continue
			}
			text := strings.TrimSpace(item.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func extractOpenAIDeltaContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var direct string
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct
	}
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		var out strings.Builder
		for _, item := range arr {
			if item.Type != "text" || item.Text == "" {
				continue
			}
			out.WriteString(item.Text)
		}
		return out.String()
	}
	return ""
}

func consumeSSEData(reader io.Reader, onData func(string) error) error {
	if reader == nil {
		return fmt.Errorf("stream reader is nil")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	dataLines := make([]string, 0, 4)
	flushBlock := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if payload == "" {
			return nil
		}
		if onData == nil {
			return nil
		}
		return onData(payload)
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := flushBlock(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		dataLines = append(dataLines, payload)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := flushBlock(); err != nil {
		return err
	}
	return nil
}

func isSSEControlToken(data string) bool {
	token := strings.TrimSpace(data)
	if token == "" {
		return true
	}
	if strings.EqualFold(token, "[DONE]") {
		return true
	}
	if len(token) < 2 || token[0] != '[' || token[len(token)-1] != ']' {
		return false
	}
	inner := strings.TrimSpace(token[1 : len(token)-1])
	if inner == "" {
		return true
	}
	for _, r := range inner {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func mapStreamConsumeError(err error) *RunnerError {
	if isStreamReadTimeout(err) {
		return &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "provider stream request failed",
			Err:     err,
		}
	}
	return &RunnerError{
		Code:    ErrorCodeProviderInvalidReply,
		Message: "provider stream response is invalid",
		Err:     err,
	}
}

func isStreamReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "client.timeout")
}

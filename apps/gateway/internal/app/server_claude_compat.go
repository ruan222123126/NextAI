package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"nextai/apps/gateway/internal/domain"
)

const (
	claudeCompatDefaultUserID  = "claude-compat-user"
	claudeCompatDefaultChannel = "console"
)

type claudeCompatMessagesRequest struct {
	Model     string                   `json:"model"`
	MaxTokens int                      `json:"max_tokens"`
	Messages  []claudeCompatMessage    `json:"messages"`
	Tools     []map[string]interface{} `json:"tools,omitempty"`
	System    interface{}              `json:"system,omitempty"`
	Metadata  map[string]interface{}   `json:"metadata,omitempty"`
	Stream    bool                     `json:"stream,omitempty"`
}

type claudeCompatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type claudeCompatUsage struct {
	InputTokens              int    `json:"input_tokens"`
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int    `json:"cache_read_input_tokens"`
	OutputTokens             int    `json:"output_tokens"`
	ServiceTier              string `json:"service_tier"`
}

type claudeCompatMessageResponse struct {
	ID           string                   `json:"id"`
	Type         string                   `json:"type"`
	Role         string                   `json:"role"`
	Model        string                   `json:"model"`
	Content      []map[string]interface{} `json:"content"`
	StopReason   string                   `json:"stop_reason"`
	StopSequence interface{}              `json:"stop_sequence"`
	Usage        claudeCompatUsage        `json:"usage"`
}

type claudeCompatCountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

func (s *Server) claudeMessages(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
		return
	}

	var req claudeCompatMessagesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
		return
	}

	if strings.TrimSpace(req.Model) == "" {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if req.MaxTokens <= 0 {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", "max_tokens must be greater than 0")
		return
	}

	input, err := buildClaudeCompatInput(req.System, req.Messages)
	if err != nil {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	userID := strings.TrimSpace(stringValue(req.Metadata["user_id"]))
	if userID == "" {
		userID = claudeCompatDefaultUserID
	}
	sessionID := strings.TrimSpace(stringValue(req.Metadata["session_id"]))
	if sessionID == "" {
		sessionID = newClaudeCompatSessionID()
	}
	channel := strings.ToLower(strings.TrimSpace(stringValue(req.Metadata["channel"])))
	if channel == "" {
		channel = claudeCompatDefaultChannel
	}

	internalReq := domain.AgentProcessRequest{
		Input:     input,
		SessionID: sessionID,
		UserID:    userID,
		Channel:   channel,
		Stream:    false,
		BizParams: map[string]interface{}{
			"prompt_mode": promptModeClaude,
		},
	}
	internalBody, err := json.Marshal(internalReq)
	if err != nil {
		writeClaudeCompatError(w, http.StatusInternalServerError, "api_error", "failed to build internal request")
		return
	}

	recorder := httptest.NewRecorder()
	s.processAgentWithBody(recorder, r.Clone(r.Context()), internalBody)
	internalResp := recorder.Result()
	defer internalResp.Body.Close()
	internalRespBody, _ := io.ReadAll(internalResp.Body)

	if internalResp.StatusCode != http.StatusOK {
		code, message := extractInternalAPIError(internalRespBody)
		if message == "" {
			message = "upstream request failed"
		}
		if code == "" {
			code = mapStatusToClaudeErrorType(internalResp.StatusCode)
		}
		writeClaudeCompatError(w, internalResp.StatusCode, code, message)
		return
	}

	var result domain.AgentProcessResponse
	if err := json.Unmarshal(internalRespBody, &result); err != nil {
		writeClaudeCompatError(w, http.StatusBadGateway, "api_error", "invalid internal response")
		return
	}

	reply := strings.TrimSpace(result.Reply)
	msgID := strings.TrimSpace(latestProviderResponseIDFromEvents(result.Events))
	if msgID == "" {
		msgID = newClaudeCompatMessageID()
	}
	contentBlocks, hasToolUse := buildClaudeCompatContentBlocks(reply, result.Events)
	stopReason := "end_turn"
	if hasToolUse && strings.TrimSpace(reply) == "" {
		stopReason = "tool_use"
	}
	usage := claudeCompatUsage{
		InputTokens:              estimateClaudeCompatInputTokens(input) + estimateClaudeCompatToolsTokens(req.Tools),
		CacheCreationInputTokens: 0,
		CacheReadInputTokens:     0,
		OutputTokens:             estimateClaudeCompatOutputTokens(contentBlocks),
		ServiceTier:              "standard",
	}
	response := claudeCompatMessageResponse{
		ID:           msgID,
		Type:         "message",
		Role:         "assistant",
		Model:        strings.TrimSpace(req.Model),
		Content:      contentBlocks,
		StopReason:   stopReason,
		StopSequence: nil,
		Usage:        usage,
	}

	if req.Stream {
		s.writeClaudeCompatStreamResponse(w, response)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) claudeCountTokens(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
		return
	}

	var req claudeCompatMessagesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	input, err := buildClaudeCompatInput(req.System, req.Messages)
	if err != nil {
		writeClaudeCompatError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, claudeCompatCountTokensResponse{
		InputTokens: estimateClaudeCompatInputTokens(input) + estimateClaudeCompatToolsTokens(req.Tools),
	})
}

func buildClaudeCompatInput(system interface{}, messages []claudeCompatMessage) ([]domain.AgentInputMessage, error) {
	out := make([]domain.AgentInputMessage, 0, len(messages)+2)
	if err := appendClaudeCompatSystemMessages(&out, system); err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages is required")
	}
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		switch role {
		case "user", "assistant", "tool", "system":
		default:
			return nil, fmt.Errorf("unsupported message role: %s", role)
		}
		content, err := claudeCompatToRuntimeContent(message.Content)
		if err != nil {
			return nil, fmt.Errorf("invalid message content for role %s: %w", role, err)
		}
		out = append(out, domain.AgentInputMessage{
			Role:    role,
			Type:    "message",
			Content: content,
		})
	}
	return out, nil
}

func appendClaudeCompatSystemMessages(out *[]domain.AgentInputMessage, raw interface{}) error {
	if raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil
		}
		*out = append(*out, domain.AgentInputMessage{
			Role: "system",
			Type: "message",
			Content: []domain.RuntimeContent{
				{Type: "text", Text: text},
			},
		})
	case []interface{}:
		for _, item := range value {
			content, err := claudeCompatToRuntimeContent(item)
			if err != nil {
				return fmt.Errorf("invalid system content: %w", err)
			}
			*out = append(*out, domain.AgentInputMessage{
				Role:    "system",
				Type:    "message",
				Content: content,
			})
		}
	default:
		content, err := claudeCompatToRuntimeContent(raw)
		if err != nil {
			return fmt.Errorf("invalid system content: %w", err)
		}
		*out = append(*out, domain.AgentInputMessage{
			Role:    "system",
			Type:    "message",
			Content: content,
		})
	}
	return nil
}

func claudeCompatToRuntimeContent(raw interface{}) ([]domain.RuntimeContent, error) {
	switch value := raw.(type) {
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil, fmt.Errorf("content text is empty")
		}
		return []domain.RuntimeContent{{Type: "text", Text: text}}, nil
	case []interface{}:
		out := make([]domain.RuntimeContent, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(claudeCompatBlockToText(item))
			if text == "" {
				continue
			}
			out = append(out, domain.RuntimeContent{Type: "text", Text: text})
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("content blocks are empty")
		}
		return out, nil
	case map[string]interface{}:
		text := strings.TrimSpace(claudeCompatBlockToText(value))
		if text == "" {
			return nil, fmt.Errorf("content block is empty")
		}
		return []domain.RuntimeContent{{Type: "text", Text: text}}, nil
	default:
		text := strings.TrimSpace(claudeCompatBlockToText(raw))
		if text == "" {
			return nil, fmt.Errorf("unsupported content type")
		}
		return []domain.RuntimeContent{{Type: "text", Text: text}}, nil
	}
}

func claudeCompatBlockToText(raw interface{}) string {
	switch value := raw.(type) {
	case string:
		return value
	case map[string]interface{}:
		blockType := strings.ToLower(strings.TrimSpace(stringValue(value["type"])))
		switch blockType {
		case "", "text":
			return strings.TrimSpace(stringValue(value["text"]))
		case "tool_result":
			if direct := strings.TrimSpace(stringValue(value["content"])); direct != "" {
				return "[tool_result] " + direct
			}
			if nested, ok := value["content"].([]interface{}); ok {
				parts := make([]string, 0, len(nested))
				for _, item := range nested {
					text := strings.TrimSpace(claudeCompatBlockToText(item))
					if text != "" {
						parts = append(parts, text)
					}
				}
				if len(parts) > 0 {
					return "[tool_result] " + strings.Join(parts, "\n")
				}
			}
			return "[tool_result] " + marshalJSONCompact(value)
		case "tool_use":
			return "[tool_use] " + marshalJSONCompact(value)
		default:
			return marshalJSONCompact(value)
		}
	default:
		return marshalJSONCompact(value)
	}
}

func marshalJSONCompact(raw interface{}) string {
	body, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(body)
}

func newClaudeCompatMessageID() string {
	return fmt.Sprintf("msg_%x", time.Now().UnixNano())
}

func newClaudeCompatSessionID() string {
	return fmt.Sprintf("claude-session-%x", time.Now().UnixNano())
}

func estimateClaudeCompatInputTokens(messages []domain.AgentInputMessage) int {
	total := 0
	for _, message := range messages {
		for _, content := range message.Content {
			total += estimatePromptTokenCount(content.Text)
		}
	}
	return total
}

func estimateClaudeCompatToolsTokens(tools []map[string]interface{}) int {
	total := 0
	for _, tool := range tools {
		body, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		total += estimatePromptTokenCount(string(body))
	}
	return total
}

func estimateClaudeCompatOutputTokens(blocks []map[string]interface{}) int {
	total := 0
	for _, block := range blocks {
		blockType := strings.ToLower(strings.TrimSpace(stringValue(block["type"])))
		switch blockType {
		case "text":
			total += estimatePromptTokenCount(strings.TrimSpace(stringValue(block["text"])))
		case "tool_use":
			name := strings.TrimSpace(stringValue(block["name"]))
			total += estimatePromptTokenCount(name)
			body, err := json.Marshal(block["input"])
			if err == nil {
				total += estimatePromptTokenCount(string(body))
			}
		default:
			body, err := json.Marshal(block)
			if err == nil {
				total += estimatePromptTokenCount(string(body))
			}
		}
	}
	return total
}

func latestProviderResponseIDFromEvents(events []domain.AgentEvent) string {
	for idx := len(events) - 1; idx >= 0; idx-- {
		meta := events[idx].Meta
		if len(meta) == 0 {
			continue
		}
		if value := strings.TrimSpace(stringValue(meta["provider_response_id"])); value != "" {
			return value
		}
	}
	return ""
}

func buildClaudeCompatContentBlocks(reply string, events []domain.AgentEvent) ([]map[string]interface{}, bool) {
	out := make([]map[string]interface{}, 0, 4)
	toolIndex := 1
	for _, evt := range events {
		if evt.Type != "tool_call" || evt.ToolCall == nil {
			continue
		}
		name := strings.TrimSpace(evt.ToolCall.Name)
		if name == "" {
			continue
		}
		block := map[string]interface{}{
			"type":  "tool_use",
			"id":    fmt.Sprintf("toolu_%03d", toolIndex),
			"name":  name,
			"input": safeMap(evt.ToolCall.Input),
		}
		out = append(out, block)
		toolIndex++
	}
	if text := strings.TrimSpace(reply); text != "" {
		out = append(out, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}
	if len(out) == 0 {
		out = append(out, map[string]interface{}{
			"type": "text",
			"text": "(empty reply)",
		})
	}
	return out, toolIndex > 1
}

func extractInternalAPIError(body []byte) (string, string) {
	var apiErr domain.APIErrorBody
	if err := json.Unmarshal(body, &apiErr); err == nil {
		if strings.TrimSpace(apiErr.Error.Code) != "" || strings.TrimSpace(apiErr.Error.Message) != "" {
			return mapInternalCodeToClaudeErrorType(apiErr.Error.Code), strings.TrimSpace(apiErr.Error.Message)
		}
	}
	return "", ""
}

func mapInternalCodeToClaudeErrorType(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "unauthorized":
		return "authentication_error"
	case "rate_limit", "rate_limited", "rate_limit_exceeded":
		return "rate_limit_error"
	case "store_error", "runner_error", "provider_request_failed", "provider_invalid_reply", "channel_dispatch_failed", "tool_runtime_unavailable":
		return "api_error"
	default:
		return "invalid_request_error"
	}
}

func mapStatusToClaudeErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	}
	if status >= http.StatusInternalServerError {
		return "api_error"
	}
	return "invalid_request_error"
}

func writeClaudeCompatError(w http.ResponseWriter, status int, errType, message string) {
	if strings.TrimSpace(errType) == "" {
		errType = mapStatusToClaudeErrorType(status)
	}
	if strings.TrimSpace(message) == "" {
		message = "request failed"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	})
}

func (s *Server) writeClaudeCompatStreamResponse(w http.ResponseWriter, response claudeCompatMessageResponse) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeClaudeCompatError(w, http.StatusInternalServerError, "api_error", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	emit := func(event string, payload interface{}) bool {
		body, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !emit("message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            response.ID,
			"type":          response.Type,
			"role":          response.Role,
			"model":         response.Model,
			"content":       []interface{}{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]interface{}{
				"input_tokens":  response.Usage.InputTokens,
				"output_tokens": 0,
			},
		},
	}) {
		return
	}

	for index, block := range response.Content {
		blockType := strings.ToLower(strings.TrimSpace(stringValue(block["type"])))
		switch blockType {
		case "tool_use":
			contentBlock := map[string]interface{}{
				"type":  "tool_use",
				"id":    strings.TrimSpace(stringValue(block["id"])),
				"name":  strings.TrimSpace(stringValue(block["name"])),
				"input": map[string]interface{}{},
			}
			if !emit("content_block_start", map[string]interface{}{
				"type":          "content_block_start",
				"index":         index,
				"content_block": contentBlock,
			}) {
				return
			}
			inputJSON := "{}"
			inputPayload := map[string]interface{}{}
			if rawInput, ok := block["input"].(map[string]interface{}); ok {
				inputPayload = safeMap(rawInput)
			}
			if body, err := json.Marshal(inputPayload); err == nil {
				inputJSON = string(body)
			}
			if !emit("content_block_delta", map[string]interface{}{
				"type":  "content_block_delta",
				"index": index,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": inputJSON,
				},
			}) {
				return
			}
		default:
			if !emit("content_block_start", map[string]interface{}{
				"type":  "content_block_start",
				"index": index,
				"content_block": map[string]interface{}{
					"type": "text",
					"text": "",
				},
			}) {
				return
			}
			text := strings.TrimSpace(stringValue(block["text"]))
			for _, chunk := range chunkClaudeCompatText(text, 96) {
				if !emit("content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": index,
					"delta": map[string]interface{}{
						"type": "text_delta",
						"text": chunk,
					},
				}) {
					return
				}
			}
		}

		if !emit("content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": index,
		}) {
			return
		}
	}

	if !emit("message_delta", map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   response.StopReason,
			"stop_sequence": response.StopSequence,
		},
		"usage": map[string]interface{}{
			"output_tokens": response.Usage.OutputTokens,
		},
	}) {
		return
	}

	_ = emit("message_stop", map[string]interface{}{
		"type": "message_stop",
	})
}

func chunkClaudeCompatText(text string, size int) []string {
	if size <= 0 {
		size = 96
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	out := make([]string, 0, len(runes)/size+1)
	for start := 0; start < len(runes); start += size {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[start:end]))
	}
	return out
}

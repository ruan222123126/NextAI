package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
	agentprotocolservice "nextai/apps/gateway/internal/service/agentprotocol"
	selfopsservice "nextai/apps/gateway/internal/service/selfops"
)

func (s *Server) processAgent(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	s.processAgentWithBody(w, r, bodyBytes)
}

type agentSystemLayerView struct {
	Name            string `json:"name"`
	Role            string `json:"role"`
	Source          string `json:"source,omitempty"`
	ContentPreview  string `json:"content_preview,omitempty"`
	LayerHash       string `json:"layer_hash,omitempty"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type agentSystemLayersResponse struct {
	Version              string                 `json:"version"`
	ModeVariant          string                 `json:"mode_variant,omitempty"`
	Layers               []agentSystemLayerView `json:"layers"`
	EstimatedTokensTotal int                    `json:"estimated_tokens_total"`
}

const assistantMetadataProviderResponseIDKey = "provider_response_id"

func (s *Server) getAgentSystemLayers(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.EnablePromptContextIntrospect {
		writeErr(w, http.StatusNotFound, "feature_disabled", "prompt context introspection is disabled", nil)
		return
	}

	promptMode := promptModeDefault
	if rawMode := strings.TrimSpace(r.URL.Query().Get(chatMetaPromptModeKey)); rawMode != "" {
		normalizedMode, ok := normalizePromptMode(rawMode)
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_request", "invalid prompt_mode", nil)
			return
		}
		promptMode = normalizedMode
	}

	layerOptions := codexLayerBuildOptions{}
	if promptMode == promptModeCodex {
		if rawTaskCommand := strings.TrimSpace(r.URL.Query().Get("task_command")); rawTaskCommand != "" {
			taskCommand, ok := normalizeSystemLayerTaskCommand(rawTaskCommand)
			if !ok {
				writeErr(w, http.StatusBadRequest, "invalid_request", "invalid task_command", nil)
				return
			}
			switch taskCommand {
			case reviewTaskCommand:
				layerOptions.ReviewTask = true
			case compactTaskCommand:
				layerOptions.CompactTask = true
			case memoryTaskCommand:
				layerOptions.MemoryTask = true
			case planTaskCommand:
				layerOptions.CollaborationMode = collaborationModePlanName
			case executeTaskCommand:
				layerOptions.CollaborationMode = collaborationModeExecuteName
			case pairTaskCommand:
				layerOptions.CollaborationMode = collaborationModePairProgrammingName
			}
		}
		layerOptions.SessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
	}

	layers, err := s.buildSystemLayersForModeWithOptions(promptMode, layerOptions)
	if err != nil {
		errorCode, errorMessage := promptUnavailableErrorForMode(promptMode)
		writeErr(w, http.StatusInternalServerError, errorCode, errorMessage, nil)
		return
	}

	resp := agentSystemLayersResponse{
		Version:     "v1",
		ModeVariant: s.resolvePromptModeVariant(promptMode),
		Layers:      make([]agentSystemLayerView, 0, len(layers)),
	}
	for _, layer := range layers {
		tokens := estimatePromptTokenCount(layer.Content)
		resp.EstimatedTokensTotal += tokens
		resp.Layers = append(resp.Layers, agentSystemLayerView{
			Name:            layer.Name,
			Role:            layer.Role,
			Source:          layer.Source,
			ContentPreview:  summarizeLayerPreview(layer.Content, 160),
			LayerHash:       normalizedLayerContentHash(layer.Content),
			EstimatedTokens: tokens,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) processQQInbound(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}

	event, err := parseQQInboundEvent(bodyBytes)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_qq_event", err.Error(), nil)
		return
	}
	if strings.TrimSpace(event.Text) == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"accepted": false,
			"reason":   "empty_text",
		})
		return
	}

	request := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{Type: "text", Text: event.Text},
				},
			},
		},
		SessionID: event.SessionID,
		UserID:    event.UserID,
		Channel:   "qq",
		Stream:    false,
		BizParams: map[string]interface{}{
			"channel": map[string]interface{}{
				"target_type": event.TargetType,
				"target_id":   event.TargetID,
				"msg_id":      event.MessageID,
			},
		},
	}

	agentBody, err := json.Marshal(request)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "qq_inbound_marshal_failed", "failed to build agent request", nil)
		return
	}
	s.processAgentWithBody(w, r, agentBody)
}

func (s *Server) processAgentWithBody(w http.ResponseWriter, r *http.Request, bodyBytes []byte) {
	var req domain.AgentProcessRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	rawRequest := map[string]interface{}{}
	if err := json.Unmarshal(bodyBytes, &rawRequest); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}

	req.Channel = resolveProcessRequestChannel(r, req.Channel)
	streaming := req.Stream

	var flusher http.Flusher
	streamStarted := false
	if streaming {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		var ok bool
		flusher, ok = w.(http.Flusher)
		if !ok {
			writeErr(w, http.StatusInternalServerError, "stream_not_supported", "streaming not supported", nil)
			return
		}
	}

	streamFail := func(status int, code, message string, details interface{}) {
		if !streaming || !streamStarted {
			writeErr(w, status, code, message, details)
			return
		}
		meta := map[string]interface{}{
			"code":    code,
			"message": message,
		}
		if details != nil {
			meta["details"] = details
		}
		payload, _ := json.Marshal(domain.AgentEvent{
			Type: "error",
			Meta: meta,
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}

	emitEvent := func(evt domain.AgentEvent) {
		if !streaming {
			return
		}
		payload, _ := json.Marshal(evt)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		streamStarted = true
	}

	response, processErr := s.processAgentCore(r.Context(), req, rawRequest, streaming, emitEvent)
	if processErr != nil {
		streamFail(processErr.Status, processErr.Code, processErr.Message, processErr.Details)
		return
	}

	if !streaming {
		writeJSON(w, http.StatusOK, response)
		return
	}

	if !streamStarted {
		for _, evt := range response.Events {
			emitEvent(evt)
		}
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func isContextResetCommand(input []domain.AgentInputMessage) bool {
	for _, msg := range input {
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			continue
		}
		for _, part := range msg.Content {
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			return strings.EqualFold(text, contextResetCommand)
		}
	}
	return false
}

func isReviewTaskCommand(input []domain.AgentInputMessage) bool {
	return matchesSlashCommand(input, reviewTaskCommand)
}

func isPlanTaskCommand(input []domain.AgentInputMessage) bool {
	return matchesSlashCommand(input, planTaskCommand)
}

func isExecuteTaskCommand(input []domain.AgentInputMessage) bool {
	return matchesSlashCommand(input, executeTaskCommand)
}

func isPairTaskCommand(input []domain.AgentInputMessage) bool {
	return matchesSlashCommand(input, pairTaskCommand)
}

func isCompactTaskCommand(input []domain.AgentInputMessage) bool {
	return matchesSlashCommand(input, compactTaskCommand)
}

func isMemoryTaskCommand(input []domain.AgentInputMessage) bool {
	return matchesSlashCommand(input, memoryTaskCommand)
}

func matchesSlashCommand(input []domain.AgentInputMessage, command string) bool {
	normalizedCommand := strings.TrimSpace(command)
	if normalizedCommand == "" {
		return false
	}
	for _, msg := range input {
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			continue
		}
		for _, part := range msg.Content {
			if !strings.EqualFold(strings.TrimSpace(part.Type), "text") {
				continue
			}
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			fields := strings.Fields(text)
			if len(fields) == 0 {
				continue
			}
			return strings.EqualFold(fields[0], normalizedCommand)
		}
	}
	return false
}

func normalizeSystemLayerTaskCommand(raw string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "", false
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	switch normalized {
	case reviewTaskCommand:
		return reviewTaskCommand, true
	case compactTaskCommand:
		return compactTaskCommand, true
	case memoryTaskCommand:
		return memoryTaskCommand, true
	case planTaskCommand:
		return planTaskCommand, true
	case executeTaskCommand:
		return executeTaskCommand, true
	case pairTaskCommand, "/pair-programming":
		return pairTaskCommand, true
	default:
		return "", false
	}
}

func (s *Server) clearChatContext(sessionID, userID, channel string) error {
	return s.store.Write(func(state *repo.State) error {
		for chatID, spec := range state.Chats {
			if spec.SessionID != sessionID || spec.UserID != userID || spec.Channel != channel {
				continue
			}
			delete(state.Chats, chatID)
			delete(state.Histories, chatID)
		}
		return nil
	})
}

func writeImmediateAgentResponse(w http.ResponseWriter, streaming bool, reply string) {
	if !streaming {
		writeJSON(w, http.StatusOK, domain.AgentProcessResponse{
			Reply: reply,
			Events: []domain.AgentEvent{
				{Type: "step_started", Step: 1},
				{Type: "assistant_delta", Step: 1, Delta: reply},
				{Type: "completed", Step: 1, Reply: reply},
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "stream_not_supported", "streaming not supported", nil)
		return
	}

	stepStartedPayload, _ := json.Marshal(domain.AgentEvent{
		Type: "step_started",
		Step: 1,
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", stepStartedPayload)
	flusher.Flush()

	for _, chunk := range splitReplyChunks(reply, replyChunkSizeDefault) {
		deltaPayload, _ := json.Marshal(domain.AgentEvent{
			Type:  "assistant_delta",
			Step:  1,
			Delta: chunk,
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", deltaPayload)
		flusher.Flush()
	}

	completedPayload, _ := json.Marshal(domain.AgentEvent{
		Type:  "completed",
		Step:  1,
		Reply: reply,
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", completedPayload)
	flusher.Flush()

	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func resolveProcessRequestChannel(r *http.Request, requestedChannel string) string {
	return agentprotocolservice.ResolveProcessRequestChannel(
		r,
		requestedChannel,
		qqInboundPath,
		qqChannelName,
		defaultProcessChannel,
	)
}

func isQQInboundRequest(r *http.Request) bool {
	return agentprotocolservice.IsQQInboundRequest(r, qqInboundPath)
}

type qqInboundEvent struct {
	Text       string
	UserID     string
	SessionID  string
	TargetType string
	TargetID   string
	MessageID  string
}

func parseQQInboundEvent(body []byte) (qqInboundEvent, error) {
	parsed, err := agentprotocolservice.ParseQQInboundEvent(body)
	if err != nil {
		return qqInboundEvent{}, err
	}
	return qqInboundEvent{
		Text:       parsed.Text,
		UserID:     parsed.UserID,
		SessionID:  parsed.SessionID,
		TargetType: parsed.TargetType,
		TargetID:   parsed.TargetID,
		MessageID:  parsed.MessageID,
	}, nil
}

func mergeChannelDispatchConfig(channelName string, cfg map[string]interface{}, bizParams map[string]interface{}) map[string]interface{} {
	return agentprotocolservice.MergeChannelDispatchConfig(channelName, cfg, bizParams)
}

func cronChatMetaFromBizParams(bizParams map[string]interface{}) map[string]interface{} {
	return agentprotocolservice.CronChatMetaFromBizParams(bizParams)
}

func parsePromptModeFromBizParams(bizParams map[string]interface{}) (string, bool, error) {
	return agentprotocolservice.ParsePromptModeFromBizParams(
		bizParams,
		chatMetaPromptModeKey,
		promptModeDefault,
		normalizePromptMode,
	)
}

func resolvePromptModeFromChatMeta(meta map[string]interface{}) string {
	return agentprotocolservice.ResolvePromptModeFromChatMeta(
		meta,
		chatMetaPromptModeKey,
		promptModeDefault,
		normalizePromptMode,
	)
}

func resolveChatActiveModelSlot(meta map[string]interface{}, state *repo.State) domain.ModelSlotConfig {
	if override, ok := parseChatActiveModelOverride(meta); ok {
		return override
	}
	if state == nil {
		return domain.ModelSlotConfig{}
	}
	return domain.ModelSlotConfig{
		ProviderID: normalizeProviderID(state.ActiveLLM.ProviderID),
		Model:      strings.TrimSpace(state.ActiveLLM.Model),
	}
}

func parseChatActiveModelOverride(meta map[string]interface{}) (domain.ModelSlotConfig, bool) {
	if len(meta) == 0 {
		return domain.ModelSlotConfig{}, false
	}
	rawOverride, ok := meta[domain.ChatMetaActiveLLM]
	if !ok || rawOverride == nil {
		return domain.ModelSlotConfig{}, false
	}
	switch value := rawOverride.(type) {
	case map[string]interface{}:
		providerID := normalizeProviderID(stringValue(value["provider_id"]))
		modelID := strings.TrimSpace(stringValue(value["model"]))
		if providerID == "" || modelID == "" {
			return domain.ModelSlotConfig{}, false
		}
		return domain.ModelSlotConfig{
			ProviderID: providerID,
			Model:      modelID,
		}, true
	case domain.ChatActiveLLMOverride:
		providerID := normalizeProviderID(value.ProviderID)
		modelID := strings.TrimSpace(value.Model)
		if providerID == "" || modelID == "" {
			return domain.ModelSlotConfig{}, false
		}
		return domain.ModelSlotConfig{
			ProviderID: providerID,
			Model:      modelID,
		}, true
	default:
		return domain.ModelSlotConfig{}, false
	}
}

func normalizePromptMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case promptModeDefault:
		return promptModeDefault, true
	case promptModeCodex:
		return promptModeCodex, true
	case promptModeClaude:
		return promptModeClaude, true
	default:
		return "", false
	}
}

func promptUnavailableErrorForMode(mode string) (string, string) {
	normalizedMode, ok := normalizePromptMode(mode)
	if !ok {
		return "ai_tool_guide_unavailable", "ai tools guide is unavailable"
	}
	switch normalizedMode {
	case promptModeCodex:
		return "codex_prompt_unavailable", "codex prompt is unavailable"
	case promptModeClaude:
		return "claude_prompt_unavailable", "claude prompt is unavailable"
	default:
		return "ai_tool_guide_unavailable", "ai tools guide is unavailable"
	}
}

func qqMap(raw interface{}) (map[string]interface{}, bool) {
	value, ok := raw.(map[string]interface{})
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

func qqString(raw interface{}) string {
	switch value := raw.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func qqFirst(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeQQTargetTypeAlias(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "c2c", "user", "private":
		return "c2c", true
	case "group":
		return "group", true
	case "guild", "channel", "dm":
		return "guild", true
	default:
		return "", false
	}
}

func toRuntimeContents(in []domain.RuntimeContent) []domain.RuntimeContent {
	if in == nil {
		return []domain.RuntimeContent{}
	}
	return in
}

func runtimeHistoryToAgentInputMessages(history []domain.RuntimeMessage) []domain.AgentInputMessage {
	if len(history) == 0 {
		return []domain.AgentInputMessage{}
	}
	out := make([]domain.AgentInputMessage, 0, len(history))
	for _, msg := range history {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}
		msgType := strings.TrimSpace(msg.Type)
		if msgType == "" {
			msgType = "message"
		}
		item := domain.AgentInputMessage{
			Role:    role,
			Type:    msgType,
			Content: append([]domain.RuntimeContent{}, msg.Content...),
		}
		if msg.Metadata != nil {
			data, err := json.Marshal(msg.Metadata)
			if err == nil {
				var meta map[string]interface{}
				if err := json.Unmarshal(data, &meta); err == nil {
					item.Metadata = meta
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func latestProviderResponseIDFromInput(history []domain.AgentInputMessage) string {
	for idx := len(history) - 1; idx >= 0; idx-- {
		item := history[idx]
		if strings.TrimSpace(item.Role) != "assistant" || item.Metadata == nil {
			continue
		}
		value, ok := item.Metadata[assistantMetadataProviderResponseIDKey]
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringValue(value))
		if id != "" {
			return id
		}
	}
	return ""
}

func providerStoreEnabled(setting repo.ProviderSetting) bool {
	if setting.Store == nil {
		return false
	}
	return *setting.Store
}

func splitReplyChunks(text string, chunkSize int) []string {
	if chunkSize <= 0 {
		chunkSize = 12
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	out := make([]string, 0, (len(runes)+chunkSize-1)/chunkSize)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func (s *Server) listToolDefinitions() []runner.ToolDefinition {
	return s.listToolDefinitionsForPromptMode(promptModeDefault)
}

func (s *Server) listToolDefinitionsForPromptMode(promptMode string) []runner.ToolDefinition {
	registeredToolNames := make([]string, 0, len(s.tools))
	for name := range s.tools {
		registeredToolNames = append(registeredToolNames, name)
	}
	names := agentprotocolservice.ListToolDefinitionNames(
		promptMode,
		registeredToolNames,
		s.toolHasDeclaredCapability,
		s.toolDisabled,
	)

	out := make([]runner.ToolDefinition, 0, len(names))
	for _, name := range names {
		out = append(out, buildToolDefinition(name))
	}
	return out
}

func claudeCompatibleToolDefinitionNames() []string {
	return agentprotocolservice.ClaudeCompatibleToolDefinitionNames()
}

func buildToolDefinition(name string) runner.ToolDefinition {
	switch name {
	case "view":
		return runner.ToolDefinition{
			Name:        "view",
			Description: "Read line ranges for one or multiple files. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of view operations; pass one item for single-file view.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Absolute file path on local filesystem.",
								},
								"start": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based starting line number (inclusive).",
								},
								"end": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based ending line number (inclusive).",
								},
							},
							"required":             []string{"path", "start", "end"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "edit":
		return runner.ToolDefinition{
			Name:        "edit",
			Description: "Replace line ranges for one or multiple files; can create missing files directly. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of edit operations; pass one item for single-file edit.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Absolute file path on local filesystem.",
								},
								"start": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based starting line number (inclusive).",
								},
								"end": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based ending line number (inclusive).",
								},
								"content": map[string]interface{}{
									"type":        "string",
									"description": "Replacement text for the selected line range.",
								},
							},
							"required":             []string{"path", "start", "end", "content"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "shell":
		return runner.ToolDefinition{
			Name:        "shell",
			Description: "Execute one or multiple shell commands under server security controls. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of shell command operations; pass one item for single command.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"command": map[string]interface{}{
									"type": "string",
								},
								"cwd": map[string]interface{}{
									"type": "string",
								},
								"timeout_seconds": map[string]interface{}{
									"type":    "integer",
									"minimum": 1,
								},
							},
							"required":             []string{"command"},
							"additionalProperties": false,
						},
					},
				},
				"required": []string{"items"},
			},
		}
	case "browser":
		return runner.ToolDefinition{
			Name:        "browser",
			Description: "Delegate browser tasks to local Playwright agent script. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of browser tasks; pass one item for single task.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"task": map[string]interface{}{
									"type":        "string",
									"description": "Natural language task for browser agent.",
								},
								"timeout_seconds": map[string]interface{}{
									"type":    "integer",
									"minimum": 1,
								},
							},
							"required":             []string{"task"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "search":
		return runner.ToolDefinition{
			Name:        "search",
			Description: "Search the web via configured search APIs. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of search requests; pass one item for single query.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"query": map[string]interface{}{
									"type":        "string",
									"description": "Search query text.",
								},
								"provider": map[string]interface{}{
									"type":        "string",
									"description": "Optional provider override: serpapi | tavily | brave.",
								},
								"count": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "Optional max results per query.",
								},
								"timeout_seconds": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "Optional timeout for a single query.",
								},
							},
							"required":             []string{"query"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "open":
		return runner.ToolDefinition{
			Name:        "open",
			Description: "Open a local absolute path via view or open an HTTP(S) URL via browser summary task.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute local file path or HTTP(S) URL.",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Alternative URL field, same as path when using HTTP(S).",
					},
					"lineno": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional line anchor for local file open.",
					},
					"start": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional 1-based start line for local file open.",
					},
					"end": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional 1-based end line for local file open.",
					},
					"timeout_seconds": map[string]interface{}{
						"type":        "integer",
						"minimum":     1,
						"description": "Optional browser timeout when path/url is HTTP(S).",
					},
				},
				"additionalProperties": true,
			},
		}
	case "find":
		return runner.ToolDefinition{
			Name:        "find",
			Description: "Find plain-text pattern in one or multiple workspace files. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of find operations; pass one item for single-file find.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Workspace file path (relative or absolute within workspace).",
								},
								"pattern": map[string]interface{}{
									"type":        "string",
									"description": "Literal pattern text to match.",
								},
								"ignore_case": map[string]interface{}{
									"type":        "boolean",
									"description": "Optional case-insensitive match flag.",
								},
							},
							"required":             []string{"path", "pattern"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "click":
		return runner.ToolDefinition{
			Name:        "click",
			Description: "Approximate click action routed to browser tool. No persistent page session is guaranteed.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Optional target URL.",
					},
					"ref_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional reference id or URL from previous context.",
					},
					"id": map[string]interface{}{
						"description": "Optional clickable element id.",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "Optional CSS/XPath selector.",
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "Optional explicit browser task text.",
					},
					"timeout_seconds": map[string]interface{}{
						"type":    "integer",
						"minimum": 1,
					},
				},
				"additionalProperties": true,
			},
		}
	case "screenshot":
		return runner.ToolDefinition{
			Name:        "screenshot",
			Description: "Approximate screenshot action routed to browser tool. No persistent page session is guaranteed.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Optional target URL.",
					},
					"ref_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional reference id or URL from previous context.",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Optional screenshot output hint.",
					},
					"task": map[string]interface{}{
						"type":        "string",
						"description": "Optional explicit browser task text.",
					},
					"timeout_seconds": map[string]interface{}{
						"type":    "integer",
						"minimum": 1,
					},
				},
				"additionalProperties": true,
			},
		}
	case "self_ops":
		return runner.ToolDefinition{
			Name:        "self_ops",
			Description: "Execute self-operation actions: bootstrap_session, set_session_model, preview_mutation, apply_mutation.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"enum": []string{"bootstrap_session", "set_session_model", "preview_mutation", "apply_mutation"},
					},
					"user_id":         map[string]interface{}{"type": "string"},
					"channel":         map[string]interface{}{"type": "string"},
					"session_seed":    map[string]interface{}{"type": "string"},
					"first_input":     map[string]interface{}{"type": "string"},
					"prompt_mode":     map[string]interface{}{"type": "string"},
					"session_id":      map[string]interface{}{"type": "string"},
					"provider_id":     map[string]interface{}{"type": "string"},
					"model":           map[string]interface{}{"type": "string"},
					"target":          map[string]interface{}{"type": "string"},
					"operations":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object", "additionalProperties": true}},
					"allow_sensitive": map[string]interface{}{"type": "boolean"},
					"mutation_id":     map[string]interface{}{"type": "string"},
					"confirm_hash":    map[string]interface{}{"type": "string"},
				},
				"required":             []string{"action"},
				"additionalProperties": true,
			},
		}
	case "Bash":
		return runner.ToolDefinition{
			Name:        "Bash",
			Description: "Execute a bash command with optional timeout in milliseconds.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command":     map[string]interface{}{"type": "string"},
					"timeout":     map[string]interface{}{"type": "number"},
					"description": map[string]interface{}{"type": "string"},
					"cwd":         map[string]interface{}{"type": "string"},
					"workdir":     map[string]interface{}{"type": "string"},
				},
				"required":             []string{"command"},
				"additionalProperties": false,
			},
		}
	case "Read":
		return runner.ToolDefinition{
			Name:        "Read",
			Description: "Read a file with optional line offset/limit.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"offset":    map[string]interface{}{"type": "number"},
					"limit":     map[string]interface{}{"type": "number"},
				},
				"required":             []string{"file_path"},
				"additionalProperties": false,
			},
		}
	case "NotebookRead":
		return runner.ToolDefinition{
			Name:        "NotebookRead",
			Description: "Read cells from a Jupyter notebook.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"notebook_path": map[string]interface{}{"type": "string"},
					"cell_id":       map[string]interface{}{"type": "string"},
				},
				"required":             []string{"notebook_path"},
				"additionalProperties": false,
			},
		}
	case "Write":
		return runner.ToolDefinition{
			Name:        "Write",
			Description: "Write full file content.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"content":   map[string]interface{}{"type": "string"},
				},
				"required":             []string{"file_path", "content"},
				"additionalProperties": false,
			},
		}
	case "Edit":
		return runner.ToolDefinition{
			Name:        "Edit",
			Description: "Replace exact string in a file.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path":   map[string]interface{}{"type": "string"},
					"old_string":  map[string]interface{}{"type": "string"},
					"new_string":  map[string]interface{}{"type": "string"},
					"replace_all": map[string]interface{}{"type": "boolean"},
				},
				"required":             []string{"file_path", "old_string", "new_string"},
				"additionalProperties": false,
			},
		}
	case "MultiEdit":
		return runner.ToolDefinition{
			Name:        "MultiEdit",
			Description: "Apply multiple exact string edits to a file atomically.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
					"edits": map[string]interface{}{
						"type":     "array",
						"minItems": 1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"old_string":  map[string]interface{}{"type": "string"},
								"new_string":  map[string]interface{}{"type": "string"},
								"replace_all": map[string]interface{}{"type": "boolean"},
							},
							"required":             []string{"old_string", "new_string"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"file_path", "edits"},
				"additionalProperties": false,
			},
		}
	case "NotebookEdit":
		return runner.ToolDefinition{
			Name:        "NotebookEdit",
			Description: "Replace, insert, or delete notebook cells.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"notebook_path": map[string]interface{}{"type": "string"},
					"cell_id":       map[string]interface{}{"type": "string"},
					"new_source":    map[string]interface{}{"type": "string"},
					"cell_type": map[string]interface{}{
						"type": "string",
						"enum": []string{"code", "markdown"},
					},
					"edit_mode": map[string]interface{}{
						"type": "string",
						"enum": []string{"replace", "insert", "delete"},
					},
				},
				"required":             []string{"notebook_path", "new_source"},
				"additionalProperties": false,
			},
		}
	case "LS":
		return runner.ToolDefinition{
			Name:        "LS",
			Description: "List entries in a directory.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
					"ignore": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
		}
	case "Glob":
		return runner.ToolDefinition{
			Name:        "Glob",
			Description: "Match files by glob pattern.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{"type": "string"},
					"path":    map[string]interface{}{"type": "string"},
				},
				"required":             []string{"pattern"},
				"additionalProperties": false,
			},
		}
	case "Grep":
		return runner.ToolDefinition{
			Name:        "Grep",
			Description: "Regex search in files.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern":     map[string]interface{}{"type": "string"},
					"path":        map[string]interface{}{"type": "string"},
					"glob":        map[string]interface{}{"type": "string"},
					"output_mode": map[string]interface{}{"type": "string", "enum": []string{"content", "files_with_matches", "count"}},
					"-B":          map[string]interface{}{"type": "number"},
					"-A":          map[string]interface{}{"type": "number"},
					"-C":          map[string]interface{}{"type": "number"},
					"-n":          map[string]interface{}{"type": "boolean"},
					"-i":          map[string]interface{}{"type": "boolean"},
					"type":        map[string]interface{}{"type": "string"},
					"head_limit":  map[string]interface{}{"type": "number"},
					"multiline":   map[string]interface{}{"type": "boolean"},
				},
				"required":             []string{"pattern"},
				"additionalProperties": false,
			},
		}
	case "WebSearch":
		return runner.ToolDefinition{
			Name:        "WebSearch",
			Description: "Search web results.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
					"allowed_domains": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"blocked_domains": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		}
	case "WebFetch":
		return runner.ToolDefinition{
			Name:        "WebFetch",
			Description: "Fetch and summarize a URL.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":    map[string]interface{}{"type": "string"},
					"prompt": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"url", "prompt"},
				"additionalProperties": false,
			},
		}
	case "Task":
		return runner.ToolDefinition{
			Name:        "Task",
			Description: "Launch a sub-agent style task (emulated in NextAI).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"description":   map[string]interface{}{"type": "string"},
					"prompt":        map[string]interface{}{"type": "string"},
					"subagent_type": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"description", "prompt", "subagent_type"},
				"additionalProperties": false,
			},
		}
	case "TodoWrite":
		return runner.ToolDefinition{
			Name:        "TodoWrite",
			Description: "Update todo list state (emulated in NextAI).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todos": map[string]interface{}{
						"type":     "array",
						"minItems": 1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"content":  map[string]interface{}{"type": "string"},
								"status":   map[string]interface{}{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
								"priority": map[string]interface{}{"type": "string", "enum": []string{"high", "medium", "low"}},
								"id":       map[string]interface{}{"type": "string"},
							},
							"required":             []string{"content", "status", "priority", "id"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"todos"},
				"additionalProperties": false,
			},
		}
	case "ExitPlanMode":
		return runner.ToolDefinition{
			Name:        "ExitPlanMode",
			Description: "Ask to exit plan mode (emulated in NextAI).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"plan"},
				"additionalProperties": false,
			},
		}
	default:
		return runner.ToolDefinition{
			Name: name,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": true,
			},
		}
	}
}

func toAgentToolCallMetadata(calls []runner.ToolCall) []map[string]interface{} {
	if len(calls) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.Name) == "" {
			continue
		}
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			callID = newID("tool-call")
		}
		args, _ := json.Marshal(safeMap(call.Arguments))
		out = append(out, map[string]interface{}{
			"id":   callID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      call.Name,
				"arguments": string(args),
			},
		})
	}
	return out
}

func cloneAgentInputMessages(input []domain.AgentInputMessage) []domain.AgentInputMessage {
	if len(input) == 0 {
		return []domain.AgentInputMessage{}
	}
	out := make([]domain.AgentInputMessage, 0, len(input))
	for _, item := range input {
		cloned := domain.AgentInputMessage{
			Role:    item.Role,
			Type:    item.Type,
			Content: append([]domain.RuntimeContent{}, item.Content...),
		}
		if item.Metadata != nil {
			data, err := json.Marshal(item.Metadata)
			if err == nil {
				var meta map[string]interface{}
				if err := json.Unmarshal(data, &meta); err == nil {
					cloned.Metadata = meta
				}
			}
		}
		out = append(out, cloned)
	}
	return out
}

type completedModelRequestLayer struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Source  string `json:"source,omitempty"`
	Content string `json:"content"`
}

type completedModelRequestPayload struct {
	PromptMode   string                       `json:"prompt_mode,omitempty"`
	ProviderID   string                       `json:"provider_id,omitempty"`
	Model        string                       `json:"model,omitempty"`
	SystemLayers []completedModelRequestLayer `json:"system_layers,omitempty"`
	Input        []domain.AgentInputMessage   `json:"input"`
}

func buildCompletedModelRequestMeta(
	promptMode string,
	systemLayers []systemPromptLayer,
	input []domain.AgentInputMessage,
	generateConfig runner.GenerateConfig,
) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	trace := completedModelRequestPayload{
		PromptMode: strings.TrimSpace(promptMode),
		ProviderID: strings.TrimSpace(generateConfig.ProviderID),
		Model:      strings.TrimSpace(generateConfig.Model),
		Input:      cloneAgentInputMessages(input),
	}
	if len(systemLayers) > 0 {
		trace.SystemLayers = make([]completedModelRequestLayer, 0, len(systemLayers))
		for _, layer := range systemLayers {
			trace.SystemLayers = append(trace.SystemLayers, completedModelRequestLayer{
				Name:    strings.TrimSpace(layer.Name),
				Role:    strings.TrimSpace(layer.Role),
				Source:  strings.TrimSpace(layer.Source),
				Content: layer.Content,
			})
		}
	}
	return map[string]interface{}{"model_request": trace}
}

func withCompletedEventMetaForEvents(events []domain.AgentEvent, completedMeta map[string]interface{}) []domain.AgentEvent {
	if len(events) == 0 || len(completedMeta) == 0 {
		return events
	}
	out := make([]domain.AgentEvent, 0, len(events))
	for _, evt := range events {
		out = append(out, withCompletedEventMeta(evt, completedMeta))
	}
	return out
}

func withCompletedEventMeta(evt domain.AgentEvent, completedMeta map[string]interface{}) domain.AgentEvent {
	if evt.Type != "completed" || len(completedMeta) == 0 {
		return evt
	}
	evt.Meta = mergeEventMeta(evt.Meta, completedMeta)
	return evt
}

func mergeEventMeta(base map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func summarizeAgentEventText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 160 {
		return trimmed
	}
	return string(runes[:160]) + "..."
}

func buildAssistantMessageMetadata(events []domain.AgentEvent) map[string]interface{} {
	notices := make([]persistedToolNotice, 0, 2)
	textOrder := 0
	toolOrder := 0
	for idx, evt := range events {
		switch evt.Type {
		case "assistant_delta":
			if textOrder == 0 {
				textOrder = idx + 1
			}
		case "tool_call":
			if evt.ToolCall == nil {
				continue
			}
			if toolOrder == 0 {
				toolOrder = idx + 1
			}
			raw, err := json.Marshal(domain.AgentEvent{
				Type:     "tool_call",
				Step:     evt.Step,
				ToolCall: evt.ToolCall,
			})
			if err != nil {
				continue
			}
			notices = append(notices, persistedToolNotice{
				raw:  string(raw),
				step: evt.Step,
				name: strings.TrimSpace(evt.ToolCall.Name),
			})
		case "tool_result":
			if evt.ToolResult == nil {
				continue
			}
			if toolOrder == 0 {
				toolOrder = idx + 1
			}
			raw, err := json.Marshal(domain.AgentEvent{
				Type:       "tool_result",
				Step:       evt.Step,
				ToolResult: evt.ToolResult,
			})
			if err != nil {
				continue
			}
			notice := persistedToolNotice{
				raw:        string(raw),
				step:       evt.Step,
				name:       strings.TrimSpace(evt.ToolResult.Name),
				fromResult: true,
			}
			pendingIdx := findPendingToolCallNoticeIndex(notices, notice.step, notice.name)
			if pendingIdx >= 0 {
				notices[pendingIdx] = notice
				continue
			}
			notices = append(notices, notice)
		}
	}
	if len(notices) == 0 {
		return nil
	}
	serializedNotices := make([]map[string]interface{}, 0, len(notices))
	for _, notice := range notices {
		if strings.TrimSpace(notice.raw) == "" {
			continue
		}
		serializedNotices = append(serializedNotices, map[string]interface{}{"raw": notice.raw})
	}
	if len(serializedNotices) == 0 {
		return nil
	}
	out := map[string]interface{}{
		"tool_call_notices": serializedNotices,
	}
	if textOrder > 0 {
		out["text_order"] = textOrder
	}
	if toolOrder > 0 {
		out["tool_order"] = toolOrder
	}
	return out
}

type persistedToolNotice struct {
	raw        string
	step       int
	name       string
	fromResult bool
}

func findPendingToolCallNoticeIndex(notices []persistedToolNotice, step int, name string) int {
	for idx := len(notices) - 1; idx >= 0; idx-- {
		notice := notices[idx]
		if notice.fromResult {
			continue
		}
		if !toolNoticeMatches(notice, step, name) {
			continue
		}
		return idx
	}
	return -1
}

func toolNoticeMatches(notice persistedToolNotice, step int, name string) bool {
	if step > 0 && notice.step > 0 && notice.step != step {
		return false
	}
	noticeName := strings.TrimSpace(notice.name)
	incomingName := strings.TrimSpace(name)
	if noticeName != "" && incomingName != "" && !strings.EqualFold(noticeName, incomingName) {
		return false
	}
	return true
}

type channelError struct {
	Code    string
	Message string
	Err     error
}

type toolCall struct {
	Name  string
	Input map[string]interface{}
}

type recoverableProviderToolCall struct {
	ID           string
	Name         string
	RawArguments string
	Input        map[string]interface{}
	Feedback     string
}

type toolError struct {
	Code    string
	Message string
	Err     error
}

func recoverInvalidProviderToolCall(err error, step int) (recoverableProviderToolCall, bool) {
	invalid, ok := runner.InvalidToolCallFromError(err)
	if !ok {
		return recoverableProviderToolCall{}, false
	}

	callID := strings.TrimSpace(invalid.CallID)
	if callID == "" {
		callID = fmt.Sprintf("call_invalid_%d", step)
	}
	callName := strings.TrimSpace(invalid.Name)
	if callName == "" {
		callName = "unknown_tool"
	}
	rawArguments := strings.TrimSpace(invalid.ArgumentsRaw)
	if rawArguments == "" {
		rawArguments = "{}"
	}
	parseErr := "invalid json arguments"
	if invalid.Err != nil {
		parseErr = strings.TrimSpace(invalid.Err.Error())
	}
	parseErr = compactFeedbackField(parseErr, 160)
	input := map[string]interface{}{
		"raw_arguments": rawArguments,
	}
	if parseErr != "" {
		input["parse_error"] = parseErr
	}

	return recoverableProviderToolCall{
		ID:           callID,
		Name:         callName,
		RawArguments: rawArguments,
		Input:        input,
		Feedback:     formatProviderToolArgumentsErrorFeedback(callName, rawArguments, parseErr),
	}, true
}

func (e *channelError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *channelError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *toolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *toolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func parseToolCall(bizParams map[string]interface{}, rawRequest map[string]interface{}, promptMode string) (toolCall, bool, error) {
	call, ok, err := agentprotocolservice.ParseToolCall(bizParams, rawRequest, promptMode)
	if err != nil || !ok {
		return toolCall{}, ok, err
	}
	return toolCall{
		Name:  call.Name,
		Input: call.Input,
	}, true, nil
}

func parseBizParamsToolCall(bizParams map[string]interface{}, promptMode string) (toolCall, bool, error) {
	call, ok, err := agentprotocolservice.ParseBizParamsToolCall(bizParams, promptMode)
	if err != nil || !ok {
		return toolCall{}, ok, err
	}
	return toolCall{Name: call.Name, Input: call.Input}, true, nil
}

func parseShortcutToolCall(rawRequest map[string]interface{}, promptMode string) (toolCall, bool, error) {
	call, ok, err := agentprotocolservice.ParseShortcutToolCall(rawRequest, promptMode)
	if err != nil || !ok {
		return toolCall{}, ok, err
	}
	return toolCall{Name: call.Name, Input: call.Input}, true, nil
}

func parseToolPayload(raw interface{}, path string) (map[string]interface{}, error) {
	return agentprotocolservice.ParseToolPayload(raw, path)
}

func normalizeToolName(name string) string {
	return agentprotocolservice.NormalizeToolName(name)
}

func normalizeToolNameForPromptMode(name string, promptMode string) string {
	return agentprotocolservice.NormalizeToolNameForPromptMode(name, promptMode)
}

func isClaudePromptMode(promptMode string) bool {
	return agentprotocolservice.IsClaudePromptMode(promptMode)
}

func (s *Server) executeToolCall(call toolCall) (string, error) {
	return s.executeToolCallForPromptMode(promptModeDefault, call)
}

func (s *Server) executeToolCallForPromptMode(promptMode string, call toolCall) (string, error) {
	name := normalizeToolNameForPromptMode(strings.ToLower(strings.TrimSpace(call.Name)), promptMode)
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(call.Name))
	}
	input := safeMap(call.Input)

	if isClaudePromptMode(promptMode) {
		if result, handled, err := s.executeClaudeCompatibleToolCall(name, input); handled || err != nil {
			return result, err
		}
	}

	if s.toolDisabled(name) {
		return "", &toolError{
			Code:    "tool_disabled",
			Message: fmt.Sprintf("tool %q is disabled by server config", name),
		}
	}

	switch name {
	case "open":
		return s.executeOpenToolCall(input)
	case "click", "screenshot":
		return s.executeApproxBrowserToolCall(name, input)
	case "self_ops":
		return s.executeSelfOpsToolCall(input)
	default:
		result, err := s.invokeRegisteredTool(name, input)
		if err != nil {
			return "", err
		}
		return renderToolResult(name, result)
	}
}

func (s *Server) executeClaudeCompatibleToolCall(name string, input map[string]interface{}) (string, bool, error) {
	switch name {
	case "bash":
		out, err := s.executeClaudeBashTool(input)
		return out, true, err
	case "read":
		out, err := s.executeClaudeReadTool(input)
		return out, true, err
	case "notebookread":
		out, err := s.executeClaudeNotebookReadTool(input)
		return out, true, err
	case "write":
		out, err := s.executeClaudeWriteTool(input)
		return out, true, err
	case "edit":
		if !looksLikeClaudeEditInput(input) {
			return "", false, nil
		}
		out, err := s.executeClaudeEditTool(input)
		return out, true, err
	case "multiedit":
		out, err := s.executeClaudeMultiEditTool(input)
		return out, true, err
	case "notebookedit":
		out, err := s.executeClaudeNotebookEditTool(input)
		return out, true, err
	case "ls":
		out, err := s.executeClaudeLSTool(input)
		return out, true, err
	case "glob":
		out, err := s.executeClaudeGlobTool(input)
		return out, true, err
	case "grep":
		out, err := s.executeClaudeGrepTool(input)
		return out, true, err
	case "websearch":
		out, err := s.executeClaudeWebSearchTool(input)
		return out, true, err
	case "webfetch":
		out, err := s.executeClaudeWebFetchTool(input)
		return out, true, err
	case "task":
		out, err := executeClaudeTaskTool(input)
		return out, true, err
	case "todowrite":
		out, err := executeClaudeTodoWriteTool(input)
		return out, true, err
	case "exitplanmode":
		out, err := executeClaudeExitPlanModeTool(input)
		return out, true, err
	default:
		return "", false, nil
	}
}

func looksLikeClaudeEditInput(input map[string]interface{}) bool {
	item := firstToolInputItem(input)
	filePath := strings.TrimSpace(firstNonEmptyString(item, "file_path"))
	if filePath == "" {
		return false
	}
	if _, ok := item["old_string"]; ok {
		return true
	}
	if _, ok := item["new_string"]; ok {
		return true
	}
	return false
}

func (s *Server) executeClaudeBashTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("shell"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	command := strings.TrimSpace(firstNonEmptyString(item, "command", "cmd"))
	if command == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Bash" invocation failed`,
			Err:     plugin.ErrShellToolCommandMissing,
		}
	}
	shellItem := map[string]interface{}{
		"command": command,
	}
	if cwd := strings.TrimSpace(firstNonEmptyString(item, "cwd", "workdir")); cwd != "" {
		shellItem["cwd"] = cwd
	}
	if timeoutMS, ok := firstPositiveInt(item, "timeout", "yield_time_ms"); ok {
		timeoutSeconds := timeoutMS / 1000
		if timeoutMS%1000 != 0 {
			timeoutSeconds++
		}
		if timeoutSeconds < 1 {
			timeoutSeconds = 1
		}
		shellItem["timeout_seconds"] = timeoutSeconds
	}
	result, err := s.invokeRegisteredTool("shell", map[string]interface{}{
		"items": []interface{}{shellItem},
	})
	if err != nil {
		return "", err
	}
	return renderToolResult("Bash", result)
}

func (s *Server) executeClaudeReadTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("view"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	filePath := strings.TrimSpace(firstNonEmptyString(item, "file_path", "path"))
	if filePath == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Read" invocation failed`,
			Err:     plugin.ErrFileLinesToolPathMissing,
		}
	}
	absPath, err := resolveClaudeToolPath(filePath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Read" invocation failed`,
			Err:     err,
		}
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Read" invocation failed`,
			Err:     err,
		}
	}
	offset := 0
	if parsed, ok := parseNonNegativeIntAny(item["offset"]); ok {
		offset = parsed
	}
	limit := 2000
	if parsed, ok := parsePositiveIntAny(item["limit"]); ok {
		limit = parsed
	}
	text := formatClaudeReadOutput(absPath, string(raw), offset, limit)
	return text, nil
}

func (s *Server) executeClaudeNotebookReadTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("view"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	notebookPath := strings.TrimSpace(firstNonEmptyString(item, "notebook_path", "file_path", "path"))
	if notebookPath == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookRead" invocation failed`,
			Err:     plugin.ErrFileLinesToolPathMissing,
		}
	}
	absPath, err := resolveClaudeToolPath(notebookPath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookRead" invocation failed`,
			Err:     err,
		}
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookRead" invocation failed`,
			Err:     err,
		}
	}
	nb := map[string]interface{}{}
	if err := json.Unmarshal(raw, &nb); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookRead" invocation failed`,
			Err:     err,
		}
	}
	rawCells, _ := nb["cells"].([]interface{})
	cellID := strings.TrimSpace(firstNonEmptyString(item, "cell_id"))
	lines := make([]string, 0, len(rawCells)+2)
	lines = append(lines, fmt.Sprintf("NotebookRead %s", absPath))
	matched := 0
	for idx, rawCell := range rawCells {
		cell, ok := rawCell.(map[string]interface{})
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringValue(cell["id"]))
		if id == "" {
			id = fmt.Sprintf("cell-%d", idx)
		}
		if cellID != "" && id != cellID {
			continue
		}
		matched++
		cellType := strings.TrimSpace(stringValue(cell["cell_type"]))
		if cellType == "" {
			cellType = "unknown"
		}
		sourceText := normalizeNotebookSource(cell["source"])
		lines = append(lines, fmt.Sprintf("--- [%d] id=%s type=%s", idx, id, cellType))
		if strings.TrimSpace(sourceText) == "" {
			lines = append(lines, "(empty source)")
		} else {
			lines = append(lines, sourceText)
		}
	}
	if matched == 0 {
		if cellID != "" {
			lines = append(lines, fmt.Sprintf("cell_id=%s not found", cellID))
		} else {
			lines = append(lines, "no cells found")
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Server) executeClaudeWriteTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("edit"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	filePath := strings.TrimSpace(firstNonEmptyString(item, "file_path", "path"))
	if filePath == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Write" invocation failed`,
			Err:     plugin.ErrFileLinesToolPathMissing,
		}
	}
	content, ok := item["content"].(string)
	if !ok {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Write" invocation failed`,
			Err:     plugin.ErrFileLinesToolContentMissing,
		}
	}
	absPath, err := resolveClaudeToolPath(filePath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Write" invocation failed`,
			Err:     err,
		}
	}
	if err := writeTextFileWithMode(absPath, content); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Write" invocation failed`,
			Err:     err,
		}
	}
	return fmt.Sprintf("Write %s ok (bytes=%d)", absPath, len(content)), nil
}

func (s *Server) executeClaudeEditTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("edit"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	filePath := strings.TrimSpace(firstNonEmptyString(item, "file_path", "path"))
	if filePath == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Edit" invocation failed`,
			Err:     plugin.ErrFileLinesToolPathMissing,
		}
	}
	oldString, ok := item["old_string"].(string)
	if !ok {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Edit" invocation failed`,
			Err:     plugin.ErrFileLinesToolContentMissing,
		}
	}
	newString, ok := item["new_string"].(string)
	if !ok {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Edit" invocation failed`,
			Err:     plugin.ErrFileLinesToolContentMissing,
		}
	}
	replaceAll := parseBoolAny(item["replace_all"])
	absPath, err := resolveClaudeToolPath(filePath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Edit" invocation failed`,
			Err:     err,
		}
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Edit" invocation failed`,
			Err:     err,
		}
	}
	original := string(raw)
	occurrences := strings.Count(original, oldString)
	if occurrences == 0 {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Edit" invocation failed`,
			Err:     errors.New("old_string not found"),
		}
	}
	if !replaceAll && occurrences != 1 {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Edit" invocation failed`,
			Err:     fmt.Errorf("old_string is not unique (matches=%d)", occurrences),
		}
	}
	replaced := original
	changed := 1
	if replaceAll {
		replaced = strings.ReplaceAll(original, oldString, newString)
		changed = occurrences
	} else {
		replaced = strings.Replace(original, oldString, newString, 1)
	}
	if err := writeTextFileWithMode(absPath, replaced); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Edit" invocation failed`,
			Err:     err,
		}
	}
	return fmt.Sprintf("Edit %s ok (replacements=%d)", absPath, changed), nil
}

func (s *Server) executeClaudeMultiEditTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("edit"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	filePath := strings.TrimSpace(firstNonEmptyString(item, "file_path", "path"))
	if filePath == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "MultiEdit" invocation failed`,
			Err:     plugin.ErrFileLinesToolPathMissing,
		}
	}
	rawEdits, ok := item["edits"].([]interface{})
	if !ok || len(rawEdits) == 0 {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "MultiEdit" invocation failed`,
			Err:     plugin.ErrFileLinesToolItemsInvalid,
		}
	}
	absPath, err := resolveClaudeToolPath(filePath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "MultiEdit" invocation failed`,
			Err:     err,
		}
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "MultiEdit" invocation failed`,
			Err:     err,
		}
	}
	updated := string(raw)
	totalReplacements := 0
	for index, rawEdit := range rawEdits {
		editItem, ok := rawEdit.(map[string]interface{})
		if !ok {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "MultiEdit" invocation failed`,
				Err:     fmt.Errorf("edits[%d] must be an object", index),
			}
		}
		oldString, ok := editItem["old_string"].(string)
		if !ok {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "MultiEdit" invocation failed`,
				Err:     fmt.Errorf("edits[%d].old_string is required", index),
			}
		}
		newString, ok := editItem["new_string"].(string)
		if !ok {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "MultiEdit" invocation failed`,
				Err:     fmt.Errorf("edits[%d].new_string is required", index),
			}
		}
		replaceAll := parseBoolAny(editItem["replace_all"])
		occurrences := strings.Count(updated, oldString)
		if occurrences == 0 {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "MultiEdit" invocation failed`,
				Err:     fmt.Errorf("edits[%d].old_string not found", index),
			}
		}
		if !replaceAll && occurrences != 1 {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "MultiEdit" invocation failed`,
				Err:     fmt.Errorf("edits[%d].old_string is not unique (matches=%d)", index, occurrences),
			}
		}
		if replaceAll {
			updated = strings.ReplaceAll(updated, oldString, newString)
			totalReplacements += occurrences
		} else {
			updated = strings.Replace(updated, oldString, newString, 1)
			totalReplacements++
		}
	}
	if err := writeTextFileWithMode(absPath, updated); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "MultiEdit" invocation failed`,
			Err:     err,
		}
	}
	return fmt.Sprintf("MultiEdit %s ok (replacements=%d)", absPath, totalReplacements), nil
}

func (s *Server) executeClaudeNotebookEditTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("edit"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	notebookPath := strings.TrimSpace(firstNonEmptyString(item, "notebook_path", "file_path", "path"))
	if notebookPath == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookEdit" invocation failed`,
			Err:     plugin.ErrFileLinesToolPathMissing,
		}
	}
	absPath, err := resolveClaudeToolPath(notebookPath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookEdit" invocation failed`,
			Err:     err,
		}
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookEdit" invocation failed`,
			Err:     err,
		}
	}
	nb := map[string]interface{}{}
	if err := json.Unmarshal(raw, &nb); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookEdit" invocation failed`,
			Err:     err,
		}
	}
	rawCells, _ := nb["cells"].([]interface{})
	cells := make([]map[string]interface{}, 0, len(rawCells))
	for _, rawCell := range rawCells {
		if cell, ok := rawCell.(map[string]interface{}); ok {
			cells = append(cells, safeMap(cell))
		}
	}
	mode := strings.ToLower(strings.TrimSpace(firstNonEmptyString(item, "edit_mode")))
	if mode == "" {
		mode = "replace"
	}
	cellID := strings.TrimSpace(firstNonEmptyString(item, "cell_id"))
	newSource := firstNonEmptyString(item, "new_source")
	cellType := strings.ToLower(strings.TrimSpace(firstNonEmptyString(item, "cell_type")))
	targetIndex := -1
	if cellID != "" {
		for index, cell := range cells {
			if strings.TrimSpace(stringValue(cell["id"])) == cellID {
				targetIndex = index
				break
			}
		}
	}
	switch mode {
	case "replace":
		if targetIndex < 0 {
			if len(cells) == 0 {
				return "", &toolError{
					Code:    "tool_invoke_failed",
					Message: `tool "NotebookEdit" invocation failed`,
					Err:     errors.New("no cells available to replace"),
				}
			}
			targetIndex = 0
		}
		cell := safeMap(cells[targetIndex])
		cell["source"] = []interface{}{newSource}
		if cellType != "" {
			cell["cell_type"] = cellType
		}
		cells[targetIndex] = cell
	case "insert":
		insertIndex := 0
		if targetIndex >= 0 {
			insertIndex = targetIndex + 1
		}
		insertType := cellType
		if insertType == "" {
			insertType = "code"
		}
		cell := map[string]interface{}{
			"id":        newID("nb-cell"),
			"cell_type": insertType,
			"metadata":  map[string]interface{}{},
			"source":    []interface{}{newSource},
		}
		if insertType == "code" {
			cell["outputs"] = []interface{}{}
			cell["execution_count"] = nil
		}
		cells = append(cells[:insertIndex], append([]map[string]interface{}{cell}, cells[insertIndex:]...)...)
	case "delete":
		if targetIndex < 0 {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "NotebookEdit" invocation failed`,
				Err:     errors.New("cell_id not found for delete"),
			}
		}
		cells = append(cells[:targetIndex], cells[targetIndex+1:]...)
	default:
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookEdit" invocation failed`,
			Err:     fmt.Errorf("unsupported edit_mode %q", mode),
		}
	}
	finalCells := make([]interface{}, 0, len(cells))
	for _, cell := range cells {
		finalCells = append(finalCells, cell)
	}
	nb["cells"] = finalCells
	encoded, err := json.MarshalIndent(nb, "", "  ")
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookEdit" invocation failed`,
			Err:     err,
		}
	}
	if err := writeTextFileWithMode(absPath, string(encoded)); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "NotebookEdit" invocation failed`,
			Err:     err,
		}
	}
	return fmt.Sprintf("NotebookEdit %s ok (mode=%s cells=%d)", absPath, mode, len(cells)), nil
}

func (s *Server) executeClaudeLSTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("find"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	pathValue := strings.TrimSpace(firstNonEmptyString(item, "path"))
	if pathValue == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "LS" invocation failed`,
			Err:     plugin.ErrFileLinesToolPathMissing,
		}
	}
	absPath, err := resolveClaudeToolPath(pathValue)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "LS" invocation failed`,
			Err:     err,
		}
	}
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "LS" invocation failed`,
			Err:     err,
		}
	}
	ignorePatterns := stringSliceFromAny(item["ignore"])
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if shouldSkipByGlob(name, ignorePatterns) {
			continue
		}
		if entry.IsDir() {
			lines = append(lines, name+"/")
		} else {
			lines = append(lines, name)
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return fmt.Sprintf("LS %s (empty)", absPath), nil
	}
	return fmt.Sprintf("LS %s (%d)\n%s", absPath, len(lines), strings.Join(lines, "\n")), nil
}

func (s *Server) executeClaudeGlobTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("find"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	pattern := strings.TrimSpace(firstNonEmptyString(item, "pattern"))
	if pattern == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Glob" invocation failed`,
			Err:     plugin.ErrFindToolPatternMissing,
		}
	}
	basePath := strings.TrimSpace(firstNonEmptyString(item, "path"))
	if basePath == "" {
		repoRoot, err := findRepoRoot()
		if err != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "Glob" invocation failed`,
				Err:     err,
			}
		}
		basePath = repoRoot
	}
	baseAbs, err := resolveClaudeToolPath(basePath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Glob" invocation failed`,
			Err:     err,
		}
	}
	matcher, err := compileClaudeGlobMatcher(pattern)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Glob" invocation failed`,
			Err:     err,
		}
	}
	type globMatch struct {
		Path    string
		ModTime int64
	}
	matches := make([]globMatch, 0, 64)
	_ = filepath.WalkDir(baseAbs, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(baseAbs, current)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if !matcher(relSlash) {
			return nil
		}
		info, infoErr := d.Info()
		modTime := int64(0)
		if infoErr == nil {
			modTime = info.ModTime().UnixNano()
		}
		matches = append(matches, globMatch{Path: current, ModTime: modTime})
		return nil
	})
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].ModTime == matches[j].ModTime {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].ModTime > matches[j].ModTime
	})
	if len(matches) == 0 {
		return fmt.Sprintf("Glob pattern=%q path=%s matched 0 file(s)", pattern, baseAbs), nil
	}
	lines := make([]string, 0, len(matches))
	for _, item := range matches {
		lines = append(lines, item.Path)
	}
	return fmt.Sprintf("Glob pattern=%q path=%s matched %d file(s)\n%s", pattern, baseAbs, len(lines), strings.Join(lines, "\n")), nil
}

func (s *Server) executeClaudeGrepTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("find"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	pattern := strings.TrimSpace(firstNonEmptyString(item, "pattern"))
	if pattern == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Grep" invocation failed`,
			Err:     plugin.ErrFindToolPatternMissing,
		}
	}
	ignoreCase := parseBoolAny(item["-i"])
	multiline := parseBoolAny(item["multiline"])
	expr := pattern
	if ignoreCase {
		expr = "(?i)" + expr
	}
	if multiline {
		expr = "(?s)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Grep" invocation failed`,
			Err:     err,
		}
	}
	basePath := strings.TrimSpace(firstNonEmptyString(item, "path"))
	if basePath == "" {
		repoRoot, rootErr := findRepoRoot()
		if rootErr != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "Grep" invocation failed`,
				Err:     rootErr,
			}
		}
		basePath = repoRoot
	}
	baseAbs, err := resolveClaudeToolPath(basePath)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "Grep" invocation failed`,
			Err:     err,
		}
	}
	globPattern := strings.TrimSpace(firstNonEmptyString(item, "glob"))
	var globMatcher func(string) bool
	if globPattern != "" {
		matcher, compileErr := compileClaudeGlobMatcher(globPattern)
		if compileErr != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "Grep" invocation failed`,
				Err:     compileErr,
			}
		}
		globMatcher = matcher
	}
	typeFilter := strings.TrimSpace(firstNonEmptyString(item, "type"))
	outputMode := strings.ToLower(strings.TrimSpace(firstNonEmptyString(item, "output_mode")))
	if outputMode == "" {
		outputMode = "files_with_matches"
	}
	showLineNo := parseBoolAny(item["-n"])
	afterCtx, _ := firstPositiveInt(item, "-A")
	beforeCtx, _ := firstPositiveInt(item, "-B")
	if ctx, ok := firstPositiveInt(item, "-C"); ok {
		afterCtx = ctx
		beforeCtx = ctx
	}
	headLimit, hasHeadLimit := firstPositiveInt(item, "head_limit")

	files := collectClaudeSearchFiles(baseAbs, globMatcher, typeFilter)
	fileMatches := make([]string, 0, 64)
	countLines := make([]string, 0, 64)
	contentLines := make([]string, 0, 128)
	for _, filePath := range files {
		raw, readErr := os.ReadFile(filePath)
		if readErr != nil {
			continue
		}
		if isLikelyBinary(raw) {
			continue
		}
		text := string(raw)
		if multiline {
			all := re.FindAllStringIndex(text, -1)
			if len(all) == 0 {
				continue
			}
			fileMatches = append(fileMatches, filePath)
			countLines = append(countLines, fmt.Sprintf("%s:%d", filePath, len(all)))
			if outputMode == "content" {
				for _, index := range all {
					segment := strings.TrimSpace(text[index[0]:index[1]])
					if segment == "" {
						continue
					}
					contentLines = append(contentLines, fmt.Sprintf("%s:%s", filePath, segment))
					if hasHeadLimit && len(contentLines) >= headLimit {
						break
					}
				}
			}
			continue
		}

		lines := splitIntoLines(text)
		matchedIndexes := make([]int, 0, 8)
		for index, line := range lines {
			if re.MatchString(line) {
				matchedIndexes = append(matchedIndexes, index)
			}
		}
		if len(matchedIndexes) == 0 {
			continue
		}
		fileMatches = append(fileMatches, filePath)
		countLines = append(countLines, fmt.Sprintf("%s:%d", filePath, len(matchedIndexes)))
		if outputMode != "content" {
			continue
		}
		include := map[int]struct{}{}
		for _, lineIndex := range matchedIndexes {
			start := lineIndex - beforeCtx
			if start < 0 {
				start = 0
			}
			end := lineIndex + afterCtx
			if end >= len(lines) {
				end = len(lines) - 1
			}
			for pos := start; pos <= end; pos++ {
				include[pos] = struct{}{}
			}
		}
		ordered := make([]int, 0, len(include))
		for index := range include {
			ordered = append(ordered, index)
		}
		sort.Ints(ordered)
		for _, index := range ordered {
			if showLineNo {
				contentLines = append(contentLines, fmt.Sprintf("%s:%d:%s", filePath, index+1, lines[index]))
			} else {
				contentLines = append(contentLines, fmt.Sprintf("%s:%s", filePath, lines[index]))
			}
			if hasHeadLimit && len(contentLines) >= headLimit {
				break
			}
		}
	}

	var output []string
	switch outputMode {
	case "count":
		output = countLines
	case "content":
		output = contentLines
	default:
		output = fileMatches
	}
	if hasHeadLimit && len(output) > headLimit {
		output = output[:headLimit]
	}
	if len(output) == 0 {
		return fmt.Sprintf("Grep pattern=%q matched 0 entries", pattern), nil
	}
	return strings.Join(output, "\n"), nil
}

func (s *Server) executeClaudeWebSearchTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("search"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	query := strings.TrimSpace(firstNonEmptyString(item, "query", "q"))
	if query == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "WebSearch" invocation failed`,
			Err:     plugin.ErrSearchToolQueryMissing,
		}
	}
	query = decorateQueryWithDomainFilters(query, stringSliceFromAny(item["allowed_domains"]), stringSliceFromAny(item["blocked_domains"]))
	searchItem := map[string]interface{}{
		"query": query,
	}
	if provider := strings.TrimSpace(firstNonEmptyString(item, "provider")); provider != "" {
		searchItem["provider"] = provider
	}
	if count, ok := firstPositiveInt(item, "count", "num_results"); ok {
		searchItem["count"] = count
	}
	if timeout, ok := firstPositiveInt(item, "timeout_seconds", "yield_time_ms"); ok {
		searchItem["timeout_seconds"] = timeout
	}
	result, err := s.invokeRegisteredTool("search", map[string]interface{}{
		"items": []interface{}{searchItem},
	})
	if err != nil {
		return "", err
	}
	return renderToolResult("WebSearch", result)
}

func (s *Server) executeClaudeWebFetchTool(input map[string]interface{}) (string, error) {
	if err := s.ensureToolEnabled("browser"); err != nil {
		return "", err
	}
	item := firstToolInputItem(input)
	urlValue := strings.TrimSpace(firstNonEmptyString(item, "url", "path"))
	promptValue := strings.TrimSpace(firstNonEmptyString(item, "prompt", "query", "task"))
	if urlValue == "" || promptValue == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "WebFetch" invocation failed`,
			Err:     plugin.ErrBrowserToolTaskMissing,
		}
	}
	normalizedURL := normalizeClaudeWebFetchURL(urlValue)
	browserItem := map[string]interface{}{
		"task": fmt.Sprintf("打开并阅读网页，然后按要求提取信息。\nURL: %s\n提取要求: %s", normalizedURL, promptValue),
	}
	if timeout, ok := firstPositiveInt(item, "timeout_seconds", "yield_time_ms"); ok {
		browserItem["timeout_seconds"] = timeout
	}
	result, err := s.invokeRegisteredTool("browser", map[string]interface{}{
		"items": []interface{}{browserItem},
	})
	if err != nil {
		return "", err
	}
	return renderToolResult("WebFetch", result)
}

func executeClaudeTaskTool(input map[string]interface{}) (string, error) {
	item := firstToolInputItem(input)
	description := strings.TrimSpace(firstNonEmptyString(item, "description"))
	prompt := strings.TrimSpace(firstNonEmptyString(item, "prompt"))
	subagentType := strings.TrimSpace(firstNonEmptyString(item, "subagent_type"))
	if description == "" && prompt == "" && subagentType == "" {
		return "Task emulation: no task payload provided.", nil
	}
	lines := []string{
		"Task emulation (NextAI single-agent fallback):",
		fmt.Sprintf("- description: %s", defaultIfEmpty(description, "(empty)")),
		fmt.Sprintf("- subagent_type: %s", defaultIfEmpty(subagentType, "(empty)")),
		fmt.Sprintf("- prompt: %s", defaultIfEmpty(prompt, "(empty)")),
		"Action: continue in current agent with explicit step-by-step progress updates.",
	}
	return strings.Join(lines, "\n"), nil
}

func executeClaudeTodoWriteTool(input map[string]interface{}) (string, error) {
	item := firstToolInputItem(input)
	rawTodos, ok := item["todos"].([]interface{})
	if !ok || len(rawTodos) == 0 {
		return "TodoWrite emulation: empty todo list.", nil
	}
	lines := make([]string, 0, len(rawTodos)+1)
	lines = append(lines, fmt.Sprintf("TodoWrite emulation: received %d todo item(s).", len(rawTodos)))
	for _, rawTodo := range rawTodos {
		todo, ok := rawTodo.(map[string]interface{})
		if !ok {
			continue
		}
		content := strings.TrimSpace(stringValue(todo["content"]))
		status := strings.TrimSpace(stringValue(todo["status"]))
		priority := strings.TrimSpace(stringValue(todo["priority"]))
		id := strings.TrimSpace(stringValue(todo["id"]))
		lines = append(lines, fmt.Sprintf("- [%s] (%s/%s) %s", defaultIfEmpty(id, "-"), defaultIfEmpty(status, "pending"), defaultIfEmpty(priority, "medium"), defaultIfEmpty(content, "(empty)")))
	}
	return strings.Join(lines, "\n"), nil
}

func executeClaudeExitPlanModeTool(input map[string]interface{}) (string, error) {
	item := firstToolInputItem(input)
	plan := strings.TrimSpace(firstNonEmptyString(item, "plan"))
	if plan == "" {
		return "ExitPlanMode emulation: no plan content.", nil
	}
	return "ExitPlanMode emulation: plan ready for user confirmation.\n" + plan, nil
}

func (s *Server) ensureToolEnabled(name string) error {
	if !s.toolDisabled(name) {
		return nil
	}
	return &toolError{
		Code:    "tool_disabled",
		Message: fmt.Sprintf("tool %q is disabled by server config", name),
	}
}

func resolveClaudeToolPath(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", plugin.ErrFileLinesToolPathMissing
	}
	if filepath.IsAbs(candidate) {
		return filepath.Clean(candidate), nil
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", plugin.ErrFileLinesToolPathInvalid
	}
	return filepath.Clean(filepath.Join(repoRoot, candidate)), nil
}

func writeTextFileWithMode(path string, content string) error {
	if strings.TrimSpace(path) == "" {
		return plugin.ErrFileLinesToolPathMissing
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	return os.WriteFile(path, []byte(content), perm)
}

func formatClaudeReadOutput(path string, raw string, offset int, limit int) string {
	lines := splitIntoLines(raw)
	start := offset
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	selected := lines[start:end]
	if len(selected) == 0 {
		return fmt.Sprintf("Read %s [empty]", path)
	}
	numbered := make([]string, 0, len(selected))
	for index, line := range selected {
		lineNo := start + index + 1
		numbered = append(numbered, fmt.Sprintf("%6d\t%s", lineNo, line))
	}
	return strings.Join(numbered, "\n")
}

func splitIntoLines(raw string) []string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func normalizeNotebookSource(raw interface{}) string {
	switch value := raw.(type) {
	case string:
		return value
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, stringValue(item))
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func stringSliceFromAny(raw interface{}) []string {
	switch value := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(stringValue(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(item)
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func compileClaudeGlobMatcher(pattern string) (func(string) bool, error) {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return nil, errors.New("glob pattern is required")
	}
	normalized := filepath.ToSlash(trimmed)
	placeholder := "__DOUBLE_STAR__PLACEHOLDER__"
	escaped := regexp.QuoteMeta(normalized)
	escaped = strings.ReplaceAll(escaped, "\\*\\*", placeholder)
	escaped = strings.ReplaceAll(escaped, "\\*", "[^/]*")
	escaped = strings.ReplaceAll(escaped, "\\?", "[^/]")
	escaped = strings.ReplaceAll(escaped, placeholder, ".*")
	re, err := regexp.Compile("^" + escaped + "$")
	if err != nil {
		return nil, err
	}
	return func(pathValue string) bool {
		return re.MatchString(filepath.ToSlash(strings.TrimSpace(pathValue)))
	}, nil
}

func shouldSkipByGlob(name string, patterns []string) bool {
	for _, pattern := range patterns {
		matcher, err := compileClaudeGlobMatcher(pattern)
		if err != nil {
			continue
		}
		if matcher(name) {
			return true
		}
	}
	return false
}

func parseBoolAny(raw interface{}) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	case float64:
		return value != 0
	case int:
		return value != 0
	case int64:
		return value != 0
	default:
		return false
	}
}

func parseNonNegativeIntAny(raw interface{}) (int, bool) {
	switch value := raw.(type) {
	case int:
		if value >= 0 {
			return value, true
		}
	case int32:
		if value >= 0 {
			return int(value), true
		}
	case int64:
		if value >= 0 {
			return int(value), true
		}
	case float64:
		number := int(value)
		if float64(number) == value && number >= 0 {
			return number, true
		}
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && number >= 0 {
			return number, true
		}
	}
	return 0, false
}

func collectClaudeSearchFiles(basePath string, globMatcher func(string) bool, typeFilter string) []string {
	normalizedType := strings.ToLower(strings.TrimSpace(typeFilter))
	files := make([]string, 0, 128)
	info, err := os.Stat(basePath)
	if err != nil {
		return files
	}
	addPath := func(pathValue string) {
		if normalizedType != "" {
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(pathValue)), ".")
			if ext != normalizedType {
				return
			}
		}
		if globMatcher != nil {
			rel := filepath.ToSlash(pathValue)
			if relPath, err := filepath.Rel(basePath, pathValue); err == nil {
				rel = filepath.ToSlash(relPath)
			}
			base := path.Base(rel)
			if !globMatcher(rel) && !globMatcher(base) {
				return
			}
		}
		files = append(files, pathValue)
	}
	if !info.IsDir() {
		addPath(basePath)
		return files
	}
	_ = filepath.WalkDir(basePath, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		addPath(current)
		return nil
	})
	sort.Strings(files)
	return files
}

func isLikelyBinary(raw []byte) bool {
	for _, b := range raw {
		if b == 0 {
			return true
		}
	}
	return false
}

func decorateQueryWithDomainFilters(query string, allowed []string, blocked []string) string {
	result := strings.TrimSpace(query)
	for _, domain := range allowed {
		trimmed := strings.TrimSpace(domain)
		if trimmed == "" {
			continue
		}
		result += " site:" + trimmed
	}
	for _, domain := range blocked {
		trimmed := strings.TrimSpace(domain)
		if trimmed == "" {
			continue
		}
		result += " -site:" + trimmed
	}
	return result
}

func normalizeClaudeWebFetchURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	if strings.EqualFold(strings.TrimSpace(parsed.Scheme), "http") {
		parsed.Scheme = "https"
		return parsed.String()
	}
	return trimmed
}

func defaultIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *Server) executeOpenToolCall(input map[string]interface{}) (string, error) {
	targetName, targetInput, routeErr := buildOpenToolRoute(input)
	if routeErr != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "open" invocation failed`,
			Err:     routeErr,
		}
	}
	switch targetName {
	case "view":
		if !s.toolHasDeclaredCapability("view", agentprotocolservice.ToolCapabilityOpenLocal) {
			return "", &toolError{
				Code:    "tool_not_supported",
				Message: `tool "open" local-path routing is not supported by current tool registry`,
			}
		}
	case "browser":
		if !s.toolHasDeclaredCapability("browser", agentprotocolservice.ToolCapabilityOpenURL) {
			return "", &toolError{
				Code:    "tool_not_supported",
				Message: `tool "open" url routing is not supported by current tool registry`,
			}
		}
	}
	result, err := s.invokeRegisteredTool(targetName, targetInput)
	if err != nil {
		return "", err
	}
	return renderToolResult("open", result)
}

func (s *Server) executeApproxBrowserToolCall(action string, input map[string]interface{}) (string, error) {
	requiredCapability := ""
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "click":
		requiredCapability = agentprotocolservice.ToolCapabilityApproxClick
	case "screenshot":
		requiredCapability = agentprotocolservice.ToolCapabilityApproxScreenshot
	}
	if requiredCapability != "" && !s.toolHasDeclaredCapability("browser", requiredCapability) {
		return "", &toolError{
			Code:    "tool_not_supported",
			Message: fmt.Sprintf(`tool %q is not supported by current tool registry`, action),
		}
	}
	browserInput, routeErr := buildApproxBrowserToolInput(action, input)
	if routeErr != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: fmt.Sprintf("tool %q invocation failed", action),
			Err:     routeErr,
		}
	}
	result, err := s.invokeRegisteredTool("browser", browserInput)
	if err != nil {
		return "", err
	}
	approx := map[string]interface{}{
		"mode":        "approx",
		"action":      action,
		"target_tool": "browser",
		"result":      result,
	}
	if ok, exists := result["ok"]; exists {
		approx["ok"] = ok
	}
	originalText := strings.TrimSpace(stringValue(result["text"]))
	if originalText == "" {
		approx["text"] = fmt.Sprintf("mode=approx action=%s routed_to=browser", action)
	} else {
		approx["text"] = fmt.Sprintf("mode=approx action=%s routed_to=browser\n%s", action, originalText)
	}
	return renderToolResult(action, approx)
}

func (s *Server) executeSelfOpsToolCall(input map[string]interface{}) (string, error) {
	payload := firstToolInputItem(input)
	action := strings.ToLower(strings.TrimSpace(stringValue(payload["action"])))
	if action == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "self_ops" invocation failed`,
			Err:     errors.New("self_ops action is required"),
		}
	}

	service := s.getSelfOpsService()
	if service == nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "self_ops" invocation failed`,
			Err:     errors.New("self_ops service is unavailable"),
		}
	}

	encodeResult := func(result interface{}) (string, error) {
		return renderToolResult("self_ops", map[string]interface{}{
			"ok":     true,
			"action": action,
			"result": result,
		})
	}
	wrapErr := func(err error) (string, error) {
		if err == nil {
			return "", nil
		}
		if serviceErr := (*selfopsservice.ServiceError)(nil); errors.As(err, &serviceErr) {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: fmt.Sprintf(`tool "self_ops" invocation failed: %s`, serviceErr.Code),
				Err:     err,
			}
		}
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "self_ops" invocation failed`,
			Err:     err,
		}
	}

	switch action {
	case "bootstrap_session":
		var req selfopsservice.BootstrapSessionInput
		if err := decodeSelfOpsInput(payload, &req); err != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "self_ops" invocation failed`,
				Err:     err,
			}
		}
		result, processErr, err := service.BootstrapSession(context.Background(), req)
		if processErr != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: fmt.Sprintf(`tool "self_ops" invocation failed: %s`, processErr.Code),
				Err:     processErr,
			}
		}
		if wrapped, wrapErrValue := wrapErr(err); wrapErrValue != nil {
			return wrapped, wrapErrValue
		}
		return encodeResult(result)
	case "set_session_model":
		var req selfopsservice.SetSessionModelInput
		if err := decodeSelfOpsInput(payload, &req); err != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "self_ops" invocation failed`,
				Err:     err,
			}
		}
		result, err := service.SetSessionModel(req)
		if wrapped, wrapErrValue := wrapErr(err); wrapErrValue != nil {
			return wrapped, wrapErrValue
		}
		return encodeResult(result)
	case "preview_mutation":
		var req selfopsservice.PreviewMutationInput
		if err := decodeSelfOpsInput(payload, &req); err != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "self_ops" invocation failed`,
				Err:     err,
			}
		}
		result, err := service.PreviewMutation(req)
		if wrapped, wrapErrValue := wrapErr(err); wrapErrValue != nil {
			return wrapped, wrapErrValue
		}
		return encodeResult(result)
	case "apply_mutation":
		var req selfopsservice.ApplyMutationInput
		if err := decodeSelfOpsInput(payload, &req); err != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "self_ops" invocation failed`,
				Err:     err,
			}
		}
		result, err := service.ApplyMutation(req)
		if wrapped, wrapErrValue := wrapErr(err); wrapErrValue != nil {
			return wrapped, wrapErrValue
		}
		return encodeResult(result)
	default:
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "self_ops" invocation failed`,
			Err:     fmt.Errorf("unsupported self_ops action %q", action),
		}
	}
}

func decodeSelfOpsInput(payload map[string]interface{}, out interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (s *Server) invokeRegisteredTool(name string, input map[string]interface{}) (map[string]interface{}, error) {
	normalized := normalizeToolName(strings.ToLower(strings.TrimSpace(name)))
	if normalized == "" {
		normalized = strings.ToLower(strings.TrimSpace(name))
	}
	if s.toolDisabled(normalized) {
		return nil, &toolError{
			Code:    "tool_disabled",
			Message: fmt.Sprintf("tool %q is disabled by server config", normalized),
		}
	}
	plug, ok := s.tools[normalized]
	if !ok {
		return nil, &toolError{
			Code:    "tool_not_supported",
			Message: fmt.Sprintf("tool %q is not supported", normalized),
		}
	}
	command, err := plugin.CommandFromMap(input)
	if err != nil {
		return nil, &toolError{
			Code:    "tool_invoke_failed",
			Message: fmt.Sprintf("tool %q invocation failed", normalized),
			Err:     err,
		}
	}
	result, err := plug.Invoke(command)
	if err != nil {
		return nil, &toolError{
			Code:    "tool_invoke_failed",
			Message: fmt.Sprintf("tool %q invocation failed", normalized),
			Err:     err,
		}
	}
	resultMap, err := result.ToMap()
	if err != nil {
		return nil, &toolError{
			Code:    "tool_invalid_result",
			Message: fmt.Sprintf("tool %q returned invalid result", normalized),
			Err:     err,
		}
	}
	return resultMap, nil
}

func renderToolResult(name string, result map[string]interface{}) (string, error) {
	if text, ok := result["text"].(string); ok && strings.TrimSpace(text) != "" {
		return text, nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invalid_result",
			Message: fmt.Sprintf("tool %q returned invalid result", name),
			Err:     err,
		}
	}
	return string(encoded), nil
}

func buildOpenToolRoute(input map[string]interface{}) (string, map[string]interface{}, error) {
	item := firstToolInputItem(input)
	pathOrURL := firstNonEmptyString(item, "path", "url", "ref_id")
	if pathOrURL == "" {
		return "", nil, plugin.ErrFileLinesToolPathMissing
	}
	if isHTTPURL(pathOrURL) {
		browserItem := map[string]interface{}{
			"task": fmt.Sprintf("打开 URL 并提取结构化摘要。\nURL: %s", strings.TrimSpace(pathOrURL)),
		}
		if timeout, ok := firstPositiveInt(item, "timeout_seconds", "yield_time_ms"); ok {
			browserItem["timeout_seconds"] = timeout
		}
		return "browser", map[string]interface{}{
			"items": []interface{}{browserItem},
		}, nil
	}
	if !filepath.IsAbs(pathOrURL) {
		return "", nil, plugin.ErrFileLinesToolPathInvalid
	}
	pathValue := filepath.Clean(pathOrURL)
	start, hasStart := firstPositiveInt(item, "start", "start_line", "lineno", "line")
	if !hasStart {
		start = 1
	}
	end, hasEnd := firstPositiveInt(item, "end", "end_line")
	if !hasEnd {
		if hasStart {
			end = start
		} else {
			end = start + 199
		}
	}
	if end < start {
		end = start
	}
	return "view", map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"path":  pathValue,
				"start": start,
				"end":   end,
			},
		},
	}, nil
}

func buildApproxBrowserToolInput(action string, input map[string]interface{}) (map[string]interface{}, error) {
	item := firstToolInputItem(input)
	task := strings.TrimSpace(firstNonEmptyString(item, "task", "query"))
	if task == "" {
		target := firstNonEmptyString(item, "url", "ref_id", "ref", "path")
		switch action {
		case "click":
			clickTarget := strings.TrimSpace(firstNonEmptyString(item, "selector", "id", "text", "target"))
			if target == "" && clickTarget == "" {
				return nil, plugin.ErrBrowserToolTaskMissing
			}
			lines := []string{
				"以近似模式执行 click 操作，访问目标并尝试点击后返回结构化摘要。",
				"要求：明确标注 mode=approx，且不保证页面会话状态连续。",
			}
			if target != "" {
				lines = append(lines, "目标: "+target)
			}
			if clickTarget != "" {
				lines = append(lines, "点击元素: "+clickTarget)
			}
			task = strings.Join(lines, "\n")
		case "screenshot":
			if target == "" {
				return nil, plugin.ErrBrowserToolTaskMissing
			}
			lines := []string{
				"以近似模式执行 screenshot 操作，访问目标并输出截图结果摘要。",
				"要求：明确标注 mode=approx，且不保证页面会话状态连续。",
				"目标: " + target,
			}
			if outputHint := strings.TrimSpace(firstNonEmptyString(item, "output", "output_path", "save_as")); outputHint != "" {
				lines = append(lines, "输出建议: "+outputHint)
			}
			task = strings.Join(lines, "\n")
		default:
			return nil, plugin.ErrBrowserToolTaskMissing
		}
	}
	browserItem := map[string]interface{}{"task": task}
	if timeout, ok := firstPositiveInt(item, "timeout_seconds", "yield_time_ms"); ok {
		browserItem["timeout_seconds"] = timeout
	}
	return map[string]interface{}{
		"items": []interface{}{browserItem},
	}, nil
}

func firstToolInputItem(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	rawItems, hasItems := input["items"]
	if hasItems && rawItems != nil {
		switch value := rawItems.(type) {
		case []interface{}:
			if len(value) > 0 {
				if first, ok := value[0].(map[string]interface{}); ok {
					return safeMap(first)
				}
			}
		case map[string]interface{}:
			return safeMap(value)
		}
	}
	return safeMap(input)
}

func firstNonEmptyString(input map[string]interface{}, keys ...string) string {
	if len(input) == 0 {
		return ""
	}
	for _, key := range keys {
		value := strings.TrimSpace(stringValue(input[key]))
		if value != "" {
			return value
		}
	}
	return ""
}

func firstPositiveInt(input map[string]interface{}, keys ...string) (int, bool) {
	if len(input) == 0 {
		return 0, false
	}
	for _, key := range keys {
		value, ok := parsePositiveIntAny(input[key])
		if ok {
			return value, true
		}
	}
	return 0, false
}

func parsePositiveIntAny(raw interface{}) (int, bool) {
	switch value := raw.(type) {
	case int:
		if value > 0 {
			return value, true
		}
	case int32:
		if value > 0 {
			return int(value), true
		}
	case int64:
		if value > 0 {
			return int(value), true
		}
	case float64:
		number := int(value)
		if float64(number) == value && number > 0 {
			return number, true
		}
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && number > 0 {
			return number, true
		}
	}
	return 0, false
}

func stringValue(v interface{}) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return ""
	}
}

func isHTTPURL(raw string) bool {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return false
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	return scheme == "http" || scheme == "https"
}

func (s *Server) resolveChannel(name string) (plugin.ChannelPlugin, map[string]interface{}, string, error) {
	channelName := strings.ToLower(strings.TrimSpace(name))
	if channelName == "" {
		return nil, nil, "", &channelError{Code: "invalid_channel", Message: "channel is required"}
	}
	plug, ok := s.channels[channelName]
	if !ok {
		return nil, nil, "", &channelError{
			Code:    "channel_not_supported",
			Message: fmt.Sprintf("channel %q is not supported", channelName),
		}
	}

	cfg := map[string]interface{}{}
	s.store.Read(func(st *repo.State) {
		if st.Channels == nil {
			return
		}
		raw := st.Channels[channelName]
		cfg = cloneChannelConfig(raw)
	})

	if !channelEnabled(channelName, cfg) {
		return nil, nil, "", &channelError{
			Code:    "channel_disabled",
			Message: fmt.Sprintf("channel %q is disabled", channelName),
		}
	}
	return plug, cfg, channelName, nil
}

func channelEnabled(name string, cfg map[string]interface{}) bool {
	if raw, ok := cfg["enabled"]; ok {
		return parseBool(raw)
	}
	return name == "console"
}

func parseBool(v interface{}) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	case float64:
		return value != 0
	case int:
		return value != 0
	case int64:
		return value != 0
	default:
		return false
	}
}

func cloneChannelConfig(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		out := map[string]interface{}{}
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		fallback := map[string]interface{}{}
		for key, value := range in {
			fallback[key] = value
		}
		return fallback
	}
	return out
}

func mapChannelError(err error) (status int, code string, message string) {
	var chErr *channelError
	if errors.As(err, &chErr) {
		switch chErr.Code {
		case "invalid_channel", "channel_not_supported", "channel_disabled":
			return http.StatusBadRequest, chErr.Code, chErr.Message
		case "channel_dispatch_failed":
			return http.StatusBadGateway, chErr.Code, chErr.Message
		default:
			return http.StatusBadGateway, "channel_dispatch_failed", "channel dispatch failed"
		}
	}
	return http.StatusBadGateway, "channel_dispatch_failed", "channel dispatch failed"
}

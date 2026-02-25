package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/provider"
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
	PromptHash           string                 `json:"prompt_hash,omitempty"`
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

	runtimeSnapshot, err := s.buildTurnRuntimeSnapshotForSystemLayers(
		promptMode,
		r.URL.Query().Get("task_command"),
		r.URL.Query().Get("session_id"),
	)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "invalid task_command", nil)
		return
	}

	compiled, err := s.compileSystemLayersForTurnRuntime(runtimeSnapshot)
	if err != nil {
		errorCode, errorMessage := promptUnavailableErrorForMode(promptMode)
		writeErr(w, http.StatusInternalServerError, errorCode, errorMessage, nil)
		return
	}

	resp := agentSystemLayersResponse{
		Version:     "v1",
		ModeVariant: s.resolvePromptModeVariant(promptMode),
		PromptHash:  compiled.Hash,
		Layers:      make([]agentSystemLayerView, 0, len(compiled.Layers)),
	}
	for _, compiledLayer := range compiled.Layers {
		layer := compiledLayer.Layer
		tokens := estimatePromptTokenCount(layer.Content)
		resp.EstimatedTokensTotal += tokens
		resp.Layers = append(resp.Layers, agentSystemLayerView{
			Name:            layer.Name,
			Role:            layer.Role,
			Source:          layer.Source,
			ContentPreview:  summarizeLayerPreview(layer.Content, 160),
			LayerHash:       compiledLayer.Hash,
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
		ProviderID: provider.NormalizeProviderID(state.ActiveLLM.ProviderID),
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
		providerID := provider.NormalizeProviderID(stringValue(value["provider_id"]))
		modelID := strings.TrimSpace(stringValue(value["model"]))
		if providerID == "" || modelID == "" {
			return domain.ModelSlotConfig{}, false
		}
		return domain.ModelSlotConfig{
			ProviderID: providerID,
			Model:      modelID,
		}, true
	case domain.ChatActiveLLMOverride:
		providerID := provider.NormalizeProviderID(value.ProviderID)
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
	default:
		return "", false
	}
}

func promptUnavailableErrorForMode(mode string) (string, string) {
	if _, ok := normalizePromptMode(mode); !ok {
		return "ai_tool_guide_unavailable", "ai tools guide is unavailable"
	}
	return "ai_tool_guide_unavailable", "ai tools guide is unavailable"
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
	snapshot := newTurnRuntimeSnapshot(promptMode, "")
	snapshot.AvailableTools = s.resolveAvailableToolDefinitionNames(snapshot.Mode.PromptMode)
	return s.listToolDefinitionsForTurnRuntime(snapshot)
}

func (s *Server) listToolDefinitionsForTurnRuntime(snapshot TurnRuntimeSnapshot) []runner.ToolDefinition {
	names := normalizeTurnRuntimeToolNames(snapshot.AvailableTools)
	if len(names) == 0 {
		names = s.resolveAvailableToolDefinitionNames(snapshot.Mode.PromptMode)
	}
	out := make([]runner.ToolDefinition, 0, len(names))
	for _, name := range names {
		if spec, ok := snapshot.runtimeToolSpecs[normalizeRuntimeToolName(name)]; ok {
			out = append(out, runner.ToolDefinition{
				Name:        spec.Name,
				Description: strings.TrimSpace(spec.Description),
				Parameters:  cloneJSONMap(spec.Parameters),
			})
			continue
		}
		out = append(out, buildToolDefinition(name))
	}
	return out
}

func (s *Server) resolveAvailableToolDefinitionNames(promptMode string) []string {
	registeredToolNames := make([]string, 0, len(s.tools))
	for name := range s.tools {
		registeredToolNames = append(registeredToolNames, name)
	}
	return agentprotocolservice.ListToolDefinitionNames(
		promptMode,
		registeredToolNames,
		s.toolHasDeclaredCapability,
		s.toolDisabled,
	)
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

func parseToolCall(
	bizParams map[string]interface{},
	rawRequest map[string]interface{},
	promptMode string,
	availableToolDefinitionNames []string,
) (toolCall, bool, error) {
	call, ok, err := agentprotocolservice.ParseToolCall(
		bizParams,
		rawRequest,
		promptMode,
		availableToolDefinitionNames,
	)
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

func parseShortcutToolCall(rawRequest map[string]interface{}, promptMode string, availableToolDefinitionNames []string) (toolCall, bool, error) {
	call, ok, err := agentprotocolservice.ParseShortcutToolCall(rawRequest, promptMode, availableToolDefinitionNames)
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

const (
	runtimeToolInputResultOverrideKey = "_nextai_runtime_result"
	runtimeToolInputDelegateToolKey   = "_nextai_runtime_tool"
	runtimeToolInputDelegateInputKey  = "_nextai_runtime_tool_input"
)

func (s *Server) executeToolCall(call toolCall) (string, error) {
	return s.executeToolCallForPromptMode(promptModeDefault, call)
}

func (s *Server) executeToolCallForPromptMode(promptMode string, call toolCall) (string, error) {
	return s.executeToolCallForPromptModeWithContext(context.Background(), promptMode, call)
}

func (s *Server) executeToolCallForPromptModeWithContext(ctx context.Context, promptMode string, call toolCall) (string, error) {
	name := normalizeToolNameForPromptMode(strings.ToLower(strings.TrimSpace(call.Name)), promptMode)
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(call.Name))
	}
	input := safeMap(call.Input)
	if err := validateShellToolSandboxPermissions(ctx, name, input); err != nil {
		return "", err
	}

	if s.toolDisabled(name) {
		return "", &toolError{
			Code:    "tool_disabled",
			Message: fmt.Sprintf("tool %q is disabled by server config", name),
		}
	}

	if runtimeSpec, ok := runtimeToolSpecFromContext(ctx, name); ok {
		return s.executeRuntimeToolCall(ctx, runtimeSpec, input)
	}

	switch name {
	case "apply_patch":
		return s.executeApplyPatchToolCall(ctx, input)
	case "open":
		return s.executeOpenToolCall(input)
	case "click", "screenshot":
		return s.executeApproxBrowserToolCall(name, input)
	case "request_user_input":
		return s.executeRequestUserInputToolCall(ctx, input)
	case "update_plan":
		return s.executeUpdatePlanToolCall(input)
	case "output_plan":
		return s.executeOutputPlanToolCall(input)
	case "spawn_agent":
		return s.executeSpawnAgentToolCall(ctx, input)
	case "send_input":
		return s.executeSendInputToolCall(input)
	case "resume_agent":
		return s.executeResumeAgentToolCall(ctx, input)
	case "wait":
		return s.executeWaitAgentToolCall(ctx, input)
	case "close_agent":
		return s.executeCloseAgentToolCall(input)
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

func validateShellToolSandboxPermissions(ctx context.Context, toolName string, input map[string]interface{}) error {
	if !strings.EqualFold(strings.TrimSpace(toolName), "shell") {
		return nil
	}
	if !shellToolRequestsEscalatedSandboxPermissions(input) {
		return nil
	}

	approvalPolicy := defaultTurnApprovalPolicy
	if policy, _, ok := runtimeToolPoliciesFromContext(ctx); ok && strings.TrimSpace(policy) != "" {
		approvalPolicy = strings.TrimSpace(policy)
	}
	normalizedPolicy := strings.ToLower(strings.TrimSpace(approvalPolicy))
	if normalizedPolicy == "on_request" || normalizedPolicy == "on-request" {
		return nil
	}
	return &toolError{
		Code:    "tool_invoke_failed",
		Message: `tool "shell" invocation failed`,
		Err:     plugin.ErrShellToolEscalationDenied,
	}
}

func shellToolRequestsEscalatedSandboxPermissions(input map[string]interface{}) bool {
	if len(input) == 0 {
		return false
	}
	if isEscalatedShellSandboxPermission(stringValue(input["sandbox_permissions"])) {
		return true
	}

	rawItems, hasItems := input["items"]
	if !hasItems || rawItems == nil {
		return false
	}
	switch value := rawItems.(type) {
	case []interface{}:
		for _, raw := range value {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if isEscalatedShellSandboxPermission(stringValue(item["sandbox_permissions"])) {
				return true
			}
		}
	case map[string]interface{}:
		return isEscalatedShellSandboxPermission(stringValue(value["sandbox_permissions"]))
	}
	return false
}

func isEscalatedShellSandboxPermission(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "require_escalated", "require_escalated_permissions":
		return true
	default:
		return false
	}
}

func (s *Server) executeRuntimeToolCall(_ context.Context, runtimeSpec turnRuntimeToolSpec, input map[string]interface{}) (string, error) {
	if override, exists := input[runtimeToolInputResultOverrideKey]; exists {
		switch value := override.(type) {
		case string:
			text := strings.TrimSpace(value)
			if text != "" {
				return text, nil
			}
		case map[string]interface{}:
			return renderToolResult(runtimeSpec.Name, value)
		}
	}

	targetName, targetInput, ok := s.resolveRuntimeToolDelegate(runtimeSpec, input)
	if !ok {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: fmt.Sprintf("tool %q invocation failed", runtimeSpec.Name),
			Err:     fmt.Errorf("runtime %s tool has no gateway executor configured", runtimeSpec.Source),
		}
	}

	result, err := s.invokeRegisteredTool(targetName, targetInput)
	if err != nil {
		return "", err
	}
	return renderToolResult(runtimeSpec.Name, result)
}

func (s *Server) resolveRuntimeToolDelegate(runtimeSpec turnRuntimeToolSpec, input map[string]interface{}) (string, map[string]interface{}, bool) {
	runtimeInput := cloneJSONMap(safeMap(input))
	if len(runtimeInput) == 0 {
		runtimeInput = map[string]interface{}{}
	}
	delete(runtimeInput, runtimeToolInputResultOverrideKey)

	delegateName := normalizeRuntimeToolDelegateName(stringValue(runtimeInput[runtimeToolInputDelegateToolKey]))
	delegateInput := schemaMapFromAny(runtimeInput[runtimeToolInputDelegateInputKey])
	delete(runtimeInput, runtimeToolInputDelegateToolKey)
	delete(runtimeInput, runtimeToolInputDelegateInputKey)
	if delegateName != "" {
		return delegateName, mergeRuntimeToolDelegateInput(delegateInput, runtimeInput), true
	}

	if runtimeSpec.GatewayTool != "" {
		return runtimeSpec.GatewayTool, mergeRuntimeToolDelegateInput(runtimeSpec.GatewayInput, runtimeInput), true
	}

	if _, exists := s.tools[runtimeSpec.Name]; exists {
		return runtimeSpec.Name, runtimeInput, true
	}
	return "", nil, false
}

func mergeRuntimeToolDelegateInput(base map[string]interface{}, overlay map[string]interface{}) map[string]interface{} {
	if len(base) == 0 && len(overlay) == 0 {
		return map[string]interface{}{}
	}
	merged := cloneJSONMap(base)
	for key, value := range overlay {
		merged[key] = value
	}
	return merged
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

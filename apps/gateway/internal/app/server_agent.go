package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
	agentservice "nextai/apps/gateway/internal/service/agent"
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
	if req.SessionID == "" || req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "session_id and user_id are required", nil)
		return
	}
	req.Channel = resolveProcessRequestChannel(r, req.Channel)
	channelPlugin, channelCfg, channelName, err := s.resolveChannel(req.Channel)
	if err != nil {
		status, code, message := mapChannelError(err)
		writeErr(w, status, code, message, nil)
		return
	}
	req.Channel = channelName
	if isContextResetCommand(req.Input) {
		if err := s.clearChatContext(req.SessionID, req.UserID, req.Channel); err != nil {
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
			return
		}
		dispatchCfg := mergeChannelDispatchConfig(channelName, channelCfg, req.BizParams)
		if err := channelPlugin.SendText(r.Context(), req.UserID, req.SessionID, contextResetReply, dispatchCfg); err != nil {
			status, code, message := mapChannelError(&channelError{
				Code:    "channel_dispatch_failed",
				Message: fmt.Sprintf("failed to dispatch message to channel %q", channelName),
				Err:     err,
			})
			writeErr(w, status, code, message, nil)
			return
		}
		writeImmediateAgentResponse(w, req.Stream, contextResetReply)
		return
	}

	requestPromptMode, hasRequestPromptMode, err := parsePromptModeFromBizParams(req.BizParams)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	effectivePromptMode := requestPromptMode
	if !hasRequestPromptMode {
		effectivePromptMode = promptModeDefault
		s.store.Read(func(state *repo.State) {
			for _, chat := range state.Chats {
				if chat.SessionID != req.SessionID || chat.UserID != req.UserID || chat.Channel != req.Channel {
					continue
				}
				effectivePromptMode = resolvePromptModeFromChatMeta(chat.Meta)
				return
			}
		})
	}
	reviewTaskRequested := effectivePromptMode == promptModeCodex && isReviewTaskCommand(req.Input)
	compactTaskRequested := effectivePromptMode == promptModeCodex && isCompactTaskCommand(req.Input)
	memoryTaskRequested := effectivePromptMode == promptModeCodex && isMemoryTaskCommand(req.Input)
	collaborationMode := collaborationModeDefaultName
	if effectivePromptMode == promptModeCodex {
		switch {
		case isPlanTaskCommand(req.Input):
			collaborationMode = collaborationModePlanName
		case isExecuteTaskCommand(req.Input):
			collaborationMode = collaborationModeExecuteName
		case isPairTaskCommand(req.Input):
			collaborationMode = collaborationModePairProgrammingName
		}
	}

	systemLayers, err := s.buildSystemLayersForModeWithOptions(effectivePromptMode, codexLayerBuildOptions{
		SessionID:         req.SessionID,
		ReviewTask:        reviewTaskRequested,
		CompactTask:       compactTaskRequested,
		MemoryTask:        memoryTaskRequested,
		CollaborationMode: collaborationMode,
	})
	if err != nil {
		errorCode, errorMessage := promptUnavailableErrorForMode(effectivePromptMode)
		writeErr(w, http.StatusInternalServerError, errorCode, errorMessage, nil)
		return
	}

	cronChatMeta := cronChatMetaFromBizParams(req.BizParams)
	chatID := ""
	activeLLM := domain.ModelSlotConfig{}
	providerSetting := repo.ProviderSetting{}
	historyInput := []domain.AgentInputMessage{}
	if err := s.store.Write(func(state *repo.State) error {
		for id, c := range state.Chats {
			if c.SessionID == req.SessionID && c.UserID == req.UserID && c.Channel == req.Channel {
				chatID = id
				break
			}
		}
		if chatID == "" {
			chatID = newID("chat")
			now := nowISO()
			state.Chats[chatID] = domain.ChatSpec{
				ID: chatID, Name: "New Chat", SessionID: req.SessionID, UserID: req.UserID, Channel: req.Channel,
				Meta: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now,
			}
		}
		if hasRequestPromptMode {
			chat := state.Chats[chatID]
			if chat.Meta == nil {
				chat.Meta = map[string]interface{}{}
			}
			chat.Meta[chatMetaPromptModeKey] = requestPromptMode
			state.Chats[chatID] = chat
		}
		if len(cronChatMeta) > 0 {
			chat := state.Chats[chatID]
			if chat.Meta == nil {
				chat.Meta = map[string]interface{}{}
			}
			for key, value := range cronChatMeta {
				chat.Meta[key] = value
			}
			state.Chats[chatID] = chat
		}
		for _, input := range req.Input {
			state.Histories[chatID] = append(state.Histories[chatID], domain.RuntimeMessage{
				ID:      newID("msg"),
				Role:    input.Role,
				Type:    input.Type,
				Content: toRuntimeContents(input.Content),
			})
		}
		historyInput = runtimeHistoryToAgentInputMessages(state.Histories[chatID])
		activeLLM = state.ActiveLLM
		activeLLM.ProviderID = normalizeProviderID(activeLLM.ProviderID)
		providerSetting = getProviderSettingByID(state, activeLLM.ProviderID)
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	requestedToolCall, hasToolCall, err := parseToolCall(req.BizParams, rawRequest)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_tool_input", err.Error(), nil)
		return
	}

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

	reply := ""
	events := make([]domain.AgentEvent, 0, 12)
	memoryRolloutContents := ""
	effectiveInput := []domain.AgentInputMessage{}
	generateConfig := runner.GenerateConfig{
		PromptCacheKey: req.SessionID,
	}
	if !hasToolCall {
		if activeLLM.ProviderID == "" || strings.TrimSpace(activeLLM.Model) == "" {
			generateConfig = runner.GenerateConfig{
				ProviderID:         runner.ProviderDemo,
				Model:              "demo-chat",
				AdapterID:          provider.AdapterDemo,
				PromptCacheKey:     req.SessionID,
				PreviousResponseID: latestProviderResponseIDFromInput(historyInput),
			}
		} else {
			if !providerEnabled(providerSetting) {
				streamFail(http.StatusBadRequest, "provider_disabled", "active provider is disabled", nil)
				return
			}
			resolvedModel, ok := provider.ResolveModelID(activeLLM.ProviderID, activeLLM.Model, providerSetting.ModelAliases)
			if !ok {
				streamFail(http.StatusBadRequest, "model_not_found", "active model is not available for provider", nil)
				return
			}
			activeLLM.Model = resolvedModel
			generateConfig = runner.GenerateConfig{
				ProviderID:         activeLLM.ProviderID,
				Model:              activeLLM.Model,
				APIKey:             resolveProviderAPIKey(activeLLM.ProviderID, providerSetting),
				BaseURL:            resolveProviderBaseURL(activeLLM.ProviderID, providerSetting),
				AdapterID:          provider.ResolveAdapter(activeLLM.ProviderID),
				Headers:            sanitizeStringMap(providerSetting.Headers),
				TimeoutMS:          providerSetting.TimeoutMS,
				Store:              providerStoreEnabled(providerSetting),
				PromptCacheKey:     req.SessionID,
				PreviousResponseID: latestProviderResponseIDFromInput(historyInput),
			}
		}
		if len(historyInput) > 0 {
			effectiveInput = prependSystemLayers(historyInput, systemLayers)
		} else {
			effectiveInput = prependSystemLayers(req.Input, systemLayers)
		}
	}
	completedEventMeta := buildCompletedModelRequestMeta(effectivePromptMode, systemLayers, effectiveInput, generateConfig)
	emitEvent := func(evt domain.AgentEvent) {
		evt = withCompletedEventMeta(evt, completedEventMeta)
		if !streaming {
			return
		}
		payload, _ := json.Marshal(evt)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		streamStarted = true
	}

	processResult, processErr := s.getAgentService().Process(
		r.Context(),
		agentservice.ProcessParams{
			Request: req,
			RequestedToolCall: agentservice.ToolCall{
				Name:  requestedToolCall.Name,
				Input: requestedToolCall.Input,
			},
			HasToolCall:    hasToolCall,
			Streaming:      streaming,
			ReplyChunkSize: replyChunkSizeDefault,
			GenerateConfig: generateConfig,
			EffectiveInput: effectiveInput,
			PromptMode:     effectivePromptMode,
		},
		emitEvent,
	)
	if processErr != nil {
		streamFail(processErr.Status, processErr.Code, processErr.Message, processErr.Details)
		return
	}
	reply = processResult.Reply
	events = withCompletedEventMetaForEvents(processResult.Events, completedEventMeta)

	assistant := domain.RuntimeMessage{
		ID:      newID("msg"),
		Role:    "assistant",
		Type:    "message",
		Content: []domain.RuntimeContent{{Type: "text", Text: reply}},
	}
	metadata := buildAssistantMessageMetadata(events)
	if responseID := strings.TrimSpace(processResult.ProviderResponseID); responseID != "" {
		if metadata == nil {
			metadata = map[string]interface{}{}
		}
		metadata[assistantMetadataProviderResponseIDKey] = responseID
	}
	if len(metadata) > 0 {
		assistant.Metadata = metadata
	}

	_ = s.store.Write(func(state *repo.State) error {
		state.Histories[chatID] = append(state.Histories[chatID], assistant)
		if memoryTaskRequested && !hasToolCall {
			memoryRolloutContents = serializeCodexMemoryRollout(state.Histories[chatID])
		}
		chat := state.Chats[chatID]
		chat.UpdatedAt = nowISO()
		if chat.Name == "New Chat" && len(req.Input) > 0 && len(req.Input[0].Content) > 0 {
			first := strings.TrimSpace(req.Input[0].Content[0].Text)
			if first != "" {
				if len([]rune(first)) > 20 {
					chat.Name = string([]rune(first)[:20])
				} else {
					chat.Name = first
				}
			}
		}
		state.Chats[chatID] = chat
		return nil
	})

	dispatchCfg := mergeChannelDispatchConfig(channelName, channelCfg, req.BizParams)
	if err := channelPlugin.SendText(r.Context(), req.UserID, req.SessionID, reply, dispatchCfg); err != nil {
		status, code, message := mapChannelError(&channelError{
			Code:    "channel_dispatch_failed",
			Message: fmt.Sprintf("failed to dispatch message to channel %q", channelName),
			Err:     err,
		})
		streamFail(status, code, message, nil)
		return
	}

	if memoryTaskRequested && !hasToolCall {
		s.startCodexMemoryPipeline(req.SessionID, generateConfig, memoryRolloutContents)
	}

	if !streaming {
		writeJSON(w, http.StatusOK, domain.AgentProcessResponse{
			Reply:  reply,
			Events: events,
		})
		return
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
	if isQQInboundRequest(r) {
		return qqChannelName
	}
	if requested := strings.ToLower(strings.TrimSpace(requestedChannel)); requested != "" {
		return requested
	}
	return defaultProcessChannel
}

func isQQInboundRequest(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.URL.Path)), qqInboundPath)
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
	raw := map[string]interface{}{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return qqInboundEvent{}, errors.New("invalid request body")
	}

	payload := raw
	if nested, ok := qqMap(raw["d"]); ok {
		payload = nested
	} else if nested, ok := qqMap(raw["data"]); ok {
		payload = nested
	}

	eventName := strings.ToUpper(qqFirst(
		qqString(raw["event"]),
		qqString(raw["event_type"]),
		qqString(raw["type"]),
		qqString(raw["t"]),
	))
	targetType, _ := normalizeQQTargetTypeAlias(strings.ToLower(eventName))
	if targetType == "" {
		targetType, _ = normalizeQQTargetTypeAlias(qqFirst(
			qqString(payload["message_type"]),
			qqString(payload["target_type"]),
		))
	}

	switch eventName {
	case "C2C_MESSAGE_CREATE":
		targetType = "c2c"
	case "GROUP_AT_MESSAGE_CREATE":
		targetType = "group"
	case "AT_MESSAGE_CREATE", "DIRECT_MESSAGE_CREATE":
		targetType = "guild"
	}
	if targetType == "" {
		return qqInboundEvent{}, errors.New("unsupported qq event type")
	}

	author, _ := qqMap(payload["author"])
	sender, _ := qqMap(payload["sender"])
	text := strings.TrimSpace(qqFirst(qqString(payload["content"]), qqString(payload["text"])))
	if text == "" {
		return qqInboundEvent{}, nil
	}

	event := qqInboundEvent{
		Text:      text,
		MessageID: strings.TrimSpace(qqString(payload["id"])),
	}

	switch targetType {
	case "c2c":
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["user_openid"]),
			qqString(author["id"]),
			qqString(sender["user_openid"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		targetID := strings.TrimSpace(qqFirst(
			qqString(payload["target_id"]),
			senderID,
		))
		if targetID == "" {
			return qqInboundEvent{}, errors.New("qq c2c event missing sender id")
		}
		userID := strings.TrimSpace(qqFirst(senderID, targetID))
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:c2c:%s", targetID),
		))
		event.UserID = userID
		event.SessionID = sessionID
		event.TargetType = "c2c"
		event.TargetID = targetID
	case "group":
		groupID := strings.TrimSpace(qqFirst(
			qqString(payload["group_openid"]),
			qqString(payload["target_id"]),
			qqString(payload["group_id"]),
		))
		if groupID == "" {
			return qqInboundEvent{}, errors.New("qq group event missing group_openid")
		}
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["member_openid"]),
			qqString(author["user_openid"]),
			qqString(author["id"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		if senderID == "" {
			senderID = groupID
		}
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:group:%s:%s", groupID, senderID),
		))
		event.UserID = senderID
		event.SessionID = sessionID
		event.TargetType = "group"
		event.TargetID = groupID
	case "guild":
		channelID := strings.TrimSpace(qqFirst(
			qqString(payload["channel_id"]),
			qqString(payload["target_id"]),
		))
		if channelID == "" {
			return qqInboundEvent{}, errors.New("qq guild event missing channel_id")
		}
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["id"]),
			qqString(author["username"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		if senderID == "" {
			senderID = channelID
		}
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:guild:%s:%s", channelID, senderID),
		))
		event.UserID = senderID
		event.SessionID = sessionID
		event.TargetType = "guild"
		event.TargetID = channelID
	}

	if event.UserID == "" || event.SessionID == "" || event.TargetID == "" {
		return qqInboundEvent{}, errors.New("qq inbound event missing required fields")
	}
	return event, nil
}

func mergeChannelDispatchConfig(channelName string, cfg map[string]interface{}, bizParams map[string]interface{}) map[string]interface{} {
	if channelName != "qq" || len(bizParams) == 0 {
		return cfg
	}
	raw, ok := bizParams["channel"]
	if !ok || raw == nil {
		return cfg
	}
	body, ok := raw.(map[string]interface{})
	if !ok {
		return cfg
	}
	merged := cloneChannelConfig(cfg)
	updated := false

	if canonical, ok := normalizeQQTargetTypeAlias(qqString(body["target_type"])); ok {
		merged["target_type"] = canonical
		updated = true
	}
	if targetID := strings.TrimSpace(qqString(body["target_id"])); targetID != "" {
		merged["target_id"] = targetID
		updated = true
	}
	if msgID := strings.TrimSpace(qqString(body["msg_id"])); msgID != "" {
		merged["msg_id"] = msgID
		updated = true
	}
	if botPrefix := qqString(body["bot_prefix"]); strings.TrimSpace(botPrefix) != "" {
		merged["bot_prefix"] = botPrefix
		updated = true
	}
	if !updated {
		return cfg
	}
	return merged
}

func cronChatMetaFromBizParams(bizParams map[string]interface{}) map[string]interface{} {
	if len(bizParams) == 0 {
		return nil
	}
	raw, ok := bizParams["cron"]
	if !ok || raw == nil {
		return nil
	}
	cronPayload, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	jobID := strings.TrimSpace(qqString(cronPayload["job_id"]))
	jobName := strings.TrimSpace(qqString(cronPayload["job_name"]))
	if jobID == "" && jobName == "" {
		return nil
	}
	meta := map[string]interface{}{
		"source": "cron",
	}
	if jobID != "" {
		meta["cron_job_id"] = jobID
	}
	if jobName != "" {
		meta["cron_job_name"] = jobName
	}
	return meta
}

func parsePromptModeFromBizParams(bizParams map[string]interface{}) (string, bool, error) {
	if len(bizParams) == 0 {
		return promptModeDefault, false, nil
	}
	rawPromptMode, hasPromptMode := bizParams[chatMetaPromptModeKey]
	if !hasPromptMode {
		return promptModeDefault, false, nil
	}
	value, ok := rawPromptMode.(string)
	if !ok {
		return "", true, errors.New("invalid prompt_mode")
	}
	mode, ok := normalizePromptMode(value)
	if !ok {
		return "", true, errors.New("invalid prompt_mode")
	}
	return mode, true, nil
}

func resolvePromptModeFromChatMeta(meta map[string]interface{}) string {
	if len(meta) == 0 {
		return promptModeDefault
	}
	rawMode, ok := meta[chatMetaPromptModeKey]
	if !ok || rawMode == nil {
		return promptModeDefault
	}
	value, ok := rawMode.(string)
	if !ok {
		return promptModeDefault
	}
	mode, ok := normalizePromptMode(value)
	if !ok {
		return promptModeDefault
	}
	return mode
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
	if len(s.tools) == 0 {
		return nil
	}
	nameSet := map[string]struct{}{}
	for name := range s.tools {
		if s.toolDisabled(name) {
			continue
		}
		nameSet[name] = struct{}{}
	}

	_, hasView := nameSet["view"]
	_, hasBrowser := nameSet["browser"]
	if (hasView || hasBrowser) && !s.toolDisabled("open") {
		nameSet["open"] = struct{}{}
	}
	if hasBrowser {
		if !s.toolDisabled("click") {
			nameSet["click"] = struct{}{}
		}
		if !s.toolDisabled("screenshot") {
			nameSet["screenshot"] = struct{}{}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]runner.ToolDefinition, 0, len(names))
	for _, name := range names {
		out = append(out, buildToolDefinition(name))
	}
	return out
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
			Description: "Replace line ranges for one or multiple files. input must be an array.",
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

func parseToolCall(bizParams map[string]interface{}, rawRequest map[string]interface{}) (toolCall, bool, error) {
	if call, ok, err := parseBizParamsToolCall(bizParams); ok || err != nil {
		return call, ok, err
	}
	return parseShortcutToolCall(rawRequest)
}

func parseBizParamsToolCall(bizParams map[string]interface{}) (toolCall, bool, error) {
	if len(bizParams) == 0 {
		return toolCall{}, false, nil
	}
	raw, ok := bizParams["tool"]
	if !ok || raw == nil {
		return toolCall{}, false, nil
	}
	toolBody, ok := raw.(map[string]interface{})
	if !ok {
		return toolCall{}, false, errors.New("biz_params.tool must be an object")
	}
	rawName, ok := toolBody["name"]
	if !ok {
		return toolCall{}, false, errors.New("biz_params.tool.name is required")
	}
	name, ok := rawName.(string)
	if !ok {
		return toolCall{}, false, errors.New("biz_params.tool.name must be a string")
	}
	name = normalizeToolName(strings.ToLower(strings.TrimSpace(name)))
	if name == "" {
		return toolCall{}, false, errors.New("biz_params.tool.name cannot be empty")
	}
	rawInput, hasInput := toolBody["input"]
	if !hasInput {
		body := map[string]interface{}{}
		for key, value := range toolBody {
			if key == "name" {
				continue
			}
			body[key] = value
		}
		rawInput = body
	}
	input, err := parseToolPayload(rawInput, "biz_params.tool")
	if err != nil {
		return toolCall{}, false, err
	}
	return toolCall{Name: name, Input: input}, true, nil
}

func parseShortcutToolCall(rawRequest map[string]interface{}) (toolCall, bool, error) {
	if len(rawRequest) == 0 {
		return toolCall{}, false, nil
	}
	shortcuts := []string{"view", "edit", "shell", "browser", "search", "open", "find", "click", "screenshot"}
	matched := make([]string, 0, 1)
	for _, key := range shortcuts {
		if raw, ok := rawRequest[key]; ok && raw != nil {
			matched = append(matched, key)
		}
	}
	if len(matched) == 0 {
		return toolCall{}, false, nil
	}
	if len(matched) > 1 {
		return toolCall{}, false, errors.New("only one shortcut tool key is allowed")
	}
	name := matched[0]
	input, err := parseToolPayload(rawRequest[name], name)
	if err != nil {
		return toolCall{}, false, err
	}
	return toolCall{Name: normalizeToolName(name), Input: input}, true, nil
}

func parseToolPayload(raw interface{}, path string) (map[string]interface{}, error) {
	if raw == nil {
		return map[string]interface{}{}, nil
	}
	switch value := raw.(type) {
	case []interface{}:
		return map[string]interface{}{"items": value}, nil
	case map[string]interface{}:
		if nested, ok := value["input"]; ok {
			return parseToolPayload(nested, path+".input")
		}
		return safeMap(value), nil
	default:
		return nil, fmt.Errorf("%s must be an object or array", path)
	}
}

func normalizeToolName(name string) string {
	switch name {
	case "view_file_lines", "view_file_lins", "view_file":
		return "view"
	case "edit_file_lines", "edit_file_lins", "edit_file":
		return "edit"
	case "exec_command", "functions.exec_command":
		return "shell"
	case "web_browser", "browser_use", "browser_tool":
		return "browser"
	case "web_search", "search_api", "search_tool":
		return "search"
	case "open":
		return "open"
	case "find":
		return "find"
	case "click":
		return "click"
	case "screenshot":
		return "screenshot"
	default:
		return name
	}
}

func (s *Server) executeToolCall(call toolCall) (string, error) {
	name := normalizeToolName(strings.ToLower(strings.TrimSpace(call.Name)))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(call.Name))
	}
	input := safeMap(call.Input)

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
	default:
		result, err := s.invokeRegisteredTool(name, input)
		if err != nil {
			return "", err
		}
		return renderToolResult(name, result)
	}
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
	result, err := s.invokeRegisteredTool(targetName, targetInput)
	if err != nil {
		return "", err
	}
	return renderToolResult("open", result)
}

func (s *Server) executeApproxBrowserToolCall(action string, input map[string]interface{}) (string, error) {
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
	result, err := plug.Invoke(input)
	if err != nil {
		return nil, &toolError{
			Code:    "tool_invoke_failed",
			Message: fmt.Sprintf("tool %q invocation failed", normalized),
			Err:     err,
		}
	}
	return result, nil
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

package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
	agentservice "nextai/apps/gateway/internal/service/agent"
	"nextai/apps/gateway/internal/service/ports"
)

func (s *Server) processAgentViaPort(
	ctx context.Context,
	req domain.AgentProcessRequest,
) (domain.AgentProcessResponse, *ports.AgentProcessError) {
	if req.Stream {
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "streaming is not supported for internal agent port",
		}
	}
	req.Channel = resolveProcessRequestChannel(nil, req.Channel)
	return s.processAgentCore(ctx, req, nil, false, nil)
}

func (s *Server) processAgentCore(
	ctx context.Context,
	req domain.AgentProcessRequest,
	rawRequest map[string]interface{},
	streaming bool,
	emit func(domain.AgentEvent),
) (domain.AgentProcessResponse, *ports.AgentProcessError) {
	if req.SessionID == "" || req.UserID == "" {
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "session_id and user_id are required",
		}
	}

	channelPlugin, channelCfg, channelName, err := s.resolveChannel(req.Channel)
	if err != nil {
		status, code, message := mapChannelError(err)
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  status,
			Code:    code,
			Message: message,
		}
	}
	req.Channel = channelName

	if isContextResetCommand(req.Input) {
		if err := s.clearChatContext(req.SessionID, req.UserID, req.Channel); err != nil {
			return domain.AgentProcessResponse{}, &ports.AgentProcessError{
				Status:  http.StatusInternalServerError,
				Code:    "store_error",
				Message: err.Error(),
			}
		}
		dispatchCfg := mergeChannelDispatchConfig(channelName, channelCfg, req.BizParams)
		if err := channelPlugin.SendText(ctx, req.UserID, req.SessionID, contextResetReply, dispatchCfg); err != nil {
			status, code, message := mapChannelError(&channelError{
				Code:    "channel_dispatch_failed",
				Message: fmt.Sprintf("failed to dispatch message to channel %q", channelName),
				Err:     err,
			})
			return domain.AgentProcessResponse{}, &ports.AgentProcessError{
				Status:  status,
				Code:    code,
				Message: message,
			}
		}
		resp := immediateAgentProcessResponse(contextResetReply)
		if streaming && emit != nil {
			for _, evt := range resp.Events {
				emit(evt)
			}
		}
		return resp, nil
	}

	requestPromptMode, hasRequestPromptMode, err := parsePromptModeFromBizParams(req.BizParams)
	if err != nil {
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: err.Error(),
		}
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
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  http.StatusInternalServerError,
			Code:    errorCode,
			Message: errorMessage,
		}
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
		chatSpec := state.Chats[chatID]
		activeLLM = resolveChatActiveModelSlot(chatSpec.Meta, state)
		providerSetting = getProviderSettingByID(state, activeLLM.ProviderID)
		return nil
	}); err != nil {
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  http.StatusInternalServerError,
			Code:    "store_error",
			Message: err.Error(),
		}
	}

	toolRawRequest := rawRequest
	if toolRawRequest == nil {
		toolRawRequest = map[string]interface{}{}
	}
	requestedToolCall, hasToolCall, err := parseToolCall(req.BizParams, toolRawRequest, effectivePromptMode)
	if err != nil {
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_tool_input",
			Message: err.Error(),
		}
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
				return domain.AgentProcessResponse{}, &ports.AgentProcessError{
					Status:  http.StatusBadRequest,
					Code:    "provider_disabled",
					Message: "active provider is disabled",
				}
			}
			resolvedModel, ok := provider.ResolveModelID(activeLLM.ProviderID, activeLLM.Model, providerSetting.ModelAliases)
			if !ok {
				return domain.AgentProcessResponse{}, &ports.AgentProcessError{
					Status:  http.StatusBadRequest,
					Code:    "model_not_found",
					Message: "active model is not available for provider",
				}
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
				ReasoningEffort:    providerSetting.ReasoningEffort,
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
		if emit != nil {
			emit(evt)
		}
	}

	processResult, processErr := s.getAgentService().Process(
		ctx,
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
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  processErr.Status,
			Code:    processErr.Code,
			Message: processErr.Message,
			Details: processErr.Details,
		}
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
	if err := channelPlugin.SendText(ctx, req.UserID, req.SessionID, reply, dispatchCfg); err != nil {
		status, code, message := mapChannelError(&channelError{
			Code:    "channel_dispatch_failed",
			Message: fmt.Sprintf("failed to dispatch message to channel %q", channelName),
			Err:     err,
		})
		return domain.AgentProcessResponse{}, &ports.AgentProcessError{
			Status:  status,
			Code:    code,
			Message: message,
		}
	}

	if memoryTaskRequested && !hasToolCall {
		s.startCodexMemoryPipeline(req.SessionID, generateConfig, memoryRolloutContents)
	}

	return domain.AgentProcessResponse{
		Reply:  reply,
		Events: events,
	}, nil
}

func immediateAgentProcessResponse(reply string) domain.AgentProcessResponse {
	return domain.AgentProcessResponse{
		Reply: reply,
		Events: []domain.AgentEvent{
			{Type: "step_started", Step: 1},
			{Type: "assistant_delta", Step: 1, Delta: reply},
			{Type: "completed", Step: 1, Reply: reply},
		},
	}
}

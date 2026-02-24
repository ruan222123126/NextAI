package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"nextai/apps/gateway/internal/domain"
	systempromptservice "nextai/apps/gateway/internal/service/systemprompt"
)

const systemPromptCompilerVersion = "v1"

type systemPromptLayer = systempromptservice.Layer

type compiledSystemPromptLayer struct {
	Layer systemPromptLayer
	Hash  string
}

type compiledSystemPromptResult struct {
	Runtime TurnRuntimeSnapshot
	Layers  []compiledSystemPromptLayer
	Hash    string
}

func (result compiledSystemPromptResult) layerSlice() []systemPromptLayer {
	if len(result.Layers) == 0 {
		return []systemPromptLayer{}
	}
	out := make([]systemPromptLayer, 0, len(result.Layers))
	for _, layer := range result.Layers {
		out = append(out, layer.Layer)
	}
	return out
}

// codexLayerBuildOptions is kept for compatibility with existing call sites/tests.
type codexLayerBuildOptions struct {
	ModelSlug         string
	Personality       string
	SessionID         string
	ReviewTask        bool
	CompactTask       bool
	MemoryTask        bool
	CollaborationMode string
}

func prependSystemLayers(input []domain.AgentInputMessage, layers []systemPromptLayer) []domain.AgentInputMessage {
	return systempromptservice.PrependLayers(input, layers)
}

func (s *Server) buildSystemLayers() ([]systemPromptLayer, error) {
	compiled, err := s.compileSystemLayersForTurnRuntime(newTurnRuntimeSnapshot(promptModeDefault, ""))
	if err != nil {
		return nil, err
	}
	return compiled.layerSlice(), nil
}

func (s *Server) buildSystemLayersForMode(mode string) ([]systemPromptLayer, error) {
	compiled, err := s.compileSystemLayersForTurnRuntime(newTurnRuntimeSnapshot(mode, ""))
	if err != nil {
		return nil, err
	}
	return compiled.layerSlice(), nil
}

func (s *Server) buildSystemLayersForModeWithOptions(runtime TurnRuntimeSnapshot) (compiledSystemPromptResult, error) {
	return s.compileSystemLayersForTurnRuntime(runtime)
}

func (s *Server) buildSystemLayersForLegacyOptions(mode string, options codexLayerBuildOptions) ([]systemPromptLayer, error) {
	compiled, err := s.buildSystemLayersForModeWithOptions(turnRuntimeSnapshotFromLegacyOptions(mode, options))
	if err != nil {
		return nil, err
	}
	return compiled.layerSlice(), nil
}

func (s *Server) buildSystemLayersForTurnRuntime(runtime TurnRuntimeSnapshot) ([]systemPromptLayer, error) {
	compiled, err := s.compileSystemLayersForTurnRuntime(runtime)
	if err != nil {
		return nil, err
	}
	return compiled.layerSlice(), nil
}

func normalizeTurnRuntimeSnapshotForPromptCompiler(runtime TurnRuntimeSnapshot) TurnRuntimeSnapshot {
	normalized := runtime
	if promptMode, ok := normalizePromptMode(runtime.Mode.PromptMode); ok {
		normalized.Mode.PromptMode = promptMode
	}
	normalized.Mode.CollaborationMode = normalizeCollaborationModeName(runtime.Mode.CollaborationMode)
	if event, ok := parseCollaborationEventName(runtime.Mode.CollaborationEvent); ok {
		normalized.Mode.CollaborationEvent = event
	} else {
		normalized.Mode.CollaborationEvent = strings.TrimSpace(runtime.Mode.CollaborationEvent)
	}
	normalized.ApprovalPolicy = strings.TrimSpace(runtime.ApprovalPolicy)
	if normalized.ApprovalPolicy == "" {
		normalized.ApprovalPolicy = defaultTurnApprovalPolicy
	}
	normalized.SandboxPolicy = strings.TrimSpace(runtime.SandboxPolicy)
	if normalized.SandboxPolicy == "" {
		normalized.SandboxPolicy = defaultTurnSandboxPolicy
	}
	normalized.AvailableTools = normalizeTurnRuntimeToolNames(runtime.AvailableTools)
	normalized.DynamicTools = normalizeTurnRuntimeToolNames(runtime.DynamicTools)
	normalized.SessionID = strings.TrimSpace(runtime.SessionID)
	normalized.ModelSlug = strings.TrimSpace(runtime.ModelSlug)
	normalized.Personality = strings.TrimSpace(runtime.Personality)
	if normalized.MCP.Enabled {
		normalized.MCP.Status = strings.TrimSpace(runtime.MCP.Status)
		if normalized.MCP.Status == "" {
			normalized.MCP.Status = mcpStatusEnabled
		}
	} else {
		normalized.MCP.Status = mcpStatusDisabled
	}
	return applyCollaborationModeToolConstraints(normalized)
}

func (s *Server) compileSystemLayersForTurnRuntime(runtime TurnRuntimeSnapshot) (compiledSystemPromptResult, error) {
	normalizedRuntime := normalizeTurnRuntimeSnapshotForPromptCompiler(runtime)
	layers, err := s.resolveSystemLayersForTurnRuntime(normalizedRuntime)
	if err != nil {
		return compiledSystemPromptResult{}, err
	}
	layers = appendTurnRuntimeToolAvailabilityLayerIfNeeded(layers, normalizedRuntime)

	compiledLayers := make([]compiledSystemPromptLayer, 0, len(layers))
	for _, layer := range layers {
		compiledLayers = append(compiledLayers, compiledSystemPromptLayer{
			Layer: layer,
			Hash:  normalizedLayerContentHash(layer.Content),
		})
	}
	return compiledSystemPromptResult{
		Runtime: normalizedRuntime,
		Layers:  compiledLayers,
		Hash:    hashCompiledSystemPrompt(normalizedRuntime, compiledLayers),
	}, nil
}

func (s *Server) resolveSystemLayersForTurnRuntime(runtime TurnRuntimeSnapshot) ([]systemPromptLayer, error) {
	normalizedMode, ok := normalizePromptMode(runtime.Mode.PromptMode)
	if !ok {
		return nil, fmt.Errorf("invalid prompt mode: %s", runtime.Mode.PromptMode)
	}
	var (
		layers []systemPromptLayer
		err    error
	)
	if normalizedMode == promptModeCodex {
		if s.cfg.EnableCodexModeV2 {
			layers, err = s.buildCodexSystemLayersV2(runtime)
		} else {
			layers, err = s.buildCodexSystemLayers(runtime)
		}
	} else {
		layers, err = s.getSystemPromptService().BuildLayersForSource(
			context.Background(),
			systempromptservice.SourceFile,
			systempromptservice.BuildRequest{
				BaseCandidates: []string{aiToolsGuideRelativePath},
				ToolGuideCandidates: []string{
					aiToolsGuideLegacyRelativePath,
					aiToolsGuideLegacyV0RelativePath,
					aiToolsGuideLegacyV1RelativePath,
					aiToolsGuideLegacyV2RelativePath,
				},
			},
		)
	}
	if err != nil {
		return nil, err
	}
	return layers, nil
}

func (s *Server) buildCodexSystemLayersV2(runtime TurnRuntimeSnapshot) ([]systemPromptLayer, error) {
	source, content, err := loadRequiredSystemLayer([]string{codexBasePromptRelativePath})
	if err != nil {
		return nil, err
	}

	layers := []systemPromptLayer{
		{
			Name:    "codex_base_system",
			Role:    "system",
			Source:  source,
			Content: systempromptservice.FormatLayerSourceContent(source, content),
		},
	}

	if layers, err = appendCodexOptionalLayer(layers, "codex_orchestrator_system", codexOrchestratorRelativePath, nil); err != nil {
		return nil, err
	}
	if layers, err = s.appendCodexModelInstructionsLayer(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexReviewPromptLayerIfNeeded(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexReviewHistoryLayersIfNeeded(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexCollaborationLayer(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexCompactLayersIfNeeded(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexMemoryLayersIfNeeded(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalLayer(layers, "codex_experimental_collab_system", codexExperimentalRelativePath, nil); err != nil {
		return nil, err
	}
	if layers, err = appendCodexSearchToolLayer(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalLayer(layers, "codex_local_policy_system", codexLocalPolicyRelativePath, nil); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalLayerFromCandidates(
		layers,
		"codex_tool_guide_system",
		[]string{
			codexToolGuideRelativePath,
			aiToolsGuideLegacyV1RelativePath,
			aiToolsGuideLegacyV2RelativePath,
		},
		nil,
	); err != nil {
		return nil, err
	}

	return dedupeLayersByNormalizedContent(layers), nil
}

func buildCodexTemplateVars(runtime TurnRuntimeSnapshot) map[string]string {
	normalizedMode := normalizeCollaborationModeName(runtime.Mode.CollaborationMode)
	availableTools := strings.Join(normalizeTurnRuntimeToolNames(runtime.AvailableTools), ", ")
	dynamicTools := strings.Join(normalizeTurnRuntimeToolNames(runtime.DynamicTools), ", ")
	requestUserInputAvailable := "false"
	if runtimeHasAvailableTool(runtime, "request_user_input") {
		requestUserInputAvailable = "true"
	}
	return map[string]string{
		"KNOWN_MODE_NAMES":             knownCollaborationModeNames(),
		"REQUEST_USER_INPUT_AVAILABLE": requestUserInputAvailable,
		"TURN_MODE":                    normalizedMode,
		"TURN_APPROVAL_POLICY":         strings.TrimSpace(runtime.ApprovalPolicy),
		"TURN_SANDBOX_POLICY":          strings.TrimSpace(runtime.SandboxPolicy),
		"TURN_AVAILABLE_TOOLS":         availableTools,
		"TURN_MCP_STATUS":              strings.TrimSpace(runtime.MCP.Status),
		"TURN_DYNAMIC_TOOLS":           dynamicTools,
	}
}

func runtimeHasAvailableTool(runtime TurnRuntimeSnapshot, toolName string) bool {
	target := normalizeRuntimeToolName(toolName)
	if target == "" {
		return false
	}
	for _, available := range normalizeTurnRuntimeToolNames(runtime.AvailableTools) {
		if normalizeRuntimeToolName(available) == target {
			return true
		}
	}
	return false
}

func appendTurnRuntimeToolAvailabilityLayerIfNeeded(
	layers []systemPromptLayer,
	runtime TurnRuntimeSnapshot,
) []systemPromptLayer {
	dynamicTools := normalizeTurnRuntimeToolNames(runtime.DynamicTools)
	if !runtime.MCP.Enabled && len(dynamicTools) == 0 {
		return layers
	}
	availableTools := normalizeTurnRuntimeToolNames(runtime.AvailableTools)

	dynamicSet := map[string]struct{}{}
	for _, name := range dynamicTools {
		dynamicSet[name] = struct{}{}
	}
	mcpTools := make([]string, 0, len(availableTools))
	for _, name := range availableTools {
		if _, ok := dynamicSet[name]; ok {
			continue
		}
		if !strings.HasPrefix(name, "mcp__") {
			continue
		}
		mcpTools = append(mcpTools, name)
	}

	lines := []string{
		"Current tool availability for this turn (source of truth):",
		"- available_tools: " + joinOrNone(availableTools),
		"- mcp_status: " + defaultIfEmpty(strings.TrimSpace(runtime.MCP.Status), mcpStatusDisabled),
		"- mcp_tools: " + joinOrNone(mcpTools),
		"- dynamic_tools: " + joinOrNone(dynamicTools),
		"Only call tools listed in available_tools. Ignore stale tool names from previous turns.",
	}
	content := strings.Join(lines, "\n")
	return append(layers, systemPromptLayer{
		Name:    "turn_runtime_tools_system",
		Role:    "system",
		Source:  "runtime://turn_tools",
		Content: systempromptservice.FormatLayerSourceContent("runtime://turn_tools", content),
	})
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}

func hashCompiledSystemPrompt(runtime TurnRuntimeSnapshot, layers []compiledSystemPromptLayer) string {
	type snapshotHashInput struct {
		PromptMode         string   `json:"prompt_mode"`
		CollaborationMode  string   `json:"collaboration_mode"`
		CollaborationEvent string   `json:"collaboration_event,omitempty"`
		ReviewTask         bool     `json:"review_task"`
		CompactTask        bool     `json:"compact_task"`
		MemoryTask         bool     `json:"memory_task"`
		ApprovalPolicy     string   `json:"approval_policy"`
		SandboxPolicy      string   `json:"sandbox_policy"`
		AvailableTools     []string `json:"available_tools"`
		MCPEnabled         bool     `json:"mcp_enabled"`
		MCPStatus          string   `json:"mcp_status"`
		DynamicTools       []string `json:"dynamic_tools"`
		SessionID          string   `json:"session_id,omitempty"`
		ModelSlug          string   `json:"model_slug,omitempty"`
		Personality        string   `json:"personality,omitempty"`
	}
	type layerHashInput struct {
		Name        string `json:"name"`
		Role        string `json:"role"`
		Source      string `json:"source"`
		ContentHash string `json:"content_hash"`
	}
	type compilerHashInput struct {
		CompilerVersion string            `json:"compiler_version"`
		Runtime         snapshotHashInput `json:"runtime"`
		Layers          []layerHashInput  `json:"layers"`
	}

	layerInputs := make([]layerHashInput, 0, len(layers))
	for _, layer := range layers {
		layerInputs = append(layerInputs, layerHashInput{
			Name:        strings.TrimSpace(layer.Layer.Name),
			Role:        strings.TrimSpace(layer.Layer.Role),
			Source:      strings.TrimSpace(layer.Layer.Source),
			ContentHash: strings.TrimSpace(layer.Hash),
		})
	}

	payload := compilerHashInput{
		CompilerVersion: systemPromptCompilerVersion,
		Runtime: snapshotHashInput{
			PromptMode:         strings.TrimSpace(runtime.Mode.PromptMode),
			CollaborationMode:  strings.TrimSpace(runtime.Mode.CollaborationMode),
			CollaborationEvent: strings.TrimSpace(runtime.Mode.CollaborationEvent),
			ReviewTask:         runtime.Mode.ReviewTask,
			CompactTask:        runtime.Mode.CompactTask,
			MemoryTask:         runtime.Mode.MemoryTask,
			ApprovalPolicy:     strings.TrimSpace(runtime.ApprovalPolicy),
			SandboxPolicy:      strings.TrimSpace(runtime.SandboxPolicy),
			AvailableTools:     normalizeTurnRuntimeToolNames(runtime.AvailableTools),
			MCPEnabled:         runtime.MCP.Enabled,
			MCPStatus:          strings.TrimSpace(runtime.MCP.Status),
			DynamicTools:       normalizeTurnRuntimeToolNames(runtime.DynamicTools),
			SessionID:          strings.TrimSpace(runtime.SessionID),
			ModelSlug:          strings.TrimSpace(runtime.ModelSlug),
			Personality:        strings.TrimSpace(runtime.Personality),
		},
		Layers: layerInputs,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:])
}

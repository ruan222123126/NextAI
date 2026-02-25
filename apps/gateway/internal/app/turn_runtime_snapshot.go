package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"nextai/apps/gateway/internal/domain"
)

const (
	defaultTurnApprovalPolicy = "never"
	defaultTurnSandboxPolicy  = "danger-full-access"
	mcpStatusDisabled         = "disabled"
	mcpStatusEnabled          = "enabled"

	turnRuntimeToolsKey             = "runtime_tools"
	turnRuntimeMCPKey               = "mcp"
	turnRuntimeMCPToolsKey          = "mcp_tools"
	turnRuntimeMCPStatusKey         = "mcp_status"
	turnRuntimeDynamicToolsKey      = "dynamic_tools"
	turnRuntimeDynamicAliasKey      = "dynamic"
	turnRuntimeToolsListKey         = "tools"
	turnRuntimeToolNameKey          = "name"
	turnRuntimeToolServerKey        = "server"
	turnRuntimeToolMethodKey        = "tool"
	turnRuntimeToolGatewayToolKey   = "gateway_tool"
	turnRuntimeToolGatewayInputKey  = "gateway_input"
	turnRuntimeToolDelegateToolKey  = "delegate_tool"
	turnRuntimeToolDelegateInputKey = "delegate_input"
	turnRuntimeToolDescKey          = "description"
	turnRuntimeToolParamsKey        = "parameters"
	turnRuntimeToolSchemaKey        = "schema"
	turnRuntimeToolInputKey         = "input_schema"
	turnRuntimeToolFunctionKey      = "function"
	turnRuntimeToolSourceMCP        = "mcp"
	turnRuntimeToolSourceDynamic    = "dynamic"
)

type TurnRuntimeSnapshot struct {
	Mode           TurnRuntimeModeSnapshot `json:"mode"`
	ApprovalPolicy string                  `json:"approval_policy"`
	SandboxPolicy  string                  `json:"sandbox_policy"`
	AvailableTools []string                `json:"available_tools"`
	MCP            TurnRuntimeMCPSnapshot  `json:"mcp"`
	DynamicTools   []string                `json:"dynamic_tools"`

	SessionID   string `json:"session_id,omitempty"`
	ModelSlug   string `json:"model_slug,omitempty"`
	Personality string `json:"personality,omitempty"`

	runtimeToolSpecs map[string]turnRuntimeToolSpec
}

type TurnRuntimeModeSnapshot struct {
	PromptMode         string `json:"prompt_mode"`
	CollaborationMode  string `json:"collaboration_mode"`
	CollaborationEvent string `json:"collaboration_event,omitempty"`
	ReviewTask         bool   `json:"review_task"`
	CompactTask        bool   `json:"compact_task"`
	MemoryTask         bool   `json:"memory_task"`
}

type TurnRuntimeMCPSnapshot struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

type turnRuntimeToolSpec struct {
	Name         string
	Description  string
	Parameters   map[string]interface{}
	Source       string
	Server       string
	Method       string
	GatewayTool  string
	GatewayInput map[string]interface{}
}

type turnRuntimeToolSet struct {
	MCPStatus    string
	MCPTools     []turnRuntimeToolSpec
	DynamicTools []turnRuntimeToolSpec
}

type turnRuntimeToolContextValue struct {
	specs          map[string]turnRuntimeToolSpec
	approvalPolicy string
	sandboxPolicy  string
}

type turnRuntimeToolContextKey struct{}

func newTurnRuntimeSnapshot(promptMode string, sessionID string) TurnRuntimeSnapshot {
	normalizedPromptMode, ok := normalizePromptMode(promptMode)
	if !ok {
		normalizedPromptMode = promptModeDefault
	}

	return TurnRuntimeSnapshot{
		Mode: TurnRuntimeModeSnapshot{
			PromptMode:        normalizedPromptMode,
			CollaborationMode: collaborationModeDefaultName,
		},
		ApprovalPolicy: defaultTurnApprovalPolicy,
		SandboxPolicy:  defaultTurnSandboxPolicy,
		AvailableTools: []string{},
		MCP: TurnRuntimeMCPSnapshot{
			Enabled: false,
			Status:  mcpStatusDisabled,
		},
		DynamicTools:     []string{},
		SessionID:        strings.TrimSpace(sessionID),
		runtimeToolSpecs: map[string]turnRuntimeToolSpec{},
	}
}

func turnRuntimeSnapshotFromLegacyOptions(
	mode string,
	options codexLayerBuildOptions,
) TurnRuntimeSnapshot {
	snapshot := newTurnRuntimeSnapshot(mode, options.SessionID)
	snapshot.ModelSlug = strings.TrimSpace(options.ModelSlug)
	snapshot.Personality = strings.TrimSpace(options.Personality)
	snapshot.Mode.ReviewTask = options.ReviewTask
	snapshot.Mode.CompactTask = options.CompactTask
	snapshot.Mode.MemoryTask = options.MemoryTask
	return snapshot
}

func (s *Server) buildTurnRuntimeSnapshotForInput(
	promptMode string,
	input []domain.AgentInputMessage,
	sessionID string,
) TurnRuntimeSnapshot {
	snapshot := newTurnRuntimeSnapshot(promptMode, sessionID)
	if snapshot.Mode.PromptMode == promptModeCodex {
		snapshot.Mode.ReviewTask = isReviewTaskCommand(input)
		snapshot.Mode.CompactTask = isCompactTaskCommand(input)
		snapshot.Mode.MemoryTask = isMemoryTaskCommand(input)
	}
	snapshot.AvailableTools = s.resolveAvailableToolDefinitionNames(snapshot.Mode.PromptMode)
	return snapshot
}

func (s *Server) buildTurnRuntimeSnapshotForSystemLayers(
	promptMode string,
	rawTaskCommand string,
	sessionID string,
) (TurnRuntimeSnapshot, error) {
	snapshot := newTurnRuntimeSnapshot(promptMode, sessionID)
	snapshot.AvailableTools = s.resolveAvailableToolDefinitionNames(snapshot.Mode.PromptMode)
	if snapshot.Mode.PromptMode != promptModeCodex {
		return snapshot, nil
	}

	taskCommand := strings.TrimSpace(rawTaskCommand)
	if taskCommand == "" {
		return snapshot, nil
	}

	normalizedTaskCommand, ok := normalizeSystemLayerTaskCommand(taskCommand)
	if !ok {
		return TurnRuntimeSnapshot{}, errors.New("invalid task_command")
	}
	switch normalizedTaskCommand {
	case reviewTaskCommand:
		snapshot.Mode.ReviewTask = true
	case compactTaskCommand:
		snapshot.Mode.CompactTask = true
	case memoryTaskCommand:
		snapshot.Mode.MemoryTask = true
	}
	return snapshot, nil
}

func normalizeTurnRuntimeToolNames(raw []string) []string {
	if len(raw) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func normalizeRuntimeToolName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeRuntimeToolDelegateName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return ""
	}
	normalized := normalizeToolName(name)
	if normalized == "" {
		return name
	}
	return normalized
}

func mergeTurnRuntimeToolNames(base []string, extra []string) []string {
	merged := make([]string, 0, len(base)+len(extra))
	merged = append(merged, base...)
	merged = append(merged, extra...)
	return normalizeTurnRuntimeToolNames(merged)
}

func parseTurnRuntimeToolSetFromChatMeta(meta map[string]interface{}) turnRuntimeToolSet {
	return parseTurnRuntimeToolSet(meta)
}

func parseTurnRuntimeToolSetFromBizParams(bizParams map[string]interface{}) turnRuntimeToolSet {
	return parseTurnRuntimeToolSet(bizParams)
}

func parseTurnRuntimeToolSet(raw map[string]interface{}) turnRuntimeToolSet {
	set := turnRuntimeToolSet{
		MCPTools:     []turnRuntimeToolSpec{},
		DynamicTools: []turnRuntimeToolSpec{},
	}
	if len(raw) == 0 {
		return set
	}

	if payload, ok := raw[turnRuntimeToolsKey]; ok {
		applyTurnRuntimeToolSetPayload(&set, payload)
	}
	applyTurnRuntimeToolSetTopLevel(&set, raw)

	set.MCPTools = normalizeTurnRuntimeToolSpecs(set.MCPTools)
	set.DynamicTools = normalizeTurnRuntimeToolSpecs(set.DynamicTools)
	if strings.TrimSpace(set.MCPStatus) == "" {
		if len(set.MCPTools) > 0 {
			set.MCPStatus = mcpStatusEnabled
		} else {
			set.MCPStatus = mcpStatusDisabled
		}
	}
	return set
}

func applyTurnRuntimeToolSetPayload(out *turnRuntimeToolSet, raw interface{}) {
	payload, ok := raw.(map[string]interface{})
	if !ok || out == nil {
		return
	}
	applyTurnRuntimeToolSetTopLevel(out, payload)
}

func applyTurnRuntimeToolSetTopLevel(out *turnRuntimeToolSet, payload map[string]interface{}) {
	if out == nil || len(payload) == 0 {
		return
	}

	if rawStatus, ok := payload[turnRuntimeMCPStatusKey]; ok {
		if status := strings.TrimSpace(stringValue(rawStatus)); status != "" {
			out.MCPStatus = status
		}
	}
	if rawMCP, ok := payload[turnRuntimeMCPKey]; ok {
		applyTurnRuntimeMCPPayload(out, rawMCP)
	}
	if rawMCPTools, ok := payload[turnRuntimeMCPToolsKey]; ok {
		out.MCPTools = append(out.MCPTools, parseTurnRuntimeToolSpecs(rawMCPTools, turnRuntimeToolSourceMCP)...)
	}
	if rawDynamic, ok := payload[turnRuntimeDynamicToolsKey]; ok {
		out.DynamicTools = append(out.DynamicTools, parseTurnRuntimeToolSpecs(rawDynamic, turnRuntimeToolSourceDynamic)...)
	}
	if rawDynamicAlias, ok := payload[turnRuntimeDynamicAliasKey]; ok {
		out.DynamicTools = append(out.DynamicTools, parseTurnRuntimeToolSpecs(rawDynamicAlias, turnRuntimeToolSourceDynamic)...)
	}
}

func applyTurnRuntimeMCPPayload(out *turnRuntimeToolSet, raw interface{}) {
	if out == nil || raw == nil {
		return
	}

	switch payload := raw.(type) {
	case string:
		if status := strings.TrimSpace(payload); status != "" {
			out.MCPStatus = status
		}
	case bool:
		if payload {
			out.MCPStatus = mcpStatusEnabled
			return
		}
		out.MCPStatus = mcpStatusDisabled
	case []interface{}, []map[string]interface{}:
		out.MCPTools = append(out.MCPTools, parseTurnRuntimeToolSpecs(payload, turnRuntimeToolSourceMCP)...)
	case map[string]interface{}:
		if rawStatus, ok := payload["status"]; ok {
			if status := strings.TrimSpace(stringValue(rawStatus)); status != "" {
				out.MCPStatus = status
			}
		}
		if rawTools, ok := payload[turnRuntimeToolsListKey]; ok {
			out.MCPTools = append(out.MCPTools, parseTurnRuntimeToolSpecs(rawTools, turnRuntimeToolSourceMCP)...)
		}
		if rawTools, ok := payload[turnRuntimeMCPToolsKey]; ok {
			out.MCPTools = append(out.MCPTools, parseTurnRuntimeToolSpecs(rawTools, turnRuntimeToolSourceMCP)...)
		}
	default:
		return
	}
}

func parseTurnRuntimeToolSpecs(raw interface{}, source string) []turnRuntimeToolSpec {
	entries := toTurnRuntimeToolEntryList(raw)
	if len(entries) == 0 {
		return []turnRuntimeToolSpec{}
	}
	out := make([]turnRuntimeToolSpec, 0, len(entries))
	for _, entry := range entries {
		spec, ok := parseTurnRuntimeToolSpecEntry(entry, source)
		if !ok {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func toTurnRuntimeToolEntryList(raw interface{}) []map[string]interface{} {
	switch value := raw.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(value))
		for _, item := range value {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, entry)
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, 0, len(value))
		for _, item := range value {
			out = append(out, item)
		}
		return out
	case map[string]interface{}:
		return []map[string]interface{}{value}
	default:
		return nil
	}
}

func parseTurnRuntimeToolSpecEntry(entry map[string]interface{}, source string) (turnRuntimeToolSpec, bool) {
	if len(entry) == 0 {
		return turnRuntimeToolSpec{}, false
	}

	sourceName := strings.ToLower(strings.TrimSpace(source))
	server := normalizeRuntimeToolName(stringValue(entry[turnRuntimeToolServerKey]))
	method := normalizeRuntimeToolName(stringValue(entry[turnRuntimeToolMethodKey]))
	name := normalizeRuntimeToolName(stringValue(entry[turnRuntimeToolNameKey]))
	if name == "" && strings.EqualFold(sourceName, turnRuntimeToolSourceMCP) {
		if server != "" && method != "" {
			name = "mcp__" + server + "__" + method
		}
	}
	if name == "" {
		return turnRuntimeToolSpec{}, false
	}

	description := strings.TrimSpace(stringValue(entry[turnRuntimeToolDescKey]))
	schema := parseRuntimeToolSchema(entry)
	gatewayTool := normalizeRuntimeToolDelegateName(stringValue(entry[turnRuntimeToolGatewayToolKey]))
	if gatewayTool == "" {
		gatewayTool = normalizeRuntimeToolDelegateName(stringValue(entry[turnRuntimeToolDelegateToolKey]))
	}
	gatewayInput := parseRuntimeToolGatewayInput(entry)

	if function, ok := entry[turnRuntimeToolFunctionKey].(map[string]interface{}); ok {
		if description == "" {
			description = strings.TrimSpace(stringValue(function[turnRuntimeToolDescKey]))
		}
		if schema == nil {
			if parsed := schemaMapFromAny(function[turnRuntimeToolParamsKey]); len(parsed) > 0 {
				schema = parsed
			}
		}
		if gatewayTool == "" {
			gatewayTool = normalizeRuntimeToolDelegateName(stringValue(function[turnRuntimeToolGatewayToolKey]))
			if gatewayTool == "" {
				gatewayTool = normalizeRuntimeToolDelegateName(stringValue(function[turnRuntimeToolDelegateToolKey]))
			}
		}
		if len(gatewayInput) == 0 {
			gatewayInput = parseRuntimeToolGatewayInput(function)
		}
	}
	if description == "" {
		description = "Runtime injected tool: " + name
	}

	return turnRuntimeToolSpec{
		Name:         name,
		Description:  description,
		Parameters:   normalizeTurnRuntimeToolSchema(schema),
		Source:       sourceName,
		Server:       server,
		Method:       method,
		GatewayTool:  gatewayTool,
		GatewayInput: cloneJSONMap(gatewayInput),
	}, true
}

func parseRuntimeToolGatewayInput(entry map[string]interface{}) map[string]interface{} {
	if len(entry) == 0 {
		return map[string]interface{}{}
	}
	for _, candidate := range []interface{}{
		entry[turnRuntimeToolGatewayInputKey],
		entry[turnRuntimeToolDelegateInputKey],
	} {
		if parsed := schemaMapFromAny(candidate); len(parsed) > 0 {
			return parsed
		}
	}
	return map[string]interface{}{}
}

func parseRuntimeToolSchema(entry map[string]interface{}) map[string]interface{} {
	if len(entry) == 0 {
		return nil
	}
	candidates := []interface{}{
		entry[turnRuntimeToolParamsKey],
		entry[turnRuntimeToolInputKey],
		entry[turnRuntimeToolSchemaKey],
	}
	for _, candidate := range candidates {
		if schema := schemaMapFromAny(candidate); len(schema) > 0 {
			return schema
		}
	}
	return nil
}

func schemaMapFromAny(raw interface{}) map[string]interface{} {
	switch value := raw.(type) {
	case map[string]interface{}:
		return cloneJSONMap(value)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		parsed := map[string]interface{}{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil
		}
		return parsed
	default:
		return nil
	}
}

func cloneJSONMap(raw map[string]interface{}) map[string]interface{} {
	if len(raw) == 0 {
		return map[string]interface{}{}
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return map[string]interface{}{}
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func normalizeTurnRuntimeToolSchema(raw map[string]interface{}) map[string]interface{} {
	if len(raw) == 0 {
		return map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{},
			"additionalProperties": true,
		}
	}
	out := cloneJSONMap(raw)
	typeName := strings.ToLower(strings.TrimSpace(stringValue(out["type"])))
	if typeName == "" {
		typeName = "object"
		out["type"] = typeName
	}
	if typeName == "object" {
		if _, ok := out["properties"].(map[string]interface{}); !ok {
			out["properties"] = map[string]interface{}{}
		}
		if _, exists := out["additionalProperties"]; !exists {
			out["additionalProperties"] = true
		}
	}
	return out
}

func normalizeTurnRuntimeToolSpecs(raw []turnRuntimeToolSpec) []turnRuntimeToolSpec {
	if len(raw) == 0 {
		return []turnRuntimeToolSpec{}
	}
	byName := map[string]turnRuntimeToolSpec{}
	for _, item := range raw {
		name := normalizeRuntimeToolName(item.Name)
		if name == "" {
			continue
		}
		spec := item
		spec.Name = name
		if strings.TrimSpace(spec.Description) == "" {
			spec.Description = "Runtime injected tool: " + name
		}
		spec.Parameters = normalizeTurnRuntimeToolSchema(spec.Parameters)
		spec.Server = normalizeRuntimeToolName(spec.Server)
		spec.Method = normalizeRuntimeToolName(spec.Method)
		spec.GatewayTool = normalizeRuntimeToolDelegateName(spec.GatewayTool)
		spec.GatewayInput = cloneJSONMap(spec.GatewayInput)
		byName[name] = spec
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]turnRuntimeToolSpec, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

func mergeTurnRuntimeToolSets(sessionSet turnRuntimeToolSet, turnSet turnRuntimeToolSet) turnRuntimeToolSet {
	out := turnRuntimeToolSet{
		MCPTools:     mergeTurnRuntimeToolSpecs(sessionSet.MCPTools, turnSet.MCPTools),
		DynamicTools: mergeTurnRuntimeToolSpecs(sessionSet.DynamicTools, turnSet.DynamicTools),
	}
	status := strings.TrimSpace(turnSet.MCPStatus)
	if status == "" {
		status = strings.TrimSpace(sessionSet.MCPStatus)
	}
	if status == "" {
		if len(out.MCPTools) > 0 {
			status = mcpStatusEnabled
		} else {
			status = mcpStatusDisabled
		}
	}
	out.MCPStatus = status
	return out
}

func mergeTurnRuntimeToolSpecs(base []turnRuntimeToolSpec, overlay []turnRuntimeToolSpec) []turnRuntimeToolSpec {
	merged := make([]turnRuntimeToolSpec, 0, len(base)+len(overlay))
	merged = append(merged, base...)
	merged = append(merged, overlay...)
	return normalizeTurnRuntimeToolSpecs(merged)
}

func runtimeToolSpecNames(raw []turnRuntimeToolSpec) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		name := normalizeRuntimeToolName(item.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return normalizeTurnRuntimeToolNames(out)
}

func (s *Server) applyRuntimeToolSetToSnapshot(
	snapshot TurnRuntimeSnapshot,
	sessionSet turnRuntimeToolSet,
	turnSet turnRuntimeToolSet,
) TurnRuntimeSnapshot {
	merged := mergeTurnRuntimeToolSets(sessionSet, turnSet)

	staticToolSet := map[string]struct{}{}
	for _, name := range snapshot.AvailableTools {
		normalized := normalizeRuntimeToolName(name)
		if normalized == "" {
			continue
		}
		staticToolSet[normalized] = struct{}{}
	}

	runtimeSpecs := map[string]turnRuntimeToolSpec{}
	runtimeNames := make([]string, 0, len(merged.MCPTools)+len(merged.DynamicTools))
	addToolSpec := func(spec turnRuntimeToolSpec) {
		name := normalizeRuntimeToolName(spec.Name)
		if name == "" {
			return
		}
		if _, exists := staticToolSet[name]; exists {
			return
		}
		normalizedSpec := spec
		normalizedSpec.Name = name
		runtimeSpecs[name] = normalizedSpec
		runtimeNames = append(runtimeNames, name)
	}
	for _, spec := range merged.MCPTools {
		addToolSpec(spec)
	}
	for _, spec := range merged.DynamicTools {
		addToolSpec(spec)
	}

	mcpNames := runtimeToolSpecNames(merged.MCPTools)
	filteredMCP := make([]string, 0, len(mcpNames))
	for _, name := range mcpNames {
		if _, ok := runtimeSpecs[name]; ok {
			filteredMCP = append(filteredMCP, name)
		}
	}

	dynamicNames := runtimeToolSpecNames(merged.DynamicTools)
	filteredDynamic := make([]string, 0, len(dynamicNames))
	for _, name := range dynamicNames {
		if _, ok := runtimeSpecs[name]; ok {
			filteredDynamic = append(filteredDynamic, name)
		}
	}

	snapshot.MCP.Enabled = len(filteredMCP) > 0
	if snapshot.MCP.Enabled {
		status := strings.TrimSpace(merged.MCPStatus)
		if status == "" {
			status = mcpStatusEnabled
		}
		snapshot.MCP.Status = status
	} else {
		snapshot.MCP.Status = mcpStatusDisabled
	}
	snapshot.DynamicTools = filteredDynamic
	snapshot.runtimeToolSpecs = runtimeSpecs
	snapshot.AvailableTools = mergeTurnRuntimeToolNames(snapshot.AvailableTools, runtimeNames)
	return snapshot
}

func withTurnRuntimeToolContext(ctx context.Context, snapshot TurnRuntimeSnapshot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	cloned := make(map[string]turnRuntimeToolSpec, len(snapshot.runtimeToolSpecs))
	for key, value := range snapshot.runtimeToolSpecs {
		cloned[key] = value
	}
	approvalPolicy := strings.TrimSpace(snapshot.ApprovalPolicy)
	if approvalPolicy == "" {
		approvalPolicy = defaultTurnApprovalPolicy
	}
	sandboxPolicy := strings.TrimSpace(snapshot.SandboxPolicy)
	if sandboxPolicy == "" {
		sandboxPolicy = defaultTurnSandboxPolicy
	}
	return context.WithValue(ctx, turnRuntimeToolContextKey{}, turnRuntimeToolContextValue{
		specs:          cloned,
		approvalPolicy: approvalPolicy,
		sandboxPolicy:  sandboxPolicy,
	})
}

func runtimeToolSpecFromContext(ctx context.Context, toolName string) (turnRuntimeToolSpec, bool) {
	if ctx == nil {
		return turnRuntimeToolSpec{}, false
	}
	raw := ctx.Value(turnRuntimeToolContextKey{})
	value, ok := raw.(turnRuntimeToolContextValue)
	if !ok || len(value.specs) == 0 {
		return turnRuntimeToolSpec{}, false
	}
	spec, exists := value.specs[normalizeRuntimeToolName(toolName)]
	if !exists {
		return turnRuntimeToolSpec{}, false
	}
	return spec, true
}

func runtimeToolPoliciesFromContext(ctx context.Context) (approvalPolicy string, sandboxPolicy string, ok bool) {
	if ctx == nil {
		return "", "", false
	}
	raw := ctx.Value(turnRuntimeToolContextKey{})
	value, ok := raw.(turnRuntimeToolContextValue)
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(value.approvalPolicy), strings.TrimSpace(value.sandboxPolicy), true
}

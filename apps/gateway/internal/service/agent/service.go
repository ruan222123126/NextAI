package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/runner"
	"nextai/apps/gateway/internal/service/ports"
)

type ToolCall struct {
	Name  string
	Input map[string]interface{}
}

type ProcessParams struct {
	Request           domain.AgentProcessRequest
	EffectiveInput    []domain.AgentInputMessage
	GenerateConfig    runner.GenerateConfig
	PromptMode        string
	HasToolCall       bool
	RequestedToolCall ToolCall
	Streaming         bool
	ReplyChunkSize    int
}

type ProcessResult struct {
	Reply  string
	Events []domain.AgentEvent
}

type ProcessError struct {
	Status  int
	Code    string
	Message string
	Details interface{}
}

func (e *ProcessError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message)
}

type Dependencies struct {
	Runner      ports.AgentRunner
	ToolRuntime ports.AgentToolRuntime
	ErrorMapper ports.AgentErrorMapper
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) Process(
	ctx context.Context,
	params ProcessParams,
	emit func(evt domain.AgentEvent),
) (ProcessResult, *ProcessError) {
	if s == nil {
		return ProcessResult{}, &ProcessError{
			Status:  500,
			Code:    "agent_service_unavailable",
			Message: "agent service is unavailable",
		}
	}
	if err := s.validateDependencies(); err != nil {
		return ProcessResult{}, &ProcessError{
			Status:  500,
			Code:    "agent_service_misconfigured",
			Message: err.Error(),
		}
	}

	reply := ""
	events := make([]domain.AgentEvent, 0, 12)
	appendEvent := func(evt domain.AgentEvent) {
		events = append(events, evt)
		if emit != nil {
			emit(evt)
		}
	}
	replyChunkSize := params.ReplyChunkSize
	if replyChunkSize <= 0 {
		replyChunkSize = 12
	}
	appendReplyDeltas := func(step int, text string) {
		for _, chunk := range splitReplyChunks(text, replyChunkSize) {
			appendEvent(domain.AgentEvent{
				Type:  "assistant_delta",
				Step:  step,
				Delta: chunk,
			})
		}
	}

	if params.HasToolCall {
		step := 1
		appendEvent(domain.AgentEvent{Type: "step_started", Step: step})
		appendEvent(domain.AgentEvent{
			Type: "tool_call",
			Step: step,
			ToolCall: &domain.AgentToolCallPayload{
				Name:  params.RequestedToolCall.Name,
				Input: safeMap(params.RequestedToolCall.Input),
			},
		})
		toolReply, err := s.deps.ToolRuntime.ExecuteToolCall(params.RequestedToolCall.Name, params.RequestedToolCall.Input)
		if err != nil {
			status, code, message := s.deps.ErrorMapper.MapToolError(err)
			return ProcessResult{}, &ProcessError{Status: status, Code: code, Message: message}
		}
		reply = toolReply
		appendEvent(domain.AgentEvent{
			Type: "tool_result",
			Step: step,
			ToolResult: &domain.AgentToolResultPayload{
				Name:    params.RequestedToolCall.Name,
				OK:      true,
				Summary: summarizeAgentEventText(reply),
			},
		})
		appendReplyDeltas(step, reply)
		appendEvent(domain.AgentEvent{Type: "completed", Step: step, Reply: reply})
		return ProcessResult{Reply: reply, Events: events}, nil
	}

	workflowInput := cloneAgentInputMessages(params.EffectiveInput)
	step := 1

	for {
		appendEvent(domain.AgentEvent{Type: "step_started", Step: step})
		turnReq := params.Request
		turnReq.Input = workflowInput

		stepHadStreamingDelta := false
		var (
			turn   runner.TurnResult
			runErr error
		)
		if params.Streaming {
			turn, runErr = s.deps.Runner.GenerateTurnStream(ctx, turnReq, params.GenerateConfig, s.deps.ToolRuntime.ListToolDefinitions(), func(delta string) {
				if delta == "" {
					return
				}
				stepHadStreamingDelta = true
				appendEvent(domain.AgentEvent{
					Type:  "assistant_delta",
					Step:  step,
					Delta: delta,
				})
			})
		} else {
			turn, runErr = s.deps.Runner.GenerateTurn(ctx, turnReq, params.GenerateConfig, s.deps.ToolRuntime.ListToolDefinitions())
		}
		if runErr != nil {
			if recoveredCall, recovered := s.deps.ToolRuntime.RecoverInvalidProviderToolCall(runErr, step); recovered {
				appendEvent(domain.AgentEvent{
					Type: "tool_call",
					Step: step,
					ToolCall: &domain.AgentToolCallPayload{
						Name:  recoveredCall.Name,
						Input: safeMap(recoveredCall.Input),
					},
				})
				appendEvent(domain.AgentEvent{
					Type: "tool_result",
					Step: step,
					ToolResult: &domain.AgentToolResultPayload{
						Name:    recoveredCall.Name,
						OK:      false,
						Summary: summarizeAgentEventText(recoveredCall.Feedback),
					},
				})
				workflowInput = append(workflowInput,
					domain.AgentInputMessage{
						Role:    "assistant",
						Type:    "message",
						Content: []domain.RuntimeContent{},
						Metadata: map[string]interface{}{
							"tool_calls": []map[string]interface{}{
								{
									"id":   recoveredCall.ID,
									"type": "function",
									"function": map[string]interface{}{
										"name":      recoveredCall.Name,
										"arguments": recoveredCall.RawArguments,
									},
								},
							},
						},
					},
					domain.AgentInputMessage{
						Role:    "tool",
						Type:    "message",
						Content: []domain.RuntimeContent{{Type: "text", Text: recoveredCall.Feedback}},
						Metadata: map[string]interface{}{
							"tool_call_id": recoveredCall.ID,
							"name":         recoveredCall.Name,
						},
					},
				)
				step++
				continue
			}
			status, code, message := s.deps.ErrorMapper.MapRunnerError(runErr)
			return ProcessResult{}, &ProcessError{Status: status, Code: code, Message: message}
		}
		if len(turn.ToolCalls) == 0 {
			reply = strings.TrimSpace(turn.Text)
			if reply == "" {
				reply = "(empty reply)"
			}
			if !params.Streaming || !stepHadStreamingDelta {
				appendReplyDeltas(step, reply)
			}
			appendEvent(domain.AgentEvent{Type: "completed", Step: step, Reply: reply})
			break
		}

		assistantMessage := domain.AgentInputMessage{
			Role:     "assistant",
			Type:     "message",
			Content:  []domain.RuntimeContent{},
			Metadata: map[string]interface{}{"tool_calls": toAgentToolCallMetadata(turn.ToolCalls)},
		}
		if text := strings.TrimSpace(turn.Text); text != "" {
			assistantMessage.Content = []domain.RuntimeContent{{Type: "text", Text: text}}
		}
		workflowInput = append(workflowInput, assistantMessage)

		for _, call := range turn.ToolCalls {
			execName := normalizeProviderToolName(call.Name)
			execInput := normalizeProviderToolInput(execName, strings.TrimSpace(params.PromptMode), safeMap(call.Arguments))
			appendEvent(domain.AgentEvent{
				Type: "tool_call",
				Step: step,
				ToolCall: &domain.AgentToolCallPayload{
					Name:  call.Name,
					Input: safeMap(call.Arguments),
				},
			})
			toolReply, toolErr := s.deps.ToolRuntime.ExecuteToolCall(execName, execInput)
			if toolErr != nil {
				toolReply = s.deps.ToolRuntime.FormatToolErrorFeedback(toolErr)
				appendEvent(domain.AgentEvent{
					Type: "tool_result",
					Step: step,
					ToolResult: &domain.AgentToolResultPayload{
						Name:    call.Name,
						OK:      false,
						Summary: summarizeAgentEventText(toolReply),
					},
				})
				workflowInput = append(workflowInput, domain.AgentInputMessage{
					Role:    "tool",
					Type:    "message",
					Content: []domain.RuntimeContent{{Type: "text", Text: toolReply}},
					Metadata: map[string]interface{}{
						"tool_call_id": call.ID,
						"name":         call.Name,
					},
				})
				continue
			}
			appendEvent(domain.AgentEvent{
				Type: "tool_result",
				Step: step,
				ToolResult: &domain.AgentToolResultPayload{
					Name:    call.Name,
					OK:      true,
					Summary: summarizeAgentEventText(toolReply),
				},
			})
			workflowInput = append(workflowInput, domain.AgentInputMessage{
				Role:    "tool",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: toolReply}},
				Metadata: map[string]interface{}{
					"tool_call_id": call.ID,
					"name":         call.Name,
				},
			})
		}
		step++
	}

	return ProcessResult{Reply: reply, Events: events}, nil
}

func (s *Service) validateDependencies() error {
	switch {
	case s.deps.Runner == nil:
		return errors.New("missing agent runner dependency")
	case s.deps.ToolRuntime == nil:
		return errors.New("missing agent tool runtime dependency")
	case s.deps.ErrorMapper == nil:
		return errors.New("missing agent error mapper dependency")
	default:
		return nil
	}
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
			callID = fmt.Sprintf("tool-call-%s", call.Name)
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

func safeMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		out := map[string]interface{}{}
		for key, value := range v {
			out[key] = value
		}
		return out
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		fallback := map[string]interface{}{}
		for key, value := range v {
			fallback[key] = value
		}
		return fallback
	}
	return out
}

func normalizeProviderToolName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
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
		return normalized
	}
}

func normalizeProviderToolInput(name, promptMode string, input map[string]interface{}) map[string]interface{} {
	normalized := normalizeProviderToolInputCompat(name, safeMap(input))
	if !isCodexPromptMode(promptMode) {
		return normalized
	}
	return normalizeCodexProviderToolInput(name, normalized)
}

func isCodexPromptMode(promptMode string) bool {
	return strings.EqualFold(strings.TrimSpace(promptMode), "codex")
}

func normalizeCodexProviderToolInput(name string, input map[string]interface{}) map[string]interface{} {
	return normalizeProviderToolInputCompat(name, input)
}

func normalizeProviderToolInputCompat(name string, input map[string]interface{}) map[string]interface{} {
	cloned := unwrapCodexProviderToolInput(safeMap(input))
	if len(cloned) == 0 {
		return cloned
	}

	if rawItems, ok := cloned["items"]; ok && rawItems != nil {
		if item, isObject := rawItems.(map[string]interface{}); isObject {
			cloned["items"] = []interface{}{safeMap(item)}
		}
	}

	if rawItems, ok := cloned["items"]; ok && rawItems != nil {
		if entries, isArray := rawItems.([]interface{}); isArray {
			normalizedEntries := make([]interface{}, 0, len(entries))
			for _, rawEntry := range entries {
				entry, isObject := rawEntry.(map[string]interface{})
				if !isObject {
					normalizedEntries = append(normalizedEntries, rawEntry)
					continue
				}
				item := safeMap(entry)
				applyCodexProviderInputAliases(item)
				if name == "shell" {
					normalizeLegacyProviderShellInput(item)
				}
				normalizedEntries = append(normalizedEntries, item)
			}
			cloned["items"] = normalizedEntries
		}
		return cloned
	}

	applyCodexProviderInputAliases(cloned)
	if name == "shell" {
		normalizeLegacyProviderShellInput(cloned)
	}
	if !isLegacySingleItemInput(name, cloned) {
		return cloned
	}
	return map[string]interface{}{
		"items": []interface{}{cloned},
	}
}

func unwrapCodexProviderToolInput(input map[string]interface{}) map[string]interface{} {
	current := safeMap(input)
	for depth := 0; depth < 4; depth++ {
		next, changed := unwrapCodexProviderToolInputOnce(current)
		if !changed {
			break
		}
		current = next
	}
	return current
}

func unwrapCodexProviderToolInputOnce(input map[string]interface{}) (map[string]interface{}, bool) {
	for _, key := range []string{"input", "arguments", "args"} {
		raw, ok := input[key]
		if !ok || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case map[string]interface{}:
			return safeMap(value), true
		case []interface{}:
			return map[string]interface{}{"items": value}, true
		case string:
			parsed, ok := parseCodexJSONMap(value)
			if ok {
				return parsed, true
			}
			parsedItems, ok := parseCodexJSONArray(value)
			if ok {
				return map[string]interface{}{"items": parsedItems}, true
			}
		}
	}
	return input, false
}

func parseCodexJSONMap(raw string) (map[string]interface{}, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil || parsed == nil {
		return nil, false
	}
	return parsed, true
}

func parseCodexJSONArray(raw string) ([]interface{}, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	var parsed []interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil || parsed == nil {
		return nil, false
	}
	return parsed, true
}

func applyCodexProviderInputAliases(input map[string]interface{}) {
	if input == nil {
		return
	}
	applyCodexAliasIfMissing(input, "start_line", "start")
	applyCodexAliasIfMissing(input, "end_line", "end")
	applyCodexAliasIfMissing(input, "q", "query")
	applyCodexAliasIfMissing(input, "num_results", "count")
	applyCodexAliasIfMissing(input, "workdir", "cwd")
	if _, hasTimeout := input["timeout_seconds"]; !hasTimeout {
		timeoutMillis, ok := parsePositiveIntFromAny(input["yield_time_ms"])
		if ok {
			timeoutSeconds := timeoutMillis / 1000
			if timeoutMillis%1000 != 0 {
				timeoutSeconds++
			}
			if timeoutSeconds < 1 {
				timeoutSeconds = 1
			}
			input["timeout_seconds"] = timeoutSeconds
		}
	}
}

func applyCodexAliasIfMissing(input map[string]interface{}, source, target string) {
	if input == nil {
		return
	}
	if _, hasTarget := input[target]; hasTarget {
		return
	}
	value, hasSource := input[source]
	if !hasSource || value == nil {
		return
	}
	input[target] = value
}

func normalizeLegacyProviderShellInput(input map[string]interface{}) {
	if input == nil {
		return
	}
	if _, hasCommand := input["command"]; !hasCommand {
		if rawCmd, ok := input["cmd"]; ok {
			input["command"] = rawCmd
		}
	}
	if _, hasCwd := input["cwd"]; !hasCwd {
		if rawWorkdir, ok := input["workdir"]; ok {
			input["cwd"] = rawWorkdir
		}
	}
	if _, hasTimeout := input["timeout_seconds"]; hasTimeout {
		return
	}
	timeoutMillis, ok := parsePositiveIntFromAny(input["yield_time_ms"])
	if !ok {
		return
	}
	timeoutSeconds := timeoutMillis / 1000
	if timeoutMillis%1000 != 0 {
		timeoutSeconds++
	}
	if timeoutSeconds < 1 {
		timeoutSeconds = 1
	}
	input["timeout_seconds"] = timeoutSeconds
}

func isLegacySingleItemInput(name string, input map[string]interface{}) bool {
	switch name {
	case "view":
		return hasAnyToolInputField(input, "path", "start", "end", "start_line", "end_line")
	case "edit":
		return hasAnyToolInputField(input, "path", "start", "end", "start_line", "end_line", "content")
	case "shell":
		return hasAnyToolInputField(input, "command", "cmd")
	case "browser":
		return hasAnyToolInputField(input, "task", "query")
	case "search":
		return hasAnyToolInputField(input, "query", "q")
	case "find":
		return hasAnyToolInputField(input, "path", "pattern", "ignore_case")
	default:
		return false
	}
}

func hasAnyToolInputField(input map[string]interface{}, keys ...string) bool {
	if len(input) == 0 || len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if value, ok := input[key]; ok && value != nil {
			return true
		}
	}
	return false
}

func parsePositiveIntFromAny(raw interface{}) (int, bool) {
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

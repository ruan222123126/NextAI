package agent

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/runner"
	"nextai/apps/gateway/internal/service/adapters"
)

func TestProcessToolCallSuccess(t *testing.T) {
	t.Parallel()

	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurn should not be called when has tool call")
				return runner.TurnResult{}, nil
			},
			GenerateTurnStreamFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition, func(string)) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurnStream should not be called when has tool call")
				return runner.TurnResult{}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func(string) []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(_ string, name string, _ map[string]interface{}) (string, error) {
				if name != "shell" {
					t.Fatalf("unexpected tool name: %s", name)
				}
				return "ok", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc:   func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) { return http.StatusBadGateway, "runner_error", err.Error() },
		},
	})

	result, processErr := svc.Process(context.Background(), ProcessParams{
		HasToolCall:       true,
		RequestedToolCall: ToolCall{Name: "shell", Input: map[string]interface{}{"command": "echo ok"}},
		ReplyChunkSize:    32,
	}, nil)
	if processErr != nil {
		t.Fatalf("unexpected process error: %+v", processErr)
	}
	if result.Reply != "ok" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if len(result.Events) != 5 {
		t.Fatalf("unexpected events count: %d", len(result.Events))
	}
	if result.Events[0].Type != "step_started" || result.Events[1].Type != "tool_call" || result.Events[2].Type != "tool_result" || result.Events[4].Type != "completed" {
		t.Fatalf("unexpected event sequence: %#v", result.Events)
	}
}

func TestProcessRunnerLoopWithToolCallAndStreamDelta(t *testing.T) {
	t.Parallel()

	step := 0
	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurn should not be called for streaming mode")
				return runner.TurnResult{}, nil
			},
			GenerateTurnStreamFunc: func(_ context.Context, _ domain.AgentProcessRequest, _ runner.GenerateConfig, _ []runner.ToolDefinition, onDelta func(string)) (runner.TurnResult, error) {
				step++
				if step == 1 {
					return runner.TurnResult{
						ToolCalls: []runner.ToolCall{
							{
								ID:        "call_1",
								Name:      "view",
								Arguments: map[string]interface{}{"path": "/tmp/a.txt"},
							},
						},
					}, nil
				}
				onDelta("hello")
				return runner.TurnResult{Text: "hello"}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func(string) []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(_ string, name string, _ map[string]interface{}) (string, error) {
				if name != "view" {
					t.Fatalf("unexpected tool name: %s", name)
				}
				return "tool-ok", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc:   func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) { return http.StatusBadGateway, "runner_error", err.Error() },
		},
	})

	emitted := make([]domain.AgentEvent, 0, 8)
	result, processErr := svc.Process(context.Background(), ProcessParams{
		Request:        domain.AgentProcessRequest{Input: []domain.AgentInputMessage{{Role: "user", Type: "message"}}},
		EffectiveInput: []domain.AgentInputMessage{{Role: "user", Type: "message", Content: []domain.RuntimeContent{{Type: "text", Text: "hi"}}}},
		Streaming:      true,
		ReplyChunkSize: 12,
	}, func(evt domain.AgentEvent) {
		emitted = append(emitted, evt)
	})
	if processErr != nil {
		t.Fatalf("unexpected process error: %+v", processErr)
	}
	if result.Reply != "hello" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if len(result.Events) == 0 || len(emitted) == 0 {
		t.Fatalf("expected streamed events")
	}
	if len(result.Events) != len(emitted) {
		t.Fatalf("result/emitted mismatch: %d vs %d", len(result.Events), len(emitted))
	}
	last := result.Events[len(result.Events)-1]
	if last.Type != "completed" {
		t.Fatalf("unexpected last event: %#v", last)
	}
}

func TestProcessRunnerErrorMapped(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				return runner.TurnResult{}, boom
			},
			GenerateTurnStreamFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition, func(string)) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurnStream should not be called")
				return runner.TurnResult{}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func(string) []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(_ string, _ string, _ map[string]interface{}) (string, error) {
				t.Fatalf("ExecuteToolCall should not be called")
				return "", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc: func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) {
				if !errors.Is(err, boom) {
					t.Fatalf("unexpected error: %v", err)
				}
				return http.StatusBadGateway, "provider_invalid_reply", "provider invalid reply"
			},
		},
	})

	_, processErr := svc.Process(context.Background(), ProcessParams{
		Request:        domain.AgentProcessRequest{},
		EffectiveInput: []domain.AgentInputMessage{{Role: "user", Type: "message"}},
		Streaming:      false,
	}, nil)
	if processErr == nil {
		t.Fatalf("expected process error")
	}
	if processErr.Status != http.StatusBadGateway || processErr.Code != "provider_invalid_reply" {
		t.Fatalf("unexpected process error: %+v", processErr)
	}
}

func TestProcessRunnerErrorMappedIncludesDetails(t *testing.T) {
	t.Parallel()

	root := errors.New("provider stream chunk is not valid json: invalid character 'P' looking for beginning of value")
	runErr := &runner.RunnerError{
		Code:    runner.ErrorCodeProviderInvalidReply,
		Message: "provider stream response is invalid",
		Err:     root,
	}

	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				return runner.TurnResult{}, runErr
			},
			GenerateTurnStreamFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition, func(string)) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurnStream should not be called")
				return runner.TurnResult{}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func(string) []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(_ string, _ string, _ map[string]interface{}) (string, error) {
				t.Fatalf("ExecuteToolCall should not be called")
				return "", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc: func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) {
				return http.StatusBadGateway, "provider_invalid_reply", "provider stream response is invalid"
			},
		},
	})

	_, processErr := svc.Process(context.Background(), ProcessParams{
		Request:        domain.AgentProcessRequest{},
		EffectiveInput: []domain.AgentInputMessage{{Role: "user", Type: "message"}},
		Streaming:      false,
	}, nil)
	if processErr == nil {
		t.Fatalf("expected process error")
	}
	if processErr.Status != http.StatusBadGateway || processErr.Code != "provider_invalid_reply" {
		t.Fatalf("unexpected process error: %+v", processErr)
	}
	details, ok := processErr.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details map, got=%T (%v)", processErr.Details, processErr.Details)
	}
	if got, _ := details["runner_message"].(string); got != "provider stream response is invalid" {
		t.Fatalf("unexpected runner_message: %q", got)
	}
	if got, _ := details["cause"].(string); got == "" || !strings.Contains(got, "provider stream chunk is not valid json") {
		t.Fatalf("unexpected cause: %q", got)
	}
}

func TestProcessCodexModeNormalizesLegacyProviderViewObject(t *testing.T) {
	t.Parallel()

	callCount := 0
	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				callCount++
				if callCount == 1 {
					return runner.TurnResult{
						ToolCalls: []runner.ToolCall{
							{
								ID:   "call_view",
								Name: "view_file_lines",
								Arguments: map[string]interface{}{
									"input": map[string]interface{}{
										"path":       "/tmp/test.txt",
										"start_line": 2,
										"end_line":   4,
									},
								},
							},
						},
					}, nil
				}
				return runner.TurnResult{Text: "done"}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func(string) []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(_ string, name string, input map[string]interface{}) (string, error) {
				if name != "view" {
					t.Fatalf("tool name=%s want=view", name)
				}
				items, ok := input["items"].([]interface{})
				if !ok || len(items) != 1 {
					t.Fatalf("items not normalized: %#v", input)
				}
				item, ok := items[0].(map[string]interface{})
				if !ok {
					t.Fatalf("item type invalid: %#v", items[0])
				}
				if _, hasStartLine := item["start_line"]; !hasStartLine {
					t.Fatalf("expected legacy field kept for compatibility, input=%#v", item)
				}
				if gotStart, _ := item["start"].(float64); gotStart != 2 {
					t.Fatalf("start=%v want=2", item["start"])
				}
				if gotEnd, _ := item["end"].(float64); gotEnd != 4 {
					t.Fatalf("end=%v want=4", item["end"])
				}
				return "view-ok", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc:   func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) { return http.StatusBadGateway, "runner_error", err.Error() },
		},
	})

	result, processErr := svc.Process(context.Background(), ProcessParams{
		Request:        domain.AgentProcessRequest{Input: []domain.AgentInputMessage{{Role: "user", Type: "message"}}},
		EffectiveInput: []domain.AgentInputMessage{{Role: "user", Type: "message"}},
		PromptMode:     "codex",
		ReplyChunkSize: 16,
	}, nil)
	if processErr != nil {
		t.Fatalf("process error: %+v", processErr)
	}
	if result.Reply != "done" {
		t.Fatalf("reply=%q want=done", result.Reply)
	}
}

func TestProcessCodexModeNormalizesExecCommandLegacyParams(t *testing.T) {
	t.Parallel()

	callCount := 0
	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				callCount++
				if callCount == 1 {
					return runner.TurnResult{
						ToolCalls: []runner.ToolCall{
							{
								ID:   "call_shell",
								Name: "exec_command",
								Arguments: map[string]interface{}{
									"arguments": map[string]interface{}{
										"cmd":           "pwd",
										"workdir":       "/tmp",
										"yield_time_ms": 1501,
									},
								},
							},
						},
					}, nil
				}
				return runner.TurnResult{Text: "done"}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func(string) []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(_ string, name string, input map[string]interface{}) (string, error) {
				if name != "shell" {
					t.Fatalf("tool name=%s want=shell", name)
				}
				items, ok := input["items"].([]interface{})
				if !ok || len(items) != 1 {
					t.Fatalf("items not normalized: %#v", input)
				}
				item, ok := items[0].(map[string]interface{})
				if !ok {
					t.Fatalf("item type invalid: %#v", items[0])
				}
				if got, _ := item["command"].(string); got != "pwd" {
					t.Fatalf("command=%q want=pwd", got)
				}
				if got, _ := item["cwd"].(string); got != "/tmp" {
					t.Fatalf("cwd=%q want=/tmp", got)
				}
				switch got := item["timeout_seconds"].(type) {
				case int:
					if got != 2 {
						t.Fatalf("timeout_seconds=%v want=2", item["timeout_seconds"])
					}
				case float64:
					if got != 2 {
						t.Fatalf("timeout_seconds=%v want=2", item["timeout_seconds"])
					}
				default:
					t.Fatalf("timeout_seconds type invalid: %#v", item["timeout_seconds"])
				}
				return "shell-ok", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc:   func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) { return http.StatusBadGateway, "runner_error", err.Error() },
		},
	})

	result, processErr := svc.Process(context.Background(), ProcessParams{
		Request:        domain.AgentProcessRequest{Input: []domain.AgentInputMessage{{Role: "user", Type: "message"}}},
		EffectiveInput: []domain.AgentInputMessage{{Role: "user", Type: "message"}},
		PromptMode:     "codex",
	}, nil)
	if processErr != nil {
		t.Fatalf("process error: %+v", processErr)
	}
	if result.Reply != "done" {
		t.Fatalf("reply=%q want=done", result.Reply)
	}
}

func TestProcessDefaultModeAlsoNormalizesLegacyProviderInput(t *testing.T) {
	t.Parallel()

	callCount := 0
	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				callCount++
				if callCount == 1 {
					return runner.TurnResult{
						ToolCalls: []runner.ToolCall{
							{
								ID:   "call_shell",
								Name: "exec_command",
								Arguments: map[string]interface{}{
									"cmd":     "pwd",
									"workdir": "/tmp",
								},
							},
						},
					}, nil
				}
				return runner.TurnResult{Text: "done"}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func(string) []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(_ string, name string, input map[string]interface{}) (string, error) {
				if name != "shell" {
					t.Fatalf("tool name=%s want=shell", name)
				}
				items, ok := input["items"].([]interface{})
				if !ok || len(items) != 1 {
					t.Fatalf("default mode should auto-wrap items, got=%#v", input)
				}
				item, ok := items[0].(map[string]interface{})
				if !ok {
					t.Fatalf("item type invalid: %#v", items[0])
				}
				if got, _ := item["command"].(string); got != "pwd" {
					t.Fatalf("command=%q want=pwd", got)
				}
				if got, _ := item["cwd"].(string); got != "/tmp" {
					t.Fatalf("cwd=%q want=/tmp", got)
				}
				return "shell-ok", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc:   func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) { return http.StatusBadGateway, "runner_error", err.Error() },
		},
	})

	_, processErr := svc.Process(context.Background(), ProcessParams{
		Request:        domain.AgentProcessRequest{Input: []domain.AgentInputMessage{{Role: "user", Type: "message"}}},
		EffectiveInput: []domain.AgentInputMessage{{Role: "user", Type: "message"}},
		PromptMode:     "default",
	}, nil)
	if processErr != nil {
		t.Fatalf("process error: %+v", processErr)
	}
}

func TestProcessSelfOpsToolCallAutoInjectsRequestScope(t *testing.T) {
	t.Parallel()

	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurn should not be called when has tool call")
				return runner.TurnResult{}, nil
			},
			GenerateTurnStreamFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition, func(string)) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurnStream should not be called when has tool call")
				return runner.TurnResult{}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func(string) []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(_ string, name string, input map[string]interface{}) (string, error) {
				if name != "self_ops" {
					t.Fatalf("unexpected tool name: %s", name)
				}
				if got, _ := input["session_id"].(string); got != "s-selfops" {
					t.Fatalf("session_id=%q, want=s-selfops", got)
				}
				if got, _ := input["user_id"].(string); got != "u-selfops" {
					t.Fatalf("user_id=%q, want=u-selfops", got)
				}
				if got, _ := input["channel"].(string); got != "console" {
					t.Fatalf("channel=%q, want=console", got)
				}
				return "ok", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc:   func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) { return http.StatusBadGateway, "runner_error", err.Error() },
		},
	})

	_, processErr := svc.Process(context.Background(), ProcessParams{
		Request: domain.AgentProcessRequest{
			SessionID: "s-selfops",
			UserID:    "u-selfops",
			Channel:   "console",
		},
		HasToolCall:       true,
		RequestedToolCall: ToolCall{Name: "self_ops", Input: map[string]interface{}{"action": "set_session_model"}},
	}, nil)
	if processErr != nil {
		t.Fatalf("unexpected process error: %+v", processErr)
	}
}

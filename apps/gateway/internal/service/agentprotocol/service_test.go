package agentprotocol

import "testing"

func TestParseQQInboundEventC2C(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-1","content":"hello","author":{"user_openid":"u-1"}}}`)
	event, err := ParseQQInboundEvent(raw)
	if err != nil {
		t.Fatalf("parse qq event failed: %v", err)
	}
	if event.UserID != "u-1" {
		t.Fatalf("unexpected user id: %q", event.UserID)
	}
	if event.TargetType != "c2c" {
		t.Fatalf("unexpected target type: %q", event.TargetType)
	}
}

func TestParseToolCallFromBizParams(t *testing.T) {
	t.Parallel()

	call, ok, err := ParseToolCall(
		map[string]interface{}{
			"tool": map[string]interface{}{
				"name": "view_file_lines",
				"input": []interface{}{
					map[string]interface{}{"path": "/tmp/a", "start": 1, "end": 1},
				},
			},
		},
		nil,
		"default",
		nil,
	)
	if err != nil {
		t.Fatalf("parse tool call failed: %v", err)
	}
	if !ok {
		t.Fatal("expected tool call present")
	}
	if call.Name != "view" {
		t.Fatalf("unexpected tool name: %q", call.Name)
	}
	items, ok := call.Input["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected input: %#v", call.Input)
	}
}

func TestParsePromptModeFromBizParamsInvalid(t *testing.T) {
	t.Parallel()

	_, _, err := ParsePromptModeFromBizParams(
		map[string]interface{}{"prompt_mode": "invalid"},
		"prompt_mode",
		"default",
		func(raw string) (string, bool) {
			if raw == "default" || raw == "codex" || raw == "claude" {
				return raw, true
			}
			return "", false
		},
	)
	if err == nil {
		t.Fatal("expected invalid prompt mode error")
	}
}

func TestListToolDefinitionNamesAddsDerivedTools(t *testing.T) {
	t.Parallel()

	names := ListToolDefinitionNames(
		"default",
		[]string{"view", "browser"},
		nil,
		func(name string) bool { return false },
	)
	has := map[string]bool{}
	for _, name := range names {
		has[name] = true
	}
	for _, required := range []string{"view", "browser", "open", "click", "screenshot", "self_ops"} {
		if !has[required] {
			t.Fatalf("missing derived tool %q from %v", required, names)
		}
	}
}

func TestListToolDefinitionNamesUsesDeclaredCapabilities(t *testing.T) {
	t.Parallel()

	names := ListToolDefinitionNames(
		"default",
		[]string{"custom-open"},
		func(name string, capability string) bool {
			return name == "custom-open" && capability == ToolCapabilityOpenURL
		},
		func(name string) bool { return false },
	)
	has := map[string]bool{}
	for _, name := range names {
		has[name] = true
	}
	if !has["open"] {
		t.Fatalf("expected derived open tool from declared capability, got=%v", names)
	}
	if has["click"] || has["screenshot"] {
		t.Fatalf("expected click/screenshot not derived without explicit capability, got=%v", names)
	}
}

func TestListToolDefinitionNamesClaudeModeBuildsFromCapabilities(t *testing.T) {
	t.Parallel()

	names := ListToolDefinitionNames(
		"claude",
		[]string{"shell", "view", "edit", "find"},
		nil,
		func(name string) bool { return false },
	)
	has := map[string]bool{}
	for _, name := range names {
		has[name] = true
	}
	for _, required := range []string{
		"Bash", "Read", "NotebookRead",
		"Write", "Edit", "MultiEdit", "NotebookEdit",
		"LS", "Glob", "Grep",
	} {
		if !has[required] {
			t.Fatalf("missing claude-derived tool %q from %v", required, names)
		}
	}
	if has["WebSearch"] || has["WebFetch"] {
		t.Fatalf("expected WebSearch/WebFetch excluded without declared capabilities, got=%v", names)
	}
}

func TestListToolDefinitionNamesCodexAddsNativeTools(t *testing.T) {
	t.Parallel()

	names := ListToolDefinitionNames(
		"codex",
		[]string{"edit", "shell"},
		nil,
		func(name string) bool { return false },
	)
	has := map[string]bool{}
	for _, name := range names {
		has[name] = true
	}
	for _, required := range []string{
		"spawn_agent",
		"send_input",
		"resume_agent",
		"wait",
		"close_agent",
		"exec_command",
		"write_stdin",
		"request_user_input",
		"update_plan",
		"apply_patch",
	} {
		if !has[required] {
			t.Fatalf("missing codex native tool %q from %v", required, names)
		}
	}
}

func TestParseShortcutToolCallSupportsSpawnAgentAliasViaNormalization(t *testing.T) {
	t.Parallel()

	rawRequest := map[string]interface{}{
		"functions.spawn_agent": map[string]interface{}{
			"task": "run tests",
		},
	}
	call, ok, err := ParseShortcutToolCall(rawRequest, "codex", []string{"spawn_agent"})
	if err != nil {
		t.Fatalf("parse shortcut failed: %v", err)
	}
	if !ok {
		t.Fatal("expected normalized spawn_agent alias shortcut match")
	}
	if call.Name != "spawn_agent" {
		t.Fatalf("unexpected tool name: %q", call.Name)
	}
}

func TestParseShortcutToolCallUsesAvailableDefinitions(t *testing.T) {
	t.Parallel()

	rawRequest := map[string]interface{}{
		"Read": []interface{}{
			map[string]interface{}{"file_path": "/tmp/ignored", "offset": 0, "limit": 10},
		},
	}
	call, ok, err := ParseShortcutToolCall(rawRequest, "default", []string{"view"})
	if err != nil {
		t.Fatalf("parse shortcut failed: %v", err)
	}
	if ok {
		t.Fatalf("expected no shortcut match when tool is unavailable, got=%+v", call)
	}

	call, ok, err = ParseShortcutToolCall(rawRequest, "claude", []string{"Read"})
	if err != nil {
		t.Fatalf("parse shortcut failed: %v", err)
	}
	if !ok {
		t.Fatal("expected shortcut match for available claude tool")
	}
	if call.Name != "read" {
		t.Fatalf("unexpected tool name: %q", call.Name)
	}
}

func TestParseShortcutToolCallSupportsAliasViaNormalization(t *testing.T) {
	t.Parallel()

	rawRequest := map[string]interface{}{
		"functions.exec_command": []interface{}{
			map[string]interface{}{"command": "echo hi"},
		},
	}
	call, ok, err := ParseShortcutToolCall(rawRequest, "default", []string{"shell"})
	if err != nil {
		t.Fatalf("parse shortcut failed: %v", err)
	}
	if !ok {
		t.Fatal("expected normalized alias shortcut match")
	}
	if call.Name != "shell" {
		t.Fatalf("unexpected tool name: %q", call.Name)
	}
}

func TestParseShortcutToolCallSupportsWriteStdinAliasViaNormalization(t *testing.T) {
	t.Parallel()

	rawRequest := map[string]interface{}{
		"functions.write_stdin": map[string]interface{}{
			"session_id": 1001,
			"chars":      "echo hi\n",
		},
	}
	call, ok, err := ParseShortcutToolCall(rawRequest, "codex", []string{"shell"})
	if err != nil {
		t.Fatalf("parse shortcut failed: %v", err)
	}
	if !ok {
		t.Fatal("expected normalized write_stdin alias shortcut match")
	}
	if call.Name != "shell" {
		t.Fatalf("unexpected tool name: %q", call.Name)
	}
	switch got := call.Input["session_id"].(type) {
	case int:
		if got != 1001 {
			t.Fatalf("session_id=%v want=1001", call.Input["session_id"])
		}
	case float64:
		if got != 1001 {
			t.Fatalf("session_id=%v want=1001", call.Input["session_id"])
		}
	default:
		t.Fatalf("session_id type=%T want int/float64", call.Input["session_id"])
	}
}

func TestParseShortcutToolCallSupportsUpdatePlanAliasViaNormalization(t *testing.T) {
	t.Parallel()

	rawRequest := map[string]interface{}{
		"functions.update_plan": map[string]interface{}{
			"plan": []interface{}{
				map[string]interface{}{"step": "stub", "status": "pending"},
			},
		},
	}
	call, ok, err := ParseShortcutToolCall(rawRequest, "codex", []string{"update_plan"})
	if err != nil {
		t.Fatalf("parse shortcut failed: %v", err)
	}
	if !ok {
		t.Fatal("expected normalized update_plan alias shortcut match")
	}
	if call.Name != "update_plan" {
		t.Fatalf("unexpected tool name: %q", call.Name)
	}
}

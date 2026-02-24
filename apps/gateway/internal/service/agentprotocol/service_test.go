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

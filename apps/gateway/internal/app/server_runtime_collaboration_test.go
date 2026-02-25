package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"nextai/apps/gateway/internal/config"
	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
)

func TestBuildSystemLayersForModeWithOptionsCompilesDeterministicHashFromSnapshot(t *testing.T) {
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	srv, err := NewServer(config.Config{
		Host:                  "127.0.0.1",
		Port:                  "0",
		DataDir:               t.TempDir(),
		EnablePromptTemplates: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	runtime := turnRuntimeSnapshotFromLegacyOptions(promptModeDefault, codexLayerBuildOptions{
		SessionID: "s-compiler",
	})
	runtime.AvailableTools = []string{"shell", "view", "mcp__calendar__list_events", "mcp__docs__search"}
	runtime.DynamicTools = []string{"dynamic_b", "dynamic_a", "dynamic_b"}
	runtime.MCP = TurnRuntimeMCPSnapshot{
		Enabled: true,
		Status:  "connected",
	}

	first, err := srv.buildSystemLayersForModeWithOptions(runtime)
	if err != nil {
		t.Fatalf("first compile failed: %v", err)
	}
	second, err := srv.buildSystemLayersForModeWithOptions(runtime)
	if err != nil {
		t.Fatalf("second compile failed: %v", err)
	}

	if strings.TrimSpace(first.Hash) == "" {
		t.Fatalf("expected non-empty compiler hash")
	}
	if first.Hash != second.Hash {
		t.Fatalf("expected deterministic compiler hash, first=%q second=%q", first.Hash, second.Hash)
	}
	if len(first.Layers) != len(second.Layers) {
		t.Fatalf("layer length mismatch: first=%d second=%d", len(first.Layers), len(second.Layers))
	}
	for index := range first.Layers {
		if first.Layers[index].Hash != second.Layers[index].Hash {
			t.Fatalf("layer hash mismatch at index=%d first=%q second=%q", index, first.Layers[index].Hash, second.Layers[index].Hash)
		}
	}

	runtime.Mode.ReviewTask = true
	changed, err := srv.buildSystemLayersForModeWithOptions(runtime)
	if err != nil {
		t.Fatalf("changed compile failed: %v", err)
	}
	if changed.Hash == first.Hash {
		t.Fatalf("expected compiler hash to change when snapshot changes, hash=%q", changed.Hash)
	}

	runtime.Mode.ReviewTask = false
}

func TestGetAgentSystemLayersRejectsUnsupportedCodexPromptMode(t *testing.T) {
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	srv, err := NewServer(config.Config{
		Host:                          "127.0.0.1",
		Port:                          "0",
		DataDir:                       t.TempDir(),
		EnablePromptTemplates:         true,
		EnablePromptContextIntrospect: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(
		w,
		httptest.NewRequest(http.MethodGet, "/agent/system-layers?prompt_mode=codex", nil),
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_request"`) ||
		!strings.Contains(w.Body.String(), "invalid prompt_mode") {
		t.Fatalf("expected invalid prompt_mode error, body=%s", w.Body.String())
	}
}

func TestProcessAgentRejectsUnsupportedCodexPromptMode(t *testing.T) {
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	srv := newTestServer(t)

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"test"}]}],"session_id":"s-invalid-codex-mode","user_id":"u-invalid-codex-mode","channel":"console","stream":false,"biz_params":{"prompt_mode":"codex"}}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_request"`) ||
		!strings.Contains(w.Body.String(), "invalid prompt_mode") {
		t.Fatalf("expected invalid prompt_mode error, body=%s", w.Body.String())
	}
}

func TestBuildTurnRuntimeSnapshotForInputCapturesUnifiedRuntimeState(t *testing.T) {
	srv := newTestServer(t)
	snapshot := srv.buildTurnRuntimeSnapshotForInput(
		promptModeDefault,
		[]domain.AgentInputMessage{
			{
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{Type: "text", Text: "/plan 先拆任务"},
				},
			},
		},
		"session-runtime-snapshot",
	)

	if snapshot.Mode.PromptMode != promptModeDefault {
		t.Fatalf("expected prompt_mode=%q, got=%q", promptModeDefault, snapshot.Mode.PromptMode)
	}
	if snapshot.Mode.CollaborationMode != collaborationModeDefaultName {
		t.Fatalf("expected collaboration_mode=%q, got=%q", collaborationModeDefaultName, snapshot.Mode.CollaborationMode)
	}
	if snapshot.Mode.CollaborationEvent != "" {
		t.Fatalf("expected empty collaboration_event for default mode, got=%q", snapshot.Mode.CollaborationEvent)
	}
	if snapshot.Mode.ReviewTask || snapshot.Mode.CompactTask || snapshot.Mode.MemoryTask {
		t.Fatalf("expected codex task flags disabled, got=%#v", snapshot.Mode)
	}
	if snapshot.ApprovalPolicy != defaultTurnApprovalPolicy {
		t.Fatalf("expected approval_policy=%q, got=%q", defaultTurnApprovalPolicy, snapshot.ApprovalPolicy)
	}
	if snapshot.SandboxPolicy != defaultTurnSandboxPolicy {
		t.Fatalf("expected sandbox_policy=%q, got=%q", defaultTurnSandboxPolicy, snapshot.SandboxPolicy)
	}
	if snapshot.MCP.Enabled {
		t.Fatalf("expected MCP disabled by default, got=%#v", snapshot.MCP)
	}
	if len(snapshot.AvailableTools) == 0 {
		t.Fatalf("expected available_tools to be populated")
	}
}

func TestListToolDefinitionsForTurnRuntimeUsesSnapshotAvailableTools(t *testing.T) {
	srv := newTestServer(t)
	snapshot := newTurnRuntimeSnapshot(promptModeDefault, "")
	snapshot.AvailableTools = []string{"Read"}

	defs := srv.listToolDefinitionsForTurnRuntime(snapshot)
	if len(defs) != 1 || defs[0].Name != "Read" {
		t.Fatalf("expected tool definitions from snapshot only, got=%#v", defs)
	}
}

func TestApplyRuntimeToolSetToSnapshotMergesSessionAndTurnToolSets(t *testing.T) {
	srv := newTestServer(t)
	snapshot := newTurnRuntimeSnapshot(promptModeDefault, "session-runtime-tools")
	snapshot.AvailableTools = []string{"view", "shell"}

	sessionSet := turnRuntimeToolSet{
		MCPStatus: "session_connected",
		MCPTools: []turnRuntimeToolSpec{
			{
				Name:        "mcp__calendar__list_events",
				Description: "List events from calendar MCP",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"date": map[string]interface{}{"type": "string"},
					},
				},
				Source: turnRuntimeToolSourceMCP,
			},
		},
		DynamicTools: []turnRuntimeToolSpec{
			{
				Name:        "dynamic_session_weather",
				Description: "Session weather dynamic tool",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{"type": "string"},
					},
				},
				Source: turnRuntimeToolSourceDynamic,
			},
		},
	}
	turnSet := turnRuntimeToolSet{
		MCPStatus: "turn_ready",
		MCPTools: []turnRuntimeToolSpec{
			{
				Name:        "mcp__calendar__create_event",
				Description: "Create event in calendar MCP",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{"type": "string"},
					},
				},
				Source: turnRuntimeToolSourceMCP,
			},
		},
		DynamicTools: []turnRuntimeToolSpec{
			{
				Name:        "dynamic_turn_database_query",
				Description: "Turn dynamic database tool",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"sql": map[string]interface{}{"type": "string"},
					},
				},
				Source: turnRuntimeToolSourceDynamic,
			},
		},
	}

	merged := srv.applyRuntimeToolSetToSnapshot(snapshot, sessionSet, turnSet)
	if !merged.MCP.Enabled {
		t.Fatalf("expected merged MCP enabled, got=%#v", merged.MCP)
	}
	if merged.MCP.Status != "turn_ready" {
		t.Fatalf("expected turn mcp status override, got=%q", merged.MCP.Status)
	}
	if len(merged.DynamicTools) != 2 {
		t.Fatalf("expected two dynamic tools, got=%#v", merged.DynamicTools)
	}

	available := map[string]bool{}
	for _, name := range merged.AvailableTools {
		available[name] = true
	}
	for _, required := range []string{
		"view",
		"shell",
		"mcp__calendar__list_events",
		"mcp__calendar__create_event",
		"dynamic_session_weather",
		"dynamic_turn_database_query",
	} {
		if !available[required] {
			t.Fatalf("missing merged available tool %q from %#v", required, merged.AvailableTools)
		}
	}

	definitions := srv.listToolDefinitionsForTurnRuntime(merged)
	byName := map[string]runner.ToolDefinition{}
	for _, def := range definitions {
		byName[def.Name] = def
	}
	if _, ok := byName["mcp__calendar__create_event"]; !ok {
		t.Fatalf("missing runtime MCP tool definition, got=%#v", byName)
	}
	if _, ok := byName["dynamic_turn_database_query"]; !ok {
		t.Fatalf("missing runtime dynamic tool definition, got=%#v", byName)
	}
}

func TestProcessAgentAggregatesSessionAndTurnRuntimeToolsIntoModelRequest(t *testing.T) {
	var (
		requestMu     sync.Mutex
		capturedModel map[string]interface{}
	)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body := map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requestMu.Lock()
		capturedModel = body
		requestMu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{"content": "runtime tools assembled"},
				},
			},
		})
	}))
	defer mock.Close()

	srv := newTestServer(t)
	configureOpenAIProviderForTest(t, srv, mock.URL)

	if err := srv.store.Write(func(state *repo.State) error {
		state.Chats["chat-runtime-tools"] = domain.ChatSpec{
			ID:        "chat-runtime-tools",
			Name:      "runtime-tools",
			SessionID: "s-runtime-tools",
			UserID:    "u-runtime-tools",
			Channel:   "console",
			CreatedAt: nowISO(),
			UpdatedAt: nowISO(),
			Meta: map[string]interface{}{
				chatMetaPromptModeKey: promptModeDefault,
				turnRuntimeToolsKey: map[string]interface{}{
					"mcp": map[string]interface{}{
						"status": "connected",
						"tools": []interface{}{
							map[string]interface{}{
								"name":        "mcp__calendar__list_events",
								"description": "list mcp events",
								"input_schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"date": map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
					"dynamic_tools": []interface{}{
						map[string]interface{}{
							"name":        "dynamic_session_tool",
							"description": "session dynamic",
							"parameters": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"session": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
			},
		}
		state.Histories["chat-runtime-tools"] = []domain.RuntimeMessage{}
		return nil
	}); err != nil {
		t.Fatalf("seed chat with runtime tools failed: %v", err)
	}

	processReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"assemble runtime tools"}]}],
		"session_id":"s-runtime-tools",
		"user_id":"u-runtime-tools",
		"channel":"console",
		"stream":false,
		"biz_params":{
			"prompt_mode":"default",
			"runtime_tools":{
				"dynamic_tools":[
					{
						"name":"dynamic_turn_tool",
						"description":"turn dynamic",
						"parameters":{
							"type":"object",
							"properties":{"turn":{"type":"string"}}
						}
					}
				]
			}
		}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(processReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	requestMu.Lock()
	modelReq := capturedModel
	requestMu.Unlock()
	if modelReq == nil {
		t.Fatalf("expected captured model request")
	}

	toolNames := collectToolNamesFromModelRequest(t, modelReq)
	toolSet := map[string]bool{}
	for _, name := range toolNames {
		toolSet[name] = true
	}
	for _, required := range []string{
		"mcp__calendar__list_events",
		"dynamic_session_tool",
		"dynamic_turn_tool",
	} {
		if !toolSet[required] {
			t.Fatalf("expected runtime tool %q in provider tools=%#v", required, toolNames)
		}
	}

	systemMessages := collectSystemMessagesFromModelRequest(t, modelReq)
	mergedSystem := strings.Join(systemMessages, "\n")
	if !strings.Contains(mergedSystem, "runtime://turn_tools") {
		t.Fatalf("expected runtime availability system layer, got=%s", mergedSystem)
	}
	if !strings.Contains(mergedSystem, "dynamic_turn_tool") || !strings.Contains(mergedSystem, "mcp__calendar__list_events") {
		t.Fatalf("expected runtime tool names in system layer, got=%s", mergedSystem)
	}
}

func TestProcessAgentExecutesRuntimeDynamicToolViaGatewayDelegate(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "runtime-dynamic-delegate")
	if err := os.WriteFile(absPath, []byte("runtime-line-1\nruntime-line-2\nruntime-line-3\n"), 0o644); err != nil {
		t.Fatalf("seed runtime dynamic tool file failed: %v", err)
	}

	processReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"run runtime dynamic tool"}]}],
		"session_id":"s-runtime-dynamic-delegate",
		"user_id":"u-runtime-dynamic-delegate",
		"channel":"console",
		"stream":false,
		"biz_params":{
			"prompt_mode":"default",
			"runtime_tools":{
				"dynamic_tools":[
					{
						"name":"dynamic_runtime_view",
						"description":"runtime dynamic view delegate",
						"gateway_tool":"view",
						"gateway_input":{
							"items":[{"path":%q,"start":2,"end":2}]
						}
					}
				]
			},
			"tool":{
				"name":"dynamic_runtime_view",
				"input":{}
			}
		}
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(processReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "2: runtime-line-2") {
		t.Fatalf("expected delegated runtime dynamic tool output, body=%s", body)
	}
}

func TestProcessAgentExecutesRuntimeMCPToolViaGatewayDelegate(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "runtime-mcp-delegate")
	if err := os.WriteFile(absPath, []byte("runtime-mcp-1\nruntime-mcp-2\nruntime-mcp-3\n"), 0o644); err != nil {
		t.Fatalf("seed runtime mcp tool file failed: %v", err)
	}

	processReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"run runtime mcp tool"}]}],
		"session_id":"s-runtime-mcp-delegate",
		"user_id":"u-runtime-mcp-delegate",
		"channel":"console",
		"stream":false,
		"biz_params":{
			"prompt_mode":"default",
			"runtime_tools":{
				"mcp":{
					"status":"connected",
					"tools":[
						{
							"server":"calendar",
							"tool":"list_events",
							"description":"runtime mcp view delegate",
							"gateway_tool":"view",
							"gateway_input":{
								"items":[{"path":%q,"start":3,"end":3}]
							}
						}
					]
				}
			},
			"tool":{
				"name":"mcp__calendar__list_events",
				"input":{}
			}
		}
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(processReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "3: runtime-mcp-3") {
		t.Fatalf("expected delegated runtime mcp tool output, body=%s", body)
	}
}

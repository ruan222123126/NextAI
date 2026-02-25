package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
)

func seedPlanChat(t *testing.T, srv *Server, chatID, sessionID, userID, channel string) {
	t.Helper()
	err := srv.store.Write(func(state *repo.State) error {
		state.Chats[chatID] = domain.ChatSpec{
			ID:        chatID,
			Name:      "plan-chat",
			SessionID: sessionID,
			UserID:    userID,
			Channel:   channel,
			CreatedAt: nowISO(),
			UpdatedAt: nowISO(),
			Meta:      map[string]interface{}{},
		}
		state.Histories[chatID] = []domain.RuntimeMessage{}
		return nil
	})
	if err != nil {
		t.Fatalf("seed chat failed: %v", err)
	}
}

func decodePlanStateResponse(t *testing.T, body string) planStateResponse {
	t.Helper()
	var out planStateResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode plan response failed: %v body=%s", err, body)
	}
	return out
}

func TestPlanModeCompileClarifyReadyFlow(t *testing.T) {
	srv := newTestServer(t)
	const chatID = "chat-plan-flow"
	seedPlanChat(t, srv, chatID, "session-plan-flow", "user-plan-flow", "console")

	toggleReq := httptest.NewRequest(http.MethodPost, "/agent/plan/toggle", strings.NewReader(`{"chat_id":"chat-plan-flow","enabled":true}`))
	toggleW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(toggleW, toggleReq)
	if toggleW.Code != http.StatusOK {
		t.Fatalf("toggle status=%d body=%s", toggleW.Code, toggleW.Body.String())
	}
	toggleResp := decodePlanStateResponse(t, toggleW.Body.String())
	if !toggleResp.PlanModeEnabled || toggleResp.PlanModeState != planModeStatePlanningIntake {
		t.Fatalf("unexpected toggle response=%#v", toggleResp)
	}

	compileReq := httptest.NewRequest(http.MethodPost, "/agent/plan/compile", strings.NewReader(`{"chat_id":"chat-plan-flow","user_input":"做一下改造"}`))
	compileW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(compileW, compileReq)
	if compileW.Code != http.StatusOK {
		t.Fatalf("compile status=%d body=%s", compileW.Code, compileW.Body.String())
	}
	compileResp := decodePlanStateResponse(t, compileW.Body.String())
	if compileResp.PlanModeState != planModeStatePlanningClarify {
		t.Fatalf("expected planning_clarify, got=%#v", compileResp)
	}
	if len(compileResp.Questions) == 0 {
		t.Fatalf("expected clarify questions, got=%#v", compileResp)
	}
	if compileResp.ClarifyAskedCount != 0 || compileResp.ClarifyMaxCount != planClarifyMaxCountDefault {
		t.Fatalf("unexpected clarify counters: %#v", compileResp)
	}
}

func TestPlanModeClarifyCapForcesPlanGeneration(t *testing.T) {
	srv := newTestServer(t)
	const chatID = "chat-plan-cap"
	seedPlanChat(t, srv, chatID, "session-plan-cap", "user-plan-cap", "console")

	toggleW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(toggleW, httptest.NewRequest(http.MethodPost, "/agent/plan/toggle", strings.NewReader(`{"chat_id":"chat-plan-cap","enabled":true}`)))
	if toggleW.Code != http.StatusOK {
		t.Fatalf("toggle status=%d body=%s", toggleW.Code, toggleW.Body.String())
	}

	compileW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(compileW, httptest.NewRequest(http.MethodPost, "/agent/plan/compile", strings.NewReader(`{"chat_id":"chat-plan-cap","user_input":"优化一下"}`)))
	if compileW.Code != http.StatusOK {
		t.Fatalf("compile status=%d body=%s", compileW.Code, compileW.Body.String())
	}

	var finalResp planStateResponse
	for i := 0; i < planClarifyMaxCountDefault; i++ {
		answerReq := httptest.NewRequest(http.MethodPost, "/agent/plan/clarify/answer", strings.NewReader(`{"chat_id":"chat-plan-cap","answers":{}}`))
		answerW := httptest.NewRecorder()
		srv.Handler().ServeHTTP(answerW, answerReq)
		if answerW.Code != http.StatusOK {
			t.Fatalf("clarify answer status=%d body=%s round=%d", answerW.Code, answerW.Body.String(), i+1)
		}
		finalResp = decodePlanStateResponse(t, answerW.Body.String())
	}
	if finalResp.ClarifyAskedCount != planClarifyMaxCountDefault {
		t.Fatalf("asked_count=%d want=%d resp=%#v", finalResp.ClarifyAskedCount, planClarifyMaxCountDefault, finalResp)
	}
	if finalResp.PlanModeState != planModeStatePlanningReady {
		t.Fatalf("expected planning_ready after cap, got=%#v", finalResp)
	}
	if finalResp.PlanSpec == nil || len(finalResp.PlanSpec.Tasks) == 0 {
		t.Fatalf("expected generated plan_spec after cap, got=%#v", finalResp)
	}
	if len(finalResp.PlanSpec.Assumptions) == 0 {
		t.Fatalf("expected assumptions when clarify capped, plan=%#v", finalResp.PlanSpec)
	}
}

func TestPlanModeReviseIncrementsRevision(t *testing.T) {
	srv := newTestServer(t)
	const chatID = "chat-plan-revise"
	seedPlanChat(t, srv, chatID, "session-plan-revise", "user-plan-revise", "console")

	srv.Handler().ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/agent/plan/toggle", strings.NewReader(`{"chat_id":"chat-plan-revise","enabled":true}`)),
	)

	compileReq := `{"chat_id":"chat-plan-revise","user_input":"实现计划模式，范围覆盖 gateway 与 web，约束一周交付，验收标准为 go test 与 vitest 通过"}`
	compileW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(compileW, httptest.NewRequest(http.MethodPost, "/agent/plan/compile", strings.NewReader(compileReq)))
	if compileW.Code != http.StatusOK {
		t.Fatalf("compile status=%d body=%s", compileW.Code, compileW.Body.String())
	}
	compileResp := decodePlanStateResponse(t, compileW.Body.String())
	if compileResp.PlanModeState != planModeStatePlanningReady || compileResp.PlanSpec == nil {
		t.Fatalf("expected planning_ready with plan, got=%#v", compileResp)
	}
	beforeRevision := compileResp.PlanSpec.Revision

	reviseReq := `{"chat_id":"chat-plan-revise","natural_language_feedback":"补充前端 e2e 验证步骤"}`
	reviseW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(reviseW, httptest.NewRequest(http.MethodPost, "/agent/plan/revise", strings.NewReader(reviseReq)))
	if reviseW.Code != http.StatusOK {
		t.Fatalf("revise status=%d body=%s", reviseW.Code, reviseW.Body.String())
	}
	reviseResp := decodePlanStateResponse(t, reviseW.Body.String())
	if reviseResp.PlanSpec == nil {
		t.Fatalf("expected plan_spec after revise, got=%#v", reviseResp)
	}
	if reviseResp.PlanSpec.Revision != beforeRevision+1 {
		t.Fatalf("revision=%d want=%d", reviseResp.PlanSpec.Revision, beforeRevision+1)
	}
}

func TestPlanModeExecuteCreatesSoftResetSession(t *testing.T) {
	srv := newTestServer(t)
	const chatID = "chat-plan-execute"
	const sessionID = "session-plan-execute"
	const userID = "user-plan-execute"
	const channel = "console"
	seedPlanChat(t, srv, chatID, sessionID, userID, channel)

	srv.Handler().ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/agent/plan/toggle", strings.NewReader(`{"chat_id":"chat-plan-execute","enabled":true}`)),
	)
	compileReq := `{"chat_id":"chat-plan-execute","user_input":"实现计划模式，范围覆盖 gateway 与 web，约束一周交付，验收标准为 go test 与 vitest 通过"}`
	srv.Handler().ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/agent/plan/compile", strings.NewReader(compileReq)),
	)

	executeW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(executeW, httptest.NewRequest(http.MethodPost, "/agent/plan/execute", strings.NewReader(`{"chat_id":"chat-plan-execute"}`)))
	if executeW.Code != http.StatusOK {
		t.Fatalf("execute status=%d body=%s", executeW.Code, executeW.Body.String())
	}
	var execResp planExecuteResponse
	if err := json.Unmarshal(executeW.Body.Bytes(), &execResp); err != nil {
		t.Fatalf("decode execute response failed: %v body=%s", err, executeW.Body.String())
	}
	if strings.TrimSpace(execResp.ExecutionSessionID) == "" {
		t.Fatalf("expected execution_session_id, got=%#v", execResp)
	}

	var executionChatID string
	srv.store.Read(func(state *repo.State) {
		for id, chat := range state.Chats {
			if chat.SessionID == execResp.ExecutionSessionID {
				executionChatID = id
				break
			}
		}
	})
	if executionChatID == "" {
		t.Fatalf("execution chat not found for session=%s", execResp.ExecutionSessionID)
	}

	historyW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(historyW, httptest.NewRequest(http.MethodGet, "/chats/"+executionChatID, nil))
	if historyW.Code != http.StatusOK {
		t.Fatalf("get execution history status=%d body=%s", historyW.Code, historyW.Body.String())
	}
	var history domain.ChatHistory
	if err := json.Unmarshal(historyW.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history failed: %v body=%s", err, historyW.Body.String())
	}
	if len(history.Messages) == 0 {
		t.Fatalf("expected execution seed message, history=%#v", history)
	}
	seedText := ""
	if len(history.Messages[0].Content) > 0 {
		seedText = history.Messages[0].Content[0].Text
	}
	if !strings.Contains(seedText, "计划结构化数据") {
		t.Fatalf("unexpected execution seed text=%q", seedText)
	}

	getPlanW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getPlanW, httptest.NewRequest(http.MethodGet, "/agent/plan/"+chatID, nil))
	if getPlanW.Code != http.StatusOK {
		t.Fatalf("get plan status=%d body=%s", getPlanW.Code, getPlanW.Body.String())
	}
	planResp := decodePlanStateResponse(t, getPlanW.Body.String())
	if planResp.PlanModeState != planModeStateExecuting {
		t.Fatalf("expected executing state, got=%#v", planResp)
	}
	if planResp.PlanExecutionSessionID != execResp.ExecutionSessionID {
		t.Fatalf("execution session mismatch plan=%s execute=%s", planResp.PlanExecutionSessionID, execResp.ExecutionSessionID)
	}
}

func TestProcessAgentInjectsPlanSystemPromptsWhenPlanModeEnabled(t *testing.T) {
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
					"message": map[string]interface{}{"content": "plan prompt injected"},
				},
			},
		})
	}))
	defer mock.Close()

	srv := newTestServer(t)
	configureOpenAIProviderForTest(t, srv, mock.URL)

	const chatID = "chat-plan-prompt-inject"
	const sessionID = "session-plan-prompt-inject"
	const userID = "user-plan-prompt-inject"
	const channel = "console"
	err := srv.store.Write(func(state *repo.State) error {
		state.Chats[chatID] = domain.ChatSpec{
			ID:        chatID,
			Name:      "plan-prompt-inject",
			SessionID: sessionID,
			UserID:    userID,
			Channel:   channel,
			CreatedAt: nowISO(),
			UpdatedAt: nowISO(),
			Meta: map[string]interface{}{
				chatMetaPromptModeKey:              promptModeDefault,
				chatMetaPlanModeEnabledKey:         true,
				chatMetaPlanModeStateKey:           planModeStatePlanningReady,
				chatMetaPlanGoalInputKey:           "测试计划提示词注入",
				chatMetaPlanSourcePromptVersionKey: "plan-test",
			},
		}
		state.Histories[chatID] = []domain.RuntimeMessage{}
		return nil
	})
	if err != nil {
		t.Fatalf("seed chat failed: %v", err)
	}

	processReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"继续执行"}]}],
		"session_id":"session-plan-prompt-inject",
		"user_id":"user-plan-prompt-inject",
		"channel":"console",
		"stream":false
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

	mergedSystem := strings.Join(collectSystemMessagesFromModelRequest(t, modelReq), "\n")
	systemMessages := collectSystemMessagesFromModelRequest(t, modelReq)
	if len(systemMessages) != 2 {
		t.Fatalf("expected exactly 2 system messages for plan mode, got=%d messages=%#v", len(systemMessages), systemMessages)
	}
	if !strings.Contains(mergedSystem, planSystemPromptRelativePath) {
		t.Fatalf("expected plan system prompt source=%q, got=%s", planSystemPromptRelativePath, mergedSystem)
	}
	if !strings.Contains(mergedSystem, planAIToolsPromptRelativePath) {
		t.Fatalf("expected plan ai-tools prompt source=%q, got=%s", planAIToolsPromptRelativePath, mergedSystem)
	}
	if strings.Contains(mergedSystem, aiToolsGuideRelativePath) {
		t.Fatalf("did not expect default system prompt source=%q in plan mode, got=%s", aiToolsGuideRelativePath, mergedSystem)
	}
}

func TestValidatePlanSpecRejectsCycle(t *testing.T) {
	spec := domain.PlanSpec{
		Goal:               "cycle",
		AcceptanceCriteria: []string{"ok"},
		Tasks: []domain.PlanTask{
			{ID: "a", Title: "A", Description: "A", DependsOn: []string{"b"}, Status: domain.PlanTaskStatusPending, Deliverables: []string{"a"}, Verification: []string{"a"}},
			{ID: "b", Title: "B", Description: "B", DependsOn: []string{"a"}, Status: domain.PlanTaskStatusPending, Deliverables: []string{"b"}, Verification: []string{"b"}},
		},
	}
	if err := validatePlanSpec(spec); err == nil {
		t.Fatal("expected cycle validation error")
	}
}

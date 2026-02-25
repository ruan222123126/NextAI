package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
)

func TestExecuteUpdatePlanToolCallPersistsPlanSnapshot(t *testing.T) {
	srv := newTestServer(t)
	const sessionID = "s-update-plan"
	const userID = "u-update-plan"
	const channel = "console"

	err := srv.store.Write(func(state *repo.State) error {
		state.Chats["chat-update-plan"] = domain.ChatSpec{
			ID:        "chat-update-plan",
			Name:      "update-plan",
			SessionID: sessionID,
			UserID:    userID,
			Channel:   channel,
			CreatedAt: nowISO(),
			UpdatedAt: nowISO(),
			Meta:      map[string]interface{}{},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed chat failed: %v", err)
	}

	result, invokeErr := srv.executeUpdatePlanToolCall(map[string]interface{}{
		"explanation": "拆成三步",
		"plan": []interface{}{
			map[string]interface{}{"step": "读取上下文", "status": "completed"},
			map[string]interface{}{"step": "实现工具", "status": "in_progress"},
			map[string]interface{}{"step": "回归测试", "status": "pending"},
		},
		requestUserInputMetaSessionIDKey: sessionID,
		requestUserInputMetaUserIDKey:    userID,
		requestUserInputMetaChannelKey:   channel,
	})
	if invokeErr != nil {
		t.Fatalf("execute update_plan failed: %v", invokeErr)
	}

	decoded := map[string]interface{}{}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("decode update_plan result failed: %v result=%s", err, result)
	}
	if _, ok := decoded["updated_at"].(string); !ok {
		t.Fatalf("expected updated_at in result, got=%#v", decoded)
	}

	srv.store.Read(func(state *repo.State) {
		chat := state.Chats["chat-update-plan"]
		raw, ok := chat.Meta[updatePlanChatMetaKey].(map[string]interface{})
		if !ok {
			t.Fatalf("expected plan snapshot in chat meta, got=%#v", chat.Meta[updatePlanChatMetaKey])
		}
		planRows, ok := raw["plan"].([]interface{})
		if !ok || len(planRows) != 3 {
			t.Fatalf("expected 3 plan rows in meta, got=%#v", raw["plan"])
		}
	})
}

func TestExecuteOutputPlanToolCallPersistsPlanSpec(t *testing.T) {
	srv := newTestServer(t)
	const sessionID = "s-output-plan"
	const userID = "u-output-plan"
	const channel = "console"
	const chatID = "chat-output-plan"

	err := srv.store.Write(func(state *repo.State) error {
		state.Chats[chatID] = domain.ChatSpec{
			ID:        chatID,
			Name:      "output-plan",
			SessionID: sessionID,
			UserID:    userID,
			Channel:   channel,
			CreatedAt: nowISO(),
			UpdatedAt: nowISO(),
			Meta: map[string]interface{}{
				chatMetaPlanModeEnabledKey: true,
				chatMetaPlanModeStateKey:   planModeStatePlanningIntake,
			},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed chat failed: %v", err)
	}

	result, invokeErr := srv.executeOutputPlanToolCall(map[string]interface{}{
		"plan": map[string]interface{}{
			"goal": "完成 Plan 模式改造",
			"tasks": []interface{}{
				map[string]interface{}{
					"id":          "task-1",
					"title":       "改造后端接口",
					"description": "补齐输出接口并写入状态",
					"depends_on":  []interface{}{},
					"status":      "pending",
					"deliverables": []interface{}{
						"接口实现",
					},
					"verification": []interface{}{
						"go test 通过",
					},
				},
			},
			"acceptance_criteria":   []interface{}{"计划可被执行"},
			"summary_for_execution": "先改后端，再改前端，最后回归。",
		},
		requestUserInputMetaSessionIDKey: sessionID,
		requestUserInputMetaUserIDKey:    userID,
		requestUserInputMetaChannelKey:   channel,
	})
	if invokeErr != nil {
		t.Fatalf("execute output_plan failed: %v", invokeErr)
	}

	decoded := map[string]interface{}{}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("decode output_plan result failed: %v result=%s", err, result)
	}
	if accepted, _ := decoded["accepted"].(bool); !accepted {
		t.Fatalf("expected accepted=true, got=%#v", decoded)
	}
	planState, ok := decoded["plan_state"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected plan_state object, got=%#v", decoded["plan_state"])
	}
	if state := strings.TrimSpace(stringValue(planState["plan_mode_state"])); state != planModeStatePlanningReady {
		t.Fatalf("expected plan_mode_state=%q, got=%q", planModeStatePlanningReady, state)
	}

	srv.store.Read(func(state *repo.State) {
		chat := state.Chats[chatID]
		snapshot := parsePlanModeSnapshot(chat.Meta)
		if !snapshot.Enabled {
			t.Fatalf("plan mode should stay enabled")
		}
		if normalizePlanModeState(snapshot.State) != planModeStatePlanningReady {
			t.Fatalf("expected planning_ready, got=%q", snapshot.State)
		}
		if snapshot.Spec == nil || strings.TrimSpace(snapshot.Spec.Goal) == "" {
			t.Fatalf("expected persisted plan spec, got=%#v", snapshot.Spec)
		}
	})
}

func TestRequestUserInputToolWaitsForAnswer(t *testing.T) {
	srv := newTestServer(t)
	const requestID = "req-rui-1"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type toolResult struct {
		reply string
		err   error
	}
	resultCh := make(chan toolResult, 1)
	go func() {
		reply, err := srv.executeRequestUserInputToolCall(ctx, map[string]interface{}{
			"request_id": requestID,
			"questions": []interface{}{
				map[string]interface{}{
					"id":       "choice",
					"header":   "环境",
					"question": "要不要启用严格模式？",
				},
			},
			requestUserInputMetaSessionIDKey:         "s-rui",
			requestUserInputMetaUserIDKey:            "u-rui",
			requestUserInputMetaChannelKey:           "console",
			requestUserInputMetaCollaborationModeKey: collaborationModePlanName,
		})
		resultCh <- toolResult{reply: reply, err: err}
	}()

	waitForPending := time.NewTimer(800 * time.Millisecond)
	defer waitForPending.Stop()
	for {
		srv.userInputMu.Lock()
		_, exists := srv.pendingUserInput[requestID]
		srv.userInputMu.Unlock()
		if exists {
			break
		}
		select {
		case <-waitForPending.C:
			t.Fatalf("pending request %q was not registered", requestID)
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	answerReq := httptest.NewRequest(
		http.MethodPost,
		"/agent/tool-input-answer",
		strings.NewReader(`{"request_id":"req-rui-1","session_id":"s-rui","user_id":"u-rui","channel":"console","answers":{"choice":{"answers":["yes"]}}}`),
	)
	answerW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(answerW, answerReq)
	if answerW.Code != http.StatusOK {
		t.Fatalf("submit answer status=%d body=%s", answerW.Code, answerW.Body.String())
	}

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("request_user_input should succeed, got err=%v", got.err)
		}
		if !strings.Contains(got.reply, `"choice"`) || !strings.Contains(got.reply, `"yes"`) {
			t.Fatalf("unexpected request_user_input result=%s", got.reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request_user_input result")
	}
}

func TestRequestUserInputToolAllowsPlanModeMetaWithoutCollaborationMode(t *testing.T) {
	srv := newTestServer(t)
	const requestID = "req-rui-plan-flag"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type toolResult struct {
		reply string
		err   error
	}
	resultCh := make(chan toolResult, 1)
	go func() {
		reply, err := srv.executeRequestUserInputToolCall(ctx, map[string]interface{}{
			"request_id": requestID,
			"questions": []interface{}{
				map[string]interface{}{
					"id":       "scope",
					"header":   "范围确认",
					"question": "是否只覆盖核心链路？",
				},
			},
			requestUserInputMetaSessionIDKey:       "s-rui-plan",
			requestUserInputMetaUserIDKey:          "u-rui-plan",
			requestUserInputMetaChannelKey:         "console",
			requestUserInputMetaPlanModeEnabledKey: true,
		})
		resultCh <- toolResult{reply: reply, err: err}
	}()

	waitForPending := time.NewTimer(800 * time.Millisecond)
	defer waitForPending.Stop()
	for {
		srv.userInputMu.Lock()
		_, exists := srv.pendingUserInput[requestID]
		srv.userInputMu.Unlock()
		if exists {
			break
		}
		select {
		case <-waitForPending.C:
			t.Fatalf("pending request %q was not registered", requestID)
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	answerReq := httptest.NewRequest(
		http.MethodPost,
		"/agent/tool-input-answer",
		strings.NewReader(`{"request_id":"req-rui-plan-flag","session_id":"s-rui-plan","user_id":"u-rui-plan","channel":"console","answers":{"scope":{"answers":["是"]}}}`),
	)
	answerW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(answerW, answerReq)
	if answerW.Code != http.StatusOK {
		t.Fatalf("submit answer status=%d body=%s", answerW.Code, answerW.Body.String())
	}

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("request_user_input should succeed with plan flag, got err=%v", got.err)
		}
		if !strings.Contains(got.reply, `"scope"`) || !strings.Contains(got.reply, `"是"`) {
			t.Fatalf("unexpected request_user_input result=%s", got.reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request_user_input result")
	}
}

func TestParseRequestUserInputArgsAllowsFlexibleQuestionShape(t *testing.T) {
	args, err := parseRequestUserInputArgs(map[string]interface{}{
		"questions": []interface{}{
			map[string]interface{}{
				"question": "先保交付还是先保性能？",
				"options": []interface{}{
					map[string]interface{}{"label": "先保交付"},
					map[string]interface{}{"label": "先保性能", "description": "允许周期更长"},
					map[string]interface{}{"label": "先保交付"},
				},
			},
			map[string]interface{}{
				"id":       "scope",
				"question": "本轮范围是否包含 Web？",
			},
		},
	})
	if err != nil {
		t.Fatalf("parse request_user_input args failed: %v", err)
	}
	if len(args.Questions) != 2 {
		t.Fatalf("questions len=%d want=2", len(args.Questions))
	}

	first := args.Questions[0]
	if first.ID != "q1" {
		t.Fatalf("first id=%q want=q1", first.ID)
	}
	if first.Header != "问题 1" {
		t.Fatalf("first header=%q want=%q", first.Header, "问题 1")
	}
	if len(first.Options) != 2 {
		t.Fatalf("first options len=%d want=2", len(first.Options))
	}
	if first.Options[0].Label != "先保交付" {
		t.Fatalf("first option label=%q want=%q", first.Options[0].Label, "先保交付")
	}
	if first.Options[1].Description != "允许周期更长" {
		t.Fatalf("first option description=%q want=%q", first.Options[1].Description, "允许周期更长")
	}

	second := args.Questions[1]
	if second.ID != "scope" {
		t.Fatalf("second id=%q want=scope", second.ID)
	}
	if second.Header != "问题 2" {
		t.Fatalf("second header=%q want=%q", second.Header, "问题 2")
	}
}

func TestSubmitToolInputAnswerReturnsNotFoundWhenRequestMissing(t *testing.T) {
	srv := newTestServer(t)
	answerReq := httptest.NewRequest(
		http.MethodPost,
		"/agent/tool-input-answer",
		strings.NewReader(`{"request_id":"missing","answers":{}}`),
	)
	answerW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(answerW, answerReq)
	if answerW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when request is missing, got=%d body=%s", answerW.Code, answerW.Body.String())
	}
}

func TestExecuteApplyPatchToolCallAppliesPatch(t *testing.T) {
	if _, err := exec.LookPath("apply_patch"); err != nil {
		t.Skip("apply_patch binary is unavailable in current test environment")
	}

	srv := newTestServer(t)
	dir := t.TempDir()
	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: hello.txt",
		"+Hello, NextAI!",
		"*** End Patch",
		"",
	}, "\n")

	reply, invokeErr := srv.executeApplyPatchToolCall(context.Background(), map[string]interface{}{
		"patch":   patch,
		"workdir": dir,
	})
	if invokeErr != nil {
		t.Fatalf("execute apply_patch failed: %v", invokeErr)
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatalf("expected apply_patch output, got empty reply")
	}

	content, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("read patched file failed: %v", err)
	}
	if strings.TrimSpace(string(content)) != "Hello, NextAI!" {
		t.Fatalf("unexpected patched file content=%q", string(content))
	}
}

func TestRequestUserInputUnavailableOutsidePlanMode(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.executeRequestUserInputToolCall(context.Background(), map[string]interface{}{
		"request_id": "req-invalid-mode",
		"questions": []interface{}{
			map[string]interface{}{"id": "q1", "header": "h", "question": "q"},
		},
		requestUserInputMetaCollaborationModeKey: collaborationModeDefaultName,
	})
	if err == nil {
		t.Fatal("expected unavailable mode error")
	}
	var te *toolError
	if !errors.As(err, &te) {
		t.Fatalf("expected toolError, got=%T %v", err, err)
	}
	if !errors.Is(te.Err, errRequestUserInputUnavailableMode) {
		t.Fatalf("expected unavailable-mode sentinel, got=%v", te.Err)
	}
}

func TestSubAgentToolsSpawnWaitCloseLifecycle(t *testing.T) {
	srv := newTestServer(t)

	spawnReply, spawnErr := srv.executeSpawnAgentToolCall(context.Background(), map[string]interface{}{
		"task":                                   "请回复一句简短确认",
		requestUserInputMetaSessionIDKey:         "s-parent",
		requestUserInputMetaUserIDKey:            "u-parent",
		requestUserInputMetaChannelKey:           "console",
		requestUserInputMetaPromptModeKey:        "default",
		requestUserInputMetaCollaborationModeKey: collaborationModeDefaultName,
	})
	if spawnErr != nil {
		t.Fatalf("spawn_agent failed: %v", spawnErr)
	}
	spawnPayload := decodeJSONMap(t, spawnReply)
	agent := decodeNestedMap(t, spawnPayload, "agent")
	agentID := strings.TrimSpace(stringValue(agent["agent_id"]))
	if agentID == "" {
		t.Fatalf("spawn_agent missing agent_id: %#v", spawnPayload)
	}

	waitReply, waitErr := srv.executeWaitAgentToolCall(context.Background(), map[string]interface{}{
		"agent_id":   agentID,
		"timeout_ms": 3000,
	})
	if waitErr != nil {
		t.Fatalf("wait failed: %v", waitErr)
	}
	waitPayload := decodeJSONMap(t, waitReply)
	waitAgent := decodeNestedMap(t, waitPayload, "agent")
	status := strings.TrimSpace(stringValue(waitAgent["status"]))
	if status == managedSubAgentStatusRunning {
		t.Fatalf("wait should stop at non-running status, got=%#v", waitPayload)
	}
	if timedOut, _ := waitPayload["timed_out"].(bool); timedOut {
		t.Fatalf("wait unexpectedly timed out: %#v", waitPayload)
	}

	closeReply, closeErr := srv.executeCloseAgentToolCall(map[string]interface{}{"agent_id": agentID})
	if closeErr != nil {
		t.Fatalf("close_agent failed: %v", closeErr)
	}
	closePayload := decodeJSONMap(t, closeReply)
	if closed, _ := closePayload["closed"].(bool); !closed {
		t.Fatalf("close_agent should return closed=true, got=%#v", closePayload)
	}
}

func TestSubAgentToolsSendInputAndResume(t *testing.T) {
	srv := newTestServer(t)

	spawnReply, spawnErr := srv.executeSpawnAgentToolCall(context.Background(), map[string]interface{}{
		"task":                            "第一轮确认",
		requestUserInputMetaSessionIDKey:  "s-parent-resume",
		requestUserInputMetaUserIDKey:     "u-parent-resume",
		requestUserInputMetaChannelKey:    "console",
		requestUserInputMetaPromptModeKey: "default",
	})
	if spawnErr != nil {
		t.Fatalf("spawn_agent failed: %v", spawnErr)
	}
	spawnPayload := decodeJSONMap(t, spawnReply)
	agent := decodeNestedMap(t, spawnPayload, "agent")
	agentID := strings.TrimSpace(stringValue(agent["agent_id"]))
	if agentID == "" {
		t.Fatalf("spawn_agent missing agent_id: %#v", spawnPayload)
	}

	if _, waitErr := srv.executeWaitAgentToolCall(context.Background(), map[string]interface{}{
		"agent_id":   agentID,
		"timeout_ms": 3000,
	}); waitErr != nil {
		t.Fatalf("initial wait failed: %v", waitErr)
	}

	sendReply, sendErr := srv.executeSendInputToolCall(map[string]interface{}{
		"agent_id": agentID,
		"input":    "第二轮请给一句确认",
	})
	if sendErr != nil {
		t.Fatalf("send_input failed: %v", sendErr)
	}
	sendPayload := decodeJSONMap(t, sendReply)
	sendAgent := decodeNestedMap(t, sendPayload, "agent")
	switch pending := sendAgent["pending_inputs"].(type) {
	case float64:
		if pending < 1 {
			t.Fatalf("expected pending_inputs >= 1 after send_input, got=%#v", sendPayload)
		}
	case int:
		if pending < 1 {
			t.Fatalf("expected pending_inputs >= 1 after send_input, got=%#v", sendPayload)
		}
	default:
		t.Fatalf("pending_inputs type invalid: %#v", sendAgent["pending_inputs"])
	}

	if _, resumeErr := srv.executeResumeAgentToolCall(context.Background(), map[string]interface{}{
		"agent_id": agentID,
	}); resumeErr != nil {
		t.Fatalf("resume_agent failed: %v", resumeErr)
	}

	waitReply, waitErr := srv.executeWaitAgentToolCall(context.Background(), map[string]interface{}{
		"agent_id":   agentID,
		"timeout_ms": 3000,
	})
	if waitErr != nil {
		t.Fatalf("second wait failed: %v", waitErr)
	}
	waitPayload := decodeJSONMap(t, waitReply)
	waitAgent := decodeNestedMap(t, waitPayload, "agent")
	status := strings.TrimSpace(stringValue(waitAgent["status"]))
	if status == managedSubAgentStatusRunning {
		t.Fatalf("expected resumed agent to stop running, got=%#v", waitPayload)
	}

	if _, closeErr := srv.executeCloseAgentToolCall(map[string]interface{}{"agent_id": agentID}); closeErr != nil {
		t.Fatalf("close_agent failed: %v", closeErr)
	}
}

func TestSubAgentToolsWaitSupportsIDsAndNotFound(t *testing.T) {
	srv := newTestServer(t)
	const missingID = "missing-agent"

	reply, waitErr := srv.executeWaitAgentToolCall(context.Background(), map[string]interface{}{
		"ids":        []interface{}{missingID},
		"timeout_ms": 500,
	})
	if waitErr != nil {
		t.Fatalf("wait failed: %v", waitErr)
	}
	payload := decodeJSONMap(t, reply)
	statusMap := decodeNestedMap(t, payload, "status")
	gotStatus := strings.TrimSpace(stringValue(statusMap[missingID]))
	if gotStatus != managedSubAgentStatusMissing {
		t.Fatalf("expected status[%q]=%q, got=%#v", missingID, managedSubAgentStatusMissing, payload)
	}
	if timedOut, _ := payload["timed_out"].(bool); timedOut {
		t.Fatalf("wait should not time out when missing id is final, got=%#v", payload)
	}
}

func TestSubAgentToolsSupportCodexStyleIDAndWaitIDs(t *testing.T) {
	srv := newTestServer(t)

	spawnReply, spawnErr := srv.executeSpawnAgentToolCall(context.Background(), map[string]interface{}{
		"message": "第一轮确认",
	})
	if spawnErr != nil {
		t.Fatalf("spawn_agent failed: %v", spawnErr)
	}
	spawnPayload := decodeJSONMap(t, spawnReply)
	agent := decodeNestedMap(t, spawnPayload, "agent")
	agentID := strings.TrimSpace(stringValue(agent["agent_id"]))
	if agentID == "" {
		t.Fatalf("spawn_agent missing agent_id: %#v", spawnPayload)
	}

	if _, waitErr := srv.executeWaitAgentToolCall(context.Background(), map[string]interface{}{
		"ids":        []interface{}{agentID},
		"timeout_ms": 3000,
	}); waitErr != nil {
		t.Fatalf("initial wait failed: %v", waitErr)
	}

	sendReply, sendErr := srv.executeSendInputToolCall(map[string]interface{}{
		"id":      agentID,
		"message": "第二轮确认",
	})
	if sendErr != nil {
		t.Fatalf("send_input failed: %v", sendErr)
	}
	sendPayload := decodeJSONMap(t, sendReply)
	submissionID := strings.TrimSpace(stringValue(sendPayload["submission_id"]))
	if submissionID == "" {
		t.Fatalf("expected submission_id in send_input payload, got=%#v", sendPayload)
	}

	if _, resumeErr := srv.executeResumeAgentToolCall(context.Background(), map[string]interface{}{
		"id": agentID,
	}); resumeErr != nil {
		t.Fatalf("resume_agent failed: %v", resumeErr)
	}

	if _, waitErr := srv.executeWaitAgentToolCall(context.Background(), map[string]interface{}{
		"ids":        []interface{}{agentID},
		"timeout_ms": 3000,
	}); waitErr != nil {
		t.Fatalf("second wait failed: %v", waitErr)
	}

	closeReply, closeErr := srv.executeCloseAgentToolCall(map[string]interface{}{"id": agentID})
	if closeErr != nil {
		t.Fatalf("close_agent failed: %v", closeErr)
	}
	closePayload := decodeJSONMap(t, closeReply)
	if closed, _ := closePayload["closed"].(bool); !closed {
		t.Fatalf("close_agent should return closed=true, got=%#v", closePayload)
	}
}

func TestSubAgentToolsCloseThenResume(t *testing.T) {
	srv := newTestServer(t)

	spawnReply, spawnErr := srv.executeSpawnAgentToolCall(context.Background(), map[string]interface{}{
		"task": "第一轮确认",
	})
	if spawnErr != nil {
		t.Fatalf("spawn_agent failed: %v", spawnErr)
	}
	spawnPayload := decodeJSONMap(t, spawnReply)
	agent := decodeNestedMap(t, spawnPayload, "agent")
	agentID := strings.TrimSpace(stringValue(agent["agent_id"]))
	if agentID == "" {
		t.Fatalf("spawn_agent missing agent_id: %#v", spawnPayload)
	}

	if _, waitErr := srv.executeWaitAgentToolCall(context.Background(), map[string]interface{}{
		"agent_id":   agentID,
		"timeout_ms": 3000,
	}); waitErr != nil {
		t.Fatalf("initial wait failed: %v", waitErr)
	}

	if _, closeErr := srv.executeCloseAgentToolCall(map[string]interface{}{"id": agentID}); closeErr != nil {
		t.Fatalf("close_agent failed: %v", closeErr)
	}

	_, sendErr := srv.executeSendInputToolCall(map[string]interface{}{
		"id":      agentID,
		"message": "关闭后发送",
	})
	if sendErr == nil {
		t.Fatal("expected send_input to fail for closed agent")
	}
	var te *toolError
	if !errors.As(sendErr, &te) {
		t.Fatalf("expected toolError, got=%T %v", sendErr, sendErr)
	}
	if !errors.Is(te.Err, errMultiAgentClosed) {
		t.Fatalf("expected errMultiAgentClosed, got=%v", te.Err)
	}

	resumeReply, resumeErr := srv.executeResumeAgentToolCall(context.Background(), map[string]interface{}{
		"id": agentID,
	})
	if resumeErr != nil {
		t.Fatalf("resume_agent failed: %v", resumeErr)
	}
	resumePayload := decodeJSONMap(t, resumeReply)
	if status := strings.TrimSpace(stringValue(resumePayload["status"])); status == managedSubAgentStatusClosed {
		t.Fatalf("resume_agent should reopen closed agent, got=%#v", resumePayload)
	}
}

func decodeJSONMap(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode json failed: %v raw=%s", err, raw)
	}
	return out
}

func decodeNestedMap(t *testing.T, raw map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	value, ok := raw[key].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map at key=%q, got=%#v", key, raw[key])
	}
	return value
}

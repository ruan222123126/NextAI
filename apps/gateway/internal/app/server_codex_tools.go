package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/ports"
)

const (
	updatePlanChatMetaKey       = "codex_update_plan"
	requestUserInputWaitTimeout = 15 * time.Minute
	agentWaitDefaultTimeout     = 30 * time.Second
	agentWaitMaxTimeout         = 5 * time.Minute
	maxSubAgentDepth            = 1

	requestUserInputMetaSessionIDKey         = "_nextai_session_id"
	requestUserInputMetaUserIDKey            = "_nextai_user_id"
	requestUserInputMetaChannelKey           = "_nextai_channel"
	requestUserInputMetaCollaborationModeKey = "_nextai_collaboration_mode"
	requestUserInputMetaRequestIDKey         = "_nextai_request_id"
	requestUserInputMetaPromptModeKey        = "_nextai_prompt_mode"
	requestUserInputMetaAgentDepthKey        = "_nextai_agent_depth"
)

var (
	errRequestUserInputUnavailableMode  = errors.New("request_user_input_unavailable_in_mode")
	errRequestUserInputQuestionsInvalid = errors.New("request_user_input_questions_invalid")
	errRequestUserInputTimeout          = errors.New("request_user_input_timeout")
	errRequestUserInputNotFound         = errors.New("request_user_input_not_found")
	errRequestUserInputIdentityMismatch = errors.New("request_user_input_identity_mismatch")
	errRequestUserInputConflict         = errors.New("request_user_input_conflict")

	errUpdatePlanInvalid      = errors.New("update_plan_invalid")
	errUpdatePlanChatNotFound = errors.New("update_plan_chat_not_found")

	errApplyPatchPayloadMissing = errors.New("apply_patch_payload_missing")
	errApplyPatchBinaryMissing  = errors.New("apply_patch_binary_missing")

	errMultiAgentIDRequired      = errors.New("multi_agent_id_required")
	errMultiAgentTaskRequired    = errors.New("multi_agent_task_required")
	errMultiAgentInputRequired   = errors.New("multi_agent_input_required")
	errMultiAgentNotFound        = errors.New("multi_agent_not_found")
	errMultiAgentConflict        = errors.New("multi_agent_conflict")
	errMultiAgentBusy            = errors.New("multi_agent_busy")
	errMultiAgentNoPendingInput  = errors.New("multi_agent_no_pending_input")
	errMultiAgentDepthExceeded   = errors.New("multi_agent_depth_exceeded")
	errMultiAgentPromptMode      = errors.New("multi_agent_prompt_mode_invalid")
	errMultiAgentCollabMode      = errors.New("multi_agent_collaboration_mode_invalid")
	errMultiAgentEmptyAgentInput = errors.New("multi_agent_input_empty")
)

type managedSubAgent struct {
	AgentID           string
	SessionID         string
	UserID            string
	Channel           string
	PromptMode        string
	CollaborationMode string
	Depth             int
	Status            string
	PendingInputs     []string
	CurrentInput      string
	LastReply         string
	LastError         string
	CreatedAt         string
	UpdatedAt         string
	LastCompletedAt   string
	waitCh            chan struct{}
	cancelCurrentTurn context.CancelFunc
}

type managedSubAgentSnapshot struct {
	AgentID           string `json:"agent_id"`
	SessionID         string `json:"session_id"`
	UserID            string `json:"user_id"`
	Channel           string `json:"channel"`
	PromptMode        string `json:"prompt_mode"`
	CollaborationMode string `json:"collaboration_mode,omitempty"`
	Depth             int    `json:"depth"`
	Status            string `json:"status"`
	PendingInputs     int    `json:"pending_inputs"`
	CurrentInput      string `json:"current_input,omitempty"`
	LastReply         string `json:"last_reply,omitempty"`
	LastError         string `json:"last_error,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
	LastCompletedAt   string `json:"last_completed_at,omitempty"`
}

const (
	managedSubAgentStatusIdle    = "idle"
	managedSubAgentStatusRunning = "running"
	managedSubAgentStatusFailed  = "failed"
	managedSubAgentStatusMissing = "not_found"
	managedSubAgentStatusClosed  = "closed"
)

type requestUserInputQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type requestUserInputQuestion struct {
	ID       string                           `json:"id"`
	Header   string                           `json:"header"`
	Question string                           `json:"question"`
	Options  []requestUserInputQuestionOption `json:"options,omitempty"`
}

type requestUserInputArgs struct {
	RequestID string                     `json:"request_id,omitempty"`
	Questions []requestUserInputQuestion `json:"questions"`
}

type requestUserInputAnswer struct {
	Answers []string `json:"answers"`
}

type requestUserInputResponse struct {
	Answers map[string]requestUserInputAnswer `json:"answers"`
}

type pendingUserInputRequest struct {
	RequestID  string
	SessionID  string
	UserID     string
	Channel    string
	ResponseCh chan requestUserInputResponse
}

type submitToolInputAnswerRequest struct {
	RequestID string                            `json:"request_id"`
	SessionID string                            `json:"session_id,omitempty"`
	UserID    string                            `json:"user_id,omitempty"`
	Channel   string                            `json:"channel,omitempty"`
	Answers   map[string]requestUserInputAnswer `json:"answers"`
}

type updatePlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type updatePlanArgs struct {
	Explanation string           `json:"explanation,omitempty"`
	Plan        []updatePlanItem `json:"plan"`
}

type updatePlanSnapshot struct {
	Explanation string           `json:"explanation,omitempty"`
	Plan        []updatePlanItem `json:"plan"`
	UpdatedAt   string           `json:"updated_at"`
}

func (s *Server) executeRequestUserInputToolCall(ctx context.Context, input map[string]interface{}) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	collaborationMode := strings.TrimSpace(stringValue(input[requestUserInputMetaCollaborationModeKey]))
	if !strings.EqualFold(collaborationMode, collaborationModePlanName) {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "request_user_input" invocation failed`,
			Err:     errRequestUserInputUnavailableMode,
		}
	}

	args, err := parseRequestUserInputArgs(input)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "request_user_input" invocation failed`,
			Err:     err,
		}
	}

	requestID := strings.TrimSpace(args.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(stringValue(input[requestUserInputMetaRequestIDKey]))
	}
	if requestID == "" {
		requestID = newID("request-user-input")
	}

	waiter := &pendingUserInputRequest{
		RequestID:  requestID,
		SessionID:  strings.TrimSpace(stringValue(input[requestUserInputMetaSessionIDKey])),
		UserID:     strings.TrimSpace(stringValue(input[requestUserInputMetaUserIDKey])),
		Channel:    strings.TrimSpace(stringValue(input[requestUserInputMetaChannelKey])),
		ResponseCh: make(chan requestUserInputResponse, 1),
	}
	if err := s.registerPendingUserInput(waiter); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "request_user_input" invocation failed`,
			Err:     err,
		}
	}
	defer s.unregisterPendingUserInput(requestID, waiter)

	timer := time.NewTimer(requestUserInputWaitTimeout)
	defer timer.Stop()

	select {
	case response := <-waiter.ResponseCh:
		encoded, encodeErr := encodeRequestUserInputToolResult(requestID, response)
		if encodeErr != nil {
			return "", &toolError{
				Code:    "tool_invalid_result",
				Message: `tool "request_user_input" returned invalid result`,
				Err:     encodeErr,
			}
		}
		return encoded, nil
	case <-ctx.Done():
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "request_user_input" invocation failed`,
			Err:     ctx.Err(),
		}
	case <-timer.C:
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "request_user_input" invocation failed`,
			Err:     errRequestUserInputTimeout,
		}
	}
}

func (s *Server) submitToolInputAnswer(w http.ResponseWriter, r *http.Request) {
	var req submitToolInputAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "request_id is required", nil)
		return
	}

	if err := s.submitPendingUserInputAnswer(req); err != nil {
		switch {
		case errors.Is(err, errRequestUserInputNotFound):
			writeErr(w, http.StatusNotFound, "request_user_input_not_found", "request_user_input request not found", nil)
			return
		case errors.Is(err, errRequestUserInputIdentityMismatch):
			writeErr(w, http.StatusConflict, "request_user_input_mismatch", "request_user_input ownership mismatch", nil)
			return
		default:
			writeErr(w, http.StatusBadRequest, "invalid_request", "failed to submit request_user_input answer", map[string]interface{}{"cause": err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"accepted":   true,
		"request_id": req.RequestID,
	})
}

func (s *Server) registerPendingUserInput(waiter *pendingUserInputRequest) error {
	if waiter == nil {
		return errRequestUserInputQuestionsInvalid
	}
	requestID := strings.TrimSpace(waiter.RequestID)
	if requestID == "" {
		return errRequestUserInputQuestionsInvalid
	}

	s.userInputMu.Lock()
	defer s.userInputMu.Unlock()
	if s.pendingUserInput == nil {
		s.pendingUserInput = map[string]*pendingUserInputRequest{}
	}
	if _, exists := s.pendingUserInput[requestID]; exists {
		return errRequestUserInputConflict
	}
	s.pendingUserInput[requestID] = waiter
	return nil
}

func (s *Server) unregisterPendingUserInput(requestID string, waiter *pendingUserInputRequest) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	s.userInputMu.Lock()
	defer s.userInputMu.Unlock()
	current, ok := s.pendingUserInput[requestID]
	if !ok {
		return
	}
	if waiter != nil && current != waiter {
		return
	}
	delete(s.pendingUserInput, requestID)
}

func (s *Server) submitPendingUserInputAnswer(req submitToolInputAnswerRequest) error {
	s.userInputMu.Lock()
	waiter, exists := s.pendingUserInput[req.RequestID]
	if !exists {
		s.userInputMu.Unlock()
		return errRequestUserInputNotFound
	}

	if sessionID := strings.TrimSpace(req.SessionID); sessionID != "" && waiter.SessionID != "" && waiter.SessionID != sessionID {
		s.userInputMu.Unlock()
		return errRequestUserInputIdentityMismatch
	}
	if userID := strings.TrimSpace(req.UserID); userID != "" && waiter.UserID != "" && waiter.UserID != userID {
		s.userInputMu.Unlock()
		return errRequestUserInputIdentityMismatch
	}
	if channel := strings.TrimSpace(req.Channel); channel != "" && waiter.Channel != "" && waiter.Channel != channel {
		s.userInputMu.Unlock()
		return errRequestUserInputIdentityMismatch
	}

	delete(s.pendingUserInput, req.RequestID)
	s.userInputMu.Unlock()

	response := normalizeRequestUserInputResponse(req.Answers)
	select {
	case waiter.ResponseCh <- response:
	default:
	}
	return nil
}

func normalizeRequestUserInputResponse(raw map[string]requestUserInputAnswer) requestUserInputResponse {
	answers := map[string]requestUserInputAnswer{}
	for key, answer := range raw {
		id := strings.TrimSpace(key)
		if id == "" {
			continue
		}
		normalized := make([]string, 0, len(answer.Answers))
		for _, item := range answer.Answers {
			value := strings.TrimSpace(item)
			if value == "" {
				continue
			}
			normalized = append(normalized, value)
		}
		answers[id] = requestUserInputAnswer{Answers: normalized}
	}
	return requestUserInputResponse{Answers: answers}
}

func parseRequestUserInputArgs(input map[string]interface{}) (requestUserInputArgs, error) {
	encoded, err := json.Marshal(safeMap(input))
	if err != nil {
		return requestUserInputArgs{}, errRequestUserInputQuestionsInvalid
	}
	var args requestUserInputArgs
	if err := json.Unmarshal(encoded, &args); err != nil {
		return requestUserInputArgs{}, errRequestUserInputQuestionsInvalid
	}
	if len(args.Questions) == 0 || len(args.Questions) > 3 {
		return requestUserInputArgs{}, errRequestUserInputQuestionsInvalid
	}

	seenIDs := map[string]struct{}{}
	for _, question := range args.Questions {
		id := strings.TrimSpace(question.ID)
		header := strings.TrimSpace(question.Header)
		prompt := strings.TrimSpace(question.Question)
		if id == "" || header == "" || prompt == "" {
			return requestUserInputArgs{}, errRequestUserInputQuestionsInvalid
		}
		if _, exists := seenIDs[id]; exists {
			return requestUserInputArgs{}, errRequestUserInputQuestionsInvalid
		}
		seenIDs[id] = struct{}{}
		for _, option := range question.Options {
			if strings.TrimSpace(option.Label) == "" || strings.TrimSpace(option.Description) == "" {
				return requestUserInputArgs{}, errRequestUserInputQuestionsInvalid
			}
		}
	}
	return args, nil
}

func encodeRequestUserInputToolResult(requestID string, response requestUserInputResponse) (string, error) {
	if response.Answers == nil {
		response.Answers = map[string]requestUserInputAnswer{}
	}
	payload := map[string]interface{}{
		"request_id": requestID,
		"answers":    response.Answers,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (s *Server) executeUpdatePlanToolCall(input map[string]interface{}) (string, error) {
	args, err := parseUpdatePlanArgs(input)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "update_plan" invocation failed`,
			Err:     err,
		}
	}

	sessionID := strings.TrimSpace(stringValue(input[requestUserInputMetaSessionIDKey]))
	userID := strings.TrimSpace(stringValue(input[requestUserInputMetaUserIDKey]))
	channel := strings.TrimSpace(stringValue(input[requestUserInputMetaChannelKey]))
	if sessionID == "" || userID == "" || channel == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "update_plan" invocation failed`,
			Err:     errUpdatePlanInvalid,
		}
	}

	snapshot, err := s.persistUpdatePlan(sessionID, userID, channel, args)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "update_plan" invocation failed`,
			Err:     err,
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invalid_result",
			Message: `tool "update_plan" returned invalid result`,
			Err:     err,
		}
	}
	return string(encoded), nil
}

func parseUpdatePlanArgs(input map[string]interface{}) (updatePlanArgs, error) {
	payload := map[string]interface{}{}
	for key, value := range safeMap(input) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "_nextai_") {
			continue
		}
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return updatePlanArgs{}, errUpdatePlanInvalid
	}
	var args updatePlanArgs
	if err := json.Unmarshal(encoded, &args); err != nil {
		return updatePlanArgs{}, errUpdatePlanInvalid
	}
	if len(args.Plan) == 0 {
		return updatePlanArgs{}, errUpdatePlanInvalid
	}
	inProgressCount := 0
	for idx := range args.Plan {
		step := strings.TrimSpace(args.Plan[idx].Step)
		status := strings.ToLower(strings.TrimSpace(args.Plan[idx].Status))
		if step == "" {
			return updatePlanArgs{}, errUpdatePlanInvalid
		}
		switch status {
		case "pending", "in_progress", "completed":
		default:
			return updatePlanArgs{}, errUpdatePlanInvalid
		}
		if status == "in_progress" {
			inProgressCount++
		}
		args.Plan[idx].Step = step
		args.Plan[idx].Status = status
	}
	if inProgressCount > 1 {
		return updatePlanArgs{}, errUpdatePlanInvalid
	}
	args.Explanation = strings.TrimSpace(args.Explanation)
	return args, nil
}

func (s *Server) persistUpdatePlan(sessionID, userID, channel string, args updatePlanArgs) (updatePlanSnapshot, error) {
	snapshot := updatePlanSnapshot{
		Explanation: args.Explanation,
		Plan:        append([]updatePlanItem{}, args.Plan...),
		UpdatedAt:   nowISO(),
	}

	found := false
	err := s.store.Write(func(state *repo.State) error {
		for chatID, chat := range state.Chats {
			if chat.SessionID != sessionID || chat.UserID != userID || chat.Channel != channel {
				continue
			}
			if chat.Meta == nil {
				chat.Meta = map[string]interface{}{}
			}
			chat.Meta[updatePlanChatMetaKey] = updatePlanSnapshotToMeta(snapshot)
			chat.UpdatedAt = nowISO()
			state.Chats[chatID] = chat
			found = true
			return nil
		}
		return nil
	})
	if err != nil {
		return updatePlanSnapshot{}, err
	}
	if !found {
		return updatePlanSnapshot{}, errUpdatePlanChatNotFound
	}
	return snapshot, nil
}

func (s *Server) executeSpawnAgentToolCall(ctx context.Context, input map[string]interface{}) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	task, err := parseSubAgentTurnInput(input, true, true, errMultiAgentTaskRequired)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "spawn_agent" invocation failed`,
			Err:     err,
		}
	}
	candidates := multiAgentInputCandidates(input)

	parentDepth := parseToolInputAgentDepth(input)
	if parentDepth >= maxSubAgentDepth {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "spawn_agent" invocation failed`,
			Err:     errMultiAgentDepthExceeded,
		}
	}

	parentSessionID := strings.TrimSpace(stringValue(input[requestUserInputMetaSessionIDKey]))
	parentUserID := strings.TrimSpace(stringValue(input[requestUserInputMetaUserIDKey]))
	parentChannel := strings.TrimSpace(stringValue(input[requestUserInputMetaChannelKey]))
	parentPromptMode := strings.TrimSpace(stringValue(input[requestUserInputMetaPromptModeKey]))
	parentCollaborationMode := strings.TrimSpace(stringValue(input[requestUserInputMetaCollaborationModeKey]))

	agentID := strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, "id", "agent_id"))
	if agentID == "" {
		agentID = newID("agent")
	}

	sessionID := strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, "session_id"))
	if sessionID == "" {
		seed := strings.TrimSpace(parentSessionID)
		if seed == "" {
			seed = "subagent"
		}
		sessionID = seed + "::" + agentID
	}

	userID := strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, "user_id"))
	if userID == "" {
		userID = parentUserID
	}
	if userID == "" {
		userID = "subagent"
	}

	channel := strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, "channel"))
	if channel == "" {
		channel = parentChannel
	}
	if channel == "" {
		channel = defaultProcessChannel
	}

	promptMode := strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, "prompt_mode"))
	if promptMode == "" {
		promptMode = parentPromptMode
	}
	if promptMode == "" {
		promptMode = promptModeDefault
	}
	normalizedPromptMode, ok := normalizePromptMode(promptMode)
	if !ok {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "spawn_agent" invocation failed`,
			Err:     errMultiAgentPromptMode,
		}
	}

	collaborationMode := strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, "collaboration_mode"))
	if collaborationMode == "" {
		collaborationMode = parentCollaborationMode
	}
	if collaborationMode == "" {
		collaborationMode = collaborationModeDefaultName
	}
	normalizedCollaborationMode, modeOK := parseCollaborationModeName(collaborationMode)
	if !modeOK {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "spawn_agent" invocation failed`,
			Err:     errMultiAgentCollabMode,
		}
	}
	if !strings.EqualFold(normalizedPromptMode, promptModeCodex) {
		normalizedCollaborationMode = collaborationModeDefaultName
	}

	now := nowISO()
	agent := &managedSubAgent{
		AgentID:           agentID,
		SessionID:         sessionID,
		UserID:            userID,
		Channel:           channel,
		PromptMode:        normalizedPromptMode,
		CollaborationMode: normalizedCollaborationMode,
		Depth:             parentDepth + 1,
		Status:            managedSubAgentStatusIdle,
		PendingInputs:     []string{},
		CreatedAt:         now,
		UpdatedAt:         now,
		waitCh:            make(chan struct{}, 1),
	}

	if err := s.registerSubAgent(agent); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "spawn_agent" invocation failed`,
			Err:     err,
		}
	}
	if err := s.startSubAgentTurn(agent.AgentID, task); err != nil {
		s.removeSubAgent(agent.AgentID)
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "spawn_agent" invocation failed`,
			Err:     err,
		}
	}
	snapshot, exists := s.getSubAgentSnapshot(agent.AgentID)
	if !exists {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "spawn_agent" invocation failed`,
			Err:     errMultiAgentNotFound,
		}
	}
	return encodeSubAgentToolPayload(map[string]interface{}{
		"ok":       true,
		"spawned":  true,
		"id":       snapshot.AgentID,
		"agent_id": snapshot.AgentID,
		"agent":    snapshot,
	})
}

func (s *Server) executeSendInputToolCall(input map[string]interface{}) (string, error) {
	agentID := parseSubAgentTargetID(input)
	if agentID == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "send_input" invocation failed`,
			Err:     errMultiAgentIDRequired,
		}
	}
	queuedInput, err := parseSubAgentTurnInput(input, true, true, errMultiAgentInputRequired)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "send_input" invocation failed`,
			Err:     err,
		}
	}
	interrupt := parseSubAgentInterrupt(input)
	submissionID := newID("submission")

	s.subAgentMu.Lock()
	agent, exists := s.subAgents[agentID]
	if !exists {
		s.subAgentMu.Unlock()
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "send_input" invocation failed`,
			Err:     errMultiAgentNotFound,
		}
	}
	if agent.Status == managedSubAgentStatusClosed {
		s.subAgentMu.Unlock()
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "send_input" invocation failed`,
			Err:     errMultiAgentClosed,
		}
	}
	agent.PendingInputs = append(agent.PendingInputs, queuedInput)
	if interrupt && agent.cancelCurrentTurn != nil {
		agent.cancelCurrentTurn()
	}
	agent.UpdatedAt = nowISO()
	snapshot := snapshotFromManagedSubAgent(agent)
	s.subAgentMu.Unlock()

	s.notifySubAgentUpdate(agent)

	return encodeSubAgentToolPayload(map[string]interface{}{
		"ok":            true,
		"accepted":      true,
		"id":            agentID,
		"agent_id":      agentID,
		"submission_id": submissionID,
		"interrupt":     interrupt,
		"agent":         snapshot,
		"queued_input":  queuedInput,
	})
}

func (s *Server) executeResumeAgentToolCall(ctx context.Context, input map[string]interface{}) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	agentID := parseSubAgentTargetID(input)
	if agentID == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "resume_agent" invocation failed`,
			Err:     errMultiAgentIDRequired,
		}
	}
	optionalInput, parseErr := parseSubAgentTurnInput(input, true, false, errMultiAgentInputRequired)
	if parseErr != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "resume_agent" invocation failed`,
			Err:     parseErr,
		}
	}
	if optionalInput != "" {
		if _, err := s.executeSendInputToolCall(map[string]interface{}{
			"id":      agentID,
			"message": optionalInput,
		}); err != nil {
			return "", err
		}
	}
	if err := s.resumeSubAgent(agentID); err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "resume_agent" invocation failed`,
			Err:     err,
		}
	}
	snapshot, exists := s.getSubAgentSnapshot(agentID)
	if !exists {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "resume_agent" invocation failed`,
			Err:     errMultiAgentNotFound,
		}
	}
	return encodeSubAgentToolPayload(map[string]interface{}{
		"ok":      true,
		"resumed": true,
		"id":      snapshot.AgentID,
		"status":  snapshot.Status,
		"agent":   snapshot,
	})
}

func (s *Server) executeWaitAgentToolCall(ctx context.Context, input map[string]interface{}) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	agentIDs, err := parseSubAgentTargetIDs(input)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "wait" invocation failed`,
			Err:     err,
		}
	}
	timeout := parseSubAgentWaitTimeout(input)
	deadline := time.Now().Add(timeout)

	for {
		finalStatuses, snapshots, waitChs := s.collectSubAgentWaitState(agentIDs)
		if len(finalStatuses) > 0 {
			return encodeSubAgentToolPayload(buildSubAgentWaitPayload(agentIDs, finalStatuses, snapshots, false))
		}

		timedOut, waitErr := waitForAnySubAgentUpdate(ctx, waitChs, deadline)
		if waitErr != nil {
			return "", &toolError{
				Code:    "tool_invoke_failed",
				Message: `tool "wait" invocation failed`,
				Err:     waitErr,
			}
		}
		if timedOut {
			_, timeoutSnapshots, _ := s.collectSubAgentWaitState(agentIDs)
			return encodeSubAgentToolPayload(buildSubAgentWaitPayload(agentIDs, map[string]string{}, timeoutSnapshots, true))
		}
	}
}

func (s *Server) collectSubAgentWaitState(agentIDs []string) (map[string]string, map[string]managedSubAgentSnapshot, []chan struct{}) {
	finalStatuses := map[string]string{}
	snapshots := map[string]managedSubAgentSnapshot{}
	waitChs := make([]chan struct{}, 0, len(agentIDs))
	waitSeen := map[chan struct{}]struct{}{}

	s.subAgentMu.Lock()
	defer s.subAgentMu.Unlock()

	for _, rawID := range agentIDs {
		agentID := strings.TrimSpace(rawID)
		if agentID == "" {
			continue
		}
		agent, exists := s.subAgents[agentID]
		if !exists {
			finalStatuses[agentID] = managedSubAgentStatusMissing
			continue
		}
		snapshot := snapshotFromManagedSubAgent(agent)
		snapshots[agentID] = snapshot
		if agent.Status != managedSubAgentStatusRunning {
			status := strings.TrimSpace(agent.Status)
			if status == "" {
				status = managedSubAgentStatusIdle
			}
			finalStatuses[agentID] = status
			continue
		}
		if agent.waitCh == nil {
			continue
		}
		if _, seen := waitSeen[agent.waitCh]; seen {
			continue
		}
		waitSeen[agent.waitCh] = struct{}{}
		waitChs = append(waitChs, agent.waitCh)
	}

	return finalStatuses, snapshots, waitChs
}

func (s *Server) executeCloseAgentToolCall(input map[string]interface{}) (string, error) {
	agentID := parseSubAgentTargetID(input)
	if agentID == "" {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "close_agent" invocation failed`,
			Err:     errMultiAgentIDRequired,
		}
	}

	snapshot, err := s.closeSubAgent(agentID)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "close_agent" invocation failed`,
			Err:     err,
		}
	}
	return encodeSubAgentToolPayload(map[string]interface{}{
		"ok":       true,
		"closed":   true,
		"id":       snapshot.AgentID,
		"status":   snapshot.Status,
		"agent":    snapshot,
		"agent_id": agentID,
	})
}

func (s *Server) registerSubAgent(agent *managedSubAgent) error {
	if agent == nil {
		return errMultiAgentTaskRequired
	}
	agentID := strings.TrimSpace(agent.AgentID)
	if agentID == "" {
		return errMultiAgentIDRequired
	}

	s.subAgentMu.Lock()
	defer s.subAgentMu.Unlock()
	if s.subAgents == nil {
		s.subAgents = map[string]*managedSubAgent{}
	}
	if _, exists := s.subAgents[agentID]; exists {
		return errMultiAgentConflict
	}
	s.subAgents[agentID] = agent
	return nil
}

func (s *Server) getSubAgentSnapshot(agentID string) (managedSubAgentSnapshot, bool) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return managedSubAgentSnapshot{}, false
	}
	s.subAgentMu.Lock()
	defer s.subAgentMu.Unlock()
	agent, exists := s.subAgents[agentID]
	if !exists {
		return managedSubAgentSnapshot{}, false
	}
	return snapshotFromManagedSubAgent(agent), true
}

func (s *Server) removeSubAgent(agentID string) (managedSubAgentSnapshot, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return managedSubAgentSnapshot{}, errMultiAgentIDRequired
	}

	s.subAgentMu.Lock()
	agent, exists := s.subAgents[agentID]
	if !exists {
		s.subAgentMu.Unlock()
		return managedSubAgentSnapshot{}, errMultiAgentNotFound
	}
	if agent.cancelCurrentTurn != nil {
		agent.cancelCurrentTurn()
		agent.cancelCurrentTurn = nil
	}
	snapshot := snapshotFromManagedSubAgent(agent)
	delete(s.subAgents, agentID)
	s.subAgentMu.Unlock()
	s.notifySubAgentUpdate(agent)
	return snapshot, nil
}

func (s *Server) closeSubAgent(agentID string) (managedSubAgentSnapshot, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return managedSubAgentSnapshot{}, errMultiAgentIDRequired
	}

	s.subAgentMu.Lock()
	agent, exists := s.subAgents[agentID]
	if !exists {
		s.subAgentMu.Unlock()
		return managedSubAgentSnapshot{}, errMultiAgentNotFound
	}
	if agent.cancelCurrentTurn != nil {
		agent.cancelCurrentTurn()
		agent.cancelCurrentTurn = nil
	}
	agent.Status = managedSubAgentStatusClosed
	agent.CurrentInput = ""
	agent.PendingInputs = []string{}
	agent.UpdatedAt = nowISO()
	snapshot := snapshotFromManagedSubAgent(agent)
	s.subAgentMu.Unlock()

	s.notifySubAgentUpdate(agent)
	return snapshot, nil
}

func (s *Server) startSubAgentTurn(agentID string, rawInput string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return errMultiAgentIDRequired
	}
	inputText := strings.TrimSpace(rawInput)
	if inputText == "" {
		return errMultiAgentEmptyAgentInput
	}

	s.subAgentMu.Lock()
	agent, exists := s.subAgents[agentID]
	if !exists {
		s.subAgentMu.Unlock()
		return errMultiAgentNotFound
	}
	if agent.Status == managedSubAgentStatusClosed {
		s.subAgentMu.Unlock()
		return errMultiAgentClosed
	}
	if agent.Status == managedSubAgentStatusRunning {
		s.subAgentMu.Unlock()
		return errMultiAgentBusy
	}

	runCtx, cancel := context.WithCancel(context.Background())
	agent.Status = managedSubAgentStatusRunning
	agent.CurrentInput = inputText
	agent.LastError = ""
	agent.UpdatedAt = nowISO()
	agent.cancelCurrentTurn = cancel
	snapshot := snapshotFromManagedSubAgent(agent)
	s.subAgentMu.Unlock()

	s.notifySubAgentUpdate(agent)

	go s.runSubAgentTurn(runCtx, snapshot.AgentID, inputText)
	return nil
}

func (s *Server) resumeSubAgent(agentID string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return errMultiAgentIDRequired
	}

	var (
		agent     *managedSubAgent
		nextInput string
	)
	s.subAgentMu.Lock()
	var exists bool
	agent, exists = s.subAgents[agentID]
	if !exists {
		s.subAgentMu.Unlock()
		return errMultiAgentNotFound
	}
	if agent.Status == managedSubAgentStatusRunning {
		s.subAgentMu.Unlock()
		return nil
	}
	if agent.Status == managedSubAgentStatusClosed {
		agent.Status = managedSubAgentStatusIdle
		agent.LastError = ""
	}
	for len(agent.PendingInputs) > 0 && nextInput == "" {
		nextInput = strings.TrimSpace(agent.PendingInputs[0])
		agent.PendingInputs = append([]string{}, agent.PendingInputs[1:]...)
	}
	agent.UpdatedAt = nowISO()
	s.subAgentMu.Unlock()
	s.notifySubAgentUpdate(agent)

	if nextInput == "" {
		return nil
	}
	return s.startSubAgentTurn(agentID, nextInput)
}

func (s *Server) runSubAgentTurn(ctx context.Context, agentID string, inputText string) {
	s.subAgentMu.Lock()
	agent, exists := s.subAgents[agentID]
	if !exists {
		s.subAgentMu.Unlock()
		return
	}
	sessionID := strings.TrimSpace(agent.SessionID)
	userID := strings.TrimSpace(agent.UserID)
	channel := strings.TrimSpace(agent.Channel)
	promptMode := strings.TrimSpace(agent.PromptMode)
	collaborationMode := strings.TrimSpace(agent.CollaborationMode)
	depth := agent.Depth
	s.subAgentMu.Unlock()

	request := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{Type: "text", Text: inputText},
				},
			},
		},
		SessionID: sessionID,
		UserID:    userID,
		Channel:   channel,
		Stream:    false,
		BizParams: map[string]interface{}{
			chatMetaPromptModeKey: promptMode,
		},
	}
	if strings.EqualFold(promptMode, promptModeCodex) {
		request.BizParams[collaborationBizParamsModeKey] = collaborationMode
	}
	request.BizParams[requestUserInputMetaAgentDepthKey] = depth

	response, processErr := s.processAgentViaPort(ctx, request)

	s.subAgentMu.Lock()
	current, exists := s.subAgents[agentID]
	if !exists || current != agent {
		s.subAgentMu.Unlock()
		return
	}
	current.cancelCurrentTurn = nil
	current.CurrentInput = ""
	current.UpdatedAt = nowISO()
	interrupted := ctx.Err() != nil
	if current.Status == managedSubAgentStatusClosed {
		current.LastReply = ""
		if processErr != nil && !interrupted {
			current.LastError = formatSubAgentProcessError(processErr)
		}
		s.subAgentMu.Unlock()
		s.notifySubAgentUpdate(agent)
		return
	}
	if processErr != nil {
		if interrupted {
			current.Status = managedSubAgentStatusIdle
			current.LastReply = ""
			current.LastError = ""
			s.subAgentMu.Unlock()
			s.notifySubAgentUpdate(agent)
			return
		}
		current.Status = managedSubAgentStatusFailed
		current.LastError = formatSubAgentProcessError(processErr)
	} else {
		current.Status = managedSubAgentStatusIdle
		current.LastReply = strings.TrimSpace(response.Reply)
		current.LastError = ""
		current.LastCompletedAt = nowISO()
	}
	s.subAgentMu.Unlock()

	s.notifySubAgentUpdate(agent)
}

func formatSubAgentProcessError(processErr *ports.AgentProcessError) string {
	if processErr == nil {
		return ""
	}
	cause := strings.TrimSpace(processErr.Message)
	if cause == "" {
		return "agent process failed"
	}
	if code := strings.TrimSpace(processErr.Code); code != "" {
		return code + ": " + cause
	}
	return cause
}

func snapshotFromManagedSubAgent(agent *managedSubAgent) managedSubAgentSnapshot {
	if agent == nil {
		return managedSubAgentSnapshot{}
	}
	return managedSubAgentSnapshot{
		AgentID:           strings.TrimSpace(agent.AgentID),
		SessionID:         strings.TrimSpace(agent.SessionID),
		UserID:            strings.TrimSpace(agent.UserID),
		Channel:           strings.TrimSpace(agent.Channel),
		PromptMode:        strings.TrimSpace(agent.PromptMode),
		CollaborationMode: strings.TrimSpace(agent.CollaborationMode),
		Depth:             agent.Depth,
		Status:            strings.TrimSpace(agent.Status),
		PendingInputs:     len(agent.PendingInputs),
		CurrentInput:      strings.TrimSpace(agent.CurrentInput),
		LastReply:         strings.TrimSpace(agent.LastReply),
		LastError:         strings.TrimSpace(agent.LastError),
		CreatedAt:         strings.TrimSpace(agent.CreatedAt),
		UpdatedAt:         strings.TrimSpace(agent.UpdatedAt),
		LastCompletedAt:   strings.TrimSpace(agent.LastCompletedAt),
	}
}

func (s *Server) notifySubAgentUpdate(agent *managedSubAgent) {
	if agent == nil || agent.waitCh == nil {
		return
	}
	select {
	case agent.waitCh <- struct{}{}:
	default:
	}
}

func encodeSubAgentToolPayload(payload map[string]interface{}) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invalid_result",
			Message: "multi-agent tool returned invalid result",
			Err:     err,
		}
	}
	return string(encoded), nil
}

func parseToolInputAgentDepth(input map[string]interface{}) int {
	if input == nil {
		return 0
	}
	if depth, ok := parseSubAgentNonNegativeIntAny(input[requestUserInputMetaAgentDepthKey]); ok {
		return depth
	}
	return 0
}

func parseSubAgentNonNegativeIntAny(raw interface{}) (int, bool) {
	switch value := raw.(type) {
	case int:
		if value >= 0 {
			return value, true
		}
	case int32:
		if value >= 0 {
			return int(value), true
		}
	case int64:
		if value >= 0 {
			return int(value), true
		}
	case float64:
		number := int(value)
		if float64(number) == value && number >= 0 {
			return number, true
		}
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, false
		}
		if trimmed == "0" {
			return 0, true
		}
		parsed, err := strconv.Atoi(trimmed)
		if err == nil && parsed >= 0 {
			return parsed, true
		}
	}
	return 0, false
}

func (s *Server) executeApplyPatchToolCall(ctx context.Context, input map[string]interface{}) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	patchText, workdir, err := parseApplyPatchInput(input)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "apply_patch" invocation failed`,
			Err:     err,
		}
	}
	binaryPath, err := exec.LookPath("apply_patch")
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "apply_patch" invocation failed`,
			Err:     errApplyPatchBinaryMissing,
		}
	}
	if strings.TrimSpace(workdir) == "" {
		if root, rootErr := findRepoRoot(); rootErr == nil {
			workdir = root
		}
	}

	cmd := exec.CommandContext(ctx, binaryPath)
	if strings.TrimSpace(workdir) != "" {
		cmd.Dir = workdir
	}
	cmd.Stdin = strings.NewReader(patchText)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	stdoutText := strings.TrimSpace(stdout.String())
	stderrText := strings.TrimSpace(stderr.String())
	if runErr != nil {
		detail := strings.TrimSpace(strings.Join([]string{stdoutText, stderrText}, "\n"))
		if detail == "" {
			detail = strings.TrimSpace(runErr.Error())
		}
		if detail == "" {
			detail = "apply_patch failed"
		}
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: `tool "apply_patch" invocation failed`,
			Err:     errors.New(detail),
		}
	}
	if stdoutText != "" {
		return stdoutText, nil
	}
	if stderrText != "" {
		return stderrText, nil
	}
	return "apply_patch completed", nil
}

func parseApplyPatchInput(input map[string]interface{}) (patch string, workdir string, err error) {
	item := firstToolInputItem(input)
	patch = firstNonEmptyRawString(item, "patch", "content", "text")
	if strings.TrimSpace(patch) == "" {
		patch = firstNonEmptyRawString(input, "patch", "content", "text")
	}
	if strings.TrimSpace(patch) == "" {
		return "", "", errApplyPatchPayloadMissing
	}
	workdir = strings.TrimSpace(firstNonEmptyString(item, "workdir", "cwd"))
	if workdir == "" {
		workdir = strings.TrimSpace(firstNonEmptyString(input, "workdir", "cwd"))
	}
	return patch, workdir, nil
}

func firstNonEmptyRawString(input map[string]interface{}, keys ...string) string {
	if len(input) == 0 {
		return ""
	}
	for _, key := range keys {
		raw, exists := input[key]
		if !exists || raw == nil {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		return value
	}
	return ""
}

func updatePlanSnapshotToMeta(snapshot updatePlanSnapshot) map[string]interface{} {
	planItems := make([]interface{}, 0, len(snapshot.Plan))
	for _, item := range snapshot.Plan {
		planItems = append(planItems, map[string]interface{}{
			"step":   item.Step,
			"status": item.Status,
		})
	}
	return map[string]interface{}{
		"explanation": snapshot.Explanation,
		"plan":        planItems,
		"updated_at":  snapshot.UpdatedAt,
	}
}

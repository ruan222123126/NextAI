package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	cronv3 "github.com/robfig/cron/v3"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
)

func (s *Server) listCronJobs(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.CronJobSpec, 0)
	s.store.Read(func(state *repo.State) {
		for _, job := range state.CronJobs {
			out = append(out, job)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createCronJob(w http.ResponseWriter, r *http.Request) {
	var req domain.CronJobSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if code, err := validateCronJobSpec(&req); err != nil {
		writeErr(w, http.StatusBadRequest, code, err.Error(), nil)
		return
	}
	now := time.Now().UTC()
	if err := s.store.Write(func(state *repo.State) error {
		state.CronJobs[req.ID] = req
		existing := state.CronStates[req.ID]
		state.CronStates[req.ID] = alignCronStateForMutation(req, normalizeCronPausedState(existing), now)
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) getCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	var spec domain.CronJobSpec
	var state domain.CronJobState
	found := false
	s.store.Read(func(st *repo.State) {
		spec, found = st.CronJobs[id]
		if found {
			state = st.CronStates[id]
		}
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, domain.CronJobView{Spec: spec, State: state})
}

func (s *Server) updateCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	var req domain.CronJobSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID != id {
		writeErr(w, http.StatusBadRequest, "job_id_mismatch", "job_id mismatch", nil)
		return
	}
	if code, err := validateCronJobSpec(&req); err != nil {
		writeErr(w, http.StatusBadRequest, code, err.Error(), nil)
		return
	}
	now := time.Now().UTC()
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return errors.New("not_found")
		}
		st.CronJobs[id] = req
		state := normalizeCronPausedState(st.CronStates[id])
		st.CronStates[id] = alignCronStateForMutation(req, state, now)
		return nil
	}); err != nil {
		if err.Error() == "not_found" {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) deleteCronJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "job_id"))
	deleted := false
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; ok {
			if id == domain.DefaultCronJobID {
				return errCronDefaultProtected
			}
			delete(st.CronJobs, id)
			delete(st.CronStates, id)
			deleted = true
		}
		return nil
	}); err != nil {
		if errors.Is(err, errCronDefaultProtected) {
			writeErr(w, http.StatusBadRequest, "default_cron_protected", "default cron job cannot be deleted", map[string]string{"job_id": domain.DefaultCronJobID})
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) pauseCronJob(w http.ResponseWriter, r *http.Request) {
	s.updateCronStatus(w, chi.URLParam(r, "job_id"), cronStatusPaused)
}

func (s *Server) resumeCronJob(w http.ResponseWriter, r *http.Request) {
	s.updateCronStatus(w, chi.URLParam(r, "job_id"), cronStatusResumed)
}

func (s *Server) runCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	if err := s.executeCronJob(id); err != nil {
		if errors.Is(err, errCronJobNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		if errors.Is(err, errCronMaxConcurrencyReached) {
			writeErr(w, http.StatusConflict, "cron_busy", "cron job reached max_concurrency", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"started": true})
}

func (s *Server) getCronJobState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	found := false
	var state domain.CronJobState
	s.store.Read(func(st *repo.State) {
		if _, ok := st.CronJobs[id]; ok {
			found = true
			state = st.CronStates[id]
		}
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) updateCronStatus(w http.ResponseWriter, id, status string) {
	now := time.Now().UTC()
	if err := s.store.Write(func(st *repo.State) error {
		job, ok := st.CronJobs[id]
		if !ok {
			return errors.New("not_found")
		}
		state := normalizeCronPausedState(st.CronStates[id])
		switch status {
		case cronStatusPaused:
			state.Paused = true
			state.NextRunAt = nil
		case cronStatusResumed:
			state.Paused = false
			state = alignCronStateForMutation(job, state, now)
		}
		state.LastStatus = &status
		st.CronStates[id] = state
		return nil
	}); err != nil {
		if err.Error() == "not_found" {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	key := "paused"
	if status == cronStatusResumed {
		key = "resumed"
	}
	writeJSON(w, http.StatusOK, map[string]bool{key: true})
}

func (s *Server) executeCronJob(id string) error {
	var job domain.CronJobSpec
	found := false
	s.store.Read(func(st *repo.State) {
		job, found = st.CronJobs[id]
	})
	if !found {
		return errCronJobNotFound
	}

	runtime := cronRuntimeSpec(job)
	slot, acquired, err := s.tryAcquireCronSlot(id, runtime)
	if err != nil {
		return err
	}
	if !acquired {
		if err := s.markCronExecutionSkipped(id, fmt.Sprintf("max_concurrency limit reached (%d)", runtime.MaxConcurrency)); err != nil {
			return err
		}
		return errCronMaxConcurrencyReached
	}
	defer s.releaseCronSlot(slot)

	startedAt := nowISO()
	running := cronStatusRunning

	if err := s.store.Write(func(st *repo.State) error {
		target, ok := st.CronJobs[id]
		if !ok {
			return errCronJobNotFound
		}
		job = target
		state := normalizeCronPausedState(st.CronStates[id])
		state.LastRunAt = &startedAt
		state.LastStatus = &running
		state.LastError = nil
		st.CronStates[id] = state
		return nil
	}); err != nil {
		return err
	}

	execCtx, cancel := context.WithTimeout(context.Background(), time.Duration(runtime.TimeoutSeconds)*time.Second)
	defer cancel()
	lastExecution, execErr := s.executeCronTask(execCtx, job)
	if errors.Is(execErr, context.DeadlineExceeded) {
		execErr = fmt.Errorf("cron execution timeout after %ds", runtime.TimeoutSeconds)
	}

	finalStatus := cronStatusSucceeded
	var finalErr *string
	if execErr != nil {
		finalStatus = cronStatusFailed
		msg := execErr.Error()
		finalErr = &msg
	}

	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return nil
		}
		state := st.CronStates[id]
		state.LastStatus = &finalStatus
		state.LastError = finalErr
		state.LastExecution = lastExecution
		st.CronStates[id] = state
		return nil
	}); err != nil {
		return err
	}

	return execErr
}

func (s *Server) executeCronTask(ctx context.Context, job domain.CronJobSpec) (*domain.CronWorkflowExecution, error) {
	if s.cronTaskExecutor != nil {
		return nil, s.cronTaskExecutor(ctx, job)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch cronTaskType(job) {
	case cronTaskTypeText:
		text := strings.TrimSpace(job.Text)
		if text == "" {
			return nil, errors.New("cron text task requires non-empty text")
		}
		return nil, s.executeCronTextTask(ctx, job, text)
	case cronTaskTypeWorkflow:
		execution, err := s.executeCronWorkflowTask(ctx, job)
		return execution, err
	default:
		return nil, fmt.Errorf("unsupported cron task_type=%q", job.TaskType)
	}
}

func (s *Server) executeCronTextTask(ctx context.Context, job domain.CronJobSpec, text string) error {
	channelName := strings.ToLower(resolveCronDispatchChannel(job))
	if channelName == qqChannelName {
		return errors.New("cron dispatch channel \"qq\" is inbound-only; use channel \"console\" to persist chat history")
	}
	channelPlugin, channelCfg, resolvedChannelName, err := s.resolveChannel(channelName)
	if err != nil {
		return err
	}
	if resolvedChannelName == "console" {
		return s.executeCronConsoleAgentTask(ctx, job, text)
	}
	if err := channelPlugin.SendText(ctx, job.Dispatch.Target.UserID, job.Dispatch.Target.SessionID, text, channelCfg); err != nil {
		return &channelError{
			Code:    "channel_dispatch_failed",
			Message: fmt.Sprintf("failed to dispatch cron job to channel %q", resolvedChannelName),
			Err:     err,
		}
	}
	return nil
}

func (s *Server) executeCronWorkflowTask(ctx context.Context, job domain.CronJobSpec) (*domain.CronWorkflowExecution, error) {
	plan, err := buildCronWorkflowPlan(job.Workflow)
	if err != nil {
		return nil, fmt.Errorf("invalid cron workflow: %w", err)
	}

	startedAt := nowISO()
	execution := &domain.CronWorkflowExecution{
		RunID:       newCronRunID(),
		StartedAt:   startedAt,
		HadFailures: false,
		Nodes:       make([]domain.CronWorkflowNodeExecution, 0, len(plan.Order)),
	}

	var firstErr error
	for idx, node := range plan.Order {
		step := domain.CronWorkflowNodeExecution{
			NodeID:          node.ID,
			NodeType:        node.Type,
			ContinueOnError: node.ContinueOnError,
			StartedAt:       nowISO(),
		}

		runResult, runErr := s.executeCronWorkflowNode(ctx, job, node)
		finishedAt := nowISO()
		step.FinishedAt = &finishedAt
		if runErr != nil {
			step.Status = cronStatusFailed
			errText := runErr.Error()
			step.Error = &errText
			execution.HadFailures = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow node %s failed: %w", node.ID, runErr)
			}
		} else {
			step.Status = cronStatusSucceeded
		}
		execution.Nodes = append(execution.Nodes, step)

		forceStop := runErr != nil && (errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded))
		shouldStop := runResult.Stop || (runErr != nil && (!node.ContinueOnError || forceStop))
		if !shouldStop {
			continue
		}
		for j := idx + 1; j < len(plan.Order); j++ {
			skippedNode := plan.Order[j]
			skippedAt := nowISO()
			skipped := domain.CronWorkflowNodeExecution{
				NodeID:          skippedNode.ID,
				NodeType:        skippedNode.Type,
				Status:          cronWorkflowNodeExecutionSkipped,
				ContinueOnError: skippedNode.ContinueOnError,
				StartedAt:       skippedAt,
				FinishedAt:      &skippedAt,
			}
			execution.Nodes = append(execution.Nodes, skipped)
		}
		break
	}

	finishedAt := nowISO()
	execution.FinishedAt = &finishedAt
	return execution, firstErr
}

type cronWorkflowNodeRunResult struct {
	Stop bool
}

func (s *Server) executeCronWorkflowNode(ctx context.Context, job domain.CronJobSpec, node domain.CronWorkflowNode) (cronWorkflowNodeRunResult, error) {
	switch node.Type {
	case cronWorkflowNodeText:
		text := strings.TrimSpace(node.Text)
		if text == "" {
			return cronWorkflowNodeRunResult{}, errors.New("workflow text_event requires non-empty text")
		}
		return cronWorkflowNodeRunResult{}, s.executeCronTextTask(ctx, job, text)
	case cronWorkflowNodeDelay:
		return cronWorkflowNodeRunResult{}, executeCronWorkflowDelay(ctx, node.DelaySeconds)
	case cronWorkflowNodeIf:
		matched, err := evaluateCronWorkflowIfCondition(node.IfCondition, job)
		if err != nil {
			return cronWorkflowNodeRunResult{}, err
		}
		if !matched {
			return cronWorkflowNodeRunResult{Stop: true}, nil
		}
		return cronWorkflowNodeRunResult{}, nil
	default:
		return cronWorkflowNodeRunResult{}, fmt.Errorf("unsupported workflow node type=%q", node.Type)
	}
}

func executeCronWorkflowDelay(ctx context.Context, seconds int) error {
	if seconds < 0 {
		return errors.New("workflow delay_seconds must be greater than or equal to 0")
	}
	if seconds == 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type cronWorkflowIfCondition struct {
	Field    string
	Operator string
	Value    string
}

func parseCronWorkflowIfCondition(raw string) (cronWorkflowIfCondition, error) {
	condition := strings.TrimSpace(raw)
	if condition == "" {
		return cronWorkflowIfCondition{}, errors.New("if_condition is required")
	}
	parts := cronWorkflowIfConditionPattern.FindStringSubmatch(condition)
	if len(parts) == 0 {
		return cronWorkflowIfCondition{}, errors.New("if_condition must match `<field> == <value>` or `<field> != <value>`")
	}
	field := strings.ToLower(strings.TrimSpace(parts[1]))
	if _, ok := cronWorkflowIfAllowedFields[field]; !ok {
		return cronWorkflowIfCondition{}, fmt.Errorf("if_condition field %q is unsupported", field)
	}
	value := parts[3]
	if value == "" {
		value = parts[4]
	}
	if value == "" {
		value = parts[5]
	}
	return cronWorkflowIfCondition{
		Field:    field,
		Operator: parts[2],
		Value:    value,
	}, nil
}

func evaluateCronWorkflowIfCondition(raw string, job domain.CronJobSpec) (bool, error) {
	condition, err := parseCronWorkflowIfCondition(raw)
	if err != nil {
		return false, err
	}
	ctx := cronWorkflowIfContext(job)
	left, ok := ctx[condition.Field]
	if !ok {
		return false, fmt.Errorf("if_condition field %q is unsupported", condition.Field)
	}
	switch condition.Operator {
	case "==":
		return left == condition.Value, nil
	case "!=":
		return left != condition.Value, nil
	default:
		return false, fmt.Errorf("if_condition operator %q is unsupported", condition.Operator)
	}
}

func cronWorkflowIfContext(job domain.CronJobSpec) map[string]string {
	return map[string]string{
		"job_id":     strings.TrimSpace(job.ID),
		"job_name":   strings.TrimSpace(job.Name),
		"channel":    strings.ToLower(strings.TrimSpace(resolveCronDispatchChannel(job))),
		"user_id":    strings.TrimSpace(job.Dispatch.Target.UserID),
		"session_id": strings.TrimSpace(job.Dispatch.Target.SessionID),
		"task_type":  strings.ToLower(strings.TrimSpace(job.TaskType)),
	}
}

func (s *Server) executeCronConsoleAgentTask(ctx context.Context, job domain.CronJobSpec, text string) error {
	sessionID := strings.TrimSpace(job.Dispatch.Target.SessionID)
	userID := strings.TrimSpace(job.Dispatch.Target.UserID)
	if sessionID == "" || userID == "" {
		return errors.New("cron dispatch target requires non-empty session_id and user_id")
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	agentReq := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{Type: "text", Text: text},
				},
			},
		},
		SessionID: sessionID,
		UserID:    userID,
		Channel:   "console",
		Stream:    false,
		BizParams: buildCronBizParams(job),
	}

	body, err := json.Marshal(agentReq)
	if err != nil {
		return fmt.Errorf("cron console agent request marshal failed: %w", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/agent/process", nil).WithContext(ctx)
	s.processAgentWithBody(recorder, request, body)

	status := recorder.Result().StatusCode
	if status >= http.StatusBadRequest {
		return fmt.Errorf("cron console agent execution failed: status=%d body=%s", status, strings.TrimSpace(recorder.Body.String()))
	}

	return nil
}

func resolveCronDispatchChannel(job domain.CronJobSpec) string {
	channelName := strings.TrimSpace(job.Dispatch.Channel)
	if channelName == "" {
		return "console"
	}
	return channelName
}

func buildCronBizParams(job domain.CronJobSpec) map[string]interface{} {
	jobID := strings.TrimSpace(job.ID)
	jobName := strings.TrimSpace(job.Name)
	if jobID == "" && jobName == "" {
		return nil
	}
	cronPayload := map[string]interface{}{}
	if jobID != "" {
		cronPayload["job_id"] = jobID
	}
	if jobName != "" {
		cronPayload["job_name"] = jobName
	}
	return map[string]interface{}{
		"cron": cronPayload,
	}
}

func validateCronJobSpec(job *domain.CronJobSpec) (string, error) {
	if job == nil {
		return "invalid_cron_task_type", errors.New("cron job is required")
	}
	job.ID = strings.TrimSpace(job.ID)
	job.Name = strings.TrimSpace(job.Name)
	if job.ID == "" || job.Name == "" {
		return "invalid_cron_task_type", errors.New("id and name are required")
	}

	taskType := cronTaskType(*job)
	switch taskType {
	case cronTaskTypeText:
		text := strings.TrimSpace(job.Text)
		if text == "" {
			return "invalid_cron_task_type", errors.New("text is required for task_type=text")
		}
		job.TaskType = cronTaskTypeText
		job.Text = text
		job.Workflow = nil
		return "", nil
	case cronTaskTypeWorkflow:
		plan, err := buildCronWorkflowPlan(job.Workflow)
		if err != nil {
			return "invalid_cron_workflow", err
		}
		job.TaskType = cronTaskTypeWorkflow
		job.Workflow = &plan.Workflow
		job.Text = ""
		return "", nil
	default:
		return "invalid_cron_task_type", fmt.Errorf("unsupported task_type=%q", strings.TrimSpace(job.TaskType))
	}
}

func cronTaskType(job domain.CronJobSpec) string {
	taskType := strings.ToLower(strings.TrimSpace(job.TaskType))
	if taskType != "" {
		return taskType
	}
	if job.Workflow != nil {
		return cronTaskTypeWorkflow
	}
	if strings.TrimSpace(job.Text) != "" {
		return cronTaskTypeText
	}
	return taskType
}

func buildCronWorkflowPlan(workflow *domain.CronWorkflowSpec) (*cronWorkflowPlan, error) {
	if workflow == nil {
		return nil, errors.New("workflow is required for task_type=workflow")
	}

	version := strings.ToLower(strings.TrimSpace(workflow.Version))
	if version != cronWorkflowVersionV1 {
		return nil, fmt.Errorf("unsupported workflow version=%q", workflow.Version)
	}
	if len(workflow.Nodes) < 2 {
		return nil, errors.New("workflow requires at least 2 nodes")
	}
	if len(workflow.Edges) < 1 {
		return nil, errors.New("workflow requires at least 1 edge")
	}

	nodeByID := make(map[string]domain.CronWorkflowNode, len(workflow.Nodes))
	normalizedNodes := make([]domain.CronWorkflowNode, 0, len(workflow.Nodes))
	startID := ""

	for _, rawNode := range workflow.Nodes {
		node := rawNode
		node.ID = strings.TrimSpace(node.ID)
		node.Type = strings.ToLower(strings.TrimSpace(node.Type))
		node.Title = strings.TrimSpace(node.Title)
		node.Text = strings.TrimSpace(node.Text)
		node.IfCondition = strings.TrimSpace(node.IfCondition)

		if node.ID == "" {
			return nil, errors.New("workflow node id is required")
		}
		if _, exists := nodeByID[node.ID]; exists {
			return nil, fmt.Errorf("workflow node id duplicated: %s", node.ID)
		}

		switch node.Type {
		case cronWorkflowNodeStart:
			node.ContinueOnError = false
			node.DelaySeconds = 0
			node.Text = ""
			node.IfCondition = ""
			if startID != "" {
				return nil, errors.New("workflow requires exactly one start node")
			}
			startID = node.ID
		case cronWorkflowNodeText:
			node.DelaySeconds = 0
			node.IfCondition = ""
			if node.Text == "" {
				return nil, fmt.Errorf("workflow node %s requires non-empty text", node.ID)
			}
		case cronWorkflowNodeDelay:
			node.Text = ""
			node.IfCondition = ""
			if node.DelaySeconds < 0 {
				return nil, fmt.Errorf("workflow node %s delay_seconds must be greater than or equal to 0", node.ID)
			}
		case cronWorkflowNodeIf:
			node.Text = ""
			node.DelaySeconds = 0
			if _, err := parseCronWorkflowIfCondition(node.IfCondition); err != nil {
				return nil, fmt.Errorf("workflow node %s if_condition invalid: %w", node.ID, err)
			}
		default:
			return nil, fmt.Errorf("workflow node %s has unsupported type=%q", node.ID, node.Type)
		}

		nodeByID[node.ID] = node
		normalizedNodes = append(normalizedNodes, node)
	}

	if startID == "" {
		return nil, errors.New("workflow requires exactly one start node")
	}

	edgeIDSet := map[string]struct{}{}
	nextByID := map[string]string{}
	inDegree := map[string]int{}
	outDegree := map[string]int{}
	normalizedEdges := make([]domain.CronWorkflowEdge, 0, len(workflow.Edges))

	for _, rawEdge := range workflow.Edges {
		edge := rawEdge
		edge.ID = strings.TrimSpace(edge.ID)
		edge.Source = strings.TrimSpace(edge.Source)
		edge.Target = strings.TrimSpace(edge.Target)

		if edge.ID == "" {
			return nil, errors.New("workflow edge id is required")
		}
		if _, exists := edgeIDSet[edge.ID]; exists {
			return nil, fmt.Errorf("workflow edge id duplicated: %s", edge.ID)
		}
		edgeIDSet[edge.ID] = struct{}{}

		if edge.Source == "" || edge.Target == "" {
			return nil, fmt.Errorf("workflow edge %s requires source and target", edge.ID)
		}
		if edge.Source == edge.Target {
			return nil, fmt.Errorf("workflow edge %s cannot link node to itself", edge.ID)
		}
		if _, ok := nodeByID[edge.Source]; !ok {
			return nil, fmt.Errorf("workflow edge %s source not found: %s", edge.ID, edge.Source)
		}
		if _, ok := nodeByID[edge.Target]; !ok {
			return nil, fmt.Errorf("workflow edge %s target not found: %s", edge.ID, edge.Target)
		}

		outDegree[edge.Source]++
		if outDegree[edge.Source] > 1 {
			return nil, fmt.Errorf("workflow node %s has more than one outgoing edge", edge.Source)
		}
		inDegree[edge.Target]++
		if inDegree[edge.Target] > 1 {
			return nil, fmt.Errorf("workflow node %s has more than one incoming edge", edge.Target)
		}
		nextByID[edge.Source] = edge.Target
		normalizedEdges = append(normalizedEdges, edge)
	}

	if inDegree[startID] > 0 {
		return nil, errors.New("workflow start node cannot have incoming edge")
	}
	if outDegree[startID] == 0 {
		return nil, errors.New("workflow start node must connect to at least one executable node")
	}

	reachable := map[string]bool{startID: true}
	order := make([]domain.CronWorkflowNode, 0, len(nodeByID)-1)
	cursor := startID
	for {
		nextID, ok := nextByID[cursor]
		if !ok {
			break
		}
		if reachable[nextID] {
			return nil, errors.New("workflow graph must be acyclic")
		}
		reachable[nextID] = true
		nextNode := nodeByID[nextID]
		if nextNode.Type == cronWorkflowNodeStart {
			return nil, errors.New("workflow start node cannot be targeted by execution path")
		}
		order = append(order, nextNode)
		cursor = nextID
	}

	if len(order) == 0 {
		return nil, errors.New("workflow requires at least one executable node")
	}
	for nodeID, node := range nodeByID {
		if node.Type == cronWorkflowNodeStart {
			continue
		}
		if !reachable[nodeID] {
			return nil, fmt.Errorf("workflow node %s is not reachable from start", nodeID)
		}
	}

	var viewport *domain.CronWorkflowViewport
	if workflow.Viewport != nil {
		v := *workflow.Viewport
		if v.Zoom <= 0 {
			v.Zoom = 1
		}
		viewport = &v
	}

	return &cronWorkflowPlan{
		Workflow: domain.CronWorkflowSpec{
			Version:  cronWorkflowVersionV1,
			Viewport: viewport,
			Nodes:    normalizedNodes,
			Edges:    normalizedEdges,
		},
		StartID:  startID,
		NodeByID: nodeByID,
		NextByID: nextByID,
		Order:    order,
	}, nil
}

func alignCronStateForMutation(job domain.CronJobSpec, state domain.CronJobState, now time.Time) domain.CronJobState {
	if !cronJobSchedulable(job, state) {
		state.NextRunAt = nil
		return state
	}
	nextRunAt, _, err := resolveCronNextRunAt(job, nil, now)
	if err != nil {
		msg := err.Error()
		state.LastError = &msg
		state.NextRunAt = nil
		return state
	}

	nextRunAtText := nextRunAt.Format(time.RFC3339)
	state.NextRunAt = &nextRunAtText
	state.LastError = nil
	return state
}

func normalizeCronPausedState(state domain.CronJobState) domain.CronJobState {
	if !state.Paused && state.LastStatus != nil && *state.LastStatus == cronStatusPaused {
		state.Paused = true
	}
	return state
}

func cronStateEqual(a, b domain.CronJobState) bool {
	return cronStringPtrEqual(a.NextRunAt, b.NextRunAt) &&
		cronStringPtrEqual(a.LastRunAt, b.LastRunAt) &&
		cronStringPtrEqual(a.LastStatus, b.LastStatus) &&
		cronStringPtrEqual(a.LastError, b.LastError) &&
		a.Paused == b.Paused
}

func cronStringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cronJobSchedulable(job domain.CronJobSpec, state domain.CronJobState) bool {
	return job.Enabled && !state.Paused
}

type cronLeaseSlot struct {
	LeaseID string `json:"lease_id"`
	JobID   string `json:"job_id"`
	Owner   string `json:"owner"`
	Slot    int    `json:"slot"`

	AcquiredAt string `json:"acquired_at"`
	ExpiresAt  string `json:"expires_at"`
}

type cronLeaseHandle struct {
	Path    string
	LeaseID string
}

func (s *Server) tryAcquireCronSlot(jobID string, runtime domain.CronRuntimeSpec) (*cronLeaseHandle, bool, error) {
	maxConcurrency := runtime.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}

	now := time.Now().UTC()
	ttl := time.Duration(runtime.TimeoutSeconds)*time.Second + 30*time.Second
	if ttl < 30*time.Second {
		ttl = 30 * time.Second
	}

	leaseID := newCronLeaseID()
	dir := filepath.Join(s.cfg.DataDir, cronLeaseDirName, encodeCronJobID(jobID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, err
	}

	for slot := 0; slot < maxConcurrency; slot++ {
		path := filepath.Join(dir, fmt.Sprintf("slot-%d.json", slot))
		if err := cleanupExpiredCronLease(path, now); err != nil {
			return nil, false, err
		}

		lease := cronLeaseSlot{
			LeaseID:    leaseID,
			JobID:      jobID,
			Owner:      fmt.Sprintf("pid:%d", os.Getpid()),
			Slot:       slot,
			AcquiredAt: now.Format(time.RFC3339Nano),
			ExpiresAt:  now.Add(ttl).Format(time.RFC3339Nano),
		}
		body, err := json.Marshal(lease)
		if err != nil {
			return nil, false, err
		}

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, false, err
		}

		if _, err := f.Write(body); err != nil {
			_ = f.Close()
			_ = removeIfExists(path)
			return nil, false, err
		}
		if err := f.Close(); err != nil {
			_ = removeIfExists(path)
			return nil, false, err
		}
		return &cronLeaseHandle{Path: path, LeaseID: leaseID}, true, nil
	}
	return nil, false, nil
}

func (s *Server) releaseCronSlot(slot *cronLeaseHandle) {
	if slot == nil || strings.TrimSpace(slot.Path) == "" {
		return
	}

	body, err := os.ReadFile(slot.Path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("release cron lease read failed: path=%s err=%v", slot.Path, err)
		return
	}

	var lease cronLeaseSlot
	if err := json.Unmarshal(body, &lease); err != nil {
		if rmErr := removeIfExists(slot.Path); rmErr != nil {
			log.Printf("release cron lease cleanup failed: path=%s err=%v", slot.Path, rmErr)
		}
		return
	}
	if lease.LeaseID != slot.LeaseID {
		return
	}
	if err := os.Remove(slot.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("release cron lease failed: path=%s err=%v", slot.Path, err)
	}
}

func cleanupExpiredCronLease(path string, now time.Time) error {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var lease cronLeaseSlot
	if err := json.Unmarshal(body, &lease); err != nil {
		return removeIfExists(path)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(lease.ExpiresAt))
	if err != nil {
		return removeIfExists(path)
	}
	if !now.After(expiresAt.UTC()) {
		return nil
	}
	return removeIfExists(path)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func encodeCronJobID(jobID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(jobID))
}

func newCronLeaseID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%x", os.Getpid(), buf)
}

func newCronRunID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("run-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("run-%d-%x", os.Getpid(), buf)
}

func (s *Server) markCronExecutionSkipped(id, message string) error {
	failed := cronStatusFailed
	return s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return errCronJobNotFound
		}
		state := normalizeCronPausedState(st.CronStates[id])
		state.LastStatus = &failed
		state.LastError = &message
		st.CronStates[id] = state
		return nil
	})
}

func cronRuntimeSpec(job domain.CronJobSpec) domain.CronRuntimeSpec {
	out := job.Runtime
	if out.MaxConcurrency <= 0 {
		out.MaxConcurrency = 1
	}
	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = 30
	}
	if out.MisfireGraceSeconds < 0 {
		out.MisfireGraceSeconds = 0
	}
	return out
}

func cronScheduleType(job domain.CronJobSpec) string {
	scheduleType := strings.ToLower(strings.TrimSpace(job.Schedule.Type))
	if scheduleType == "" {
		return "interval"
	}
	return scheduleType
}

func cronInterval(job domain.CronJobSpec) (time.Duration, error) {
	if cronScheduleType(job) != "interval" {
		return 0, fmt.Errorf("unsupported schedule.type=%q", job.Schedule.Type)
	}

	raw := strings.TrimSpace(job.Schedule.Cron)
	if raw == "" {
		return 0, errors.New("schedule.cron is required for interval jobs")
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0, errors.New("schedule interval must be greater than 0")
		}
		return time.Duration(secs) * time.Second, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid schedule interval: %q", raw)
	}
	return parsed, nil
}

func resolveCronNextRunAt(job domain.CronJobSpec, current *string, now time.Time) (time.Time, *time.Time, error) {
	switch cronScheduleType(job) {
	case "interval":
		interval, err := cronInterval(job)
		if err != nil {
			return time.Time{}, nil, err
		}
		next, dueAt := resolveIntervalNextRunAt(current, interval, now)
		return next, dueAt, nil
	case "cron":
		schedule, loc, err := cronExpression(job)
		if err != nil {
			return time.Time{}, nil, err
		}
		next, dueAt := resolveExpressionNextRunAt(current, schedule, loc, now)
		return next, dueAt, nil
	default:
		return time.Time{}, nil, fmt.Errorf("unsupported schedule.type=%q", job.Schedule.Type)
	}
}

func cronExpression(job domain.CronJobSpec) (cronv3.Schedule, *time.Location, error) {
	raw := strings.TrimSpace(job.Schedule.Cron)
	if raw == "" {
		return nil, nil, errors.New("schedule.cron is required for cron jobs")
	}

	loc := time.UTC
	if tz := strings.TrimSpace(job.Schedule.Timezone); tz != "" {
		nextLoc, err := time.LoadLocation(tz)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid schedule.timezone=%q", job.Schedule.Timezone)
		}
		loc = nextLoc
	}

	parser := cronv3.NewParser(cronv3.SecondOptional | cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow | cronv3.Descriptor)
	schedule, err := parser.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	return schedule, loc, nil
}

func resolveIntervalNextRunAt(current *string, interval time.Duration, now time.Time) (time.Time, *time.Time) {
	next := now.Add(interval)
	if current == nil {
		return next, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*current))
	if err != nil {
		return next, nil
	}
	if parsed.After(now) {
		return parsed, nil
	}

	dueAt := parsed
	for !parsed.After(now) {
		parsed = parsed.Add(interval)
	}
	return parsed, &dueAt
}

func resolveExpressionNextRunAt(current *string, schedule cronv3.Schedule, loc *time.Location, now time.Time) (time.Time, *time.Time) {
	nowInLoc := now.In(loc)
	next := schedule.Next(nowInLoc).UTC()
	if current == nil {
		return next, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*current))
	if err != nil {
		return next, nil
	}
	if parsed.After(now) {
		return parsed, nil
	}

	dueAt := parsed
	cursor := parsed.In(loc)
	for i := 0; i < 2048 && !cursor.After(nowInLoc); i++ {
		nextCursor := schedule.Next(cursor)
		if !nextCursor.After(cursor) {
			return schedule.Next(nowInLoc).UTC(), &dueAt
		}
		cursor = nextCursor
	}
	if !cursor.After(nowInLoc) {
		cursor = schedule.Next(nowInLoc)
	}
	return cursor.UTC(), &dueAt
}

func cronMisfireExceeded(dueAt *time.Time, runtime domain.CronRuntimeSpec, now time.Time) bool {
	if dueAt == nil {
		return false
	}
	if runtime.MisfireGraceSeconds <= 0 {
		return false
	}
	grace := time.Duration(runtime.MisfireGraceSeconds) * time.Second
	return now.Sub(dueAt.UTC()) > grace
}

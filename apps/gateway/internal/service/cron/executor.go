package cron

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"nextai/apps/gateway/internal/domain"
	workflowengine "nextai/apps/gateway/internal/service/cron/workflow"
	"nextai/apps/gateway/internal/service/ports"
)

func (s *Service) executeJob(jobID string) error {
	if err := s.validateStore(); err != nil {
		return err
	}

	var job domain.CronJobSpec
	found := false
	s.deps.Store.ReadCron(func(st ports.CronAggregate) {
		job, found = st.Jobs[jobID]
	})
	if !found {
		return ErrJobNotFound
	}

	runtime := runtimeSpec(job)
	slot, acquired, err := s.tryAcquireSlot(jobID, runtime)
	if err != nil {
		return err
	}
	if !acquired {
		if err := s.markExecutionSkipped(jobID, fmt.Sprintf("max_concurrency limit reached (%d)", runtime.MaxConcurrency)); err != nil {
			return err
		}
		return ErrMaxConcurrencyReached
	}
	defer s.releaseSlot(slot)

	startedAt := nowISO()
	running := statusRunning
	if err := s.deps.Store.WriteCron(func(st *ports.CronAggregate) error {
		target, ok := st.Jobs[jobID]
		if !ok {
			return ErrJobNotFound
		}
		job = target
		state := normalizePausedState(st.States[jobID])
		state.LastRunAt = &startedAt
		state.LastStatus = &running
		state.LastError = nil
		st.States[jobID] = state
		return nil
	}); err != nil {
		return err
	}

	execCtx, cancel := context.WithTimeout(context.Background(), time.Duration(runtime.TimeoutSeconds)*time.Second)
	defer cancel()
	lastExecution, execErr := s.executeTask(execCtx, job)
	if errors.Is(execErr, context.DeadlineExceeded) {
		execErr = fmt.Errorf("cron execution timeout after %ds", runtime.TimeoutSeconds)
	}

	finalStatus := statusSucceeded
	var finalErr *string
	if execErr != nil {
		finalStatus = statusFailed
		msg := execErr.Error()
		finalErr = &msg
	}
	if err := s.deps.Store.WriteCron(func(st *ports.CronAggregate) error {
		if _, ok := st.Jobs[jobID]; !ok {
			return nil
		}
		state := st.States[jobID]
		state.LastStatus = &finalStatus
		state.LastError = finalErr
		state.LastExecution = lastExecution
		st.States[jobID] = state
		return nil
	}); err != nil {
		return err
	}

	return execErr
}

func (s *Service) executeTask(ctx context.Context, job domain.CronJobSpec) (*domain.CronWorkflowExecution, error) {
	if s.deps.ExecuteTask != nil {
		handled, err := s.deps.ExecuteTask(ctx, job)
		if handled {
			return nil, err
		}
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch taskType(job) {
	case taskTypeText:
		text := strings.TrimSpace(job.Text)
		if text == "" {
			return nil, errors.New("cron text task requires non-empty text")
		}
		return nil, s.executeTextTask(ctx, job, text)
	case taskTypeWorkflow:
		execution, err := s.executeWorkflowTask(ctx, job)
		return execution, err
	default:
		return nil, fmt.Errorf("unsupported cron task_type=%q", job.TaskType)
	}
}

func (s *Service) executeTextTask(ctx context.Context, job domain.CronJobSpec, text string) error {
	channelName := strings.ToLower(resolveDispatchChannel(job))
	if channelName == qqChannelName {
		return errors.New("cron dispatch channel \"qq\" is inbound-only; use channel \"console\" to persist chat history")
	}
	if s.deps.ChannelResolver == nil {
		return errors.New("cron channel resolver is unavailable")
	}
	channelPlugin, channelCfg, resolvedChannelName, err := s.deps.ChannelResolver.ResolveChannel(channelName)
	if err != nil {
		return err
	}
	if resolvedChannelName == "console" {
		if s.deps.ExecuteConsoleAgentTask == nil {
			return errors.New("cron console agent executor is unavailable")
		}
		return s.deps.ExecuteConsoleAgentTask(ctx, job, text)
	}
	if err := channelPlugin.SendText(ctx, job.Dispatch.Target.UserID, job.Dispatch.Target.SessionID, text, channelCfg); err != nil {
		return &channelError{
			Message: fmt.Sprintf("failed to dispatch cron job to channel %q", resolvedChannelName),
			Err:     err,
		}
	}
	return nil
}

func (s *Service) executeWorkflowTask(ctx context.Context, job domain.CronJobSpec) (*domain.CronWorkflowExecution, error) {
	plan, err := s.buildWorkflowPlan(job.Workflow)
	if err != nil {
		return nil, fmt.Errorf("invalid cron workflow: %w", err)
	}
	return workflowengine.Run(ctx, job, plan, s.executeWorkflowNode, nowISO, newRunID)
}

func (s *Service) executeWorkflowNode(
	ctx context.Context,
	job domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (workflowengine.NodeResult, error) {
	handler, ok := s.resolveCronNodeHandler(node.Type)
	if !ok {
		return workflowengine.NodeResult{}, fmt.Errorf("unsupported workflow node type=%q", node.Type)
	}
	result, err := handler.Execute(ctx, job, node)
	if err != nil {
		return workflowengine.NodeResult{}, err
	}
	return result, nil
}

func executeWorkflowDelay(ctx context.Context, seconds int) error {
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

type workflowIfCondition struct {
	Field    string
	Operator string
	Value    string
}

func parseWorkflowIfCondition(raw string) (workflowIfCondition, error) {
	condition := strings.TrimSpace(raw)
	if condition == "" {
		return workflowIfCondition{}, errors.New("if_condition is required")
	}
	parts := workflowIfConditionPattern.FindStringSubmatch(condition)
	if len(parts) == 0 {
		return workflowIfCondition{}, errors.New("if_condition must match `<field> == <value>` or `<field> != <value>`")
	}
	field := strings.ToLower(strings.TrimSpace(parts[1]))
	if _, ok := workflowIfAllowedFields[field]; !ok {
		return workflowIfCondition{}, fmt.Errorf("if_condition field %q is unsupported", field)
	}
	value := parts[3]
	if value == "" {
		value = parts[4]
	}
	if value == "" {
		value = parts[5]
	}
	return workflowIfCondition{Field: field, Operator: parts[2], Value: value}, nil
}

func evaluateWorkflowIfCondition(raw string, job domain.CronJobSpec) (bool, error) {
	condition, err := parseWorkflowIfCondition(raw)
	if err != nil {
		return false, err
	}
	ctx := workflowIfContext(job)
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

func workflowIfContext(job domain.CronJobSpec) map[string]string {
	return map[string]string{
		"job_id":     strings.TrimSpace(job.ID),
		"job_name":   strings.TrimSpace(job.Name),
		"channel":    strings.ToLower(strings.TrimSpace(resolveDispatchChannel(job))),
		"user_id":    strings.TrimSpace(job.Dispatch.Target.UserID),
		"session_id": strings.TrimSpace(job.Dispatch.Target.SessionID),
		"task_type":  strings.ToLower(strings.TrimSpace(job.TaskType)),
	}
}

func buildBizParams(job domain.CronJobSpec) map[string]interface{} {
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
	return map[string]interface{}{"cron": cronPayload}
}

func newRunID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("run-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("run-%d-%x", os.Getpid(), buf)
}

func (s *Service) markExecutionSkipped(jobID, message string) error {
	failed := statusFailed
	return s.deps.Store.WriteCron(func(st *ports.CronAggregate) error {
		if _, ok := st.Jobs[jobID]; !ok {
			return ErrJobNotFound
		}
		state := normalizePausedState(st.States[jobID])
		state.LastStatus = &failed
		state.LastError = &message
		st.States[jobID] = state
		return nil
	})
}

func resolveDispatchChannel(job domain.CronJobSpec) string {
	channelName := strings.TrimSpace(job.Dispatch.Channel)
	if channelName == "" {
		return "console"
	}
	return channelName
}

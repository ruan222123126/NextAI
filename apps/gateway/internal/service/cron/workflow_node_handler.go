package cron

import (
	"context"
	"errors"
	"strings"

	"nextai/apps/gateway/internal/domain"
)

type CronNodeResult struct {
	Stop bool
}

type CronNodeHandler interface {
	Type() string
	Execute(ctx context.Context, job domain.CronJobSpec, node domain.CronWorkflowNode) (CronNodeResult, error)
}

type textWorkflowNodeHandler struct {
	executeTextTask func(ctx context.Context, job domain.CronJobSpec, text string) error
}

func (h *textWorkflowNodeHandler) Type() string {
	return workflowNodeText
}

func (h *textWorkflowNodeHandler) Execute(
	ctx context.Context,
	job domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (CronNodeResult, error) {
	if h == nil || h.executeTextTask == nil {
		return CronNodeResult{}, errors.New("cron text node executor is unavailable")
	}
	text := strings.TrimSpace(node.Text)
	if text == "" {
		return CronNodeResult{}, errors.New("workflow text_event requires non-empty text")
	}
	return CronNodeResult{}, h.executeTextTask(ctx, job, text)
}

type delayWorkflowNodeHandler struct{}

func (h *delayWorkflowNodeHandler) Type() string {
	return workflowNodeDelay
}

func (h *delayWorkflowNodeHandler) Execute(
	ctx context.Context,
	_ domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (CronNodeResult, error) {
	return CronNodeResult{}, executeWorkflowDelay(ctx, node.DelaySeconds)
}

type ifWorkflowNodeHandler struct {
	evaluateCondition func(raw string, job domain.CronJobSpec) (bool, error)
}

func (h *ifWorkflowNodeHandler) Type() string {
	return workflowNodeIf
}

func (h *ifWorkflowNodeHandler) Execute(
	_ context.Context,
	job domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (CronNodeResult, error) {
	if h == nil || h.evaluateCondition == nil {
		return CronNodeResult{}, errors.New("cron if node evaluator is unavailable")
	}
	matched, err := h.evaluateCondition(node.IfCondition, job)
	if err != nil {
		return CronNodeResult{}, err
	}
	if !matched {
		return CronNodeResult{Stop: true}, nil
	}
	return CronNodeResult{}, nil
}

func (s *Service) registerDefaultWorkflowNodeHandlers() {
	s.RegisterCronNodeHandler(&textWorkflowNodeHandler{executeTextTask: s.executeTextTask})
	s.RegisterCronNodeHandler(&delayWorkflowNodeHandler{})
	s.RegisterCronNodeHandler(&ifWorkflowNodeHandler{evaluateCondition: evaluateWorkflowIfCondition})
}

func (s *Service) RegisterCronNodeHandler(handler CronNodeHandler) {
	if s == nil || handler == nil {
		return
	}
	nodeType := normalizeWorkflowNodeType(handler.Type())
	if nodeType == "" || nodeType == workflowNodeStart {
		return
	}
	if s.nodeHandlers == nil {
		s.nodeHandlers = map[string]CronNodeHandler{}
	}
	s.nodeHandlers[nodeType] = handler
}

func (s *Service) resolveCronNodeHandler(nodeType string) (CronNodeHandler, bool) {
	if s == nil {
		return nil, false
	}
	handler, ok := s.nodeHandlers[normalizeWorkflowNodeType(nodeType)]
	return handler, ok
}

func (s *Service) supportsWorkflowNodeType(nodeType string) bool {
	if normalizeWorkflowNodeType(nodeType) == workflowNodeStart {
		return true
	}
	_, ok := s.resolveCronNodeHandler(nodeType)
	return ok
}

func (s *Service) buildWorkflowPlan(workflow *domain.CronWorkflowSpec) (*workflowPlan, error) {
	return buildWorkflowPlanWithNodeSupport(workflow, s.supportsWorkflowNodeType)
}

func normalizeWorkflowNodeType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

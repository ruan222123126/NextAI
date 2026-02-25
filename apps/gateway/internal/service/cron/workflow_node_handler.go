package cron

import (
	"strings"

	"nextai/apps/gateway/internal/domain"
	workflowengine "nextai/apps/gateway/internal/service/cron/workflow"
	"nextai/apps/gateway/internal/service/cron/workflow/nodes"
)

type CronNodeResult = workflowengine.NodeResult
type CronNodeHandler = workflowengine.NodeHandler

func (s *Service) registerDefaultWorkflowNodeHandlers() {
	s.RegisterCronNodeHandler(nodes.NewTextEventHandler(s.executeTextTask))
	s.RegisterCronNodeHandler(nodes.NewDelayNodeHandler(executeWorkflowDelay))
	s.RegisterCronNodeHandler(nodes.NewIfEventHandler(evaluateWorkflowIfCondition))
}

func (s *Service) RegisterCronNodeHandler(handler CronNodeHandler) {
	if s == nil || s.nodeRegistry == nil || handler == nil {
		return
	}
	s.nodeRegistry.Register(handler)
}

func (s *Service) resolveCronNodeHandler(nodeType string) (CronNodeHandler, bool) {
	if s == nil || s.nodeRegistry == nil {
		return nil, false
	}
	return s.nodeRegistry.Resolve(nodeType)
}

func (s *Service) supportsWorkflowNodeType(nodeType string) bool {
	if s == nil || s.nodeRegistry == nil {
		return false
	}
	return s.nodeRegistry.Supports(nodeType)
}

func (s *Service) buildWorkflowPlan(spec *domain.CronWorkflowSpec) (*workflowengine.Plan, error) {
	return workflowengine.BuildPlan(spec, s.supportsWorkflowNodeType, func(raw string) error {
		_, err := parseWorkflowIfCondition(raw)
		return err
	})
}

func normalizeWorkflowNodeType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

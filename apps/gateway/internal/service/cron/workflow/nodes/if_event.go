package nodes

import (
	"context"
	"errors"

	"nextai/apps/gateway/internal/domain"
	cronworkflow "nextai/apps/gateway/internal/service/cron/workflow"
)

type IfEventHandler struct {
	EvaluateCondition func(raw string, job domain.CronJobSpec) (bool, error)
}

func NewIfEventHandler(evaluateCondition func(raw string, job domain.CronJobSpec) (bool, error)) *IfEventHandler {
	return &IfEventHandler{EvaluateCondition: evaluateCondition}
}

func (h *IfEventHandler) Type() string {
	return cronworkflow.NodeTypeIfEvent
}

func (h *IfEventHandler) Execute(
	_ context.Context,
	job domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (cronworkflow.NodeResult, error) {
	if h == nil || h.EvaluateCondition == nil {
		return cronworkflow.NodeResult{}, errors.New("cron if node evaluator is unavailable")
	}
	matched, err := h.EvaluateCondition(node.IfCondition, job)
	if err != nil {
		return cronworkflow.NodeResult{}, err
	}
	if !matched {
		return cronworkflow.NodeResult{Stop: true}, nil
	}
	return cronworkflow.NodeResult{}, nil
}

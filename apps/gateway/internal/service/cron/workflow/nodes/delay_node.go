package nodes

import (
	"context"
	"errors"

	"nextai/apps/gateway/internal/domain"
	cronworkflow "nextai/apps/gateway/internal/service/cron/workflow"
)

type DelayNodeHandler struct {
	ExecuteDelay func(ctx context.Context, seconds int) error
}

func NewDelayNodeHandler(executeDelay func(ctx context.Context, seconds int) error) *DelayNodeHandler {
	return &DelayNodeHandler{ExecuteDelay: executeDelay}
}

func (h *DelayNodeHandler) Type() string {
	return cronworkflow.NodeTypeDelay
}

func (h *DelayNodeHandler) Execute(
	ctx context.Context,
	_ domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (cronworkflow.NodeResult, error) {
	if h == nil || h.ExecuteDelay == nil {
		return cronworkflow.NodeResult{}, errors.New("cron delay node executor is unavailable")
	}
	return cronworkflow.NodeResult{}, h.ExecuteDelay(ctx, node.DelaySeconds)
}

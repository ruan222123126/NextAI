package nodes

import (
	"context"
	"errors"
	"strings"

	"nextai/apps/gateway/internal/domain"
	cronworkflow "nextai/apps/gateway/internal/service/cron/workflow"
)

type TextEventHandler struct {
	ExecuteTextTask func(ctx context.Context, job domain.CronJobSpec, text string) error
}

func NewTextEventHandler(executeTextTask func(ctx context.Context, job domain.CronJobSpec, text string) error) *TextEventHandler {
	return &TextEventHandler{ExecuteTextTask: executeTextTask}
}

func (h *TextEventHandler) Type() string {
	return cronworkflow.NodeTypeTextEvent
}

func (h *TextEventHandler) Execute(
	ctx context.Context,
	job domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (cronworkflow.NodeResult, error) {
	if h == nil || h.ExecuteTextTask == nil {
		return cronworkflow.NodeResult{}, errors.New("cron text node executor is unavailable")
	}
	text := strings.TrimSpace(node.Text)
	if text == "" {
		return cronworkflow.NodeResult{}, errors.New("workflow text_event requires non-empty text")
	}
	return cronworkflow.NodeResult{}, h.ExecuteTextTask(ctx, job, text)
}

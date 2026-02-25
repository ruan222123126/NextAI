package workflow

import (
	"context"
	"errors"
	"fmt"

	"nextai/apps/gateway/internal/domain"
)

const (
	nodeStatusSucceeded = "succeeded"
	nodeStatusFailed    = "failed"
	nodeStatusSkipped   = "skipped"
)

type NodeRunner func(ctx context.Context, job domain.CronJobSpec, node domain.CronWorkflowNode) (NodeResult, error)

func Run(
	ctx context.Context,
	job domain.CronJobSpec,
	plan *Plan,
	runNode NodeRunner,
	nowISO func() string,
	newRunID func() string,
) (*domain.CronWorkflowExecution, error) {
	if plan == nil {
		return nil, errors.New("workflow plan is required")
	}
	if runNode == nil {
		return nil, errors.New("workflow node runner is unavailable")
	}
	if nowISO == nil {
		return nil, errors.New("workflow clock helper is unavailable")
	}
	if newRunID == nil {
		return nil, errors.New("workflow run id generator is unavailable")
	}

	startedAt := nowISO()
	execution := &domain.CronWorkflowExecution{
		RunID:       newRunID(),
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

		runResult, runErr := runNode(ctx, job, node)
		finishedAt := nowISO()
		step.FinishedAt = &finishedAt
		if runErr != nil {
			step.Status = nodeStatusFailed
			errText := runErr.Error()
			step.Error = &errText
			execution.HadFailures = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow node %s failed: %w", node.ID, runErr)
			}
		} else {
			step.Status = nodeStatusSucceeded
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
				Status:          nodeStatusSkipped,
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

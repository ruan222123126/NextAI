package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/service/adapters"
	cronservice "nextai/apps/gateway/internal/service/cron"
	"nextai/apps/gateway/internal/service/ports"
)

func (s *Server) getCronService() *cronservice.Service {
	if s.cronService == nil {
		s.cronService = s.newCronService()
	}
	return s.cronService
}

func (s *Server) newCronService() *cronservice.Service {
	agentProcessor := adapters.AgentProcessor{
		ProcessFunc: s.processAgentViaPort,
	}
	return cronservice.NewService(cronservice.Dependencies{
		Store:   s.stateStore,
		DataDir: s.cfg.DataDir,
		ChannelResolver: adapters.ChannelResolver{
			ResolveChannelFunc: func(name string) (ports.Channel, map[string]interface{}, string, error) {
				return s.resolveChannel(name)
			},
		},
		ExecuteConsoleAgentTask: func(ctx context.Context, job domain.CronJobSpec, text string) error {
			return s.executeCronConsoleAgentTask(ctx, agentProcessor, job, text)
		},
		ExecuteTask: func(ctx context.Context, job domain.CronJobSpec) (bool, error) {
			if s.cronTaskExecutor == nil {
				return false, nil
			}
			return true, s.cronTaskExecutor(ctx, job)
		},
	})
}

func (s *Server) executeCronConsoleAgentTask(
	ctx context.Context,
	agentProcessor ports.AgentProcessor,
	job domain.CronJobSpec,
	text string,
) error {
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
		BizParams: cronservice.BuildBizParams(job),
	}

	if agentProcessor == nil {
		return errors.New("cron console agent processor is unavailable")
	}
	if _, processErr := agentProcessor.Process(ctx, agentReq); processErr != nil {
		return fmt.Errorf(
			"cron console agent execution failed: status=%d code=%s message=%s",
			processErr.Status,
			strings.TrimSpace(processErr.Code),
			strings.TrimSpace(processErr.Message),
		)
	}

	return nil
}

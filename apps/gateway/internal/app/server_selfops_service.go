package app

import (
	"context"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/service/adapters"
	selfopsservice "nextai/apps/gateway/internal/service/selfops"
)

func (s *Server) getSelfOpsService() *selfopsservice.Service {
	if s.selfOpsService == nil {
		s.selfOpsService = s.newSelfOpsService()
	}
	return s.selfOpsService
}

func (s *Server) newSelfOpsService() *selfopsservice.Service {
	agentProcessor := adapters.AgentProcessor{
		ProcessFunc: s.processAgentViaPort,
	}
	return selfopsservice.NewService(selfopsservice.Dependencies{
		Store: s.stateStore,
		ProcessAgent: func(ctx context.Context, req domain.AgentProcessRequest) (domain.AgentProcessResponse, *selfopsservice.ProcessError) {
			out, processErr := agentProcessor.Process(ctx, req)
			if processErr != nil {
				return domain.AgentProcessResponse{}, &selfopsservice.ProcessError{
					Status:  processErr.Status,
					Code:    processErr.Code,
					Message: processErr.Message,
					Details: processErr.Details,
				}
			}
			return out, nil
		},
		GetWorkspaceFile: func(path string) (interface{}, error) {
			return s.getWorkspaceService().GetFile(path)
		},
		PutWorkspaceFile: func(path string, body []byte) error {
			return s.getWorkspaceService().PutFile(path, body)
		},
	})
}

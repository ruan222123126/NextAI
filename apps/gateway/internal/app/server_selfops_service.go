package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"nextai/apps/gateway/internal/domain"
	selfopsservice "nextai/apps/gateway/internal/service/selfops"
)

func (s *Server) getSelfOpsService() *selfopsservice.Service {
	if s.selfOpsService == nil {
		s.selfOpsService = s.newSelfOpsService()
	}
	return s.selfOpsService
}

func (s *Server) newSelfOpsService() *selfopsservice.Service {
	return selfopsservice.NewService(selfopsservice.Dependencies{
		Store: s.stateStore,
		ProcessAgent: func(ctx context.Context, req domain.AgentProcessRequest) (domain.AgentProcessResponse, *selfopsservice.ProcessError) {
			body, err := json.Marshal(req)
			if err != nil {
				return domain.AgentProcessResponse{}, &selfopsservice.ProcessError{
					Status:  http.StatusInternalServerError,
					Code:    "invalid_json",
					Message: "failed to build agent request",
				}
			}
			recorder := httptest.NewRecorder()
			httpReq := httptest.NewRequest(http.MethodPost, "/agent/process", bytes.NewReader(body)).WithContext(ctx)
			s.processAgentWithBody(recorder, httpReq, body)
			if recorder.Code != http.StatusOK {
				apiErr := domain.APIErrorBody{}
				if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &apiErr); decodeErr == nil && strings.TrimSpace(apiErr.Error.Code) != "" {
					return domain.AgentProcessResponse{}, &selfopsservice.ProcessError{
						Status:  recorder.Code,
						Code:    apiErr.Error.Code,
						Message: apiErr.Error.Message,
						Details: apiErr.Error.Details,
					}
				}
				return domain.AgentProcessResponse{}, &selfopsservice.ProcessError{
					Status:  recorder.Code,
					Code:    "agent_process_failed",
					Message: strings.TrimSpace(recorder.Body.String()),
				}
			}

			var out domain.AgentProcessResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
				return domain.AgentProcessResponse{}, &selfopsservice.ProcessError{
					Status:  http.StatusInternalServerError,
					Code:    "invalid_json",
					Message: "failed to decode agent response",
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

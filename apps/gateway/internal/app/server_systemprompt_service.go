package app

import systempromptservice "nextai/apps/gateway/internal/service/systemprompt"

func (s *Server) getSystemPromptService() *systempromptservice.Service {
	if s.systemPromptService == nil {
		s.systemPromptService = s.newSystemPromptService()
	}
	return s.systemPromptService
}

func (s *Server) newSystemPromptService() *systempromptservice.Service {
	return systempromptservice.NewService(systempromptservice.Dependencies{
		EnableEnvironmentContext: s != nil && s.cfg.EnablePromptContextIntrospect,
		LoadRequiredLayer:        loadRequiredSystemLayer,
	})
}

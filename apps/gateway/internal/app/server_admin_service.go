package app

import (
	adminservice "nextai/apps/gateway/internal/service/admin"
)

func (s *Server) getAdminService() *adminservice.Service {
	if s.adminService == nil {
		s.adminService = s.newAdminService()
	}
	return s.adminService
}

func (s *Server) newAdminService() *adminservice.Service {
	supportedChannels := map[string]struct{}{}
	for name := range s.channels {
		supportedChannels[name] = struct{}{}
	}
	return adminservice.NewService(adminservice.Dependencies{
		Store:             s.stateStore,
		DataDir:           s.cfg.DataDir,
		SupportedChannels: supportedChannels,
	})
}

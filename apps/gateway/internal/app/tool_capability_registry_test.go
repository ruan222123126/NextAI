package app

import (
	"testing"

	agentprotocolservice "nextai/apps/gateway/internal/service/agentprotocol"
)

func TestToolHasDeclaredCapabilityUsesRegistryDeclaration(t *testing.T) {
	t.Parallel()

	srv := &Server{
		toolCapabilities: map[string]toolCapabilitySet{
			"browser": newToolCapabilitySet(agentprotocolservice.ToolCapabilityOpenURL),
		},
	}

	if !srv.toolHasDeclaredCapability("browser", agentprotocolservice.ToolCapabilityOpenURL) {
		t.Fatalf("expected declared capability open_url")
	}
	if srv.toolHasDeclaredCapability("browser", agentprotocolservice.ToolCapabilityApproxClick) {
		t.Fatalf("did not expect undeclared capability approx_click")
	}
}

func TestToolHasDeclaredCapabilityFallsBackToLegacyNameMapping(t *testing.T) {
	t.Parallel()

	srv := &Server{}

	if !srv.toolHasDeclaredCapability("view", agentprotocolservice.ToolCapabilityOpenLocal) {
		t.Fatalf("expected legacy fallback capability open_local for view")
	}
	if !srv.toolHasDeclaredCapability("browser", agentprotocolservice.ToolCapabilityApproxClick) {
		t.Fatalf("expected legacy fallback capability approx_click for browser")
	}
	if !srv.toolHasDeclaredCapability("find", agentprotocolservice.ToolCapabilityFileSearch) {
		t.Fatalf("expected legacy fallback capability file_search for find")
	}
	if !srv.toolHasDeclaredCapability("search", agentprotocolservice.ToolCapabilityWebSearch) {
		t.Fatalf("expected legacy fallback capability web_search for search")
	}
}

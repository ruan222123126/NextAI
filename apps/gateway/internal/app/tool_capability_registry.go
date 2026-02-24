package app

import "strings"

type toolCapabilitySet map[string]struct{}

func newToolCapabilitySet(capabilities ...string) toolCapabilitySet {
	set := toolCapabilitySet{}
	for _, capability := range capabilities {
		normalized := normalizeToolCapability(capability)
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	return set
}

func normalizeToolCapability(capability string) string {
	return strings.ToLower(strings.TrimSpace(capability))
}

func (s *Server) toolHasDeclaredCapability(name string, capability string) bool {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	normalizedCapability := normalizeToolCapability(capability)
	if normalizedName == "" || normalizedCapability == "" {
		return false
	}
	if s == nil || len(s.toolCapabilities) == 0 {
		return legacyToolCapabilityByName(normalizedName, normalizedCapability)
	}
	capabilitySet, ok := s.toolCapabilities[normalizedName]
	if !ok || len(capabilitySet) == 0 {
		return legacyToolCapabilityByName(normalizedName, normalizedCapability)
	}
	_, exists := capabilitySet[normalizedCapability]
	return exists
}

func legacyToolCapabilityByName(name string, capability string) bool {
	switch name {
	case "shell":
		return capability == "execute"
	case "view":
		return capability == "read" || capability == "open_local"
	case "edit":
		return capability == "write"
	case "find":
		return capability == "read" || capability == "file_search"
	case "search":
		return capability == "network" || capability == "web_search"
	case "browser":
		return capability == "network" ||
			capability == "open_url" ||
			capability == "approx_click" ||
			capability == "approx_screenshot" ||
			capability == "web_fetch"
	default:
		return false
	}
}

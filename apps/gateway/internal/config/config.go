package config

import (
	"os"
	"strings"
)

type Config struct {
	Host                           string
	Port                           string
	DataDir                        string
	APIKey                         string
	WebDir                         string
	EnablePromptTemplates          bool
	EnablePromptContextIntrospect  bool
	EnableCodexModeV2              bool
	CodexPromptSource              string
	EnableCodexPromptShadowCompare bool
	ProviderRegistryFile           string
}

func Load() Config {
	host := os.Getenv("NEXTAI_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("NEXTAI_PORT")
	if port == "" {
		port = "8088"
	}
	dataDir := os.Getenv("NEXTAI_DATA_DIR")
	if dataDir == "" {
		dataDir = ".data"
	}
	apiKey := os.Getenv("NEXTAI_API_KEY")
	webDir := os.Getenv("NEXTAI_WEB_DIR")
	enablePromptTemplates := parseEnvBool("NEXTAI_ENABLE_PROMPT_TEMPLATES")
	enablePromptContextIntrospect := parseEnvBool("NEXTAI_ENABLE_PROMPT_CONTEXT_INTROSPECT")
	enableCodexModeV2 := parseEnvBool("NEXTAI_ENABLE_CODEX_MODE_V2")
	codexPromptSource := parseCodexPromptSource("NEXTAI_CODEX_PROMPT_SOURCE")
	enableCodexPromptShadowCompare := parseEnvBool("NEXTAI_CODEX_PROMPT_SHADOW_COMPARE")
	providerRegistryFile := strings.TrimSpace(os.Getenv("NEXTAI_PROVIDER_REGISTRY_FILE"))
	return Config{
		Host:                           host,
		Port:                           port,
		DataDir:                        dataDir,
		APIKey:                         apiKey,
		WebDir:                         webDir,
		EnablePromptTemplates:          enablePromptTemplates,
		EnablePromptContextIntrospect:  enablePromptContextIntrospect,
		EnableCodexModeV2:              enableCodexModeV2,
		CodexPromptSource:              codexPromptSource,
		EnableCodexPromptShadowCompare: enableCodexPromptShadowCompare,
		ProviderRegistryFile:           providerRegistryFile,
	}
}

func parseEnvBool(key string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "true")
}

func parseCodexPromptSource(key string) string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "catalog":
		return "catalog"
	case "file":
		fallthrough
	default:
		return "file"
	}
}

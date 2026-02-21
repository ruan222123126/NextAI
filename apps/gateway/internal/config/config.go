package config

import (
	"os"
	"strings"
)

type Config struct {
	Host                          string
	Port                          string
	DataDir                       string
	APIKey                        string
	WebDir                        string
	EnablePromptTemplates         bool
	EnablePromptContextIntrospect bool
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
	return Config{
		Host:                          host,
		Port:                          port,
		DataDir:                       dataDir,
		APIKey:                        apiKey,
		WebDir:                        webDir,
		EnablePromptTemplates:         enablePromptTemplates,
		EnablePromptContextIntrospect: enablePromptContextIntrospect,
	}
}

func parseEnvBool(key string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "true")
}

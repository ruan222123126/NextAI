package provider

import (
	"errors"
	"os"
	"strings"

	"nextai/apps/gateway/internal/repo"
)

func NormalizeProviderID(providerID string) string {
	return normalizeProviderID(providerID)
}

func NormalizeProviderSetting(setting *repo.ProviderSetting) {
	if setting == nil {
		return
	}
	setting.DisplayName = strings.TrimSpace(setting.DisplayName)
	setting.APIKey = strings.TrimSpace(setting.APIKey)
	setting.BaseURL = strings.TrimSpace(setting.BaseURL)
	setting.ReasoningEffort = strings.ToLower(strings.TrimSpace(setting.ReasoningEffort))
	if setting.Enabled == nil {
		enabled := true
		setting.Enabled = &enabled
	}
	if setting.Headers == nil {
		setting.Headers = map[string]string{}
	}
	if setting.ModelAliases == nil {
		setting.ModelAliases = map[string]string{}
	}
}

func ProviderEnabled(setting repo.ProviderSetting) bool {
	if setting.Enabled == nil {
		return true
	}
	return *setting.Enabled
}

func ProviderStoreEnabled(setting repo.ProviderSetting) bool {
	if setting.Store == nil {
		return false
	}
	return *setting.Store
}

func ResolveProviderDisplayName(setting repo.ProviderSetting, defaultName string) string {
	if displayName := strings.TrimSpace(setting.DisplayName); displayName != "" {
		return displayName
	}
	return strings.TrimSpace(defaultName)
}

func SanitizeStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func SanitizeModelAliases(raw *map[string]string) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	out := map[string]string{}
	for key, value := range *raw {
		alias := strings.TrimSpace(key)
		modelID := strings.TrimSpace(value)
		if alias == "" || modelID == "" {
			return nil, errors.New("model_aliases requires non-empty key and value")
		}
		out[alias] = modelID
	}
	return out, nil
}

func FindProviderSettingByID(
	providers map[string]repo.ProviderSetting,
	providerID string,
) (repo.ProviderSetting, bool) {
	if providers == nil {
		return repo.ProviderSetting{}, false
	}
	if setting, ok := providers[providerID]; ok {
		return setting, true
	}
	for key, setting := range providers {
		if NormalizeProviderID(key) == providerID {
			return setting, true
		}
	}
	return repo.ProviderSetting{}, false
}

func GetProviderSettingByID(providers map[string]repo.ProviderSetting, providerID string) repo.ProviderSetting {
	if setting, ok := FindProviderSettingByID(providers, providerID); ok {
		return setting
	}
	setting := repo.ProviderSetting{}
	NormalizeProviderSetting(&setting)
	return setting
}

func ResolveProviderAPIKey(providerID string, setting repo.ProviderSetting, envLookup func(string) string) string {
	if key := strings.TrimSpace(setting.APIKey); key != "" {
		return key
	}
	if envLookup == nil {
		envLookup = os.Getenv
	}
	return strings.TrimSpace(envLookup(EnvPrefix(providerID) + "_API_KEY"))
}

func ResolveProviderBaseURL(providerID string, setting repo.ProviderSetting, envLookup func(string) string) string {
	if baseURL := strings.TrimSpace(setting.BaseURL); baseURL != "" {
		return baseURL
	}
	if envLookup == nil {
		envLookup = os.Getenv
	}
	if envBaseURL := strings.TrimSpace(envLookup(EnvPrefix(providerID) + "_BASE_URL")); envBaseURL != "" {
		return envBaseURL
	}
	return ResolveProvider(providerID).DefaultBaseURL
}

func MaskKey(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-3:]
}

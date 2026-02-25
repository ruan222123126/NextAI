package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"nextai/apps/gateway/internal/domain"
)

const (
	AdapterDemo             = "demo"
	AdapterOpenAICompatible = "openai-compatible"
	AdapterCodexCompatible  = "codex-compatible"
)

type ModelSpec struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Status       string                   `json:"status,omitempty"`
	Capabilities domain.ModelCapabilities `json:"capabilities,omitempty"`
	Limit        domain.ModelLimit        `json:"limit,omitempty"`
}

type ProviderSpec struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	APIKeyPrefix       string      `json:"api_key_prefix,omitempty"`
	AllowCustomBaseURL bool        `json:"allow_custom_base_url"`
	DefaultBaseURL     string      `json:"default_base_url,omitempty"`
	Adapter            string      `json:"adapter,omitempty"`
	Models             []ModelSpec `json:"models,omitempty"`
}

type ProviderTypeSpec struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

var defaultBuiltinProviders = map[string]ProviderSpec{
	"openai": {
		ID:                 "openai",
		Name:               "OPENAI",
		APIKeyPrefix:       "OPENAI_API_KEY",
		AllowCustomBaseURL: true,
		DefaultBaseURL:     "https://api.openai.com/v1",
		Adapter:            AdapterOpenAICompatible,
		Models: []ModelSpec{
			{
				ID:     "gpt-4o-mini",
				Name:   "GPT-4o Mini",
				Status: "active",
				Capabilities: domain.ModelCapabilities{
					Temperature: true,
					Reasoning:   true,
					Attachment:  true,
					ToolCall:    true,
					Input:       &domain.ModelModalities{Text: true, Image: true, Audio: true},
					Output:      &domain.ModelModalities{Text: true, Audio: true},
				},
				Limit: domain.ModelLimit{Context: 128000, Output: 16384},
			},
			{
				ID:     "gpt-4.1-mini",
				Name:   "GPT-4.1 Mini",
				Status: "active",
				Capabilities: domain.ModelCapabilities{
					Temperature: true,
					Reasoning:   true,
					Attachment:  true,
					ToolCall:    true,
					Input:       &domain.ModelModalities{Text: true, Image: true},
					Output:      &domain.ModelModalities{Text: true},
				},
				Limit: domain.ModelLimit{Context: 128000, Output: 16384},
			},
		},
	},
}

var defaultProviderTypes = []ProviderTypeSpec{
	{
		ID:          "openai",
		DisplayName: "openai",
	},
	{
		ID:          AdapterOpenAICompatible,
		DisplayName: "openai Compatible",
	},
	{
		ID:          AdapterCodexCompatible,
		DisplayName: "codex Compatible",
	},
}

var (
	registryMu         sync.RWMutex
	providerRegistry   map[string]ProviderSpec
	builtinProviderIDs map[string]struct{}
	providerTypes      []ProviderTypeSpec
)

type RegistryConfig struct {
	Providers     []ProviderSpec     `json:"providers"`
	ProviderTypes []ProviderTypeSpec `json:"provider_types"`
}

func init() {
	ResetRegistry()
}

func ResetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()

	providerRegistry = map[string]ProviderSpec{}
	builtinProviderIDs = map[string]struct{}{}
	for rawID, rawSpec := range defaultBuiltinProviders {
		spec, err := normalizeProviderSpec(rawSpec)
		if err != nil {
			continue
		}
		id := normalizeProviderID(rawID)
		if id != "" {
			spec.ID = id
		}
		providerRegistry[spec.ID] = spec
		builtinProviderIDs[spec.ID] = struct{}{}
	}

	providerTypes = make([]ProviderTypeSpec, 0, len(defaultProviderTypes))
	for _, item := range defaultProviderTypes {
		normalized := normalizeProviderTypeSpec(item)
		if normalized.ID == "" {
			continue
		}
		providerTypes = append(providerTypes, normalized)
	}
}

func RegisterProvider(spec ProviderSpec) error {
	normalized, err := normalizeProviderSpec(spec)
	if err != nil {
		return err
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	providerRegistry[normalized.ID] = normalized
	return nil
}

func RegisterProviderType(spec ProviderTypeSpec) error {
	normalized := normalizeProviderTypeSpec(spec)
	if normalized.ID == "" {
		return errors.New("provider type id is required")
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	for i := range providerTypes {
		if providerTypes[i].ID == normalized.ID {
			providerTypes[i] = normalized
			return nil
		}
	}
	providerTypes = append(providerTypes, normalized)
	sort.Slice(providerTypes, func(i, j int) bool {
		return providerTypes[i].ID < providerTypes[j].ID
	})
	return nil
}

func LoadRegistryFromFile(path string) error {
	target := strings.TrimSpace(path)
	if target == "" {
		return nil
	}

	b, err := os.ReadFile(target)
	if err != nil {
		return err
	}

	cfg := RegistryConfig{}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("decode provider registry: %w", err)
	}

	for _, item := range cfg.ProviderTypes {
		if err := RegisterProviderType(item); err != nil {
			return err
		}
	}
	for _, item := range cfg.Providers {
		if err := RegisterProvider(item); err != nil {
			return err
		}
	}
	return nil
}

func ListBuiltinProviderIDs() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(builtinProviderIDs))
	for id := range builtinProviderIDs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func ListProviderTypes() []ProviderTypeSpec {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]ProviderTypeSpec, 0, len(providerTypes))
	for _, item := range providerTypes {
		out = append(out, item)
	}
	return out
}

func ResolveProvider(providerID string) ProviderSpec {
	id := normalizeProviderID(providerID)
	registryMu.RLock()
	spec, ok := providerRegistry[id]
	registryMu.RUnlock()
	if ok {
		return cloneProviderSpec(spec)
	}
	return ProviderSpec{
		ID:                 id,
		Name:               strings.ToUpper(id),
		APIKeyPrefix:       EnvPrefix(id) + "_API_KEY",
		AllowCustomBaseURL: true,
		DefaultBaseURL:     "",
		Adapter:            resolveCustomAdapter(id),
		Models:             []ModelSpec{},
	}
}

func IsBuiltinProviderID(providerID string) bool {
	id := strings.ToLower(strings.TrimSpace(providerID))
	if id == "" {
		return false
	}
	registryMu.RLock()
	_, ok := builtinProviderIDs[id]
	registryMu.RUnlock()
	return ok
}

func ResolveAdapter(providerID string) string {
	return ResolveProvider(providerID).Adapter
}

func IsCodexCompatibleProviderID(providerID string) bool {
	id := strings.ToLower(strings.TrimSpace(providerID))
	if id == "" {
		return false
	}
	if id == AdapterCodexCompatible {
		return true
	}
	return strings.HasPrefix(id, AdapterCodexCompatible+"-")
}

func ResolveModels(providerID string, aliases map[string]string) []domain.ModelInfo {
	spec := ResolveProvider(providerID)
	out := make([]domain.ModelInfo, 0, len(spec.Models)+len(aliases))
	seen := map[string]struct{}{}
	modelByID := map[string]domain.ModelInfo{}

	for _, model := range spec.Models {
		item := domain.ModelInfo{
			ID:           model.ID,
			Name:         model.Name,
			Status:       model.Status,
			Capabilities: cloneCapabilities(model.Capabilities),
			Limit:        cloneLimit(model.Limit),
		}
		out = append(out, item)
		modelByID[model.ID] = item
		seen[model.ID] = struct{}{}
	}

	keys := sortedAliasKeys(aliases)
	for _, alias := range keys {
		target := strings.TrimSpace(aliases[alias])
		if target == "" {
			continue
		}
		if _, exists := seen[alias]; exists {
			continue
		}

		base, ok := modelByID[target]
		if !ok {
			if len(spec.Models) > 0 {
				continue
			}
			item := domain.ModelInfo{
				ID:   alias,
				Name: alias,
			}
			if alias != target {
				item.AliasOf = target
			}
			out = append(out, item)
			seen[alias] = struct{}{}
			continue
		}
		base.ID = alias
		base.Name = alias
		base.AliasOf = target
		out = append(out, base)
		seen[alias] = struct{}{}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].AliasOf == "" && out[j].AliasOf != "" {
			return true
		}
		if out[i].AliasOf != "" && out[j].AliasOf == "" {
			return false
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func ResolveModelID(providerID, requestedModelID string, aliases map[string]string) (string, bool) {
	modelID := strings.TrimSpace(requestedModelID)
	if modelID == "" {
		return "", false
	}
	spec := ResolveProvider(providerID)
	modelSet := map[string]struct{}{}
	for _, model := range spec.Models {
		modelSet[model.ID] = struct{}{}
	}
	if _, ok := modelSet[modelID]; ok {
		return modelID, true
	}
	if target, ok := aliases[modelID]; ok {
		target = strings.TrimSpace(target)
		if target != "" {
			if _, exists := modelSet[target]; exists {
				return target, true
			}
			if len(spec.Models) == 0 {
				return target, true
			}
		}
	}
	// 允许自定义 provider 使用任意模型 ID（openai-compatible 场景）
	if len(spec.Models) == 0 {
		return modelID, true
	}
	return "", false
}

func DefaultModelID(providerID string) string {
	spec := ResolveProvider(providerID)
	if len(spec.Models) == 0 {
		return ""
	}
	return spec.Models[0].ID
}

func EnvPrefix(providerID string) string {
	prefix := strings.ToUpper(strings.TrimSpace(providerID))
	if prefix == "" {
		return "PROVIDER"
	}
	replacer := strings.NewReplacer("-", "_", ".", "_", " ", "_")
	return replacer.Replace(prefix)
}

func normalizeProviderSpec(spec ProviderSpec) (ProviderSpec, error) {
	out := cloneProviderSpec(spec)
	out.ID = normalizeProviderID(out.ID)
	if out.ID == "" {
		return ProviderSpec{}, errors.New("provider id is required")
	}

	out.Name = strings.TrimSpace(out.Name)
	if out.Name == "" {
		out.Name = strings.ToUpper(out.ID)
	}
	out.APIKeyPrefix = strings.TrimSpace(out.APIKeyPrefix)
	if out.APIKeyPrefix == "" {
		out.APIKeyPrefix = EnvPrefix(out.ID) + "_API_KEY"
	}
	out.DefaultBaseURL = strings.TrimSpace(out.DefaultBaseURL)
	out.Adapter = strings.TrimSpace(out.Adapter)
	if out.Adapter == "" {
		out.Adapter = resolveCustomAdapter(out.ID)
	}

	normalizedModels := make([]ModelSpec, 0, len(out.Models))
	seen := map[string]struct{}{}
	for _, model := range out.Models {
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = modelID
		}
		normalizedModels = append(normalizedModels, ModelSpec{
			ID:           modelID,
			Name:         name,
			Status:       strings.TrimSpace(model.Status),
			Capabilities: model.Capabilities,
			Limit:        model.Limit,
		})
	}
	out.Models = normalizedModels
	return out, nil
}

func normalizeProviderTypeSpec(spec ProviderTypeSpec) ProviderTypeSpec {
	id := strings.TrimSpace(spec.ID)
	displayName := strings.TrimSpace(spec.DisplayName)
	if displayName == "" {
		displayName = id
	}
	return ProviderTypeSpec{
		ID:          id,
		DisplayName: displayName,
	}
}

func sortedAliasKeys(aliases map[string]string) []string {
	if len(aliases) == 0 {
		return nil
	}
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func resolveCustomAdapter(providerID string) string {
	if IsCodexCompatibleProviderID(providerID) {
		return AdapterCodexCompatible
	}
	return AdapterOpenAICompatible
}

func normalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func cloneProviderSpec(in ProviderSpec) ProviderSpec {
	out := in
	out.Models = make([]ModelSpec, 0, len(in.Models))
	for _, model := range in.Models {
		out.Models = append(out.Models, ModelSpec{
			ID:           model.ID,
			Name:         model.Name,
			Status:       model.Status,
			Capabilities: model.Capabilities,
			Limit:        model.Limit,
		})
	}
	return out
}

func cloneCapabilities(in domain.ModelCapabilities) *domain.ModelCapabilities {
	out := in
	if in.Input != nil {
		input := *in.Input
		out.Input = &input
	}
	if in.Output != nil {
		output := *in.Output
		out.Output = &output
	}
	return &out
}

func cloneLimit(in domain.ModelLimit) *domain.ModelLimit {
	out := in
	return &out
}

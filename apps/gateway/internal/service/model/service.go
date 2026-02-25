package model

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/ports"
)

var ErrProviderNotFound = errors.New("provider_not_found")
var ErrProviderDisabled = errors.New("provider_disabled")
var ErrModelNotFound = errors.New("model_not_found")

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type Dependencies struct {
	Store     ports.StateStore
	EnvLookup func(string) string
}

type Service struct {
	deps Dependencies
}

type ConfigureProviderInput struct {
	ProviderID      string
	APIKey          *string
	BaseURL         *string
	DisplayName     *string
	ReasoningEffort *string
	Enabled         *bool
	Store           *bool
	Headers         *map[string]string
	TimeoutMS       *int
	ModelAliases    *map[string]string
}

func NewService(deps Dependencies) *Service {
	if deps.EnvLookup == nil {
		deps.EnvLookup = os.Getenv
	}
	return &Service{deps: deps}
}

func (s *Service) ListProviders() ([]domain.ProviderInfo, error) {
	providers, _, _, err := s.collectProviderCatalog()
	if err != nil {
		return nil, err
	}
	return providers, nil
}

func (s *Service) GetCatalog() (domain.ModelCatalogInfo, error) {
	providers, defaults, active, err := s.collectProviderCatalog()
	if err != nil {
		return domain.ModelCatalogInfo{}, err
	}
	providerTypes := provider.ListProviderTypes()
	typeOut := make([]domain.ProviderTypeInfo, 0, len(providerTypes))
	for _, item := range providerTypes {
		typeOut = append(typeOut, domain.ProviderTypeInfo{
			ID:          item.ID,
			DisplayName: item.DisplayName,
		})
	}
	return domain.ModelCatalogInfo{
		Providers:     providers,
		Defaults:      defaults,
		ActiveLLM:     active,
		ProviderTypes: typeOut,
	}, nil
}

func (s *Service) ConfigureProvider(input ConfigureProviderInput) (domain.ProviderInfo, error) {
	if err := s.validateStore(); err != nil {
		return domain.ProviderInfo{}, err
	}

	providerID := provider.NormalizeProviderID(input.ProviderID)
	if providerID == "" {
		return domain.ProviderInfo{}, &ValidationError{
			Code:    "invalid_provider_id",
			Message: "provider_id is required",
		}
	}
	if input.TimeoutMS != nil && *input.TimeoutMS < 0 {
		return domain.ProviderInfo{}, &ValidationError{
			Code:    "invalid_provider_config",
			Message: "timeout_ms must be >= 0",
		}
	}
	sanitizedReasoningEffort, reasoningErr := sanitizeReasoningEffort(providerID, input.ReasoningEffort)
	if reasoningErr != nil {
		return domain.ProviderInfo{}, &ValidationError{
			Code:    "invalid_provider_config",
			Message: reasoningErr.Error(),
		}
	}

	sanitizedAliases, aliasErr := provider.SanitizeModelAliases(input.ModelAliases)
	if aliasErr != nil {
		return domain.ProviderInfo{}, &ValidationError{
			Code:    "invalid_provider_config",
			Message: aliasErr.Error(),
		}
	}

	var out domain.ProviderInfo
	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		setting := provider.GetProviderSettingByID(st.Providers, providerID)
		provider.NormalizeProviderSetting(&setting)
		if input.APIKey != nil {
			setting.APIKey = strings.TrimSpace(*input.APIKey)
		}
		if input.BaseURL != nil {
			setting.BaseURL = strings.TrimSpace(*input.BaseURL)
		}
		if input.DisplayName != nil {
			setting.DisplayName = strings.TrimSpace(*input.DisplayName)
		}
		if input.ReasoningEffort != nil {
			setting.ReasoningEffort = sanitizedReasoningEffort
		}
		if input.Enabled != nil {
			enabled := *input.Enabled
			setting.Enabled = &enabled
		}
		if input.Store != nil {
			store := *input.Store
			setting.Store = &store
		}
		if input.Headers != nil {
			setting.Headers = provider.SanitizeStringMap(*input.Headers)
		}
		if input.TimeoutMS != nil {
			setting.TimeoutMS = *input.TimeoutMS
		}
		if input.ModelAliases != nil {
			setting.ModelAliases = sanitizedAliases
		}
		st.Providers[providerID] = setting
		out = s.buildProviderInfo(providerID, setting)
		return nil
	}); err != nil {
		return domain.ProviderInfo{}, err
	}
	return out, nil
}

func (s *Service) DeleteProvider(providerID string) (bool, error) {
	if err := s.validateStore(); err != nil {
		return false, err
	}

	providerID = provider.NormalizeProviderID(providerID)
	if providerID == "" {
		return false, &ValidationError{
			Code:    "invalid_provider_id",
			Message: "provider_id is required",
		}
	}

	deleted := false
	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		for key := range st.Providers {
			if provider.NormalizeProviderID(key) == providerID {
				delete(st.Providers, key)
				deleted = true
			}
		}
		if deleted && provider.NormalizeProviderID(st.ActiveLLM.ProviderID) == providerID {
			st.ActiveLLM = domain.ModelSlotConfig{}
		}
		return nil
	}); err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *Service) GetActiveModels() (domain.ActiveModelsInfo, error) {
	if err := s.validateStore(); err != nil {
		return domain.ActiveModelsInfo{}, err
	}

	out := domain.ActiveModelsInfo{}
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		out = domain.ActiveModelsInfo{ActiveLLM: st.ActiveLLM}
	})
	return out, nil
}

func (s *Service) SetActiveModels(body domain.ModelSlotConfig) (domain.ActiveModelsInfo, error) {
	if err := s.validateStore(); err != nil {
		return domain.ActiveModelsInfo{}, err
	}

	body.ProviderID = provider.NormalizeProviderID(body.ProviderID)
	body.Model = strings.TrimSpace(body.Model)
	if body.ProviderID == "" || body.Model == "" {
		return domain.ActiveModelsInfo{}, &ValidationError{
			Code:    "invalid_model_slot",
			Message: "provider_id and model are required",
		}
	}

	var out domain.ModelSlotConfig
	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		setting, ok := provider.FindProviderSettingByID(st.Providers, body.ProviderID)
		if !ok {
			return ErrProviderNotFound
		}
		provider.NormalizeProviderSetting(&setting)
		if !provider.ProviderEnabled(setting) {
			return ErrProviderDisabled
		}
		resolvedModel, ok := provider.ResolveModelID(body.ProviderID, body.Model, setting.ModelAliases)
		if !ok {
			return ErrModelNotFound
		}
		out = domain.ModelSlotConfig{
			ProviderID: body.ProviderID,
			Model:      resolvedModel,
		}
		st.ActiveLLM = out
		return nil
	}); err != nil {
		return domain.ActiveModelsInfo{}, err
	}
	return domain.ActiveModelsInfo{ActiveLLM: out}, nil
}

func (s *Service) collectProviderCatalog() ([]domain.ProviderInfo, map[string]string, domain.ModelSlotConfig, error) {
	if err := s.validateStore(); err != nil {
		return nil, nil, domain.ModelSlotConfig{}, err
	}

	out := make([]domain.ProviderInfo, 0)
	defaults := map[string]string{}
	active := domain.ModelSlotConfig{}

	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		active = st.ActiveLLM
		settingsByID := map[string]repo.ProviderSetting{}

		for rawID, setting := range st.Providers {
			id := provider.NormalizeProviderID(rawID)
			if id == "" {
				continue
			}
			provider.NormalizeProviderSetting(&setting)
			settingsByID[id] = setting
		}

		ids := make([]string, 0, len(settingsByID))
		for id := range settingsByID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			setting := settingsByID[id]
			out = append(out, s.buildProviderInfo(id, setting))
			defaults[id] = provider.DefaultModelID(id)
		}
	})
	return out, defaults, active, nil
}

func (s *Service) buildProviderInfo(providerID string, setting repo.ProviderSetting) domain.ProviderInfo {
	provider.NormalizeProviderSetting(&setting)
	spec := provider.ResolveProvider(providerID)
	apiKey := provider.ResolveProviderAPIKey(providerID, setting, s.deps.EnvLookup)
	return domain.ProviderInfo{
		ID:                 providerID,
		Name:               spec.Name,
		DisplayName:        provider.ResolveProviderDisplayName(setting, spec.Name),
		OpenAICompatible:   provider.ResolveAdapter(providerID) == provider.AdapterOpenAICompatible,
		APIKeyPrefix:       spec.APIKeyPrefix,
		Models:             provider.ResolveModels(providerID, setting.ModelAliases),
		ReasoningEffort:    setting.ReasoningEffort,
		Store:              provider.ProviderStoreEnabled(setting),
		Headers:            provider.SanitizeStringMap(setting.Headers),
		TimeoutMS:          setting.TimeoutMS,
		ModelAliases:       provider.SanitizeStringMap(setting.ModelAliases),
		AllowCustomBaseURL: spec.AllowCustomBaseURL,
		Enabled:            provider.ProviderEnabled(setting),
		HasAPIKey:          strings.TrimSpace(apiKey) != "",
		CurrentAPIKey:      provider.MaskKey(apiKey),
		CurrentBaseURL:     provider.ResolveProviderBaseURL(providerID, setting, s.deps.EnvLookup),
	}
}

func (s *Service) validateStore() error {
	if s == nil || s.deps.Store == nil {
		return errors.New("model state store is required")
	}
	return nil
}

var allowedReasoningEfforts = map[string]struct{}{
	"minimal": {},
	"low":     {},
	"medium":  {},
	"high":    {},
}

func sanitizeReasoningEffort(providerID string, raw *string) (string, error) {
	if raw == nil {
		return "", nil
	}
	effort := normalizeReasoningEffort(*raw)
	if effort == "" {
		return "", nil
	}
	if !providerSupportsReasoningEffort(providerID) {
		return "", errors.New("reasoning_effort is only supported for openai-compatible and codex-compatible providers")
	}
	if _, ok := allowedReasoningEfforts[effort]; !ok {
		return "", errors.New("reasoning_effort must be one of: minimal, low, medium, high")
	}
	return effort, nil
}

func providerSupportsReasoningEffort(providerID string) bool {
	adapter := provider.ResolveAdapter(providerID)
	return adapter == provider.AdapterOpenAICompatible || adapter == provider.AdapterCodexCompatible
}

func normalizeReasoningEffort(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func (e *ValidationError) String() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Code) == "" {
		return e.Message
	}
	if strings.TrimSpace(e.Message) == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

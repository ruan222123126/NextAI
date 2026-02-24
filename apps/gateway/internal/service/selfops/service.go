package selfops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/ports"
)

const (
	defaultChannel      = "console"
	defaultMutationTTL  = 5 * time.Minute
	demoProviderID      = "demo"
	demoModelID         = "demo-chat"
	mutationIDPrefix    = "mutation"
	mutationAuditPrefix = "audit"
)

const (
	TargetWorkspaceFile  = "workspace_file"
	TargetProviderConfig = "provider_config"
	TargetActiveLLM      = "active_llm"
)

const (
	OperationReplace     = "replace"
	OperationJSONPatch   = "json_patch"
	OperationTextRewrite = "text_rewrite"
)

type ProcessError struct {
	Status  int
	Code    string
	Message string
	Details interface{}
}

func (e *ProcessError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return fmt.Sprintf("%d %s", e.Status, e.Code)
}

type ProcessAgentFunc func(ctx context.Context, req domain.AgentProcessRequest) (domain.AgentProcessResponse, *ProcessError)

type Dependencies struct {
	Store            ports.StateStore
	ProcessAgent     ProcessAgentFunc
	GetWorkspaceFile func(path string) (interface{}, error)
	PutWorkspaceFile func(path string, body []byte) error
	Now              func() time.Time
	MutationTTL      time.Duration
}

type Service struct {
	deps Dependencies

	mutationMu sync.Mutex
	mutations  map[string]mutationRecord
}

type ServiceError struct {
	Code    string
	Message string
	Details interface{}
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type BootstrapSessionInput struct {
	UserID      string `json:"user_id"`
	Channel     string `json:"channel"`
	SessionSeed string `json:"session_seed"`
	FirstInput  string `json:"first_input"`
	PromptMode  string `json:"prompt_mode"`
	Stream      bool   `json:"stream"`
}

type BootstrapSessionOutput struct {
	Chat         domain.ChatSpec        `json:"chat"`
	Reply        string                 `json:"reply"`
	Events       []domain.AgentEvent    `json:"events,omitempty"`
	AppliedModel domain.ModelSlotConfig `json:"applied_model"`
}

type SetSessionModelInput struct {
	SessionID  string `json:"session_id"`
	UserID     string `json:"user_id"`
	Channel    string `json:"channel"`
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

type SetSessionModelOutput struct {
	SessionID string                       `json:"session_id"`
	ChatID    string                       `json:"chat_id"`
	Override  domain.ChatActiveLLMOverride `json:"active_llm_override"`
}

type JSONPatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

type MutationOperation struct {
	Kind    string               `json:"kind"`
	Path    string               `json:"path,omitempty"`
	Value   interface{}          `json:"value,omitempty"`
	Search  string               `json:"search,omitempty"`
	Replace string               `json:"replace,omitempty"`
	Patch   []JSONPatchOperation `json:"patch,omitempty"`
}

type PreviewMutationInput struct {
	Target         string              `json:"target"`
	Operations     []MutationOperation `json:"operations"`
	AllowSensitive bool                `json:"allow_sensitive"`
}

type MutationChecks struct {
	PathWhitelistPassed bool     `json:"path_whitelist_passed"`
	StructureValid      bool     `json:"structure_valid"`
	RiskLevel           string   `json:"risk_level"`
	SensitiveFields     []string `json:"sensitive_fields,omitempty"`
	DeniedPaths         []string `json:"denied_paths,omitempty"`
}

type MutationDiffSummary struct {
	Target     string `json:"target"`
	Path       string `json:"path"`
	Changed    bool   `json:"changed"`
	BeforeHash string `json:"before_hash"`
	AfterHash  string `json:"after_hash"`
}

type PreviewMutationOutput struct {
	MutationID             string                `json:"mutation_id"`
	ExpiresAt              string                `json:"expires_at"`
	ConfirmHash            string                `json:"confirm_hash"`
	Checks                 MutationChecks        `json:"checks"`
	DiffSummary            []MutationDiffSummary `json:"diff_summary"`
	UnifiedDiff            string                `json:"unified_diff"`
	RequiresSensitiveAllow bool                  `json:"requires_sensitive_allow"`
}

type ApplyMutationInput struct {
	MutationID     string `json:"mutation_id"`
	ConfirmHash    string `json:"confirm_hash"`
	AllowSensitive bool   `json:"allow_sensitive"`
}

type ApplyMutationOutput struct {
	Applied        bool     `json:"applied"`
	AppliedTargets []string `json:"applied_targets"`
	AuditID        string   `json:"audit_id"`
}

func NewService(deps Dependencies) *Service {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.MutationTTL <= 0 {
		deps.MutationTTL = defaultMutationTTL
	}
	return &Service{
		deps:      deps,
		mutations: map[string]mutationRecord{},
	}
}

func (s *Service) BootstrapSession(ctx context.Context, input BootstrapSessionInput) (BootstrapSessionOutput, *ProcessError, error) {
	if err := s.validateStore(); err != nil {
		return BootstrapSessionOutput{}, nil, err
	}
	if s.deps.ProcessAgent == nil {
		return BootstrapSessionOutput{}, nil, errors.New("selfops process dependency is required")
	}

	userID := strings.TrimSpace(input.UserID)
	firstInput := strings.TrimSpace(input.FirstInput)
	if userID == "" || firstInput == "" {
		return BootstrapSessionOutput{}, nil, &ServiceError{
			Code:    "invalid_request",
			Message: "user_id and first_input are required",
		}
	}

	channel := normalizeChannel(input.Channel)
	sessionID := strings.TrimSpace(input.SessionSeed)
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", s.deps.Now().UnixNano())
	}

	bizParams := map[string]interface{}{}
	if promptMode := strings.TrimSpace(input.PromptMode); promptMode != "" {
		bizParams["prompt_mode"] = promptMode
	}

	req := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{Type: "text", Text: firstInput},
				},
			},
		},
		SessionID: sessionID,
		UserID:    userID,
		Channel:   channel,
		Stream:    false,
		BizParams: bizParams,
	}
	processResp, processErr := s.deps.ProcessAgent(ctx, req)
	if processErr != nil {
		return BootstrapSessionOutput{}, processErr, nil
	}

	chat, appliedModel, err := s.readChatAndAppliedModel(sessionID, userID, channel)
	if err != nil {
		return BootstrapSessionOutput{}, nil, err
	}

	return BootstrapSessionOutput{
		Chat:         chat,
		Reply:        processResp.Reply,
		Events:       processResp.Events,
		AppliedModel: appliedModel,
	}, nil, nil
}

func (s *Service) SetSessionModel(input SetSessionModelInput) (SetSessionModelOutput, error) {
	if err := s.validateStore(); err != nil {
		return SetSessionModelOutput{}, err
	}

	sessionID := strings.TrimSpace(input.SessionID)
	userID := strings.TrimSpace(input.UserID)
	channel := normalizeChannel(input.Channel)
	providerID := normalizeProviderID(input.ProviderID)
	modelID := strings.TrimSpace(input.Model)
	if sessionID == "" || userID == "" || providerID == "" || modelID == "" {
		return SetSessionModelOutput{}, &ServiceError{
			Code:    "session_model_invalid",
			Message: "session_id, user_id, provider_id and model are required",
		}
	}

	out := SetSessionModelOutput{}
	now := nowISO(s.deps.Now())
	err := s.deps.Store.Write(func(st *repo.State) error {
		chatID, chat, found := findChatBySession(st, sessionID, userID, channel)
		if !found {
			return &ServiceError{
				Code:    "session_not_found",
				Message: "session not found",
				Details: map[string]string{
					"session_id": sessionID,
					"user_id":    userID,
					"channel":    channel,
				},
			}
		}

		resolved, validationErr := resolveAndValidateModel(st, providerID, modelID)
		if validationErr != nil {
			return validationErr
		}

		if chat.Meta == nil {
			chat.Meta = map[string]interface{}{}
		}
		override := domain.ChatActiveLLMOverride{
			ProviderID: resolved.ProviderID,
			Model:      resolved.Model,
			UpdatedAt:  now,
		}
		chat.Meta[domain.ChatMetaActiveLLM] = map[string]interface{}{
			"provider_id": override.ProviderID,
			"model":       override.Model,
			"updated_at":  override.UpdatedAt,
		}
		chat.UpdatedAt = now
		st.Chats[chatID] = chat

		out = SetSessionModelOutput{
			SessionID: sessionID,
			ChatID:    chatID,
			Override:  override,
		}
		return nil
	})
	if err != nil {
		return SetSessionModelOutput{}, err
	}
	return out, nil
}

func (s *Service) ResolveSessionModel(sessionID, userID, channel string) (domain.ModelSlotConfig, error) {
	if err := s.validateStore(); err != nil {
		return domain.ModelSlotConfig{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	userID = strings.TrimSpace(userID)
	channel = normalizeChannel(channel)

	if sessionID == "" || userID == "" {
		return domain.ModelSlotConfig{}, &ServiceError{
			Code:    "invalid_request",
			Message: "session_id and user_id are required",
		}
	}

	slot := domain.ModelSlotConfig{
		ProviderID: demoProviderID,
		Model:      demoModelID,
	}

	s.deps.Store.Read(func(st *repo.State) {
		_, chat, found := findChatBySession(st, sessionID, userID, channel)
		if found {
			if override, ok := parseChatActiveLLMOverride(chat.Meta); ok {
				if resolved, err := resolveAndValidateModel(st, override.ProviderID, override.Model); err == nil {
					slot = resolved
					return
				}
			}
		}
		if resolved, err := resolveAndValidateModel(st, st.ActiveLLM.ProviderID, st.ActiveLLM.Model); err == nil {
			slot = resolved
		}
	})
	return slot, nil
}

func (s *Service) readChatAndAppliedModel(sessionID, userID, channel string) (domain.ChatSpec, domain.ModelSlotConfig, error) {
	var (
		chat         domain.ChatSpec
		appliedModel = domain.ModelSlotConfig{ProviderID: demoProviderID, Model: demoModelID}
		found        bool
	)

	s.deps.Store.Read(func(st *repo.State) {
		_, matchedChat, ok := findChatBySession(st, sessionID, userID, channel)
		if !ok {
			return
		}
		found = true
		chat = matchedChat

		if override, ok := parseChatActiveLLMOverride(chat.Meta); ok {
			if resolved, err := resolveAndValidateModel(st, override.ProviderID, override.Model); err == nil {
				appliedModel = resolved
				return
			}
		}
		if resolved, err := resolveAndValidateModel(st, st.ActiveLLM.ProviderID, st.ActiveLLM.Model); err == nil {
			appliedModel = resolved
			return
		}
	})

	if !found {
		return domain.ChatSpec{}, domain.ModelSlotConfig{}, &ServiceError{
			Code:    "session_not_found",
			Message: "session not found after process",
			Details: map[string]string{
				"session_id": sessionID,
				"user_id":    userID,
				"channel":    channel,
			},
		}
	}
	return chat, appliedModel, nil
}

func (s *Service) validateStore() error {
	if s == nil || s.deps.Store == nil {
		return errors.New("selfops store dependency is required")
	}
	return nil
}

func nowISO(now time.Time) string {
	return now.UTC().Format(time.RFC3339)
}

func normalizeProviderID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeChannel(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return defaultChannel
	}
	return normalized
}

func findChatBySession(st *repo.State, sessionID, userID, channel string) (string, domain.ChatSpec, bool) {
	if st == nil {
		return "", domain.ChatSpec{}, false
	}
	for id, chat := range st.Chats {
		if chat.SessionID != sessionID || chat.UserID != userID || chat.Channel != channel {
			continue
		}
		return id, chat, true
	}
	return "", domain.ChatSpec{}, false
}

func parseChatActiveLLMOverride(meta map[string]interface{}) (domain.ChatActiveLLMOverride, bool) {
	if len(meta) == 0 {
		return domain.ChatActiveLLMOverride{}, false
	}
	raw, ok := meta[domain.ChatMetaActiveLLM]
	if !ok || raw == nil {
		return domain.ChatActiveLLMOverride{}, false
	}

	switch value := raw.(type) {
	case domain.ChatActiveLLMOverride:
		override := value
		override.ProviderID = normalizeProviderID(override.ProviderID)
		override.Model = strings.TrimSpace(override.Model)
		override.UpdatedAt = strings.TrimSpace(override.UpdatedAt)
		if override.ProviderID == "" || override.Model == "" {
			return domain.ChatActiveLLMOverride{}, false
		}
		return override, true
	case map[string]interface{}:
		override := domain.ChatActiveLLMOverride{
			ProviderID: normalizeProviderID(stringValue(value["provider_id"])),
			Model:      strings.TrimSpace(stringValue(value["model"])),
			UpdatedAt:  strings.TrimSpace(stringValue(value["updated_at"])),
		}
		if override.ProviderID == "" || override.Model == "" {
			return domain.ChatActiveLLMOverride{}, false
		}
		return override, true
	default:
		return domain.ChatActiveLLMOverride{}, false
	}
}

func resolveAndValidateModel(st *repo.State, providerID, modelID string) (domain.ModelSlotConfig, error) {
	providerID = normalizeProviderID(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return domain.ModelSlotConfig{}, &ServiceError{
			Code:    "session_model_invalid",
			Message: "provider_id and model are required",
		}
	}
	setting, ok := findProviderSettingByID(st, providerID)
	if !ok {
		return domain.ModelSlotConfig{}, &ServiceError{
			Code:    "session_model_invalid",
			Message: "provider not found",
			Details: map[string]string{"provider_id": providerID},
		}
	}
	normalizeProviderSetting(&setting)
	if !providerEnabled(setting) {
		return domain.ModelSlotConfig{}, &ServiceError{
			Code:    "session_model_invalid",
			Message: "provider is disabled",
			Details: map[string]string{"provider_id": providerID},
		}
	}
	resolvedModel, ok := provider.ResolveModelID(providerID, modelID, setting.ModelAliases)
	if !ok {
		return domain.ModelSlotConfig{}, &ServiceError{
			Code:    "session_model_invalid",
			Message: "model not found for provider",
			Details: map[string]string{
				"provider_id": providerID,
				"model":       modelID,
			},
		}
	}
	return domain.ModelSlotConfig{
		ProviderID: providerID,
		Model:      resolvedModel,
	}, nil
}

func findProviderSettingByID(st *repo.State, providerID string) (repo.ProviderSetting, bool) {
	if st == nil {
		return repo.ProviderSetting{}, false
	}
	if setting, ok := st.Providers[providerID]; ok {
		return setting, true
	}
	for key, setting := range st.Providers {
		if normalizeProviderID(key) == providerID {
			return setting, true
		}
	}
	return repo.ProviderSetting{}, false
}

func normalizeProviderSetting(setting *repo.ProviderSetting) {
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

func providerEnabled(setting repo.ProviderSetting) bool {
	if setting.Enabled == nil {
		return true
	}
	return *setting.Enabled
}

func stringValue(raw interface{}) string {
	switch value := raw.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func cloneJSONValue(input interface{}) interface{} {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

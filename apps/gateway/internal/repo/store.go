package repo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nextai/apps/gateway/internal/domain"
)

type ProviderSetting struct {
	APIKey       string            `json:"api_key"`
	BaseURL      string            `json:"base_url"`
	DisplayName  string            `json:"display_name,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	TimeoutMS    int               `json:"timeout_ms,omitempty"`
	ModelAliases map[string]string `json:"model_aliases,omitempty"`
}

type State struct {
	Chats      map[string]domain.ChatSpec         `json:"chats"`
	Histories  map[string][]domain.RuntimeMessage `json:"histories"`
	CronJobs   map[string]domain.CronJobSpec      `json:"cron_jobs"`
	CronStates map[string]domain.CronJobState     `json:"cron_states"`
	Providers  map[string]ProviderSetting         `json:"providers"`
	ActiveLLM  domain.ModelSlotConfig             `json:"active_llm"`
	Envs       map[string]string                  `json:"envs"`
	Skills     map[string]domain.SkillSpec        `json:"skills"`
	Channels   domain.ChannelConfigMap            `json:"channels"`
}

type Store struct {
	mu        sync.RWMutex
	state     State
	stateFile string
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		stateFile: filepath.Join(dataDir, "state.json"),
		state:     defaultState(dataDir),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func defaultState(dataDir string) State {
	state := State{
		Chats:      map[string]domain.ChatSpec{},
		Histories:  map[string][]domain.RuntimeMessage{},
		CronJobs:   map[string]domain.CronJobSpec{},
		CronStates: map[string]domain.CronJobState{},
		Providers: map[string]ProviderSetting{
			"openai": defaultProviderSetting(),
		},
		ActiveLLM: domain.ModelSlotConfig{},
		Envs:      map[string]string{},
		Skills:    map[string]domain.SkillSpec{},
		Channels: domain.ChannelConfigMap{
			"console": {
				"enabled":    true,
				"bot_prefix": "",
			},
			"webhook": {
				"enabled":         false,
				"url":             "",
				"method":          "POST",
				"headers":         map[string]interface{}{},
				"timeout_seconds": 5,
			},
			"qq": {
				"enabled":         false,
				"app_id":          "",
				"client_secret":   "",
				"bot_prefix":      "",
				"target_type":     "c2c",
				"target_id":       "",
				"api_base":        "https://api.sgroup.qq.com",
				"token_url":       "https://bots.qq.com/app/getAppAccessToken",
				"timeout_seconds": 8,
			},
		},
	}
	ensureDefaultChat(&state)
	ensureDefaultCronJob(&state)
	return state
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return s.saveLocked()
	}
	if err != nil {
		return err
	}
	var state State
	if err := json.Unmarshal(b, &state); err != nil {
		return err
	}
	if state.Chats == nil {
		state.Chats = map[string]domain.ChatSpec{}
	}
	if state.Histories == nil {
		state.Histories = map[string][]domain.RuntimeMessage{}
	}
	if state.CronJobs == nil {
		state.CronJobs = map[string]domain.CronJobSpec{}
	}
	if state.CronStates == nil {
		state.CronStates = map[string]domain.CronJobState{}
	}
	if state.Providers == nil {
		state.Providers = map[string]ProviderSetting{
			"openai": defaultProviderSetting(),
		}
	}
	normalizedProviders := map[string]ProviderSetting{}

	for rawID, setting := range state.Providers {
		id := normalizeProviderID(rawID)
		if id == "" {
			continue
		}
		if id == "demo" {
			continue
		}
		normalizeProviderSetting(&setting)
		normalizedProviders[id] = setting
	}
	state.Providers = normalizedProviders
	activeProviderID := normalizeProviderID(state.ActiveLLM.ProviderID)
	activeModelID := strings.TrimSpace(state.ActiveLLM.Model)
	if activeProviderID == "" || activeModelID == "" {
		state.ActiveLLM = domain.ModelSlotConfig{}
	} else if _, ok := normalizedProviders[activeProviderID]; !ok {
		state.ActiveLLM = domain.ModelSlotConfig{}
	} else {
		state.ActiveLLM = domain.ModelSlotConfig{
			ProviderID: activeProviderID,
			Model:      activeModelID,
		}
	}
	if state.Envs == nil {
		state.Envs = map[string]string{}
	}
	if state.Skills == nil {
		state.Skills = map[string]domain.SkillSpec{}
	}
	if state.Channels == nil {
		state.Channels = domain.ChannelConfigMap{}
	}
	if _, ok := state.Channels["console"]; !ok {
		state.Channels["console"] = map[string]interface{}{
			"enabled":    true,
			"bot_prefix": "",
		}
	}
	if _, ok := state.Channels["webhook"]; !ok {
		state.Channels["webhook"] = map[string]interface{}{
			"enabled":         false,
			"url":             "",
			"method":          "POST",
			"headers":         map[string]interface{}{},
			"timeout_seconds": 5,
		}
	}
	if _, ok := state.Channels["qq"]; !ok {
		state.Channels["qq"] = map[string]interface{}{
			"enabled":         false,
			"app_id":          "",
			"client_secret":   "",
			"bot_prefix":      "",
			"target_type":     "c2c",
			"target_id":       "",
			"api_base":        "https://api.sgroup.qq.com",
			"token_url":       "https://bots.qq.com/app/getAppAccessToken",
			"timeout_seconds": 8,
		}
	}
	ensureDefaultChat(&state)
	ensureDefaultCronJob(&state)
	s.state = state
	return nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	ensureDefaultChat(&s.state)
	ensureDefaultCronJob(&s.state)
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.stateFile, b, 0o644)
}

func ensureDefaultChat(state *State) {
	if state == nil {
		return
	}
	if state.Chats == nil {
		state.Chats = map[string]domain.ChatSpec{}
	}
	if state.Histories == nil {
		state.Histories = map[string][]domain.RuntimeMessage{}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	defaultMeta := map[string]interface{}{
		domain.ChatMetaSystemDefault: true,
	}
	defaultChat := domain.ChatSpec{
		ID:        domain.DefaultChatID,
		Name:      domain.DefaultChatName,
		SessionID: domain.DefaultChatSessionID,
		UserID:    domain.DefaultChatUserID,
		Channel:   domain.DefaultChatChannel,
		CreatedAt: now,
		UpdatedAt: now,
		Meta:      defaultMeta,
	}

	if current, ok := state.Chats[domain.DefaultChatID]; ok {
		if strings.TrimSpace(current.CreatedAt) != "" {
			defaultChat.CreatedAt = current.CreatedAt
		}
		if strings.TrimSpace(current.UpdatedAt) != "" {
			defaultChat.UpdatedAt = current.UpdatedAt
		}
		if current.Meta != nil {
			for key, value := range current.Meta {
				defaultChat.Meta[key] = value
			}
		}
		defaultChat.Meta[domain.ChatMetaSystemDefault] = true
	}

	state.Chats[domain.DefaultChatID] = defaultChat
	if _, ok := state.Histories[domain.DefaultChatID]; !ok {
		state.Histories[domain.DefaultChatID] = []domain.RuntimeMessage{}
	}
}

func ensureDefaultCronJob(state *State) {
	if state == nil {
		return
	}
	if state.CronJobs == nil {
		state.CronJobs = map[string]domain.CronJobSpec{}
	}
	if state.CronStates == nil {
		state.CronStates = map[string]domain.CronJobState{}
	}

	defaultJob := domain.CronJobSpec{
		ID:      domain.DefaultCronJobID,
		Name:    domain.DefaultCronJobName,
		Enabled: false,
		Schedule: domain.CronScheduleSpec{
			Type: "interval",
			Cron: domain.DefaultCronJobInterval,
		},
		TaskType: "text",
		Text:     domain.DefaultCronJobText,
		Dispatch: domain.CronDispatchSpec{
			Type:    "channel",
			Channel: domain.DefaultChatChannel,
			Target: domain.CronDispatchTarget{
				UserID:    domain.DefaultChatUserID,
				SessionID: domain.DefaultChatSessionID,
			},
			Mode: "",
			Meta: map[string]interface{}{},
		},
		Runtime: domain.CronRuntimeSpec{
			MaxConcurrency:      1,
			TimeoutSeconds:      30,
			MisfireGraceSeconds: 0,
		},
		Meta: map[string]interface{}{
			domain.CronMetaSystemDefault: true,
		},
	}

	current, ok := state.CronJobs[domain.DefaultCronJobID]
	if !ok {
		state.CronJobs[domain.DefaultCronJobID] = defaultJob
		return
	}

	current.ID = domain.DefaultCronJobID
	if strings.TrimSpace(current.Name) == "" {
		current.Name = domain.DefaultCronJobName
	}

	scheduleType := strings.ToLower(strings.TrimSpace(current.Schedule.Type))
	if scheduleType != "interval" && scheduleType != "cron" {
		scheduleType = "interval"
	}
	current.Schedule.Type = scheduleType
	if strings.TrimSpace(current.Schedule.Cron) == "" {
		current.Schedule.Cron = domain.DefaultCronJobInterval
	}

	if strings.TrimSpace(current.Dispatch.Type) == "" {
		current.Dispatch.Type = "channel"
	}
	if strings.TrimSpace(current.Dispatch.Channel) == "" {
		current.Dispatch.Channel = domain.DefaultChatChannel
	}
	if strings.TrimSpace(current.Dispatch.Target.UserID) == "" {
		current.Dispatch.Target.UserID = domain.DefaultChatUserID
	}
	if strings.TrimSpace(current.Dispatch.Target.SessionID) == "" {
		current.Dispatch.Target.SessionID = domain.DefaultChatSessionID
	}
	if current.Dispatch.Meta == nil {
		current.Dispatch.Meta = map[string]interface{}{}
	}

	if current.Runtime.MaxConcurrency <= 0 {
		current.Runtime.MaxConcurrency = 1
	}
	if current.Runtime.TimeoutSeconds <= 0 {
		current.Runtime.TimeoutSeconds = 30
	}
	if current.Runtime.MisfireGraceSeconds < 0 {
		current.Runtime.MisfireGraceSeconds = 0
	}

	taskType := strings.ToLower(strings.TrimSpace(current.TaskType))
	switch taskType {
	case "workflow":
		current.TaskType = "workflow"
		if current.Workflow == nil {
			current.TaskType = "text"
			current.Text = domain.DefaultCronJobText
		}
	case "text":
		current.TaskType = "text"
		if strings.TrimSpace(current.Text) == "" {
			current.Text = domain.DefaultCronJobText
		}
		current.Workflow = nil
	default:
		current.TaskType = "text"
		current.Workflow = nil
		if strings.TrimSpace(current.Text) == "" {
			current.Text = domain.DefaultCronJobText
		}
	}

	if current.Meta == nil {
		current.Meta = map[string]interface{}{}
	}
	current.Meta[domain.CronMetaSystemDefault] = true

	state.CronJobs[domain.DefaultCronJobID] = current
}

func (s *Store) Read(fn func(state *State)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(&s.state)
}

func (s *Store) Write(fn func(state *State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.state); err != nil {
		return err
	}
	return s.saveLocked()
}

func defaultProviderSetting() ProviderSetting {
	enabled := true
	return ProviderSetting{
		Enabled:      &enabled,
		Headers:      map[string]string{},
		ModelAliases: map[string]string{},
	}
}

func normalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func normalizeProviderSetting(setting *ProviderSetting) {
	if setting == nil {
		return
	}
	setting.DisplayName = strings.TrimSpace(setting.DisplayName)
	setting.APIKey = strings.TrimSpace(setting.APIKey)
	setting.BaseURL = strings.TrimSpace(setting.BaseURL)
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

func mergeProviderSetting(dst *ProviderSetting, src ProviderSetting) {
	if dst == nil {
		return
	}
	if src.DisplayName != "" {
		dst.DisplayName = src.DisplayName
	}
	if src.APIKey != "" {
		dst.APIKey = src.APIKey
	}
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
	if src.Enabled != nil {
		enabled := *src.Enabled
		dst.Enabled = &enabled
	}
	if len(src.Headers) > 0 {
		dst.Headers = map[string]string{}
		for key, value := range src.Headers {
			k := strings.TrimSpace(key)
			v := strings.TrimSpace(value)
			if k == "" || v == "" {
				continue
			}
			dst.Headers[k] = v
		}
	}
	if src.TimeoutMS > 0 {
		dst.TimeoutMS = src.TimeoutMS
	}
	if len(src.ModelAliases) > 0 {
		dst.ModelAliases = map[string]string{}
		for key, value := range src.ModelAliases {
			alias := strings.TrimSpace(key)
			modelID := strings.TrimSpace(value)
			if alias == "" || modelID == "" {
				continue
			}
			dst.ModelAliases[alias] = modelID
		}
	}
}

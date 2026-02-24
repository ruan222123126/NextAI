package ports

import (
	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
)

type SettingsAggregate struct {
	Envs      map[string]string
	Skills    map[string]domain.SkillSpec
	Channels  domain.ChannelConfigMap
	Providers map[string]repo.ProviderSetting
	ActiveLLM domain.ModelSlotConfig
}

type ConversationsAggregate struct {
	Chats     map[string]domain.ChatSpec
	Histories map[string][]domain.RuntimeMessage
}

type SessionAggregate struct {
	Chats     map[string]domain.ChatSpec
	Providers map[string]repo.ProviderSetting
	ActiveLLM domain.ModelSlotConfig
}

type CronAggregate struct {
	Jobs   map[string]domain.CronJobSpec
	States map[string]domain.CronJobState
}

type StateStore interface {
	ReadSettings(func(state SettingsAggregate))
	WriteSettings(func(state *SettingsAggregate) error) error

	ReadConversations(func(state ConversationsAggregate))
	WriteConversations(func(state *ConversationsAggregate) error) error

	ReadSession(func(state SessionAggregate))
	WriteSession(func(state *SessionAggregate) error) error

	ReadCron(func(state CronAggregate))
	WriteCron(func(state *CronAggregate) error) error
}

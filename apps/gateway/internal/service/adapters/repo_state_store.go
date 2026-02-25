package adapters

import (
	"errors"

	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/ports"
)

type RepoStateStore struct {
	Store *repo.Store
}

func NewRepoStateStore(store *repo.Store) RepoStateStore {
	return RepoStateStore{Store: store}
}

func (s RepoStateStore) ReadSettings(fn func(state ports.SettingsAggregate)) {
	if s.Store == nil || fn == nil {
		return
	}
	s.Store.ReadSettings(func(state *repo.State) {
		fn(ports.SettingsAggregate{
			Envs:      state.Envs,
			Skills:    state.Skills,
			Channels:  state.Channels,
			Providers: state.Providers,
			ActiveLLM: state.ActiveLLM,
		})
	})
}

func (s RepoStateStore) WriteSettings(fn func(state *ports.SettingsAggregate) error) error {
	if s.Store == nil {
		return errors.New("state store is unavailable")
	}
	return s.Store.WriteSettings(func(state *repo.State) error {
		if fn == nil {
			return nil
		}
		aggregate := ports.SettingsAggregate{
			Envs:      state.Envs,
			Skills:    state.Skills,
			Channels:  state.Channels,
			Providers: state.Providers,
			ActiveLLM: state.ActiveLLM,
		}
		if err := fn(&aggregate); err != nil {
			return err
		}
		state.Envs = aggregate.Envs
		state.Skills = aggregate.Skills
		state.Channels = aggregate.Channels
		state.Providers = aggregate.Providers
		state.ActiveLLM = aggregate.ActiveLLM
		return nil
	})
}

func (s RepoStateStore) ReadConversations(fn func(state ports.ConversationsAggregate)) {
	if s.Store == nil || fn == nil {
		return
	}
	s.Store.ReadConversations(func(state *repo.State) {
		fn(ports.ConversationsAggregate{
			Chats:     state.Chats,
			Histories: state.Histories,
		})
	})
}

func (s RepoStateStore) WriteConversations(fn func(state *ports.ConversationsAggregate) error) error {
	if s.Store == nil {
		return errors.New("state store is unavailable")
	}
	return s.Store.WriteConversations(func(state *repo.State) error {
		if fn == nil {
			return nil
		}
		aggregate := ports.ConversationsAggregate{
			Chats:     state.Chats,
			Histories: state.Histories,
		}
		if err := fn(&aggregate); err != nil {
			return err
		}
		state.Chats = aggregate.Chats
		state.Histories = aggregate.Histories
		return nil
	})
}

func (s RepoStateStore) ReadSession(fn func(state ports.SessionAggregate)) {
	if s.Store == nil || fn == nil {
		return
	}
	s.Store.ReadSession(func(state *repo.State) {
		fn(ports.SessionAggregate{
			Chats:     state.Chats,
			Providers: state.Providers,
			ActiveLLM: state.ActiveLLM,
		})
	})
}

func (s RepoStateStore) WriteSession(fn func(state *ports.SessionAggregate) error) error {
	if s.Store == nil {
		return errors.New("state store is unavailable")
	}
	return s.Store.WriteSession(func(state *repo.State) error {
		if fn == nil {
			return nil
		}
		aggregate := ports.SessionAggregate{
			Chats:     state.Chats,
			Providers: state.Providers,
			ActiveLLM: state.ActiveLLM,
		}
		if err := fn(&aggregate); err != nil {
			return err
		}
		state.Chats = aggregate.Chats
		state.Providers = aggregate.Providers
		state.ActiveLLM = aggregate.ActiveLLM
		return nil
	})
}

func (s RepoStateStore) ReadCron(fn func(state ports.CronAggregate)) {
	if s.Store == nil || fn == nil {
		return
	}
	s.Store.ReadCron(func(state *repo.State) {
		fn(ports.CronAggregate{
			Jobs:   state.CronJobs,
			States: state.CronStates,
		})
	})
}

func (s RepoStateStore) WriteCron(fn func(state *ports.CronAggregate) error) error {
	if s.Store == nil {
		return errors.New("state store is unavailable")
	}
	return s.Store.WriteCron(func(state *repo.State) error {
		if fn == nil {
			return nil
		}
		aggregate := ports.CronAggregate{
			Jobs:   state.CronJobs,
			States: state.CronStates,
		}
		if err := fn(&aggregate); err != nil {
			return err
		}
		state.CronJobs = aggregate.Jobs
		state.CronStates = aggregate.States
		return nil
	})
}

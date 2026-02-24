package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nextai/apps/gateway/internal/domain"
)

func TestLoadKeepsCustomProviderAndActiveProvider(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "providers": {
    "Custom-OpenAI": {
      "api_key": "sk-legacy",
      "base_url": "http://127.0.0.1:19002/v1",
	    "display_name": "Legacy Gateway",
	    "enabled": true,
	    "reasoning_effort": "high",
	    "store": true,
	    "headers": {"X-Test": "1"},
	    "timeout_ms": 12000,
	    "model_aliases": {"fast": "gpt-4o-mini"}
    }
  },
  "active_llm": {"provider_id": "Custom-OpenAI", "model": "legacy-model"}
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		if len(st.Providers) != 1 {
			t.Fatalf("expected custom provider to remain, got=%d", len(st.Providers))
		}
		custom, ok := st.Providers["custom-openai"]
		if !ok {
			t.Fatalf("custom provider should exist")
		}
		if custom.DisplayName != "Legacy Gateway" {
			t.Fatalf("expected display_name preserved, got=%q", custom.DisplayName)
		}
		if custom.APIKey != "sk-legacy" {
			t.Fatalf("expected api_key preserved, got=%q", custom.APIKey)
		}
		if custom.BaseURL != "http://127.0.0.1:19002/v1" {
			t.Fatalf("expected base_url preserved, got=%q", custom.BaseURL)
		}
		if custom.TimeoutMS != 12000 {
			t.Fatalf("expected timeout_ms preserved, got=%d", custom.TimeoutMS)
		}
		if custom.ModelAliases["fast"] != "gpt-4o-mini" {
			t.Fatalf("expected model_aliases preserved, got=%v", custom.ModelAliases)
		}
		if custom.ReasoningEffort != "high" {
			t.Fatalf("expected reasoning_effort preserved, got=%q", custom.ReasoningEffort)
		}
		if custom.Store == nil || !*custom.Store {
			t.Fatalf("expected store preserved, got=%v", custom.Store)
		}

		if st.ActiveLLM.ProviderID != "custom-openai" {
			t.Fatalf("expected active provider preserved, got=%q", st.ActiveLLM.ProviderID)
		}
		if st.ActiveLLM.Model != "legacy-model" {
			t.Fatalf("expected active model preserved, got=%q", st.ActiveLLM.Model)
		}
	})
}

func TestLoadKeepsEmptyProvidersAndEmptyActive(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "providers": {},
  "active_llm": {"provider_id": "", "model": ""}
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		if len(st.Providers) != 0 {
			t.Fatalf("expected providers to stay empty, got=%d", len(st.Providers))
		}
		if st.ActiveLLM.ProviderID != "" || st.ActiveLLM.Model != "" {
			t.Fatalf("expected empty active_llm, got=%+v", st.ActiveLLM)
		}
	})
}

func TestLoadDropsLegacyDemoProvider(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "providers": {
    "demo": {"enabled": true},
    "openai": {"enabled": true}
  },
  "active_llm": {"provider_id": "demo", "model": "demo-chat"}
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		if _, ok := st.Providers["demo"]; ok {
			t.Fatalf("expected legacy demo provider to be removed")
		}
		if _, ok := st.Providers["openai"]; !ok {
			t.Fatalf("expected openai provider to remain")
		}
		if st.ActiveLLM.ProviderID != "" || st.ActiveLLM.Model != "" {
			t.Fatalf("expected active_llm to be cleared when demo is removed, got=%+v", st.ActiveLLM)
		}
	})
}
func TestLoadEnsuresDefaultChat(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "chats": {},
  "histories": {},
  "providers": {
    "openai": {"enabled": true}
  }
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		chat, ok := st.Chats[domain.DefaultChatID]
		if !ok {
			t.Fatalf("default chat should exist")
		}
		if chat.SessionID != domain.DefaultChatSessionID {
			t.Fatalf("default chat session mismatch: %q", chat.SessionID)
		}
		if chat.UserID != domain.DefaultChatUserID {
			t.Fatalf("default chat user mismatch: %q", chat.UserID)
		}
		if chat.Channel != domain.DefaultChatChannel {
			t.Fatalf("default chat channel mismatch: %q", chat.Channel)
		}
		flag, ok := chat.Meta[domain.ChatMetaSystemDefault].(bool)
		if !ok || !flag {
			t.Fatalf("default chat meta.system_default should be true, meta=%#v", chat.Meta)
		}
		if _, ok := st.Histories[domain.DefaultChatID]; !ok {
			t.Fatalf("default chat history should exist")
		}
	})
}

func TestLoadEnsuresDefaultCronJob(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "cron_jobs": {},
  "cron_states": {},
  "providers": {
    "openai": {"enabled": true}
  }
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		job, ok := st.CronJobs[domain.DefaultCronJobID]
		if !ok {
			t.Fatalf("default cron job should exist")
		}
		if job.Name != domain.DefaultCronJobName {
			t.Fatalf("default cron job name mismatch: %q", job.Name)
		}
		if job.TaskType != "text" {
			t.Fatalf("default cron job task_type mismatch: %q", job.TaskType)
		}
		if job.Text != domain.DefaultCronJobText {
			t.Fatalf("default cron job text mismatch: %q", job.Text)
		}
		if job.Enabled {
			t.Fatalf("default cron job should be disabled by default")
		}
		if job.Schedule.Type != "interval" || job.Schedule.Cron != domain.DefaultCronJobInterval {
			t.Fatalf("default cron schedule mismatch: %+v", job.Schedule)
		}
		if job.Dispatch.Channel != domain.DefaultChatChannel {
			t.Fatalf("default cron channel mismatch: %q", job.Dispatch.Channel)
		}
		if job.Dispatch.Target.UserID != domain.DefaultChatUserID {
			t.Fatalf("default cron user_id mismatch: %q", job.Dispatch.Target.UserID)
		}
		if job.Dispatch.Target.SessionID != domain.DefaultChatSessionID {
			t.Fatalf("default cron session_id mismatch: %q", job.Dispatch.Target.SessionID)
		}
		flag, ok := job.Meta[domain.CronMetaSystemDefault].(bool)
		if !ok || !flag {
			t.Fatalf("default cron meta.system_default should be true, meta=%#v", job.Meta)
		}
	})
}

func TestLoadMigratesLegacyStateWithoutSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "providers": {
    "OpenAI": {"enabled": true}
  },
  "active_llm": {"provider_id": "OpenAI", "model": "gpt-4o-mini"}
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		if st.SchemaVersion != currentStateSchemaVersion {
			t.Fatalf("expected schema_version=%d, got=%d", currentStateSchemaVersion, st.SchemaVersion)
		}
		if st.ActiveLLM.ProviderID != "openai" {
			t.Fatalf("expected normalized active provider, got=%q", st.ActiveLLM.ProviderID)
		}
	})

	persistedRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read migrated state failed: %v", err)
	}
	var persisted map[string]interface{}
	if err := json.Unmarshal(persistedRaw, &persisted); err != nil {
		t.Fatalf("decode migrated state failed: %v", err)
	}
	gotVersion, ok := persisted["schema_version"].(float64)
	if !ok {
		t.Fatalf("expected schema_version to be persisted")
	}
	if int(gotVersion) != currentStateSchemaVersion {
		t.Fatalf("expected persisted schema_version=%d, got=%v", currentStateSchemaVersion, gotVersion)
	}
}

func TestNewStoreWritesSchemaVersionOnNewFile(t *testing.T) {
	dir := t.TempDir()

	if _, err := NewStore(dir); err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	statePath := filepath.Join(dir, "state.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state failed: %v", err)
	}
	var persisted map[string]interface{}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("decode state failed: %v", err)
	}
	gotVersion, ok := persisted["schema_version"].(float64)
	if !ok {
		t.Fatalf("expected schema_version to be persisted")
	}
	if int(gotVersion) != currentStateSchemaVersion {
		t.Fatalf("expected schema_version=%d, got=%v", currentStateSchemaVersion, gotVersion)
	}
}

func TestLoadRejectsFutureSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "schema_version": 999,
  "providers": {
    "openai": {"enabled": true}
  }
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	if _, err := NewStore(dir); err == nil {
		t.Fatalf("expected new store to fail for future schema version")
	}
}

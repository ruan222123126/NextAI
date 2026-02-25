package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModelsIncludesCustomAliasTargetsForCustomProvider(t *testing.T) {
	models := ResolveModels("custom-openai", map[string]string{
		"fast":     "gpt-4o-mini",
		"my-model": "my-model",
	})
	if len(models) != 2 {
		t.Fatalf("expected 2 custom models, got=%d", len(models))
	}

	byID := map[string]bool{}
	for _, model := range models {
		byID[model.ID] = true
		if model.ID == "fast" && model.AliasOf != "gpt-4o-mini" {
			t.Fatalf("expected alias_of for fast model, got=%q", model.AliasOf)
		}
		if model.ID == "my-model" && model.AliasOf != "" {
			t.Fatalf("expected my-model not to set alias_of, got=%q", model.AliasOf)
		}
	}
	if !byID["fast"] || !byID["my-model"] {
		t.Fatalf("expected models to include fast and my-model, got=%v", byID)
	}
}

func TestResolveModelIDUsesCustomAliasTargetForCustomProvider(t *testing.T) {
	got, ok := ResolveModelID("custom-openai", "fast", map[string]string{
		"fast": "gpt-4o-mini",
	})
	if !ok {
		t.Fatalf("expected alias resolution ok=true")
	}
	if got != "gpt-4o-mini" {
		t.Fatalf("expected resolved model gpt-4o-mini, got=%q", got)
	}
}

func TestListProviderTypes(t *testing.T) {
	types := ListProviderTypes()
	if len(types) < 3 {
		t.Fatalf("expected at least 3 provider types, got=%d", len(types))
	}
	if types[0].ID != "openai" || types[0].DisplayName != "openai" {
		t.Fatalf("unexpected first provider type: %+v", types[0])
	}
	if types[1].ID != AdapterOpenAICompatible || types[1].DisplayName != "openai Compatible" {
		t.Fatalf("unexpected second provider type: %+v", types[1])
	}
	if types[2].ID != AdapterCodexCompatible || types[2].DisplayName != "codex Compatible" {
		t.Fatalf("unexpected third provider type: %+v", types[2])
	}
}

func TestResolveAdapterUsesCodexForCodexCompatibleProviderIDs(t *testing.T) {
	if got := ResolveAdapter("codex-compatible"); got != AdapterCodexCompatible {
		t.Fatalf("expected codex adapter for codex-compatible, got=%q", got)
	}
	if got := ResolveAdapter("codex-compatible-2"); got != AdapterCodexCompatible {
		t.Fatalf("expected codex adapter for codex-compatible-2, got=%q", got)
	}
	if got := ResolveAdapter("custom-openai"); got != AdapterOpenAICompatible {
		t.Fatalf("expected openai-compatible adapter for custom-openai, got=%q", got)
	}
}

func TestRegisterProviderAndType(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)

	if err := RegisterProviderType(ProviderTypeSpec{ID: "acme", DisplayName: "Acme"}); err != nil {
		t.Fatalf("register provider type failed: %v", err)
	}
	if err := RegisterProvider(ProviderSpec{
		ID:                 "acme",
		Name:               "Acme",
		Adapter:            AdapterOpenAICompatible,
		AllowCustomBaseURL: true,
		Models: []ModelSpec{
			{ID: "acme-chat", Name: "Acme Chat"},
		},
	}); err != nil {
		t.Fatalf("register provider failed: %v", err)
	}

	spec := ResolveProvider("acme")
	if spec.ID != "acme" {
		t.Fatalf("unexpected provider id: %q", spec.ID)
	}
	if len(spec.Models) != 1 || spec.Models[0].ID != "acme-chat" {
		t.Fatalf("unexpected provider models: %+v", spec.Models)
	}

	types := ListProviderTypes()
	found := false
	for _, item := range types {
		if item.ID == "acme" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected provider type acme in list, types=%+v", types)
	}
}

func TestLoadRegistryFromFile(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)

	dir := t.TempDir()
	path := filepath.Join(dir, "provider-registry.json")
	raw := `{
  "provider_types": [{"id":"custom","display_name":"Custom"}],
  "providers": [{
    "id":"custom-openai",
    "name":"Custom OpenAI",
    "adapter":"openai-compatible",
    "allow_custom_base_url": true,
    "default_base_url": "https://example.com/v1",
    "models":[{"id":"custom-1","name":"Custom 1"}]
  }]
}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write provider registry file failed: %v", err)
	}

	if err := LoadRegistryFromFile(path); err != nil {
		t.Fatalf("load provider registry failed: %v", err)
	}

	spec := ResolveProvider("custom-openai")
	if spec.DefaultBaseURL != "https://example.com/v1" {
		t.Fatalf("unexpected default base url: %q", spec.DefaultBaseURL)
	}
	if len(spec.Models) != 1 || spec.Models[0].ID != "custom-1" {
		t.Fatalf("unexpected loaded provider models: %+v", spec.Models)
	}
}

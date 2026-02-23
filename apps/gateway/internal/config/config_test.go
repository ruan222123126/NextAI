package config

import "testing"

func TestLoadCodexPromptSourceDefaultsToFile(t *testing.T) {
	t.Setenv("NEXTAI_CODEX_PROMPT_SOURCE", "")
	t.Setenv("NEXTAI_CODEX_PROMPT_SHADOW_COMPARE", "")

	cfg := Load()
	if cfg.CodexPromptSource != "file" {
		t.Fatalf("expected default codex prompt source to be file, got=%q", cfg.CodexPromptSource)
	}
	if cfg.EnableCodexPromptShadowCompare {
		t.Fatalf("expected codex prompt shadow compare to default false")
	}
}

func TestLoadCodexPromptSourceCatalog(t *testing.T) {
	t.Setenv("NEXTAI_CODEX_PROMPT_SOURCE", "catalog")
	t.Setenv("NEXTAI_CODEX_PROMPT_SHADOW_COMPARE", "true")

	cfg := Load()
	if cfg.CodexPromptSource != "catalog" {
		t.Fatalf("expected codex prompt source catalog, got=%q", cfg.CodexPromptSource)
	}
	if !cfg.EnableCodexPromptShadowCompare {
		t.Fatalf("expected codex prompt shadow compare true")
	}
}

func TestLoadCodexPromptSourceInvalidFallsBackToFile(t *testing.T) {
	t.Setenv("NEXTAI_CODEX_PROMPT_SOURCE", "unknown")

	cfg := Load()
	if cfg.CodexPromptSource != "file" {
		t.Fatalf("expected invalid source to fallback file, got=%q", cfg.CodexPromptSource)
	}
}

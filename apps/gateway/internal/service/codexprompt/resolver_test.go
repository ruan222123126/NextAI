package codexprompt

import (
	"errors"
	"strings"
	"testing"
)

func mustNewResolverFromJSON(t *testing.T, content string) *Resolver {
	t.Helper()

	catalog, err := ParseCatalog([]byte(content))
	if err != nil {
		t.Fatalf("parse catalog failed: %v", err)
	}
	resolver, err := NewResolverWithCatalog(catalog)
	if err != nil {
		t.Fatalf("new resolver failed: %v", err)
	}
	return resolver
}

func TestResolverRendersPersonalityTemplate(t *testing.T) {
	t.Parallel()

	resolver := mustNewResolverFromJSON(t, `[
		{
			"slug":"gpt-5.2-codex",
			"base_instructions":"BASE",
			"model_messages":{
				"instructions_template":"TOP\n{{ personality }}\nBOTTOM",
				"instructions_variables":{
					"personality_friendly":"FRIENDLY",
					"personality_pragmatic":"PRAGMATIC"
				}
			}
		}
	]`)

	friendly, meta, err := resolver.Resolve("gpt-5.2-codex", "friendly")
	if err != nil {
		t.Fatalf("resolve friendly failed: %v", err)
	}
	if !meta.UsedTemplate {
		t.Fatalf("expected UsedTemplate=true, meta=%+v", meta)
	}
	if meta.FallbackReason != "" {
		t.Fatalf("expected empty fallback reason, got=%q", meta.FallbackReason)
	}
	if !strings.Contains(friendly, "FRIENDLY") {
		t.Fatalf("friendly personality not rendered: %q", friendly)
	}

	pragmatic, _, err := resolver.Resolve("gpt-5.2-codex", "pragmatic")
	if err != nil {
		t.Fatalf("resolve pragmatic failed: %v", err)
	}
	if !strings.Contains(pragmatic, "PRAGMATIC") {
		t.Fatalf("pragmatic personality not rendered: %q", pragmatic)
	}
}

func TestResolverFallsBackToBaseWhenTemplateMissing(t *testing.T) {
	t.Parallel()

	resolver := mustNewResolverFromJSON(t, `[
		{
			"slug":"gpt-5.2-codex",
			"base_instructions":"BASE ONLY"
		}
	]`)

	resolved, meta, err := resolver.Resolve("gpt-5.2-codex", "")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved != "BASE ONLY" {
		t.Fatalf("expected base instructions fallback, got=%q", resolved)
	}
	if meta.UsedTemplate {
		t.Fatalf("expected UsedTemplate=false, meta=%+v", meta)
	}
	if meta.FallbackReason != "missing_template" {
		t.Fatalf("unexpected fallback reason: %q", meta.FallbackReason)
	}
}

func TestResolverMissingSlugReturnsNotFoundError(t *testing.T) {
	t.Parallel()

	resolver := mustNewResolverFromJSON(t, `[
		{"slug":"gpt-5.2-codex","base_instructions":"BASE"}
	]`)

	_, _, err := resolver.Resolve("gpt-unknown-codex", "pragmatic")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got=%v", err)
	}
}

func TestResolverMissingPersonalityVariableFallsBackToBase(t *testing.T) {
	t.Parallel()

	resolver := mustNewResolverFromJSON(t, `[
		{
			"slug":"gpt-5.2-codex",
			"base_instructions":"BASE",
			"model_messages":{
				"instructions_template":"{{ personality }}",
				"instructions_variables":{"personality_friendly":"FRIENDLY"}
			}
		}
	]`)

	resolved, meta, err := resolver.Resolve("gpt-5.2-codex", "pragmatic")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved != "BASE" {
		t.Fatalf("expected base fallback, got=%q", resolved)
	}
	if meta.FallbackReason != "missing_personality_variable:personality_pragmatic" {
		t.Fatalf("unexpected fallback reason: %q", meta.FallbackReason)
	}
}

func TestResolverUnresolvedPlaceholderFallsBackToBase(t *testing.T) {
	t.Parallel()

	resolver := mustNewResolverFromJSON(t, `[
		{
			"slug":"gpt-5.2-codex",
			"base_instructions":"BASE",
			"model_messages":{
				"instructions_template":"{{ personality }}",
				"instructions_variables":{"personality_pragmatic":"{{ personality }}"}
			}
		}
	]`)

	resolved, meta, err := resolver.Resolve("gpt-5.2-codex", "pragmatic")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved != "BASE" {
		t.Fatalf("expected base fallback, got=%q", resolved)
	}
	if meta.FallbackReason != "unresolved_personality_placeholder" {
		t.Fatalf("unexpected fallback reason: %q", meta.FallbackReason)
	}
}

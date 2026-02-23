package codexprompt

import (
	"errors"
	"testing"
)

func TestParseCatalogRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	if _, err := ParseCatalog([]byte("{not-json")); err == nil {
		t.Fatal("expected invalid json error")
	}
}

func TestParseCatalogRejectsDuplicateSlug(t *testing.T) {
	t.Parallel()

	content := []byte(`[
		{"slug":"gpt-5.2-codex","base_instructions":"A"},
		{"slug":"gpt-5.2-codex","base_instructions":"B"}
	]`)
	_, err := ParseCatalog(content)
	if err == nil {
		t.Fatal("expected duplicate slug validation error")
	}
	if !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("expected ErrInvalidCatalog, got=%v", err)
	}
}

func TestParseCatalogRejectsEmptyBaseInstructions(t *testing.T) {
	t.Parallel()

	content := []byte(`[
		{"slug":"gpt-5.2-codex","base_instructions":"   "}
	]`)
	_, err := ParseCatalog(content)
	if err == nil {
		t.Fatal("expected empty base_instructions validation error")
	}
	if !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("expected ErrInvalidCatalog, got=%v", err)
	}
}

func TestNormalizePersonalityFallbackToPragmatic(t *testing.T) {
	t.Parallel()

	got, downgraded := NormalizePersonality("invalid")
	if got != PersonalityPragmatic {
		t.Fatalf("expected pragmatic fallback, got=%q", got)
	}
	if !downgraded {
		t.Fatalf("expected downgraded=true for invalid personality")
	}
}

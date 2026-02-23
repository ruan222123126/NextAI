package codexprompt

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var personalityPlaceholderPattern = regexp.MustCompile(`\{\{\s*personality\s*\}\}`)

type Resolver struct {
	catalog *Catalog
}

func NewResolver(relativeCatalogPath string) (*Resolver, error) {
	catalog, err := LoadCatalog(relativeCatalogPath)
	if err != nil {
		return nil, err
	}
	return NewResolverWithCatalog(catalog)
}

func NewResolverWithCatalog(catalog *Catalog) (*Resolver, error) {
	if catalog == nil {
		return nil, errors.New("codex runtime catalog is required")
	}
	return &Resolver{catalog: catalog}, nil
}

func (r *Resolver) Resolve(modelSlug string, personality string) (string, ResolveMeta, error) {
	meta := ResolveMeta{}
	if r == nil || r.catalog == nil {
		return "", meta, errors.New("codex instruction resolver is unavailable")
	}
	normalizedSlug := NormalizeModelSlug(modelSlug)
	model, ok := r.catalog.Find(normalizedSlug)
	if !ok {
		if normalizedSlug == "" {
			normalizedSlug = "<empty>"
		}
		return "", meta, fmt.Errorf("%w: %s", ErrModelNotFound, normalizedSlug)
	}
	meta.SourceSlug = model.Slug
	baseInstructions := strings.TrimSpace(model.BaseInstructions)
	if baseInstructions == "" {
		return "", meta, fmt.Errorf("%w: empty base_instructions for %s", ErrInvalidCatalog, model.Slug)
	}

	template := strings.TrimSpace(model.ModelMessages.InstructionsTemplate)
	if template == "" {
		meta.FallbackReason = "missing_template"
		return baseInstructions, meta, nil
	}

	resolvedPersonality, _ := NormalizePersonality(personality)
	variableName := "personality_" + resolvedPersonality
	personalityValue := strings.TrimSpace(model.ModelMessages.InstructionsVariables[strings.ToLower(variableName)])
	if personalityValue == "" {
		meta.FallbackReason = "missing_personality_variable:" + variableName
		return baseInstructions, meta, nil
	}

	rendered := strings.TrimSpace(personalityPlaceholderPattern.ReplaceAllString(template, personalityValue))
	if rendered == "" {
		meta.FallbackReason = "empty_rendered_template"
		return baseInstructions, meta, nil
	}
	if personalityPlaceholderPattern.MatchString(rendered) {
		meta.FallbackReason = "unresolved_personality_placeholder"
		return baseInstructions, meta, nil
	}

	meta.UsedTemplate = true
	return rendered, meta, nil
}

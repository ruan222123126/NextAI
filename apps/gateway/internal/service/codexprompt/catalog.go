package codexprompt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	systempromptservice "nextai/apps/gateway/internal/service/systemprompt"
)

type Catalog struct {
	modelsBySlug map[string]RuntimeCatalogModel
}

func LoadCatalog(relativePath string) (*Catalog, error) {
	normalizedPath, ok := systempromptservice.NormalizeRelativePath(relativePath)
	if !ok {
		return nil, fmt.Errorf("%w: invalid catalog path", ErrInvalidCatalog)
	}
	workspaceRoot, err := systempromptservice.FindWorkspaceRoot()
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(filepath.Join(workspaceRoot, filepath.FromSlash(normalizedPath)))
	if err != nil {
		return nil, err
	}
	return ParseCatalog(content)
}

func ParseCatalog(content []byte) (*Catalog, error) {
	var models []RuntimeCatalogModel
	if err := json.Unmarshal(content, &models); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("%w: empty model list", ErrInvalidCatalog)
	}
	index := make(map[string]RuntimeCatalogModel, len(models))
	for _, item := range models {
		slug := NormalizeModelSlug(item.Slug)
		if slug == "" {
			return nil, fmt.Errorf("%w: model slug is required", ErrInvalidCatalog)
		}
		if _, exists := index[slug]; exists {
			return nil, fmt.Errorf("%w: duplicate slug %q", ErrInvalidCatalog, slug)
		}
		base := strings.TrimSpace(item.BaseInstructions)
		if base == "" {
			return nil, fmt.Errorf("%w: base_instructions is required for %q", ErrInvalidCatalog, slug)
		}
		template := strings.TrimSpace(item.ModelMessages.InstructionsTemplate)
		variables := map[string]string{}
		for key, value := range item.ModelMessages.InstructionsVariables {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			if normalizedKey == "" {
				continue
			}
			variables[normalizedKey] = strings.TrimSpace(value)
		}
		index[slug] = RuntimeCatalogModel{
			Slug:             slug,
			BaseInstructions: base,
			ModelMessages: RuntimeCatalogModelMessages{
				InstructionsTemplate:  template,
				InstructionsVariables: variables,
			},
		}
	}
	return &Catalog{modelsBySlug: index}, nil
}

func (c *Catalog) Find(slug string) (RuntimeCatalogModel, bool) {
	if c == nil || len(c.modelsBySlug) == 0 {
		return RuntimeCatalogModel{}, false
	}
	item, ok := c.modelsBySlug[NormalizeModelSlug(slug)]
	return item, ok
}

func NormalizeModelSlug(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func NormalizePersonality(raw string) (normalized string, downgraded bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case PersonalityFriendly:
		return PersonalityFriendly, false
	case "", PersonalityPragmatic:
		return PersonalityPragmatic, false
	default:
		return PersonalityPragmatic, true
	}
}

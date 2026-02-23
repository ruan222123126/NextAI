package codexprompt

import "errors"

const (
	PromptSourceFile    = "file"
	PromptSourceCatalog = "catalog"

	PersonalityFriendly  = "friendly"
	PersonalityPragmatic = "pragmatic"
)

var ErrModelNotFound = errors.New("codex model not found")
var ErrInvalidCatalog = errors.New("invalid codex runtime catalog")

type ResolveMeta struct {
	SourceSlug     string
	UsedTemplate   bool
	FallbackReason string
}

type CodexInstructionResolver interface {
	Resolve(modelSlug string, personality string) (resolved string, meta ResolveMeta, err error)
}

type RuntimeCatalogModel struct {
	Slug             string                      `json:"slug"`
	BaseInstructions string                      `json:"base_instructions"`
	ModelMessages    RuntimeCatalogModelMessages `json:"model_messages"`
}

type RuntimeCatalogModelMessages struct {
	InstructionsTemplate  string            `json:"instructions_template"`
	InstructionsVariables map[string]string `json:"instructions_variables"`
}

package systemprompt

import (
	"fmt"
	"os"
	"strings"

	"nextai/apps/gateway/internal/domain"
)

type Layer struct {
	Name    string
	Role    string
	Source  string
	Content string
}

type Dependencies struct {
	EnableEnvironmentContext bool
	LoadRequiredLayer        func(candidatePaths []string) (string, string, error)
	WorkingDirectory         func() (string, error)
	LookupEnv                func(string) string
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	if deps.WorkingDirectory == nil {
		deps.WorkingDirectory = os.Getwd
	}
	if deps.LookupEnv == nil {
		deps.LookupEnv = os.Getenv
	}
	return &Service{deps: deps}
}

func (s *Service) BuildLayers(baseCandidates, toolGuideCandidates []string) ([]Layer, error) {
	if s == nil || s.deps.LoadRequiredLayer == nil {
		return nil, fmt.Errorf("system prompt layer loader is unavailable")
	}

	layers := make([]Layer, 0, 5)
	basePath, baseContent, err := s.deps.LoadRequiredLayer(baseCandidates)
	if err != nil {
		return nil, err
	}
	layers = append(layers, Layer{
		Name:    "base_system",
		Role:    "system",
		Source:  basePath,
		Content: FormatLayerSourceContent(basePath, baseContent),
	})

	toolGuidePath, toolGuideContent, err := s.deps.LoadRequiredLayer(toolGuideCandidates)
	if err != nil {
		return nil, err
	}
	layers = append(layers, Layer{
		Name:    "tool_guide_system",
		Role:    "system",
		Source:  toolGuidePath,
		Content: FormatLayerSourceContent(toolGuidePath, toolGuideContent),
	})

	layers = AppendLayerIfPresent(layers, Layer{Name: "workspace_policy_system", Role: "system"})
	layers = AppendLayerIfPresent(layers, Layer{Name: "session_policy_system", Role: "system"})

	if s.deps.EnableEnvironmentContext {
		layers = append(layers, BuildEnvironmentContextLayer(s.deps.WorkingDirectory, s.deps.LookupEnv))
	}
	return layers, nil
}

func PrependLayers(input []domain.AgentInputMessage, layers []Layer) []domain.AgentInputMessage {
	effective := make([]domain.AgentInputMessage, 0, len(input)+len(layers))
	for _, layer := range layers {
		if strings.TrimSpace(layer.Content) == "" {
			continue
		}
		effective = append(effective, domain.AgentInputMessage{
			Role:    "system",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: layer.Content}},
		})
	}
	effective = append(effective, input...)
	return effective
}

func AppendLayerIfPresent(layers []Layer, layer Layer) []Layer {
	if strings.TrimSpace(layer.Content) == "" {
		return layers
	}
	return append(layers, layer)
}

func FormatLayerSourceContent(sourcePath, content string) string {
	return fmt.Sprintf("## %s\n%s", sourcePath, content)
}

func BuildEnvironmentContextLayer(getwd func() (string, error), lookupEnv func(string) string) Layer {
	return Layer{
		Name:    "environment_context_system",
		Role:    "system",
		Source:  "runtime",
		Content: BuildEnvironmentContextContent(getwd, lookupEnv),
	}
}

func BuildEnvironmentContextContent(getwd func() (string, error), lookupEnv func(string) string) string {
	if getwd == nil {
		getwd = os.Getwd
	}
	if lookupEnv == nil {
		lookupEnv = os.Getenv
	}

	cwd, err := getwd()
	if err != nil {
		cwd = ""
	}
	shell := strings.TrimSpace(lookupEnv("SHELL"))
	if shell == "" {
		shell = "unknown"
	}
	network := strings.TrimSpace(lookupEnv("NEXTAI_NETWORK_ACCESS"))
	if network == "" {
		network = "unknown"
	}

	return fmt.Sprintf(
		"<environment_context>\n  <cwd>%s</cwd>\n  <shell>%s</shell>\n  <network>%s</network>\n</environment_context>",
		EscapeXMLText(cwd),
		EscapeXMLText(shell),
		EscapeXMLText(network),
	)
}

func EscapeXMLText(v string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	).Replace(v)
}

func SummarizeLayerPreview(text string, limit int) string {
	normalized := strings.TrimSpace(text)
	if normalized == "" || limit <= 0 {
		return ""
	}
	runes := []rune(normalized)
	if len(runes) <= limit {
		return normalized
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func EstimateTokenCount(text string) int {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return 0
	}

	cjkCount := 0
	remaining := make([]rune, 0, len(normalized))
	for _, r := range normalized {
		if isCJKTokenRune(r) {
			cjkCount++
			remaining = append(remaining, ' ')
			continue
		}
		remaining = append(remaining, r)
	}

	estimate := cjkCount
	for _, chunk := range strings.Fields(string(remaining)) {
		runeLen := len([]rune(chunk))
		if runeLen == 0 {
			continue
		}
		tokenCount := (runeLen + 3) / 4
		if tokenCount < 1 {
			tokenCount = 1
		}
		estimate += tokenCount
	}
	return estimate
}

func isCJKTokenRune(r rune) bool {
	switch {
	case r >= 0x3400 && r <= 0x4DBF:
		return true
	case r >= 0x4E00 && r <= 0x9FFF:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0x3040 && r <= 0x30FF:
		return true
	case r >= 0xAC00 && r <= 0xD7AF:
		return true
	default:
		return false
	}
}

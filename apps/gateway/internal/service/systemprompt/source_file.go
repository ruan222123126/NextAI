package systemprompt

import (
	"context"
	"fmt"
)

type FileSource struct {
	loadRequiredLayer func(candidatePaths []string) (string, string, error)
}

func NewFileSource(loadRequiredLayer func(candidatePaths []string) (string, string, error)) *FileSource {
	return &FileSource{loadRequiredLayer: loadRequiredLayer}
}

func (s *FileSource) Name() string {
	return SourceFile
}

func (s *FileSource) Build(_ context.Context, req BuildRequest) ([]Layer, error) {
	if s == nil || s.loadRequiredLayer == nil {
		return nil, fmt.Errorf("system prompt layer loader is unavailable")
	}

	layers := make([]Layer, 0, 4)

	basePath, baseContent, err := s.loadRequiredLayer(req.BaseCandidates)
	if err != nil {
		return nil, err
	}
	layers = append(layers, Layer{
		Name:    "base_system",
		Role:    "system",
		Source:  basePath,
		Content: FormatLayerSourceContent(basePath, baseContent),
	})

	toolGuidePath, toolGuideContent, err := s.loadRequiredLayer(req.ToolGuideCandidates)
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
	return layers, nil
}

package systemprompt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildLayersIncludesEnvironmentContext(t *testing.T) {
	t.Parallel()

	call := 0
	svc := NewService(Dependencies{
		EnableEnvironmentContext: true,
		LoadRequiredLayer: func(candidatePaths []string) (string, string, error) {
			call++
			if len(candidatePaths) == 0 {
				return "", "", errors.New("missing candidates")
			}
			if call == 1 {
				return candidatePaths[0], "base-content", nil
			}
			return candidatePaths[0], "tool-content", nil
		},
		WorkingDirectory: func() (string, error) { return "/tmp/nextai", nil },
		LookupEnv: func(key string) string {
			switch key {
			case "SHELL":
				return "/bin/bash"
			case "NEXTAI_NETWORK_ACCESS":
				return "enabled"
			default:
				return ""
			}
		},
	})

	layers, err := svc.BuildLayers(
		[]string{"docs/AI/AGENTS.md"},
		[]string{"docs/AI/ai-tools.md"},
	)
	if err != nil {
		t.Fatalf("build layers failed: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("unexpected layer count: %d", len(layers))
	}
	if layers[0].Name != "base_system" || !strings.Contains(layers[0].Content, "base-content") {
		t.Fatalf("unexpected base layer: %+v", layers[0])
	}
	if layers[1].Name != "tool_guide_system" || !strings.Contains(layers[1].Content, "tool-content") {
		t.Fatalf("unexpected tool layer: %+v", layers[1])
	}
	if layers[2].Name != "environment_context_system" {
		t.Fatalf("unexpected environment layer: %+v", layers[2])
	}
	if !strings.Contains(layers[2].Content, "<cwd>/tmp/nextai</cwd>") {
		t.Fatalf("missing cwd in environment context: %s", layers[2].Content)
	}
}

func TestBuildLayersRequiresLoader(t *testing.T) {
	t.Parallel()

	svc := NewService(Dependencies{})
	if _, err := svc.BuildLayers([]string{"a"}, []string{"b"}); err == nil {
		t.Fatal("expected build layers to fail without loader")
	}
}

func TestBuildLayersForSourceUsesRegisteredSource(t *testing.T) {
	t.Parallel()

	svc := NewService(Dependencies{})
	svc.RegisterSource(stubSource{
		name: "stub-source",
		build: func(context.Context, BuildRequest) ([]Layer, error) {
			return []Layer{
				{Name: "stub_layer", Role: "system", Content: "stub"},
			}, nil
		},
	})

	layers, err := svc.BuildLayersForSource(context.Background(), "stub-source", BuildRequest{})
	if err != nil {
		t.Fatalf("build layers for source failed: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("unexpected layer count: %d", len(layers))
	}
	if layers[0].Name != "stub_layer" {
		t.Fatalf("unexpected layer: %+v", layers[0])
	}
}

func TestBuildLayersForSourceRejectsUnknownSource(t *testing.T) {
	t.Parallel()

	svc := NewService(Dependencies{})
	if _, err := svc.BuildLayersForSource(context.Background(), "missing", BuildRequest{}); err == nil {
		t.Fatal("expected unknown source to fail")
	}
}

func TestNormalizeRelativePathRejectsTraversal(t *testing.T) {
	t.Parallel()

	if _, ok := NormalizeRelativePath("../docs/AI/AGENTS.md"); ok {
		t.Fatal("expected traversal path to be rejected")
	}
	if normalized, ok := NormalizeRelativePath("docs/AI/AGENTS.md"); !ok || normalized != "docs/AI/AGENTS.md" {
		t.Fatalf("unexpected normalized path: %q, ok=%v", normalized, ok)
	}
}

func TestEstimateTokenCountSupportsMixedText(t *testing.T) {
	t.Parallel()

	count := EstimateTokenCount("你好 world test")
	if count <= 0 {
		t.Fatalf("expected positive token count, got %d", count)
	}
}

type stubSource struct {
	name  string
	build func(ctx context.Context, req BuildRequest) ([]Layer, error)
}

func (s stubSource) Name() string {
	return s.name
}

func (s stubSource) Build(ctx context.Context, req BuildRequest) ([]Layer, error) {
	if s.build == nil {
		return nil, errors.New("build not implemented")
	}
	return s.build(ctx, req)
}

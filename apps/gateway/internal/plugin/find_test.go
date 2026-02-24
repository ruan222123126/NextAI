package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	systempromptservice "nextai/apps/gateway/internal/service/systemprompt"
)

func TestFindToolInvokeBasicMatch(t *testing.T) {
	t.Parallel()

	tool := NewFindTool()
	relPath := seedFindTestFile(t, "alpha\nBeta\nbeta\n")

	result, err := tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{
				Path:    relPath,
				Pattern: "beta",
			},
		},
	})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	typed, ok := result.Data.(findSingleResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", result.Data)
	}
	if typed.Count != 1 {
		t.Fatalf("count=%d want=1", typed.Count)
	}
	if len(typed.Matches) != 1 {
		t.Fatalf("matches len=%d want=1", len(typed.Matches))
	}
	if typed.Matches[0].Line != 3 {
		t.Fatalf("line=%d want=3", typed.Matches[0].Line)
	}
}

func TestFindToolInvokeIgnoreCase(t *testing.T) {
	t.Parallel()

	tool := NewFindTool()
	relPath := seedFindTestFile(t, "Alpha\nbeta\nBETA\n")

	result, err := tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{
				Path:       relPath,
				Pattern:    "beta",
				IgnoreCase: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	typed, ok := result.Data.(findSingleResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", result.Data)
	}
	if typed.Count != 2 {
		t.Fatalf("count=%d want=2", typed.Count)
	}
}

func TestFindToolInvokeRejectsEmptyPattern(t *testing.T) {
	t.Parallel()

	tool := NewFindTool()
	relPath := seedFindTestFile(t, "line\n")
	_, err := tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{
				Path:    relPath,
				Pattern: "   ",
			},
		},
	})
	if !errors.Is(err, ErrFindToolPatternMissing) {
		t.Fatalf("expected ErrFindToolPatternMissing, got=%v", err)
	}
}

func TestFindToolInvokeRejectsPathOutsideWorkspace(t *testing.T) {
	t.Parallel()

	tool := NewFindTool()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("line\n"), 0o644); err != nil {
		t.Fatalf("seed outside file failed: %v", err)
	}
	_, err := tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{
				Path:    outside,
				Pattern: "line",
			},
		},
	})
	if !errors.Is(err, ErrFindToolPathInvalid) {
		t.Fatalf("expected ErrFindToolPathInvalid, got=%v", err)
	}
}

func TestFindToolInvokeCapsReturnedMatches(t *testing.T) {
	t.Parallel()

	tool := NewFindTool()
	var builder strings.Builder
	for i := 1; i <= findToolMaxMatches+50; i++ {
		builder.WriteString(fmt.Sprintf("line-%03d needle\n", i))
	}
	relPath := seedFindTestFile(t, builder.String())

	result, err := tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{
				Path:    relPath,
				Pattern: "needle",
			},
		},
	})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	typed, ok := result.Data.(findSingleResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", result.Data)
	}
	if typed.Count != findToolMaxMatches+50 {
		t.Fatalf("count=%d want=%d", typed.Count, findToolMaxMatches+50)
	}
	if len(typed.Matches) != findToolMaxMatches {
		t.Fatalf("matches len=%d want=%d", len(typed.Matches), findToolMaxMatches)
	}
}

func seedFindTestFile(t *testing.T, content string) string {
	t.Helper()

	workspaceRoot, err := systempromptservice.FindWorkspaceRoot()
	if err != nil {
		t.Fatalf("find workspace root failed: %v", err)
	}
	dir, err := os.MkdirTemp(workspaceRoot, ".find-tool-test-*")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed file failed: %v", err)
	}
	rel, err := filepath.Rel(workspaceRoot, path)
	if err != nil {
		t.Fatalf("relative path failed: %v", err)
	}
	return filepath.ToSlash(rel)
}

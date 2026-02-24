package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceBackedHandlersAvoidDirectStoreAndRegistryAccess(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current file path")
	}
	baseDir := filepath.Dir(thisFile)
	targetFiles := []string{
		"server_admin_service.go",
		"server_agent_service.go",
		"server_cron.go",
		"server_cron_service.go",
		"server_model_service.go",
		"server_selfops_service.go",
		"server_workspace_service.go",
	}
	forbidden := []string{
		"s.store.Read(",
		"s.store.Write(",
		"s.channels[",
		"s.tools[",
		"httptest.NewRecorder(",
		"httptest.NewRequest(",
	}

	for _, name := range targetFiles {
		path := filepath.Join(baseDir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s failed: %v", name, err)
		}
		content := string(raw)
		for _, pattern := range forbidden {
			if strings.Contains(content, pattern) {
				t.Fatalf("%s should not contain %q", name, pattern)
			}
		}
	}
}

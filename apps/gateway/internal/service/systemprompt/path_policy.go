package systemprompt

import (
	"os"
	"path/filepath"
	"strings"
)

func NormalizeRelativePath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || filepath.IsAbs(trimmed) {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(trimmed))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", false
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return clean, true
}

func FindWorkspaceRoot() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	// Release bundles do not contain .git metadata. In that case, treat the
	// current working directory as the workspace root.
	return start, nil
}

package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	fileToolMaxReadBytes = 64 * 1024
)

var (
	ErrInvalidPath      = errors.New("file_tool_invalid_path")
	ErrForbiddenPath    = errors.New("file_tool_forbidden_path")
	ErrFileExists       = errors.New("file_tool_file_exists")
	ErrFileNotFound     = errors.New("file_tool_file_not_found")
	ErrFileContentType  = errors.New("file_tool_content_must_be_string")
	ErrFileModeInvalid  = errors.New("file_tool_mode_invalid")
	ErrFilePathRequired = errors.New("file_tool_path_required")
	ErrRepoRootNotFound = errors.New("file_tool_repo_root_not_found")
)

type filePathTarget struct {
	absolute string
	relative string
}

func resolveRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current := wd
	for {
		gitPath := filepath.Join(current, ".git")
		if info, statErr := os.Stat(gitPath); statErr == nil && info.IsDir() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", ErrRepoRootNotFound
		}
		current = parent
	}
}

func resolveFilePath(input map[string]interface{}) (filePathTarget, error) {
	raw := strings.TrimSpace(stringValue(input["path"]))
	if raw == "" {
		return filePathTarget{}, ErrFilePathRequired
	}
	if filepath.IsAbs(raw) {
		return filePathTarget{}, ErrForbiddenPath
	}

	cleaned := filepath.Clean(raw)
	if cleaned == "." || cleaned == "" {
		return filePathTarget{}, ErrInvalidPath
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return filePathTarget{}, ErrForbiddenPath
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return filePathTarget{}, err
	}

	absolute := filepath.Join(repoRoot, cleaned)
	rel, err := filepath.Rel(repoRoot, absolute)
	if err != nil {
		return filePathTarget{}, ErrInvalidPath
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return filePathTarget{}, ErrInvalidPath
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filePathTarget{}, ErrForbiddenPath
	}

	return filePathTarget{
		absolute: absolute,
		relative: filepath.ToSlash(rel),
	}, nil
}

func readToolContent(input map[string]interface{}) (string, error) {
	raw, ok := input["content"]
	if !ok || raw == nil {
		return "", ErrFileContentType
	}
	content, ok := raw.(string)
	if !ok {
		return "", ErrFileContentType
	}
	return content, nil
}

func readUpdateMode(input map[string]interface{}) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(stringValue(input["mode"])))
	if mode == "" {
		return "overwrite", nil
	}
	if mode != "overwrite" && mode != "append" {
		return "", ErrFileModeInvalid
	}
	return mode, nil
}

func truncateReadContent(raw []byte) (content string, truncated bool) {
	if len(raw) <= fileToolMaxReadBytes {
		return string(raw), false
	}
	return string(raw[:fileToolMaxReadBytes]), true
}

func buildReadText(path string, size int, content string, truncated bool) string {
	if !truncated {
		return content
	}
	return fmt.Sprintf("%s\n\n... (truncated, showing first %d bytes of %d bytes from %s)", content, fileToolMaxReadBytes, size, path)
}

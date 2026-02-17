package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type FileCreateTool struct{}

func NewFileCreateTool() *FileCreateTool {
	return &FileCreateTool{}
}

func (t *FileCreateTool) Name() string {
	return "create_file"
}

func (t *FileCreateTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	target, err := resolveFilePath(input)
	if err != nil {
		return nil, err
	}
	content, err := readToolContent(input)
	if err != nil {
		return nil, err
	}

	if _, statErr := os.Stat(target.absolute); statErr == nil {
		return nil, ErrFileExists
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(target.absolute), 0o755); mkdirErr != nil {
		return nil, mkdirErr
	}
	file, openErr := os.OpenFile(target.absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if openErr != nil {
		if errors.Is(openErr, os.ErrExist) {
			return nil, ErrFileExists
		}
		return nil, openErr
	}
	if _, writeErr := file.WriteString(content); writeErr != nil {
		_ = file.Close()
		return nil, writeErr
	}
	if closeErr := file.Close(); closeErr != nil {
		return nil, closeErr
	}
	return map[string]interface{}{
		"ok":   true,
		"path": target.relative,
		"size": len(content),
		"text": fmt.Sprintf("created %s (%d bytes)", target.relative, len(content)),
	}, nil
}

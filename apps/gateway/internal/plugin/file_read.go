package plugin

import (
	"errors"
	"os"
)

type FileReadTool struct{}

func NewFileReadTool() *FileReadTool {
	return &FileReadTool{}
}

func (t *FileReadTool) Name() string {
	return "read_file"
}

func (t *FileReadTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	target, err := resolveFilePath(input)
	if err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(target.absolute)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	content, truncated := truncateReadContent(payload)
	text := buildReadText(target.relative, len(payload), content, truncated)
	return map[string]interface{}{
		"ok":        true,
		"path":      target.relative,
		"size":      len(payload),
		"truncated": truncated,
		"content":   content,
		"text":      text,
	}, nil
}

package plugin

import (
	"errors"
	"fmt"
	"os"
)

type FileUpdateTool struct{}

func NewFileUpdateTool() *FileUpdateTool {
	return &FileUpdateTool{}
}

func (t *FileUpdateTool) Name() string {
	return "update_file"
}

func (t *FileUpdateTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	target, err := resolveFilePath(input)
	if err != nil {
		return nil, err
	}
	content, err := readToolContent(input)
	if err != nil {
		return nil, err
	}
	mode, err := readUpdateMode(input)
	if err != nil {
		return nil, err
	}

	if _, statErr := os.Stat(target.absolute); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil, ErrFileNotFound
		}
		return nil, statErr
	}

	switch mode {
	case "append":
		file, openErr := os.OpenFile(target.absolute, os.O_WRONLY|os.O_APPEND, 0)
		if openErr != nil {
			return nil, openErr
		}
		if _, writeErr := file.WriteString(content); writeErr != nil {
			_ = file.Close()
			return nil, writeErr
		}
		if closeErr := file.Close(); closeErr != nil {
			return nil, closeErr
		}
	default:
		file, openErr := os.OpenFile(target.absolute, os.O_WRONLY|os.O_TRUNC, 0)
		if openErr != nil {
			return nil, openErr
		}
		if _, writeErr := file.WriteString(content); writeErr != nil {
			_ = file.Close()
			return nil, writeErr
		}
		if closeErr := file.Close(); closeErr != nil {
			return nil, closeErr
		}
	}

	info, statErr := os.Stat(target.absolute)
	if statErr != nil {
		return nil, statErr
	}
	return map[string]interface{}{
		"ok":   true,
		"path": target.relative,
		"mode": mode,
		"size": info.Size(),
		"text": fmt.Sprintf("updated %s with mode=%s", target.relative, mode),
	}, nil
}

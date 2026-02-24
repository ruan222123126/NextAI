package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fileLinesToolMaxRange = 400

var (
	ErrFileLinesToolPathMissing    = errors.New("file_lines_tool_path_missing")
	ErrFileLinesToolPathInvalid    = errors.New("file_lines_tool_path_invalid")
	ErrFileLinesToolItemsInvalid   = errors.New("file_lines_tool_items_invalid")
	ErrFileLinesToolStartInvalid   = errors.New("file_lines_tool_start_invalid")
	ErrFileLinesToolEndInvalid     = errors.New("file_lines_tool_end_invalid")
	ErrFileLinesToolRangeInvalid   = errors.New("file_lines_tool_range_invalid")
	ErrFileLinesToolRangeTooLarge  = errors.New("file_lines_tool_range_too_large")
	ErrFileLinesToolContentMissing = errors.New("file_lines_tool_content_missing")
	ErrFileLinesToolOutOfRange     = errors.New("file_lines_tool_out_of_range")
	ErrFileLinesToolFileNotFound   = errors.New("file_lines_tool_file_not_found")
	ErrFileLinesToolFileRead       = errors.New("file_lines_tool_file_read_failed")
	ErrFileLinesToolFileWrite      = errors.New("file_lines_tool_file_write_failed")
)

type ViewFileLinesTool struct {
}

type viewFileLinesResult struct {
	OK         bool   `json:"ok"`
	Path       string `json:"path"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	TotalLines int    `json:"total_lines"`
	Content    string `json:"content"`
	Text       string `json:"text"`
}

type viewFileLinesBatchResult struct {
	OK      bool                  `json:"ok"`
	Count   int                   `json:"count"`
	Results []viewFileLinesResult `json:"results"`
	Text    string                `json:"text"`
}

type editFileLinesResult struct {
	OK              bool   `json:"ok"`
	Path            string `json:"path"`
	Start           int    `json:"start"`
	End             int    `json:"end"`
	ReplacedLines   int    `json:"replaced_lines"`
	InsertedLines   int    `json:"inserted_lines"`
	TotalLinesAfter int    `json:"total_lines_after"`
	Text            string `json:"text"`
}

type editFileLinesBatchResult struct {
	OK      bool                  `json:"ok"`
	Count   int                   `json:"count"`
	Results []editFileLinesResult `json:"results"`
	Text    string                `json:"text"`
}

func NewViewFileLinesTool(_ string) *ViewFileLinesTool {
	return &ViewFileLinesTool{}
}

func (t *ViewFileLinesTool) Name() string {
	return "view"
}

func (t *ViewFileLinesTool) Invoke(command ToolCommand) (ToolResult, error) {
	items, err := parseInvocationItems(command)
	if err != nil {
		return ToolResult{}, err
	}
	results := make([]viewFileLinesResult, 0, len(items))
	for _, item := range items {
		viewResult, viewErr := t.viewOne(item)
		if viewErr != nil {
			return ToolResult{}, viewErr
		}
		results = append(results, viewResult)
	}
	if len(results) == 1 {
		return NewToolResult(results[0]), nil
	}

	textBlocks := make([]string, 0, len(results))
	for _, item := range results {
		if text := strings.TrimSpace(item.Text); text != "" {
			textBlocks = append(textBlocks, text)
		}
	}
	return NewToolResult(viewFileLinesBatchResult{
		OK:      true,
		Count:   len(results),
		Results: results,
		Text:    strings.Join(textBlocks, "\n\n"),
	}), nil
}

type EditFileLinesTool struct {
}

func NewEditFileLinesTool(_ string) *EditFileLinesTool {
	return &EditFileLinesTool{}
}

func (t *EditFileLinesTool) Name() string {
	return "edit"
}

func (t *EditFileLinesTool) Invoke(command ToolCommand) (ToolResult, error) {
	items, err := parseInvocationItems(command)
	if err != nil {
		return ToolResult{}, err
	}
	results := make([]editFileLinesResult, 0, len(items))
	for _, item := range items {
		editResult, editErr := t.editOne(item)
		if editErr != nil {
			return ToolResult{}, editErr
		}
		results = append(results, editResult)
	}
	if len(results) == 1 {
		return NewToolResult(results[0]), nil
	}

	textBlocks := make([]string, 0, len(results))
	for _, item := range results {
		if text := strings.TrimSpace(item.Text); text != "" {
			textBlocks = append(textBlocks, text)
		}
	}
	return NewToolResult(editFileLinesBatchResult{
		OK:      true,
		Count:   len(results),
		Results: results,
		Text:    strings.Join(textBlocks, "\n"),
	}), nil
}

func (t *ViewFileLinesTool) viewOne(input ToolCommandItem) (viewFileLinesResult, error) {
	relPath, absPath, err := resolveFileLinesPath(input)
	if err != nil {
		return viewFileLinesResult{}, err
	}
	start, end, err := parseLineRange(input)
	if err != nil {
		return viewFileLinesResult{}, err
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return viewFileLinesResult{}, fmt.Errorf("%w: %s", ErrFileLinesToolFileNotFound, relPath)
		}
		return viewFileLinesResult{}, fmt.Errorf("%w: %v", ErrFileLinesToolFileRead, err)
	}
	lines, _ := splitFileLines(string(raw))
	total := len(lines)
	if total == 0 {
		return viewFileLinesResult{
			OK:         true,
			Path:       relPath,
			Start:      0,
			End:        0,
			TotalLines: 0,
			Content:    "",
			Text: fmt.Sprintf(
				"view %s [empty] (fallback from requested [%d-%d], total=0)",
				relPath,
				start,
				end,
			),
		}, nil
	}
	actualStart := start
	actualEnd := end
	fallbackToFull := false
	if start > total || end > total {
		actualStart = 1
		actualEnd = total
		fallbackToFull = true
	}

	selected := lines[actualStart-1 : actualEnd]
	content := strings.Join(selected, "\n")
	numbered := make([]string, 0, len(selected))
	for idx, line := range selected {
		lineNo := actualStart + idx
		numbered = append(numbered, fmt.Sprintf("%d: %s", lineNo, line))
	}
	text := fmt.Sprintf("view %s [%d-%d]\n%s", relPath, actualStart, actualEnd, strings.Join(numbered, "\n"))
	if fallbackToFull {
		text = fmt.Sprintf(
			"view %s [%d-%d] (fallback from requested [%d-%d], total=%d)\n%s",
			relPath,
			actualStart,
			actualEnd,
			start,
			end,
			total,
			strings.Join(numbered, "\n"),
		)
	}
	return viewFileLinesResult{
		OK:         true,
		Path:       relPath,
		Start:      actualStart,
		End:        actualEnd,
		TotalLines: total,
		Content:    content,
		Text:       text,
	}, nil
}

func (t *EditFileLinesTool) editOne(input ToolCommandItem) (editFileLinesResult, error) {
	relPath, absPath, err := resolveFileLinesPath(input)
	if err != nil {
		return editFileLinesResult{}, err
	}
	start, end, err := parseLineRange(input)
	if err != nil {
		return editFileLinesResult{}, err
	}
	if input.Content == nil {
		return editFileLinesResult{}, ErrFileLinesToolContentMissing
	}
	content := *input.Content

	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			raw = []byte{}
		} else {
			return editFileLinesResult{}, fmt.Errorf("%w: %v", ErrFileLinesToolFileRead, err)
		}
	}
	lines, hadTrailingNewline := splitFileLines(string(raw))
	total := len(lines)
	replLines, _ := splitFileLines(content)
	updatedLines := make([]string, 0, max(len(lines), len(replLines)))
	if total == 0 {
		updatedLines = append(updatedLines, replLines...)
	} else {
		if start > total || end > total {
			return editFileLinesResult{}, fmt.Errorf("%w: path=%s total=%d range=%d-%d", ErrFileLinesToolOutOfRange, relPath, total, start, end)
		}
		updatedLines = append(updatedLines, lines[:start-1]...)
		updatedLines = append(updatedLines, replLines...)
		updatedLines = append(updatedLines, lines[end:]...)
	}

	output := strings.Join(updatedLines, "\n")
	if hadTrailingNewline && len(updatedLines) > 0 {
		output += "\n"
	}

	perm := os.FileMode(0o644)
	if info, statErr := os.Stat(absPath); statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return editFileLinesResult{}, fmt.Errorf("%w: %v", ErrFileLinesToolFileWrite, err)
	}
	if err := os.WriteFile(absPath, []byte(output), perm); err != nil {
		return editFileLinesResult{}, fmt.Errorf("%w: %v", ErrFileLinesToolFileWrite, err)
	}

	changed := 0
	if total > 0 {
		changed = end - start + 1
	}
	text := fmt.Sprintf("edit %s [%d-%d] replaced %d line(s).", relPath, start, end, changed)
	return editFileLinesResult{
		OK:              true,
		Path:            relPath,
		Start:           start,
		End:             end,
		ReplacedLines:   changed,
		InsertedLines:   len(replLines),
		TotalLinesAfter: len(updatedLines),
		Text:            text,
	}, nil
}

func parseLineRange(input ToolCommandItem) (int, int, error) {
	start := firstPositive(input.Start, input.StartLine, input.Line, input.Lineno)
	if start < 1 {
		return 0, 0, ErrFileLinesToolStartInvalid
	}
	end := firstPositive(input.End, input.EndLine)
	if end < 1 {
		return 0, 0, ErrFileLinesToolEndInvalid
	}
	if start > end {
		return 0, 0, ErrFileLinesToolRangeInvalid
	}
	if end-start+1 > fileLinesToolMaxRange {
		return 0, 0, ErrFileLinesToolRangeTooLarge
	}
	return start, end, nil
}

func resolveFileLinesPath(input ToolCommandItem) (string, string, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return "", "", ErrFileLinesToolPathMissing
	}
	absPath, err := normalizeAbsolutePath(path)
	if err != nil {
		return "", "", err
	}
	return absPath, absPath, nil
}

func normalizeAbsolutePath(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", ErrFileLinesToolPathMissing
	}
	if !filepath.IsAbs(candidate) {
		return "", ErrFileLinesToolPathInvalid
	}
	return filepath.Clean(candidate), nil
}

func splitFileLines(content string) ([]string, bool) {
	if content == "" {
		return []string{}, false
	}
	lines := strings.Split(content, "\n")
	hadTrailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if hadTrailingNewline {
		lines = lines[:len(lines)-1]
	}
	return lines, hadTrailingNewline
}

func parseInvocationItems(command ToolCommand) ([]ToolCommandItem, error) {
	if len(command.Items) == 0 {
		return nil, ErrFileLinesToolItemsInvalid
	}
	out := make([]ToolCommandItem, 0, len(command.Items))
	for _, item := range command.Items {
		out = append(out, item)
	}
	return out, nil
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

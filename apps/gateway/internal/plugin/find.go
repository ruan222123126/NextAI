package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	systempromptservice "nextai/apps/gateway/internal/service/systemprompt"
)

const findToolMaxMatches = 200

var (
	ErrFindToolItemsInvalid   = errors.New("find_tool_items_invalid")
	ErrFindToolPathMissing    = errors.New("find_tool_path_missing")
	ErrFindToolPathInvalid    = errors.New("find_tool_path_invalid")
	ErrFindToolPatternMissing = errors.New("find_tool_pattern_missing")
	ErrFindToolFileNotFound   = errors.New("find_tool_file_not_found")
	ErrFindToolFileRead       = errors.New("find_tool_file_read_failed")
)

type FindTool struct{}

type findItem struct {
	Path       string
	Pattern    string
	IgnoreCase bool
}

func NewFindTool() *FindTool {
	return &FindTool{}
}

func (t *FindTool) Name() string {
	return "find"
}

func (t *FindTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	items, err := parseFindItems(input)
	if err != nil {
		return nil, err
	}
	workspaceRoot, err := systempromptservice.FindWorkspaceRoot()
	if err != nil {
		return nil, fmt.Errorf("find workspace root unavailable: %w", err)
	}

	results := make([]map[string]interface{}, 0, len(items))
	totalMatches := 0
	for _, item := range items {
		one, oneErr := findOne(workspaceRoot, item)
		if oneErr != nil {
			return nil, oneErr
		}
		if count, ok := one["count"].(int); ok {
			totalMatches += count
		}
		results = append(results, one)
	}

	if len(results) == 1 {
		return results[0], nil
	}

	texts := make([]string, 0, len(results))
	for _, item := range results {
		if text, ok := item["text"].(string); ok {
			texts = append(texts, text)
		}
	}
	return map[string]interface{}{
		"ok":            true,
		"count":         len(results),
		"total_matches": totalMatches,
		"results":       results,
		"text":          strings.Join(texts, "\n\n"),
	}, nil
}

func parseFindItems(input map[string]interface{}) ([]findItem, error) {
	rawItems, ok := input["items"]
	if !ok || rawItems == nil {
		return nil, ErrFindToolItemsInvalid
	}
	entries, ok := rawItems.([]interface{})
	if !ok || len(entries) == 0 {
		return nil, ErrFindToolItemsInvalid
	}

	out := make([]findItem, 0, len(entries))
	for _, item := range entries {
		entry, ok := item.(map[string]interface{})
		if !ok {
			return nil, ErrFindToolItemsInvalid
		}
		path := strings.TrimSpace(stringValue(entry["path"]))
		if path == "" {
			return nil, ErrFindToolPathMissing
		}
		pattern := stringValue(entry["pattern"])
		if strings.TrimSpace(pattern) == "" {
			return nil, ErrFindToolPatternMissing
		}
		out = append(out, findItem{
			Path:       path,
			Pattern:    pattern,
			IgnoreCase: parseFindIgnoreCase(entry["ignore_case"]),
		})
	}
	return out, nil
}

func findOne(workspaceRoot string, item findItem) (map[string]interface{}, error) {
	absPath, displayPath, err := resolveFindPath(workspaceRoot, item.Path)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFindToolFileNotFound, displayPath)
		}
		return nil, fmt.Errorf("%w: %v", ErrFindToolFileRead, err)
	}

	lines, _ := splitFileLines(string(raw))
	matches := make([]map[string]interface{}, 0, findToolMaxMatches)
	totalMatches := 0
	for idx, line := range lines {
		if !findLineMatches(line, item.Pattern, item.IgnoreCase) {
			continue
		}
		totalMatches++
		if len(matches) < findToolMaxMatches {
			matches = append(matches, map[string]interface{}{
				"line": idx + 1,
				"text": line,
			})
		}
	}

	return map[string]interface{}{
		"ok":          true,
		"path":        displayPath,
		"pattern":     item.Pattern,
		"ignore_case": item.IgnoreCase,
		"count":       totalMatches,
		"matches":     matches,
		"text":        formatFindSummaryText(displayPath, item.Pattern, item.IgnoreCase, totalMatches, matches),
	}, nil
}

func resolveFindPath(workspaceRoot, rawPath string) (string, string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return "", "", ErrFindToolPathInvalid
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", ErrFindToolPathInvalid
	}

	pathValue := strings.TrimSpace(rawPath)
	if pathValue == "" {
		return "", "", ErrFindToolPathMissing
	}
	var candidate string
	if filepath.IsAbs(pathValue) {
		candidate = filepath.Clean(pathValue)
	} else {
		candidate = filepath.Clean(filepath.Join(rootAbs, pathValue))
	}

	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", ErrFindToolPathInvalid
	}
	if !isPathWithinRoot(rootAbs, candidateAbs) {
		return "", "", ErrFindToolPathInvalid
	}

	displayPath := candidateAbs
	if rel, relErr := filepath.Rel(rootAbs, candidateAbs); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		displayPath = filepath.ToSlash(rel)
	}
	return candidateAbs, displayPath, nil
}

func isPathWithinRoot(rootAbs, candidateAbs string) bool {
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return false
	}
	normalized := filepath.ToSlash(rel)
	if normalized == "." {
		return true
	}
	return !strings.HasPrefix(normalized, "../") && normalized != ".."
}

func findLineMatches(line, pattern string, ignoreCase bool) bool {
	if !ignoreCase {
		return strings.Contains(line, pattern)
	}
	return strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
}

func parseFindIgnoreCase(raw interface{}) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	case float64:
		return value != 0
	case int:
		return value != 0
	case int64:
		return value != 0
	default:
		return false
	}
}

func formatFindSummaryText(path, pattern string, ignoreCase bool, totalMatches int, matches []map[string]interface{}) string {
	header := fmt.Sprintf("find %s pattern=%q matched %d line(s)", path, pattern, totalMatches)
	if ignoreCase {
		header += " (ignore_case=true)"
	}
	if totalMatches == 0 {
		return header
	}
	if totalMatches > len(matches) {
		header += fmt.Sprintf(" (showing first %d)", len(matches))
	}
	lines := make([]string, 0, len(matches)+1)
	lines = append(lines, header)
	for _, match := range matches {
		lineNo, _ := match["line"].(int)
		text, _ := match["text"].(string)
		lines = append(lines, fmt.Sprintf("%d: %s", lineNo, text))
	}
	return strings.Join(lines, "\n")
}

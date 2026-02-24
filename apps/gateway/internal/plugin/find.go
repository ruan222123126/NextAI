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

type findMatch struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

type findSingleResult struct {
	OK         bool        `json:"ok"`
	Path       string      `json:"path"`
	Pattern    string      `json:"pattern"`
	IgnoreCase bool        `json:"ignore_case"`
	Count      int         `json:"count"`
	Matches    []findMatch `json:"matches"`
	Text       string      `json:"text"`
}

type findBatchResult struct {
	OK           bool               `json:"ok"`
	Count        int                `json:"count"`
	TotalMatches int                `json:"total_matches"`
	Results      []findSingleResult `json:"results"`
	Text         string             `json:"text"`
}

func NewFindTool() *FindTool {
	return &FindTool{}
}

func (t *FindTool) Name() string {
	return "find"
}

func (t *FindTool) Invoke(command ToolCommand) (ToolResult, error) {
	items, err := parseFindItems(command)
	if err != nil {
		return ToolResult{}, err
	}
	workspaceRoot, err := systempromptservice.FindWorkspaceRoot()
	if err != nil {
		return ToolResult{}, fmt.Errorf("find workspace root unavailable: %w", err)
	}

	results := make([]findSingleResult, 0, len(items))
	totalMatches := 0
	for _, item := range items {
		one, oneErr := findOne(workspaceRoot, item)
		if oneErr != nil {
			return ToolResult{}, oneErr
		}
		totalMatches += one.Count
		results = append(results, one)
	}

	if len(results) == 1 {
		return NewToolResult(results[0]), nil
	}

	texts := make([]string, 0, len(results))
	for _, item := range results {
		if text := strings.TrimSpace(item.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return NewToolResult(findBatchResult{
		OK:           true,
		Count:        len(results),
		TotalMatches: totalMatches,
		Results:      results,
		Text:         strings.Join(texts, "\n\n"),
	}), nil
}

func parseFindItems(command ToolCommand) ([]findItem, error) {
	if len(command.Items) == 0 {
		return nil, ErrFindToolItemsInvalid
	}

	out := make([]findItem, 0, len(command.Items))
	for _, entry := range command.Items {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			return nil, ErrFindToolPathMissing
		}
		pattern := entry.Pattern
		if strings.TrimSpace(pattern) == "" {
			return nil, ErrFindToolPatternMissing
		}
		out = append(out, findItem{
			Path:       path,
			Pattern:    pattern,
			IgnoreCase: entry.IgnoreCase,
		})
	}
	return out, nil
}

func findOne(workspaceRoot string, item findItem) (findSingleResult, error) {
	absPath, displayPath, err := resolveFindPath(workspaceRoot, item.Path)
	if err != nil {
		return findSingleResult{}, err
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return findSingleResult{}, fmt.Errorf("%w: %s", ErrFindToolFileNotFound, displayPath)
		}
		return findSingleResult{}, fmt.Errorf("%w: %v", ErrFindToolFileRead, err)
	}

	lines, _ := splitFileLines(string(raw))
	matches := make([]findMatch, 0, findToolMaxMatches)
	totalMatches := 0
	for idx, line := range lines {
		if !findLineMatches(line, item.Pattern, item.IgnoreCase) {
			continue
		}
		totalMatches++
		if len(matches) < findToolMaxMatches {
			matches = append(matches, findMatch{
				Line: idx + 1,
				Text: line,
			})
		}
	}

	return findSingleResult{
		OK:         true,
		Path:       displayPath,
		Pattern:    item.Pattern,
		IgnoreCase: item.IgnoreCase,
		Count:      totalMatches,
		Matches:    matches,
		Text:       formatFindSummaryText(displayPath, item.Pattern, item.IgnoreCase, totalMatches, matches),
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

func formatFindSummaryText(path, pattern string, ignoreCase bool, totalMatches int, matches []findMatch) string {
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
		lines = append(lines, fmt.Sprintf("%d: %s", match.Line, match.Text))
	}
	return strings.Join(lines, "\n")
}

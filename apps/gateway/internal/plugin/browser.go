package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	browserToolDefaultTimeout = 120 * time.Second
	browserToolMaxTimeout     = 600 * time.Second
	browserToolMaxOutputBytes = 32 * 1024
)

var (
	ErrBrowserToolAgentDirMissing  = errors.New("browser_tool_agent_dir_missing")
	ErrBrowserToolAgentUnavailable = errors.New("browser_tool_agent_unavailable")
	ErrBrowserToolItemsInvalid     = errors.New("browser_tool_items_invalid")
	ErrBrowserToolTaskMissing      = errors.New("browser_tool_task_missing")
)

type browserToolRunFunc func(ctx context.Context, agentDir, task string, timeout time.Duration) (string, int, error)

type browserTaskItem struct {
	Task    string
	Timeout time.Duration
}

type browserInvocationResult struct {
	OK         bool   `json:"ok"`
	Task       string `json:"task"`
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	DurationMS int64  `json:"duration_ms"`
	RunID      string `json:"run_id,omitempty"`
	LogPath    string `json:"log_path,omitempty"`
	ShotsPath  string `json:"shots_path,omitempty"`
	Text       string `json:"text"`
}

type browserBatchResult struct {
	OK      bool                      `json:"ok"`
	Count   int                       `json:"count"`
	Results []browserInvocationResult `json:"results"`
	Text    string                    `json:"text"`
}

type BrowserTool struct {
	agentDir string
	runFn    browserToolRunFunc
}

func NewBrowserTool(agentDir string) (*BrowserTool, error) {
	resolved, err := resolveBrowserAgentDir(agentDir)
	if err != nil {
		return nil, err
	}
	return &BrowserTool{
		agentDir: resolved,
		runFn:    runBrowserToolCommand,
	}, nil
}

func resolveBrowserAgentDir(agentDir string) (string, error) {
	trimmed := strings.TrimSpace(agentDir)
	if trimmed == "" {
		return "", ErrBrowserToolAgentDirMissing
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBrowserToolAgentUnavailable, err)
	}
	info, err := os.Stat(absPath)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrBrowserToolAgentUnavailable, absPath)
	}
	entryPath := filepath.Join(absPath, "agent.js")
	if entryInfo, entryErr := os.Stat(entryPath); entryErr != nil || entryInfo.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrBrowserToolAgentUnavailable, entryPath)
	}
	return absPath, nil
}

func (t *BrowserTool) Name() string {
	return "browser"
}

func (t *BrowserTool) Invoke(command ToolCommand) (ToolResult, error) {
	items, err := parseBrowserItems(command)
	if err != nil {
		return ToolResult{}, err
	}

	results := make([]browserInvocationResult, 0, len(items))
	allOK := true
	for _, item := range items {
		one, oneErr := t.invokeOne(item)
		if oneErr != nil {
			return ToolResult{}, oneErr
		}
		if !one.OK {
			allOK = false
		}
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
	return NewToolResult(browserBatchResult{
		OK:      allOK,
		Count:   len(results),
		Results: results,
		Text:    strings.Join(texts, "\n\n"),
	}), nil
}

func (t *BrowserTool) invokeOne(item browserTaskItem) (browserInvocationResult, error) {
	startedAt := time.Now()
	output, exitCode, err := t.runFn(context.Background(), t.agentDir, item.Task, item.Timeout)
	ok := err == nil

	result := browserInvocationResult{
		OK:         ok,
		Task:       item.Task,
		ExitCode:   exitCode,
		Output:     output,
		DurationMS: time.Since(startedAt).Milliseconds(),
		Text:       formatBrowserToolText(item.Task, ok, exitCode, output),
	}

	meta := extractBrowserRunMeta(output)
	if runID := meta["run_id"]; runID != "" {
		result.RunID = runID
	}
	if logPath := meta["log"]; logPath != "" {
		result.LogPath = logPath
	}
	if shotsPath := meta["shots"]; shotsPath != "" {
		result.ShotsPath = shotsPath
	}
	return result, nil
}

func parseBrowserItems(command ToolCommand) ([]browserTaskItem, error) {
	if len(command.Items) == 0 {
		return nil, ErrBrowserToolItemsInvalid
	}

	out := make([]browserTaskItem, 0, len(command.Items))
	for _, item := range command.Items {
		task := strings.TrimSpace(item.Task)
		if task == "" {
			task = strings.TrimSpace(item.Query)
		}
		if task == "" {
			return nil, ErrBrowserToolTaskMissing
		}
		out = append(out, browserTaskItem{
			Task:    task,
			Timeout: parseBrowserTimeout(item.TimeoutSeconds),
		})
	}
	return out, nil
}

func parseBrowserTimeout(rawSeconds int) time.Duration {
	seconds := int64(browserToolDefaultTimeout / time.Second)
	if rawSeconds > 0 {
		seconds = int64(rawSeconds)
	}
	if seconds <= 0 {
		seconds = int64(browserToolDefaultTimeout / time.Second)
	}
	maxSeconds := int64(browserToolMaxTimeout / time.Second)
	if seconds > maxSeconds {
		seconds = maxSeconds
	}
	return time.Duration(seconds) * time.Second
}

func runBrowserToolCommand(ctx context.Context, agentDir, task string, timeout time.Duration) (string, int, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "node", "agent.js", task)
	cmd.Dir = agentDir
	outputBytes, err := cmd.CombinedOutput()
	output := truncateOutput(string(outputBytes), browserToolMaxOutputBytes)
	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			return output, 124, cmdCtx.Err()
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return output, exitErr.ExitCode(), err
		}
		return output, -1, err
	}
	return output, 0, nil
}

func extractBrowserRunMeta(output string) map[string]string {
	meta := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "run_id:"):
			meta["run_id"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "run_id:"))
		case strings.HasPrefix(trimmed, "log:"):
			meta["log"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "log:"))
		case strings.HasPrefix(trimmed, "shots:"):
			meta["shots"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "shots:"))
		}
	}
	return meta
}

func formatBrowserToolText(task string, ok bool, exitCode int, output string) string {
	trimmed := strings.TrimSpace(output)
	if ok {
		if trimmed == "" {
			return fmt.Sprintf("browser task %q completed with no output", task)
		}
		return fmt.Sprintf("browser task %q succeeded\n%s", task, trimmed)
	}
	if trimmed == "" {
		return fmt.Sprintf("browser task %q failed with exit code %d", task, exitCode)
	}
	return fmt.Sprintf("browser task %q failed with exit code %d\n%s", task, exitCode, trimmed)
}

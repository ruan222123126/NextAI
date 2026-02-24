package plugin

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	shellToolDefaultTimeout = 20 * time.Second
	shellToolMaxTimeout     = 120 * time.Second
	shellToolMaxOutputBytes = 16 * 1024
)

var (
	ErrShellToolCommandMissing      = errors.New("shell_tool_command_missing")
	ErrShellToolItemsInvalid        = errors.New("shell_tool_items_invalid")
	ErrShellToolExecutorUnavailable = errors.New("shell_tool_executor_unavailable")
)

type ShellTool struct{}

type shellSingleResult struct {
	OK       bool   `json:"ok"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Text     string `json:"text"`
}

type shellBatchResult struct {
	OK      bool                `json:"ok"`
	Count   int                 `json:"count"`
	Results []shellSingleResult `json:"results"`
	Text    string              `json:"text"`
}

func NewShellTool() *ShellTool {
	return &ShellTool{}
}

func (t *ShellTool) Name() string {
	return "shell"
}

func (t *ShellTool) Invoke(command ToolCommand) (ToolResult, error) {
	items, err := parseShellItems(command)
	if err != nil {
		return ToolResult{}, err
	}
	results := make([]shellSingleResult, 0, len(items))
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
	return NewToolResult(shellBatchResult{
		OK:      allOK,
		Count:   len(results),
		Results: results,
		Text:    strings.Join(texts, "\n"),
	}), nil
}

func (t *ShellTool) invokeOne(input ToolCommandItem) (shellSingleResult, error) {
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return shellSingleResult{}, ErrShellToolCommandMissing
	}

	timeout := parseShellTimeout(input.TimeoutSeconds)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	program, baseArgs, resolveErr := resolveShellExecutor(runtime.GOOS, exec.LookPath)
	if resolveErr != nil {
		return shellSingleResult{}, resolveErr
	}
	args := append(append([]string{}, baseArgs...), command)
	cmd := exec.CommandContext(ctx, program, args...)
	if cwd := strings.TrimSpace(input.Cwd); cwd != "" {
		cmd.Dir = cwd
	}

	outputBytes, err := cmd.CombinedOutput()
	output := truncateOutput(string(outputBytes), shellToolMaxOutputBytes)
	ok := err == nil
	exitCode := 0

	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr):
			exitCode = exitErr.ExitCode()
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			exitCode = 124
		default:
			exitCode = -1
		}
	}

	text := formatShellText(command, ok, exitCode, output)
	return shellSingleResult{
		OK:       ok,
		Command:  command,
		ExitCode: exitCode,
		Output:   output,
		Text:     text,
	}, nil
}

func parseShellItems(command ToolCommand) ([]ToolCommandItem, error) {
	if len(command.Items) == 0 {
		if command.legacyCommand || strings.TrimSpace(command.Command) != "" {
			return []ToolCommandItem{
				{
					Command:        command.Command,
					Cwd:            command.Cwd,
					TimeoutSeconds: command.TimeoutSeconds,
				},
			}, nil
		}
		return nil, ErrShellToolItemsInvalid
	}
	out := make([]ToolCommandItem, 0, len(command.Items))
	for _, item := range command.Items {
		out = append(out, item)
	}
	return out, nil
}

func parseShellTimeout(rawSeconds int) time.Duration {
	seconds := int64(shellToolDefaultTimeout / time.Second)
	if rawSeconds > 0 {
		seconds = int64(rawSeconds)
	}
	if seconds <= 0 {
		seconds = int64(shellToolDefaultTimeout / time.Second)
	}
	maxSeconds := int64(shellToolMaxTimeout / time.Second)
	if seconds > maxSeconds {
		seconds = maxSeconds
	}
	return time.Duration(seconds) * time.Second
}

func truncateOutput(raw string, maxBytes int) string {
	if maxBytes <= 0 || len(raw) <= maxBytes {
		return raw
	}
	return raw[:maxBytes] + "\n... (output truncated)"
}

func formatShellText(command string, ok bool, exitCode int, output string) string {
	trimmed := strings.TrimSpace(output)
	if ok {
		if trimmed == "" {
			return fmt.Sprintf("$ %s\n(command completed with no output)", command)
		}
		return fmt.Sprintf("$ %s\n%s", command, trimmed)
	}
	if trimmed == "" {
		return fmt.Sprintf("$ %s\n(command failed with exit code %d)", command, exitCode)
	}
	return fmt.Sprintf("$ %s\n(command failed with exit code %d)\n%s", command, exitCode, trimmed)
}

func resolveShellExecutor(goos string, lookPath func(file string) (string, error)) (string, []string, error) {
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		if hasExecutable(lookPath, "powershell", "powershell.exe") {
			return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command"}, nil
		}
		if hasExecutable(lookPath, "cmd", "cmd.exe") {
			return "cmd", []string{"/C"}, nil
		}
		return "", nil, ErrShellToolExecutorUnavailable
	}

	if hasExecutable(lookPath, "sh") {
		return "sh", []string{"-lc"}, nil
	}
	if hasExecutable(lookPath, "bash") {
		return "bash", []string{"-lc"}, nil
	}
	return "", nil, ErrShellToolExecutorUnavailable
}

func hasExecutable(lookPath func(file string) (string, error), candidates ...string) bool {
	for _, name := range candidates {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, err := lookPath(name); err == nil {
			return true
		}
	}
	return false
}

package plugin

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestResolveShellExecutorWindowsPrefersPowerShell(t *testing.T) {
	program, args, err := resolveShellExecutor("windows", fakeLookPath(map[string]bool{
		"powershell": true,
		"cmd":        true,
	}))
	if err != nil {
		t.Fatalf("resolve executor failed: %v", err)
	}
	if program != "powershell" {
		t.Fatalf("expected powershell, got=%q", program)
	}
	if len(args) != 3 || args[0] != "-NoProfile" || args[1] != "-NonInteractive" || args[2] != "-Command" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveShellExecutorWindowsFallsBackToCmd(t *testing.T) {
	program, args, err := resolveShellExecutor("windows", fakeLookPath(map[string]bool{
		"cmd": true,
	}))
	if err != nil {
		t.Fatalf("resolve executor failed: %v", err)
	}
	if program != "cmd" {
		t.Fatalf("expected cmd, got=%q", program)
	}
	if len(args) != 1 || args[0] != "/C" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveShellExecutorUnixUsesSh(t *testing.T) {
	program, args, err := resolveShellExecutor("linux", fakeLookPath(map[string]bool{
		"sh": true,
	}))
	if err != nil {
		t.Fatalf("resolve executor failed: %v", err)
	}
	if program != "sh" {
		t.Fatalf("expected sh, got=%q", program)
	}
	if len(args) != 1 || args[0] != "-lc" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveShellExecutorUnixFallsBackToBash(t *testing.T) {
	program, args, err := resolveShellExecutor("darwin", fakeLookPath(map[string]bool{
		"bash": true,
	}))
	if err != nil {
		t.Fatalf("resolve executor failed: %v", err)
	}
	if program != "bash" {
		t.Fatalf("expected bash, got=%q", program)
	}
	if len(args) != 1 || args[0] != "-lc" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveShellExecutorReturnsUnavailableWhenNoneFound(t *testing.T) {
	_, _, err := resolveShellExecutor("windows", fakeLookPath(map[string]bool{}))
	if !errors.Is(err, ErrShellToolExecutorUnavailable) {
		t.Fatalf("expected ErrShellToolExecutorUnavailable, got=%v", err)
	}
}

func TestParseShellItemsAcceptsLegacySingleCommandObject(t *testing.T) {
	items, err := parseShellItems(ToolCommand{
		Command: "pwd",
		Cwd:     "/tmp",
	})
	if err != nil {
		t.Fatalf("parse shell items failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got=%d", len(items))
	}
	if got := items[0].Command; got != "pwd" {
		t.Fatalf("unexpected command: %#v", got)
	}
	if got := items[0].Cwd; got != "/tmp" {
		t.Fatalf("unexpected cwd: %#v", got)
	}
}

func TestParseShellItemsRejectsMissingItemsAndLegacyCommand(t *testing.T) {
	_, err := parseShellItems(ToolCommand{
		Cwd: "/tmp",
	})
	if !errors.Is(err, ErrShellToolItemsInvalid) {
		t.Fatalf("expected ErrShellToolItemsInvalid, got=%v", err)
	}
}

func TestShellToolSupportsSessionExecAndWriteStdin(t *testing.T) {
	tool := NewShellTool()

	execResult, err := tool.Invoke(ToolCommand{
		Cmd:         "cat",
		TTY:         true,
		YieldTimeMS: 100,
	})
	if err != nil {
		t.Fatalf("exec invoke failed: %v", err)
	}
	execMap, err := execResult.ToMap()
	if err != nil {
		t.Fatalf("convert exec result failed: %v", err)
	}
	sessionID := intFromAny(execMap["session_id"])
	if sessionID <= 0 {
		t.Fatalf("expected session_id > 0, got=%#v", execMap["session_id"])
	}
	t.Cleanup(func() { tool.releaseSession(sessionID) })

	writeResult, err := tool.Invoke(ToolCommand{
		SessionID:   sessionID,
		Chars:       "hello\n",
		YieldTimeMS: 600,
	})
	if err != nil {
		t.Fatalf("write_stdin invoke failed: %v", err)
	}
	writeMap, err := writeResult.ToMap()
	if err != nil {
		t.Fatalf("convert write result failed: %v", err)
	}
	if got := strings.TrimSpace(stringFromAny(writeMap["output"])); got != "hello" {
		t.Fatalf("output=%q want=hello", got)
	}
	if got := intFromAny(writeMap["session_id"]); got != sessionID {
		t.Fatalf("session_id=%v want=%d", writeMap["session_id"], sessionID)
	}
}

func TestShellToolWriteStdinRejectsUnknownSession(t *testing.T) {
	tool := NewShellTool()
	_, err := tool.Invoke(ToolCommand{SessionID: 99999})
	if !errors.Is(err, ErrShellToolSessionNotFound) {
		t.Fatalf("expected ErrShellToolSessionNotFound, got=%v", err)
	}
}

func TestShellToolWriteStdinRejectsNonTTYInput(t *testing.T) {
	tool := NewShellTool()
	execResult, err := tool.Invoke(ToolCommand{
		Cmd:         `read line; printf "%s" "$line"`,
		TTY:         false,
		YieldTimeMS: 100,
	})
	if err != nil {
		t.Fatalf("exec invoke failed: %v", err)
	}
	execMap, err := execResult.ToMap()
	if err != nil {
		t.Fatalf("convert exec result failed: %v", err)
	}
	sessionID := intFromAny(execMap["session_id"])
	if sessionID <= 0 {
		t.Fatalf("expected session_id > 0, got=%#v", execMap["session_id"])
	}
	t.Cleanup(func() { tool.releaseSession(sessionID) })

	_, err = tool.Invoke(ToolCommand{
		SessionID: sessionID,
		Chars:     "hello\n",
	})
	if !errors.Is(err, ErrShellToolStdinUnsupported) {
		t.Fatalf("expected ErrShellToolStdinUnsupported, got=%v", err)
	}
}

func fakeLookPath(available map[string]bool) func(file string) (string, error) {
	return func(file string) (string, error) {
		if available[file] {
			return file, nil
		}
		return "", exec.ErrNotFound
	}
}

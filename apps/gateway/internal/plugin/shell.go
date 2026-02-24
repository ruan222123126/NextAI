package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	shellToolDefaultTimeout = 20 * time.Second
	shellToolMaxTimeout     = 120 * time.Second

	shellToolDefaultYield      = 10 * time.Second
	shellToolDefaultWriteYield = 250 * time.Millisecond
	shellToolMaxYield          = 60 * time.Second

	shellToolMaxOutputBytes = 16 * 1024
	shellToolMaxSessions    = 32
	shellToolSessionIdleTTL = 10 * time.Minute
)

var (
	ErrShellToolCommandMissing      = errors.New("shell_tool_command_missing")
	ErrShellToolItemsInvalid        = errors.New("shell_tool_items_invalid")
	ErrShellToolExecutorUnavailable = errors.New("shell_tool_executor_unavailable")
	ErrShellToolSessionIDInvalid    = errors.New("shell_tool_session_id_invalid")
	ErrShellToolSessionNotFound     = errors.New("shell_tool_session_not_found")
	ErrShellToolStdinUnsupported    = errors.New("shell_tool_stdin_unsupported")
	ErrShellToolSessionLimitReached = errors.New("shell_tool_session_limit_reached")
	ErrShellToolEscalationDenied    = errors.New("shell_tool_escalation_denied")
)

type shellMode string

const (
	shellModeLegacy     shellMode = ""
	shellModeExec       shellMode = "exec_command"
	shellModeWriteStdin shellMode = "write_stdin"
)

type ShellTool struct {
	mu            sync.Mutex
	sessions      map[int]*shellSession
	nextSessionID int
}

type shellSession struct {
	id      int
	command string
	tty     bool
	stdin   io.WriteCloser
	cmd     *exec.Cmd
	cancel  context.CancelFunc

	mu         sync.Mutex
	pending    []byte
	notify     chan struct{}
	done       chan struct{}
	exited     bool
	exitCode   int
	lastActive time.Time
}

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

type shellSessionResult struct {
	OK        bool   `json:"ok"`
	Command   string `json:"command,omitempty"`
	SessionID *int   `json:"session_id,omitempty"`
	ProcessID *int   `json:"process_id,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Output    string `json:"output"`
	Text      string `json:"text"`
}

type shellExecRequest struct {
	Command string
	Cwd     string
	Yield   time.Duration
	TTY     bool
}

type shellWriteRequest struct {
	SessionID int
	Input     string
	Yield     time.Duration
}

func NewShellTool() *ShellTool {
	return &ShellTool{
		sessions:      map[int]*shellSession{},
		nextSessionID: 1000,
	}
}

func (t *ShellTool) Name() string {
	return "shell"
}

func (t *ShellTool) Invoke(command ToolCommand) (ToolResult, error) {
	t.reapExpiredSessions()

	switch detectShellMode(command) {
	case shellModeExec:
		req, err := parseShellExecRequest(command)
		if err != nil {
			return ToolResult{}, err
		}
		result, err := t.invokeSessionExec(req)
		if err != nil {
			return ToolResult{}, err
		}
		return NewToolResult(result), nil
	case shellModeWriteStdin:
		req, err := parseShellWriteRequest(command)
		if err != nil {
			return ToolResult{}, err
		}
		result, err := t.invokeSessionWrite(req)
		if err != nil {
			return ToolResult{}, err
		}
		return NewToolResult(result), nil
	default:
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
}

func (t *ShellTool) invokeSessionExec(req shellExecRequest) (shellSessionResult, error) {
	session, err := t.startSession(req)
	if err != nil {
		return shellSessionResult{}, err
	}

	output := t.collectOutput(session, req.Yield)
	exited, exitCode := session.state()

	result := shellSessionResult{
		OK:      !exited || exitCode == 0,
		Command: req.Command,
		Output:  output,
	}
	if exited {
		result.ExitCode = &exitCode
		t.releaseSession(session.id)
	} else {
		sid := session.id
		result.SessionID = &sid
		result.ProcessID = &sid
	}
	result.Text = formatShellSessionExecText(req.Command, output, result.SessionID, result.ExitCode)
	return result, nil
}

func (t *ShellTool) invokeSessionWrite(req shellWriteRequest) (shellSessionResult, error) {
	session, ok := t.getSession(req.SessionID)
	if !ok {
		return shellSessionResult{}, ErrShellToolSessionNotFound
	}

	if req.Input != "" {
		if !session.tty {
			return shellSessionResult{}, ErrShellToolStdinUnsupported
		}
		if _, err := io.WriteString(session.stdin, req.Input); err != nil {
			return shellSessionResult{}, ErrShellToolSessionNotFound
		}
	}

	output := t.collectOutput(session, req.Yield)
	exited, exitCode := session.state()

	result := shellSessionResult{
		OK:      !exited || exitCode == 0,
		Command: session.command,
		Output:  output,
	}
	if exited {
		result.ExitCode = &exitCode
		t.releaseSession(session.id)
	} else {
		sid := session.id
		result.SessionID = &sid
		result.ProcessID = &sid
	}
	result.Text = formatShellSessionWriteText(req.SessionID, session.command, output, result.SessionID, result.ExitCode)
	return result, nil
}

func (t *ShellTool) startSession(req shellExecRequest) (*shellSession, error) {
	program, baseArgs, resolveErr := resolveShellExecutor(runtime.GOOS, exec.LookPath)
	if resolveErr != nil {
		return nil, resolveErr
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := append(append([]string{}, baseArgs...), req.Command)
	cmd := exec.CommandContext(ctx, program, args...)
	if cwd := strings.TrimSpace(req.Cwd); cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	now := time.Now()
	session := &shellSession{
		command:    req.Command,
		tty:        req.TTY,
		stdin:      stdin,
		cmd:        cmd,
		cancel:     cancel,
		notify:     make(chan struct{}, 1),
		done:       make(chan struct{}),
		lastActive: now,
	}

	t.mu.Lock()
	if len(t.sessions) >= shellToolMaxSessions {
		t.mu.Unlock()
		_ = stdin.Close()
		cancel()
		_ = cmd.Process.Kill()
		return nil, ErrShellToolSessionLimitReached
	}
	id := t.allocateSessionIDLocked()
	session.id = id
	t.sessions[id] = session
	t.mu.Unlock()

	go t.captureSessionOutput(session, stdout)
	go t.captureSessionOutput(session, stderr)
	go t.watchSessionExit(session)
	return session, nil
}

func (t *ShellTool) watchSessionExit(session *shellSession) {
	if session == nil || session.cmd == nil {
		return
	}
	err := session.cmd.Wait()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	session.markExited(exitCode)
}

func (t *ShellTool) captureSessionOutput(session *shellSession, reader io.Reader) {
	if session == nil || reader == nil {
		return
	}
	buffer := make([]byte, 4096)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			session.appendOutput(buffer[:count])
		}
		if err != nil {
			return
		}
	}
}

func (t *ShellTool) collectOutput(session *shellSession, waitFor time.Duration) string {
	if session == nil {
		return ""
	}
	deadline := time.Now().Add(waitFor)
	for {
		raw := session.drainOutput()
		if len(raw) > 0 {
			return truncateOutput(string(raw), shellToolMaxOutputBytes)
		}
		exited, _ := session.state()
		if exited {
			raw = session.drainOutput()
			if len(raw) > 0 {
				return truncateOutput(string(raw), shellToolMaxOutputBytes)
			}
			return ""
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ""
		}
		select {
		case <-session.notify:
		case <-session.done:
		case <-time.After(remaining):
			return ""
		}
	}
}

func (t *ShellTool) allocateSessionIDLocked() int {
	if t.nextSessionID < 1000 {
		t.nextSessionID = 1000
	}
	id := t.nextSessionID
	for {
		if _, exists := t.sessions[id]; !exists {
			t.nextSessionID = id + 1
			return id
		}
		id++
	}
}

func (t *ShellTool) getSession(id int) (*shellSession, bool) {
	t.mu.Lock()
	session, ok := t.sessions[id]
	t.mu.Unlock()
	if !ok || session == nil {
		return nil, false
	}
	session.touch()
	return session, true
}

func (t *ShellTool) releaseSession(id int) {
	var session *shellSession
	t.mu.Lock()
	session = t.sessions[id]
	delete(t.sessions, id)
	t.mu.Unlock()
	if session == nil {
		return
	}
	_ = session.stdin.Close()
	if session.cancel != nil {
		session.cancel()
	}
}

func (t *ShellTool) reapExpiredSessions() {
	now := time.Now()
	staleIDs := make([]int, 0)

	t.mu.Lock()
	for id, session := range t.sessions {
		if session == nil || session.expired(now, shellToolSessionIdleTTL) {
			staleIDs = append(staleIDs, id)
		}
	}
	t.mu.Unlock()

	for _, id := range staleIDs {
		t.releaseSession(id)
	}
}

func (s *shellSession) appendOutput(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	s.mu.Lock()
	s.pending = append(s.pending, chunk...)
	if len(s.pending) > shellToolMaxOutputBytes*4 {
		s.pending = append([]byte{}, s.pending[len(s.pending)-shellToolMaxOutputBytes*4:]...)
	}
	s.lastActive = time.Now()
	s.mu.Unlock()

	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *shellSession) drainOutput() []byte {
	s.mu.Lock()
	if len(s.pending) == 0 {
		s.mu.Unlock()
		return nil
	}
	out := append([]byte{}, s.pending...)
	s.pending = nil
	s.lastActive = time.Now()
	s.mu.Unlock()
	return out
}

func (s *shellSession) markExited(exitCode int) {
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return
	}
	s.exited = true
	s.exitCode = exitCode
	s.lastActive = time.Now()
	s.mu.Unlock()
	close(s.done)

	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *shellSession) state() (bool, int) {
	s.mu.Lock()
	exited := s.exited
	exitCode := s.exitCode
	s.mu.Unlock()
	return exited, exitCode
}

func (s *shellSession) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
}

func (s *shellSession) expired(now time.Time, ttl time.Duration) bool {
	s.mu.Lock()
	lastActive := s.lastActive
	s.mu.Unlock()
	if lastActive.IsZero() {
		return false
	}
	return now.Sub(lastActive) > ttl
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

func detectShellMode(command ToolCommand) shellMode {
	if mode := normalizeShellMode(command.ShellMode); mode != shellModeLegacy {
		return mode
	}

	if len(command.Items) > 0 {
		first := command.Items[0]
		if mode := normalizeShellMode(first.ShellMode); mode != shellModeLegacy {
			return mode
		}
		if shellWriteFieldsPresent(command, first) {
			return shellModeWriteStdin
		}
		if shellExecFieldsPresent(command, first) {
			return shellModeExec
		}
	}

	if shellWriteFieldsPresent(command, ToolCommandItem{}) {
		return shellModeWriteStdin
	}
	if shellExecFieldsPresent(command, ToolCommandItem{}) {
		return shellModeExec
	}
	return shellModeLegacy
}

func shellExecFieldsPresent(command ToolCommand, item ToolCommandItem) bool {
	if strings.TrimSpace(item.Cmd) != "" || strings.TrimSpace(command.Cmd) != "" {
		return true
	}
	if strings.TrimSpace(item.Workdir) != "" || strings.TrimSpace(command.Workdir) != "" {
		return true
	}
	if item.YieldTimeMS > 0 || command.YieldTimeMS > 0 {
		return true
	}
	if item.TTY || command.TTY {
		return true
	}
	return false
}

func shellWriteFieldsPresent(command ToolCommand, item ToolCommandItem) bool {
	if firstPositiveInt(item.SessionID, command.SessionID, item.ProcessID, command.ProcessID) > 0 {
		return true
	}
	if item.Chars != "" || command.Chars != "" {
		return true
	}
	return false
}

func parseShellExecRequest(command ToolCommand) (shellExecRequest, error) {
	if len(command.Items) > 1 {
		return shellExecRequest{}, ErrShellToolItemsInvalid
	}

	item := ToolCommandItem{}
	if len(command.Items) == 1 {
		item = command.Items[0]
	}

	cmdText := firstNonEmpty(item.Command, item.Cmd, command.Command, command.Cmd)
	if strings.TrimSpace(cmdText) == "" {
		return shellExecRequest{}, ErrShellToolCommandMissing
	}
	cwd := firstNonEmpty(item.Cwd, item.Workdir, command.Cwd, command.Workdir)
	yield := parseShellYieldDuration(
		firstPositiveInt(item.YieldTimeMS, command.YieldTimeMS),
		firstPositiveInt(item.TimeoutSeconds, command.TimeoutSeconds),
		shellToolDefaultYield,
	)
	return shellExecRequest{
		Command: strings.TrimSpace(cmdText),
		Cwd:     strings.TrimSpace(cwd),
		Yield:   yield,
		TTY:     item.TTY || command.TTY,
	}, nil
}

func parseShellWriteRequest(command ToolCommand) (shellWriteRequest, error) {
	if len(command.Items) > 1 {
		return shellWriteRequest{}, ErrShellToolItemsInvalid
	}

	item := ToolCommandItem{}
	if len(command.Items) == 1 {
		item = command.Items[0]
	}

	sessionID := firstPositiveInt(item.SessionID, command.SessionID, item.ProcessID, command.ProcessID)
	if sessionID <= 0 {
		return shellWriteRequest{}, ErrShellToolSessionIDInvalid
	}
	input := item.Chars
	if input == "" {
		input = command.Chars
	}
	yield := parseShellYieldDuration(
		firstPositiveInt(item.YieldTimeMS, command.YieldTimeMS),
		firstPositiveInt(item.TimeoutSeconds, command.TimeoutSeconds),
		shellToolDefaultWriteYield,
	)
	return shellWriteRequest{
		SessionID: sessionID,
		Input:     input,
		Yield:     yield,
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

func parseShellYieldDuration(rawMilliseconds int, rawSeconds int, defaultValue time.Duration) time.Duration {
	if rawMilliseconds > 0 {
		d := time.Duration(rawMilliseconds) * time.Millisecond
		if d > shellToolMaxYield {
			return shellToolMaxYield
		}
		return d
	}
	if rawSeconds > 0 {
		d := time.Duration(rawSeconds) * time.Second
		if d > shellToolMaxYield {
			return shellToolMaxYield
		}
		return d
	}
	if defaultValue <= 0 {
		return shellToolDefaultYield
	}
	if defaultValue > shellToolMaxYield {
		return shellToolMaxYield
	}
	return defaultValue
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

func formatShellSessionExecText(command string, output string, sessionID *int, exitCode *int) string {
	trimmed := strings.TrimSpace(output)
	if sessionID != nil {
		if trimmed == "" {
			return fmt.Sprintf("$ %s\n(session %d is running; no output yet)", command, *sessionID)
		}
		return fmt.Sprintf("$ %s\n(session %d is running)\n%s", command, *sessionID, trimmed)
	}
	if exitCode == nil {
		if trimmed == "" {
			return fmt.Sprintf("$ %s\n(command status is unknown)", command)
		}
		return fmt.Sprintf("$ %s\n%s", command, trimmed)
	}
	return formatShellText(command, *exitCode == 0, *exitCode, output)
}

func formatShellSessionWriteText(sessionID int, command string, output string, runningSessionID *int, exitCode *int) string {
	trimmed := strings.TrimSpace(output)
	if runningSessionID != nil {
		if trimmed == "" {
			return fmt.Sprintf("session %d is running (no output yet)", *runningSessionID)
		}
		return fmt.Sprintf("session %d output\n%s", *runningSessionID, trimmed)
	}
	if exitCode == nil {
		if trimmed == "" {
			return fmt.Sprintf("session %d status is unknown", sessionID)
		}
		return fmt.Sprintf("session %d output\n%s", sessionID, trimmed)
	}
	if trimmed == "" {
		if strings.TrimSpace(command) == "" {
			return fmt.Sprintf("session %d exited with code %d", sessionID, *exitCode)
		}
		return fmt.Sprintf("$ %s\n(command exited with code %d)", command, *exitCode)
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Sprintf("session %d exited with code %d\n%s", sessionID, *exitCode, trimmed)
	}
	return fmt.Sprintf("$ %s\n(command exited with code %d)\n%s", command, *exitCode, trimmed)
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

func normalizeShellMode(raw string) shellMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(shellModeExec):
		return shellModeExec
	case string(shellModeWriteStdin):
		return shellModeWriteStdin
	default:
		return shellModeLegacy
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

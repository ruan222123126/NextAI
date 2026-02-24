package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrToolCommandInvalid = errors.New("tool_command_invalid")

type ChannelPlugin interface {
	Name() string
	SendText(ctx context.Context, userID, sessionID, text string, cfg map[string]interface{}) error
}

type ToolPlugin interface {
	Name() string
	Invoke(command ToolCommand) (ToolResult, error)
}

type ToolCommand struct {
	Items          []ToolCommandItem `json:"items,omitempty"`
	Command        string            `json:"command,omitempty"`
	Cmd            string            `json:"cmd,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	Workdir        string            `json:"workdir,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	YieldTimeMS    int               `json:"yield_time_ms,omitempty"`
	TTY            bool              `json:"tty,omitempty"`
	SessionID      int               `json:"session_id,omitempty"`
	ProcessID      int               `json:"process_id,omitempty"`
	Chars          string            `json:"chars,omitempty"`
	ShellMode      string            `json:"_nextai_shell_mode,omitempty"`
	legacyCommand  bool              `json:"-"`
}

type ToolCommandItem struct {
	Path           string  `json:"path,omitempty"`
	URL            string  `json:"url,omitempty"`
	RefID          string  `json:"ref_id,omitempty"`
	Start          int     `json:"start,omitempty"`
	StartLine      int     `json:"start_line,omitempty"`
	End            int     `json:"end,omitempty"`
	EndLine        int     `json:"end_line,omitempty"`
	Lineno         int     `json:"lineno,omitempty"`
	Line           int     `json:"line,omitempty"`
	Content        *string `json:"content,omitempty"`
	Pattern        string  `json:"pattern,omitempty"`
	IgnoreCase     bool    `json:"ignore_case,omitempty"`
	Command        string  `json:"command,omitempty"`
	Cmd            string  `json:"cmd,omitempty"`
	Cwd            string  `json:"cwd,omitempty"`
	Workdir        string  `json:"workdir,omitempty"`
	TimeoutSeconds int     `json:"timeout_seconds,omitempty"`
	YieldTimeMS    int     `json:"yield_time_ms,omitempty"`
	TTY            bool    `json:"tty,omitempty"`
	SessionID      int     `json:"session_id,omitempty"`
	ProcessID      int     `json:"process_id,omitempty"`
	Chars          string  `json:"chars,omitempty"`
	ShellMode      string  `json:"_nextai_shell_mode,omitempty"`
	Query          string  `json:"query,omitempty"`
	Q              string  `json:"q,omitempty"`
	Provider       string  `json:"provider,omitempty"`
	Count          int     `json:"count,omitempty"`
	Task           string  `json:"task,omitempty"`
}

type ToolResult struct {
	Data interface{}
}

func NewToolResult(data interface{}) ToolResult {
	return ToolResult{Data: data}
}

func (r ToolResult) ToMap() (map[string]interface{}, error) {
	if r.Data == nil {
		return map[string]interface{}{}, nil
	}
	if m, ok := r.Data.(map[string]interface{}); ok {
		out := make(map[string]interface{}, len(m))
		for key, value := range m {
			out[key] = value
		}
		return out, nil
	}
	raw, err := json.Marshal(r.Data)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func CommandFromMap(input map[string]interface{}) (ToolCommand, error) {
	out := ToolCommand{}
	if input == nil {
		return out, nil
	}
	out.Command = stringFromAny(input["command"])
	out.Cmd = stringFromAny(input["cmd"])
	if _, ok := input["command"]; ok {
		out.legacyCommand = true
	}
	out.Cwd = stringFromAny(input["cwd"])
	out.Workdir = stringFromAny(input["workdir"])
	out.TimeoutSeconds = intFromAny(input["timeout_seconds"])
	out.YieldTimeMS = intFromAny(input["yield_time_ms"])
	out.TTY = boolFromAny(input["tty"])
	out.SessionID = intFromAny(input["session_id"])
	out.ProcessID = intFromAny(input["process_id"])
	out.Chars = stringFromAny(input["chars"])
	out.ShellMode = stringFromAny(input["_nextai_shell_mode"])

	rawItems, hasItems := input["items"]
	if !hasItems || rawItems == nil {
		return out, nil
	}
	switch entries := rawItems.(type) {
	case []interface{}:
		out.Items = make([]ToolCommandItem, 0, len(entries))
		for index, raw := range entries {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				return ToolCommand{}, fmt.Errorf("%w: items[%d] must be object", ErrToolCommandInvalid, index)
			}
			out.Items = append(out.Items, commandItemFromMap(entry))
		}
	case []map[string]interface{}:
		out.Items = make([]ToolCommandItem, 0, len(entries))
		for _, entry := range entries {
			out.Items = append(out.Items, commandItemFromMap(entry))
		}
	default:
		return ToolCommand{}, fmt.Errorf("%w: items must be array", ErrToolCommandInvalid)
	}
	return out, nil
}

func (c ToolCommand) ToMap() (map[string]interface{}, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func commandItemFromMap(entry map[string]interface{}) ToolCommandItem {
	out := ToolCommandItem{
		Path:           stringFromAny(entry["path"]),
		URL:            stringFromAny(entry["url"]),
		RefID:          stringFromAny(entry["ref_id"]),
		Start:          intFromAny(entry["start"]),
		StartLine:      intFromAny(entry["start_line"]),
		End:            intFromAny(entry["end"]),
		EndLine:        intFromAny(entry["end_line"]),
		Lineno:         intFromAny(entry["lineno"]),
		Line:           intFromAny(entry["line"]),
		Pattern:        stringFromAny(entry["pattern"]),
		IgnoreCase:     boolFromAny(entry["ignore_case"]),
		Command:        stringFromAny(entry["command"]),
		Cmd:            stringFromAny(entry["cmd"]),
		Cwd:            stringFromAny(entry["cwd"]),
		Workdir:        stringFromAny(entry["workdir"]),
		TimeoutSeconds: intFromAny(entry["timeout_seconds"]),
		YieldTimeMS:    intFromAny(entry["yield_time_ms"]),
		TTY:            boolFromAny(entry["tty"]),
		SessionID:      intFromAny(entry["session_id"]),
		ProcessID:      intFromAny(entry["process_id"]),
		Chars:          stringFromAny(entry["chars"]),
		ShellMode:      stringFromAny(entry["_nextai_shell_mode"]),
		Query:          stringFromAny(entry["query"]),
		Q:              stringFromAny(entry["q"]),
		Provider:       stringFromAny(entry["provider"]),
		Count:          intFromAny(entry["count"]),
		Task:           stringFromAny(entry["task"]),
	}
	if rawContent, ok := entry["content"]; ok {
		if value, ok := rawContent.(string); ok {
			content := value
			out.Content = &content
		}
	}
	return out
}

func stringFromAny(v interface{}) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return ""
	}
}

func intFromAny(v interface{}) int {
	switch value := v.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return 0
}

func boolFromAny(v interface{}) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		trimmed := strings.TrimSpace(value)
		return strings.EqualFold(trimmed, "true") || trimmed == "1"
	case float64:
		return value != 0
	case int:
		return value != 0
	case int64:
		return value != 0
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return parsed != 0
		}
	}
	return false
}

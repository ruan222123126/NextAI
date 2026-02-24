package agentprotocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const (
	promptModeClaude = "claude"

	ToolCapabilityRead             = "read"
	ToolCapabilityWrite            = "write"
	ToolCapabilityExecute          = "execute"
	ToolCapabilityNetwork          = "network"
	ToolCapabilityOpenLocal        = "open_local"
	ToolCapabilityOpenURL          = "open_url"
	ToolCapabilityApproxClick      = "approx_click"
	ToolCapabilityApproxScreenshot = "approx_screenshot"
)

type QQInboundEvent struct {
	Text       string
	UserID     string
	SessionID  string
	TargetType string
	TargetID   string
	MessageID  string
}

type ToolCall struct {
	Name  string
	Input map[string]interface{}
}

func ResolveProcessRequestChannel(
	r *http.Request,
	requestedChannel string,
	qqInboundPath string,
	qqChannelName string,
	defaultProcessChannel string,
) string {
	if IsQQInboundRequest(r, qqInboundPath) {
		return qqChannelName
	}
	if requested := strings.ToLower(strings.TrimSpace(requestedChannel)); requested != "" {
		return requested
	}
	return defaultProcessChannel
}

func IsQQInboundRequest(r *http.Request, qqInboundPath string) bool {
	if r == nil || r.URL == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.URL.Path)), strings.ToLower(strings.TrimSpace(qqInboundPath)))
}

func ParseQQInboundEvent(body []byte) (QQInboundEvent, error) {
	raw := map[string]interface{}{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return QQInboundEvent{}, errors.New("invalid request body")
	}

	payload := raw
	if nested, ok := qqMap(raw["d"]); ok {
		payload = nested
	} else if nested, ok := qqMap(raw["data"]); ok {
		payload = nested
	}

	eventName := strings.ToUpper(qqFirst(
		qqString(raw["event"]),
		qqString(raw["event_type"]),
		qqString(raw["type"]),
		qqString(raw["t"]),
	))
	targetType, _ := normalizeQQTargetTypeAlias(strings.ToLower(eventName))
	if targetType == "" {
		targetType, _ = normalizeQQTargetTypeAlias(qqFirst(
			qqString(payload["message_type"]),
			qqString(payload["target_type"]),
		))
	}

	switch eventName {
	case "C2C_MESSAGE_CREATE":
		targetType = "c2c"
	case "GROUP_AT_MESSAGE_CREATE":
		targetType = "group"
	case "AT_MESSAGE_CREATE", "DIRECT_MESSAGE_CREATE":
		targetType = "guild"
	}
	if targetType == "" {
		return QQInboundEvent{}, errors.New("unsupported qq event type")
	}

	author, _ := qqMap(payload["author"])
	sender, _ := qqMap(payload["sender"])
	text := strings.TrimSpace(qqFirst(qqString(payload["content"]), qqString(payload["text"])))
	if text == "" {
		return QQInboundEvent{}, nil
	}

	event := QQInboundEvent{
		Text:      text,
		MessageID: strings.TrimSpace(qqString(payload["id"])),
	}

	switch targetType {
	case "c2c":
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["user_openid"]),
			qqString(author["id"]),
			qqString(sender["user_openid"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		targetID := strings.TrimSpace(qqFirst(
			qqString(payload["target_id"]),
			senderID,
		))
		if targetID == "" {
			return QQInboundEvent{}, errors.New("qq c2c event missing sender id")
		}
		userID := strings.TrimSpace(qqFirst(senderID, targetID))
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:c2c:%s", targetID),
		))
		event.UserID = userID
		event.SessionID = sessionID
		event.TargetType = "c2c"
		event.TargetID = targetID
	case "group":
		groupID := strings.TrimSpace(qqFirst(
			qqString(payload["group_openid"]),
			qqString(payload["target_id"]),
			qqString(payload["group_id"]),
		))
		if groupID == "" {
			return QQInboundEvent{}, errors.New("qq group event missing group_openid")
		}
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["member_openid"]),
			qqString(author["user_openid"]),
			qqString(author["id"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		if senderID == "" {
			senderID = groupID
		}
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:group:%s:%s", groupID, senderID),
		))
		event.UserID = senderID
		event.SessionID = sessionID
		event.TargetType = "group"
		event.TargetID = groupID
	case "guild":
		channelID := strings.TrimSpace(qqFirst(
			qqString(payload["channel_id"]),
			qqString(payload["target_id"]),
		))
		if channelID == "" {
			return QQInboundEvent{}, errors.New("qq guild event missing channel_id")
		}
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["id"]),
			qqString(author["username"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		if senderID == "" {
			senderID = channelID
		}
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:guild:%s:%s", channelID, senderID),
		))
		event.UserID = senderID
		event.SessionID = sessionID
		event.TargetType = "guild"
		event.TargetID = channelID
	}

	if event.UserID == "" || event.SessionID == "" || event.TargetID == "" {
		return QQInboundEvent{}, errors.New("qq inbound event missing required fields")
	}
	return event, nil
}

func MergeChannelDispatchConfig(channelName string, cfg map[string]interface{}, bizParams map[string]interface{}) map[string]interface{} {
	if channelName != "qq" || len(bizParams) == 0 {
		return cfg
	}
	raw, ok := bizParams["channel"]
	if !ok || raw == nil {
		return cfg
	}
	body, ok := raw.(map[string]interface{})
	if !ok {
		return cfg
	}
	merged := cloneChannelConfig(cfg)
	updated := false

	if canonical, ok := normalizeQQTargetTypeAlias(qqString(body["target_type"])); ok {
		merged["target_type"] = canonical
		updated = true
	}
	if targetID := strings.TrimSpace(qqString(body["target_id"])); targetID != "" {
		merged["target_id"] = targetID
		updated = true
	}
	if msgID := strings.TrimSpace(qqString(body["msg_id"])); msgID != "" {
		merged["msg_id"] = msgID
		updated = true
	}
	if botPrefix := qqString(body["bot_prefix"]); strings.TrimSpace(botPrefix) != "" {
		merged["bot_prefix"] = botPrefix
		updated = true
	}
	if !updated {
		return cfg
	}
	return merged
}

func CronChatMetaFromBizParams(bizParams map[string]interface{}) map[string]interface{} {
	if len(bizParams) == 0 {
		return nil
	}
	raw, ok := bizParams["cron"]
	if !ok || raw == nil {
		return nil
	}
	cronPayload, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	jobID := strings.TrimSpace(qqString(cronPayload["job_id"]))
	jobName := strings.TrimSpace(qqString(cronPayload["job_name"]))
	if jobID == "" && jobName == "" {
		return nil
	}
	meta := map[string]interface{}{
		"source": "cron",
	}
	if jobID != "" {
		meta["cron_job_id"] = jobID
	}
	if jobName != "" {
		meta["cron_job_name"] = jobName
	}
	return meta
}

func ParsePromptModeFromBizParams(
	bizParams map[string]interface{},
	metaKey string,
	defaultMode string,
	normalizePromptMode func(string) (string, bool),
) (string, bool, error) {
	if normalizePromptMode == nil {
		return defaultMode, false, nil
	}
	if len(bizParams) == 0 {
		return defaultMode, false, nil
	}
	rawPromptMode, hasPromptMode := bizParams[metaKey]
	if !hasPromptMode {
		return defaultMode, false, nil
	}
	value, ok := rawPromptMode.(string)
	if !ok {
		return "", true, errors.New("invalid prompt_mode")
	}
	mode, ok := normalizePromptMode(value)
	if !ok {
		return "", true, errors.New("invalid prompt_mode")
	}
	return mode, true, nil
}

func ResolvePromptModeFromChatMeta(
	meta map[string]interface{},
	metaKey string,
	defaultMode string,
	normalizePromptMode func(string) (string, bool),
) string {
	if normalizePromptMode == nil {
		return defaultMode
	}
	if len(meta) == 0 {
		return defaultMode
	}
	rawMode, ok := meta[metaKey]
	if !ok || rawMode == nil {
		return defaultMode
	}
	value, ok := rawMode.(string)
	if !ok {
		return defaultMode
	}
	mode, ok := normalizePromptMode(value)
	if !ok {
		return defaultMode
	}
	return mode
}

func ParseToolCall(bizParams map[string]interface{}, rawRequest map[string]interface{}, promptMode string) (ToolCall, bool, error) {
	if call, ok, err := ParseBizParamsToolCall(bizParams, promptMode); ok || err != nil {
		return call, ok, err
	}
	return ParseShortcutToolCall(rawRequest, promptMode)
}

func ParseBizParamsToolCall(bizParams map[string]interface{}, promptMode string) (ToolCall, bool, error) {
	if len(bizParams) == 0 {
		return ToolCall{}, false, nil
	}
	raw, ok := bizParams["tool"]
	if !ok || raw == nil {
		return ToolCall{}, false, nil
	}
	toolBody, ok := raw.(map[string]interface{})
	if !ok {
		return ToolCall{}, false, errors.New("biz_params.tool must be an object")
	}
	rawName, ok := toolBody["name"]
	if !ok {
		return ToolCall{}, false, errors.New("biz_params.tool.name is required")
	}
	name, ok := rawName.(string)
	if !ok {
		return ToolCall{}, false, errors.New("biz_params.tool.name must be a string")
	}
	name = NormalizeToolNameForPromptMode(strings.ToLower(strings.TrimSpace(name)), promptMode)
	if name == "" {
		return ToolCall{}, false, errors.New("biz_params.tool.name cannot be empty")
	}
	rawInput, hasInput := toolBody["input"]
	if !hasInput {
		body := map[string]interface{}{}
		for key, value := range toolBody {
			if key == "name" {
				continue
			}
			body[key] = value
		}
		rawInput = body
	}
	input, err := ParseToolPayload(rawInput, "biz_params.tool")
	if err != nil {
		return ToolCall{}, false, err
	}
	return ToolCall{Name: name, Input: input}, true, nil
}

func ParseShortcutToolCall(rawRequest map[string]interface{}, promptMode string) (ToolCall, bool, error) {
	if len(rawRequest) == 0 {
		return ToolCall{}, false, nil
	}
	shortcuts := []string{
		"view", "edit", "shell", "browser", "search", "open", "find", "click", "screenshot",
		"read", "write", "bash", "glob", "grep", "ls", "task", "todowrite", "exitplanmode",
		"websearch", "webfetch", "multiedit", "notebookread", "notebookedit",
		"Read", "Write", "Bash", "Glob", "Grep", "LS", "Task", "TodoWrite", "ExitPlanMode",
		"WebSearch", "WebFetch", "MultiEdit", "NotebookRead", "NotebookEdit", "Edit",
	}
	matched := make([]string, 0, 1)
	for _, key := range shortcuts {
		if raw, ok := rawRequest[key]; ok && raw != nil {
			matched = append(matched, key)
		}
	}
	if len(matched) == 0 {
		return ToolCall{}, false, nil
	}
	if len(matched) > 1 {
		return ToolCall{}, false, errors.New("only one shortcut tool key is allowed")
	}
	name := matched[0]
	input, err := ParseToolPayload(rawRequest[name], name)
	if err != nil {
		return ToolCall{}, false, err
	}
	return ToolCall{Name: NormalizeToolNameForPromptMode(name, promptMode), Input: input}, true, nil
}

func ParseToolPayload(raw interface{}, path string) (map[string]interface{}, error) {
	if raw == nil {
		return map[string]interface{}{}, nil
	}
	switch value := raw.(type) {
	case []interface{}:
		return map[string]interface{}{"items": value}, nil
	case map[string]interface{}:
		if nested, ok := value["input"]; ok {
			return ParseToolPayload(nested, path+".input")
		}
		return safeMap(value), nil
	default:
		return nil, fmt.Errorf("%s must be an object or array", path)
	}
}

func NormalizeToolName(name string) string {
	switch name {
	case "view_file_lines", "view_file_lins", "view_file":
		return "view"
	case "edit_file_lines", "edit_file_lins", "edit_file":
		return "edit"
	case "exec_command", "functions.exec_command":
		return "shell"
	case "web_browser", "browser_use", "browser_tool":
		return "browser"
	case "web_search", "search_api", "search_tool":
		return "search"
	case "open":
		return "open"
	case "find":
		return "find"
	case "click":
		return "click"
	case "screenshot":
		return "screenshot"
	case "selfops", "self_ops":
		return "self_ops"
	default:
		return name
	}
}

func NormalizeToolNameForPromptMode(name string, promptMode string) string {
	normalized := NormalizeToolName(strings.ToLower(strings.TrimSpace(name)))
	if !IsClaudePromptMode(promptMode) {
		return normalized
	}
	switch normalized {
	case "bash", "read", "write", "glob", "grep", "ls", "task", "todowrite", "exitplanmode",
		"websearch", "webfetch", "multiedit", "notebookread", "notebookedit":
		return normalized
	default:
		return normalized
	}
}

func IsClaudePromptMode(promptMode string) bool {
	return strings.EqualFold(strings.TrimSpace(promptMode), promptModeClaude)
}

func ListToolDefinitionNames(
	promptMode string,
	registeredToolNames []string,
	toolHasCapability func(name string, capability string) bool,
	isToolDisabled func(string) bool,
) []string {
	toolDisabled := func(name string) bool {
		if isToolDisabled == nil {
			return false
		}
		return isToolDisabled(name)
	}
	hasCapability := func(name string, capability string) bool {
		if toolHasCapability != nil {
			return toolHasCapability(name, capability)
		}
		switch capability {
		case ToolCapabilityOpenLocal:
			return name == "view"
		case ToolCapabilityOpenURL, ToolCapabilityApproxClick, ToolCapabilityApproxScreenshot:
			return name == "browser"
		default:
			return false
		}
	}

	if len(registeredToolNames) == 0 && toolDisabled("self_ops") {
		return nil
	}

	nameSet := map[string]struct{}{}
	for _, rawName := range registeredToolNames {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if name == "" {
			continue
		}
		if toolDisabled(name) {
			continue
		}
		nameSet[name] = struct{}{}
	}

	hasOpenLocal := false
	hasOpenURL := false
	hasApproxClick := false
	hasApproxScreenshot := false
	for name := range nameSet {
		if hasCapability(name, ToolCapabilityOpenLocal) {
			hasOpenLocal = true
		}
		if hasCapability(name, ToolCapabilityOpenURL) {
			hasOpenURL = true
		}
		if hasCapability(name, ToolCapabilityApproxClick) {
			hasApproxClick = true
		}
		if hasCapability(name, ToolCapabilityApproxScreenshot) {
			hasApproxScreenshot = true
		}
	}

	if (hasOpenLocal || hasOpenURL) && !toolDisabled("open") {
		nameSet["open"] = struct{}{}
	}
	if hasApproxClick && !toolDisabled("click") {
		nameSet["click"] = struct{}{}
	}
	if hasApproxScreenshot && !toolDisabled("screenshot") {
		nameSet["screenshot"] = struct{}{}
	}
	if !toolDisabled("self_ops") {
		nameSet["self_ops"] = struct{}{}
	}
	if IsClaudePromptMode(promptMode) {
		for _, name := range ClaudeCompatibleToolDefinitionNames() {
			nameSet[name] = struct{}{}
		}
	}

	out := make([]string, 0, len(nameSet))
	for name := range nameSet {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func ClaudeCompatibleToolDefinitionNames() []string {
	return []string{
		"Bash",
		"Edit",
		"ExitPlanMode",
		"Glob",
		"Grep",
		"LS",
		"MultiEdit",
		"NotebookEdit",
		"NotebookRead",
		"Read",
		"Task",
		"TodoWrite",
		"WebFetch",
		"WebSearch",
		"Write",
	}
}

func qqMap(raw interface{}) (map[string]interface{}, bool) {
	value, ok := raw.(map[string]interface{})
	return value, ok
}

func qqString(raw interface{}) string {
	value, _ := raw.(string)
	return value
}

func qqFirst(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeQQTargetTypeAlias(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "c2c", "private", "dm", "direct":
		return "c2c", true
	case "group", "group_at", "group_at_message_create":
		return "group", true
	case "guild", "guild_channel", "channel", "at_message_create", "direct_message_create":
		return "guild", true
	default:
		return "", false
	}
}

func cloneChannelConfig(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func safeMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

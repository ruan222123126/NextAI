package app

import (
	"context"
	"reflect"
	"strings"
	"time"
)

func hasAnyToolInputField(input map[string]interface{}, keys ...string) bool {
	if len(input) == 0 {
		return false
	}
	for _, key := range keys {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		value, exists := input[name]
		if !exists || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
		case []interface{}:
			if len(typed) == 0 {
				continue
			}
		case map[string]interface{}:
			if len(typed) == 0 {
				continue
			}
		}
		return true
	}
	return false
}

func multiAgentInputCandidates(input map[string]interface{}) []map[string]interface{} {
	candidates := make([]map[string]interface{}, 0, 2)
	if wrapped, ok := firstWrappedMultiAgentInputItem(input); ok {
		candidates = append(candidates, wrapped)
	}
	candidates = append(candidates, safeMap(input))
	return candidates
}

func firstWrappedMultiAgentInputItem(input map[string]interface{}) (map[string]interface{}, bool) {
	if input == nil {
		return map[string]interface{}{}, false
	}
	rawItems, exists := input["items"]
	if !exists || rawItems == nil {
		return map[string]interface{}{}, false
	}
	items, ok := rawItems.([]interface{})
	if !ok || len(items) == 0 {
		return map[string]interface{}{}, false
	}
	firstItem, ok := items[0].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}, false
	}
	if !looksLikeWrappedMultiAgentInputItem(firstItem) {
		return map[string]interface{}{}, false
	}
	return safeMap(firstItem), true
}

func looksLikeWrappedMultiAgentInputItem(item map[string]interface{}) bool {
	return hasAnyToolInputField(
		item,
		"id",
		"agent_id",
		"message",
		"input",
		"task",
		"prompt",
		"interrupt",
		"ids",
		"timeout_ms",
		"timeout",
		"yield_time_ms",
		"session_id",
		"user_id",
		"channel",
		"prompt_mode",
		"collaboration_mode",
		"agent_type",
	)
}

func firstNonEmptyStringFromCandidates(candidates []map[string]interface{}, keys ...string) string {
	for _, candidate := range candidates {
		value := strings.TrimSpace(firstNonEmptyString(candidate, keys...))
		if value != "" {
			return value
		}
	}
	return ""
}

func firstPositiveIntFromCandidates(candidates []map[string]interface{}, keys ...string) (int, bool) {
	for _, candidate := range candidates {
		if value, ok := firstPositiveInt(candidate, keys...); ok {
			return value, true
		}
	}
	return 0, false
}

func firstRawValueFromCandidates(candidates []map[string]interface{}, key string) (interface{}, bool) {
	name := strings.TrimSpace(key)
	if name == "" {
		return nil, false
	}
	for _, candidate := range candidates {
		if raw, exists := candidate[name]; exists && raw != nil {
			return raw, true
		}
	}
	return nil, false
}

func parseSubAgentBoolAny(raw interface{}) (bool, bool) {
	switch value := raw.(type) {
	case bool:
		return value, true
	case string:
		trimmed := strings.ToLower(strings.TrimSpace(value))
		switch trimmed {
		case "true", "1", "yes", "y", "on":
			return true, true
		case "false", "0", "no", "n", "off":
			return false, true
		}
	}
	return false, false
}

func parseSubAgentTargetID(input map[string]interface{}) string {
	candidates := multiAgentInputCandidates(input)
	return strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, "id", "agent_id"))
}

func parseSubAgentTargetIDs(input map[string]interface{}) ([]string, error) {
	candidates := multiAgentInputCandidates(input)
	if rawIDs, hasIDs := firstRawValueFromCandidates(candidates, "ids"); hasIDs {
		ids, ok := parseSubAgentIDList(rawIDs)
		if !ok {
			return nil, errMultiAgentIDsInvalid
		}
		return ids, nil
	}
	agentID := strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, "id", "agent_id"))
	if agentID == "" {
		return nil, errMultiAgentIDRequired
	}
	return []string{agentID}, nil
}

func parseSubAgentIDList(raw interface{}) ([]string, bool) {
	normalized := []string{}
	seen := map[string]struct{}{}
	appendID := func(rawID interface{}) bool {
		id := strings.TrimSpace(stringValue(rawID))
		if id == "" {
			return false
		}
		if _, exists := seen[id]; exists {
			return true
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
		return true
	}
	switch value := raw.(type) {
	case []interface{}:
		if len(value) == 0 {
			return nil, false
		}
		for _, rawID := range value {
			if !appendID(rawID) {
				return nil, false
			}
		}
	case []string:
		if len(value) == 0 {
			return nil, false
		}
		for _, rawID := range value {
			if !appendID(rawID) {
				return nil, false
			}
		}
	case string:
		if !appendID(value) {
			return nil, false
		}
	default:
		return nil, false
	}
	if len(normalized) == 0 {
		return nil, false
	}
	return normalized, true
}

func parseSubAgentWaitTimeout(input map[string]interface{}) time.Duration {
	timeout := agentWaitDefaultTimeout
	candidates := multiAgentInputCandidates(input)
	if timeoutMS, ok := firstPositiveIntFromCandidates(candidates, "timeout_ms", "timeout", "yield_time_ms"); ok {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	if timeout > agentWaitMaxTimeout {
		timeout = agentWaitMaxTimeout
	}
	if timeout <= 0 {
		timeout = agentWaitDefaultTimeout
	}
	return timeout
}

func parseSubAgentInterrupt(input map[string]interface{}) bool {
	candidates := multiAgentInputCandidates(input)
	for _, candidate := range candidates {
		raw, exists := candidate["interrupt"]
		if !exists {
			continue
		}
		if interrupt, ok := parseSubAgentBoolAny(raw); ok {
			return interrupt
		}
	}
	return false
}

func parseSubAgentTurnInput(input map[string]interface{}, allowTask bool, required bool, requiredErr error) (string, error) {
	candidates := multiAgentInputCandidates(input)
	keys := []string{"message", "input", "prompt"}
	if allowTask {
		keys = append(keys, "task")
	}
	message := strings.TrimSpace(firstNonEmptyStringFromCandidates(candidates, keys...))

	rawItems, hasItems := firstRawValueFromCandidates(candidates, "items")
	if hasItems {
		itemsText, recognized, err := parseSubAgentCollabItemsInput(rawItems)
		if err != nil {
			return "", err
		}
		if recognized {
			if message != "" {
				return "", errMultiAgentInputConflict
			}
			return itemsText, nil
		}
	}
	if message != "" {
		return message, nil
	}
	if required {
		return "", requiredErr
	}
	return "", nil
}

func parseSubAgentCollabItemsInput(raw interface{}) (text string, recognized bool, err error) {
	items, isArray := raw.([]interface{})
	if !isArray {
		return "", false, nil
	}
	if len(items) == 0 {
		return "", true, errMultiAgentItemsInvalid
	}
	if firstItem, ok := items[0].(map[string]interface{}); ok && looksLikeWrappedMultiAgentInputItem(firstItem) {
		return "", false, nil
	}
	parts := make([]string, 0, len(items))
	for _, rawItem := range items {
		switch value := rawItem.(type) {
		case map[string]interface{}:
			itemText := strings.TrimSpace(renderSubAgentCollabItem(value))
			if itemText != "" {
				parts = append(parts, itemText)
			}
		case string:
			itemText := strings.TrimSpace(value)
			if itemText != "" {
				parts = append(parts, itemText)
			}
		default:
			return "", true, errMultiAgentItemsInvalid
		}
	}
	if len(parts) == 0 {
		return "", true, errMultiAgentItemsInvalid
	}
	return strings.Join(parts, "\n"), true, nil
}

func renderSubAgentCollabItem(item map[string]interface{}) string {
	if len(item) == 0 {
		return ""
	}
	itemType := strings.ToLower(strings.TrimSpace(stringValue(item["type"])))
	switch itemType {
	case "text":
		return strings.TrimSpace(firstNonEmptyString(item, "text"))
	case "image":
		imageURL := strings.TrimSpace(firstNonEmptyString(item, "image_url", "url"))
		if imageURL == "" {
			return "[image]"
		}
		return "[image:" + imageURL + "]"
	case "local_image":
		path := strings.TrimSpace(firstNonEmptyString(item, "path"))
		if path == "" {
			return "[local_image]"
		}
		return "[local_image:" + path + "]"
	case "skill":
		name := strings.TrimSpace(firstNonEmptyString(item, "name"))
		path := strings.TrimSpace(firstNonEmptyString(item, "path"))
		if name == "" && path == "" {
			return "[skill]"
		}
		if path == "" {
			return "[skill:$" + name + "]"
		}
		return "[skill:$" + name + "](" + path + ")"
	case "mention":
		name := strings.TrimSpace(firstNonEmptyString(item, "name"))
		path := strings.TrimSpace(firstNonEmptyString(item, "path"))
		if name == "" && path == "" {
			return "[mention]"
		}
		if path == "" {
			return "[mention:$" + name + "]"
		}
		return "[mention:$" + name + "](" + path + ")"
	default:
		if text := strings.TrimSpace(firstNonEmptyString(item, "text")); text != "" {
			return text
		}
		if path := strings.TrimSpace(firstNonEmptyString(item, "path")); path != "" {
			return path
		}
		if name := strings.TrimSpace(firstNonEmptyString(item, "name")); name != "" {
			return name
		}
		return ""
	}
}

func buildSubAgentWaitPayload(
	agentIDs []string,
	finalStatuses map[string]string,
	snapshots map[string]managedSubAgentSnapshot,
	timedOut bool,
) map[string]interface{} {
	normalizedStatus := map[string]string{}
	for id, status := range finalStatuses {
		normalizedID := strings.TrimSpace(id)
		if normalizedID == "" {
			continue
		}
		normalized := strings.TrimSpace(status)
		if normalized == "" {
			normalized = managedSubAgentStatusIdle
		}
		normalizedStatus[normalizedID] = normalized
	}
	payload := map[string]interface{}{
		"ok":        true,
		"ready":     !timedOut && len(normalizedStatus) > 0,
		"timed_out": timedOut,
		"ids":       append([]string{}, agentIDs...),
		"status":    normalizedStatus,
	}
	if len(agentIDs) == 1 {
		agentID := strings.TrimSpace(agentIDs[0])
		payload["id"] = agentID
		payload["agent_id"] = agentID
		if snapshot, exists := snapshots[agentID]; exists {
			payload["agent"] = snapshot
		}
	}
	return payload
}

func waitForAnySubAgentUpdate(ctx context.Context, waitChs []chan struct{}, deadline time.Time) (timedOut bool, err error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return true, nil
	}
	timer := time.NewTimer(remaining)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	cases := make([]reflect.SelectCase, 0, len(waitChs)+2)
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	})
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(timer.C),
	})
	for _, waitCh := range waitChs {
		if waitCh == nil {
			continue
		}
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(waitCh),
		})
	}

	index, _, recvOK := reflect.Select(cases)
	switch index {
	case 0:
		return false, ctx.Err()
	case 1:
		return true, nil
	default:
		if !recvOK {
			return false, nil
		}
		return false, nil
	}
}

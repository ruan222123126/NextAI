package app

import (
	"errors"
	"strings"
)

const (
	chatMetaCollaborationModeKey         = "collaboration_mode"
	chatMetaCollaborationLastEventKey    = "collaboration_last_event"
	chatMetaCollaborationEventSourceKey  = "collaboration_event_source"
	chatMetaCollaborationUpdatedAtKey    = "collaboration_updated_at"
	collaborationBizParamsRootKey        = "collaboration"
	collaborationBizParamsModeKey        = "collaboration_mode"
	collaborationBizParamsEventKey       = "collaboration_event"
	collaborationPayloadModeKey          = "mode"
	collaborationPayloadEventKey         = "event"
	collaborationEventSetDefault         = "set_default"
	collaborationEventSetPlan            = "set_plan"
	collaborationEventSetExecute         = "set_execute"
	collaborationEventSetPairProgramming = "set_pair_programming"
	collaborationEventSourceBizParams    = "biz_params"
)

var (
	errInvalidCollaborationMode       = errors.New("invalid collaboration_mode")
	errInvalidCollaborationEvent      = errors.New("invalid collaboration_event")
	errConflictingCollaborationEvent  = errors.New("conflicting collaboration_event")
	errCollaborationModeRequiresCodex = errors.New("collaboration mode is only supported when prompt_mode=codex")
)

type collaborationModeCapabilities struct {
	RequestUserInputEnabled bool
}

type collaborationModeTransition struct {
	Event  string
	Source string
}

func resolveCollaborationModeFromChatMeta(meta map[string]interface{}) string {
	if len(meta) == 0 {
		return collaborationModeDefaultName
	}
	return normalizeCollaborationModeName(stringValue(meta[chatMetaCollaborationModeKey]))
}

func parseCollaborationModeName(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case strings.ToLower(collaborationModeDefaultName), "default":
		return collaborationModeDefaultName, true
	case strings.ToLower(collaborationModePlanName), "plan":
		return collaborationModePlanName, true
	case strings.ToLower(collaborationModeExecuteName), "execute":
		return collaborationModeExecuteName, true
	case strings.ToLower(collaborationModePairProgrammingName), "pair_programming", "pair-programming", "pairprogramming":
		return collaborationModePairProgrammingName, true
	default:
		return "", false
	}
}

func normalizeCollaborationModeName(modeName string) string {
	normalized, ok := parseCollaborationModeName(modeName)
	if !ok {
		return collaborationModeDefaultName
	}
	return normalized
}

func parseCollaborationEventName(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case collaborationEventSetDefault, "default", "reset", "exit", "switch_default", "to_default":
		return collaborationEventSetDefault, true
	case collaborationEventSetPlan, "plan", "switch_plan", "to_plan":
		return collaborationEventSetPlan, true
	case collaborationEventSetExecute, "execute", "switch_execute", "to_execute":
		return collaborationEventSetExecute, true
	case collaborationEventSetPairProgramming, "pair_programming", "pair-programming", "pairprogramming", "switch_pair_programming", "to_pair_programming":
		return collaborationEventSetPairProgramming, true
	default:
		return "", false
	}
}

func collaborationEventForMode(mode string) string {
	switch normalizeCollaborationModeName(mode) {
	case collaborationModePlanName:
		return collaborationEventSetPlan
	case collaborationModeExecuteName:
		return collaborationEventSetExecute
	case collaborationModePairProgrammingName:
		return collaborationEventSetPairProgramming
	default:
		return collaborationEventSetDefault
	}
}

func applyCollaborationModeEvent(currentMode string, event string) string {
	mode := normalizeCollaborationModeName(currentMode)
	normalizedEvent, ok := parseCollaborationEventName(event)
	if !ok {
		return mode
	}
	switch normalizedEvent {
	case collaborationEventSetPlan:
		return collaborationModePlanName
	case collaborationEventSetExecute:
		return collaborationModeExecuteName
	case collaborationEventSetPairProgramming:
		return collaborationModePairProgrammingName
	default:
		return collaborationModeDefaultName
	}
}

func resolveTurnCollaborationMode(
	promptMode string,
	currentMode string,
	bizParams map[string]interface{},
) (string, collaborationModeTransition, error) {
	mode := normalizeCollaborationModeName(currentMode)
	transition := collaborationModeTransition{}

	event, hasExplicitEvent, err := parseCollaborationEventFromBizParams(bizParams)
	if err != nil {
		return mode, transition, err
	}

	if !strings.EqualFold(strings.TrimSpace(promptMode), promptModeCodex) {
		if hasExplicitEvent {
			return mode, transition, errCollaborationModeRequiresCodex
		}
		return mode, transition, nil
	}

	if hasExplicitEvent {
		transition.Event = event
		transition.Source = collaborationEventSourceBizParams
		return applyCollaborationModeEvent(mode, event), transition, nil
	}
	return mode, transition, nil
}

func parseCollaborationEventFromBizParams(bizParams map[string]interface{}) (string, bool, error) {
	if len(bizParams) == 0 {
		return "", false, nil
	}

	events := make([]string, 0, 4)
	appendEvent := func(raw interface{}) error {
		value, ok := raw.(string)
		if !ok {
			return errInvalidCollaborationEvent
		}
		normalized, ok := parseCollaborationEventName(value)
		if !ok {
			return errInvalidCollaborationEvent
		}
		events = append(events, normalized)
		return nil
	}
	appendMode := func(raw interface{}) error {
		value, ok := raw.(string)
		if !ok {
			return errInvalidCollaborationMode
		}
		normalizedMode, ok := parseCollaborationModeName(value)
		if !ok {
			return errInvalidCollaborationMode
		}
		events = append(events, collaborationEventForMode(normalizedMode))
		return nil
	}

	if raw, exists := bizParams[collaborationBizParamsEventKey]; exists {
		if err := appendEvent(raw); err != nil {
			return "", false, err
		}
	}
	if raw, exists := bizParams[collaborationBizParamsModeKey]; exists {
		if err := appendMode(raw); err != nil {
			return "", false, err
		}
	}
	if raw, exists := bizParams[collaborationBizParamsRootKey]; exists {
		payload, ok := raw.(map[string]interface{})
		if !ok {
			return "", false, errInvalidCollaborationEvent
		}
		if value, exists := payload[collaborationPayloadEventKey]; exists {
			if err := appendEvent(value); err != nil {
				return "", false, err
			}
		}
		if value, exists := payload[collaborationPayloadModeKey]; exists {
			if err := appendMode(value); err != nil {
				return "", false, err
			}
		}
	}

	if len(events) == 0 {
		return "", false, nil
	}
	resolved := events[0]
	for _, item := range events[1:] {
		if item != resolved {
			return "", false, errConflictingCollaborationEvent
		}
	}
	return resolved, true, nil
}

func collaborationModeCapabilitiesFor(mode string) collaborationModeCapabilities {
	switch normalizeCollaborationModeName(mode) {
	case collaborationModePlanName:
		return collaborationModeCapabilities{RequestUserInputEnabled: true}
	case collaborationModeExecuteName:
		return collaborationModeCapabilities{RequestUserInputEnabled: false}
	case collaborationModePairProgrammingName:
		return collaborationModeCapabilities{RequestUserInputEnabled: false}
	default:
		return collaborationModeCapabilities{RequestUserInputEnabled: false}
	}
}

func applyCollaborationModeToolConstraints(snapshot TurnRuntimeSnapshot) TurnRuntimeSnapshot {
	if !strings.EqualFold(snapshot.Mode.PromptMode, promptModeCodex) {
		return snapshot
	}
	capabilities := collaborationModeCapabilitiesFor(snapshot.Mode.CollaborationMode)
	if capabilities.RequestUserInputEnabled {
		return snapshot
	}
	filtered := make([]string, 0, len(snapshot.AvailableTools))
	for _, toolName := range snapshot.AvailableTools {
		if strings.EqualFold(strings.TrimSpace(toolName), "request_user_input") {
			continue
		}
		filtered = append(filtered, toolName)
	}
	snapshot.AvailableTools = normalizeTurnRuntimeToolNames(filtered)
	return snapshot
}

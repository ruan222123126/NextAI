package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
	systempromptservice "nextai/apps/gateway/internal/service/systemprompt"
)

const (
	chatMetaPlanModeEnabledKey          = "plan_mode_enabled"
	chatMetaPlanModeStateKey            = "plan_mode_state"
	chatMetaPlanSpecKey                 = "plan_spec"
	chatMetaClarifyAskedCountKey        = "clarify_asked_count"
	chatMetaClarifyMaxCountKey          = "clarify_max_count"
	chatMetaClarifyUnresolvedKey        = "clarify_unresolved"
	chatMetaPlanExecutionSessionIDKey   = "plan_execution_session_id"
	chatMetaPlanSourcePromptVersionKey  = "plan_source_prompt_version"
	chatMetaPlanGoalInputKey            = "plan_goal_input"
	chatMetaPlanClarifyAnswersKey       = "plan_clarify_answers"
	chatMetaPlanExecutionSourceChatID   = "plan_execution_source_chat_id"
	defaultPlanPromptVersionPlaceholder = "plan_prompt_placeholder"
	planClarifyMaxCountDefault          = 5
	planSystemPromptRelativePath        = "prompts/plan/system.md"
	planAIToolsPromptRelativePath       = "prompts/plan/ai-tools.md"

	planModeStateOff              = "off"
	planModeStatePlanningIntake   = "planning_intake"
	planModeStatePlanningClarify  = "planning_clarify"
	planModeStatePlanningReady    = "planning_ready"
	planModeStatePlanningRevising = "planning_revising"
	planModeStateExecuting        = "executing"
	planModeStateDone             = "done"
	planModeStateAborted          = "aborted"
)

var (
	errPlanModeChatNotFound          = errors.New("plan_mode_chat_not_found")
	errPlanModeDisabled              = errors.New("plan_mode_disabled")
	errPlanModeStateInvalid          = errors.New("plan_mode_state_invalid")
	errPlanModeToggleConfirmRequired = errors.New("plan_mode_toggle_confirmation_required")
	errPlanPromptUnconfigured        = errors.New("plan_prompt_unconfigured")
	errPlanSpecInvalid               = errors.New("invalid_plan_spec")
	errPlanSpecMissing               = errors.New("plan_spec_missing")
)

type planToggleRequest struct {
	ChatID  string `json:"chat_id"`
	Enabled bool   `json:"enabled"`
	Confirm bool   `json:"confirm,omitempty"`
}

type planCompileRequest struct {
	ChatID    string `json:"chat_id"`
	UserInput string `json:"user_input"`
}

type planClarifyAnswerRequest struct {
	ChatID  string                            `json:"chat_id"`
	Answers map[string]requestUserInputAnswer `json:"answers"`
}

type planReviseRequest struct {
	ChatID                  string `json:"chat_id"`
	NaturalLanguageFeedback string `json:"natural_language_feedback"`
}

type planExecuteRequest struct {
	ChatID string `json:"chat_id"`
}

type planExecuteResponse struct {
	ExecutionSessionID string `json:"execution_session_id"`
}

type planStateResponse struct {
	ChatID                  string                     `json:"chat_id"`
	PlanModeEnabled         bool                       `json:"plan_mode_enabled"`
	PlanModeState           string                     `json:"plan_mode_state"`
	PlanSpec                *domain.PlanSpec           `json:"plan_spec,omitempty"`
	ClarifyAskedCount       int                        `json:"clarify_asked_count"`
	ClarifyMaxCount         int                        `json:"clarify_max_count"`
	ClarifyUnresolved       []string                   `json:"clarify_unresolved"`
	PlanExecutionSessionID  string                     `json:"plan_execution_session_id,omitempty"`
	PlanSourcePromptVersion string                     `json:"plan_source_prompt_version,omitempty"`
	Questions               []requestUserInputQuestion `json:"questions,omitempty"`
}

type planModeSnapshot struct {
	Enabled             bool
	State               string
	Spec                *domain.PlanSpec
	ClarifyAskedCount   int
	ClarifyMaxCount     int
	ClarifyUnresolved   []string
	ExecutionSessionID  string
	SourcePromptVersion string
	GoalInput           string
	ClarifyAnswers      map[string][]string
}

func defaultPlanModeSnapshot() planModeSnapshot {
	return planModeSnapshot{
		Enabled:           false,
		State:             planModeStateOff,
		Spec:              nil,
		ClarifyAskedCount: 0,
		ClarifyMaxCount:   planClarifyMaxCountDefault,
		ClarifyUnresolved: []string{},
		ClarifyAnswers:    map[string][]string{},
	}
}

func normalizePlanModeState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case planModeStateOff:
		return planModeStateOff
	case planModeStatePlanningIntake:
		return planModeStatePlanningIntake
	case planModeStatePlanningClarify:
		return planModeStatePlanningClarify
	case planModeStatePlanningReady:
		return planModeStatePlanningReady
	case planModeStatePlanningRevising:
		return planModeStatePlanningRevising
	case planModeStateExecuting:
		return planModeStateExecuting
	case planModeStateDone:
		return planModeStateDone
	case planModeStateAborted:
		return planModeStateAborted
	default:
		return planModeStateOff
	}
}

func isPlanningPlanModeState(state string) bool {
	switch normalizePlanModeState(state) {
	case planModeStatePlanningIntake, planModeStatePlanningClarify, planModeStatePlanningReady, planModeStatePlanningRevising:
		return true
	default:
		return false
	}
}

func isPlanModeStateEnabled(state string) bool {
	return normalizePlanModeState(state) != planModeStateOff
}

func parsePlanModeSnapshot(meta map[string]interface{}) planModeSnapshot {
	snapshot := defaultPlanModeSnapshot()
	if len(meta) == 0 {
		return snapshot
	}
	snapshot.Enabled = parseBoolFromAny(meta[chatMetaPlanModeEnabledKey], false)
	snapshot.State = normalizePlanModeState(stringValue(meta[chatMetaPlanModeStateKey]))
	snapshot.Spec = parsePlanSpecFromMeta(meta[chatMetaPlanSpecKey])
	snapshot.ClarifyAskedCount = parseIntFromAny(meta[chatMetaClarifyAskedCountKey], 0)
	if snapshot.ClarifyAskedCount < 0 {
		snapshot.ClarifyAskedCount = 0
	}
	snapshot.ClarifyMaxCount = parseIntFromAny(meta[chatMetaClarifyMaxCountKey], planClarifyMaxCountDefault)
	if snapshot.ClarifyMaxCount <= 0 {
		snapshot.ClarifyMaxCount = planClarifyMaxCountDefault
	}
	if snapshot.ClarifyAskedCount > snapshot.ClarifyMaxCount {
		snapshot.ClarifyAskedCount = snapshot.ClarifyMaxCount
	}
	snapshot.ClarifyUnresolved = normalizeStringSlice(parseStringSliceFromAny(meta[chatMetaClarifyUnresolvedKey]))
	snapshot.ExecutionSessionID = strings.TrimSpace(stringValue(meta[chatMetaPlanExecutionSessionIDKey]))
	snapshot.SourcePromptVersion = strings.TrimSpace(stringValue(meta[chatMetaPlanSourcePromptVersionKey]))
	snapshot.GoalInput = strings.TrimSpace(stringValue(meta[chatMetaPlanGoalInputKey]))
	snapshot.ClarifyAnswers = parsePlanClarifyAnswers(meta[chatMetaPlanClarifyAnswersKey])

	if snapshot.Enabled && snapshot.State == planModeStateOff {
		snapshot.State = planModeStatePlanningIntake
	}
	if !snapshot.Enabled {
		snapshot.State = planModeStateOff
	}
	return snapshot
}

func applyPlanModeSnapshot(meta map[string]interface{}, snapshot planModeSnapshot) map[string]interface{} {
	if meta == nil {
		meta = map[string]interface{}{}
	}
	snapshot.State = normalizePlanModeState(snapshot.State)
	if !snapshot.Enabled {
		snapshot.State = planModeStateOff
	}
	if snapshot.ClarifyMaxCount <= 0 {
		snapshot.ClarifyMaxCount = planClarifyMaxCountDefault
	}
	if snapshot.ClarifyAskedCount < 0 {
		snapshot.ClarifyAskedCount = 0
	}
	if snapshot.ClarifyAskedCount > snapshot.ClarifyMaxCount {
		snapshot.ClarifyAskedCount = snapshot.ClarifyMaxCount
	}

	meta[chatMetaPlanModeEnabledKey] = snapshot.Enabled
	meta[chatMetaPlanModeStateKey] = snapshot.State
	meta[chatMetaClarifyAskedCountKey] = snapshot.ClarifyAskedCount
	meta[chatMetaClarifyMaxCountKey] = snapshot.ClarifyMaxCount
	meta[chatMetaClarifyUnresolvedKey] = normalizeStringSlice(snapshot.ClarifyUnresolved)
	meta[chatMetaPlanExecutionSessionIDKey] = strings.TrimSpace(snapshot.ExecutionSessionID)
	meta[chatMetaPlanSourcePromptVersionKey] = strings.TrimSpace(snapshot.SourcePromptVersion)
	meta[chatMetaPlanGoalInputKey] = strings.TrimSpace(snapshot.GoalInput)
	meta[chatMetaPlanClarifyAnswersKey] = planClarifyAnswersToMeta(snapshot.ClarifyAnswers)
	if snapshot.Spec != nil {
		meta[chatMetaPlanSpecKey] = planSpecToMeta(snapshot.Spec)
	} else {
		delete(meta, chatMetaPlanSpecKey)
	}
	return meta
}

func parsePlanClarifyAnswers(raw interface{}) map[string][]string {
	out := map[string][]string{}
	payload, ok := raw.(map[string]interface{})
	if !ok || len(payload) == 0 {
		return out
	}
	for key, value := range payload {
		id := strings.TrimSpace(key)
		if id == "" {
			continue
		}
		switch typed := value.(type) {
		case []interface{}:
			out[id] = normalizeStringSlice(parseStringSliceFromAny(typed))
		case []string:
			out[id] = normalizeStringSlice(typed)
		case map[string]interface{}:
			out[id] = normalizeStringSlice(parseStringSliceFromAny(typed["answers"]))
		}
	}
	return out
}

func mergePlanClarifyAnswers(
	base map[string][]string,
	incoming map[string]requestUserInputAnswer,
) map[string][]string {
	out := map[string][]string{}
	for key, answers := range base {
		id := strings.TrimSpace(key)
		if id == "" {
			continue
		}
		out[id] = normalizeStringSlice(answers)
	}
	for key, answer := range incoming {
		id := strings.TrimSpace(key)
		if id == "" {
			continue
		}
		out[id] = normalizeStringSlice(answer.Answers)
	}
	return out
}

func planClarifyAnswersToMeta(answers map[string][]string) map[string]interface{} {
	out := map[string]interface{}{}
	for key, values := range answers {
		id := strings.TrimSpace(key)
		if id == "" {
			continue
		}
		items := normalizeStringSlice(values)
		rows := make([]interface{}, 0, len(items))
		for _, item := range items {
			rows = append(rows, item)
		}
		out[id] = rows
	}
	return out
}

func planSpecToMeta(spec *domain.PlanSpec) map[string]interface{} {
	if spec == nil {
		return map[string]interface{}{}
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return map[string]interface{}{}
	}
	decoded := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return map[string]interface{}{}
	}
	return decoded
}

func parsePlanSpecFromMeta(raw interface{}) *domain.PlanSpec {
	if raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var spec domain.PlanSpec
	if err := json.Unmarshal(encoded, &spec); err != nil {
		return nil
	}
	spec = normalizePlanSpec(spec)
	return &spec
}

func clonePlanSpec(spec *domain.PlanSpec) *domain.PlanSpec {
	if spec == nil {
		return nil
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		cloned := *spec
		return &cloned
	}
	var cloned domain.PlanSpec
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		copied := *spec
		return &copied
	}
	cloned = normalizePlanSpec(cloned)
	return &cloned
}

func parseBoolFromAny(raw interface{}, fallback bool) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		switch normalized {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		default:
			return fallback
		}
	case float64:
		return value != 0
	case int:
		return value != 0
	default:
		return fallback
	}
}

func parseIntFromAny(raw interface{}, fallback int) int {
	switch value := raw.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

func parseStringSliceFromAny(raw interface{}) []string {
	switch value := raw.(type) {
	case []string:
		return append([]string{}, value...)
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(stringValue(item))
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return []string{}
	}
}

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (s *Server) resolvePlanPromptVersion() (string, error) {
	requiredFiles := []string{
		planSystemPromptRelativePath,
		planAIToolsPromptRelativePath,
	}
	root, err := findRepoRoot()
	if err != nil {
		return "", errPlanPromptUnconfigured
	}
	hasher := sha256.New()
	for _, rel := range requiredFiles {
		absPath := filepath.Join(root, filepath.FromSlash(rel))
		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return "", errPlanPromptUnconfigured
		}
		trimmed := strings.TrimSpace(string(content))
		if trimmed == "" {
			return "", errPlanPromptUnconfigured
		}
		_, _ = hasher.Write([]byte(rel))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(trimmed))
		_, _ = hasher.Write([]byte{0})
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if len(sum) < 12 {
		return "plan-" + sum, nil
	}
	return "plan-" + sum[:12], nil
}

func shouldInjectPlanSystemPromptLayers(promptMode string, snapshot planModeSnapshot) bool {
	normalizedPromptMode, ok := normalizePromptMode(promptMode)
	if !ok || normalizedPromptMode != promptModeDefault {
		return false
	}
	if !snapshot.Enabled {
		return false
	}
	return isPlanModeStateEnabled(snapshot.State)
}

func (s *Server) buildPlanSystemPromptLayers() ([]systemPromptLayer, error) {
	layerSpecs := []struct {
		name string
		path string
	}{
		{
			name: "plan_system_prompt_system",
			path: planSystemPromptRelativePath,
		},
		{
			name: "plan_ai_tools_prompt_system",
			path: planAIToolsPromptRelativePath,
		},
	}
	layers := make([]systemPromptLayer, 0, len(layerSpecs))
	for _, spec := range layerSpecs {
		source, content, err := loadRequiredSystemLayer([]string{spec.path})
		if err != nil {
			return nil, errPlanPromptUnconfigured
		}
		layers = append(layers, systemPromptLayer{
			Name:    spec.name,
			Role:    "system",
			Source:  source,
			Content: systempromptservice.FormatLayerSourceContent(source, content),
		})
	}
	return layers, nil
}

func detectPlanUnresolved(goal string, answers map[string][]string) []string {
	normalizedGoal := strings.ToLower(strings.TrimSpace(goal))
	unresolved := []string{}
	if len([]rune(strings.TrimSpace(goal))) < 6 {
		unresolved = append(unresolved, "goal_detail")
	}
	if len(normalizeStringSlice(answers["scope_in"])) == 0 &&
		!strings.Contains(normalizedGoal, "范围") &&
		!strings.Contains(normalizedGoal, "scope") {
		unresolved = append(unresolved, "scope_in")
	}
	if len(normalizeStringSlice(answers["constraints"])) == 0 &&
		!strings.Contains(normalizedGoal, "约束") &&
		!strings.Contains(normalizedGoal, "限制") &&
		!strings.Contains(normalizedGoal, "deadline") {
		unresolved = append(unresolved, "constraints")
	}
	if len(normalizeStringSlice(answers["acceptance_criteria"])) == 0 &&
		!strings.Contains(normalizedGoal, "验收") &&
		!strings.Contains(normalizedGoal, "成功标准") &&
		!strings.Contains(normalizedGoal, "test") {
		unresolved = append(unresolved, "acceptance_criteria")
	}
	return normalizeStringSlice(unresolved)
}

func buildPlanClarifyQuestions(unresolved []string, remaining int) []requestUserInputQuestion {
	if remaining <= 0 || len(unresolved) == 0 {
		return []requestUserInputQuestion{}
	}
	catalog := map[string]requestUserInputQuestion{
		"goal_detail": {
			ID:       "goal_detail",
			Header:   "目标细化",
			Question: "这次任务最核心的结果物是什么？",
			Options: []requestUserInputQuestionOption{
				{Label: "最小可交付 (Recommended)", Description: "先做最小闭环，缩短交付周期"},
				{Label: "完整交付", Description: "覆盖更多能力，周期会更长"},
			},
		},
		"scope_in": {
			ID:       "scope_in",
			Header:   "范围",
			Question: "本次计划要覆盖哪些模块/子系统？",
			Options: []requestUserInputQuestionOption{
				{Label: "核心路径 (Recommended)", Description: "先锁定主流程，避免范围蔓延"},
				{Label: "多模块并行", Description: "范围更大，联调复杂度上升"},
			},
		},
		"constraints": {
			ID:       "constraints",
			Header:   "约束",
			Question: "是否有明确工期、技术栈或安全合规限制？",
			Options: []requestUserInputQuestionOption{
				{Label: "工期优先 (Recommended)", Description: "优先可上线，后续再迭代优化"},
				{Label: "质量优先", Description: "优先完整质量保障，交付周期更长"},
			},
		},
		"acceptance_criteria": {
			ID:       "acceptance_criteria",
			Header:   "验收",
			Question: "你希望用什么标准来判断计划完成？",
			Options: []requestUserInputQuestionOption{
				{Label: "基础验收 (Recommended)", Description: "以核心功能和关键测试通过为主"},
				{Label: "严格验收", Description: "覆盖性能、稳定性和回归等更多维度"},
			},
		},
	}
	questions := make([]requestUserInputQuestion, 0, remaining)
	for _, key := range unresolved {
		question, ok := catalog[key]
		if !ok {
			continue
		}
		questions = append(questions, question)
		if len(questions) >= remaining {
			break
		}
	}
	return questions
}

func planAssumptionsFromUnresolved(unresolved []string) []string {
	out := make([]string, 0, len(unresolved))
	for _, key := range unresolved {
		switch key {
		case "goal_detail":
			out = append(out, "目标细节缺失，默认按最小可交付闭环推进")
		case "scope_in":
			out = append(out, "实施范围未完全明确，默认只覆盖核心主链路")
		case "constraints":
			out = append(out, "约束信息不完整，默认遵循现有架构并优先保证可交付")
		case "acceptance_criteria":
			out = append(out, "验收标准未完全明确，默认以核心功能可用与关键测试通过为验收")
		default:
			out = append(out, fmt.Sprintf("关键条件 %q 未明确，按保守假设处理", key))
		}
	}
	return normalizeStringSlice(out)
}

func firstPlanAnswer(answers map[string][]string, key string) string {
	values := normalizeStringSlice(answers[key])
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func appendIfNonEmpty(values []string, item string) []string {
	text := strings.TrimSpace(item)
	if text == "" {
		return values
	}
	return append(values, text)
}

func buildPlanSpecFromInputs(
	goal string,
	answers map[string][]string,
	assumptions []string,
	revision int,
) domain.PlanSpec {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		goal = "完成用户指定目标"
	}
	scopeIn := []string{"交付核心执行闭环"}
	scopeIn = appendIfNonEmpty(scopeIn, firstPlanAnswer(answers, "scope_in"))
	scopeOut := []string{"不在本次目标内的扩展需求", "非关键链路的大规模重构"}
	constraints := []string{"优先复用现有网关与前端架构", "先完成 contracts 与校验再落实现"}
	constraints = appendIfNonEmpty(constraints, firstPlanAnswer(answers, "constraints"))
	acceptance := []string{"核心功能可执行", "关键测试通过并可回归验证"}
	acceptance = appendIfNonEmpty(acceptance, firstPlanAnswer(answers, "acceptance_criteria"))
	assumptions = normalizeStringSlice(assumptions)

	tasks := []domain.PlanTask{
		{
			ID:          "plan-1",
			Title:       "梳理现状与边界",
			Description: "梳理现有实现、确认数据结构与状态机边界，输出变更清单。",
			DependsOn:   []string{},
			Status:      domain.PlanTaskStatusPending,
			Deliverables: []string{
				"现状分析记录",
				"范围确认说明",
			},
			Verification: []string{
				"关键路径可追溯",
			},
		},
		{
			ID:          "plan-2",
			Title:       "完善契约与业务编排",
			Description: "补齐接口契约、状态流转与校验逻辑，保证规划态和执行态切换清晰。",
			DependsOn:   []string{"plan-1"},
			Status:      domain.PlanTaskStatusPending,
			Deliverables: []string{
				"API 契约更新",
				"状态机与校验器实现",
			},
			Verification: []string{
				"状态迁移单元测试通过",
			},
		},
		{
			ID:          "plan-3",
			Title:       "实现执行桥接与前端交互",
			Description: "实现软重置执行会话、计划展示面板和补充/执行交互入口。",
			DependsOn:   []string{"plan-2"},
			Status:      domain.PlanTaskStatusPending,
			Deliverables: []string{
				"执行会话桥接逻辑",
				"前端计划面板与控制区",
			},
			Verification: []string{
				"执行会话不携带原规划历史",
				"前端关键交互可走通",
			},
		},
		{
			ID:          "plan-4",
			Title:       "回归验证与交付",
			Description: "补齐必要测试并回归，确认主链路可用后交付。",
			DependsOn:   []string{"plan-3"},
			Status:      domain.PlanTaskStatusPending,
			Deliverables: []string{
				"单元/集成测试结果",
				"交付说明",
			},
			Verification: []string{
				"go test / tsc / vitest 通过",
			},
		},
	}
	risks := []domain.PlanRisk{
		{
			ID:          "risk-1",
			Title:       "范围蔓延",
			Description: "需求未收敛导致任务持续扩张。",
			Mitigation:  "固定本期 scope_out，新增需求进入下一轮 revision。",
		},
		{
			ID:          "risk-2",
			Title:       "上下文割裂",
			Description: "规划会话与执行会话切换后可能遗漏关键约束。",
			Mitigation:  "执行种子消息强制注入计划结构与摘要。",
		},
	}
	summary := fmt.Sprintf(
		"目标：%s。按 %d 个任务分阶段推进，先契约与状态机，再执行桥接与前端交互，最后测试回归。",
		goal,
		len(tasks),
	)
	return domain.PlanSpec{
		Goal:                goal,
		ScopeIn:             normalizeStringSlice(scopeIn),
		ScopeOut:            normalizeStringSlice(scopeOut),
		Constraints:         normalizeStringSlice(constraints),
		Assumptions:         assumptions,
		Tasks:               tasks,
		AcceptanceCriteria:  normalizeStringSlice(acceptance),
		Risks:               risks,
		SummaryForExecution: strings.TrimSpace(summary),
		Revision:            revision,
		UpdatedAt:           nowISO(),
	}
}

func normalizePlanSpec(spec domain.PlanSpec) domain.PlanSpec {
	spec.Goal = strings.TrimSpace(spec.Goal)
	spec.ScopeIn = normalizeStringSlice(spec.ScopeIn)
	spec.ScopeOut = normalizeStringSlice(spec.ScopeOut)
	spec.Constraints = normalizeStringSlice(spec.Constraints)
	spec.Assumptions = normalizeStringSlice(spec.Assumptions)
	spec.AcceptanceCriteria = normalizeStringSlice(spec.AcceptanceCriteria)
	spec.SummaryForExecution = strings.TrimSpace(spec.SummaryForExecution)
	if spec.Revision < 0 {
		spec.Revision = 0
	}
	if strings.TrimSpace(spec.UpdatedAt) == "" {
		spec.UpdatedAt = nowISO()
	}
	for idx := range spec.Tasks {
		task := &spec.Tasks[idx]
		task.ID = strings.TrimSpace(task.ID)
		task.Title = strings.TrimSpace(task.Title)
		task.Description = strings.TrimSpace(task.Description)
		task.DependsOn = normalizeStringSlice(task.DependsOn)
		task.Deliverables = normalizeStringSlice(task.Deliverables)
		task.Verification = normalizeStringSlice(task.Verification)
		task.Status = domain.PlanTaskStatus(strings.ToLower(strings.TrimSpace(string(task.Status))))
	}
	for idx := range spec.Risks {
		risk := &spec.Risks[idx]
		risk.ID = strings.TrimSpace(risk.ID)
		risk.Title = strings.TrimSpace(risk.Title)
		risk.Description = strings.TrimSpace(risk.Description)
		risk.Mitigation = strings.TrimSpace(risk.Mitigation)
	}
	return spec
}

func validatePlanSpec(spec domain.PlanSpec) error {
	spec = normalizePlanSpec(spec)
	if strings.TrimSpace(spec.Goal) == "" {
		return fmt.Errorf("%w: goal is required", errPlanSpecInvalid)
	}
	if len(spec.Tasks) == 0 {
		return fmt.Errorf("%w: tasks are required", errPlanSpecInvalid)
	}
	if len(spec.AcceptanceCriteria) == 0 {
		return fmt.Errorf("%w: acceptance_criteria are required", errPlanSpecInvalid)
	}

	taskIDs := map[string]struct{}{}
	inProgressCount := 0
	for _, task := range spec.Tasks {
		if task.ID == "" {
			return fmt.Errorf("%w: tasks.id is required", errPlanSpecInvalid)
		}
		if _, exists := taskIDs[task.ID]; exists {
			return fmt.Errorf("%w: duplicate tasks.id=%s", errPlanSpecInvalid, task.ID)
		}
		taskIDs[task.ID] = struct{}{}
		switch task.Status {
		case domain.PlanTaskStatusPending, domain.PlanTaskStatusInProgress, domain.PlanTaskStatusCompleted, domain.PlanTaskStatusBlocked:
		default:
			return fmt.Errorf("%w: invalid task status=%s", errPlanSpecInvalid, task.Status)
		}
		if task.Status == domain.PlanTaskStatusInProgress {
			inProgressCount++
		}
	}
	if inProgressCount > 1 {
		return fmt.Errorf("%w: multiple in_progress tasks", errPlanSpecInvalid)
	}
	for _, task := range spec.Tasks {
		for _, dep := range task.DependsOn {
			if _, exists := taskIDs[dep]; !exists {
				return fmt.Errorf("%w: depends_on target missing: %s", errPlanSpecInvalid, dep)
			}
		}
	}
	if hasPlanTaskCycle(spec.Tasks) {
		return fmt.Errorf("%w: task dependency cycle detected", errPlanSpecInvalid)
	}
	return nil
}

func hasPlanTaskCycle(tasks []domain.PlanTask) bool {
	if len(tasks) == 0 {
		return false
	}
	graph := map[string][]string{}
	for _, task := range tasks {
		graph[task.ID] = append([]string{}, task.DependsOn...)
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var dfs func(string) bool
	dfs = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, dep := range graph[id] {
			if dfs(dep) {
				return true
			}
		}
		visiting[id] = false
		visited[id] = true
		return false
	}
	for id := range graph {
		if dfs(id) {
			return true
		}
	}
	return false
}

func repairInvalidPlanSpec(spec domain.PlanSpec) domain.PlanSpec {
	spec = normalizePlanSpec(spec)
	if strings.TrimSpace(spec.Goal) == "" {
		spec.Goal = "完成计划目标"
	}
	if len(spec.Tasks) == 0 {
		spec.Tasks = []domain.PlanTask{
			{
				ID:           "task-1",
				Title:        "补全计划任务",
				Description:  "根据目标补全最小任务闭环。",
				DependsOn:    []string{},
				Status:       domain.PlanTaskStatusPending,
				Deliverables: []string{"最小任务闭环"},
				Verification: []string{"可执行"},
			},
		}
	}
	if len(spec.AcceptanceCriteria) == 0 {
		spec.AcceptanceCriteria = []string{"关键目标达成", "核心验证通过"}
	}

	seen := map[string]int{}
	inProgressKept := false
	for idx := range spec.Tasks {
		task := &spec.Tasks[idx]
		if task.ID == "" {
			task.ID = fmt.Sprintf("task-%d", idx+1)
		}
		baseID := task.ID
		if count, exists := seen[baseID]; exists {
			count++
			seen[baseID] = count
			task.ID = fmt.Sprintf("%s-%d", baseID, count)
		} else {
			seen[baseID] = 1
		}
		if task.Title == "" {
			task.Title = task.ID
		}
		if task.Description == "" {
			task.Description = "待补充描述"
		}
		switch task.Status {
		case domain.PlanTaskStatusPending, domain.PlanTaskStatusInProgress, domain.PlanTaskStatusCompleted, domain.PlanTaskStatusBlocked:
		default:
			task.Status = domain.PlanTaskStatusPending
		}
		if task.Status == domain.PlanTaskStatusInProgress {
			if inProgressKept {
				task.Status = domain.PlanTaskStatusPending
			} else {
				inProgressKept = true
			}
		}
	}

	taskSet := map[string]struct{}{}
	for _, task := range spec.Tasks {
		taskSet[task.ID] = struct{}{}
	}
	for idx := range spec.Tasks {
		task := &spec.Tasks[idx]
		filtered := make([]string, 0, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if dep == task.ID {
				continue
			}
			if _, exists := taskSet[dep]; !exists {
				continue
			}
			filtered = append(filtered, dep)
		}
		task.DependsOn = normalizeStringSlice(filtered)
	}
	if hasPlanTaskCycle(spec.Tasks) {
		for idx := range spec.Tasks {
			spec.Tasks[idx].DependsOn = []string{}
		}
	}
	if strings.TrimSpace(spec.SummaryForExecution) == "" {
		spec.SummaryForExecution = fmt.Sprintf("目标：%s。共 %d 个任务。", spec.Goal, len(spec.Tasks))
	}
	spec.UpdatedAt = nowISO()
	return normalizePlanSpec(spec)
}

func (s *Server) validateAndRepairPlanSpec(spec domain.PlanSpec) (domain.PlanSpec, error) {
	spec = normalizePlanSpec(spec)
	if err := validatePlanSpec(spec); err == nil {
		return spec, nil
	}
	repaired := repairInvalidPlanSpec(spec)
	if err := validatePlanSpec(repaired); err != nil {
		return domain.PlanSpec{}, errPlanSpecInvalid
	}
	return repaired, nil
}

func (s *Server) compilePlanSnapshot(snapshot planModeSnapshot) (planModeSnapshot, []requestUserInputQuestion, error) {
	goal := strings.TrimSpace(snapshot.GoalInput)
	if goal == "" {
		return snapshot, nil, errPlanModeStateInvalid
	}
	unresolved := detectPlanUnresolved(goal, snapshot.ClarifyAnswers)
	snapshot.ClarifyUnresolved = unresolved
	remaining := snapshot.ClarifyMaxCount - snapshot.ClarifyAskedCount
	if remaining > 0 && len(unresolved) > 0 {
		snapshot.State = planModeStatePlanningClarify
		questions := buildPlanClarifyQuestions(unresolved, remaining)
		return snapshot, questions, nil
	}

	nextRevision := 1
	if snapshot.Spec != nil && snapshot.Spec.Revision > 0 {
		nextRevision = snapshot.Spec.Revision + 1
	}
	spec := buildPlanSpecFromInputs(
		goal,
		snapshot.ClarifyAnswers,
		planAssumptionsFromUnresolved(unresolved),
		nextRevision,
	)
	validated, err := s.validateAndRepairPlanSpec(spec)
	if err != nil {
		return snapshot, nil, err
	}
	snapshot.Spec = &validated
	snapshot.State = planModeStatePlanningReady
	return snapshot, []requestUserInputQuestion{}, nil
}

func revisePlanSpec(spec domain.PlanSpec, feedback string) domain.PlanSpec {
	spec = normalizePlanSpec(spec)
	text := strings.TrimSpace(feedback)
	if text != "" {
		spec.Constraints = append(spec.Constraints, "用户补充: "+text)
	}
	spec.Constraints = normalizeStringSlice(spec.Constraints)
	spec.UpdatedAt = nowISO()
	spec.Revision++
	if spec.Revision <= 0 {
		spec.Revision = 1
	}
	if strings.TrimSpace(spec.SummaryForExecution) == "" {
		spec.SummaryForExecution = fmt.Sprintf("目标：%s。共 %d 个任务。", spec.Goal, len(spec.Tasks))
	}
	return spec
}

func buildPlanExecutionSeedMessage(spec domain.PlanSpec) string {
	encoded, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		encoded = []byte("{}")
	}
	return strings.TrimSpace(
		"请切换到执行模式，并严格按以下最终计划执行。\n\n" +
			"【计划摘要】\n" + strings.TrimSpace(spec.SummaryForExecution) +
			"\n\n【计划结构化数据】\n" + string(encoded),
	)
}

func encodePlanStateResponse(chatID string, snapshot planModeSnapshot, questions []requestUserInputQuestion) planStateResponse {
	resp := planStateResponse{
		ChatID:                  strings.TrimSpace(chatID),
		PlanModeEnabled:         snapshot.Enabled,
		PlanModeState:           normalizePlanModeState(snapshot.State),
		PlanSpec:                clonePlanSpec(snapshot.Spec),
		ClarifyAskedCount:       snapshot.ClarifyAskedCount,
		ClarifyMaxCount:         snapshot.ClarifyMaxCount,
		ClarifyUnresolved:       normalizeStringSlice(snapshot.ClarifyUnresolved),
		PlanExecutionSessionID:  strings.TrimSpace(snapshot.ExecutionSessionID),
		PlanSourcePromptVersion: strings.TrimSpace(snapshot.SourcePromptVersion),
		Questions:               questions,
	}
	if resp.ClarifyUnresolved == nil {
		resp.ClarifyUnresolved = []string{}
	}
	if len(resp.Questions) == 0 {
		resp.Questions = nil
	}
	return resp
}

func (s *Server) togglePlanMode(w http.ResponseWriter, r *http.Request) {
	var req planToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	if req.ChatID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "chat_id is required", nil)
		return
	}

	promptVersion := defaultPlanPromptVersionPlaceholder
	if version, err := s.resolvePlanPromptVersion(); err == nil {
		promptVersion = version
	}

	var response planStateResponse
	err := s.store.Write(func(state *repo.State) error {
		chat, ok := state.Chats[req.ChatID]
		if !ok {
			return errPlanModeChatNotFound
		}
		snapshot := parsePlanModeSnapshot(chat.Meta)

		if req.Enabled {
			snapshot.Enabled = true
			snapshot.State = planModeStatePlanningIntake
			snapshot.Spec = nil
			snapshot.ClarifyAskedCount = 0
			snapshot.ClarifyMaxCount = planClarifyMaxCountDefault
			snapshot.ClarifyUnresolved = []string{}
			snapshot.GoalInput = ""
			snapshot.ClarifyAnswers = map[string][]string{}
			snapshot.ExecutionSessionID = ""
			snapshot.SourcePromptVersion = promptVersion
		} else {
			if isPlanningPlanModeState(snapshot.State) && !req.Confirm {
				return errPlanModeToggleConfirmRequired
			}
			snapshot.Enabled = false
			snapshot.State = planModeStateOff
			snapshot.ClarifyUnresolved = []string{}
			snapshot.GoalInput = ""
			snapshot.ClarifyAnswers = map[string][]string{}
		}

		chat.Meta = applyPlanModeSnapshot(chat.Meta, snapshot)
		chat.UpdatedAt = nowISO()
		state.Chats[req.ChatID] = chat
		response = encodePlanStateResponse(req.ChatID, snapshot, nil)
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errPlanModeChatNotFound):
			writeErr(w, http.StatusNotFound, "not_found", "chat not found", map[string]string{"chat_id": req.ChatID})
		case errors.Is(err, errPlanModeToggleConfirmRequired):
			writeErr(w, http.StatusConflict, "plan_toggle_confirmation_required", "disable plan mode requires confirmation", nil)
		default:
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) compilePlan(w http.ResponseWriter, r *http.Request) {
	var req planCompileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.UserInput = strings.TrimSpace(req.UserInput)
	if req.ChatID == "" || req.UserInput == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "chat_id and user_input are required", nil)
		return
	}
	promptVersion, promptErr := s.resolvePlanPromptVersion()
	if promptErr != nil {
		writeErr(w, http.StatusServiceUnavailable, "plan_prompt_unconfigured", "plan prompt is not configured", nil)
		return
	}

	var response planStateResponse
	err := s.store.Write(func(state *repo.State) error {
		chat, ok := state.Chats[req.ChatID]
		if !ok {
			return errPlanModeChatNotFound
		}
		snapshot := parsePlanModeSnapshot(chat.Meta)
		if !snapshot.Enabled {
			return errPlanModeDisabled
		}
		switch normalizePlanModeState(snapshot.State) {
		case planModeStatePlanningIntake, planModeStatePlanningClarify, planModeStatePlanningReady, planModeStatePlanningRevising:
		default:
			return errPlanModeStateInvalid
		}
		snapshot.GoalInput = req.UserInput
		snapshot.SourcePromptVersion = promptVersion

		updatedSnapshot, questions, compileErr := s.compilePlanSnapshot(snapshot)
		if compileErr != nil {
			return compileErr
		}
		chat.Meta = applyPlanModeSnapshot(chat.Meta, updatedSnapshot)
		chat.UpdatedAt = nowISO()
		state.Chats[req.ChatID] = chat
		response = encodePlanStateResponse(req.ChatID, updatedSnapshot, questions)
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errPlanModeChatNotFound):
			writeErr(w, http.StatusNotFound, "not_found", "chat not found", map[string]string{"chat_id": req.ChatID})
		case errors.Is(err, errPlanModeDisabled):
			writeErr(w, http.StatusBadRequest, "plan_mode_disabled", "plan mode is disabled for this chat", nil)
		case errors.Is(err, errPlanModeStateInvalid):
			writeErr(w, http.StatusBadRequest, "plan_state_invalid", "plan mode state does not allow compile", nil)
		case errors.Is(err, errPlanSpecInvalid):
			writeErr(w, http.StatusBadRequest, "invalid_plan_spec", "plan spec is invalid after repair", nil)
		default:
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) submitPlanClarifyAnswer(w http.ResponseWriter, r *http.Request) {
	var req planClarifyAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	if req.ChatID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "chat_id is required", nil)
		return
	}
	promptVersion, promptErr := s.resolvePlanPromptVersion()
	if promptErr != nil {
		writeErr(w, http.StatusServiceUnavailable, "plan_prompt_unconfigured", "plan prompt is not configured", nil)
		return
	}

	var response planStateResponse
	err := s.store.Write(func(state *repo.State) error {
		chat, ok := state.Chats[req.ChatID]
		if !ok {
			return errPlanModeChatNotFound
		}
		snapshot := parsePlanModeSnapshot(chat.Meta)
		if !snapshot.Enabled {
			return errPlanModeDisabled
		}
		if normalizePlanModeState(snapshot.State) != planModeStatePlanningClarify {
			return errPlanModeStateInvalid
		}
		snapshot.ClarifyAnswers = mergePlanClarifyAnswers(snapshot.ClarifyAnswers, req.Answers)
		if snapshot.ClarifyAskedCount < snapshot.ClarifyMaxCount {
			snapshot.ClarifyAskedCount++
		}
		snapshot.SourcePromptVersion = promptVersion

		updatedSnapshot, questions, compileErr := s.compilePlanSnapshot(snapshot)
		if compileErr != nil {
			return compileErr
		}
		chat.Meta = applyPlanModeSnapshot(chat.Meta, updatedSnapshot)
		chat.UpdatedAt = nowISO()
		state.Chats[req.ChatID] = chat
		response = encodePlanStateResponse(req.ChatID, updatedSnapshot, questions)
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errPlanModeChatNotFound):
			writeErr(w, http.StatusNotFound, "not_found", "chat not found", map[string]string{"chat_id": req.ChatID})
		case errors.Is(err, errPlanModeDisabled):
			writeErr(w, http.StatusBadRequest, "plan_mode_disabled", "plan mode is disabled for this chat", nil)
		case errors.Is(err, errPlanModeStateInvalid):
			writeErr(w, http.StatusBadRequest, "plan_state_invalid", "plan mode state does not allow clarify answer", nil)
		case errors.Is(err, errPlanSpecInvalid):
			writeErr(w, http.StatusBadRequest, "invalid_plan_spec", "plan spec is invalid after repair", nil)
		default:
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) revisePlan(w http.ResponseWriter, r *http.Request) {
	var req planReviseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.NaturalLanguageFeedback = strings.TrimSpace(req.NaturalLanguageFeedback)
	if req.ChatID == "" || req.NaturalLanguageFeedback == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "chat_id and natural_language_feedback are required", nil)
		return
	}
	promptVersion, promptErr := s.resolvePlanPromptVersion()
	if promptErr != nil {
		writeErr(w, http.StatusServiceUnavailable, "plan_prompt_unconfigured", "plan prompt is not configured", nil)
		return
	}

	var response planStateResponse
	err := s.store.Write(func(state *repo.State) error {
		chat, ok := state.Chats[req.ChatID]
		if !ok {
			return errPlanModeChatNotFound
		}
		snapshot := parsePlanModeSnapshot(chat.Meta)
		if !snapshot.Enabled {
			return errPlanModeDisabled
		}
		if normalizePlanModeState(snapshot.State) != planModeStatePlanningReady {
			return errPlanModeStateInvalid
		}
		if snapshot.Spec == nil {
			return errPlanSpecMissing
		}

		snapshot.State = planModeStatePlanningRevising
		revisedSpec := revisePlanSpec(*snapshot.Spec, req.NaturalLanguageFeedback)
		validatedSpec, validateErr := s.validateAndRepairPlanSpec(revisedSpec)
		if validateErr != nil {
			return validateErr
		}
		snapshot.Spec = &validatedSpec
		snapshot.State = planModeStatePlanningReady
		snapshot.SourcePromptVersion = promptVersion

		chat.Meta = applyPlanModeSnapshot(chat.Meta, snapshot)
		chat.UpdatedAt = nowISO()
		state.Chats[req.ChatID] = chat
		response = encodePlanStateResponse(req.ChatID, snapshot, nil)
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errPlanModeChatNotFound):
			writeErr(w, http.StatusNotFound, "not_found", "chat not found", map[string]string{"chat_id": req.ChatID})
		case errors.Is(err, errPlanModeDisabled):
			writeErr(w, http.StatusBadRequest, "plan_mode_disabled", "plan mode is disabled for this chat", nil)
		case errors.Is(err, errPlanModeStateInvalid):
			writeErr(w, http.StatusBadRequest, "plan_state_invalid", "plan mode state does not allow revise", nil)
		case errors.Is(err, errPlanSpecMissing):
			writeErr(w, http.StatusBadRequest, "plan_spec_missing", "plan spec is missing", nil)
		case errors.Is(err, errPlanSpecInvalid):
			writeErr(w, http.StatusBadRequest, "invalid_plan_spec", "plan spec is invalid after repair", nil)
		default:
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) executePlan(w http.ResponseWriter, r *http.Request) {
	var req planExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	if req.ChatID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "chat_id is required", nil)
		return
	}

	response := planExecuteResponse{}
	err := s.store.Write(func(state *repo.State) error {
		sourceChat, ok := state.Chats[req.ChatID]
		if !ok {
			return errPlanModeChatNotFound
		}
		snapshot := parsePlanModeSnapshot(sourceChat.Meta)
		if !snapshot.Enabled {
			return errPlanModeDisabled
		}
		if normalizePlanModeState(snapshot.State) != planModeStatePlanningReady {
			return errPlanModeStateInvalid
		}
		if snapshot.Spec == nil {
			return errPlanSpecMissing
		}

		sessionSeed := strings.TrimSpace(sourceChat.SessionID)
		if sessionSeed == "" {
			sessionSeed = "session"
		}
		executionSessionID := fmt.Sprintf("%s::execute-%d", sessionSeed, time.Now().UTC().UnixNano())
		executionChatID := newID("chat")
		executionChatName := strings.TrimSpace(sourceChat.Name)
		if executionChatName == "" {
			executionChatName = "Execution Chat"
		}
		executionChatName = executionChatName + " · Execute"
		now := nowISO()
		executionMeta := map[string]interface{}{
			chatMetaPromptModeKey:             promptModeDefault,
			chatMetaPlanExecutionSourceChatID: req.ChatID,
		}
		state.Chats[executionChatID] = domain.ChatSpec{
			ID:        executionChatID,
			Name:      executionChatName,
			SessionID: executionSessionID,
			UserID:    sourceChat.UserID,
			Channel:   sourceChat.Channel,
			CreatedAt: now,
			UpdatedAt: now,
			Meta:      executionMeta,
		}
		state.Histories[executionChatID] = []domain.RuntimeMessage{
			{
				ID:   newID("msg"),
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{
						Type: "text",
						Text: buildPlanExecutionSeedMessage(*snapshot.Spec),
					},
				},
			},
		}

		snapshot.State = planModeStateExecuting
		snapshot.ExecutionSessionID = executionSessionID
		sourceChat.Meta = applyPlanModeSnapshot(sourceChat.Meta, snapshot)
		sourceChat.UpdatedAt = nowISO()
		state.Chats[req.ChatID] = sourceChat

		response.ExecutionSessionID = executionSessionID
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errPlanModeChatNotFound):
			writeErr(w, http.StatusNotFound, "not_found", "chat not found", map[string]string{"chat_id": req.ChatID})
		case errors.Is(err, errPlanModeDisabled):
			writeErr(w, http.StatusBadRequest, "plan_mode_disabled", "plan mode is disabled for this chat", nil)
		case errors.Is(err, errPlanModeStateInvalid):
			writeErr(w, http.StatusBadRequest, "plan_state_invalid", "plan mode state does not allow execute", nil)
		case errors.Is(err, errPlanSpecMissing):
			writeErr(w, http.StatusBadRequest, "plan_spec_missing", "plan spec is missing", nil)
		default:
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	chatID := strings.TrimSpace(chi.URLParam(r, "chat_id"))
	if chatID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "chat_id is required", nil)
		return
	}
	response := planStateResponse{}
	found := false
	s.store.Read(func(state *repo.State) {
		chat, ok := state.Chats[chatID]
		if !ok {
			return
		}
		found = true
		response = encodePlanStateResponse(chatID, parsePlanModeSnapshot(chat.Meta), nil)
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "chat not found", map[string]string{"chat_id": chatID})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func applyUpdatePlanToPlanSpecMeta(meta map[string]interface{}, planRows []updatePlanItem) map[string]interface{} {
	if len(meta) == 0 || len(planRows) == 0 {
		return meta
	}
	snapshot := parsePlanModeSnapshot(meta)
	if !snapshot.Enabled || !isPlanModeStateEnabled(snapshot.State) || snapshot.Spec == nil || len(snapshot.Spec.Tasks) == 0 {
		return meta
	}
	statusByKey := map[string]domain.PlanTaskStatus{}
	for _, row := range planRows {
		key := normalizePlanStatusLookupKey(row.Step)
		if key == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(row.Status)) {
		case "pending":
			statusByKey[key] = domain.PlanTaskStatusPending
		case "in_progress":
			statusByKey[key] = domain.PlanTaskStatusInProgress
		case "completed":
			statusByKey[key] = domain.PlanTaskStatusCompleted
		}
	}
	if len(statusByKey) == 0 {
		return meta
	}

	updated := false
	spec := *snapshot.Spec
	for idx := range spec.Tasks {
		task := &spec.Tasks[idx]
		keys := []string{
			normalizePlanStatusLookupKey(task.ID),
			normalizePlanStatusLookupKey(task.Title),
			normalizePlanStatusLookupKey(task.Description),
		}
		for _, key := range keys {
			status, ok := statusByKey[key]
			if !ok {
				continue
			}
			if task.Status != status {
				task.Status = status
				updated = true
			}
			break
		}
	}
	if !updated {
		return meta
	}
	spec.UpdatedAt = nowISO()
	spec = normalizePlanSpec(spec)
	snapshot.Spec = &spec
	meta = applyPlanModeSnapshot(meta, snapshot)
	return meta
}

func normalizePlanStatusLookupKey(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

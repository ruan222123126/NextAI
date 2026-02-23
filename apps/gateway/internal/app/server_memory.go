package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/runner"
	systempromptservice "nextai/apps/gateway/internal/service/systemprompt"
)

const (
	codexMemoryDefaultRootRelativePath          = ".codex/memories"
	codexMemorySummaryFileName                  = "memory_summary.md"
	codexMemoryRegistryFileName                 = "MEMORY.md"
	codexMemoryRawMemoriesFileName              = "raw_memories.md"
	codexMemoryRolloutSummariesDirName          = "rollout_summaries"
	codexMemorySummaryPromptTokenLimit          = 5000
	codexMemoryDefaultStageOneRolloutTokenLimit = 150000
	codexMemoryRolloutSlugMaxLen                = 60
	codexMemoryPipelineUserID                   = "__codex_memory_pipeline__"
	codexMemoryPipelineChannel                  = "console"
	codexMemoryMissingSummaryFallback           = "No memory summary is available yet."
)

type codexMemoryPhaseOneOutput struct {
	RawMemory      string `json:"raw_memory"`
	RolloutSummary string `json:"rollout_summary"`
	RolloutSlug    string `json:"rollout_slug"`
}

type codexMemoryPhaseTwoOutput struct {
	MemoryMD        string `json:"memory_md"`
	MemorySummaryMD string `json:"memory_summary_md"`
}

func resolveCodexMemoryRootContext() (string, string, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	override := strings.TrimSpace(os.Getenv(codexMemoryRootOverrideEnv))
	if override == "" {
		return repoRoot, filepath.Join(repoRoot, filepath.FromSlash(codexMemoryDefaultRootRelativePath)), nil
	}
	if filepath.IsAbs(override) {
		return repoRoot, filepath.Clean(override), nil
	}
	normalized, ok := systempromptservice.NormalizeRelativePath(override)
	if !ok {
		return "", "", fmt.Errorf("%s must be an absolute path or a safe relative path", codexMemoryRootOverrideEnv)
	}
	return repoRoot, filepath.Join(repoRoot, filepath.FromSlash(normalized)), nil
}

func readCodexMemorySummaryForPrompt() string {
	_, memoryRoot, err := resolveCodexMemoryRootContext()
	if err != nil {
		return codexMemoryMissingSummaryFallback
	}
	summaryPath := filepath.Join(memoryRoot, codexMemorySummaryFileName)
	content, err := os.ReadFile(summaryPath)
	if err != nil {
		return codexMemoryMissingSummaryFallback
	}
	summary := strings.TrimSpace(string(content))
	if summary == "" {
		return codexMemoryMissingSummaryFallback
	}
	return truncateCodexMemoryTextByTokenLimit(summary, codexMemorySummaryPromptTokenLimit)
}

func truncateCodexMemoryTextByTokenLimit(raw string, maxTokens int) string {
	normalized := strings.TrimSpace(raw)
	if normalized == "" || maxTokens <= 0 {
		return normalized
	}
	if estimatePromptTokenCount(normalized) <= maxTokens {
		return normalized
	}
	runes := []rune(normalized)
	if len(runes) < 2 {
		return normalized
	}

	keep := len(runes)
	for keep > 64 {
		keep = keep * 3 / 4
		headLen := keep / 2
		tailLen := keep - headLen
		if headLen < 1 || tailLen < 1 || headLen+tailLen >= len(runes) {
			continue
		}
		middle := string(runes[headLen : len(runes)-tailLen])
		candidate := strings.TrimSpace(fmt.Sprintf(
			"%s\n\n...(%d tokens truncated)...\n\n%s",
			string(runes[:headLen]),
			estimatePromptTokenCount(middle),
			string(runes[len(runes)-tailLen:]),
		))
		if estimatePromptTokenCount(candidate) <= maxTokens {
			return candidate
		}
	}

	// Last-resort fallback if token-guided truncation cannot reach the target.
	return summarizeLayerPreview(normalized, 4000)
}

func sanitizeCodexMemorySlug(raw string) string {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	if normalized == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(normalized))
	for _, ch := range normalized {
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= codexMemoryRolloutSlugMaxLen {
			break
		}
	}
	slug := strings.Trim(b.String(), "_")
	for strings.Contains(slug, "__") {
		slug = strings.ReplaceAll(slug, "__", "_")
	}
	return slug
}

func codexMemoryRolloutSummaryFileName(sessionID, rolloutSlug string, now time.Time) string {
	ts := now.UTC().Format("2006-01-02T15-04-05")
	sum := sha256.Sum256([]byte(sessionID + ":" + now.UTC().Format(time.RFC3339Nano)))
	hash4 := fmt.Sprintf("%x", sum[:2])
	slug := sanitizeCodexMemorySlug(rolloutSlug)
	if slug == "" {
		return fmt.Sprintf("%s-%s.md", ts, hash4)
	}
	return fmt.Sprintf("%s-%s-%s.md", ts, hash4, slug)
}

func serializeCodexMemoryRollout(history []domain.RuntimeMessage) string {
	type rolloutMessage struct {
		Role     string `json:"role"`
		Type     string `json:"type,omitempty"`
		Content  string `json:"content,omitempty"`
		Metadata bool   `json:"metadata,omitempty"`
	}
	items := make([]rolloutMessage, 0, len(history))
	for _, msg := range history {
		text := strings.TrimSpace(flattenRuntimeContentsText(msg.Content))
		if text == "" {
			continue
		}
		items = append(items, rolloutMessage{
			Role:     strings.TrimSpace(msg.Role),
			Type:     strings.TrimSpace(msg.Type),
			Content:  text,
			Metadata: len(msg.Metadata) > 0,
		})
	}
	if len(items) == 0 {
		return ""
	}
	out, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(out)
}

func flattenRuntimeContentsText(content []domain.RuntimeContent) string {
	if len(content) == 0 {
		return ""
	}
	parts := make([]string, 0, len(content))
	for _, item := range content {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (s *Server) startCodexMemoryPipeline(
	sessionID string,
	generateConfig runner.GenerateConfig,
	rolloutContents string,
) {
	if s == nil || s.runner == nil {
		return
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(rolloutContents) == "" {
		return
	}
	go func() {
		s.memoryMu.Lock()
		defer s.memoryMu.Unlock()
		if err := s.runCodexMemoryPipeline(context.Background(), sessionID, generateConfig, rolloutContents); err != nil {
			log.Printf("warning: codex memory pipeline failed for session %q: %v", sessionID, err)
		}
	}()
}

func (s *Server) runCodexMemoryPipeline(
	ctx context.Context,
	sessionID string,
	generateConfig runner.GenerateConfig,
	rolloutContents string,
) error {
	repoRoot, memoryRoot, err := resolveCodexMemoryRootContext()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(memoryRoot, codexMemoryRolloutSummariesDirName), 0o755); err != nil {
		return err
	}

	stageOneOutput, err := s.runCodexMemoryPhaseOne(ctx, sessionID, repoRoot, memoryRoot, generateConfig, rolloutContents)
	if err != nil {
		return err
	}

	rawMemoriesContent, existingMemory, existingSummary, err := persistCodexMemoryPhaseOneArtifacts(
		memoryRoot,
		sessionID,
		repoRoot,
		stageOneOutput,
	)
	if err != nil {
		return err
	}

	stageTwoOutput, err := s.runCodexMemoryPhaseTwo(
		ctx,
		sessionID,
		memoryRoot,
		generateConfig,
		rawMemoriesContent,
		existingMemory,
		existingSummary,
	)
	if err != nil {
		return err
	}
	if strings.TrimSpace(stageTwoOutput.MemoryMD) == "" || strings.TrimSpace(stageTwoOutput.MemorySummaryMD) == "" {
		return errors.New("memory phase-2 output is incomplete")
	}

	if err := os.WriteFile(
		filepath.Join(memoryRoot, codexMemoryRegistryFileName),
		[]byte(strings.TrimSpace(stageTwoOutput.MemoryMD)+"\n"),
		0o644,
	); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(memoryRoot, codexMemorySummaryFileName),
		[]byte(strings.TrimSpace(stageTwoOutput.MemorySummaryMD)+"\n"),
		0o644,
	); err != nil {
		return err
	}
	log.Printf("info: codex memory pipeline completed for session %q", sessionID)
	return nil
}

func (s *Server) runCodexMemoryPhaseOne(
	ctx context.Context,
	sessionID, repoRoot, memoryRoot string,
	generateConfig runner.GenerateConfig,
	rolloutContents string,
) (codexMemoryPhaseOneOutput, error) {
	stageOneSource, stageOneSystem, err := loadRequiredSystemLayer([]string{codexMemoriesStageOneSystemPath})
	if err != nil {
		return codexMemoryPhaseOneOutput{}, err
	}
	stageOneInputSource, stageOneInputTemplate, err := loadRequiredSystemLayer([]string{codexMemoriesStageOneInputPath})
	if err != nil {
		return codexMemoryPhaseOneOutput{}, err
	}

	safeSession := sanitizeCodexMemorySlug(sessionID)
	if safeSession == "" {
		safeSession = "session"
	}
	stageOneVars := map[string]string{
		"rollout_path": filepath.ToSlash(filepath.Join(memoryRoot, codexMemoryRolloutSummariesDirName, safeSession+".jsonl")),
		"rollout_cwd":  filepath.ToSlash(repoRoot),
		"rollout_contents": truncateCodexMemoryTextByTokenLimit(
			rolloutContents,
			codexMemoryDefaultStageOneRolloutTokenLimit,
		),
	}
	stageOneInput := strings.TrimSpace(renderTemplate(stageOneInputTemplate, stageOneVars))
	if stageOneInput == "" || hasTemplatePlaceholder(stageOneInput, "rollout_contents") {
		return codexMemoryPhaseOneOutput{}, errors.New("memory phase-1 input rendering failed")
	}
	stageOneInput = stageOneInput + "\n\nReturn valid JSON only with keys: rollout_summary, rollout_slug, raw_memory."

	req := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role:    "system",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: systempromptservice.FormatLayerSourceContent(stageOneSource, stageOneSystem)}},
			},
			{
				Role:    "user",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: systempromptservice.FormatLayerSourceContent(stageOneInputSource, stageOneInput)}},
			},
		},
		SessionID: sessionID,
		UserID:    codexMemoryPipelineUserID,
		Channel:   codexMemoryPipelineChannel,
		Stream:    false,
	}

	reply, err := s.runner.GenerateReply(ctx, req, generateConfig)
	if err != nil {
		return codexMemoryPhaseOneOutput{}, err
	}
	var out codexMemoryPhaseOneOutput
	if err := decodeCodexJSONPayload(reply, &out); err != nil {
		return codexMemoryPhaseOneOutput{}, err
	}
	out.RawMemory = strings.TrimSpace(out.RawMemory)
	out.RolloutSummary = strings.TrimSpace(out.RolloutSummary)
	out.RolloutSlug = strings.TrimSpace(out.RolloutSlug)
	if out.RawMemory == "" || out.RolloutSummary == "" {
		return codexMemoryPhaseOneOutput{}, errors.New("memory phase-1 output is empty")
	}
	return out, nil
}

func persistCodexMemoryPhaseOneArtifacts(
	memoryRoot, sessionID, cwd string,
	stageOne codexMemoryPhaseOneOutput,
) (string, string, string, error) {
	rolloutFile := codexMemoryRolloutSummaryFileName(sessionID, stageOne.RolloutSlug, time.Now())
	rolloutPath := filepath.Join(memoryRoot, codexMemoryRolloutSummariesDirName, rolloutFile)
	rolloutContent := strings.TrimSpace(fmt.Sprintf(
		"session_id: %s\nupdated_at: %s\ncwd: %s\n\n%s\n",
		sessionID,
		time.Now().UTC().Format(time.RFC3339),
		filepath.ToSlash(cwd),
		stageOne.RolloutSummary,
	))
	if err := os.WriteFile(rolloutPath, []byte(rolloutContent+"\n"), 0o644); err != nil {
		return "", "", "", err
	}

	rawPath := filepath.Join(memoryRoot, codexMemoryRawMemoriesFileName)
	existingRaw, _ := os.ReadFile(rawPath)
	existingRawSections := extractCodexMemoryRawSections(string(existingRaw))
	entry := strings.TrimSpace(fmt.Sprintf(
		"## Session `%s`\nupdated_at: %s\ncwd: %s\nrollout_summary_file: %s\n\n%s",
		sessionID,
		time.Now().UTC().Format(time.RFC3339),
		filepath.ToSlash(cwd),
		rolloutFile,
		strings.TrimSpace(stageOne.RawMemory),
	))
	rawDoc := "# Raw Memories\n\nMerged stage-1 raw memories (latest first):\n\n" + entry
	if existingRawSections != "" {
		rawDoc += "\n\n" + existingRawSections
	}
	rawDoc = strings.TrimSpace(rawDoc) + "\n"
	if err := os.WriteFile(rawPath, []byte(rawDoc), 0o644); err != nil {
		return "", "", "", err
	}

	existingMemory, _ := os.ReadFile(filepath.Join(memoryRoot, codexMemoryRegistryFileName))
	existingSummary, _ := os.ReadFile(filepath.Join(memoryRoot, codexMemorySummaryFileName))
	return rawDoc, strings.TrimSpace(string(existingMemory)), strings.TrimSpace(string(existingSummary)), nil
}

func extractCodexMemoryRawSections(raw string) string {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return ""
	}
	index := strings.Index(normalized, "## ")
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(normalized[index:])
}

func (s *Server) runCodexMemoryPhaseTwo(
	ctx context.Context,
	sessionID, memoryRoot string,
	generateConfig runner.GenerateConfig,
	rawMemories, existingMemory, existingSummary string,
) (codexMemoryPhaseTwoOutput, error) {
	consolidationSource, consolidationTemplate, err := loadRequiredSystemLayer([]string{codexMemoriesConsolidationPath})
	if err != nil {
		return codexMemoryPhaseTwoOutput{}, err
	}
	consolidationPrompt := strings.TrimSpace(renderTemplate(consolidationTemplate, map[string]string{
		"memory_root": filepath.ToSlash(memoryRoot),
	}))
	if consolidationPrompt == "" {
		return codexMemoryPhaseTwoOutput{}, errors.New("memory phase-2 consolidation prompt is empty")
	}

	userInput := strings.TrimSpace(fmt.Sprintf(
		"Run memory phase 2 consolidation in offline mode and return strict JSON only.\n\n"+
			"Output schema:\n{\n  \"memory_md\": \"<full MEMORY.md content>\",\n  \"memory_summary_md\": \"<full memory_summary.md content>\"\n}\n\n"+
			"<session_id>%s</session_id>\n\n<raw_memories_md>\n%s\n</raw_memories_md>\n\n"+
			"<existing_memory_md>\n%s\n</existing_memory_md>\n\n<existing_memory_summary_md>\n%s\n</existing_memory_summary_md>\n",
		sessionID,
		truncateCodexMemoryTextByTokenLimit(rawMemories, codexMemoryDefaultStageOneRolloutTokenLimit),
		truncateCodexMemoryTextByTokenLimit(existingMemory, codexMemoryDefaultStageOneRolloutTokenLimit/2),
		truncateCodexMemoryTextByTokenLimit(existingSummary, codexMemorySummaryPromptTokenLimit),
	))

	req := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role:    "system",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: systempromptservice.FormatLayerSourceContent(consolidationSource, consolidationPrompt)}},
			},
			{
				Role:    "user",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: userInput}},
			},
		},
		SessionID: sessionID,
		UserID:    codexMemoryPipelineUserID,
		Channel:   codexMemoryPipelineChannel,
		Stream:    false,
	}

	reply, err := s.runner.GenerateReply(ctx, req, generateConfig)
	if err != nil {
		return codexMemoryPhaseTwoOutput{}, err
	}
	var out codexMemoryPhaseTwoOutput
	if err := decodeCodexJSONPayload(reply, &out); err != nil {
		alt := map[string]string{}
		if altErr := decodeCodexJSONPayload(reply, &alt); altErr == nil {
			out.MemoryMD = strings.TrimSpace(alt["MEMORY.md"])
			out.MemorySummaryMD = strings.TrimSpace(alt["memory_summary.md"])
		} else {
			return codexMemoryPhaseTwoOutput{}, err
		}
	}
	out.MemoryMD = strings.TrimSpace(out.MemoryMD)
	out.MemorySummaryMD = strings.TrimSpace(out.MemorySummaryMD)
	return out, nil
}

func decodeCodexJSONPayload(raw string, out interface{}) error {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return errors.New("empty json payload")
	}
	if err := json.Unmarshal([]byte(normalized), out); err == nil {
		return nil
	}

	withoutFence := normalized
	if strings.HasPrefix(withoutFence, "```") {
		withoutFence = strings.TrimPrefix(withoutFence, "```json")
		withoutFence = strings.TrimPrefix(withoutFence, "```")
		if idx := strings.LastIndex(withoutFence, "```"); idx >= 0 {
			withoutFence = withoutFence[:idx]
		}
		withoutFence = strings.TrimSpace(withoutFence)
		if withoutFence != "" {
			if err := json.Unmarshal([]byte(withoutFence), out); err == nil {
				return nil
			}
		}
	}

	candidate := extractFirstJSONObject(normalized)
	if candidate == "" {
		return errors.New("json object not found in payload")
	}
	return json.Unmarshal([]byte(candidate), out)
}

func extractFirstJSONObject(raw string) string {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

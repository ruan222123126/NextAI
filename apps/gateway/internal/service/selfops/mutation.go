package selfops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/ports"
)

const (
	providerConfigMutationPath = "config/models.json"
	activeLLMMutationPath      = "config/active-llm.json"
)

type mutationRecord struct {
	ID                     string
	Target                 string
	ExpiresAt              time.Time
	ConfirmHash            string
	AllowSensitive         bool
	RequiresSensitiveAllow bool
	Checks                 MutationChecks
	DiffSummary            []MutationDiffSummary
	UnifiedDiff            string
	BaseHashes             map[string]string
	WorkspaceWrites        []workspaceMutationWrite
	ProviderConfig         map[string]repo.ProviderSetting
	ActiveLLM              domain.ModelSlotConfig
	AppliedTargets         []string
	Applied                bool
}

type workspaceMutationWrite struct {
	Path string
	Text *string
	JSON interface{}
}

type preparedMutation struct {
	Target                 string
	Checks                 MutationChecks
	DiffSummary            []MutationDiffSummary
	UnifiedDiff            string
	RequiresSensitiveAllow bool
	BaseHashes             map[string]string
	WorkspaceWrites        []workspaceMutationWrite
	ProviderConfig         map[string]repo.ProviderSetting
	ActiveLLM              domain.ModelSlotConfig
	AppliedTargets         []string
}

type workspaceMutationDoc struct {
	Path         string
	IsText       bool
	OriginalText string
	CurrentText  string
	OriginalJSON interface{}
	CurrentJSON  interface{}
}

func (s *Service) PreviewMutation(input PreviewMutationInput) (PreviewMutationOutput, error) {
	if err := s.validateStore(); err != nil {
		return PreviewMutationOutput{}, err
	}

	target := normalizeMutationTarget(input.Target)
	if target == "" {
		return PreviewMutationOutput{}, &ServiceError{
			Code:    "invalid_request",
			Message: "target is required",
		}
	}
	if len(input.Operations) == 0 {
		return PreviewMutationOutput{}, &ServiceError{
			Code:    "invalid_request",
			Message: "operations are required",
		}
	}

	prepared, err := s.prepareMutation(target, input.Operations)
	if err != nil {
		return PreviewMutationOutput{}, err
	}

	now := s.deps.Now()
	record := mutationRecord{
		ID:                     fmt.Sprintf("%s-%d", mutationIDPrefix, now.UnixNano()),
		Target:                 prepared.Target,
		ExpiresAt:              now.Add(s.deps.MutationTTL),
		AllowSensitive:         input.AllowSensitive,
		RequiresSensitiveAllow: prepared.RequiresSensitiveAllow,
		Checks:                 prepared.Checks,
		DiffSummary:            append([]MutationDiffSummary{}, prepared.DiffSummary...),
		UnifiedDiff:            prepared.UnifiedDiff,
		BaseHashes:             cloneStringMap(prepared.BaseHashes),
		WorkspaceWrites:        append([]workspaceMutationWrite{}, prepared.WorkspaceWrites...),
		ProviderConfig:         cloneProviderSettings(prepared.ProviderConfig),
		ActiveLLM:              prepared.ActiveLLM,
		AppliedTargets:         append([]string{}, prepared.AppliedTargets...),
	}
	record.ConfirmHash = buildMutationConfirmHash(record)

	s.mutationMu.Lock()
	s.purgeExpiredMutationsLocked(now)
	s.mutations[record.ID] = record
	s.mutationMu.Unlock()

	return PreviewMutationOutput{
		MutationID:             record.ID,
		ExpiresAt:              nowISO(record.ExpiresAt),
		ConfirmHash:            record.ConfirmHash,
		Checks:                 record.Checks,
		DiffSummary:            append([]MutationDiffSummary{}, record.DiffSummary...),
		UnifiedDiff:            record.UnifiedDiff,
		RequiresSensitiveAllow: record.RequiresSensitiveAllow,
	}, nil
}

func (s *Service) ApplyMutation(input ApplyMutationInput) (ApplyMutationOutput, error) {
	if err := s.validateStore(); err != nil {
		return ApplyMutationOutput{}, err
	}

	mutationID := strings.TrimSpace(input.MutationID)
	confirmHash := strings.TrimSpace(input.ConfirmHash)
	if mutationID == "" || confirmHash == "" {
		return ApplyMutationOutput{}, &ServiceError{
			Code:    "invalid_request",
			Message: "mutation_id and confirm_hash are required",
		}
	}

	now := s.deps.Now()

	s.mutationMu.Lock()
	record, ok := s.mutations[mutationID]
	if !ok {
		s.purgeExpiredMutationsLocked(now)
		s.mutationMu.Unlock()
		return ApplyMutationOutput{}, &ServiceError{
			Code:    "mutation_not_found",
			Message: "mutation not found",
		}
	}
	if now.After(record.ExpiresAt) {
		delete(s.mutations, mutationID)
		s.mutationMu.Unlock()
		return ApplyMutationOutput{}, &ServiceError{
			Code:    "mutation_expired",
			Message: "mutation is expired",
		}
	}
	s.purgeExpiredMutationsLocked(now)
	if record.Applied {
		s.mutationMu.Unlock()
		return ApplyMutationOutput{}, &ServiceError{
			Code:    "mutation_apply_conflict",
			Message: "mutation is already applied",
		}
	}
	if record.ConfirmHash != confirmHash {
		s.mutationMu.Unlock()
		return ApplyMutationOutput{}, &ServiceError{
			Code:    "mutation_hash_mismatch",
			Message: "confirm_hash does not match preview result",
		}
	}
	if record.AllowSensitive != input.AllowSensitive {
		s.mutationMu.Unlock()
		return ApplyMutationOutput{}, &ServiceError{
			Code:    "mutation_sensitive_denied",
			Message: "allow_sensitive must match preview request",
		}
	}
	if record.RequiresSensitiveAllow && !input.AllowSensitive {
		s.mutationMu.Unlock()
		return ApplyMutationOutput{}, &ServiceError{
			Code:    "mutation_sensitive_denied",
			Message: "sensitive changes require allow_sensitive=true",
		}
	}
	if !record.Checks.PathWhitelistPassed {
		s.mutationMu.Unlock()
		return ApplyMutationOutput{}, &ServiceError{
			Code:    "mutation_path_denied",
			Message: "mutation path is not allowed",
			Details: map[string]interface{}{
				"denied_paths": append([]string{}, record.Checks.DeniedPaths...),
			},
		}
	}
	record.Applied = true
	s.mutations[mutationID] = record
	s.mutationMu.Unlock()

	appliedTargets, applyErr := s.applyMutationRecord(record)
	if applyErr != nil {
		s.mutationMu.Lock()
		if current, ok := s.mutations[mutationID]; ok {
			current.Applied = false
			s.mutations[mutationID] = current
		}
		s.mutationMu.Unlock()
		return ApplyMutationOutput{}, applyErr
	}

	s.mutationMu.Lock()
	delete(s.mutations, mutationID)
	s.mutationMu.Unlock()

	return ApplyMutationOutput{
		Applied:        true,
		AppliedTargets: appliedTargets,
		AuditID:        fmt.Sprintf("%s-%d", mutationAuditPrefix, now.UnixNano()),
	}, nil
}

func (s *Service) applyMutationRecord(record mutationRecord) ([]string, error) {
	switch record.Target {
	case TargetWorkspaceFile:
		return s.applyWorkspaceMutation(record)
	case TargetProviderConfig:
		return s.applyProviderConfigMutation(record)
	case TargetActiveLLM:
		return s.applyActiveLLMMutation(record)
	default:
		return nil, &ServiceError{
			Code:    "mutation_apply_conflict",
			Message: "unsupported mutation target",
		}
	}
}

func (s *Service) applyWorkspaceMutation(record mutationRecord) ([]string, error) {
	if s.deps.GetWorkspaceFile == nil || s.deps.PutWorkspaceFile == nil {
		return nil, errors.New("workspace dependencies are required")
	}
	if len(record.WorkspaceWrites) == 0 {
		return []string{}, nil
	}

	for _, write := range record.WorkspaceWrites {
		doc, err := s.loadWorkspaceMutationDoc(write.Path)
		if err != nil {
			return nil, err
		}
		current := serializeWorkspaceMutationDoc(doc, true)
		if currentHash := hashString(current); currentHash != record.BaseHashes[write.Path] {
			return nil, &ServiceError{
				Code:    "mutation_apply_conflict",
				Message: "workspace file changed after preview",
				Details: map[string]string{"path": write.Path},
			}
		}
	}

	for _, write := range record.WorkspaceWrites {
		if write.Text != nil {
			body, err := json.Marshal(map[string]string{"content": *write.Text})
			if err != nil {
				return nil, &ServiceError{
					Code:    "mutation_apply_conflict",
					Message: "failed to encode workspace write payload",
					Details: map[string]string{"path": write.Path},
				}
			}
			if err := s.deps.PutWorkspaceFile(write.Path, body); err != nil {
				return nil, &ServiceError{
					Code:    "mutation_apply_conflict",
					Message: "failed to apply workspace mutation",
					Details: map[string]string{"path": write.Path},
				}
			}
			continue
		}
		body, err := json.Marshal(write.JSON)
		if err != nil {
			return nil, &ServiceError{
				Code:    "mutation_apply_conflict",
				Message: "failed to encode workspace write payload",
				Details: map[string]string{"path": write.Path},
			}
		}
		if err := s.deps.PutWorkspaceFile(write.Path, body); err != nil {
			return nil, &ServiceError{
				Code:    "mutation_apply_conflict",
				Message: "failed to apply workspace mutation",
				Details: map[string]string{"path": write.Path},
			}
		}
	}

	targets := make([]string, 0, len(record.WorkspaceWrites))
	for _, item := range record.WorkspaceWrites {
		targets = append(targets, item.Path)
	}
	sort.Strings(targets)
	return targets, nil
}

func (s *Service) applyProviderConfigMutation(record mutationRecord) ([]string, error) {
	beforeHash := ""
	if record.BaseHashes != nil {
		beforeHash = record.BaseHashes[TargetProviderConfig]
	}
	currentHash := ""
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		currentHash = hashString(stableJSON(cloneProviderSettings(st.Providers)))
	})
	if beforeHash != currentHash {
		return nil, &ServiceError{
			Code:    "mutation_apply_conflict",
			Message: "provider config changed after preview",
		}
	}

	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		st.Providers = cloneProviderSettings(record.ProviderConfig)
		activeProviderID := provider.NormalizeProviderID(st.ActiveLLM.ProviderID)
		if activeProviderID != "" {
			if _, ok := st.Providers[activeProviderID]; !ok {
				st.ActiveLLM = domain.ModelSlotConfig{}
			}
		}
		return nil
	}); err != nil {
		return nil, &ServiceError{
			Code:    "mutation_apply_conflict",
			Message: "failed to apply provider config mutation",
		}
	}
	return []string{providerConfigMutationPath}, nil
}

func (s *Service) applyActiveLLMMutation(record mutationRecord) ([]string, error) {
	beforeHash := ""
	if record.BaseHashes != nil {
		beforeHash = record.BaseHashes[TargetActiveLLM]
	}
	currentHash := ""
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		currentHash = hashString(stableJSON(st.ActiveLLM))
	})
	if beforeHash != currentHash {
		return nil, &ServiceError{
			Code:    "mutation_apply_conflict",
			Message: "active_llm changed after preview",
		}
	}

	if err := s.deps.Store.WriteSettings(func(st *ports.SettingsAggregate) error {
		st.ActiveLLM = record.ActiveLLM
		return nil
	}); err != nil {
		return nil, &ServiceError{
			Code:    "mutation_apply_conflict",
			Message: "failed to apply active_llm mutation",
		}
	}
	return []string{activeLLMMutationPath}, nil
}

func (s *Service) prepareMutation(target string, operations []MutationOperation) (preparedMutation, error) {
	switch target {
	case TargetWorkspaceFile:
		return s.prepareWorkspaceMutation(operations)
	case TargetProviderConfig:
		return s.prepareProviderConfigMutation(operations)
	case TargetActiveLLM:
		return s.prepareActiveLLMMutation(operations)
	default:
		return preparedMutation{}, &ServiceError{
			Code:    "invalid_request",
			Message: "unsupported mutation target",
		}
	}
}

func (s *Service) prepareWorkspaceMutation(operations []MutationOperation) (preparedMutation, error) {
	if s.deps.GetWorkspaceFile == nil || s.deps.PutWorkspaceFile == nil {
		return preparedMutation{}, errors.New("workspace dependencies are required")
	}

	checks := MutationChecks{
		PathWhitelistPassed: true,
		StructureValid:      true,
		RiskLevel:           "low",
		SensitiveFields:     []string{},
		DeniedPaths:         []string{},
	}
	docs := map[string]*workspaceMutationDoc{}
	deniedPathSet := map[string]struct{}{}

	for _, rawOp := range operations {
		op := rawOp
		kind := normalizeMutationOperationKind(op.Kind)
		if kind == "" {
			return preparedMutation{}, &ServiceError{
				Code:    "invalid_request",
				Message: "operation.kind must be one of replace|json_patch|text_rewrite",
			}
		}

		normalizedPath, ok := normalizeWorkspaceMutationPath(op.Path)
		if !ok {
			deniedPathSet[strings.TrimSpace(op.Path)] = struct{}{}
			continue
		}
		if !isWorkspaceMutationPathAllowed(normalizedPath) {
			deniedPathSet[normalizedPath] = struct{}{}
			continue
		}

		doc, ok := docs[normalizedPath]
		if !ok {
			loaded, err := s.loadWorkspaceMutationDoc(normalizedPath)
			if err != nil {
				return preparedMutation{}, err
			}
			doc = loaded
			docs[normalizedPath] = doc
		}

		switch kind {
		case OperationReplace:
			if doc.IsText {
				content, ok := op.Value.(string)
				if !ok {
					return preparedMutation{}, &ServiceError{
						Code:    "invalid_request",
						Message: "workspace text replace requires string value",
						Details: map[string]string{"path": normalizedPath},
					}
				}
				doc.CurrentText = content
				continue
			}
			doc.CurrentJSON = cloneJSONValue(op.Value)
		case OperationTextRewrite:
			if !doc.IsText {
				return preparedMutation{}, &ServiceError{
					Code:    "invalid_request",
					Message: "text_rewrite is only supported for text workspace files",
					Details: map[string]string{"path": normalizedPath},
				}
			}
			if strings.TrimSpace(op.Search) == "" {
				return preparedMutation{}, &ServiceError{
					Code:    "invalid_request",
					Message: "text_rewrite requires non-empty search value",
					Details: map[string]string{"path": normalizedPath},
				}
			}
			doc.CurrentText = strings.ReplaceAll(doc.CurrentText, op.Search, op.Replace)
		case OperationJSONPatch:
			if doc.IsText {
				return preparedMutation{}, &ServiceError{
					Code:    "invalid_request",
					Message: "json_patch is not supported for text workspace files",
					Details: map[string]string{"path": normalizedPath},
				}
			}
			patched, err := applyJSONPatchOperations(cloneJSONValue(doc.CurrentJSON), op.Patch)
			if err != nil {
				return preparedMutation{}, &ServiceError{
					Code:    "invalid_request",
					Message: "json_patch is invalid",
					Details: map[string]string{
						"path":  normalizedPath,
						"error": err.Error(),
					},
				}
			}
			doc.CurrentJSON = patched
		default:
			return preparedMutation{}, &ServiceError{
				Code:    "invalid_request",
				Message: "unsupported operation kind",
			}
		}
	}

	if len(deniedPathSet) > 0 {
		checks.PathWhitelistPassed = false
		paths := make([]string, 0, len(deniedPathSet))
		for path := range deniedPathSet {
			if path == "" {
				path = "(empty)"
			}
			paths = append(paths, path)
		}
		sort.Strings(paths)
		checks.DeniedPaths = paths
	}

	diffSummary := make([]MutationDiffSummary, 0, len(docs)+len(checks.DeniedPaths))
	unifiedDiffChunks := make([]string, 0, len(docs))
	workspaceWrites := make([]workspaceMutationWrite, 0, len(docs))
	baseHashes := map[string]string{}
	appliedTargets := []string{}
	sensitiveFields := map[string]struct{}{}

	docPaths := make([]string, 0, len(docs))
	for path := range docs {
		docPaths = append(docPaths, path)
	}
	sort.Strings(docPaths)
	for _, path := range docPaths {
		doc := docs[path]
		before := serializeWorkspaceMutationDoc(doc, false)
		after := serializeWorkspaceMutationDoc(doc, true)
		beforeHash := hashString(before)
		afterHash := hashString(after)
		changed := beforeHash != afterHash
		baseHashes[path] = beforeHash
		diffSummary = append(diffSummary, MutationDiffSummary{
			Target:     TargetWorkspaceFile,
			Path:       path,
			Changed:    changed,
			BeforeHash: beforeHash,
			AfterHash:  afterHash,
		})

		if doc.IsText {
			collectSensitiveFieldsFromText(after, sensitiveFields)
		} else {
			collectSensitiveFieldsFromValue(doc.CurrentJSON, sensitiveFields)
		}

		if !changed {
			continue
		}
		appliedTargets = append(appliedTargets, path)
		if doc.IsText {
			value := doc.CurrentText
			workspaceWrites = append(workspaceWrites, workspaceMutationWrite{
				Path: path,
				Text: &value,
			})
		} else {
			workspaceWrites = append(workspaceWrites, workspaceMutationWrite{
				Path: path,
				JSON: cloneJSONValue(doc.CurrentJSON),
			})
		}
		diff := buildUnifiedDiff(path, before, after)
		if diff != "" {
			unifiedDiffChunks = append(unifiedDiffChunks, diff)
		}
	}

	for _, path := range checks.DeniedPaths {
		diffSummary = append(diffSummary, MutationDiffSummary{
			Target:     TargetWorkspaceFile,
			Path:       path,
			Changed:    false,
			BeforeHash: "",
			AfterHash:  "",
		})
	}

	sensitive := sortedSensitiveFieldList(sensitiveFields)
	checks.SensitiveFields = sensitive
	requiresSensitiveAllow := len(sensitive) > 0
	if !checks.PathWhitelistPassed || requiresSensitiveAllow {
		checks.RiskLevel = "high"
	} else if len(appliedTargets) > 0 {
		checks.RiskLevel = "medium"
	} else {
		checks.RiskLevel = "low"
	}

	return preparedMutation{
		Target:                 TargetWorkspaceFile,
		Checks:                 checks,
		DiffSummary:            diffSummary,
		UnifiedDiff:            strings.Join(unifiedDiffChunks, "\n"),
		RequiresSensitiveAllow: requiresSensitiveAllow,
		BaseHashes:             baseHashes,
		WorkspaceWrites:        workspaceWrites,
		AppliedTargets:         appliedTargets,
	}, nil
}

func (s *Service) prepareProviderConfigMutation(operations []MutationOperation) (preparedMutation, error) {
	var baseProviders map[string]repo.ProviderSetting
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		baseProviders = cloneProviderSettings(st.Providers)
	})
	current := cloneJSONValue(baseProviders)

	for _, rawOp := range operations {
		op := rawOp
		kind := normalizeMutationOperationKind(op.Kind)
		switch kind {
		case OperationReplace:
			next, err := applyJSONReplaceOperation(current, op.Path, op.Value)
			if err != nil {
				return preparedMutation{}, &ServiceError{
					Code:    "invalid_request",
					Message: "replace operation is invalid",
					Details: map[string]string{"error": err.Error()},
				}
			}
			current = next
		case OperationJSONPatch:
			next, err := applyJSONPatchOperations(current, op.Patch)
			if err != nil {
				return preparedMutation{}, &ServiceError{
					Code:    "invalid_request",
					Message: "json_patch operation is invalid",
					Details: map[string]string{"error": err.Error()},
				}
			}
			current = next
		case OperationTextRewrite:
			return preparedMutation{}, &ServiceError{
				Code:    "invalid_request",
				Message: "text_rewrite is not supported for provider_config target",
			}
		default:
			return preparedMutation{}, &ServiceError{
				Code:    "invalid_request",
				Message: "operation.kind must be one of replace|json_patch|text_rewrite",
			}
		}
	}

	nextProviders, err := decodeProviderSettings(current)
	if err != nil {
		return preparedMutation{}, &ServiceError{
			Code:    "invalid_request",
			Message: "provider_config structure is invalid",
			Details: map[string]string{"error": err.Error()},
		}
	}

	before := stableJSON(baseProviders)
	after := stableJSON(nextProviders)
	beforeHash := hashString(before)
	afterHash := hashString(after)
	changed := beforeHash != afterHash

	sensitiveFields := map[string]struct{}{}
	collectSensitiveFieldsFromValue(nextProviders, sensitiveFields)
	sensitive := sortedSensitiveFieldList(sensitiveFields)
	requiresSensitiveAllow := len(sensitive) > 0

	checks := MutationChecks{
		PathWhitelistPassed: true,
		StructureValid:      true,
		RiskLevel:           "medium",
		SensitiveFields:     sensitive,
		DeniedPaths:         []string{},
	}
	if requiresSensitiveAllow {
		checks.RiskLevel = "high"
	}
	if !changed {
		checks.RiskLevel = "low"
	}

	diffSummary := []MutationDiffSummary{
		{
			Target:     TargetProviderConfig,
			Path:       providerConfigMutationPath,
			Changed:    changed,
			BeforeHash: beforeHash,
			AfterHash:  afterHash,
		},
	}
	unifiedDiff := ""
	if changed {
		unifiedDiff = buildUnifiedDiff(providerConfigMutationPath, before, after)
	}

	return preparedMutation{
		Target:                 TargetProviderConfig,
		Checks:                 checks,
		DiffSummary:            diffSummary,
		UnifiedDiff:            unifiedDiff,
		RequiresSensitiveAllow: requiresSensitiveAllow,
		BaseHashes: map[string]string{
			TargetProviderConfig: beforeHash,
		},
		ProviderConfig: nextProviders,
		AppliedTargets: []string{providerConfigMutationPath},
	}, nil
}

func (s *Service) prepareActiveLLMMutation(operations []MutationOperation) (preparedMutation, error) {
	baseSlot := domain.ModelSlotConfig{}
	providers := map[string]repo.ProviderSetting{}
	s.deps.Store.ReadSettings(func(st ports.SettingsAggregate) {
		baseSlot = st.ActiveLLM
		providers = cloneProviderSettings(st.Providers)
	})
	current := cloneJSONValue(baseSlot)

	for _, rawOp := range operations {
		op := rawOp
		kind := normalizeMutationOperationKind(op.Kind)
		switch kind {
		case OperationReplace:
			next, err := applyJSONReplaceOperation(current, op.Path, op.Value)
			if err != nil {
				return preparedMutation{}, &ServiceError{
					Code:    "invalid_request",
					Message: "replace operation is invalid",
					Details: map[string]string{"error": err.Error()},
				}
			}
			current = next
		case OperationJSONPatch:
			next, err := applyJSONPatchOperations(current, op.Patch)
			if err != nil {
				return preparedMutation{}, &ServiceError{
					Code:    "invalid_request",
					Message: "json_patch operation is invalid",
					Details: map[string]string{"error": err.Error()},
				}
			}
			current = next
		case OperationTextRewrite:
			return preparedMutation{}, &ServiceError{
				Code:    "invalid_request",
				Message: "text_rewrite is not supported for active_llm target",
			}
		default:
			return preparedMutation{}, &ServiceError{
				Code:    "invalid_request",
				Message: "operation.kind must be one of replace|json_patch|text_rewrite",
			}
		}
	}

	slot, err := decodeModelSlot(current)
	if err != nil {
		return preparedMutation{}, &ServiceError{
			Code:    "invalid_request",
			Message: "active_llm structure is invalid",
			Details: map[string]string{"error": err.Error()},
		}
	}
	slot.ProviderID = provider.NormalizeProviderID(slot.ProviderID)
	slot.Model = strings.TrimSpace(slot.Model)
	if (slot.ProviderID == "") != (slot.Model == "") {
		return preparedMutation{}, &ServiceError{
			Code:    "invalid_request",
			Message: "provider_id and model must be set together",
		}
	}
	if slot.ProviderID != "" {
		resolved, validationErr := resolveAndValidateModel(providers, slot.ProviderID, slot.Model)
		if validationErr != nil {
			return preparedMutation{}, validationErr
		}
		slot = resolved
	}

	before := stableJSON(baseSlot)
	after := stableJSON(slot)
	beforeHash := hashString(before)
	afterHash := hashString(after)
	changed := beforeHash != afterHash

	checks := MutationChecks{
		PathWhitelistPassed: true,
		StructureValid:      true,
		RiskLevel:           "medium",
		SensitiveFields:     []string{},
		DeniedPaths:         []string{},
	}
	if !changed {
		checks.RiskLevel = "low"
	}

	diffSummary := []MutationDiffSummary{
		{
			Target:     TargetActiveLLM,
			Path:       activeLLMMutationPath,
			Changed:    changed,
			BeforeHash: beforeHash,
			AfterHash:  afterHash,
		},
	}
	unifiedDiff := ""
	if changed {
		unifiedDiff = buildUnifiedDiff(activeLLMMutationPath, before, after)
	}

	return preparedMutation{
		Target:                 TargetActiveLLM,
		Checks:                 checks,
		DiffSummary:            diffSummary,
		UnifiedDiff:            unifiedDiff,
		RequiresSensitiveAllow: false,
		BaseHashes: map[string]string{
			TargetActiveLLM: beforeHash,
		},
		ActiveLLM:      slot,
		AppliedTargets: []string{activeLLMMutationPath},
	}, nil
}

func (s *Service) loadWorkspaceMutationDoc(path string) (*workspaceMutationDoc, error) {
	raw, err := s.deps.GetWorkspaceFile(path)
	if err != nil {
		return nil, &ServiceError{
			Code:    "invalid_request",
			Message: "workspace file not found",
			Details: map[string]string{"path": path},
		}
	}

	if asMap, ok := raw.(map[string]string); ok {
		if len(asMap) == 1 {
			if content, exists := asMap["content"]; exists {
				return &workspaceMutationDoc{
					Path:         path,
					IsText:       true,
					OriginalText: content,
					CurrentText:  content,
				}, nil
			}
		}
	}
	if asMap, ok := raw.(map[string]interface{}); ok {
		if len(asMap) == 1 {
			if contentRaw, exists := asMap["content"]; exists {
				if content, ok := contentRaw.(string); ok {
					return &workspaceMutationDoc{
						Path:         path,
						IsText:       true,
						OriginalText: content,
						CurrentText:  content,
					}, nil
				}
			}
		}
	}

	normalizedJSON := cloneJSONValue(raw)
	return &workspaceMutationDoc{
		Path:         path,
		IsText:       false,
		OriginalJSON: normalizedJSON,
		CurrentJSON:  cloneJSONValue(normalizedJSON),
	}, nil
}

func serializeWorkspaceMutationDoc(doc *workspaceMutationDoc, current bool) string {
	if doc == nil {
		return ""
	}
	if doc.IsText {
		if current {
			return doc.CurrentText
		}
		return doc.OriginalText
	}
	if current {
		return stableJSON(doc.CurrentJSON)
	}
	return stableJSON(doc.OriginalJSON)
}

func normalizeMutationTarget(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case TargetWorkspaceFile:
		return TargetWorkspaceFile
	case TargetProviderConfig:
		return TargetProviderConfig
	case TargetActiveLLM:
		return TargetActiveLLM
	default:
		return ""
	}
}

func normalizeMutationOperationKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case OperationReplace:
		return OperationReplace
	case OperationJSONPatch:
		return OperationJSONPatch
	case OperationTextRewrite:
		return OperationTextRewrite
	default:
		return ""
	}
}

func normalizeWorkspaceMutationPath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if trimmed == "" {
		return "", false
	}
	if unescaped, err := url.PathUnescape(trimmed); err == nil {
		trimmed = unescaped
	}
	trimmed = filepath.ToSlash(trimmed)
	parts := strings.Split(trimmed, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			return "", false
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/"), true
}

func isWorkspaceMutationPathAllowed(path string) bool {
	switch {
	case path == providerConfigMutationPath:
		return true
	case path == activeLLMMutationPath:
		return true
	case strings.HasPrefix(path, "prompts/"):
		return true
	case strings.HasPrefix(path, "prompt/"):
		return true
	case strings.HasPrefix(path, "docs/AI/"):
		return true
	default:
		return false
	}
}

func decodeProviderSettings(raw interface{}) (map[string]repo.ProviderSetting, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	parsed := map[string]repo.ProviderSetting{}
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return nil, err
	}

	out := map[string]repo.ProviderSetting{}
	for rawID, rawSetting := range parsed {
		id := provider.NormalizeProviderID(rawID)
		if id == "" {
			return nil, errors.New("provider id cannot be empty")
		}
		if id == demoProviderID {
			continue
		}
		setting := rawSetting
		provider.NormalizeProviderSetting(&setting)
		if setting.TimeoutMS < 0 {
			return nil, fmt.Errorf("provider %q timeout_ms must be >= 0", rawID)
		}
		setting.Headers = provider.SanitizeStringMap(setting.Headers)
		setting.ModelAliases = provider.SanitizeStringMap(setting.ModelAliases)
		out[id] = setting
	}
	return out, nil
}

func decodeModelSlot(raw interface{}) (domain.ModelSlotConfig, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return domain.ModelSlotConfig{}, err
	}
	out := domain.ModelSlotConfig{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return domain.ModelSlotConfig{}, err
	}
	return out, nil
}

func buildMutationConfirmHash(record mutationRecord) string {
	payload := map[string]interface{}{
		"target":                   record.Target,
		"checks":                   record.Checks,
		"diff_summary":             record.DiffSummary,
		"base_hashes":              record.BaseHashes,
		"allow_sensitive":          record.AllowSensitive,
		"requires_sensitive_allow": record.RequiresSensitiveAllow,
	}
	return hashString(stableJSON(payload))
}

func buildUnifiedDiff(path, before, after string) string {
	if before == after {
		return ""
	}
	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("--- a/%s\n", path))
	builder.WriteString(fmt.Sprintf("+++ b/%s\n", path))
	builder.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", len(beforeLines), len(afterLines)))
	maxLen := len(beforeLines)
	if len(afterLines) > maxLen {
		maxLen = len(afterLines)
	}
	for index := 0; index < maxLen; index++ {
		if index < len(beforeLines) {
			builder.WriteString("-" + beforeLines[index] + "\n")
		}
		if index < len(afterLines) {
			builder.WriteString("+" + afterLines[index] + "\n")
		}
	}
	return strings.TrimRight(builder.String(), "\n")
}

func hashString(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func stableJSON(value interface{}) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func collectSensitiveFieldsFromValue(value interface{}, fields map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			if isSensitiveFieldName(key) {
				fields[key] = struct{}{}
			}
			collectSensitiveFieldsFromValue(item, fields)
		}
	case map[string]string:
		for key := range typed {
			if isSensitiveFieldName(key) {
				fields[key] = struct{}{}
			}
		}
	case []interface{}:
		for _, item := range typed {
			collectSensitiveFieldsFromValue(item, fields)
		}
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return
		}
		var generic interface{}
		if err := json.Unmarshal(data, &generic); err != nil {
			return
		}
		switch concrete := generic.(type) {
		case map[string]interface{}, []interface{}, map[string]string:
			collectSensitiveFieldsFromValue(concrete, fields)
		}
	}
}

func collectSensitiveFieldsFromText(text string, fields map[string]struct{}) {
	upper := strings.ToUpper(text)
	if strings.Contains(upper, "API_KEY") {
		fields["api_key"] = struct{}{}
	}
	for _, suffix := range []string{"_KEY", "_TOKEN", "_SECRET"} {
		if strings.Contains(upper, suffix) {
			fields[suffix] = struct{}{}
		}
	}
}

func sortedSensitiveFieldList(fields map[string]struct{}) []string {
	if len(fields) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(fields))
	for key := range fields {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func isSensitiveFieldName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "api_key") {
		return true
	}
	upper := strings.ToUpper(trimmed)
	return strings.HasSuffix(upper, "_KEY") ||
		strings.HasSuffix(upper, "_TOKEN") ||
		strings.HasSuffix(upper, "_SECRET")
}

func cloneProviderSettings(in map[string]repo.ProviderSetting) map[string]repo.ProviderSetting {
	if len(in) == 0 {
		return map[string]repo.ProviderSetting{}
	}
	out := map[string]repo.ProviderSetting{}
	for id, raw := range in {
		setting := raw
		provider.NormalizeProviderSetting(&setting)
		headers := map[string]string{}
		for key, value := range setting.Headers {
			headers[key] = value
		}
		aliases := map[string]string{}
		for key, value := range setting.ModelAliases {
			aliases[key] = value
		}
		setting.Headers = headers
		setting.ModelAliases = aliases
		if setting.Enabled != nil {
			enabled := *setting.Enabled
			setting.Enabled = &enabled
		}
		if setting.Store != nil {
			store := *setting.Store
			setting.Store = &store
		}
		out[id] = setting
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *Service) purgeExpiredMutationsLocked(now time.Time) {
	for id, item := range s.mutations {
		if now.After(item.ExpiresAt) {
			delete(s.mutations, id)
		}
	}
}

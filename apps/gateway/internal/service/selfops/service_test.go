package selfops

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/adapters"
)

func TestResolveSessionModelPrefersSessionOverride(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	if err := store.Write(func(st *repo.State) error {
		st.ActiveLLM = domain.ModelSlotConfig{
			ProviderID: "openai",
			Model:      "ghost-model",
		}
		chat := domain.ChatSpec{
			ID:        "chat-selfops",
			Name:      "selfops",
			SessionID: "s-selfops",
			UserID:    "u-selfops",
			Channel:   "console",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			Meta: map[string]interface{}{
				domain.ChatMetaActiveLLM: map[string]interface{}{
					"provider_id": "openai",
					"model":       "gpt-4o-mini",
					"updated_at":  time.Now().UTC().Format(time.RFC3339),
				},
			},
		}
		st.Chats[chat.ID] = chat
		st.Histories[chat.ID] = []domain.RuntimeMessage{}
		return nil
	}); err != nil {
		t.Fatalf("seed store failed: %v", err)
	}

	svc := NewService(Dependencies{
		Store: adapters.NewRepoStateStore(store),
	})

	slot, err := svc.ResolveSessionModel("s-selfops", "u-selfops", "console")
	if err != nil {
		t.Fatalf("ResolveSessionModel failed: %v", err)
	}
	if slot.ProviderID != "openai" {
		t.Fatalf("provider_id=%q, want=openai", slot.ProviderID)
	}
	if slot.Model != "gpt-4o-mini" {
		t.Fatalf("model=%q, want=gpt-4o-mini", slot.Model)
	}
}

func TestPreviewApplyMutationSuccess(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	workspace := newWorkspaceFixture(map[string]interface{}{
		"prompts/selfops-test.md": map[string]string{"content": "before"},
	})
	svc := NewService(Dependencies{
		Store: adapters.NewRepoStateStore(store),
		GetWorkspaceFile: func(path string) (interface{}, error) {
			return workspace.Get(path)
		},
		PutWorkspaceFile: func(path string, body []byte) error {
			return workspace.Put(path, body)
		},
	})

	preview, err := svc.PreviewMutation(PreviewMutationInput{
		Target: TargetWorkspaceFile,
		Operations: []MutationOperation{
			{
				Kind:  OperationReplace,
				Path:  "prompts/selfops-test.md",
				Value: "after",
			},
		},
	})
	if err != nil {
		t.Fatalf("PreviewMutation failed: %v", err)
	}
	if preview.MutationID == "" || preview.ConfirmHash == "" {
		t.Fatalf("preview missing mutation id/hash: %+v", preview)
	}

	apply, err := svc.ApplyMutation(ApplyMutationInput{
		MutationID:  preview.MutationID,
		ConfirmHash: preview.ConfirmHash,
	})
	if err != nil {
		t.Fatalf("ApplyMutation failed: %v", err)
	}
	if !apply.Applied {
		t.Fatalf("expected applied=true")
	}

	got, err := workspace.Get("prompts/selfops-test.md")
	if err != nil {
		t.Fatalf("workspace get failed: %v", err)
	}
	content := ""
	switch value := got.(type) {
	case map[string]string:
		content = value["content"]
	case map[string]interface{}:
		if text, ok := value["content"].(string); ok {
			content = text
		}
	default:
		t.Fatalf("unexpected workspace payload type: %T", got)
	}
	if content != "after" {
		t.Fatalf("content=%q, want=after", content)
	}
}

func TestApplyMutationRejectsExpiredMutation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	workspace := newWorkspaceFixture(map[string]interface{}{
		"prompts/selfops-expired.md": map[string]string{"content": "before"},
	})
	now := time.Now().UTC()
	svc := NewService(Dependencies{
		Store: adapters.NewRepoStateStore(store),
		GetWorkspaceFile: func(path string) (interface{}, error) {
			return workspace.Get(path)
		},
		PutWorkspaceFile: func(path string, body []byte) error {
			return workspace.Put(path, body)
		},
		Now: func() time.Time {
			return now
		},
		MutationTTL: 2 * time.Minute,
	})

	preview, err := svc.PreviewMutation(PreviewMutationInput{
		Target: TargetWorkspaceFile,
		Operations: []MutationOperation{
			{
				Kind:  OperationReplace,
				Path:  "prompts/selfops-expired.md",
				Value: "after",
			},
		},
	})
	if err != nil {
		t.Fatalf("PreviewMutation failed: %v", err)
	}
	now = now.Add(3 * time.Minute)

	_, err = svc.ApplyMutation(ApplyMutationInput{
		MutationID:  preview.MutationID,
		ConfirmHash: preview.ConfirmHash,
	})
	serviceErr := (*ServiceError)(nil)
	if !errors.As(err, &serviceErr) {
		t.Fatalf("expected service error, got=%v", err)
	}
	if serviceErr.Code != "mutation_expired" {
		t.Fatalf("code=%q, want=mutation_expired", serviceErr.Code)
	}
}

func TestApplyMutationRejectsHashMismatch(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	workspace := newWorkspaceFixture(map[string]interface{}{
		"prompts/selfops-hash.md": map[string]string{"content": "before"},
	})
	svc := NewService(Dependencies{
		Store: adapters.NewRepoStateStore(store),
		GetWorkspaceFile: func(path string) (interface{}, error) {
			return workspace.Get(path)
		},
		PutWorkspaceFile: func(path string, body []byte) error {
			return workspace.Put(path, body)
		},
	})

	preview, err := svc.PreviewMutation(PreviewMutationInput{
		Target: TargetWorkspaceFile,
		Operations: []MutationOperation{
			{
				Kind:  OperationReplace,
				Path:  "prompts/selfops-hash.md",
				Value: "after",
			},
		},
	})
	if err != nil {
		t.Fatalf("PreviewMutation failed: %v", err)
	}

	_, err = svc.ApplyMutation(ApplyMutationInput{
		MutationID:  preview.MutationID,
		ConfirmHash: "wrong-hash",
	})
	serviceErr := (*ServiceError)(nil)
	if !errors.As(err, &serviceErr) {
		t.Fatalf("expected service error, got=%v", err)
	}
	if serviceErr.Code != "mutation_hash_mismatch" {
		t.Fatalf("code=%q, want=mutation_hash_mismatch", serviceErr.Code)
	}
}

func TestApplyMutationRejectsDeniedPath(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	workspace := newWorkspaceFixture(map[string]interface{}{})
	svc := NewService(Dependencies{
		Store: adapters.NewRepoStateStore(store),
		GetWorkspaceFile: func(path string) (interface{}, error) {
			return workspace.Get(path)
		},
		PutWorkspaceFile: func(path string, body []byte) error {
			return workspace.Put(path, body)
		},
	})

	preview, err := svc.PreviewMutation(PreviewMutationInput{
		Target: TargetWorkspaceFile,
		Operations: []MutationOperation{
			{
				Kind:  OperationReplace,
				Path:  "secrets/config.txt",
				Value: "blocked",
			},
		},
	})
	if err != nil {
		t.Fatalf("PreviewMutation failed: %v", err)
	}
	if preview.Checks.PathWhitelistPassed {
		t.Fatalf("expected path whitelist check to fail")
	}

	_, err = svc.ApplyMutation(ApplyMutationInput{
		MutationID:  preview.MutationID,
		ConfirmHash: preview.ConfirmHash,
	})
	serviceErr := (*ServiceError)(nil)
	if !errors.As(err, &serviceErr) {
		t.Fatalf("expected service error, got=%v", err)
	}
	if serviceErr.Code != "mutation_path_denied" {
		t.Fatalf("code=%q, want=mutation_path_denied", serviceErr.Code)
	}
}

func TestApplyMutationRejectsSensitiveChangeWithoutAllow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	svc := NewService(Dependencies{
		Store: adapters.NewRepoStateStore(store),
		GetWorkspaceFile: func(path string) (interface{}, error) {
			return map[string]string{"content": ""}, nil
		},
		PutWorkspaceFile: func(path string, body []byte) error {
			return nil
		},
	})

	preview, err := svc.PreviewMutation(PreviewMutationInput{
		Target: TargetProviderConfig,
		Operations: []MutationOperation{
			{
				Kind: OperationJSONPatch,
				Patch: []JSONPatchOperation{
					{Op: "add", Path: "/openai/api_key", Value: "sk-test-123"},
				},
			},
		},
		AllowSensitive: false,
	})
	if err != nil {
		t.Fatalf("PreviewMutation failed: %v", err)
	}
	if !preview.RequiresSensitiveAllow {
		t.Fatalf("expected sensitive preview to require allow_sensitive")
	}

	_, err = svc.ApplyMutation(ApplyMutationInput{
		MutationID:     preview.MutationID,
		ConfirmHash:    preview.ConfirmHash,
		AllowSensitive: false,
	})
	serviceErr := (*ServiceError)(nil)
	if !errors.As(err, &serviceErr) {
		t.Fatalf("expected service error, got=%v", err)
	}
	if serviceErr.Code != "mutation_sensitive_denied" {
		t.Fatalf("code=%q, want=mutation_sensitive_denied", serviceErr.Code)
	}
}

func newTestStore(t *testing.T) *repo.Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "nextai-selfops-service-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := repo.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

type workspaceFixture struct {
	mu    sync.Mutex
	files map[string]interface{}
}

func newWorkspaceFixture(files map[string]interface{}) *workspaceFixture {
	out := map[string]interface{}{}
	for path, value := range files {
		out[path] = cloneJSONValue(value)
	}
	return &workspaceFixture{files: out}
}

func (f *workspaceFixture) Get(path string) (interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return cloneJSONValue(value), nil
}

func (f *workspaceFixture) Put(path string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var textReq map[string]string
	if err := json.Unmarshal(body, &textReq); err == nil {
		if content, ok := textReq["content"]; ok {
			f.files[path] = map[string]string{"content": content}
			return nil
		}
	}
	var generic interface{}
	if err := json.Unmarshal(body, &generic); err != nil {
		return err
	}
	f.files[path] = generic
	return nil
}

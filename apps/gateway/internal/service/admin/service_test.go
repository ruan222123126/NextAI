package admin

import (
	"errors"
	"testing"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/adapters"
)

func TestReplaceEnvsRejectsEmptyKey(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	_, err := svc.ReplaceEnvs(map[string]string{"": "x"})
	validation := (*ValidationError)(nil)
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got=%v", err)
	}
	if validation.Code != "invalid_env_key" {
		t.Fatalf("validation code=%s", validation.Code)
	}
}

func TestCreateAndLoadSkillFile(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	created, err := svc.CreateSkill(CreateSkillInput{
		Name:    "hello",
		Content: "body",
		References: map[string]interface{}{
			"docs": map[string]interface{}{
				"intro.md": "hello-ref",
			},
		},
	})
	if err != nil {
		t.Fatalf("create skill failed: %v", err)
	}
	if !created {
		t.Fatal("created should be true")
	}

	content, found, err := svc.LoadSkillFile("hello", "references/docs/intro.md")
	if err != nil {
		t.Fatalf("load skill file failed: %v", err)
	}
	if !found {
		t.Fatal("skill file should exist")
	}
	if content != "hello-ref" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestReplaceChannelsRejectsUnsupported(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	_, err := svc.ReplaceChannels(domain.ChannelConfigMap{
		"sms": {},
	})
	validation := (*ValidationError)(nil)
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got=%v", err)
	}
	if validation.Code != "channel_not_supported" {
		t.Fatalf("validation code=%s", validation.Code)
	}
}

func TestSetSkillEnabledNotFound(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	found, err := svc.SetSkillEnabled("not-exists", true)
	if err != nil {
		t.Fatalf("set skill enabled failed: %v", err)
	}
	if found {
		t.Fatal("expected skill not found")
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()

	dir := t.TempDir()
	store, err := repo.NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	return NewService(Dependencies{
		Store:   adapters.NewRepoStateStore(store),
		DataDir: dir,
		SupportedChannels: map[string]struct{}{
			"console": {},
			"webhook": {},
		},
	})
}

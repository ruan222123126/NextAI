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

func TestListChannelsReturnsDeepCopy(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	_, err := svc.ReplaceChannels(domain.ChannelConfigMap{
		"webhook": {
			"enabled": true,
			"headers": map[string]interface{}{"X-Test": "v1"},
		},
	})
	if err != nil {
		t.Fatalf("replace channels failed: %v", err)
	}

	channels, err := svc.ListChannels()
	if err != nil {
		t.Fatalf("list channels failed: %v", err)
	}
	webhook := channels["webhook"]
	webhook["enabled"] = false
	headers, ok := webhook["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("headers type=%T", webhook["headers"])
	}
	headers["X-Test"] = "mutated"

	got, found, err := svc.GetChannel("webhook")
	if err != nil {
		t.Fatalf("get channel failed: %v", err)
	}
	if !found {
		t.Fatal("expected webhook channel exists")
	}
	if enabled, _ := got["enabled"].(bool); !enabled {
		t.Fatalf("expected enabled=true, got=%v", got["enabled"])
	}
	gotHeaders, ok := got["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("stored headers type=%T", got["headers"])
	}
	if value := gotHeaders["X-Test"]; value != "v1" {
		t.Fatalf("expected X-Test=v1, got=%v", value)
	}
}

func TestGetChannelReturnsDeepCopy(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	if _, err := svc.ReplaceChannels(domain.ChannelConfigMap{
		"webhook": {
			"enabled": true,
			"url":     "http://example.com",
		},
	}); err != nil {
		t.Fatalf("replace channels failed: %v", err)
	}

	first, found, err := svc.GetChannel("webhook")
	if err != nil {
		t.Fatalf("get channel failed: %v", err)
	}
	if !found {
		t.Fatal("expected webhook channel exists")
	}
	first["url"] = "http://mutated.example.com"

	second, found, err := svc.GetChannel("webhook")
	if err != nil {
		t.Fatalf("get channel second failed: %v", err)
	}
	if !found {
		t.Fatal("expected webhook channel exists")
	}
	if second["url"] != "http://example.com" {
		t.Fatalf("expected url preserved, got=%v", second["url"])
	}
}

func TestReplaceChannelsStoresClonedInput(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	input := domain.ChannelConfigMap{
		"webhook": {
			"enabled": true,
			"headers": map[string]interface{}{"X-Test": "v1"},
		},
	}
	if _, err := svc.ReplaceChannels(input); err != nil {
		t.Fatalf("replace channels failed: %v", err)
	}

	input["webhook"]["enabled"] = false
	headers, ok := input["webhook"]["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("input headers type=%T", input["webhook"]["headers"])
	}
	headers["X-Test"] = "mutated"

	got, found, err := svc.GetChannel("webhook")
	if err != nil {
		t.Fatalf("get channel failed: %v", err)
	}
	if !found {
		t.Fatal("expected webhook channel exists")
	}
	if enabled, _ := got["enabled"].(bool); !enabled {
		t.Fatalf("expected enabled=true, got=%v", got["enabled"])
	}
	gotHeaders, ok := got["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("stored headers type=%T", got["headers"])
	}
	if value := gotHeaders["X-Test"]; value != "v1" {
		t.Fatalf("expected X-Test=v1, got=%v", value)
	}
}

func TestListSkillsReturnsDeepCopy(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	created, err := svc.CreateSkill(CreateSkillInput{
		Name:    "copy-check",
		Content: "body",
		References: map[string]interface{}{
			"docs": map[string]interface{}{
				"intro.md": "origin",
			},
		},
	})
	if err != nil {
		t.Fatalf("create skill failed: %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	skills, err := svc.ListSkills(false)
	if err != nil {
		t.Fatalf("list skills failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected one skill, got=%d", len(skills))
	}
	docs, ok := skills[0].References["docs"].(map[string]interface{})
	if !ok {
		t.Fatalf("docs type=%T", skills[0].References["docs"])
	}
	docs["intro.md"] = "mutated"

	content, found, err := svc.LoadSkillFile("copy-check", "references/docs/intro.md")
	if err != nil {
		t.Fatalf("load skill file failed: %v", err)
	}
	if !found {
		t.Fatal("expected skill file exists")
	}
	if content != "origin" {
		t.Fatalf("expected original content, got=%q", content)
	}
}

func TestPutChannelStoresClonedInput(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	input := map[string]interface{}{
		"enabled": true,
		"headers": map[string]interface{}{"X-Test": "v1"},
	}
	if err := svc.PutChannel("webhook", input); err != nil {
		t.Fatalf("put channel failed: %v", err)
	}

	input["enabled"] = false
	headers, ok := input["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("input headers type=%T", input["headers"])
	}
	headers["X-Test"] = "mutated"

	got, found, err := svc.GetChannel("webhook")
	if err != nil {
		t.Fatalf("get channel failed: %v", err)
	}
	if !found {
		t.Fatal("expected webhook channel exists")
	}
	if enabled, _ := got["enabled"].(bool); !enabled {
		t.Fatalf("expected enabled=true, got=%v", got["enabled"])
	}
	gotHeaders, ok := got["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("stored headers type=%T", got["headers"])
	}
	if value := gotHeaders["X-Test"]; value != "v1" {
		t.Fatalf("expected X-Test=v1, got=%v", value)
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

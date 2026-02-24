package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"nextai/apps/gateway/internal/domain"
)

func TestBootstrapSessionEndpointCreatesChatAndFirstMessage(t *testing.T) {
	srv := newTestServer(t)

	body := `{
		"user_id":"u-selfops-bootstrap",
		"session_seed":"s-selfops-bootstrap",
		"first_input":"hello selfops bootstrap"
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/self/sessions/bootstrap", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Chat         domain.ChatSpec        `json:"chat"`
		Reply        string                 `json:"reply"`
		AppliedModel domain.ModelSlotConfig `json:"applied_model"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode bootstrap response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Chat.SessionID != "s-selfops-bootstrap" {
		t.Fatalf("session_id=%q, want=s-selfops-bootstrap", resp.Chat.SessionID)
	}
	if resp.Chat.UserID != "u-selfops-bootstrap" {
		t.Fatalf("user_id=%q, want=u-selfops-bootstrap", resp.Chat.UserID)
	}
	if resp.Chat.Channel != "console" {
		t.Fatalf("channel=%q, want=console", resp.Chat.Channel)
	}
	if !strings.Contains(resp.Reply, "Echo: hello selfops bootstrap") {
		t.Fatalf("unexpected reply: %q", resp.Reply)
	}
	if resp.AppliedModel.ProviderID == "" || resp.AppliedModel.Model == "" {
		t.Fatalf("expected non-empty applied_model, got=%+v", resp.AppliedModel)
	}

	listW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-selfops-bootstrap&channel=console", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list chats status=%d body=%s", listW.Code, listW.Body.String())
	}
	var chats []domain.ChatSpec
	if err := json.Unmarshal(listW.Body.Bytes(), &chats); err != nil {
		t.Fatalf("decode chats failed: %v body=%s", err, listW.Body.String())
	}
	if len(chats) != 1 {
		t.Fatalf("expected one chat, got=%d body=%s", len(chats), listW.Body.String())
	}

	historyW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(historyW, httptest.NewRequest(http.MethodGet, "/chats/"+resp.Chat.ID, nil))
	if historyW.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", historyW.Code, historyW.Body.String())
	}
	var history domain.ChatHistory
	if err := json.Unmarshal(historyW.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history failed: %v body=%s", err, historyW.Body.String())
	}
	if len(history.Messages) < 2 {
		t.Fatalf("expected >=2 history messages, got=%d body=%s", len(history.Messages), historyW.Body.String())
	}
}

func TestSetSessionModelEndpointOnlyAffectsTargetSession(t *testing.T) {
	srv := newTestServer(t)

	bootstrap1 := `{"user_id":"u-selfops-model","session_seed":"s-model-1","first_input":"hello one"}`
	bootstrap2 := `{"user_id":"u-selfops-model","session_seed":"s-model-2","first_input":"hello two"}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/agent/self/sessions/bootstrap", strings.NewReader(bootstrap1)))
	if w1.Code != http.StatusOK {
		t.Fatalf("bootstrap1 status=%d body=%s", w1.Code, w1.Body.String())
	}
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/agent/self/sessions/bootstrap", strings.NewReader(bootstrap2)))
	if w2.Code != http.StatusOK {
		t.Fatalf("bootstrap2 status=%d body=%s", w2.Code, w2.Body.String())
	}

	setModelReq := `{
		"user_id":"u-selfops-model",
		"channel":"console",
		"provider_id":"openai",
		"model":"gpt-4o-mini"
	}`
	setW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(setW, httptest.NewRequest(http.MethodPut, "/agent/self/sessions/s-model-1/model", strings.NewReader(setModelReq)))
	if setW.Code != http.StatusOK {
		t.Fatalf("set session model status=%d body=%s", setW.Code, setW.Body.String())
	}

	listW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-selfops-model&channel=console", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list chats status=%d body=%s", listW.Code, listW.Body.String())
	}
	var chats []domain.ChatSpec
	if err := json.Unmarshal(listW.Body.Bytes(), &chats); err != nil {
		t.Fatalf("decode chats failed: %v body=%s", err, listW.Body.String())
	}
	if len(chats) != 2 {
		t.Fatalf("expected 2 chats, got=%d body=%s", len(chats), listW.Body.String())
	}

	chatBySession := map[string]domain.ChatSpec{}
	for _, chat := range chats {
		chatBySession[chat.SessionID] = chat
	}
	chatOne, ok := chatBySession["s-model-1"]
	if !ok {
		t.Fatalf("missing chat for s-model-1")
	}
	chatTwo, ok := chatBySession["s-model-2"]
	if !ok {
		t.Fatalf("missing chat for s-model-2")
	}

	overrideRaw, ok := chatOne.Meta[domain.ChatMetaActiveLLM].(map[string]interface{})
	if !ok {
		t.Fatalf("expected active_llm_override on target session, got=%#v", chatOne.Meta[domain.ChatMetaActiveLLM])
	}
	if overrideRaw["provider_id"] != "openai" {
		t.Fatalf("provider_id=%#v, want=openai", overrideRaw["provider_id"])
	}
	if overrideRaw["model"] != "gpt-4o-mini" {
		t.Fatalf("model=%#v, want=gpt-4o-mini", overrideRaw["model"])
	}
	if _, exists := chatTwo.Meta[domain.ChatMetaActiveLLM]; exists {
		t.Fatalf("non-target session should not contain active_llm_override, meta=%#v", chatTwo.Meta)
	}
}

func TestConfigMutationsPreviewApplyEndToEnd(t *testing.T) {
	srv := newTestServer(t)
	relPath, absPath := newPromptTemplateTestPath(t, "selfops-mutation")
	if err := os.WriteFile(absPath, []byte("before mutation"), 0o644); err != nil {
		t.Fatalf("seed prompt file failed: %v", err)
	}

	previewReq := `{
		"target":"workspace_file",
		"operations":[
			{"kind":"replace","path":"` + relPath + `","value":"after mutation"}
		]
	}`
	previewW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(previewW, httptest.NewRequest(http.MethodPost, "/agent/self/config-mutations/preview", strings.NewReader(previewReq)))
	if previewW.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewW.Code, previewW.Body.String())
	}
	var previewResp struct {
		MutationID  string `json:"mutation_id"`
		ConfirmHash string `json:"confirm_hash"`
	}
	if err := json.Unmarshal(previewW.Body.Bytes(), &previewResp); err != nil {
		t.Fatalf("decode preview failed: %v body=%s", err, previewW.Body.String())
	}
	if previewResp.MutationID == "" || previewResp.ConfirmHash == "" {
		t.Fatalf("preview response missing mutation id/hash: %+v", previewResp)
	}

	applyReq := `{
		"mutation_id":"` + previewResp.MutationID + `",
		"confirm_hash":"` + previewResp.ConfirmHash + `"
	}`
	applyW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(applyW, httptest.NewRequest(http.MethodPost, "/agent/self/config-mutations/apply", strings.NewReader(applyReq)))
	if applyW.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", applyW.Code, applyW.Body.String())
	}

	updated, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read updated file failed: %v", err)
	}
	if string(updated) != "after mutation" {
		t.Fatalf("file content=%q, want=after mutation", string(updated))
	}
}

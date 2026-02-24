package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nextai/apps/gateway/internal/config"
	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/plugin"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	dir, err := os.MkdirTemp("", "nextai-gateway-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return newTestServerWithDataDir(t, dir)
}

func newTestServerWithDataDir(t *testing.T, dataDir string) *Server {
	t.Helper()
	srv, err := NewServer(config.Config{Host: "127.0.0.1", Port: "0", DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

func newToolTestPath(t *testing.T, prefix string) (string, string) {
	t.Helper()
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join("apps/gateway/.data/tool-tests", fmt.Sprintf("%s-%d.txt", prefix, time.Now().UnixNano())))
	abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(abs) })
	return rel, abs
}

func newDocsAITestPath(t *testing.T, prefix string) (string, string) {
	t.Helper()
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join("docs/AI", fmt.Sprintf("%s-%d.md", prefix, time.Now().UnixNano())))
	abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(abs) })
	return rel, abs
}

func newPromptTemplateTestPath(t *testing.T, prefix string) (string, string) {
	t.Helper()
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join("prompts", fmt.Sprintf("%s-%d.md", prefix, time.Now().UnixNano())))
	abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(abs) })
	return rel, abs
}

func writeWebFixture(t *testing.T, baseDir string) string {
	t.Helper()
	webDir := filepath.Join(baseDir, "web")
	assetDir := filepath.Join(webDir, "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	index := "<!doctype html><html><body><div id=\"app\">nextai</div><script src=\"/assets/app.js\"></script></body></html>"
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "app.js"), []byte("console.log('ok');"), 0o644); err != nil {
		t.Fatal(err)
	}
	return webDir
}

type stubToolPlugin struct {
	name   string
	invoke func(input map[string]interface{}) (map[string]interface{}, error)
}

func (p *stubToolPlugin) Name() string {
	return p.name
}

func (p *stubToolPlugin) Invoke(command plugin.ToolCommand) (plugin.ToolResult, error) {
	input, err := command.ToMap()
	if err != nil {
		return plugin.ToolResult{}, err
	}
	if p.invoke == nil {
		return plugin.NewToolResult(map[string]interface{}{"ok": true}), nil
	}
	out, err := p.invoke(input)
	if err != nil {
		return plugin.ToolResult{}, err
	}
	return plugin.NewToolResult(out), nil
}

func collectSystemMessagesFromModelRequest(t *testing.T, requestBody map[string]interface{}) []string {
	t.Helper()
	rawMessages, ok := requestBody["messages"].([]interface{})
	if !ok {
		t.Fatalf("model request missing messages: %#v", requestBody["messages"])
	}
	out := make([]string, 0, len(rawMessages))
	for _, item := range rawMessages {
		message, _ := item.(map[string]interface{})
		role, _ := message["role"].(string)
		content, _ := message["content"].(string)
		if role == "system" {
			out = append(out, content)
		}
	}
	return out
}

func collectToolNamesFromModelRequest(t *testing.T, requestBody map[string]interface{}) []string {
	t.Helper()
	rawTools, ok := requestBody["tools"].([]interface{})
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(rawTools))
	for _, rawTool := range rawTools {
		toolObj, _ := rawTool.(map[string]interface{})
		functionObj, _ := toolObj["function"].(map[string]interface{})
		name, _ := functionObj["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func configureOpenAIProviderForTest(t *testing.T, srv *Server, baseURL string) {
	t.Helper()

	configProvider := `{"api_key":"sk-test","base_url":"` + baseURL + `"}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config provider status=%d body=%s", w1.Code, w1.Body.String())
	}
	setActive := `{"provider_id":"openai","model":"gpt-4o-mini"}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w2.Code != http.StatusOK {
		t.Fatalf("set active status=%d body=%s", w2.Code, w2.Body.String())
	}
}

func intFromAny(raw interface{}) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func TestFindRepoRootFallsBackToCurrentWorkingDirectoryWithoutGit(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot returned error: %v", err)
	}
	if root != tmp {
		t.Fatalf("findRepoRoot=%q, want %q", root, tmp)
	}
}

func TestHandlerServesWebStaticFiles(t *testing.T) {
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	tmp := t.TempDir()
	webDir := writeWebFixture(t, tmp)
	srv, err := NewServer(config.Config{
		Host:    "127.0.0.1",
		Port:    "0",
		DataDir: tmp,
		WebDir:  webDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	rootW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rootW, httptest.NewRequest(http.MethodGet, "/", nil))
	if rootW.Code != http.StatusOK {
		t.Fatalf("root status=%d body=%s", rootW.Code, rootW.Body.String())
	}
	if !strings.Contains(rootW.Body.String(), "nextai") {
		t.Fatalf("unexpected root body: %s", rootW.Body.String())
	}

	assetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(assetW, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if assetW.Code != http.StatusOK {
		t.Fatalf("asset status=%d body=%s", assetW.Code, assetW.Body.String())
	}
	if !strings.Contains(assetW.Body.String(), "console.log") {
		t.Fatalf("unexpected asset body: %s", assetW.Body.String())
	}

	spaW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(spaW, httptest.NewRequest(http.MethodGet, "/any/deep/link", nil))
	if spaW.Code != http.StatusOK {
		t.Fatalf("spa fallback status=%d body=%s", spaW.Code, spaW.Body.String())
	}
	if !strings.Contains(spaW.Body.String(), "nextai") {
		t.Fatalf("unexpected spa fallback body: %s", spaW.Body.String())
	}
}

func TestHandlerWebStaticIsPublicWhenAPIKeyEnabled(t *testing.T) {
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	tmp := t.TempDir()
	webDir := writeWebFixture(t, tmp)
	srv, err := NewServer(config.Config{
		Host:    "127.0.0.1",
		Port:    "0",
		DataDir: tmp,
		APIKey:  "secret",
		WebDir:  webDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	rootW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rootW, httptest.NewRequest(http.MethodGet, "/", nil))
	if rootW.Code != http.StatusOK {
		t.Fatalf("root with api key status=%d body=%s", rootW.Code, rootW.Body.String())
	}

	chatsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsW, httptest.NewRequest(http.MethodGet, "/chats", nil))
	if chatsW.Code != http.StatusUnauthorized {
		t.Fatalf("chats without key status=%d body=%s", chatsW.Code, chatsW.Body.String())
	}
}

type streamingProbeWriter struct {
	header      http.Header
	status      int
	body        strings.Builder
	signal      chan struct{}
	signalOnce  sync.Once
	mutex       sync.Mutex
	wroteHeader bool
}

func newStreamingProbeWriter() *streamingProbeWriter {
	return &streamingProbeWriter{
		header: make(http.Header),
		signal: make(chan struct{}, 1),
		status: http.StatusOK,
	}
}

func (w *streamingProbeWriter) Header() http.Header {
	return w.header
}

func (w *streamingProbeWriter) WriteHeader(statusCode int) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.status = statusCode
	w.wroteHeader = true
}

func (w *streamingProbeWriter) Write(p []byte) (int, error) {
	w.mutex.Lock()
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	n, err := w.body.Write(p)
	w.mutex.Unlock()
	w.notify()
	return n, err
}

func (w *streamingProbeWriter) Flush() {
	w.notify()
}

func (w *streamingProbeWriter) BodyString() string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.body.String()
}

func (w *streamingProbeWriter) notify() {
	w.signalOnce.Do(func() {
		select {
		case w.signal <- struct{}{}:
		default:
		}
	})
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}
}

func TestRuntimeConfigEndpointReflectsFeatureFlags(t *testing.T) {
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	dir := t.TempDir()
	srv, err := NewServer(config.Config{
		Host:                          "127.0.0.1",
		Port:                          "0",
		DataDir:                       dir,
		EnablePromptTemplates:         true,
		EnablePromptContextIntrospect: false,
		EnableCodexModeV2:             true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/runtime-config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("runtime config status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Features struct {
			PromptTemplates         bool `json:"prompt_templates"`
			PromptContextIntrospect bool `json:"prompt_context_introspect"`
			CodexModeV2             bool `json:"codex_mode_v2"`
		} `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode runtime config failed: %v body=%s", err, w.Body.String())
	}
	if !resp.Features.PromptTemplates {
		t.Fatalf("expected prompt_templates=true, body=%s", w.Body.String())
	}
	if resp.Features.PromptContextIntrospect {
		t.Fatalf("expected prompt_context_introspect=false, body=%s", w.Body.String())
	}
	if !resp.Features.CodexModeV2 {
		t.Fatalf("expected codex_mode_v2=true, body=%s", w.Body.String())
	}
}

func TestRuntimeConfigEndpointBypassesAPIKeyAuth(t *testing.T) {
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	dir := t.TempDir()
	srv, err := NewServer(config.Config{
		Host:                          "127.0.0.1",
		Port:                          "0",
		DataDir:                       dir,
		APIKey:                        "secret-token",
		EnablePromptTemplates:         false,
		EnablePromptContextIntrospect: true,
		EnableCodexModeV2:             false,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	runtimeConfigW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(runtimeConfigW, httptest.NewRequest(http.MethodGet, "/runtime-config", nil))
	if runtimeConfigW.Code != http.StatusOK {
		t.Fatalf("runtime config should bypass auth, got=%d body=%s", runtimeConfigW.Code, runtimeConfigW.Body.String())
	}

	var resp struct {
		Features struct {
			PromptTemplates         bool `json:"prompt_templates"`
			PromptContextIntrospect bool `json:"prompt_context_introspect"`
			CodexModeV2             bool `json:"codex_mode_v2"`
		} `json:"features"`
	}
	if err := json.Unmarshal(runtimeConfigW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode runtime config failed: %v body=%s", err, runtimeConfigW.Body.String())
	}
	if resp.Features.PromptTemplates {
		t.Fatalf("expected prompt_templates=false, body=%s", runtimeConfigW.Body.String())
	}
	if !resp.Features.PromptContextIntrospect {
		t.Fatalf("expected prompt_context_introspect=true, body=%s", runtimeConfigW.Body.String())
	}
	if resp.Features.CodexModeV2 {
		t.Fatalf("expected codex_mode_v2=false, body=%s", runtimeConfigW.Body.String())
	}

	chatsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsW, httptest.NewRequest(http.MethodGet, "/chats", nil))
	if chatsW.Code != http.StatusUnauthorized {
		t.Fatalf("chats without key should still require auth, got=%d body=%s", chatsW.Code, chatsW.Body.String())
	}
}

func TestMapToolErrorShellExecutorUnavailable(t *testing.T) {
	status, code, message := mapToolError(&toolError{
		Code: "tool_invoke_failed",
		Err:  plugin.ErrShellToolExecutorUnavailable,
	})
	if status != http.StatusBadGateway {
		t.Fatalf("expected status 502, got=%d", status)
	}
	if code != "tool_runtime_unavailable" {
		t.Fatalf("unexpected code: %q", code)
	}
	if message != "shell executor is unavailable on current host" {
		t.Fatalf("unexpected message: %q", message)
	}
}

func TestAPIKeyAuthMiddleware(t *testing.T) {
	dir, err := os.MkdirTemp("", "nextai-gateway-auth-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	srv, err := NewServer(config.Config{Host: "127.0.0.1", Port: "0", DataDir: dir, APIKey: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(healthW, healthReq)
	if healthW.Code != http.StatusOK {
		t.Fatalf("health endpoint should bypass auth, got=%d", healthW.Code)
	}

	noAuthReq := httptest.NewRequest(http.MethodGet, "/chats", nil)
	noAuthW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(noAuthW, noAuthReq)
	if noAuthW.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got=%d body=%s", noAuthW.Code, noAuthW.Body.String())
	}
	if !strings.Contains(noAuthW.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unexpected unauthorized body: %s", noAuthW.Body.String())
	}

	apiKeyReq := httptest.NewRequest(http.MethodGet, "/chats", nil)
	apiKeyReq.Header.Set("X-API-Key", "secret-token")
	apiKeyW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(apiKeyW, apiKeyReq)
	if apiKeyW.Code != http.StatusOK {
		t.Fatalf("expected authorized status via X-API-Key, got=%d body=%s", apiKeyW.Code, apiKeyW.Body.String())
	}

	bearerReq := httptest.NewRequest(http.MethodGet, "/chats", nil)
	bearerReq.Header.Set("Authorization", "Bearer secret-token")
	bearerW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(bearerW, bearerReq)
	if bearerW.Code != http.StatusOK {
		t.Fatalf("expected authorized status via bearer, got=%d body=%s", bearerW.Code, bearerW.Body.String())
	}
}

func TestChatCreateAndGetHistory(t *testing.T) {
	srv := newTestServer(t)

	createReq := `{"name":"A","session_id":"s1","user_id":"u1","channel":"console","meta":{}}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(createReq)))
	if w1.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w1.Code, w1.Body.String())
	}

	var created map[string]interface{}
	if err := json.Unmarshal(w1.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	chatID, _ := created["id"].(string)
	if chatID == "" {
		t.Fatalf("empty chat id")
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello"}]}],"session_id":"s1","user_id":"u1","channel":"console","stream":false}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w2.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w2.Code, w2.Body.String())
	}

	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/chats/"+chatID, nil))
	if w3.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", w3.Code, w3.Body.String())
	}
	if !strings.Contains(w3.Body.String(), "assistant") {
		t.Fatalf("history should contain assistant message: %s", w3.Body.String())
	}
}

func TestListChatsContainsDefaultChat(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/chats?user_id=demo-user&channel=console", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list chats status=%d body=%s", w.Code, w.Body.String())
	}

	var chats []domain.ChatSpec
	if err := json.Unmarshal(w.Body.Bytes(), &chats); err != nil {
		t.Fatalf("decode chats failed: %v body=%s", err, w.Body.String())
	}

	var defaultChat *domain.ChatSpec
	for i := range chats {
		if chats[i].ID == domain.DefaultChatID {
			defaultChat = &chats[i]
			break
		}
	}
	if defaultChat == nil {
		t.Fatalf("default chat should exist in list: %s", w.Body.String())
	}
	if defaultChat.SessionID != domain.DefaultChatSessionID {
		t.Fatalf("unexpected default chat session_id: %q", defaultChat.SessionID)
	}
	if defaultChat.UserID != domain.DefaultChatUserID {
		t.Fatalf("unexpected default chat user_id: %q", defaultChat.UserID)
	}
	if defaultChat.Channel != domain.DefaultChatChannel {
		t.Fatalf("unexpected default chat channel: %q", defaultChat.Channel)
	}
	flag, ok := defaultChat.Meta[domain.ChatMetaSystemDefault].(bool)
	if !ok || !flag {
		t.Fatalf("default chat should have meta.system_default=true, meta=%#v", defaultChat.Meta)
	}
}

func TestDeleteDefaultChatRejected(t *testing.T) {
	srv := newTestServer(t)

	deleteW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(deleteW, httptest.NewRequest(http.MethodDelete, "/chats/"+domain.DefaultChatID, nil))
	if deleteW.Code != http.StatusBadRequest {
		t.Fatalf("delete default chat status=%d body=%s", deleteW.Code, deleteW.Body.String())
	}
	if !strings.Contains(deleteW.Body.String(), `"code":"default_chat_protected"`) {
		t.Fatalf("unexpected delete error body: %s", deleteW.Body.String())
	}

	batchDeleteW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(batchDeleteW, httptest.NewRequest(http.MethodPost, "/chats/batch-delete", strings.NewReader(`["`+domain.DefaultChatID+`"]`)))
	if batchDeleteW.Code != http.StatusBadRequest {
		t.Fatalf("batch delete default chat status=%d body=%s", batchDeleteW.Code, batchDeleteW.Body.String())
	}
	if !strings.Contains(batchDeleteW.Body.String(), `"code":"default_chat_protected"`) {
		t.Fatalf("unexpected batch delete error body: %s", batchDeleteW.Body.String())
	}

	listW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/chats?user_id=demo-user&channel=console", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list chats status=%d body=%s", listW.Code, listW.Body.String())
	}
	if !strings.Contains(listW.Body.String(), `"id":"`+domain.DefaultChatID+`"`) {
		t.Fatalf("default chat should still exist after delete attempts: %s", listW.Body.String())
	}
}

func TestListCronJobsContainsDefaultCronJob(t *testing.T) {
	srv := newTestServer(t)

	listW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/cron/jobs", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list cron jobs status=%d body=%s", listW.Code, listW.Body.String())
	}

	var jobs []domain.CronJobSpec
	if err := json.Unmarshal(listW.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("decode cron jobs failed: %v body=%s", err, listW.Body.String())
	}

	var defaultJob *domain.CronJobSpec
	for i := range jobs {
		if jobs[i].ID == domain.DefaultCronJobID {
			defaultJob = &jobs[i]
			break
		}
	}
	if defaultJob == nil {
		t.Fatalf("default cron job should exist in list: %s", listW.Body.String())
	}
	if defaultJob.TaskType != "text" {
		t.Fatalf("unexpected default cron task_type: %q", defaultJob.TaskType)
	}
	if defaultJob.Text != domain.DefaultCronJobText {
		t.Fatalf("unexpected default cron text: %q", defaultJob.Text)
	}
	if defaultJob.Enabled {
		t.Fatalf("default cron job should be disabled by default")
	}
	flag, ok := defaultJob.Meta[domain.CronMetaSystemDefault].(bool)
	if !ok || !flag {
		t.Fatalf("default cron should have meta.system_default=true, meta=%#v", defaultJob.Meta)
	}
}

func TestDeleteDefaultCronJobRejected(t *testing.T) {
	srv := newTestServer(t)

	deleteW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(deleteW, httptest.NewRequest(http.MethodDelete, "/cron/jobs/"+domain.DefaultCronJobID, nil))
	if deleteW.Code != http.StatusBadRequest {
		t.Fatalf("delete default cron status=%d body=%s", deleteW.Code, deleteW.Body.String())
	}
	if !strings.Contains(deleteW.Body.String(), `"code":"default_cron_protected"`) {
		t.Fatalf("unexpected delete default cron body: %s", deleteW.Body.String())
	}

	listW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/cron/jobs", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list cron jobs status=%d body=%s", listW.Code, listW.Body.String())
	}
	if !strings.Contains(listW.Body.String(), `"id":"`+domain.DefaultCronJobID+`"`) {
		t.Fatalf("default cron job should still exist after delete attempt: %s", listW.Body.String())
	}
}
func TestProcessAgentReusesChatHistoryContext(t *testing.T) {
	srv := newTestServer(t)

	firstReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"1+1等于几"}]}],"session_id":"s-context","user_id":"u-context","channel":"console","stream":false}`
	firstW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(firstW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(firstReq)))
	if firstW.Code != http.StatusOK {
		t.Fatalf("first process status=%d body=%s", firstW.Code, firstW.Body.String())
	}

	secondReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"把你之前回答的数学问题再回答一次"}]}],"session_id":"s-context","user_id":"u-context","channel":"console","stream":false}`
	secondW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(secondW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(secondReq)))
	if secondW.Code != http.StatusOK {
		t.Fatalf("second process status=%d body=%s", secondW.Code, secondW.Body.String())
	}

	var secondResp domain.AgentProcessResponse
	if err := json.Unmarshal(secondW.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second response failed: %v body=%s", err, secondW.Body.String())
	}
	if !strings.Contains(secondResp.Reply, "1+1等于几") {
		t.Fatalf("expected second reply to include previous user context, got=%q", secondResp.Reply)
	}
	if !strings.Contains(secondResp.Reply, "把你之前回答的数学问题再回答一次") {
		t.Fatalf("expected second reply to include latest user input, got=%q", secondResp.Reply)
	}
}

func TestProcessAgentPersistsToolCallNoticesInHistory(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "history-tool-call")
	if err := os.WriteFile(absPath, []byte("line-1\nline-2\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	createReq := `{"name":"A","session_id":"s-history-tool","user_id":"u-history-tool","channel":"console","meta":{}}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(createReq)))
	if w1.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w1.Code, w1.Body.String())
	}
	var created map[string]interface{}
	if err := json.Unmarshal(w1.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	chatID, _ := created["id"].(string)
	if strings.TrimSpace(chatID) == "" {
		t.Fatalf("empty chat id: %v", created)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view history tool call"}]}],
		"session_id":"s-history-tool",
		"user_id":"u-history-tool",
		"channel":"console",
		"stream":false,
		"view":[{"path":%q,"start":1,"end":1}]
	}`, absPath)
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w2.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w2.Code, w2.Body.String())
	}

	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/chats/"+chatID, nil))
	if w3.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", w3.Code, w3.Body.String())
	}

	var history domain.ChatHistory
	if err := json.Unmarshal(w3.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history failed: %v body=%s", err, w3.Body.String())
	}
	if len(history.Messages) == 0 {
		t.Fatalf("expected non-empty history, body=%s", w3.Body.String())
	}
	assistant := history.Messages[len(history.Messages)-1]
	if assistant.Role != "assistant" {
		t.Fatalf("expected assistant message at tail, got=%q", assistant.Role)
	}
	if len(assistant.Metadata) == 0 {
		t.Fatalf("expected assistant metadata, body=%s", w3.Body.String())
	}
	rawNotices, ok := assistant.Metadata["tool_call_notices"].([]interface{})
	if !ok || len(rawNotices) == 0 {
		t.Fatalf("expected tool_call_notices metadata, got=%#v", assistant.Metadata["tool_call_notices"])
	}
	first, _ := rawNotices[0].(map[string]interface{})
	raw, _ := first["raw"].(string)
	if !strings.Contains(raw, `"type":"tool_result"`) || !strings.Contains(raw, `"name":"view"`) || !strings.Contains(raw, `line-1`) {
		t.Fatalf("unexpected persisted tool notice raw: %q", raw)
	}
	toolOrder, ok := assistant.Metadata["tool_order"].(float64)
	if !ok || toolOrder <= 0 {
		t.Fatalf("expected positive tool_order, got=%#v", assistant.Metadata["tool_order"])
	}
	textOrder, ok := assistant.Metadata["text_order"].(float64)
	if !ok || textOrder <= 0 {
		t.Fatalf("expected positive text_order, got=%#v", assistant.Metadata["text_order"])
	}
}

func TestProcessAgentRejectsUnsupportedChannel(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello"}]}],"session_id":"s1","user_id":"u1","channel":"sms","stream":false}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"channel_not_supported"`) {
		t.Fatalf("unexpected error body: %s", w.Body.String())
	}
}

func TestProcessAgentRespectsRequestedChannelForWebSource(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u-web-auto/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	configW := httptest.NewRecorder()
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("config qq status=%d body=%s", configW.Code, configW.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello from web source"}]}],"session_id":"s-web-auto","user_id":"u-web-auto","channel":"qq","stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq))
	req.Header.Set(channelSourceHeader, "web")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	chatsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-web-auto&channel=qq", nil))
	if chatsW.Code != http.StatusOK {
		t.Fatalf("list qq chats status=%d body=%s", chatsW.Code, chatsW.Body.String())
	}
	var qqChats []domain.ChatSpec
	if err := json.Unmarshal(chatsW.Body.Bytes(), &qqChats); err != nil {
		t.Fatalf("decode qq chats failed: %v body=%s", err, chatsW.Body.String())
	}
	if len(qqChats) != 1 {
		t.Fatalf("expected one qq chat, got=%d body=%s", len(qqChats), chatsW.Body.String())
	}
	if qqChats[0].Channel != "qq" {
		t.Fatalf("expected chat channel qq, got=%q", qqChats[0].Channel)
	}

	consoleChatsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(consoleChatsW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-web-auto&channel=console", nil))
	if consoleChatsW.Code != http.StatusOK {
		t.Fatalf("list console chats status=%d body=%s", consoleChatsW.Code, consoleChatsW.Body.String())
	}
	var consoleChats []domain.ChatSpec
	if err := json.Unmarshal(consoleChatsW.Body.Bytes(), &consoleChats); err != nil {
		t.Fatalf("decode console chats failed: %v body=%s", err, consoleChatsW.Body.String())
	}
	if len(consoleChats) != 0 {
		t.Fatalf("expected no console chats, got=%d body=%s", len(consoleChats), consoleChatsW.Body.String())
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := messageCalls.Load(); got != 1 {
		t.Fatalf("expected one qq message call, got=%d", got)
	}
}

func TestProcessAgentDefaultsToConsoleForCLISourceWithoutChannel(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello from cli source"}]}],"session_id":"s-cli-auto","user_id":"u-cli-auto","stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq))
	req.Header.Set(channelSourceHeader, "cli")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	chatsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-cli-auto&channel=console", nil))
	if chatsW.Code != http.StatusOK {
		t.Fatalf("list console chats status=%d body=%s", chatsW.Code, chatsW.Body.String())
	}
	var chats []domain.ChatSpec
	if err := json.Unmarshal(chatsW.Body.Bytes(), &chats); err != nil {
		t.Fatalf("decode chats failed: %v body=%s", err, chatsW.Body.String())
	}
	if len(chats) != 1 {
		t.Fatalf("expected one console chat, got=%d body=%s", len(chats), chatsW.Body.String())
	}
	if chats[0].Channel != "console" {
		t.Fatalf("expected chat channel console, got=%q", chats[0].Channel)
	}
}

func TestProcessAgentDispatchesToWebhookChannel(t *testing.T) {
	var received atomic.Int32
	var gotBody map[string]interface{}
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.Header.Get("X-Test-Token") != "abc123" {
			t.Fatalf("unexpected webhook header: %s", r.Header.Get("X-Test-Token"))
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode webhook body failed: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhook.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"url":"` + webhook.URL + `","headers":{"X-Test-Token":"abc123"}}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/webhook", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello webhook"}]}],"session_id":"s1","user_id":"u1","channel":"webhook","stream":false}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	if got := received.Load(); got != 1 {
		t.Fatalf("expected one webhook call, got=%d", got)
	}
	if gotBody["user_id"] != "u1" {
		t.Fatalf("unexpected webhook user_id: %#v", gotBody["user_id"])
	}
	if gotBody["session_id"] != "s1" {
		t.Fatalf("unexpected webhook session_id: %#v", gotBody["session_id"])
	}
	if text, _ := gotBody["text"].(string); !strings.Contains(text, "Echo: hello webhook") {
		t.Fatalf("unexpected webhook text: %#v", gotBody["text"])
	}
}

func TestProcessAgentQQChannelDispatchesOutboundMessage(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u1/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","bot_prefix":"[BOT] ","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello qq"}]}],"session_id":"s1","user_id":"u1","channel":"qq","stream":false}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Echo: hello qq") {
		t.Fatalf("unexpected process body: %s", w.Body.String())
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := messageCalls.Load(); got != 1 {
		t.Fatalf("expected one qq message call, got=%d", got)
	}
}

func TestProcessAgentNewCommandClearsSessionContext(t *testing.T) {
	srv := newTestServer(t)

	firstReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello before reset"}]}],"session_id":"s-reset","user_id":"u-reset","channel":"console","stream":false}`
	firstW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(firstW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(firstReq)))
	if firstW.Code != http.StatusOK {
		t.Fatalf("first process status=%d body=%s", firstW.Code, firstW.Body.String())
	}

	chatsBeforeResetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsBeforeResetW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-reset&channel=console", nil))
	if chatsBeforeResetW.Code != http.StatusOK {
		t.Fatalf("list chats before reset status=%d body=%s", chatsBeforeResetW.Code, chatsBeforeResetW.Body.String())
	}

	var chatsBeforeReset []domain.ChatSpec
	if err := json.Unmarshal(chatsBeforeResetW.Body.Bytes(), &chatsBeforeReset); err != nil {
		t.Fatalf("decode chats before reset failed: %v body=%s", err, chatsBeforeResetW.Body.String())
	}
	if len(chatsBeforeReset) != 1 {
		t.Fatalf("expected one chat before reset, got=%d body=%s", len(chatsBeforeReset), chatsBeforeResetW.Body.String())
	}
	originalChat := chatsBeforeReset[0]

	originalHistoryW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(originalHistoryW, httptest.NewRequest(http.MethodGet, "/chats/"+originalChat.ID, nil))
	if originalHistoryW.Code != http.StatusOK {
		t.Fatalf("get original history status=%d body=%s", originalHistoryW.Code, originalHistoryW.Body.String())
	}
	var originalHistory domain.ChatHistory
	if err := json.Unmarshal(originalHistoryW.Body.Bytes(), &originalHistory); err != nil {
		t.Fatalf("decode original history failed: %v body=%s", err, originalHistoryW.Body.String())
	}
	if !chatHistoryContainsText(originalHistory, "hello before reset") {
		t.Fatalf("expected original history to contain first user text, body=%s", originalHistoryW.Body.String())
	}

	resetReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":" /new "}]}],"session_id":"s-reset","user_id":"u-reset","channel":"console","stream":false}`
	resetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resetW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(resetReq)))
	if resetW.Code != http.StatusOK {
		t.Fatalf("reset process status=%d body=%s", resetW.Code, resetW.Body.String())
	}
	var resetResp domain.AgentProcessResponse
	if err := json.Unmarshal(resetW.Body.Bytes(), &resetResp); err != nil {
		t.Fatalf("decode reset response failed: %v body=%s", err, resetW.Body.String())
	}
	if !strings.Contains(resetResp.Reply, "上下文已清理") {
		t.Fatalf("unexpected reset reply: %#v", resetResp.Reply)
	}

	chatsAfterResetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsAfterResetW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-reset&channel=console", nil))
	if chatsAfterResetW.Code != http.StatusOK {
		t.Fatalf("list chats after reset status=%d body=%s", chatsAfterResetW.Code, chatsAfterResetW.Body.String())
	}
	var chatsAfterReset []domain.ChatSpec
	if err := json.Unmarshal(chatsAfterResetW.Body.Bytes(), &chatsAfterReset); err != nil {
		t.Fatalf("decode chats after reset failed: %v body=%s", err, chatsAfterResetW.Body.String())
	}
	if len(chatsAfterReset) != 0 {
		t.Fatalf("expected no chats after reset, got=%d body=%s", len(chatsAfterReset), chatsAfterResetW.Body.String())
	}

	secondReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello after reset"}]}],"session_id":"s-reset","user_id":"u-reset","channel":"console","stream":false}`
	secondW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(secondW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(secondReq)))
	if secondW.Code != http.StatusOK {
		t.Fatalf("second process status=%d body=%s", secondW.Code, secondW.Body.String())
	}

	chatsAfterSecondW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsAfterSecondW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-reset&channel=console", nil))
	if chatsAfterSecondW.Code != http.StatusOK {
		t.Fatalf("list chats after second message status=%d body=%s", chatsAfterSecondW.Code, chatsAfterSecondW.Body.String())
	}
	var chatsAfterSecond []domain.ChatSpec
	if err := json.Unmarshal(chatsAfterSecondW.Body.Bytes(), &chatsAfterSecond); err != nil {
		t.Fatalf("decode chats after second message failed: %v body=%s", err, chatsAfterSecondW.Body.String())
	}
	if len(chatsAfterSecond) != 1 {
		t.Fatalf("expected one chat after second message, got=%d body=%s", len(chatsAfterSecond), chatsAfterSecondW.Body.String())
	}
	if chatsAfterSecond[0].ID == originalChat.ID {
		t.Fatalf("expected a new chat id after reset, got unchanged id=%s", chatsAfterSecond[0].ID)
	}

	newHistoryW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(newHistoryW, httptest.NewRequest(http.MethodGet, "/chats/"+chatsAfterSecond[0].ID, nil))
	if newHistoryW.Code != http.StatusOK {
		t.Fatalf("get new history status=%d body=%s", newHistoryW.Code, newHistoryW.Body.String())
	}
	var newHistory domain.ChatHistory
	if err := json.Unmarshal(newHistoryW.Body.Bytes(), &newHistory); err != nil {
		t.Fatalf("decode new history failed: %v body=%s", err, newHistoryW.Body.String())
	}
	if chatHistoryContainsText(newHistory, "hello before reset") {
		t.Fatalf("expected previous context to be cleared, body=%s", newHistoryW.Body.String())
	}
	if !chatHistoryContainsText(newHistory, "hello after reset") {
		t.Fatalf("expected new history to contain post-reset text, body=%s", newHistoryW.Body.String())
	}
}

func TestQQInboundC2CEventTriggersOutboundDispatch(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u-c2c/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	inboundReq := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-c2c-1","content":"hello inbound c2c","author":{"user_openid":"u-c2c"}}}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(inboundReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("inbound status=%d body=%s", w.Code, w.Body.String())
	}

	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := messageCalls.Load(); got != 1 {
		t.Fatalf("expected one qq c2c dispatch, got=%d", got)
	}
}

func TestQQInboundGroupEventTriggersOutboundDispatch(t *testing.T) {
	var tokenCalls atomic.Int32
	var groupCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/groups/group-openid-1/messages":
			groupCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	inboundReq := `{"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"m-group-1","content":"hello inbound group","group_openid":"group-openid-1","author":{"member_openid":"u-group-1"}}}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(inboundReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("inbound status=%d body=%s", w.Code, w.Body.String())
	}

	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := groupCalls.Load(); got != 1 {
		t.Fatalf("expected one qq group dispatch, got=%d", got)
	}
}

func TestQQInboundNewCommandClearsSessionContext(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u-c2c-reset/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	firstInboundReq := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-c2c-1","content":"hello inbound before reset","author":{"user_openid":"u-c2c-reset"}}}`
	firstInboundW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(firstInboundW, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(firstInboundReq)))
	if firstInboundW.Code != http.StatusOK {
		t.Fatalf("first inbound status=%d body=%s", firstInboundW.Code, firstInboundW.Body.String())
	}

	chatsBeforeResetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsBeforeResetW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-c2c-reset&channel=qq", nil))
	if chatsBeforeResetW.Code != http.StatusOK {
		t.Fatalf("list qq chats before reset status=%d body=%s", chatsBeforeResetW.Code, chatsBeforeResetW.Body.String())
	}
	var chatsBeforeReset []domain.ChatSpec
	if err := json.Unmarshal(chatsBeforeResetW.Body.Bytes(), &chatsBeforeReset); err != nil {
		t.Fatalf("decode qq chats before reset failed: %v body=%s", err, chatsBeforeResetW.Body.String())
	}
	if len(chatsBeforeReset) != 1 {
		t.Fatalf("expected one qq chat before reset, got=%d body=%s", len(chatsBeforeReset), chatsBeforeResetW.Body.String())
	}
	originalChat := chatsBeforeReset[0]

	resetInboundReq := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-c2c-2","content":" /new ","author":{"user_openid":"u-c2c-reset"}}}`
	resetInboundW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resetInboundW, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(resetInboundReq)))
	if resetInboundW.Code != http.StatusOK {
		t.Fatalf("reset inbound status=%d body=%s", resetInboundW.Code, resetInboundW.Body.String())
	}
	var resetResp domain.AgentProcessResponse
	if err := json.Unmarshal(resetInboundW.Body.Bytes(), &resetResp); err != nil {
		t.Fatalf("decode reset inbound response failed: %v body=%s", err, resetInboundW.Body.String())
	}
	if !strings.Contains(resetResp.Reply, "上下文已清理") {
		t.Fatalf("unexpected reset inbound reply: %#v", resetResp.Reply)
	}

	chatsAfterResetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsAfterResetW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-c2c-reset&channel=qq", nil))
	if chatsAfterResetW.Code != http.StatusOK {
		t.Fatalf("list qq chats after reset status=%d body=%s", chatsAfterResetW.Code, chatsAfterResetW.Body.String())
	}
	var chatsAfterReset []domain.ChatSpec
	if err := json.Unmarshal(chatsAfterResetW.Body.Bytes(), &chatsAfterReset); err != nil {
		t.Fatalf("decode qq chats after reset failed: %v body=%s", err, chatsAfterResetW.Body.String())
	}
	if len(chatsAfterReset) != 0 {
		t.Fatalf("expected no qq chats after reset, got=%d body=%s", len(chatsAfterReset), chatsAfterResetW.Body.String())
	}

	secondInboundReq := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-c2c-3","content":"hello inbound after reset","author":{"user_openid":"u-c2c-reset"}}}`
	secondInboundW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(secondInboundW, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(secondInboundReq)))
	if secondInboundW.Code != http.StatusOK {
		t.Fatalf("second inbound status=%d body=%s", secondInboundW.Code, secondInboundW.Body.String())
	}

	chatsAfterSecondW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsAfterSecondW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-c2c-reset&channel=qq", nil))
	if chatsAfterSecondW.Code != http.StatusOK {
		t.Fatalf("list qq chats after second message status=%d body=%s", chatsAfterSecondW.Code, chatsAfterSecondW.Body.String())
	}
	var chatsAfterSecond []domain.ChatSpec
	if err := json.Unmarshal(chatsAfterSecondW.Body.Bytes(), &chatsAfterSecond); err != nil {
		t.Fatalf("decode qq chats after second message failed: %v body=%s", err, chatsAfterSecondW.Body.String())
	}
	if len(chatsAfterSecond) != 1 {
		t.Fatalf("expected one qq chat after second message, got=%d body=%s", len(chatsAfterSecond), chatsAfterSecondW.Body.String())
	}
	if chatsAfterSecond[0].ID == originalChat.ID {
		t.Fatalf("expected a new qq chat id after reset, got unchanged id=%s", chatsAfterSecond[0].ID)
	}

	newHistoryW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(newHistoryW, httptest.NewRequest(http.MethodGet, "/chats/"+chatsAfterSecond[0].ID, nil))
	if newHistoryW.Code != http.StatusOK {
		t.Fatalf("get new qq history status=%d body=%s", newHistoryW.Code, newHistoryW.Body.String())
	}
	var newHistory domain.ChatHistory
	if err := json.Unmarshal(newHistoryW.Body.Bytes(), &newHistory); err != nil {
		t.Fatalf("decode new qq history failed: %v body=%s", err, newHistoryW.Body.String())
	}
	if chatHistoryContainsText(newHistory, "hello inbound before reset") {
		t.Fatalf("expected qq previous context to be cleared, body=%s", newHistoryW.Body.String())
	}
	if !chatHistoryContainsText(newHistory, "hello inbound after reset") {
		t.Fatalf("expected qq new history to contain post-reset text, body=%s", newHistoryW.Body.String())
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call across qq reset flow, got=%d", got)
	}
	if got := messageCalls.Load(); got != 3 {
		t.Fatalf("expected three qq dispatches across reset flow, got=%d", got)
	}
}

func TestQQInboundRejectsUnsupportedEvent(t *testing.T) {
	srv := newTestServer(t)
	inboundReq := `{"t":"MESSAGE_DELETE","d":{"id":"m-delete"}}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(inboundReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_qq_event"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func chatHistoryContainsText(history domain.ChatHistory, want string) bool {
	for _, msg := range history.Messages {
		for _, content := range msg.Content {
			if strings.Contains(content.Text, want) {
				return true
			}
		}
	}
	return false
}

func TestQQInboundStateEndpointReturnsRuntimeSnapshot(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channels/qq/state", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode state body failed: %v", err)
	}
	if _, ok := body["configured"].(bool); !ok {
		t.Fatalf("missing configured bool: %#v", body["configured"])
	}
	if _, ok := body["running"].(bool); !ok {
		t.Fatalf("missing running bool: %#v", body["running"])
	}
	if _, ok := body["connected"].(bool); !ok {
		t.Fatalf("missing connected bool: %#v", body["connected"])
	}
	if _, ok := body["config"].(map[string]interface{}); !ok {
		t.Fatalf("missing config map: %#v", body["config"])
	}
}

func TestQQInboundStateEndpointReflectsConfiguredIntents(t *testing.T) {
	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","inbound_enabled":true,"inbound_intents":42}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channels/qq/state", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode state body failed: %v", err)
	}
	if configured, _ := body["configured"].(bool); !configured {
		t.Fatalf("expected configured=true, got=%#v", body["configured"])
	}
	configObj, _ := body["config"].(map[string]interface{})
	if intents, _ := configObj["intents"].(float64); intents != 42 {
		t.Fatalf("expected config intents=42, got=%#v", configObj["intents"])
	}
}

func TestProcessAgentRunsShellTool(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"/shell printf hello"}]}],
		"session_id":"s-shell",
		"user_id":"u-shell",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"shell","items":[{"command":"printf hello"}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "hello") {
		t.Fatalf("expected shell output in reply body, got=%s", w.Body.String())
	}
}

func TestProcessAgentRejectsUnknownTool(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"run desktop"}]}],
		"session_id":"s-tool-unknown",
		"user_id":"u-tool-unknown",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"desktop","input":{}}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"tool_not_supported"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsShellToolWhenDisabled(t *testing.T) {
	t.Setenv("NEXTAI_DISABLED_TOOLS", "shell")
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"/shell pwd"}]}],
		"session_id":"s-shell-disabled",
		"user_id":"u-shell-disabled",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"shell","items":[{"command":"pwd"}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"tool_disabled"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsShellToolWithoutCommand(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"/shell"}]}],
		"session_id":"s-shell-empty",
		"user_id":"u-shell-empty",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"shell","items":[{}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentAcceptsBizParamsShellInputCommandForBackwardCompatibility(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"/shell compat"}]}],
		"session_id":"s-shell-biz-compat",
		"user_id":"u-shell-biz-compat",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"shell","input":{"command":"printf compat"}}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"reply":"$ printf compat`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestPrependSystemLayersPreservesOrder(t *testing.T) {
	input := []domain.AgentInputMessage{
		{
			Role: "user",
			Type: "message",
			Content: []domain.RuntimeContent{
				{Type: "text", Text: "hello"},
			},
		},
	}

	layers := []systemPromptLayer{
		{Name: "base_system", Role: "system", Content: "base"},
		{Name: "tool_guide_system", Role: "system", Content: "tool"},
		{Name: "workspace_policy_system", Role: "system", Content: ""},
		{Name: "session_policy_system", Role: "system", Content: "session"},
	}

	out := prependSystemLayers(input, layers)
	if len(out) != 4 {
		t.Fatalf("expected 4 messages, got=%d", len(out))
	}
	if got := out[0].Content[0].Text; got != "base" {
		t.Fatalf("first layer mismatch: %q", got)
	}
	if got := out[1].Content[0].Text; got != "tool" {
		t.Fatalf("second layer mismatch: %q", got)
	}
	if got := out[2].Content[0].Text; got != "session" {
		t.Fatalf("third layer mismatch: %q", got)
	}
	if got := out[3].Role; got != "user" {
		t.Fatalf("last message should be user, got=%q", got)
	}
}

func TestBuildSystemLayersOrder(t *testing.T) {
	srv := newTestServer(t)

	layers, err := srv.buildSystemLayers()
	if err != nil {
		t.Fatalf("buildSystemLayers failed: %v", err)
	}
	if len(layers) < 2 {
		t.Fatalf("expected at least 2 layers, got=%d", len(layers))
	}
	if layers[0].Name != "base_system" {
		t.Fatalf("first layer should be base_system, got=%q", layers[0].Name)
	}
	if layers[1].Name != "tool_guide_system" {
		t.Fatalf("second layer should be tool_guide_system, got=%q", layers[1].Name)
	}
}

func TestBuildSystemLayersForCodexModeFallsBackToDefaultLayers(t *testing.T) {
	srv := newTestServer(t)

	layers, err := srv.buildSystemLayersForMode(promptModeCodex)
	if err != nil {
		t.Fatalf("expected fallback to default layers, got err=%v", err)
	}
	if len(layers) < 2 {
		t.Fatalf("expected default layers, got=%#v", layers)
	}
	if layers[0].Name != "base_system" || layers[1].Name != "tool_guide_system" {
		t.Fatalf("expected fallback default layer order, got=%#v", layers)
	}
}

func TestBuildSystemLayersForLegacyOptionsCodexModeFallsBackToDefaultLayers(t *testing.T) {
	srv := newTestServer(t)

	layers, err := srv.buildSystemLayersForLegacyOptions(promptModeCodex, codexLayerBuildOptions{
		SessionID: "s-legacy-codex-mode",
	})
	if err != nil {
		t.Fatalf("expected fallback to default layers, got err=%v", err)
	}
	if len(layers) < 2 {
		t.Fatalf("expected default layers, got=%#v", layers)
	}
	if layers[0].Name != "base_system" || layers[1].Name != "tool_guide_system" {
		t.Fatalf("expected fallback default layer order, got=%#v", layers)
	}
}

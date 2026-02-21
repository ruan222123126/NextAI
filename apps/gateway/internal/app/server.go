package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"nextai/apps/gateway/internal/channel"
	"nextai/apps/gateway/internal/config"
	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/observability"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
)

const version = "0.1.0"

const (
	cronTickInterval = time.Second

	cronStatusPaused    = "paused"
	cronStatusResumed   = "resumed"
	cronStatusRunning   = "running"
	cronStatusSucceeded = "succeeded"
	cronStatusFailed    = "failed"

	cronTaskTypeText     = "text"
	cronTaskTypeWorkflow = "workflow"

	cronWorkflowVersionV1 = "v1"
	cronWorkflowNodeStart = "start"
	cronWorkflowNodeText  = "text_event"
	cronWorkflowNodeDelay = "delay"
	cronWorkflowNodeIf    = "if_event"

	cronWorkflowNodeExecutionSkipped = "skipped"

	cronLeaseDirName = "cron-leases"

	aiToolsGuideRelativePath         = "docs/AI/AGENTS.md"
	aiToolsGuideLegacyRelativePath   = "docs/AI/ai-tools.md"
	aiToolsGuideLegacyV0RelativePath = "docs/ai-tools.md"
	aiToolsGuidePathEnv              = "NEXTAI_AI_TOOLS_GUIDE_PATH"
	disabledToolsEnv                 = "NEXTAI_DISABLED_TOOLS"
	enableBrowserToolEnv             = "NEXTAI_ENABLE_BROWSER_TOOL"
	browserToolAgentDirEnv           = "NEXTAI_BROWSER_AGENT_DIR"
	enableSearchToolEnv              = "NEXTAI_ENABLE_SEARCH_TOOL"
	disableQQInboundSupervisorEnv    = "NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR"

	replyChunkSizeDefault = 12
	contextResetCommand   = "/new"
	contextResetReply     = "上下文已清理，已开始新会话。"

	defaultProcessChannel = "console"
	qqChannelName         = "qq"
	channelSourceHeader   = "X-NextAI-Source"
	qqInboundPath         = "/channels/qq/inbound"
	defaultWebDirName     = "web"
)

var errCronJobNotFound = errors.New("cron_job_not_found")
var errCronMaxConcurrencyReached = errors.New("cron_max_concurrency_reached")
var errCronDefaultProtected = errors.New("cron_default_protected")

var cronWorkflowIfConditionPattern = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(==|!=)\s*(?:"([^"]*)"|'([^']*)'|(\S+))\s*$`)

var cronWorkflowIfAllowedFields = map[string]struct{}{
	"job_id":     {},
	"job_name":   {},
	"channel":    {},
	"user_id":    {},
	"session_id": {},
	"task_type":  {},
}

type cronWorkflowPlan struct {
	Workflow domain.CronWorkflowSpec
	StartID  string
	NodeByID map[string]domain.CronWorkflowNode
	NextByID map[string]string
	Order    []domain.CronWorkflowNode
}

type Server struct {
	cfg      config.Config
	store    *repo.Store
	runner   *runner.Runner
	channels map[string]plugin.ChannelPlugin
	tools    map[string]plugin.ToolPlugin

	disabledTools map[string]struct{}
	qqInboundMu   sync.RWMutex
	qqInbound     qqInboundRuntimeState

	cronStop chan struct{}
	cronDone chan struct{}
	cronWG   sync.WaitGroup

	cronTaskExecutor func(context.Context, domain.CronJobSpec) error
	closeOnce        sync.Once
}

func NewServer(cfg config.Config) (*Server, error) {
	store, err := repo.NewStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		cfg:      cfg,
		store:    store,
		runner:   runner.New(),
		channels: map[string]plugin.ChannelPlugin{},
		tools:    map[string]plugin.ToolPlugin{},
		disabledTools: parseDisabledTools(
			os.Getenv(disabledToolsEnv),
		),
		cronStop: make(chan struct{}),
		cronDone: make(chan struct{}),
	}
	srv.registerChannelPlugin(channel.NewConsoleChannel())
	srv.registerChannelPlugin(channel.NewWebhookChannel())
	srv.registerChannelPlugin(channel.NewQQChannel())
	srv.registerToolPlugin(plugin.NewShellTool())
	srv.registerToolPlugin(plugin.NewViewFileLinesTool(""))
	srv.registerToolPlugin(plugin.NewEditFileLinesTool(""))
	if parseBool(os.Getenv(enableBrowserToolEnv)) {
		browserTool, toolErr := plugin.NewBrowserTool(strings.TrimSpace(os.Getenv(browserToolAgentDirEnv)))
		if toolErr != nil {
			return nil, fmt.Errorf("init browser tool failed: %w", toolErr)
		}
		srv.registerToolPlugin(browserTool)
	}
	if parseBool(os.Getenv(enableSearchToolEnv)) {
		searchTool, toolErr := plugin.NewSearchToolFromEnv()
		if toolErr != nil {
			return nil, fmt.Errorf("init search tool failed: %w", toolErr)
		}
		srv.registerToolPlugin(searchTool)
	}
	srv.startCronScheduler()
	if !parseBool(os.Getenv(disableQQInboundSupervisorEnv)) {
		srv.startQQInboundSupervisor()
	}
	return srv, nil
}

func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.cronStop)
		<-s.cronDone
		s.cronWG.Wait()
	})
}

func (s *Server) registerChannelPlugin(ch plugin.ChannelPlugin) {
	if ch == nil {
		return
	}
	name := strings.ToLower(strings.TrimSpace(ch.Name()))
	if name == "" {
		return
	}
	s.channels[name] = ch
}

func (s *Server) registerToolPlugin(tp plugin.ToolPlugin) {
	if tp == nil {
		return
	}
	name := strings.ToLower(strings.TrimSpace(tp.Name()))
	if name == "" {
		return
	}
	s.tools[name] = tp
}

func parseDisabledTools(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

func (s *Server) toolDisabled(name string) bool {
	if s == nil {
		return false
	}
	if len(s.disabledTools) == 0 {
		return false
	}
	_, ok := s.disabledTools[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(observability.RequestID)
	r.Use(observability.Logging)
	r.Use(cors)

	r.Get("/version", s.handleVersion)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/runtime-config", s.handleRuntimeConfig)

	r.Group(func(api chi.Router) {
		api.Use(observability.APIKey(s.cfg.APIKey))

		api.Route("/chats", func(r chi.Router) {
			r.Get("/", s.listChats)
			r.Post("/", s.createChat)
			r.Post("/batch-delete", s.batchDeleteChats)
			r.Get("/{chat_id}", s.getChat)
			r.Put("/{chat_id}", s.updateChat)
			r.Delete("/{chat_id}", s.deleteChat)
		})

		api.Post("/agent/process", s.processAgent)
		api.Get("/agent/system-layers", s.getAgentSystemLayers)
		api.Post("/channels/qq/inbound", s.processQQInbound)
		api.Get("/channels/qq/state", s.getQQInboundState)

		api.Route("/cron", func(r chi.Router) {
			r.Get("/jobs", s.listCronJobs)
			r.Post("/jobs", s.createCronJob)
			r.Get("/jobs/{job_id}", s.getCronJob)
			r.Put("/jobs/{job_id}", s.updateCronJob)
			r.Delete("/jobs/{job_id}", s.deleteCronJob)
			r.Post("/jobs/{job_id}/pause", s.pauseCronJob)
			r.Post("/jobs/{job_id}/resume", s.resumeCronJob)
			r.Post("/jobs/{job_id}/run", s.runCronJob)
			r.Get("/jobs/{job_id}/state", s.getCronJobState)
		})

		api.Route("/models", func(r chi.Router) {
			r.Get("/", s.listProviders)
			r.Get("/catalog", s.getModelCatalog)
			r.Put("/{provider_id}/config", s.configureProvider)
			r.Delete("/{provider_id}", s.deleteProvider)
			r.Get("/active", s.getActiveModels)
			r.Put("/active", s.setActiveModels)
		})

		api.Route("/envs", func(r chi.Router) {
			r.Get("/", s.listEnvs)
			r.Put("/", s.putEnvs)
			r.Delete("/{key}", s.deleteEnv)
		})

		api.Route("/skills", func(r chi.Router) {
			r.Get("/", s.listSkills)
			r.Get("/available", s.listAvailableSkills)
			r.Post("/batch-disable", s.batchDisableSkills)
			r.Post("/batch-enable", s.batchEnableSkills)
			r.Post("/", s.createSkill)
			r.Post("/{skill_name}/disable", s.disableSkill)
			r.Post("/{skill_name}/enable", s.enableSkill)
			r.Delete("/{skill_name}", s.deleteSkill)
			r.Get("/{skill_name}/files/{source}/{file_path}", s.loadSkillFile)
		})

		api.Route("/workspace", func(r chi.Router) {
			r.Get("/files", s.listWorkspaceFiles)
			r.Get("/files/*", s.getWorkspaceFile)
			r.Put("/files/*", s.putWorkspaceFile)
			r.Delete("/files/*", s.deleteWorkspaceFile)
			r.Get("/export", s.exportWorkspace)
			r.Post("/import", s.importWorkspace)
		})

		api.Route("/config", func(r chi.Router) {
			r.Get("/channels", s.listChannels)
			r.Get("/channels/types", s.listChannelTypes)
			r.Put("/channels", s.putChannels)
			r.Get("/channels/{channel_name}", s.getChannel)
			r.Put("/channels/{channel_name}", s.putChannel)
		})
	})

	if webHandler := webStaticHandler(s.cfg.WebDir); webHandler != nil {
		r.Get("/*", webHandler)
	}

	return r
}

func (s *Server) startCronScheduler() {
	go func() {
		defer close(s.cronDone)
		s.cronSchedulerTick()

		ticker := time.NewTicker(cronTickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.cronSchedulerTick()
			case <-s.cronStop:
				return
			}
		}
	}()
}

type dueCronExecution struct {
	JobID string
}

func (s *Server) cronSchedulerTick() {
	now := time.Now().UTC()
	stateUpdates := map[string]domain.CronJobState{}
	dueJobs := make([]dueCronExecution, 0)
	s.store.Read(func(st *repo.State) {
		for id, job := range st.CronJobs {
			current := st.CronStates[id]
			next := normalizeCronPausedState(current)
			if !cronJobSchedulable(job, next) {
				next.NextRunAt = nil
				if !cronStateEqual(current, next) {
					stateUpdates[id] = next
				}
				continue
			}

			nextRunAt, dueAt, err := resolveCronNextRunAt(job, next.NextRunAt, now)
			if err != nil {
				msg := err.Error()
				next.LastError = &msg
				next.NextRunAt = nil
				if !cronStateEqual(current, next) {
					stateUpdates[id] = next
				}
				continue
			}

			nextRun := nextRunAt.Format(time.RFC3339)
			next.NextRunAt = &nextRun
			next.LastError = nil
			if dueAt != nil && cronMisfireExceeded(dueAt, cronRuntimeSpec(job), now) {
				failed := cronStatusFailed
				msg := fmt.Sprintf("misfire skipped: scheduled_at=%s", dueAt.Format(time.RFC3339))
				next.LastStatus = &failed
				next.LastError = &msg
				dueAt = nil
			}
			if !cronStateEqual(current, next) {
				stateUpdates[id] = next
			}
			if dueAt != nil {
				dueJobs = append(dueJobs, dueCronExecution{JobID: id})
			}
		}
	})
	if len(stateUpdates) > 0 {
		if err := s.store.Write(func(st *repo.State) error {
			for id, next := range stateUpdates {
				if _, ok := st.CronJobs[id]; !ok {
					continue
				}
				st.CronStates[id] = next
			}
			return nil
		}); err != nil {
			log.Printf("cron scheduler tick failed: %v", err)
			return
		}
	}

	for _, due := range dueJobs {
		s.cronWG.Add(1)
		go func(jobID string) {
			defer s.cronWG.Done()
			if err := s.executeCronJob(jobID); err != nil &&
				!errors.Is(err, errCronJobNotFound) &&
				!errors.Is(err, errCronMaxConcurrencyReached) {
				log.Printf("cron job %s execute failed: %v", jobID, err)
			}
		}(due.JobID)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Request-Id,X-NextAI-Source")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type runtimeConfigFeaturesResponse struct {
	PromptTemplates         bool `json:"prompt_templates"`
	PromptContextIntrospect bool `json:"prompt_context_introspect"`
}

type runtimeConfigResponse struct {
	Features runtimeConfigFeaturesResponse `json:"features"`
}

func (s *Server) handleRuntimeConfig(w http.ResponseWriter, _ *http.Request) {
	resp := runtimeConfigResponse{
		Features: runtimeConfigFeaturesResponse{
			PromptTemplates:         s.cfg.EnablePromptTemplates,
			PromptContextIntrospect: s.cfg.EnablePromptContextIntrospect,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func webStaticHandler(configuredWebDir string) http.HandlerFunc {
	webDir, ok := resolveWebDir(configuredWebDir)
	if !ok {
		return nil
	}
	fileServer := http.FileServer(http.Dir(webDir))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		relPath := strings.TrimPrefix(cleanPath, "/")
		if relPath != "" {
			targetPath := filepath.Join(webDir, filepath.FromSlash(relPath))
			if info, err := os.Stat(targetPath); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		indexPath := filepath.Join(webDir, "index.html")
		if info, err := os.Stat(indexPath); err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	}
}

func resolveWebDir(configuredWebDir string) (string, bool) {
	raw := strings.TrimSpace(configuredWebDir)
	if raw == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		raw = filepath.Join(cwd, defaultWebDirName)
	}
	if !filepath.IsAbs(raw) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		raw = filepath.Join(cwd, raw)
	}
	info, err := os.Stat(raw)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return raw, true
}

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	channel := r.URL.Query().Get("channel")
	out := make([]domain.ChatSpec, 0)
	s.store.Read(func(state *repo.State) {
		for _, v := range state.Chats {
			if userID != "" && v.UserID != userID {
				continue
			}
			if channel != "" && v.Channel != channel {
				continue
			}
			out = append(out, v)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createChat(w http.ResponseWriter, r *http.Request) {
	var req domain.ChatSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID == "" {
		req.ID = newID("chat")
	}
	if req.Name == "" {
		req.Name = "New Chat"
	}
	if req.SessionID == "" || req.UserID == "" || req.Channel == "" {
		writeErr(w, http.StatusBadRequest, "invalid_chat", "session_id, user_id, channel are required", nil)
		return
	}
	if req.Meta == nil {
		req.Meta = map[string]interface{}{}
	}
	now := nowISO()
	req.CreatedAt = now
	req.UpdatedAt = now
	if err := s.store.Write(func(state *repo.State) error {
		state.Chats[req.ID] = req
		if _, ok := state.Histories[req.ID]; !ok {
			state.Histories[req.ID] = []domain.RuntimeMessage{}
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) batchDeleteChats(w http.ResponseWriter, r *http.Request) {
	var ids []string
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == domain.DefaultChatID {
			writeErr(w, http.StatusBadRequest, "default_chat_protected", "default chat cannot be deleted", map[string]string{"chat_id": domain.DefaultChatID})
			return
		}
	}
	if err := s.store.Write(func(state *repo.State) error {
		for _, id := range ids {
			delete(state.Chats, id)
			delete(state.Histories, id)
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) getChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "chat_id")
	var history []domain.RuntimeMessage
	found := false
	s.store.Read(func(state *repo.State) {
		if _, ok := state.Chats[id]; ok {
			history = state.Histories[id]
			found = true
		}
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "chat not found", map[string]string{"chat_id": id})
		return
	}
	writeJSON(w, http.StatusOK, domain.ChatHistory{Messages: history})
}

func (s *Server) updateChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "chat_id")
	var req domain.ChatSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID != id {
		writeErr(w, http.StatusBadRequest, "chat_id_mismatch", "chat_id mismatch", nil)
		return
	}
	if err := s.store.Write(func(state *repo.State) error {
		old, ok := state.Chats[id]
		if !ok {
			return errors.New("not_found")
		}
		req.CreatedAt = old.CreatedAt
		req.UpdatedAt = nowISO()
		state.Chats[id] = req
		return nil
	}); err != nil {
		if err.Error() == "not_found" {
			writeErr(w, http.StatusNotFound, "not_found", "chat not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) deleteChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "chat_id")
	if id == domain.DefaultChatID {
		writeErr(w, http.StatusBadRequest, "default_chat_protected", "default chat cannot be deleted", map[string]string{"chat_id": domain.DefaultChatID})
		return
	}
	deleted := false
	if err := s.store.Write(func(state *repo.State) error {
		if _, ok := state.Chats[id]; ok {
			deleted = true
			delete(state.Chats, id)
			delete(state.Histories, id)
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !deleted {
		writeErr(w, http.StatusNotFound, "not_found", "chat not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

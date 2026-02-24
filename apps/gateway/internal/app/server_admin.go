package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
	adminservice "nextai/apps/gateway/internal/service/admin"
	codexpromptservice "nextai/apps/gateway/internal/service/codexprompt"
	modelservice "nextai/apps/gateway/internal/service/model"
	systempromptservice "nextai/apps/gateway/internal/service/systemprompt"
	workspaceservice "nextai/apps/gateway/internal/service/workspace"
)

func (s *Server) listProviders(w http.ResponseWriter, _ *http.Request) {
	providers, err := s.getModelService().ListProviders()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) getModelCatalog(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getModelService().GetCatalog()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) configureProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey          *string            `json:"api_key"`
		BaseURL         *string            `json:"base_url"`
		DisplayName     *string            `json:"display_name"`
		ReasoningEffort *string            `json:"reasoning_effort"`
		Enabled         *bool              `json:"enabled"`
		Store           *bool              `json:"store"`
		Headers         *map[string]string `json:"headers"`
		TimeoutMS       *int               `json:"timeout_ms"`
		ModelAliases    *map[string]string `json:"model_aliases"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	out, err := s.getModelService().ConfigureProvider(modelservice.ConfigureProviderInput{
		ProviderID:      chi.URLParam(r, "provider_id"),
		APIKey:          body.APIKey,
		BaseURL:         body.BaseURL,
		DisplayName:     body.DisplayName,
		ReasoningEffort: body.ReasoningEffort,
		Enabled:         body.Enabled,
		Store:           body.Store,
		Headers:         body.Headers,
		TimeoutMS:       body.TimeoutMS,
		ModelAliases:    body.ModelAliases,
	})
	if err != nil {
		if validation := (*modelservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.getModelService().DeleteProvider(chi.URLParam(r, "provider_id"))
	if err != nil {
		if validation := (*modelservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) getActiveModels(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getModelService().GetActiveModels()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setActiveModels(w http.ResponseWriter, r *http.Request) {
	var body domain.ModelSlotConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	out, err := s.getModelService().SetActiveModels(body)
	if err != nil {
		if validation := (*modelservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		switch {
		case errors.Is(err, modelservice.ErrProviderNotFound):
			writeErr(w, http.StatusNotFound, "provider_not_found", "provider not found", nil)
			return
		case errors.Is(err, modelservice.ErrProviderDisabled):
			writeErr(w, http.StatusBadRequest, "provider_disabled", "provider is disabled", nil)
			return
		case errors.Is(err, modelservice.ErrModelNotFound):
			writeErr(w, http.StatusBadRequest, "model_not_found", "model not found for provider", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listEnvs(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getAdminService().ListEnvs()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putEnvs(w http.ResponseWriter, r *http.Request) {
	body := map[string]string{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	out, err := s.getAdminService().ReplaceEnvs(body)
	if err != nil {
		if validation := (*adminservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteEnv(w http.ResponseWriter, r *http.Request) {
	out, exists, err := s.getAdminService().DeleteEnv(chi.URLParam(r, "key"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "not_found", "env key not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listSkills(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getAdminService().ListSkills(false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAvailableSkills(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getAdminService().ListSkills(true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) batchDisableSkills(w http.ResponseWriter, r *http.Request) {
	s.batchSetSkillEnabled(w, r, false)
}

func (s *Server) batchEnableSkills(w http.ResponseWriter, r *http.Request) {
	s.batchSetSkillEnabled(w, r, true)
}

func (s *Server) batchSetSkillEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	var names []string
	if err := json.NewDecoder(r.Body).Decode(&names); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.getAdminService().BatchSetSkillEnabled(names, enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string                 `json:"name"`
		Content    string                 `json:"content"`
		References map[string]interface{} `json:"references"`
		Scripts    map[string]interface{} `json:"scripts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	created, err := s.getAdminService().CreateSkill(adminservice.CreateSkillInput{
		Name:       body.Name,
		Content:    body.Content,
		References: body.References,
		Scripts:    body.Scripts,
	})
	if err != nil {
		if validation := (*adminservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"created": created})
}

func (s *Server) disableSkill(w http.ResponseWriter, r *http.Request) {
	s.setSkillEnabled(w, chi.URLParam(r, "skill_name"), false)
}

func (s *Server) enableSkill(w http.ResponseWriter, r *http.Request) {
	s.setSkillEnabled(w, chi.URLParam(r, "skill_name"), true)
}

func (s *Server) setSkillEnabled(w http.ResponseWriter, name string, enabled bool) {
	exists, err := s.getAdminService().SetSkillEnabled(name, enabled)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "not_found", "skill not found", nil)
		return
	}
	key := "enabled"
	if !enabled {
		key = "disabled"
	}
	writeJSON(w, http.StatusOK, map[string]bool{key: true})
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.getAdminService().DeleteSkill(chi.URLParam(r, "skill_name"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) loadSkillFile(w http.ResponseWriter, r *http.Request) {
	content, found, err := s.getAdminService().LoadSkillFile(
		chi.URLParam(r, "skill_name"),
		chi.URLParam(r, "file_path"),
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "skill file not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func readSkillVirtualFile(skill domain.SkillSpec, filePath string) (string, bool) {
	parts := strings.Split(strings.Trim(filePath, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	var node interface{}
	switch parts[0] {
	case "references":
		node = skill.References
	case "scripts":
		node = skill.Scripts
	default:
		return "", false
	}
	for _, p := range parts[1:] {
		m, ok := node.(map[string]interface{})
		if !ok {
			return "", false
		}
		n, ok := m[p]
		if !ok {
			return "", false
		}
		node = n
	}
	s, ok := node.(string)
	return s, ok
}

const (
	workspaceFileEnvs      = "config/envs.json"
	workspaceFileChannels  = "config/channels.json"
	workspaceFileModels    = "config/models.json"
	workspaceFileActiveLLM = "config/active-llm.json"
	workspaceFileAITools   = aiToolsGuideRelativePath
	workspaceDocsAIDir     = "docs/AI"
	workspacePromptsDir    = "prompts"
	workspacePromptDir     = "prompt"
	workspaceUploadDir     = "uploads"
	workspaceUploadField   = "file"
	workspaceUploadMaxSize = int64(20 << 20)
	workspaceUploadNameMax = 96
)

type workspaceFileEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int    `json:"size"`
}

type workspaceFileListResponse struct {
	Files []workspaceFileEntry `json:"files"`
}

type workspaceExportModels struct {
	Providers map[string]repo.ProviderSetting `json:"providers"`
	ActiveLLM domain.ModelSlotConfig          `json:"active_llm"`
}

type workspaceExportConfig struct {
	Envs     map[string]string       `json:"envs"`
	Channels domain.ChannelConfigMap `json:"channels"`
	Models   workspaceExportModels   `json:"models"`
}

type workspaceExportPayload struct {
	Version string                      `json:"version"`
	Skills  map[string]domain.SkillSpec `json:"skills"`
	Config  workspaceExportConfig       `json:"config"`
}

type workspaceImportRequest struct {
	Mode    string                 `json:"mode"`
	Payload workspaceExportPayload `json:"payload"`
}

type workspaceUploadResponse struct {
	Uploaded bool   `json:"uploaded"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
}

func (s *Server) listWorkspaceFiles(w http.ResponseWriter, _ *http.Request) {
	result, err := s.getWorkspaceService().ListFiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	out := workspaceFileListResponse{Files: make([]workspaceFileEntry, 0, len(result.Files))}
	for _, item := range result.Files {
		out.Files = append(out.Files, workspaceFileEntry{
			Path: item.Path,
			Kind: item.Kind,
			Size: item.Size,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	data, err := s.getWorkspaceService().GetFile(filePath)
	if err != nil {
		if errors.Is(err, workspaceservice.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "workspace file not found", nil)
			return
		}
		if fileErr := (*workspaceservice.FileError)(nil); errors.As(err, &fileErr) {
			writeErr(w, http.StatusInternalServerError, "file_error", fileErr.Error(), nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) putWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.getWorkspaceService().PutFile(filePath, body); err != nil {
		if validation := (*workspaceservice.ValidationError)(nil); errors.As(err, &validation) {
			status := http.StatusBadRequest
			if validation.Code == "provider_not_found" {
				status = http.StatusNotFound
			}
			writeErr(w, status, validation.Code, validation.Message, nil)
			return
		}
		if fileErr := (*workspaceservice.FileError)(nil); errors.As(err, &fileErr) {
			writeErr(w, http.StatusInternalServerError, "file_error", fileErr.Error(), nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

func (s *Server) uploadWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, workspaceUploadMaxSize)
	if err := r.ParseMultipartForm(workspaceUploadMaxSize); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErr(
				w,
				http.StatusRequestEntityTooLarge,
				"payload_too_large",
				"upload file exceeds size limit",
				map[string]int64{"max_bytes": workspaceUploadMaxSize},
			)
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid_multipart", "invalid multipart form data", nil)
		return
	}

	srcFile, header, err := r.FormFile(workspaceUploadField)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_multipart", "multipart field \"file\" is required", nil)
		return
	}
	defer srcFile.Close()

	fileName := sanitizeWorkspaceUploadName(header.Filename)
	targetDir := filepath.Join(s.cfg.DataDir, workspaceUploadDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, "file_error", err.Error(), nil)
		return
	}

	targetPath := filepath.Join(targetDir, fmt.Sprintf("%s-%s", newID("upload"), fileName))
	dstFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "file_error", err.Error(), nil)
		return
	}
	writtenSize, copyErr := io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if copyErr != nil {
		_ = os.Remove(targetPath)
		writeErr(w, http.StatusInternalServerError, "file_error", copyErr.Error(), nil)
		return
	}
	if closeErr != nil {
		_ = os.Remove(targetPath)
		writeErr(w, http.StatusInternalServerError, "file_error", closeErr.Error(), nil)
		return
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "file_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, workspaceUploadResponse{
		Uploaded: true,
		Path:     filepath.Clean(absPath),
		Name:     fileName,
		Size:     writtenSize,
	})
}

func (s *Server) deleteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	deleted, err := s.getWorkspaceService().DeleteFile(filePath)
	if err != nil {
		if errors.Is(err, workspaceservice.ErrMethodNotAllowed) {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "config files cannot be deleted", nil)
			return
		}
		if validation := (*workspaceservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) exportWorkspace(w http.ResponseWriter, _ *http.Request) {
	result, err := s.getWorkspaceService().Export()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	out := workspaceExportPayload{
		Version: result.Version,
		Skills:  result.Skills,
		Config: workspaceExportConfig{
			Envs:     result.Config.Envs,
			Channels: result.Config.Channels,
			Models: workspaceExportModels{
				Providers: result.Config.Models.Providers,
				ActiveLLM: result.Config.Models.ActiveLLM,
			},
		},
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) importWorkspace(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.getWorkspaceService().Import(body); err != nil {
		if validation := (*workspaceservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"imported": true})
}

func collectWorkspaceFiles(st *repo.State) []workspaceFileEntry {
	files := []workspaceFileEntry{
		{Path: workspaceFileEnvs, Kind: "config", Size: jsonSize(cloneWorkspaceEnvs(st.Envs))},
		{Path: workspaceFileChannels, Kind: "config", Size: jsonSize(cloneWorkspaceChannels(st.Channels))},
		{Path: workspaceFileModels, Kind: "config", Size: jsonSize(cloneWorkspaceProviders(st.Providers))},
		{Path: workspaceFileActiveLLM, Kind: "config", Size: jsonSize(st.ActiveLLM)},
	}
	for name, spec := range st.Skills {
		files = append(files, workspaceFileEntry{
			Path: workspaceSkillFilePath(name),
			Kind: "skill",
			Size: jsonSize(cloneWorkspaceSkill(spec)),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func collectWorkspaceTextFileEntries() []workspaceFileEntry {
	files := collectWorkspaceDocsAIFileEntries()
	files = append(files, collectWorkspacePromptFileEntries()...)
	if aiToolsFile, ok := workspaceAIToolsFileEntry(); ok {
		files = append(files, aiToolsFile)
	}
	return mergeWorkspaceFileEntries(nil, files...)
}

func collectWorkspaceDocsAIFileEntries() []workspaceFileEntry {
	return collectWorkspaceDirFileEntries(workspaceDocsAIDir, isWorkspaceDocsAIFilePath)
}

func collectWorkspacePromptFileEntries() []workspaceFileEntry {
	files := collectWorkspaceDirFileEntries(workspacePromptsDir, isWorkspacePromptFilePath)
	files = append(files, collectWorkspaceDirFileEntries(workspacePromptDir, isWorkspacePromptFilePath)...)
	return mergeWorkspaceFileEntries(nil, files...)
}

func collectWorkspaceDirFileEntries(relativeDir string, allow func(string) bool) []workspaceFileEntry {
	if allow == nil {
		return nil
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return nil
	}
	targetDir := filepath.Join(repoRoot, filepath.FromSlash(relativeDir))
	info, err := os.Stat(targetDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	files := []workspaceFileEntry{}
	_ = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		relPath := filepath.ToSlash(rel)
		if !allow(relPath) {
			return nil
		}
		files = append(files, workspaceFileEntry{
			Path: relPath,
			Kind: "config",
			Size: int(info.Size()),
		})
		return nil
	})
	return files
}

func mergeWorkspaceFileEntries(base []workspaceFileEntry, extra ...workspaceFileEntry) []workspaceFileEntry {
	out := make([]workspaceFileEntry, 0, len(base)+len(extra))
	indexByPath := map[string]int{}
	entries := append(append([]workspaceFileEntry{}, base...), extra...)
	for _, item := range entries {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		item.Path = path
		if idx, ok := indexByPath[path]; ok {
			if out[idx].Size <= 0 && item.Size > 0 {
				out[idx] = item
			}
			continue
		}
		indexByPath[path] = len(out)
		out = append(out, item)
	}
	return out
}

func readWorkspaceFileData(st *repo.State, filePath string) (interface{}, bool) {
	switch filePath {
	case workspaceFileEnvs:
		return cloneWorkspaceEnvs(st.Envs), true
	case workspaceFileChannels:
		return cloneWorkspaceChannels(st.Channels), true
	case workspaceFileModels:
		return cloneWorkspaceProviders(st.Providers), true
	case workspaceFileActiveLLM:
		return st.ActiveLLM, true
	default:
		name, ok := workspaceSkillNameFromPath(filePath)
		if !ok {
			return nil, false
		}
		spec, exists := st.Skills[name]
		if !exists {
			return nil, false
		}
		return cloneWorkspaceSkill(spec), true
	}
}

func workspaceFilePathFromRequest(r *http.Request) (string, bool) {
	raw := chi.URLParam(r, "*")
	if raw == "" {
		raw = chi.URLParam(r, "file_path")
	}
	return normalizeWorkspaceFilePath(raw)
}

func normalizeWorkspaceFilePath(raw string) (string, bool) {
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

func sanitizeWorkspaceUploadName(raw string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	baseName := strings.TrimSpace(filepath.Base(normalized))
	if baseName == "" || baseName == "." || baseName == ".." {
		return "upload.bin"
	}
	var builder strings.Builder
	builder.Grow(len(baseName))
	for _, ch := range baseName {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			builder.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '.' || ch == '-' || ch == '_':
			builder.WriteRune(ch)
		case ch == ' ':
			builder.WriteByte('_')
		}
	}
	cleaned := strings.Trim(builder.String(), "._-")
	if cleaned == "" {
		return "upload.bin"
	}
	if len(cleaned) > workspaceUploadNameMax {
		cleaned = cleaned[:workspaceUploadNameMax]
	}
	return cleaned
}

func workspaceSkillNameFromPath(filePath string) (string, bool) {
	if !strings.HasPrefix(filePath, "skills/") || !strings.HasSuffix(filePath, ".json") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(filePath, "skills/"), ".json"))
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func workspaceSkillFilePath(name string) string {
	return "skills/" + strings.TrimSpace(name) + ".json"
}

func isWorkspaceConfigFile(filePath string) bool {
	return filePath == workspaceFileEnvs ||
		filePath == workspaceFileChannels ||
		filePath == workspaceFileModels ||
		filePath == workspaceFileActiveLLM ||
		isWorkspaceTextFilePath(filePath)
}

func normalizeWorkspaceEnvs(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range in {
		k := strings.TrimSpace(key)
		if k == "" {
			return nil, errors.New("env key cannot be empty")
		}
		out[k] = value
	}
	return out, nil
}

func normalizeWorkspaceChannels(in domain.ChannelConfigMap, supported map[string]plugin.ChannelPlugin) (domain.ChannelConfigMap, error) {
	out := domain.ChannelConfigMap{}
	for name, cfg := range in {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return nil, errors.New("channel name cannot be empty")
		}
		if _, ok := supported[normalized]; !ok {
			return nil, fmt.Errorf("channel %q is not supported", name)
		}
		out[normalized] = cloneWorkspaceJSONMap(cfg)
	}
	return out, nil
}

func normalizeWorkspaceProviders(in map[string]repo.ProviderSetting) (map[string]repo.ProviderSetting, error) {
	out := map[string]repo.ProviderSetting{}
	for rawID, rawSetting := range in {
		id := normalizeProviderID(rawID)
		if id == "" {
			return nil, errors.New("provider id cannot be empty")
		}
		if id == "demo" {
			continue
		}
		setting := rawSetting
		normalizeProviderSetting(&setting)
		if setting.TimeoutMS < 0 {
			return nil, fmt.Errorf("provider %q timeout_ms must be >= 0", rawID)
		}
		setting.Headers = sanitizeStringMap(setting.Headers)
		setting.ModelAliases = sanitizeStringMap(setting.ModelAliases)
		out[id] = setting
	}
	return out, nil
}

func normalizeWorkspaceSkills(in map[string]domain.SkillSpec, dataDir string) (map[string]domain.SkillSpec, error) {
	out := map[string]domain.SkillSpec{}
	for rawName, rawSpec := range in {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, errors.New("skill name cannot be empty")
		}
		content := strings.TrimSpace(rawSpec.Content)
		if content == "" {
			return nil, fmt.Errorf("skill %q content is required", name)
		}
		source := strings.TrimSpace(rawSpec.Source)
		if source == "" {
			source = "customized"
		}
		out[name] = domain.SkillSpec{
			Name:       name,
			Content:    rawSpec.Content,
			Source:     source,
			Path:       filepath.Join(dataDir, "skills", name),
			References: safeMap(rawSpec.References),
			Scripts:    safeMap(rawSpec.Scripts),
			Enabled:    rawSpec.Enabled,
		}
	}
	return out, nil
}

func normalizeWorkspaceActiveLLM(in domain.ModelSlotConfig, providers map[string]repo.ProviderSetting) (domain.ModelSlotConfig, error) {
	providerID := normalizeProviderID(in.ProviderID)
	modelID := strings.TrimSpace(in.Model)
	if providerID == "" && modelID == "" {
		return domain.ModelSlotConfig{}, nil
	}
	if providerID == "" || modelID == "" {
		return domain.ModelSlotConfig{}, errors.New("provider_id and model must be set together")
	}
	if _, ok := providers[providerID]; !ok {
		return domain.ModelSlotConfig{}, errors.New("active_llm provider not found")
	}
	return domain.ModelSlotConfig{ProviderID: providerID, Model: modelID}, nil
}

func cloneWorkspaceEnvs(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneWorkspaceChannels(in domain.ChannelConfigMap) domain.ChannelConfigMap {
	out := domain.ChannelConfigMap{}
	for name, cfg := range in {
		out[name] = cloneWorkspaceJSONMap(cfg)
	}
	return out
}

func cloneWorkspaceProviders(in map[string]repo.ProviderSetting) map[string]repo.ProviderSetting {
	out := map[string]repo.ProviderSetting{}
	for id, raw := range in {
		setting := raw
		normalizeProviderSetting(&setting)
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
		out[id] = setting
	}
	return out
}

func cloneWorkspaceSkills(in map[string]domain.SkillSpec) map[string]domain.SkillSpec {
	out := map[string]domain.SkillSpec{}
	for name, spec := range in {
		out[name] = cloneWorkspaceSkill(spec)
	}
	return out
}

func cloneWorkspaceSkill(in domain.SkillSpec) domain.SkillSpec {
	return domain.SkillSpec{
		Name:       in.Name,
		Content:    in.Content,
		Source:     in.Source,
		Path:       in.Path,
		References: cloneWorkspaceJSONMap(in.References),
		Scripts:    cloneWorkspaceJSONMap(in.Scripts),
		Enabled:    in.Enabled,
	}
}

func cloneWorkspaceJSONMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf, &out); err != nil {
		return map[string]interface{}{}
	}
	if out == nil {
		return map[string]interface{}{}
	}
	return out
}

func jsonSize(v interface{}) int {
	buf, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(buf)
}

func (s *Server) listChannels(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getAdminService().ListChannels()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listChannelTypes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.getAdminService().ListChannelTypes())
}

func (s *Server) putChannels(w http.ResponseWriter, r *http.Request) {
	var body domain.ChannelConfigMap
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	out, err := s.getAdminService().ReplaceChannels(body)
	if err != nil {
		if validation := (*adminservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	out, found, err := s.getAdminService().GetChannel(chi.URLParam(r, "channel_name"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "channel not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channel_name")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.getAdminService().PutChannel(name, body); err != nil {
		if validation := (*adminservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func mapRunnerError(err error) (status int, code string, message string) {
	var runnerErr *runner.RunnerError
	if errors.As(err, &runnerErr) {
		switch runnerErr.Code {
		case runner.ErrorCodeProviderNotConfigured:
			return http.StatusBadRequest, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderNotSupported:
			return http.StatusBadRequest, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderRequestFailed:
			return http.StatusBadGateway, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderInvalidReply:
			return http.StatusBadGateway, runnerErr.Code, runnerErr.Message
		default:
			return http.StatusInternalServerError, "runner_error", "runner execution failed"
		}
	}
	return http.StatusInternalServerError, "runner_error", "runner execution failed"
}

func mapToolError(err error) (status int, code string, message string) {
	var te *toolError
	if errors.As(err, &te) {
		switch te.Code {
		case "tool_disabled":
			return http.StatusForbidden, te.Code, te.Message
		case "tool_not_supported":
			return http.StatusBadRequest, te.Code, te.Message
		case "tool_invoke_failed":
			switch {
			case errors.Is(te.Err, errRequestUserInputUnavailableMode):
				return http.StatusBadRequest, "tool_not_available", "request_user_input is only available in Plan mode"
			case errors.Is(te.Err, errRequestUserInputQuestionsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "request_user_input questions are invalid"
			case errors.Is(te.Err, errRequestUserInputTimeout):
				return http.StatusGatewayTimeout, "tool_timeout", "request_user_input timed out waiting for answer"
			case errors.Is(te.Err, errRequestUserInputConflict):
				return http.StatusConflict, "tool_conflict", "request_user_input request_id is already pending"
			case errors.Is(te.Err, errUpdatePlanInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "update_plan payload is invalid"
			case errors.Is(te.Err, errUpdatePlanChatNotFound):
				return http.StatusBadRequest, "invalid_request", "update_plan target chat not found"
			case errors.Is(te.Err, errApplyPatchPayloadMissing):
				return http.StatusBadRequest, "invalid_tool_input", "apply_patch patch payload is required"
			case errors.Is(te.Err, errApplyPatchBinaryMissing):
				return http.StatusBadGateway, "tool_runtime_unavailable", "apply_patch runtime is unavailable on current host"
			case errors.Is(te.Err, errMultiAgentIDRequired):
				return http.StatusBadRequest, "invalid_tool_input", "id or agent_id is required"
			case errors.Is(te.Err, errMultiAgentIDsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "ids must be a non-empty array of agent ids"
			case errors.Is(te.Err, errMultiAgentTaskRequired):
				return http.StatusBadRequest, "invalid_tool_input", "spawn_agent task is required"
			case errors.Is(te.Err, errMultiAgentInputRequired), errors.Is(te.Err, errMultiAgentEmptyAgentInput):
				return http.StatusBadRequest, "invalid_tool_input", "agent input is required"
			case errors.Is(te.Err, errMultiAgentItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "items must be a non-empty array"
			case errors.Is(te.Err, errMultiAgentInputConflict):
				return http.StatusBadRequest, "invalid_tool_input", "provide either message/input or items, but not both"
			case errors.Is(te.Err, errMultiAgentPromptMode), errors.Is(te.Err, errMultiAgentCollabMode):
				return http.StatusBadRequest, "invalid_tool_input", "multi-agent mode payload is invalid"
			case errors.Is(te.Err, errMultiAgentDepthExceeded):
				return http.StatusBadRequest, "tool_not_available", "sub-agent nesting depth is exceeded"
			case errors.Is(te.Err, errMultiAgentNotFound):
				return http.StatusNotFound, "invalid_request", "managed sub-agent not found"
			case errors.Is(te.Err, errMultiAgentClosed):
				return http.StatusConflict, "invalid_request", "managed sub-agent is closed"
			case errors.Is(te.Err, errMultiAgentConflict):
				return http.StatusConflict, "tool_conflict", "managed sub-agent already exists"
			case errors.Is(te.Err, errMultiAgentBusy):
				return http.StatusConflict, "tool_conflict", "managed sub-agent is running"
			case errors.Is(te.Err, errMultiAgentNoPendingInput):
				return http.StatusBadRequest, "invalid_request", "managed sub-agent has no pending input"
			case errors.Is(te.Err, plugin.ErrShellToolCommandMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input command is required"
			case errors.Is(te.Err, plugin.ErrShellToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrShellToolExecutorUnavailable):
				return http.StatusBadGateway, "tool_runtime_unavailable", "shell executor is unavailable on current host"
			case errors.Is(te.Err, plugin.ErrShellToolSessionIDInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input session_id is required and must be > 0"
			case errors.Is(te.Err, plugin.ErrShellToolSessionNotFound):
				return http.StatusBadRequest, "invalid_tool_input", "shell session not found or already closed"
			case errors.Is(te.Err, plugin.ErrShellToolStdinUnsupported):
				return http.StatusBadRequest, "invalid_tool_input", "shell session does not accept stdin writes"
			case errors.Is(te.Err, plugin.ErrShellToolSessionLimitReached):
				return http.StatusBadGateway, "tool_runtime_unavailable", "shell session limit reached"
			case errors.Is(te.Err, plugin.ErrShellToolEscalationDenied):
				return http.StatusBadRequest, "tool_permission_denied", "shell escalation requires approval policy on-request"
			case errors.Is(te.Err, plugin.ErrFileLinesToolPathMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input path is required"
			case errors.Is(te.Err, plugin.ErrFileLinesToolPathInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input path is invalid"
			case errors.Is(te.Err, plugin.ErrFileLinesToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrFileLinesToolStartInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input start must be an integer >= 1"
			case errors.Is(te.Err, plugin.ErrFileLinesToolEndInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input end must be an integer >= 1"
			case errors.Is(te.Err, plugin.ErrFileLinesToolRangeInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is invalid"
			case errors.Is(te.Err, plugin.ErrFileLinesToolRangeTooLarge):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is too large"
			case errors.Is(te.Err, plugin.ErrFileLinesToolContentMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input content is required"
			case errors.Is(te.Err, plugin.ErrFileLinesToolOutOfRange):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is out of file bounds"
			case errors.Is(te.Err, plugin.ErrFileLinesToolFileNotFound):
				return http.StatusBadRequest, "invalid_tool_input", "target file does not exist"
			case errors.Is(te.Err, plugin.ErrBrowserToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrBrowserToolTaskMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input task is required"
			case errors.Is(te.Err, plugin.ErrSearchToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrSearchToolQueryMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input query is required"
			case errors.Is(te.Err, plugin.ErrSearchToolProviderUnsupported):
				return http.StatusBadRequest, "invalid_tool_input", "tool input provider is unsupported"
			case errors.Is(te.Err, plugin.ErrSearchToolProviderUnconfigured):
				return http.StatusBadRequest, "invalid_tool_input", "tool input provider is not configured"
			case errors.Is(te.Err, plugin.ErrFindToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrFindToolPathMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input path is required"
			case errors.Is(te.Err, plugin.ErrFindToolPathInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input path is invalid"
			case errors.Is(te.Err, plugin.ErrFindToolPatternMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input pattern is required"
			case errors.Is(te.Err, plugin.ErrFindToolFileNotFound):
				return http.StatusBadRequest, "invalid_tool_input", "target file does not exist"
			default:
				return http.StatusBadGateway, te.Code, te.Message
			}
		case "tool_invalid_result":
			return http.StatusBadGateway, te.Code, te.Message
		default:
			return http.StatusInternalServerError, "tool_error", "tool execution failed"
		}
	}
	return http.StatusInternalServerError, "tool_error", "tool execution failed"
}

func compactFeedbackField(raw string, maxLen int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	compact := strings.Join(strings.Fields(trimmed), " ")
	if maxLen <= 0 {
		return compact
	}
	runes := []rune(compact)
	if len(runes) <= maxLen {
		return compact
	}
	return string(runes[:maxLen]) + "...(truncated)"
}

func formatProviderToolArgumentsErrorFeedback(toolName, rawArguments, parseErr string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "unknown_tool"
	}
	detail := compactFeedbackField(parseErr, 160)
	if detail == "" {
		detail = "invalid json arguments"
	}
	raw := compactFeedbackField(rawArguments, 320)
	if raw == "" {
		raw = "{}"
	}
	return fmt.Sprintf(
		"tool_error code=invalid_tool_input message=provider tool call arguments for %s are invalid detail=%s raw_arguments=%s",
		name,
		detail,
		raw,
	)
}

func formatToolErrorFeedback(err error) string {
	if err == nil {
		return "tool_error code=tool_error message=tool execution failed"
	}
	_, code, message := mapToolError(err)
	if strings.TrimSpace(code) == "" {
		code = "tool_error"
	}
	if strings.TrimSpace(message) == "" {
		message = "tool execution failed"
	}
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		return fmt.Sprintf("tool_error code=%s message=%s", code, message)
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if detail == message {
		return fmt.Sprintf("tool_error code=%s message=%s", code, message)
	}
	return fmt.Sprintf("tool_error code=%s message=%s detail=%s", code, message, detail)
}

func (s *Server) collectProviderCatalog() ([]domain.ProviderInfo, map[string]string, domain.ModelSlotConfig) {
	out := make([]domain.ProviderInfo, 0)
	defaults := map[string]string{}
	active := domain.ModelSlotConfig{}

	s.store.Read(func(st *repo.State) {
		active = st.ActiveLLM
		settingsByID := map[string]repo.ProviderSetting{}

		for rawID, setting := range st.Providers {
			id := normalizeProviderID(rawID)
			if id == "" {
				continue
			}
			normalizeProviderSetting(&setting)
			settingsByID[id] = setting
		}

		ids := make([]string, 0, len(settingsByID))
		for id := range settingsByID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			setting := settingsByID[id]
			out = append(out, buildProviderInfo(id, setting))
			defaults[id] = provider.DefaultModelID(id)
		}
	})
	return out, defaults, active
}

func buildProviderInfo(providerID string, setting repo.ProviderSetting) domain.ProviderInfo {
	normalizeProviderSetting(&setting)
	spec := provider.ResolveProvider(providerID)
	apiKey := resolveProviderAPIKey(providerID, setting)
	return domain.ProviderInfo{
		ID:                 providerID,
		Name:               spec.Name,
		DisplayName:        resolveProviderDisplayName(setting, spec.Name),
		OpenAICompatible:   provider.ResolveAdapter(providerID) == provider.AdapterOpenAICompatible,
		APIKeyPrefix:       spec.APIKeyPrefix,
		Models:             provider.ResolveModels(providerID, setting.ModelAliases),
		ReasoningEffort:    setting.ReasoningEffort,
		Headers:            sanitizeStringMap(setting.Headers),
		TimeoutMS:          setting.TimeoutMS,
		ModelAliases:       sanitizeStringMap(setting.ModelAliases),
		AllowCustomBaseURL: spec.AllowCustomBaseURL,
		Enabled:            providerEnabled(setting),
		HasAPIKey:          strings.TrimSpace(apiKey) != "",
		CurrentAPIKey:      maskKey(apiKey),
		CurrentBaseURL:     resolveProviderBaseURL(providerID, setting),
	}
}

func resolveProviderAPIKey(providerID string, setting repo.ProviderSetting) string {
	if key := strings.TrimSpace(setting.APIKey); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv(providerEnvPrefix(providerID) + "_API_KEY"))
}

func resolveProviderBaseURL(providerID string, setting repo.ProviderSetting) string {
	if baseURL := strings.TrimSpace(setting.BaseURL); baseURL != "" {
		return baseURL
	}
	if envBaseURL := strings.TrimSpace(os.Getenv(providerEnvPrefix(providerID) + "_BASE_URL")); envBaseURL != "" {
		return envBaseURL
	}
	return provider.ResolveProvider(providerID).DefaultBaseURL
}

func resolveProviderDisplayName(setting repo.ProviderSetting, defaultName string) string {
	if displayName := strings.TrimSpace(setting.DisplayName); displayName != "" {
		return displayName
	}
	return strings.TrimSpace(defaultName)
}

func providerEnvPrefix(providerID string) string {
	return provider.EnvPrefix(providerID)
}

func normalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func providerEnabled(setting repo.ProviderSetting) bool {
	if setting.Enabled == nil {
		return true
	}
	return *setting.Enabled
}

func normalizeProviderSetting(setting *repo.ProviderSetting) {
	if setting == nil {
		return
	}
	setting.DisplayName = strings.TrimSpace(setting.DisplayName)
	setting.APIKey = strings.TrimSpace(setting.APIKey)
	setting.BaseURL = strings.TrimSpace(setting.BaseURL)
	setting.ReasoningEffort = strings.ToLower(strings.TrimSpace(setting.ReasoningEffort))
	if setting.Enabled == nil {
		enabled := true
		setting.Enabled = &enabled
	}
	if setting.Headers == nil {
		setting.Headers = map[string]string{}
	}
	if setting.ModelAliases == nil {
		setting.ModelAliases = map[string]string{}
	}
}

func sanitizeStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func sanitizeModelAliases(raw *map[string]string) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	out := map[string]string{}
	for key, value := range *raw {
		alias := strings.TrimSpace(key)
		modelID := strings.TrimSpace(value)
		if alias == "" || modelID == "" {
			return nil, errors.New("model_aliases requires non-empty key and value")
		}
		out[alias] = modelID
	}
	return out, nil
}

func getProviderSettingByID(st *repo.State, providerID string) repo.ProviderSetting {
	if setting, ok := findProviderSettingByID(st, providerID); ok {
		return setting
	}
	setting := repo.ProviderSetting{}
	normalizeProviderSetting(&setting)
	return setting
}

func findProviderSettingByID(st *repo.State, providerID string) (repo.ProviderSetting, bool) {
	if st == nil {
		return repo.ProviderSetting{}, false
	}
	if setting, ok := st.Providers[providerID]; ok {
		return setting, true
	}
	for key, setting := range st.Providers {
		if normalizeProviderID(key) == providerID {
			return setting, true
		}
	}
	return repo.ProviderSetting{}, false
}

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, code int, errCode, message string, details interface{}) {
	writeJSON(w, code, domain.APIErrorBody{Error: domain.APIError{Code: errCode, Message: message, Details: details}})
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func maskKey(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-3:]
}

func safeMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

func (s *Server) buildCodexSystemLayers(runtime TurnRuntimeSnapshot) ([]systemPromptLayer, error) {
	source, content, err := loadRequiredSystemLayer([]string{codexBasePromptRelativePath})
	if err != nil {
		return nil, err
	}

	layers := []systemPromptLayer{
		{
			Name:    "codex_base_system",
			Role:    "system",
			Source:  source,
			Content: systempromptservice.FormatLayerSourceContent(source, content),
		},
	}
	if !s.cfg.EnablePromptTemplates {
		if layers, err = appendCodexReviewPromptLayerIfNeeded(layers, runtime); err != nil {
			return nil, err
		}
		return appendCodexOptionalLayer(layers, "codex_local_policy_system", codexLocalPolicyRelativePath, nil)
	}

	if layers, err = appendCodexOptionalLayer(layers, "codex_orchestrator_system", codexOrchestratorRelativePath, nil); err != nil {
		return nil, err
	}
	if layers, err = s.appendCodexModelInstructionsLayer(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexReviewPromptLayerIfNeeded(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexReviewHistoryLayersIfNeeded(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexCollaborationLayer(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexCompactLayersIfNeeded(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexMemoryLayersIfNeeded(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalLayer(layers, "codex_experimental_collab_system", codexExperimentalRelativePath, nil); err != nil {
		return nil, err
	}
	if layers, err = appendCodexSearchToolLayer(layers, runtime); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalLayer(layers, "codex_local_policy_system", codexLocalPolicyRelativePath, nil); err != nil {
		return nil, err
	}
	return layers, nil
}

func appendCodexCollaborationLayer(
	layers []systemPromptLayer,
	runtime TurnRuntimeSnapshot,
) ([]systemPromptLayer, error) {
	modeName := normalizeCollaborationModeName(runtime.Mode.CollaborationMode)
	if modeName == collaborationModePlanName {
		return appendCodexOptionalLayer(layers, "codex_collaboration_plan_system", codexCollabPlanRelativePath, nil)
	}
	if modeName == collaborationModeExecuteName {
		return appendCodexOptionalLayer(layers, "codex_collaboration_execute_system", codexCollabExecuteRelativePath, nil)
	}
	if modeName == collaborationModePairProgrammingName {
		return appendCodexOptionalLayer(
			layers,
			"codex_collaboration_pair_programming_system",
			codexCollabPairProgrammingPath,
			nil,
		)
	}

	templateVars := buildCodexTemplateVars(runtime)
	return appendCodexOptionalTemplateLayer(
		layers,
		"codex_collaboration_default_system",
		codexCollabDefaultRelativePath,
		templateVars,
		[]string{"KNOWN_MODE_NAMES", "TURN_MODE", "REQUEST_USER_INPUT_AVAILABLE"},
	)
}

func appendCodexReviewPromptLayerIfNeeded(
	layers []systemPromptLayer,
	runtime TurnRuntimeSnapshot,
) ([]systemPromptLayer, error) {
	if !runtime.Mode.ReviewTask {
		return layers, nil
	}
	source, content, err := loadRequiredSystemLayer([]string{codexReviewPromptRelativePath})
	if err != nil {
		return nil, err
	}
	return append(layers, systemPromptLayer{
		Name:    "codex_review_system",
		Role:    "system",
		Source:  source,
		Content: systempromptservice.FormatLayerSourceContent(source, content),
	}), nil
}

func appendCodexReviewHistoryLayersIfNeeded(
	layers []systemPromptLayer,
	runtime TurnRuntimeSnapshot,
) ([]systemPromptLayer, error) {
	if !runtime.Mode.ReviewTask {
		return layers, nil
	}
	var err error
	if layers, err = appendCodexOptionalLayer(layers, "codex_review_history_completed_system", codexReviewHistoryCompletedPath, nil); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalLayer(layers, "codex_review_history_interrupted_system", codexReviewHistoryInterruptedPath, nil); err != nil {
		return nil, err
	}
	return layers, nil
}

func appendCodexCompactLayersIfNeeded(
	layers []systemPromptLayer,
	runtime TurnRuntimeSnapshot,
) ([]systemPromptLayer, error) {
	if !runtime.Mode.CompactTask {
		return layers, nil
	}
	var err error
	if layers, err = appendCodexOptionalLayer(layers, "codex_compact_prompt_system", codexCompactPromptRelativePath, nil); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalLayer(layers, "codex_compact_summary_prefix_system", codexCompactSummaryPrefixPath, nil); err != nil {
		return nil, err
	}
	return layers, nil
}

func appendCodexMemoryLayersIfNeeded(
	layers []systemPromptLayer,
	runtime TurnRuntimeSnapshot,
) ([]systemPromptLayer, error) {
	if !runtime.Mode.MemoryTask {
		return layers, nil
	}

	memoryVars := buildCodexMemoryTemplateVars(runtime.SessionID)
	requiredKeysForReadPath := []string{"base_path", "memory_summary"}
	requiredKeysForStageOneInput := []string{"rollout_path", "rollout_cwd", "rollout_contents"}
	requiredKeysForConsolidation := []string{"memory_root"}

	var err error
	if layers, err = appendCodexOptionalTemplateLayer(
		layers,
		"codex_memories_read_path_system",
		codexMemoriesReadPathPath,
		memoryVars,
		requiredKeysForReadPath,
	); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalLayer(layers, "codex_memories_stage_one_system", codexMemoriesStageOneSystemPath, nil); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalTemplateLayer(
		layers,
		"codex_memories_stage_one_input_system",
		codexMemoriesStageOneInputPath,
		memoryVars,
		requiredKeysForStageOneInput,
	); err != nil {
		return nil, err
	}
	if layers, err = appendCodexOptionalTemplateLayer(
		layers,
		"codex_memories_consolidation_system",
		codexMemoriesConsolidationPath,
		memoryVars,
		requiredKeysForConsolidation,
	); err != nil {
		return nil, err
	}
	return layers, nil
}

func appendCodexSearchToolLayer(layers []systemPromptLayer, runtime TurnRuntimeSnapshot) ([]systemPromptLayer, error) {
	return appendCodexOptionalTemplateLayer(
		layers,
		"codex_search_tool_system",
		codexSearchToolDescriptionPath,
		buildCodexSearchToolTemplateVars(runtime),
		[]string{"app_names"},
	)
}

func (s *Server) appendCodexModelInstructionsLayer(
	layers []systemPromptLayer,
	runtime TurnRuntimeSnapshot,
) ([]systemPromptLayer, error) {
	_, personalityContent, personalityLoaded, err := loadOptionalSystemLayer(codexPersonalityRelativePath)
	if err != nil {
		return nil, err
	}
	personality := s.resolveCodexPersonality(runtime.Personality)
	modelSlug := s.resolveCodexModelSlug(runtime.ModelSlug)
	sourceMode := normalizeCodexPromptSource(s.cfg.CodexPromptSource)

	fileSource, fileContent, fileOK, err := resolveCodexModelInstructionsFromFile(personalityContent, personalityLoaded)
	if err != nil {
		return nil, err
	}

	var (
		catalogSource  string
		catalogContent string
		catalogMeta    codexpromptservice.ResolveMeta
		catalogErr     error
	)
	needCatalog := sourceMode == codexPromptSourceCatalog || (sourceMode == codexPromptSourceFile && s.cfg.EnableCodexPromptShadowCompare)
	if needCatalog {
		catalogSource, catalogContent, catalogMeta, catalogErr = s.resolveCodexModelInstructionsFromCatalog(modelSlug, personality)
	}
	if sourceMode == codexPromptSourceFile && s.cfg.EnableCodexPromptShadowCompare {
		logCodexPromptShadowDiff(runtime.SessionID, modelSlug, fileContent, catalogContent, catalogMeta, catalogErr)
	}

	selectedSource, selectedContent, selectedOK := "", "", false
	switch sourceMode {
	case codexPromptSourceCatalog:
		if catalogErr != nil {
			log.Printf("warning: fallback codex model instructions to file due to catalog error: model_slug=%s err=%v", modelSlug, catalogErr)
		} else if strings.TrimSpace(catalogContent) != "" {
			selectedSource = catalogSource
			selectedContent = catalogContent
			selectedOK = true
		}
		if !selectedOK && fileOK {
			selectedSource = fileSource
			selectedContent = fileContent
			selectedOK = true
		}
	default:
		if fileOK {
			selectedSource = fileSource
			selectedContent = fileContent
			selectedOK = true
		}
	}
	if !selectedOK {
		return layers, nil
	}
	return append(layers, systemPromptLayer{
		Name:    "codex_model_instructions_system",
		Role:    "system",
		Source:  selectedSource,
		Content: systempromptservice.FormatLayerSourceContent(selectedSource, selectedContent),
	}), nil
}

func resolveCodexModelInstructionsFromFile(personalityContent string, personalityLoaded bool) (string, string, bool, error) {
	source, content, ok, err := loadOptionalSystemLayer(codexModelTemplateRelativePath)
	if err != nil {
		return "", "", false, err
	}
	if !ok {
		return "", "", false, nil
	}
	if personalityLoaded {
		content = replaceTemplateVariable(content, "personality", personalityContent)
	}
	if unresolved := templatePlaceholderKeys(content); len(unresolved) > 0 {
		log.Printf(
			"warning: skip codex model instructions from file due to unresolved template vars: %s",
			strings.Join(unresolved, ", "),
		)
		return "", "", false, nil
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", false, nil
	}
	return source, content, true, nil
}

func (s *Server) resolveCodexModelInstructionsFromCatalog(modelSlug, personality string) (string, string, codexpromptservice.ResolveMeta, error) {
	meta := codexpromptservice.ResolveMeta{}
	if s == nil || s.codexPromptResolver == nil {
		return "", "", meta, errors.New("codex prompt resolver is unavailable")
	}

	resolved, meta, err := s.codexPromptResolver.Resolve(modelSlug, personality)
	if err != nil && errors.Is(err, codexpromptservice.ErrModelNotFound) && codexpromptservice.NormalizeModelSlug(modelSlug) != defaultCodexModelSlug {
		fallbackResolved, fallbackMeta, fallbackErr := s.codexPromptResolver.Resolve(defaultCodexModelSlug, personality)
		if fallbackErr == nil {
			resolved = fallbackResolved
			meta = fallbackMeta
			if strings.TrimSpace(meta.FallbackReason) == "" {
				meta.FallbackReason = "fallback_to_default_slug"
			} else {
				meta.FallbackReason = "fallback_to_default_slug|" + meta.FallbackReason
			}
			err = nil
		}
	}
	if err != nil {
		return "", "", meta, err
	}

	resolved = strings.TrimSpace(resolved)
	if resolved == "" {
		return "", "", meta, errors.New("resolved codex model instructions are empty")
	}
	sourceSlug := strings.TrimSpace(meta.SourceSlug)
	if sourceSlug == "" {
		sourceSlug = codexpromptservice.NormalizeModelSlug(modelSlug)
	}
	if sourceSlug == "" {
		sourceSlug = defaultCodexModelSlug
	}
	return codexRuntimeCatalogRelativePath + "#" + sourceSlug, resolved, meta, nil
}

func (s *Server) resolveCodexModelSlug(explicitSlug string) string {
	if slug := codexpromptservice.NormalizeModelSlug(explicitSlug); slug != "" {
		return slug
	}
	resolved := ""
	if s != nil && s.store != nil {
		s.store.Read(func(st *repo.State) {
			if st == nil {
				return
			}
			resolved = codexpromptservice.NormalizeModelSlug(st.ActiveLLM.Model)
		})
	}
	if resolved == "" {
		return defaultCodexModelSlug
	}
	return resolved
}

func (s *Server) resolveCodexPersonality(explicit string) string {
	trimmed := strings.TrimSpace(explicit)
	if trimmed == "" {
		return defaultCodexPersonalityFromPath(codexPersonalityRelativePath)
	}
	resolved, downgraded := codexpromptservice.NormalizePersonality(trimmed)
	if downgraded {
		log.Printf("warning: invalid codex personality %q, fallback to %s", trimmed, resolved)
	}
	return resolved
}

func defaultCodexPersonalityFromPath(path string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	if strings.Contains(base, codexpromptservice.PersonalityFriendly) {
		return codexpromptservice.PersonalityFriendly
	}
	return defaultCodexPersonality
}

func logCodexPromptShadowDiff(
	sessionID string,
	modelSlug string,
	fileContent string,
	catalogContent string,
	catalogMeta codexpromptservice.ResolveMeta,
	catalogErr error,
) {
	fileHash := normalizedLayerContentHash(fileContent)
	catalogHash := normalizedLayerContentHash(catalogContent)

	diffReason := ""
	switch {
	case catalogErr != nil:
		diffReason = "catalog_resolve_failed"
	case fileHash == "" && catalogHash == "":
		return
	case fileHash == "":
		diffReason = "file_layer_missing"
	case catalogHash == "":
		diffReason = "catalog_layer_missing"
	case fileHash != catalogHash:
		diffReason = "content_hash_mismatch"
	default:
		return
	}
	if fallback := strings.TrimSpace(catalogMeta.FallbackReason); fallback != "" {
		diffReason = diffReason + "|" + fallback
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "-"
	}
	if strings.TrimSpace(modelSlug) == "" {
		modelSlug = defaultCodexModelSlug
	}
	log.Printf(
		"codex_prompt_shadow_diff session_id=%s model_slug=%s file_hash=%s catalog_hash=%s diff_reason=%s",
		sessionID,
		modelSlug,
		fileHash,
		catalogHash,
		diffReason,
	)
}

func normalizeCodexPromptSource(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case codexPromptSourceCatalog:
		return codexPromptSourceCatalog
	case codexPromptSourceFile:
		fallthrough
	default:
		return codexPromptSourceFile
	}
}

func appendCodexOptionalLayer(
	layers []systemPromptLayer,
	layerName string,
	path string,
	render func(string) string,
) ([]systemPromptLayer, error) {
	source, content, ok, err := loadOptionalSystemLayer(path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return layers, nil
	}
	if render != nil {
		content = strings.TrimSpace(render(content))
		if content == "" {
			return layers, nil
		}
	}
	return append(layers, systemPromptLayer{
		Name:    layerName,
		Role:    "system",
		Source:  source,
		Content: systempromptservice.FormatLayerSourceContent(source, content),
	}), nil
}

func appendCodexOptionalLayerFromCandidates(
	layers []systemPromptLayer,
	layerName string,
	candidatePaths []string,
	render func(string) string,
) ([]systemPromptLayer, error) {
	source, content, ok, err := loadOptionalSystemLayerFromCandidates(candidatePaths)
	if err != nil {
		return nil, err
	}
	if !ok {
		return layers, nil
	}
	if render != nil {
		content = strings.TrimSpace(render(content))
		if content == "" {
			return layers, nil
		}
	}
	return append(layers, systemPromptLayer{
		Name:    layerName,
		Role:    "system",
		Source:  source,
		Content: systempromptservice.FormatLayerSourceContent(source, content),
	}), nil
}

func appendCodexOptionalTemplateLayer(
	layers []systemPromptLayer,
	layerName string,
	path string,
	vars map[string]string,
	requiredKeys []string,
) ([]systemPromptLayer, error) {
	source, content, ok, err := loadOptionalSystemLayer(path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return layers, nil
	}

	expectedKeys := expectedTemplateKeys(content, requiredKeys)
	missingKeys := make([]string, 0, len(expectedKeys))
	for _, key := range expectedKeys {
		if strings.TrimSpace(vars[key]) == "" {
			missingKeys = append(missingKeys, key)
		}
	}
	if len(missingKeys) > 0 {
		return nil, fmt.Errorf(
			"codex layer %s missing template vars: %s",
			layerName,
			strings.Join(missingKeys, ", "),
		)
	}

	rendered := strings.TrimSpace(renderTemplate(content, vars))
	if rendered == "" {
		return layers, nil
	}
	if unresolved := templatePlaceholderKeys(rendered); len(unresolved) > 0 {
		return nil, fmt.Errorf(
			"codex layer %s has unresolved template vars after render: %s",
			layerName,
			strings.Join(unresolved, ", "),
		)
	}

	return append(layers, systemPromptLayer{
		Name:    layerName,
		Role:    "system",
		Source:  source,
		Content: systempromptservice.FormatLayerSourceContent(source, rendered),
	}), nil
}

func loadRequiredSystemLayer(candidatePaths []string) (string, string, error) {
	var lastNotFound error
	for _, candidatePath := range candidatePaths {
		_, rawContent, err := readWorkspaceTextFileRawForPath(candidatePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				lastNotFound = err
				continue
			}
			return "", "", err
		}
		trimmed := strings.TrimSpace(rawContent)
		if trimmed == "" {
			return "", "", fmt.Errorf("system layer is empty: %s", candidatePath)
		}
		return candidatePath, trimmed, nil
	}
	if lastNotFound != nil {
		return "", "", lastNotFound
	}
	return "", "", fmt.Errorf("%w: no candidate paths configured", os.ErrNotExist)
}

func loadOptionalSystemLayer(path string) (string, string, bool, error) {
	source, rawContent, err := readWorkspaceTextFileRawForPath(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	trimmed := strings.TrimSpace(rawContent)
	if trimmed == "" {
		return "", "", false, nil
	}
	return source, trimmed, true, nil
}

func loadOptionalSystemLayerFromCandidates(candidatePaths []string) (string, string, bool, error) {
	for _, candidatePath := range candidatePaths {
		source, content, ok, err := loadOptionalSystemLayer(candidatePath)
		if err != nil {
			return "", "", false, err
		}
		if ok {
			return source, content, true, nil
		}
	}
	return "", "", false, nil
}

func replaceTemplateVariable(content, key, value string) string {
	replacer := strings.NewReplacer(
		"{{"+key+"}}", value,
		"{{ "+key+" }}", value,
	)
	return replacer.Replace(content)
}

func renderTemplate(content string, vars map[string]string) string {
	if len(vars) == 0 {
		return content
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	replacements := make([]string, 0, len(keys)*4)
	for _, key := range keys {
		value := vars[key]
		replacements = append(
			replacements,
			"{{"+key+"}}", value,
			"{{ "+key+" }}", value,
		)
	}
	return strings.NewReplacer(replacements...).Replace(content)
}

func hasTemplatePlaceholder(content, key string) bool {
	return strings.Contains(content, "{{"+key+"}}") || strings.Contains(content, "{{ "+key+" }}")
}

var templatePlaceholderPattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func templatePlaceholderKeys(content string) []string {
	matches := templatePlaceholderPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	keys := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		key := strings.TrimSpace(match[1])
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func expectedTemplateKeys(content string, requiredKeys []string) []string {
	seen := map[string]struct{}{}
	keys := make([]string, 0, len(requiredKeys))
	appendKey := func(raw string) {
		key := strings.TrimSpace(raw)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for _, key := range requiredKeys {
		appendKey(key)
	}
	for _, key := range templatePlaceholderKeys(content) {
		appendKey(key)
	}
	sort.Strings(keys)
	return keys
}

func knownCollaborationModeNames() string {
	names := supportedCollaborationModeNames()
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
	}
}

func supportedCollaborationModeNames() []string {
	return []string{
		collaborationModeDefaultName,
		collaborationModePlanName,
		collaborationModeExecuteName,
		collaborationModePairProgrammingName,
	}
}

func buildCodexMemoryTemplateVars(sessionID string) map[string]string {
	repoRoot, memoryRoot, err := resolveCodexMemoryRootContext()
	if err != nil {
		repoRoot = "."
		memoryRoot = codexMemoryDefaultRootRelativePath
	}
	safeSession := sanitizeCodexMemorySlug(sessionID)
	if safeSession == "" {
		safeSession = "latest"
	}
	rolloutPath := filepath.ToSlash(filepath.Join(memoryRoot, codexMemoryRolloutSummariesDirName, safeSession+"-latest.jsonl"))
	return map[string]string{
		"memory_root":      memoryRoot,
		"base_path":        memoryRoot,
		"memory_summary":   readCodexMemorySummaryForPrompt(),
		"rollout_path":     rolloutPath,
		"rollout_cwd":      filepath.ToSlash(repoRoot),
		"rollout_contents": readCodexMemoryRolloutForPrompt(rolloutPath),
	}
}

func readCodexMemoryRolloutForPrompt(rolloutPath string) string {
	path := strings.TrimSpace(rolloutPath)
	if path == "" {
		return "(none)"
	}
	content, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		return "(none)"
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return "(none)"
	}
	return truncateCodexMemoryTextByTokenLimit(trimmed, codexMemoryDefaultStageOneRolloutTokenLimit)
}

func buildCodexSearchToolTemplateVars(runtime TurnRuntimeSnapshot) map[string]string {
	return map[string]string{
		"app_names": joinOrNone(codexSearchAppNamesFromRuntime(runtime)),
	}
}

func codexSearchAppNamesFromRuntime(runtime TurnRuntimeSnapshot) []string {
	tools := normalizeTurnRuntimeToolNames(runtime.AvailableTools)
	if len(tools) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	appNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		if !strings.HasPrefix(tool, "mcp__") {
			continue
		}
		parts := strings.Split(tool, "__")
		if len(parts) < 3 {
			continue
		}
		appName := strings.TrimSpace(parts[1])
		if appName == "" {
			continue
		}
		if _, exists := seen[appName]; exists {
			continue
		}
		seen[appName] = struct{}{}
		appNames = append(appNames, appName)
	}
	sort.Strings(appNames)
	return appNames
}

func dedupeLayersByNormalizedContent(layers []systemPromptLayer) []systemPromptLayer {
	if len(layers) <= 1 {
		return layers
	}
	deduped := make([]systemPromptLayer, 0, len(layers))
	indexByHash := map[string]int{}
	priorityByHash := map[string]int{}
	for _, layer := range layers {
		normalized := normalizeLayerContentForDedupe(layer.Content)
		if normalized == "" {
			deduped = append(deduped, layer)
			continue
		}
		hash := hashNormalizedLayerContent(normalized)
		priority := codexLayerPriority(layer.Name)
		if index, exists := indexByHash[hash]; exists {
			if priority > priorityByHash[hash] {
				deduped[index] = layer
				priorityByHash[hash] = priority
			}
			continue
		}
		indexByHash[hash] = len(deduped)
		priorityByHash[hash] = priority
		deduped = append(deduped, layer)
	}
	return deduped
}

func codexLayerPriority(layerName string) int {
	switch layerName {
	case "codex_base_system",
		"codex_orchestrator_system",
		"codex_model_instructions_system",
		"codex_collaboration_default_system",
		"codex_collaboration_plan_system",
		"codex_collaboration_execute_system",
		"codex_collaboration_pair_programming_system",
		"codex_experimental_collab_system",
		"codex_compact_prompt_system",
		"codex_compact_summary_prefix_system",
		"codex_review_history_completed_system",
		"codex_review_history_interrupted_system",
		"codex_memories_read_path_system",
		"codex_memories_stage_one_system",
		"codex_memories_stage_one_input_system",
		"codex_memories_consolidation_system",
		"codex_search_tool_system":
		return 300
	case "codex_local_policy_system":
		return 200
	case "codex_tool_guide_system":
		return 100
	default:
		return 0
	}
}

func normalizeLayerContent(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(normalized)
}

func normalizeLayerContentForDedupe(content string) string {
	normalized := normalizeLayerContent(content)
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, "## ") {
		if split := strings.Index(normalized, "\n"); split >= 0 {
			normalized = strings.TrimSpace(normalized[split+1:])
		}
	}
	return normalizeLayerContent(normalized)
}

func hashNormalizedLayerContent(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", sum[:])
}

func normalizedLayerContentHash(content string) string {
	normalized := normalizeLayerContent(content)
	if normalized == "" {
		return ""
	}
	return hashNormalizedLayerContent(normalized)
}

func (s *Server) resolvePromptModeVariant(mode string) string {
	normalizedMode, ok := normalizePromptMode(mode)
	if !ok || normalizedMode == promptModeDefault {
		return promptModeVariantDefault
	}
	if s != nil && s.cfg.EnableCodexModeV2 {
		return promptModeVariantCodexV2
	}
	return promptModeVariantCodexV1
}

func summarizeLayerPreview(text string, limit int) string {
	return systempromptservice.SummarizeLayerPreview(text, limit)
}

func estimatePromptTokenCount(text string) int {
	return systempromptservice.EstimateTokenCount(text)
}

func workspaceAIToolsFileEntry() (workspaceFileEntry, bool) {
	relativePath, content, err := readAIToolsGuideRawWithPath()
	if err != nil {
		return workspaceFileEntry{}, false
	}
	return workspaceFileEntry{
		Path: relativePath,
		Kind: "config",
		Size: len([]byte(content)),
	}, true
}

func readAIToolsGuideRaw() (string, error) {
	_, content, err := readAIToolsGuideRawWithPath()
	if err != nil {
		return "", err
	}
	return content, nil
}

func readAIToolsGuideRawWithPath() (string, string, error) {
	guidePath, relativePath, err := resolveAIToolsGuidePathForRead()
	if err != nil {
		return "", "", err
	}
	content, err := os.ReadFile(guidePath)
	if err != nil {
		return "", "", err
	}
	return relativePath, string(content), nil
}

func writeAIToolsGuideRaw(content string) error {
	return writeAIToolsGuideRawForPath("", content)
}

func writeAIToolsGuideRawForPath(relativePath, content string) error {
	guidePath, _, err := resolveAIToolsGuidePathForWrite(relativePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(guidePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(guidePath, []byte(content), 0o644)
}

func readWorkspaceTextFileRawForPath(relativePath string) (string, string, error) {
	normalized, ok := normalizeAIToolsGuideRelativePath(relativePath)
	if !ok {
		return "", "", errors.New("invalid workspace text file path")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(repoRoot, filepath.FromSlash(normalized))
	content, err := os.ReadFile(target)
	if err != nil {
		return "", "", err
	}
	return normalized, string(content), nil
}

func writeWorkspaceTextFileRawForPath(relativePath, content string) error {
	normalized, ok := normalizeAIToolsGuideRelativePath(relativePath)
	if !ok {
		return errors.New("invalid workspace text file path")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	target := filepath.Join(repoRoot, filepath.FromSlash(normalized))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(content), 0o644)
}

func isWorkspaceTextFilePath(filePath string) bool {
	return isWorkspaceDocsAIFilePath(filePath) ||
		isWorkspacePromptFilePath(filePath) ||
		isAIToolsWorkspaceFilePath(filePath)
}

func isWorkspaceDocsAIFilePath(filePath string) bool {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return false
	}
	return strings.HasPrefix(path, workspaceDocsAIDir+"/")
}

func isWorkspacePromptFilePath(filePath string) bool {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return false
	}
	return strings.HasPrefix(path, workspacePromptsDir+"/") ||
		strings.HasPrefix(path, workspacePromptDir+"/")
}

func isAIToolsWorkspaceFilePath(filePath string) bool {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return false
	}
	candidates, err := aiToolsGuidePathCandidates()
	if err != nil {
		return false
	}
	for _, candidate := range candidates {
		if path == candidate {
			return true
		}
	}
	return false
}

func resolveAIToolsGuidePathForRead() (string, string, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	candidates, err := aiToolsGuidePathCandidates()
	if err != nil {
		return "", "", err
	}
	for _, relativePath := range candidates {
		guidePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
		info, statErr := os.Stat(guidePath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return "", "", statErr
		}
		if info.IsDir() {
			continue
		}
		return guidePath, relativePath, nil
	}
	return "", "", fmt.Errorf("%w: ai tools guide not found in %s", os.ErrNotExist, strings.Join(candidates, ", "))
}

func resolveAIToolsGuidePathForWrite(relativePath string) (string, string, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	target := strings.TrimSpace(relativePath)
	if target == "" {
		envPath, hasEnv, err := aiToolsGuidePathFromEnv()
		if err != nil {
			return "", "", err
		}
		if hasEnv {
			target = envPath
		} else {
			target = aiToolsGuideRelativePath
		}
	}
	normalized, ok := normalizeAIToolsGuideRelativePath(target)
	if !ok {
		return "", "", errors.New("invalid ai tools guide path")
	}
	return filepath.Join(repoRoot, filepath.FromSlash(normalized)), normalized, nil
}

func aiToolsGuidePathCandidates() ([]string, error) {
	candidates := []string{}
	if envPath, hasEnv, err := aiToolsGuidePathFromEnv(); err != nil {
		return nil, err
	} else if hasEnv {
		candidates = append(candidates, envPath)
	}
	candidates = append(
		candidates,
		aiToolsGuideRelativePath,
		aiToolsGuideLegacyRelativePath,
		aiToolsGuideLegacyV0RelativePath,
		aiToolsGuideLegacyV1RelativePath,
		aiToolsGuideLegacyV2RelativePath,
	)

	seen := map[string]struct{}{}
	unique := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	return unique, nil
}

func aiToolsGuidePathFromEnv() (string, bool, error) {
	raw := strings.TrimSpace(os.Getenv(aiToolsGuidePathEnv))
	if raw == "" {
		return "", false, nil
	}
	normalized, ok := normalizeAIToolsGuideRelativePath(raw)
	if !ok {
		return "", false, fmt.Errorf("%s must be a relative path without traversal", aiToolsGuidePathEnv)
	}
	return normalized, true, nil
}

func normalizeAIToolsGuideRelativePath(raw string) (string, bool) {
	return systempromptservice.NormalizeRelativePath(raw)
}

func findRepoRoot() (string, error) {
	return systempromptservice.FindWorkspaceRoot()
}

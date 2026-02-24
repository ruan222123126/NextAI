package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	selfopsservice "nextai/apps/gateway/internal/service/selfops"
)

func (s *Server) bootstrapSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID      string `json:"user_id"`
		Channel     string `json:"channel"`
		SessionSeed string `json:"session_seed"`
		FirstInput  string `json:"first_input"`
		PromptMode  string `json:"prompt_mode"`
		Stream      *bool  `json:"stream"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	stream := false
	if body.Stream != nil {
		stream = *body.Stream
	}

	result, processErr, err := s.getSelfOpsService().BootstrapSession(r.Context(), selfopsservice.BootstrapSessionInput{
		UserID:      body.UserID,
		Channel:     body.Channel,
		SessionSeed: body.SessionSeed,
		FirstInput:  body.FirstInput,
		PromptMode:  body.PromptMode,
		Stream:      stream,
	})
	if processErr != nil {
		writeErr(w, processErr.Status, processErr.Code, processErr.Message, processErr.Details)
		return
	}
	if err != nil {
		if writeSelfOpsError(w, err) {
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) setSessionModel(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	var body struct {
		UserID     string `json:"user_id"`
		Channel    string `json:"channel"`
		ProviderID string `json:"provider_id"`
		Model      string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}

	out, err := s.getSelfOpsService().SetSessionModel(selfopsservice.SetSessionModelInput{
		SessionID:  sessionID,
		UserID:     body.UserID,
		Channel:    body.Channel,
		ProviderID: body.ProviderID,
		Model:      body.Model,
	})
	if err != nil {
		if writeSelfOpsError(w, err) {
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) previewMutation(w http.ResponseWriter, r *http.Request) {
	var body selfopsservice.PreviewMutationInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	out, err := s.getSelfOpsService().PreviewMutation(body)
	if err != nil {
		if writeSelfOpsError(w, err) {
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) applyMutation(w http.ResponseWriter, r *http.Request) {
	var body selfopsservice.ApplyMutationInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	out, err := s.getSelfOpsService().ApplyMutation(body)
	if err != nil {
		if writeSelfOpsError(w, err) {
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeSelfOpsError(w http.ResponseWriter, err error) bool {
	serviceErr := (*selfopsservice.ServiceError)(nil)
	if !errors.As(err, &serviceErr) {
		return false
	}
	status := http.StatusBadRequest
	switch serviceErr.Code {
	case "session_not_found":
		status = http.StatusNotFound
	case "session_model_invalid":
		status = http.StatusBadRequest
	case "mutation_not_found":
		status = http.StatusNotFound
	case "mutation_expired":
		status = http.StatusGone
	case "mutation_hash_mismatch":
		status = http.StatusConflict
	case "mutation_sensitive_denied":
		status = http.StatusForbidden
	case "mutation_path_denied":
		status = http.StatusForbidden
	case "mutation_apply_conflict":
		status = http.StatusConflict
	case "invalid_request":
		status = http.StatusBadRequest
	default:
		status = http.StatusInternalServerError
	}
	writeErr(w, status, serviceErr.Code, serviceErr.Message, serviceErr.Details)
	return true
}

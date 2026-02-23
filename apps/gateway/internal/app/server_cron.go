package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nextai/apps/gateway/internal/domain"
	cronservice "nextai/apps/gateway/internal/service/cron"
)

func (s *Server) listCronJobs(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getCronService().ListJobs()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createCronJob(w http.ResponseWriter, r *http.Request) {
	var req domain.CronJobSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	job, err := s.getCronService().CreateJob(req)
	if err != nil {
		if validation := (*cronservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) getCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	view, err := s.getCronService().GetJob(id)
	if err != nil {
		if errors.Is(err, errCronJobNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) updateCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	var req domain.CronJobSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID != id {
		writeErr(w, http.StatusBadRequest, "job_id_mismatch", "job_id mismatch", nil)
		return
	}
	job, err := s.getCronService().UpdateJob(id, req)
	if err != nil {
		if validation := (*cronservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		if errors.Is(err, errCronJobNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) deleteCronJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "job_id"))
	deleted, err := s.getCronService().DeleteJob(id)
	if err != nil {
		if errors.Is(err, errCronDefaultProtected) {
			writeErr(w, http.StatusBadRequest, "default_cron_protected", "default cron job cannot be deleted", map[string]string{"job_id": domain.DefaultCronJobID})
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) pauseCronJob(w http.ResponseWriter, r *http.Request) {
	s.updateCronStatus(w, chi.URLParam(r, "job_id"), cronStatusPaused)
}

func (s *Server) resumeCronJob(w http.ResponseWriter, r *http.Request) {
	s.updateCronStatus(w, chi.URLParam(r, "job_id"), cronStatusResumed)
}

func (s *Server) runCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	if err := s.executeCronJob(id); err != nil {
		if errors.Is(err, errCronJobNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		if errors.Is(err, errCronMaxConcurrencyReached) {
			writeErr(w, http.StatusConflict, "cron_busy", "cron job reached max_concurrency", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"started": true})
}

func (s *Server) getCronJobState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	state, err := s.getCronService().GetState(id)
	if err != nil {
		if errors.Is(err, errCronJobNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) updateCronStatus(w http.ResponseWriter, id, status string) {
	if err := s.getCronService().UpdateStatus(id, status); err != nil {
		if errors.Is(err, errCronJobNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	key := "paused"
	if status == cronStatusResumed {
		key = "resumed"
	}
	writeJSON(w, http.StatusOK, map[string]bool{key: true})
}

func (s *Server) executeCronJob(id string) error {
	return s.getCronService().ExecuteJob(id)
}

func resolveCronNextRunAt(job domain.CronJobSpec, current *string, now time.Time) (time.Time, *time.Time, error) {
	return cronservice.ResolveNextRunAt(job, current, now)
}

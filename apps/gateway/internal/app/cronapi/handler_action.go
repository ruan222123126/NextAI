package cronapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

const (
	statusPaused  = "paused"
	statusResumed = "resumed"
)

func (h *Handler) PauseCronJob(w http.ResponseWriter, r *http.Request) {
	h.updateCronStatus(w, chi.URLParam(r, "job_id"), statusPaused)
}

func (h *Handler) ResumeCronJob(w http.ResponseWriter, r *http.Request) {
	h.updateCronStatus(w, chi.URLParam(r, "job_id"), statusResumed)
}

func (h *Handler) RunCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	if err := h.executeCronJob(id); err != nil {
		if errors.Is(err, h.errJobNotFound) {
			h.writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		if errors.Is(err, h.errMaxConcurrency) {
			h.writeErr(w, http.StatusConflict, "cron_busy", "cron job reached max_concurrency", nil)
			return
		}
		h.writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]bool{"started": true})
}

func (h *Handler) GetCronJobState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	state, err := h.getCronService().GetState(id)
	if err != nil {
		if errors.Is(err, h.errJobNotFound) {
			h.writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		h.writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	h.writeJSON(w, http.StatusOK, state)
}

func (h *Handler) updateCronStatus(w http.ResponseWriter, id, status string) {
	if err := h.getCronService().UpdateStatus(id, status); err != nil {
		if errors.Is(err, h.errJobNotFound) {
			h.writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		h.writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}

	key := "paused"
	if status == statusResumed {
		key = "resumed"
	}
	h.writeJSON(w, http.StatusOK, map[string]bool{key: true})
}

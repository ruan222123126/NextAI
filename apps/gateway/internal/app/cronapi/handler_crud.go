package cronapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"nextai/apps/gateway/internal/domain"
	cronservice "nextai/apps/gateway/internal/service/cron"
)

type CronService interface {
	ListJobs() ([]domain.CronJobSpec, error)
	CreateJob(domain.CronJobSpec) (domain.CronJobSpec, error)
	GetJob(string) (domain.CronJobView, error)
	UpdateJob(string, domain.CronJobSpec) (domain.CronJobSpec, error)
	DeleteJob(string) (bool, error)
	UpdateStatus(string, string) error
	GetState(string) (domain.CronJobState, error)
}

type HandlerDependencies struct {
	GetCronService func() CronService
	ExecuteCronJob func(string) error
	WriteJSON      func(http.ResponseWriter, int, interface{})
	WriteErr       func(http.ResponseWriter, int, string, string, interface{})

	ErrJobNotFound          error
	ErrMaxConcurrency       error
	ErrDefaultCronProtected error
}

type Handler struct {
	getCronService          func() CronService
	executeCronJob          func(string) error
	writeJSON               func(http.ResponseWriter, int, interface{})
	writeErr                func(http.ResponseWriter, int, string, string, interface{})
	errJobNotFound          error
	errMaxConcurrency       error
	errDefaultCronProtected error
}

func NewHandler(deps HandlerDependencies) *Handler {
	h := &Handler{
		getCronService:          deps.GetCronService,
		executeCronJob:          deps.ExecuteCronJob,
		writeJSON:               deps.WriteJSON,
		writeErr:                deps.WriteErr,
		errJobNotFound:          deps.ErrJobNotFound,
		errMaxConcurrency:       deps.ErrMaxConcurrency,
		errDefaultCronProtected: deps.ErrDefaultCronProtected,
	}
	if h.errJobNotFound == nil {
		h.errJobNotFound = cronservice.ErrJobNotFound
	}
	if h.errMaxConcurrency == nil {
		h.errMaxConcurrency = cronservice.ErrMaxConcurrencyReached
	}
	if h.errDefaultCronProtected == nil {
		h.errDefaultCronProtected = cronservice.ErrDefaultProtected
	}
	return h
}

func (h *Handler) ListCronJobs(w http.ResponseWriter, _ *http.Request) {
	out, err := h.getCronService().ListJobs()
	if err != nil {
		h.writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	h.writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateCronJob(w http.ResponseWriter, r *http.Request) {
	var req domain.CronJobSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	job, err := h.getCronService().CreateJob(req)
	if err != nil {
		if validation := (*cronservice.ValidationError)(nil); errors.As(err, &validation) {
			h.writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		h.writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	h.writeJSON(w, http.StatusOK, job)
}

func (h *Handler) GetCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	view, err := h.getCronService().GetJob(id)
	if err != nil {
		if errors.Is(err, h.errJobNotFound) {
			h.writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		h.writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	h.writeJSON(w, http.StatusOK, view)
}

func (h *Handler) UpdateCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	var req domain.CronJobSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID != id {
		h.writeErr(w, http.StatusBadRequest, "job_id_mismatch", "job_id mismatch", nil)
		return
	}
	job, err := h.getCronService().UpdateJob(id, req)
	if err != nil {
		if validation := (*cronservice.ValidationError)(nil); errors.As(err, &validation) {
			h.writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		if errors.Is(err, h.errJobNotFound) {
			h.writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		h.writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	h.writeJSON(w, http.StatusOK, job)
}

func (h *Handler) DeleteCronJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "job_id"))
	deleted, err := h.getCronService().DeleteJob(id)
	if err != nil {
		if errors.Is(err, h.errDefaultCronProtected) {
			h.writeErr(
				w,
				http.StatusBadRequest,
				"default_cron_protected",
				"default cron job cannot be deleted",
				map[string]string{"job_id": domain.DefaultCronJobID},
			)
			return
		}
		h.writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

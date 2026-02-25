package cron

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/service/ports"
)

func (s *Service) listJobs() ([]domain.CronJobSpec, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	out := make([]domain.CronJobSpec, 0)
	s.deps.Store.ReadCron(func(state ports.CronAggregate) {
		for _, job := range state.Jobs {
			out = append(out, job)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Service) createJob(job domain.CronJobSpec) (domain.CronJobSpec, error) {
	if err := s.validateStore(); err != nil {
		return domain.CronJobSpec{}, err
	}
	if code, err := s.validateJobSpec(&job); err != nil {
		return domain.CronJobSpec{}, &ValidationError{Code: code, Message: err.Error()}
	}

	now := time.Now().UTC()
	if err := s.deps.Store.WriteCron(func(state *ports.CronAggregate) error {
		state.Jobs[job.ID] = job
		existing := state.States[job.ID]
		state.States[job.ID] = alignStateForMutation(job, normalizePausedState(existing), now)
		return nil
	}); err != nil {
		return domain.CronJobSpec{}, err
	}
	return job, nil
}

func (s *Service) getJob(jobID string) (domain.CronJobView, error) {
	if err := s.validateStore(); err != nil {
		return domain.CronJobView{}, err
	}

	found := false
	var spec domain.CronJobSpec
	var state domain.CronJobState
	s.deps.Store.ReadCron(func(st ports.CronAggregate) {
		spec, found = st.Jobs[jobID]
		if found {
			state = st.States[jobID]
		}
	})
	if !found {
		return domain.CronJobView{}, ErrJobNotFound
	}
	return domain.CronJobView{Spec: spec, State: state}, nil
}

func (s *Service) updateJob(jobID string, job domain.CronJobSpec) (domain.CronJobSpec, error) {
	if err := s.validateStore(); err != nil {
		return domain.CronJobSpec{}, err
	}
	if code, err := s.validateJobSpec(&job); err != nil {
		return domain.CronJobSpec{}, &ValidationError{Code: code, Message: err.Error()}
	}

	now := time.Now().UTC()
	if err := s.deps.Store.WriteCron(func(st *ports.CronAggregate) error {
		if _, ok := st.Jobs[jobID]; !ok {
			return ErrJobNotFound
		}
		st.Jobs[jobID] = job
		state := normalizePausedState(st.States[jobID])
		st.States[jobID] = alignStateForMutation(job, state, now)
		return nil
	}); err != nil {
		return domain.CronJobSpec{}, err
	}
	return job, nil
}

func (s *Service) deleteJob(jobID string) (bool, error) {
	if err := s.validateStore(); err != nil {
		return false, err
	}

	jobID = strings.TrimSpace(jobID)
	deleted := false
	if err := s.deps.Store.WriteCron(func(st *ports.CronAggregate) error {
		if _, ok := st.Jobs[jobID]; ok {
			if jobID == domain.DefaultCronJobID {
				return ErrDefaultProtected
			}
			delete(st.Jobs, jobID)
			delete(st.States, jobID)
			deleted = true
		}
		return nil
	}); err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *Service) updateStatus(jobID, status string) error {
	if err := s.validateStore(); err != nil {
		return err
	}

	now := time.Now().UTC()
	return s.deps.Store.WriteCron(func(st *ports.CronAggregate) error {
		job, ok := st.Jobs[jobID]
		if !ok {
			return ErrJobNotFound
		}
		state := normalizePausedState(st.States[jobID])
		switch status {
		case statusPaused:
			state.Paused = true
			state.NextRunAt = nil
		case statusResumed:
			state.Paused = false
			state = alignStateForMutation(job, state, now)
		}
		state.LastStatus = &status
		st.States[jobID] = state
		return nil
	})
}

func (s *Service) getState(jobID string) (domain.CronJobState, error) {
	if err := s.validateStore(); err != nil {
		return domain.CronJobState{}, err
	}

	found := false
	var state domain.CronJobState
	s.deps.Store.ReadCron(func(st ports.CronAggregate) {
		if _, ok := st.Jobs[jobID]; ok {
			found = true
			state = st.States[jobID]
		}
	})
	if !found {
		return domain.CronJobState{}, ErrJobNotFound
	}
	return state, nil
}

func (s *Service) validateJobSpec(job *domain.CronJobSpec) (string, error) {
	if job == nil {
		return "invalid_cron_task_type", errors.New("cron job is required")
	}
	job.ID = strings.TrimSpace(job.ID)
	job.Name = strings.TrimSpace(job.Name)
	if job.ID == "" || job.Name == "" {
		return "invalid_cron_task_type", errors.New("id and name are required")
	}

	switch taskType(*job) {
	case taskTypeText:
		text := strings.TrimSpace(job.Text)
		if text == "" {
			return "invalid_cron_task_type", errors.New("text is required for task_type=text")
		}
		job.TaskType = taskTypeText
		job.Text = text
		job.Workflow = nil
		return "", nil
	case taskTypeWorkflow:
		plan, err := s.buildWorkflowPlan(job.Workflow)
		if err != nil {
			return "invalid_cron_workflow", err
		}
		job.TaskType = taskTypeWorkflow
		job.Workflow = &plan.Workflow
		job.Text = ""
		return "", nil
	default:
		return "invalid_cron_task_type", fmt.Errorf("unsupported task_type=%q", strings.TrimSpace(job.TaskType))
	}
}

func taskType(job domain.CronJobSpec) string {
	t := strings.ToLower(strings.TrimSpace(job.TaskType))
	if t != "" {
		return t
	}
	if job.Workflow != nil {
		return taskTypeWorkflow
	}
	if strings.TrimSpace(job.Text) != "" {
		return taskTypeText
	}
	return t
}

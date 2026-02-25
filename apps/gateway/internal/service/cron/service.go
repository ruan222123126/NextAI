package cron

import (
	"context"
	"errors"
	"regexp"
	"time"

	"nextai/apps/gateway/internal/domain"
	workflowengine "nextai/apps/gateway/internal/service/cron/workflow"
	"nextai/apps/gateway/internal/service/ports"
)

const (
	statusPaused    = "paused"
	statusResumed   = "resumed"
	statusRunning   = "running"
	statusSucceeded = "succeeded"
	statusFailed    = "failed"

	taskTypeText     = "text"
	taskTypeWorkflow = "workflow"

	workflowVersionV1 = "v1"
	workflowNodeStart = "start"
	workflowNodeText  = "text_event"
	workflowNodeDelay = "delay"
	workflowNodeIf    = "if_event"

	workflowNodeExecutionSkipped = "skipped"

	cronLeaseDirName = "cron-leases"
	qqChannelName    = "qq"
)

var ErrJobNotFound = errors.New("cron_job_not_found")
var ErrMaxConcurrencyReached = errors.New("cron_max_concurrency_reached")
var ErrDefaultProtected = errors.New("cron_default_protected")

var workflowIfConditionPattern = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(==|!=)\s*(?:"([^"]*)"|'([^']*)'|(\S+))\s*$`)

var workflowIfAllowedFields = map[string]struct{}{
	"job_id":     {},
	"job_name":   {},
	"channel":    {},
	"user_id":    {},
	"session_id": {},
	"task_type":  {},
}

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type channelError struct {
	Message string
	Err     error
}

func (e *channelError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return ""
}

func (e *channelError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type TaskExecutor func(ctx context.Context, job domain.CronJobSpec) (handled bool, err error)

type Dependencies struct {
	Store                   ports.StateStore
	DataDir                 string
	ChannelResolver         ports.ChannelResolver
	ExecuteConsoleAgentTask func(ctx context.Context, job domain.CronJobSpec, text string) error
	ExecuteTask             TaskExecutor
}

type Service struct {
	deps         Dependencies
	nodeRegistry *workflowengine.NodeRegistry
}

func NewService(deps Dependencies) *Service {
	svc := &Service{
		deps:         deps,
		nodeRegistry: workflowengine.NewNodeRegistry(),
	}
	svc.registerDefaultWorkflowNodeHandlers()
	return svc
}

func (s *Service) ListJobs() ([]domain.CronJobSpec, error) {
	return s.listJobs()
}

func (s *Service) CreateJob(job domain.CronJobSpec) (domain.CronJobSpec, error) {
	return s.createJob(job)
}

func (s *Service) GetJob(jobID string) (domain.CronJobView, error) {
	return s.getJob(jobID)
}

func (s *Service) UpdateJob(jobID string, job domain.CronJobSpec) (domain.CronJobSpec, error) {
	return s.updateJob(jobID, job)
}

func (s *Service) DeleteJob(jobID string) (bool, error) {
	return s.deleteJob(jobID)
}

func (s *Service) UpdateStatus(jobID, status string) error {
	return s.updateStatus(jobID, status)
}

func (s *Service) GetState(jobID string) (domain.CronJobState, error) {
	return s.getState(jobID)
}

func (s *Service) SchedulerTick(now time.Time) ([]string, error) {
	return s.schedulerTick(now)
}

func (s *Service) ExecuteJob(jobID string) error {
	return s.executeJob(jobID)
}

func BuildBizParams(job domain.CronJobSpec) map[string]interface{} {
	return buildBizParams(job)
}

func ResolveNextRunAt(job domain.CronJobSpec, current *string, now time.Time) (time.Time, *time.Time, error) {
	return resolveNextRunAt(job, current, now)
}

func MisfireExceeded(dueAt *time.Time, runtime domain.CronRuntimeSpec, now time.Time) bool {
	return misfireExceeded(dueAt, runtime, now)
}

func (s *Service) validateStore() error {
	if s == nil || s.deps.Store == nil {
		return errors.New("cron service store is unavailable")
	}
	return nil
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

package cronapi

// CronJobIDPathRequest is the shared path payload for /cron/jobs/{job_id} endpoints.
type CronJobIDPathRequest struct {
	JobID string `json:"job_id" validate:"required,min=1"`
}

type CronScheduleSpec struct {
	Type     string `json:"type" validate:"omitempty,oneof=interval cron"`
	Cron     string `json:"cron" validate:"required,min=1"`
	Timezone string `json:"timezone" validate:"omitempty"`
}

type CronDispatchTarget struct {
	UserID    string `json:"user_id" validate:"required,min=1"`
	SessionID string `json:"session_id" validate:"required,min=1"`
}

type CronDispatchSpec struct {
	Type    string                 `json:"type" validate:"omitempty"`
	Channel string                 `json:"channel" validate:"omitempty"`
	Target  CronDispatchTarget     `json:"target" validate:"required"`
	Mode    string                 `json:"mode" validate:"omitempty"`
	Meta    map[string]interface{} `json:"meta,omitempty" validate:"omitempty"`
}

type CronRuntimeSpec struct {
	MaxConcurrency      int `json:"max_concurrency" validate:"omitempty,min=1"`
	TimeoutSeconds      int `json:"timeout_seconds" validate:"omitempty,min=1"`
	MisfireGraceSeconds int `json:"misfire_grace_seconds" validate:"omitempty,min=0"`
}

type CronWorkflowSpec struct {
	Version  string                `json:"version" validate:"required,oneof=v1"`
	Viewport *CronWorkflowViewport `json:"viewport,omitempty" validate:"omitempty"`
	Nodes    []CronWorkflowNode    `json:"nodes" validate:"required,min=2,dive"`
	Edges    []CronWorkflowEdge    `json:"edges" validate:"required,min=1,dive"`
}

type CronWorkflowNode struct {
	ID              string  `json:"id" validate:"required,min=1"`
	Type            string  `json:"type" validate:"required,oneof=start text_event delay if_event"`
	Title           string  `json:"title,omitempty" validate:"omitempty"`
	X               float64 `json:"x" validate:"-"`
	Y               float64 `json:"y" validate:"-"`
	Text            string  `json:"text,omitempty" validate:"omitempty"`
	DelaySeconds    int     `json:"delay_seconds,omitempty" validate:"omitempty,min=0"`
	IfCondition     string  `json:"if_condition,omitempty" validate:"omitempty"`
	ContinueOnError bool    `json:"continue_on_error,omitempty" validate:"-"`
}

type CronWorkflowEdge struct {
	ID     string `json:"id" validate:"required,min=1"`
	Source string `json:"source" validate:"required,min=1"`
	Target string `json:"target" validate:"required,min=1"`
}

type CronWorkflowViewport struct {
	PanX float64 `json:"pan_x,omitempty" validate:"-"`
	PanY float64 `json:"pan_y,omitempty" validate:"-"`
	Zoom float64 `json:"zoom,omitempty" validate:"-"`
}

type CronWorkflowExecution struct {
	RunID       string                      `json:"run_id" validate:"required,min=1"`
	StartedAt   string                      `json:"started_at" validate:"required,min=1"`
	FinishedAt  *string                     `json:"finished_at,omitempty" validate:"omitempty,min=1"`
	HadFailures bool                        `json:"had_failures" validate:"-"`
	Nodes       []CronWorkflowNodeExecution `json:"nodes" validate:"required,dive"`
}

type CronWorkflowNodeExecution struct {
	NodeID          string  `json:"node_id" validate:"required,min=1"`
	NodeType        string  `json:"node_type" validate:"required,oneof=text_event delay if_event"`
	Status          string  `json:"status" validate:"required,oneof=succeeded failed skipped"`
	ContinueOnError bool    `json:"continue_on_error" validate:"-"`
	StartedAt       string  `json:"started_at" validate:"required,min=1"`
	FinishedAt      *string `json:"finished_at,omitempty" validate:"omitempty,min=1"`
	Error           *string `json:"error,omitempty" validate:"omitempty,min=1"`
}

type CronJobState struct {
	NextRunAt     *string                `json:"next_run_at,omitempty" validate:"omitempty,min=1"`
	LastRunAt     *string                `json:"last_run_at,omitempty" validate:"omitempty,min=1"`
	LastStatus    *string                `json:"last_status,omitempty" validate:"omitempty,oneof=paused resumed running succeeded failed"`
	LastError     *string                `json:"last_error,omitempty" validate:"omitempty"`
	Paused        bool                   `json:"paused,omitempty" validate:"-"`
	LastExecution *CronWorkflowExecution `json:"last_execution,omitempty" validate:"omitempty"`
}

type CronJob struct {
	ID       string                 `json:"id" validate:"required,min=1"`
	Name     string                 `json:"name" validate:"required,min=1"`
	Enabled  bool                   `json:"enabled" validate:"-"`
	Schedule CronScheduleSpec       `json:"schedule" validate:"required"`
	TaskType string                 `json:"task_type" validate:"required,oneof=text workflow"`
	Text     string                 `json:"text,omitempty" validate:"omitempty"`
	Workflow *CronWorkflowSpec      `json:"workflow,omitempty" validate:"omitempty"`
	Request  map[string]interface{} `json:"request,omitempty" validate:"omitempty"`
	Dispatch CronDispatchSpec       `json:"dispatch" validate:"required"`
	Runtime  CronRuntimeSpec        `json:"runtime" validate:"required"`
	Meta     map[string]interface{} `json:"meta,omitempty" validate:"omitempty"`
}

type CronJobView struct {
	Spec  CronJob      `json:"spec" validate:"required"`
	State CronJobState `json:"state" validate:"required"`
}

type CronDeleteJobResponse struct {
	Deleted bool `json:"deleted" validate:"-"`
}

type CronPauseJobResponse struct {
	Paused bool `json:"paused" validate:"-"`
}

type CronResumeJobResponse struct {
	Resumed bool `json:"resumed" validate:"-"`
}

type CronRunJobResponse struct {
	Started bool `json:"started" validate:"-"`
}

type ListCronJobsRequest struct{}
type ListCronJobsResponse []CronJob

type CreateCronJobRequest = CronJob
type CreateCronJobResponse = CronJob

type GetCronJobRequest = CronJobIDPathRequest
type GetCronJobResponse = CronJobView

type UpdateCronJobRequest = CronJob
type UpdateCronJobResponse = CronJob

type DeleteCronJobRequest = CronJobIDPathRequest

type PauseCronJobRequest = CronJobIDPathRequest

type ResumeCronJobRequest = CronJobIDPathRequest

type RunCronJobRequest = CronJobIDPathRequest

type GetCronJobStateRequest = CronJobIDPathRequest
type GetCronJobStateResponse = CronJobState

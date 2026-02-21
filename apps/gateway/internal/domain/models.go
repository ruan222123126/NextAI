package domain

const (
	DefaultChatID         = "chat-default"
	DefaultChatName       = "Default Chat"
	DefaultChatSessionID  = "session-default"
	DefaultChatUserID     = "demo-user"
	DefaultChatChannel    = "console"
	ChatMetaSystemDefault = "system_default"

	DefaultCronJobID       = "cron-default"
	DefaultCronJobName     = "\u4f60\u597d\u6587\u672c\u4efb\u52a1"
	DefaultCronJobText     = "\u4f60\u597d"
	DefaultCronJobInterval = "60s"
	CronMetaSystemDefault  = "system_default"
)
type APIErrorBody struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

type ChatSpec struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	Channel   string                 `json:"channel"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
	Meta      map[string]interface{} `json:"meta"`
}

type RuntimeContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type RuntimeMessage struct {
	ID       string                 `json:"id,omitempty"`
	Role     string                 `json:"role,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Content  []RuntimeContent       `json:"content,omitempty"`
}

type ChatHistory struct {
	Messages []RuntimeMessage `json:"messages"`
}

type AgentInputMessage struct {
	Role     string                 `json:"role"`
	Type     string                 `json:"type"`
	Content  []RuntimeContent       `json:"content"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type AgentProcessRequest struct {
	Input     []AgentInputMessage    `json:"input"`
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	Channel   string                 `json:"channel"`
	Stream    bool                   `json:"stream"`
	BizParams map[string]interface{} `json:"biz_params,omitempty"`
}

type AgentToolCallPayload struct {
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input,omitempty"`
}

type AgentToolResultPayload struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Summary string `json:"summary,omitempty"`
}

type AgentEvent struct {
	Type       string                  `json:"type"`
	Step       int                     `json:"step,omitempty"`
	Delta      string                  `json:"delta,omitempty"`
	Reply      string                  `json:"reply,omitempty"`
	ToolCall   *AgentToolCallPayload   `json:"tool_call,omitempty"`
	ToolResult *AgentToolResultPayload `json:"tool_result,omitempty"`
	Meta       map[string]interface{}  `json:"meta,omitempty"`
}

type AgentProcessResponse struct {
	Reply  string       `json:"reply"`
	Events []AgentEvent `json:"events,omitempty"`
}

type CronScheduleSpec struct {
	Type     string `json:"type"`
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}

type CronDispatchTarget struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
}

type CronDispatchSpec struct {
	Type    string                 `json:"type"`
	Channel string                 `json:"channel"`
	Target  CronDispatchTarget     `json:"target"`
	Mode    string                 `json:"mode"`
	Meta    map[string]interface{} `json:"meta"`
}

type CronRuntimeSpec struct {
	MaxConcurrency      int `json:"max_concurrency"`
	TimeoutSeconds      int `json:"timeout_seconds"`
	MisfireGraceSeconds int `json:"misfire_grace_seconds"`
}

type CronWorkflowSpec struct {
	Version  string                `json:"version"`
	Viewport *CronWorkflowViewport `json:"viewport,omitempty"`
	Nodes    []CronWorkflowNode    `json:"nodes"`
	Edges    []CronWorkflowEdge    `json:"edges"`
}

type CronWorkflowNode struct {
	ID              string  `json:"id"`
	Type            string  `json:"type"`
	Title           string  `json:"title,omitempty"`
	X               float64 `json:"x"`
	Y               float64 `json:"y"`
	Text            string  `json:"text,omitempty"`
	DelaySeconds    int     `json:"delay_seconds,omitempty"`
	IfCondition     string  `json:"if_condition,omitempty"`
	ContinueOnError bool    `json:"continue_on_error,omitempty"`
}

type CronWorkflowEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type CronWorkflowViewport struct {
	PanX float64 `json:"pan_x,omitempty"`
	PanY float64 `json:"pan_y,omitempty"`
	Zoom float64 `json:"zoom,omitempty"`
}

type CronWorkflowExecution struct {
	RunID       string                      `json:"run_id"`
	StartedAt   string                      `json:"started_at"`
	FinishedAt  *string                     `json:"finished_at,omitempty"`
	HadFailures bool                        `json:"had_failures"`
	Nodes       []CronWorkflowNodeExecution `json:"nodes"`
}

type CronWorkflowNodeExecution struct {
	NodeID          string  `json:"node_id"`
	NodeType        string  `json:"node_type"`
	Status          string  `json:"status"`
	ContinueOnError bool    `json:"continue_on_error"`
	StartedAt       string  `json:"started_at"`
	FinishedAt      *string `json:"finished_at,omitempty"`
	Error           *string `json:"error,omitempty"`
}

type CronJobSpec struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Enabled  bool                   `json:"enabled"`
	Schedule CronScheduleSpec       `json:"schedule"`
	TaskType string                 `json:"task_type"`
	Text     string                 `json:"text,omitempty"`
	Workflow *CronWorkflowSpec      `json:"workflow,omitempty"`
	Request  map[string]interface{} `json:"request,omitempty"`
	Dispatch CronDispatchSpec       `json:"dispatch"`
	Runtime  CronRuntimeSpec        `json:"runtime"`
	Meta     map[string]interface{} `json:"meta"`
}

type CronJobState struct {
	NextRunAt     *string                `json:"next_run_at,omitempty"`
	LastRunAt     *string                `json:"last_run_at,omitempty"`
	LastStatus    *string                `json:"last_status,omitempty"`
	LastError     *string                `json:"last_error,omitempty"`
	Paused        bool                   `json:"paused,omitempty"`
	LastExecution *CronWorkflowExecution `json:"last_execution,omitempty"`
}

type CronJobView struct {
	Spec  CronJobSpec  `json:"spec"`
	State CronJobState `json:"state"`
}

type ModelInfo struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Status       string             `json:"status,omitempty"`
	AliasOf      string             `json:"alias_of,omitempty"`
	Capabilities *ModelCapabilities `json:"capabilities,omitempty"`
	Limit        *ModelLimit        `json:"limit,omitempty"`
}

type ModelModalities struct {
	Text  bool `json:"text"`
	Audio bool `json:"audio"`
	Image bool `json:"image"`
	Video bool `json:"video"`
	PDF   bool `json:"pdf"`
}

type ModelCapabilities struct {
	Temperature bool             `json:"temperature"`
	Reasoning   bool             `json:"reasoning"`
	Attachment  bool             `json:"attachment"`
	ToolCall    bool             `json:"tool_call"`
	Input       *ModelModalities `json:"input,omitempty"`
	Output      *ModelModalities `json:"output,omitempty"`
}

type ModelLimit struct {
	Context int `json:"context,omitempty"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output,omitempty"`
}

type ProviderInfo struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	DisplayName        string            `json:"display_name"`
	OpenAICompatible   bool              `json:"openai_compatible"`
	APIKeyPrefix       string            `json:"api_key_prefix"`
	Models             []ModelInfo       `json:"models"`
	Headers            map[string]string `json:"headers,omitempty"`
	TimeoutMS          int               `json:"timeout_ms,omitempty"`
	ModelAliases       map[string]string `json:"model_aliases,omitempty"`
	AllowCustomBaseURL bool              `json:"allow_custom_base_url"`
	Enabled            bool              `json:"enabled"`
	HasAPIKey          bool              `json:"has_api_key"`
	CurrentAPIKey      string            `json:"current_api_key"`
	CurrentBaseURL     string            `json:"current_base_url"`
}

type ProviderTypeInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type ModelSlotConfig struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

type ActiveModelsInfo struct {
	ActiveLLM ModelSlotConfig `json:"active_llm"`
}

type ModelCatalogInfo struct {
	Providers     []ProviderInfo     `json:"providers"`
	Defaults      map[string]string  `json:"defaults"`
	ActiveLLM     ModelSlotConfig    `json:"active_llm"`
	ProviderTypes []ProviderTypeInfo `json:"provider_types"`
}

type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type SkillSpec struct {
	Name       string                 `json:"name"`
	Content    string                 `json:"content"`
	Source     string                 `json:"source"`
	Path       string                 `json:"path"`
	References map[string]interface{} `json:"references"`
	Scripts    map[string]interface{} `json:"scripts"`
	Enabled    bool                   `json:"enabled"`
}

type ChannelConfigMap map[string]map[string]interface{}

# API / CLI 最小契约

## API
- `/version`, `/healthz`
- `/runtime-config`
- `/chats`, `/chats/{chat_id}`, `/chats/batch-delete`
- `/agent/process`
- `/agent/system-layers`
- `/agent/self/sessions/bootstrap`
- `/agent/self/sessions/{session_id}/model`
- `/agent/self/config-mutations/preview`
- `/agent/self/config-mutations/apply`
- `/channels/qq/inbound`
- `/channels/qq/state`
- `/cron/jobs` 系列
- `/models` 系列
- `/envs` 系列
- `/skills` 系列
- `/workspace/files`, `/workspace/files/{file_path}`
- `/workspace/uploads`, `/workspace/export`, `/workspace/import`
- `/config/channels` 系列

### SelfOps 契约（`/agent/self/*`）
- `POST /agent/self/sessions/bootstrap`
  - 单次原子入口：创建会话 + 写首条 user 输入 + 触发一次 process。
  - `channel` 默认 `console`，`stream` 默认 `false`。
  - 返回 `chat + reply + events + applied_model`。
- `PUT /agent/self/sessions/{session_id}/model`
  - 仅设置目标会话模型覆盖（`chat.meta.active_llm_override`），不影响全局 `/models/active`。
  - 会校验 provider 存在、启用、model/alias 可解析。
- 两阶段配置变更：
  - `POST /agent/self/config-mutations/preview`
  - `POST /agent/self/config-mutations/apply`

#### preview -> apply 时序
1. 调 `preview` 生成一次性 `mutation_id` 与 `confirm_hash`（默认 TTL 5 分钟）。
2. `preview` 返回 `checks + diff_summary + unified_diff + requires_sensitive_allow`。
3. 调 `apply` 必须带回同一 `mutation_id + confirm_hash`，且 `allow_sensitive` 必须与 preview 一致。
4. 命中敏感字段且未显式放行时，`apply` 拒绝。

#### 路径白名单（workspace_file target）
- `prompts/**`
- `prompt/**`
- `docs/AI/**`
- `config/models.json`
- `config/active-llm.json`

#### 敏感字段判定
- `api_key`
- 环境变量形态后缀：`*_KEY` / `*_TOKEN` / `*_SECRET`

#### SelfOps 错误码
- `session_not_found`
- `session_model_invalid`
- `mutation_not_found`
- `mutation_expired`
- `mutation_hash_mismatch`
- `mutation_sensitive_denied`
- `mutation_path_denied`
- `mutation_apply_conflict`

### 渠道配置契约（`/config/channels`）
- 支持类型：`console`、`webhook`、`qq`
- `qq` 推荐字段：`enabled`、`app_id`、`client_secret`、`bot_prefix`、`target_type(c2c/group/guild)`、`target_id`、`api_base`、`token_url`、`timeout_seconds`

### QQ 入站契约（`/channels/qq/inbound`）
- 接收 QQ 入站事件（支持 `C2C_MESSAGE_CREATE`、`GROUP_AT_MESSAGE_CREATE`、`AT_MESSAGE_CREATE`、`DIRECT_MESSAGE_CREATE`，并兼容 `message_type` 结构）。
- 网关会将入站文本转换为内部 `channel=qq` 的 `/agent/process` 请求并自动回发。
- 回发目标按事件动态覆盖 `target_type/target_id`，无需写死在全局配置。

## CLI
- `nextai app start`
- `nextai chats list/create/get/delete/send`
- `nextai cron list/create/update/delete/pause/resume/run/state`
- `nextai models list/config/active-get/active-set`
- `nextai env list/set/delete`
- `nextai skills list/create/enable/disable/delete`
- `nextai workspace ls/cat/put/rm/export/import`
- `nextai channels list/types/get/set`
- `nextai tui`

## 扩展点矩阵（Provider / Channel / Tool / Prompt Source / Cron Node）

### 矩阵总览

| 扩展点类型 | 核心职责 | 注册入口 | 标准输入 | 标准输出 | 失败语义 |
| --- | --- | --- | --- | --- | --- |
| Model Provider | 对接模型 API，返回文本与工具调用 | `apps/gateway/internal/runner/runner.go` | `domain.AgentProcessRequest + runner.GenerateConfig + []runner.ToolDefinition` | `runner.TurnResult` | `runner.RunnerError{code=provider_*}` |
| Channel | 把最终文本分发到外部渠道 | `apps/gateway/internal/app/server.go` | `ctx + user_id + session_id + text + channel_cfg` | `error` | `channel_not_supported/channel_disabled/channel_dispatch_failed` |
| Tool | 执行本地工具调用 | `apps/gateway/internal/app/server.go` | `plugin.ToolCommand` | `plugin.ToolResult` | `tool_not_supported/tool_invoke_failed/tool_invalid_result` |
| Prompt Source | 解析系统层提示词来源（文件/目录/catalog） | `apps/gateway/internal/service/systemprompt` | `prompt_mode + session_id + runtime env` | `[]systemprompt.Layer` | `*_prompt_unavailable` |
| Cron Node | 执行 workflow 节点（`text_event/delay/if_event/...`） | `apps/gateway/internal/service/cron/service.go` | `ctx + CronJobSpec + CronWorkflowNode` | `CronNodeResult` | `unsupported workflow node type`/节点执行错误 |

### 1) Model Provider

标准接口（Runner 适配器契约）：

```go
package runner

import (
	"context"

	"nextai/apps/gateway/internal/domain"
)

type ProviderAdapter interface {
	ID() string
	GenerateTurn(
		ctx context.Context,
		req domain.AgentProcessRequest,
		cfg GenerateConfig,
		tools []ToolDefinition,
		runner *Runner,
	) (TurnResult, error)
}

type StreamProviderAdapter interface {
	ProviderAdapter
	GenerateTurnStream(
		ctx context.Context,
		req domain.AgentProcessRequest,
		cfg GenerateConfig,
		tools []ToolDefinition,
		runner *Runner,
		onDelta func(string),
	) (TurnResult, error)
}
```

最小实现模板：

```go
package runner

import (
	"context"
	"strings"

	"nextai/apps/gateway/internal/domain"
)

type acmeCompatibleAdapter struct{}

func (a *acmeCompatibleAdapter) ID() string {
	return "acme-compatible"
}

func (a *acmeCompatibleAdapter) GenerateTurn(
	_ context.Context,
	req domain.AgentProcessRequest,
	_ GenerateConfig,
	_ []ToolDefinition,
	_ *Runner,
) (TurnResult, error) {
	text := "acme: ok"
	if len(req.Input) > 0 {
		last := req.Input[len(req.Input)-1]
		if len(last.Content) > 0 {
			text = "acme: " + strings.TrimSpace(last.Content[0].Text)
		}
	}
	return TurnResult{Text: text}, nil
}

func (a *acmeCompatibleAdapter) GenerateTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	r *Runner,
	onDelta func(string),
) (TurnResult, error) {
	turn, err := a.GenerateTurn(ctx, req, cfg, tools, r)
	if err != nil {
		return TurnResult{}, err
	}
	if onDelta != nil && strings.TrimSpace(turn.Text) != "" {
		onDelta(turn.Text)
	}
	return turn, nil
}
```

注册示例（在 `runner.NewWithHTTPClient` 同包内）：

```go
r.registerAdapter(&acmeCompatibleAdapter{})
```

### 2) Channel

标准接口：

```go
package plugin

import "context"

type ChannelPlugin interface {
	Name() string
	SendText(ctx context.Context, userID, sessionID, text string, cfg map[string]interface{}) error
}
```

最小实现模板：

```go
package channel

import "context"

type DingTalkChannel struct{}

func NewDingTalkChannel() *DingTalkChannel { return &DingTalkChannel{} }

func (c *DingTalkChannel) Name() string { return "dingtalk" }

func (c *DingTalkChannel) SendText(
	_ context.Context,
	userID string,
	sessionID string,
	text string,
	cfg map[string]interface{},
) error {
	_ = userID
	_ = sessionID
	_ = text
	_ = cfg
	return nil
}
```

注册示例（在 `NewServer`）：

```go
srv.registerChannelPlugin(channel.NewDingTalkChannel())
```

### 3) Tool

标准接口：

```go
package plugin

type ToolPlugin interface {
	Name() string
	Invoke(command ToolCommand) (ToolResult, error)
}
```

最小实现模板：

```go
package plugin

type EchoTool struct{}

func NewEchoTool() *EchoTool { return &EchoTool{} }

func (t *EchoTool) Name() string { return "echo" }

func (t *EchoTool) Invoke(command ToolCommand) (ToolResult, error) {
	return NewToolResult(map[string]interface{}{
		"ok":   true,
		"text": "echo tool ok",
		"raw":  command,
	}), nil
}
```

注册示例（在 `NewServer`）：

```go
srv.registerToolPlugin(plugin.NewEchoTool())
```

### 4) Prompt Source

标准接口（建议抽象；与 `systemprompt.Layer` 对齐）：

```go
package systemprompt

import "context"

type BuildRequest struct {
	PromptMode  string
	TaskCommand string
	SessionID   string
}

type Source interface {
	Name() string
	Build(ctx context.Context, req BuildRequest) ([]Layer, error)
}
```

最小实现模板（文件源）：

```go
package systemprompt

import "context"

type FileSource struct {
	LoadRequiredLayer func(candidatePaths []string) (string, string, error)
}

func (s *FileSource) Name() string { return "file" }

func (s *FileSource) Build(_ context.Context, _ BuildRequest) ([]Layer, error) {
	basePath, baseContent, err := s.LoadRequiredLayer([]string{"prompts/AGENTS.md"})
	if err != nil {
		return nil, err
	}
	return []Layer{
		{
			Name:    "base_system",
			Role:    "system",
			Source:  basePath,
			Content: FormatLayerSourceContent(basePath, baseContent),
		},
	}, nil
}
```

接入建议：
- source 内部失败返回 error；上层统一映射到 `{ error: { code, message, details } }`。

### 5) Cron Node

标准接口（建议抽象；与 `executeWorkflowNode` 现状对齐）：

```go
package cron

import (
	"context"

	"nextai/apps/gateway/internal/domain"
)

type CronNodeResult struct {
	Stop bool
}

type CronNodeHandler interface {
	Type() string
	Validate(node domain.CronWorkflowNode) error
	Execute(ctx context.Context, job domain.CronJobSpec, node domain.CronWorkflowNode) (CronNodeResult, error)
}
```

最小实现模板（`text_event`）：

```go
package cron

import (
	"context"
	"errors"
	"strings"

	"nextai/apps/gateway/internal/domain"
)

type TextNodeHandler struct {
	ExecuteTextTask func(ctx context.Context, job domain.CronJobSpec, text string) error
}

func (h *TextNodeHandler) Type() string { return "text_event" }

func (h *TextNodeHandler) Validate(node domain.CronWorkflowNode) error {
	if strings.TrimSpace(node.Text) == "" {
		return errors.New("workflow text_event requires non-empty text")
	}
	return nil
}

func (h *TextNodeHandler) Execute(
	ctx context.Context,
	job domain.CronJobSpec,
	node domain.CronWorkflowNode,
) (CronNodeResult, error) {
	if err := h.Validate(node); err != nil {
		return CronNodeResult{}, err
	}
	if err := h.ExecuteTextTask(ctx, job, node.Text); err != nil {
		return CronNodeResult{}, err
	}
	return CronNodeResult{}, nil
}
```

接入建议：
- 保持 `start` 节点仅用于拓扑起点，不执行 side effect。
- 新节点默认要求 `Validate` 可离线运行，避免运行期才炸。

### 扩展点统一约束
- 错误模型统一：外部接口始终映射为 `{ error: { code, message, details } }`。
- 全部扩展点必须接收 `context.Context` 并遵守超时/取消。
- 输出数据必须是可 JSON 序列化结构，避免 `tool_invalid_result`。
- 注册键（`Name()` / `ID()` / `Type()`）统一小写、稳定不可变，避免状态迁移与历史数据失配。

## `/agent/process` 多步 Agent 协议

`POST /agent/process` 支持两种模式：
1. 常规对话（模型自治多步）
2. 显式工具调用（推荐顶层 `view/edit/shell/browser/search/find`，兼容 `biz_params.tool`；上述工具值均为对象数组，单次操作也需传 1 个元素）

特殊指令约定：
- 当用户文本输入为 `/new`（忽略前后空白）时，Gateway 不调用模型，直接清理当前 `session_id + user_id + channel` 对应会话历史，并返回确认回复（流式/非流式均适用）。
- `channel` 字段在 `/agent/process` 中为可选；请求未显式传值时默认 `console`。QQ 入站路径固定使用 `channel=qq`。

工具启用策略：
- 默认注册工具可用。
- 通过环境变量 `NEXTAI_DISABLED_TOOLS`（逗号分隔，如 `shell,edit`）按名称禁用工具。
- 调用被禁用工具时，返回 `403` 与错误码 `tool_disabled`。
- 浏览器工具默认关闭；需设置 `NEXTAI_ENABLE_BROWSER_TOOL=true`，并提供 `NEXTAI_BROWSER_AGENT_DIR`（指向 `agent.js` 所在目录）后才会注册。
- 搜索工具默认关闭；需设置 `NEXTAI_ENABLE_SEARCH_TOOL=true`。支持多 provider（`serpapi` / `tavily` / `brave`）：
  - `NEXTAI_SEARCH_SERPAPI_KEY` / `NEXTAI_SEARCH_SERPAPI_BASE_URL`
  - `NEXTAI_SEARCH_TAVILY_KEY` / `NEXTAI_SEARCH_TAVILY_BASE_URL`
  - `NEXTAI_SEARCH_BRAVE_KEY` / `NEXTAI_SEARCH_BRAVE_BASE_URL`

请求示例：

```json
{
  "input": [
    {
      "role": "user",
      "type": "message",
      "content": [{ "type": "text", "text": "请读取配置并给出结论" }]
    }
  ],
  "session_id": "s1",
  "user_id": "u1",
  "channel": "console",
  "stream": true
}
```

`stream=false` 返回：

```json
{
  "reply": "最终回复文本",
  "events": [
    { "type": "step_started", "step": 1 },
    { "type": "tool_call", "step": 1, "tool_call": { "name": "shell" } },
    { "type": "tool_result", "step": 1, "tool_result": { "name": "shell", "ok": true, "summary": "..." } },
    { "type": "assistant_delta", "step": 2, "delta": "..." },
    { "type": "completed", "step": 2, "reply": "最终回复文本" }
  ]
}
```

`stream=true` 返回 SSE：`data` payload 与上面的 `events` 同构，事件在执行过程中实时推送（每个事件写出后立即 `flush`），并以 `data: [DONE]` 结束。

其中常规对话的 `assistant_delta` 在 OpenAI-compatible 适配器下透传上游原生 token/delta（不再由 Gateway 按字符二次切片模拟）。若流式处理中途失败，额外发送 `{"type":"error","meta":{"code","message"}}` 后结束。

事件类型：
- `step_started`
- `tool_call`
- `tool_result`
- `assistant_delta`
- `completed`
- `error`（仅流式失败场景）

## Chat Default Session Rule
- Gateway always keeps one protected default chat in state (`id=chat-default`).
- Default chat baseline fields: `session_id=session-default`, `user_id=demo-user`, `channel=console`.
- Default chat carries `meta.system_default=true`.
- `DELETE /chats/{chat_id}` and `POST /chats/batch-delete` reject deleting `chat-default` with `400 default_chat_protected`.

## Cron Default Job Rule
- Gateway always keeps one protected default cron job in state (`id=cron-default`).
- Default cron job baseline fields: `name=你好文本任务`, `task_type=text`, `text=你好`, `enabled=false`.
- `DELETE /cron/jobs/{job_id}` rejects deleting `cron-default` with `400 default_cron_protected`.

## Prompt Layering And Template Rollout (2026-02)

### Phase 1: system layers (no external behavior change)
- Gateway keeps `/agent/process` request/response contract unchanged.
- Internal prompt injection changes from a single `system` message to ordered `system` layers:
  1. `base_system`
  2. `tool_guide_system`
  3. `workspace_policy_system`
  4. `session_policy_system`
- Injection position is unchanged (still prepended before model generation loop).

### Phase 2: `/prompts:<name>` command expansion (client side)
- Template source is `prompts/*.md`.
- Web and TUI expand `/prompts:<name>` before sending to `/agent/process`.
- Phase 2 only supports named args: `KEY=VALUE`.
- Expansion failure blocks sending and returns a client-side error.
- Existing UI slash commands (`/history`, `/new`, `/refresh`, `/settings`, `/exit`) keep current behavior.

### Phase 3: environment context and observability
- Gateway adds a structured `environment_context` as an independent `system` layer when feature flag is enabled.
- New read-only endpoint:
  - `GET /agent/system-layers`
  - 可选 query：
    - `prompt_mode=default|codex`（默认 `default`）
    - `session_id=<chat-session-id>`（可选）
  - Purpose: return effective injected layers and token estimate used for this runtime.

Sample response:

```json
{
  "version": "v1",
  "mode_variant": "default",
  "layers": [
    {
      "name": "base_system",
      "role": "system",
      "source": "prompts/AGENTS.md",
      "content_preview": "## prompts/AGENTS.md ...",
      "layer_hash": "9f2d4d...",
      "estimated_tokens": 12
    }
  ],
  "estimated_tokens_total": 12
}
```

- Error model remains unchanged:
  - `{ "error": { "code": "...", "message": "...", "details": ... } }`
- 若 `prompt_mode` 非法，返回：
  - `400 invalid_request`
  - `message=invalid prompt_mode`

### Feature flags
- `NEXTAI_ENABLE_PROMPT_TEMPLATES` (default: `false`).
- `NEXTAI_ENABLE_PROMPT_CONTEXT_INTROSPECT` (default: `false`).
- `NEXTAI_ENABLE_CODEX_MODE_V2` / `NEXTAI_CODEX_PROMPT_SOURCE` / `NEXTAI_CODEX_PROMPT_SHADOW_COMPARE`：历史兼容字段，当前版本不影响 `prompt_mode` 行为。

## Runtime Config Endpoint (2026-02)
- Gateway 提供公开只读接口（不含敏感信息）：`GET /runtime-config`。
- 返回体：

```json
{
  "features": {
    "prompt_templates": false,
    "prompt_context_introspect": false,
    "codex_mode_v2": false
  }
}
```

- 字段来源：
  - `features.prompt_templates` <- `NEXTAI_ENABLE_PROMPT_TEMPLATES`
  - `features.prompt_context_introspect` <- `NEXTAI_ENABLE_PROMPT_CONTEXT_INTROSPECT`
  - `features.codex_mode_v2` <- 历史兼容位（保留返回，当前不驱动运行时注入路径）
- Web 侧特性开关优先级：`query > localStorage > runtime-config > false`。

## Prompt Mode（会话级，2026-02）
- `POST /agent/process` 支持可选字段：`biz_params.prompt_mode`。
- 枚举值：`default` | `codex`。
- 非法值（包含非字符串或不在枚举内）返回：
  - `400 invalid_request`
  - `message=invalid prompt_mode`

### 会话持久化规则
- 会话元数据新增：`chat.meta.prompt_mode`。
- 若请求显式携带 `biz_params.prompt_mode`，Gateway 会写回并持久化到该会话 `meta.prompt_mode`。
- 若请求未携带 `biz_params.prompt_mode`，执行时按以下优先级解析有效模式：
  1. 请求显式值
  2. 会话 `meta.prompt_mode`
  3. `default`

## Collaboration Mode（历史兼容）
- 当前版本不支持 `prompt_mode=codex`。
- 显式携带 `biz_params.collaboration_mode` / `biz_params.collaboration_event` / `biz_params.collaboration.{mode|event}` 时，返回：
  - `400 invalid_request`
  - `message=collaboration mode is only supported when prompt_mode=codex`

### 系统层注入规则
- `prompt_mode=default`：
  - 维持原行为（`AGENTS + ai-tools` 分层注入）。

### 可观测字段
- `GET /agent/system-layers` 保持原结构兼容，并新增可选字段：
  - 顶层 `mode_variant`: `default` | `codex_v1` | `codex_v2`
  - `layers[].layer_hash`: 每层归一化内容 hash（用于漂移排查）

### 错误语义
- `prompt_mode=default` 继续沿用既有系统层错误语义（如 `ai_tool_guide_unavailable`）。

## Provider Tool Routing（2026-02 A 档进阶集）

- 生效范围：provider tool-call 执行路径（`prompt_mode=default|codex`）。
- 手工入口（`biz_params.tool` 与顶层快捷键）仍保持严格 `items` 契约，不放宽格式校验。

### 注册表能力声明（Capability-Driven）
- Provider Adapter 在注册时声明能力：`stream` / `tool_call` / `attachments` / `reasoning`。
- 调度层按 provider capability 决策：
  - `stream=false`：自动降级为非流式执行，并回放文本增量。
  - `tool_call=false`：不向 provider 下发工具定义。
  - `reasoning=false`：忽略 `reasoning_effort`。
  - `attachments=false`：当请求包含非 `text` 内容时返回 `provider_not_supported`。
- Tool 在注册时声明能力（示例）：`open_local` / `open_url` / `approx_click` / `approx_screenshot`。
- `open` / `click` / `screenshot` 的暴露与路由改为按 tool capability 派生，不再仅依赖工具名硬编码。
- 兼容性：未显式声明 capability 的旧注册项，保留 `view/browser` 名称回退映射。

### 参数归一化（provider 兜底，全 prompt_mode）
- 包装层自动解包：`input` / `arguments` / `args`。
- `items` 若为对象会转为单元素数组。
- 别名映射：
  - `start_line -> start`
  - `end_line -> end`
  - `q -> query`
  - `num_results -> count`
  - `workdir -> cwd`
  - `yield_time_ms -> timeout_seconds`（毫秒向上取整秒）
- 对 `view/edit/shell/browser/search/find`，单对象参数会自动封装为 `{"items":[...]}`。

### 工具名兼容与新增
- 兼容映射：
  - `exec_command` / `functions.exec_command` -> `shell`
  - `view_file_lines` / `view_file_lins` / `view_file` -> `view`
- 新增可识别工具名：`open`、`find`、`click`、`screenshot`。
- `POST /agent/process` 请求结构不变，但流式/非流式事件中的 `tool_call.name` 可能出现上述新增工具名。

### 路由语义
- `open`：
  - 本地绝对路径 -> `view`
  - `http(s)://` -> `browser`（任务模板：打开 URL 并提取结构化摘要）
- `find`：
  - 本地插件实现，输入 `items[].path + items[].pattern`（可选 `ignore_case`）
  - 字面匹配（非正则），默认最多返回 200 条匹配行
  - 路径限制在工作区内（相对路径或工作区内绝对路径）
- `click` / `screenshot`（A 档）：
  - 近似路由到 `browser`，不维护页面会话状态
  - 返回文本显式标注 `mode=approx`

## Gateway 运行时超时与优雅停机（2026-02）

网关启动入口 `apps/gateway/cmd/gateway/main.go` 使用 `http.Server` 承载，并支持以下运行时参数：

- `NEXTAI_HTTP_READ_HEADER_TIMEOUT_SECONDS`（默认 `10`）
- `NEXTAI_HTTP_READ_TIMEOUT_SECONDS`（默认 `120`）
- `NEXTAI_HTTP_WRITE_TIMEOUT_SECONDS`（默认 `0`，即不限制，适配 SSE 长连接）
- `NEXTAI_HTTP_IDLE_TIMEOUT_SECONDS`（默认 `120`）
- `NEXTAI_HTTP_SHUTDOWN_TIMEOUT_SECONDS`（默认 `30`）

行为约定：
- 收到 `SIGINT`/`SIGTERM` 后进入优雅停机，先停止接收新连接，再等待在途请求完成。
- 超过 `NEXTAI_HTTP_SHUTDOWN_TIMEOUT_SECONDS` 仍未完成时，执行强制关闭（可控降级），避免无限阻塞重启流程。

## 默认日志脱敏约定

- 默认日志禁止打印用户消息正文。
- Console 渠道发送日志仅记录统计信息（例如字符数），不记录 `text` 原文。
- 排查问题优先用请求 ID、错误码和事件时间线，不依赖敏感文本回放。

## 用户错误排查手册

### 1) 连不上网关（Connection refused / timeout）
- 典型现象：
  - `curl: (7) Failed to connect`
  - 浏览器打不开 `http://127.0.0.1:8088`
- 快速排查：
  - `curl -sS http://127.0.0.1:8088/healthz`
  - `lsof -iTCP:8088 -sTCP:LISTEN`
  - 检查环境变量：`NEXTAI_HOST`、`NEXTAI_PORT`
- 修复动作：
  - 先启动网关：`cd apps/gateway && go run ./cmd/gateway`
  - 若端口冲突，改端口后重启：`NEXTAI_PORT=18088 go run ./cmd/gateway`

### 2) `401 unauthorized`（缺失或错误 API Key）
- 典型现象：
  - 返回：`{"error":{"code":"unauthorized","message":"missing or invalid api key"}}`
- 快速排查：
  - 检查网关是否设置了 `NEXTAI_API_KEY`
  - 请求是否携带 `X-API-Key` 或 `Authorization: Bearer <key>`
- 修复动作：
  - curl 示例：

```bash
curl -sS http://127.0.0.1:8088/chats \
  -H 'X-API-Key: <your-key>'
```

### 3) 模型不可用（`model_not_found` / `provider_disabled` / `provider_request_failed`）
- 典型现象：
  - `{"error":{"code":"model_not_found",...}}`
  - `{"error":{"code":"provider_disabled",...}}`
  - `{"error":{"code":"provider_request_failed",...}}`
- 快速排查：
  - `GET /models/catalog` 查看 provider 与 active_llm
  - `GET /models/active` 查看当前激活模型
  - 检查 provider `api_key`、`base_url`、`model_aliases`、`store`、`reasoning_effort`
- 修复动作：
  - 先配置 provider，再设置 active model：

```bash
curl -sS -X PUT http://127.0.0.1:8088/models/openai/config \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"api_key":"sk-xxx","base_url":"https://api.openai.com/v1"}'

curl -sS -X PUT http://127.0.0.1:8088/models/active \
  -H 'Content-Type: application/json' \
  -d '{"provider_id":"openai","model":"gpt-4o-mini"}'
```

### 4) 重启时请求中断
- 现象：重启窗口内个别长请求失败。
- 原因：请求耗时超过优雅停机窗口，被强制关闭。
- 修复动作：
  - 适度增大 `NEXTAI_HTTP_SHUTDOWN_TIMEOUT_SECONDS`，例如 `60`。
  - 对超长请求，客户端增加重试与幂等处理。

## 新人 30 分钟首轮对话清单

1. 安装依赖：`pnpm install --recursive`
2. 启动网关：`cd apps/gateway && go run ./cmd/gateway`
3. 健康检查：`curl -sS http://127.0.0.1:8088/healthz`
4. 发起首轮对话（无 API Key 场景）：

```bash
curl -sS -X POST http://127.0.0.1:8088/agent/process \
  -H 'Content-Type: application/json' \
  -d '{
    "input":[{"role":"user","type":"message","content":[{"type":"text","text":"你好，做个自我介绍"}]}],
    "session_id":"quickstart-s1",
    "user_id":"quickstart-u1",
    "channel":"console",
    "stream":false
  }'
```

5. 看到 `reply` 字段即表示首轮对话成功。

推荐回归入口：
- `pnpm run test:all`（串联 Go + TS + contract + smoke）

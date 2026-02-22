# API / CLI 最小契约

## API
- `/version`, `/healthz`
- `/runtime-config`
- `/chats`, `/chats/{chat_id}`, `/chats/batch-delete`
- `/agent/process`
- `/agent/system-layers`
- `/channels/qq/inbound`
- `/channels/qq/state`
- `/cron/jobs` 系列
- `/models` 系列
- `/envs` 系列
- `/skills` 系列
- `/workspace/files`, `/workspace/files/{file_path}`
- `/workspace/uploads`, `/workspace/export`, `/workspace/import`
- `/config/channels` 系列

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
  - 可选 query：`prompt_mode=default|codex|claude`（默认 `default`）
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
- `NEXTAI_ENABLE_CODEX_MODE_V2` (default: `false`).

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
  - `features.codex_mode_v2` <- `NEXTAI_ENABLE_CODEX_MODE_V2`
- Web 侧特性开关优先级：`query > localStorage > runtime-config > false`。

## Prompt Mode（会话级，2026-02）
- `POST /agent/process` 支持可选字段：`biz_params.prompt_mode`。
- 枚举值：`default` | `codex` | `claude`。
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

### 系统层注入规则
- `prompt_mode=default`：
  - 维持原行为（`AGENTS + ai-tools` 分层注入）。
- `prompt_mode=codex`：
  - 必选注入 `prompts/codex/codex-rs/core/prompt.md`（codex base）。
  - 当 `NEXTAI_ENABLE_CODEX_MODE_V2=false`（`mode_variant=codex_v1`）：
    - 维持原行为；仅当 `NEXTAI_ENABLE_PROMPT_TEMPLATES=true` 时追加 codex 模板层。
  - 当 `NEXTAI_ENABLE_CODEX_MODE_V2=true`（`mode_variant=codex_v2`）：
    - 按确定性顺序尝试注入：
      1. `codex_base_system`（必选）
      2. `codex_orchestrator_system`（可选）
      3. `codex_model_instructions_system`（可选，模板渲染）
      4. `codex_collaboration_default_system`（可选，模板渲染）
      5. `codex_experimental_collab_system`（可选）
      6. `codex_local_policy_system`（可选，`prompts/AGENTS.md`）
      7. `codex_tool_guide_system`（可选，`prompts/ai-tools.md` + legacy fallback）
    - 模板变量标准化来源：
      - `personality` <- `prompts/codex/codex-rs/core/templates/personalities/gpt-5.2-codex_pragmatic.md`
      - `KNOWN_MODE_NAMES` <- 当前支持模式名拼接（非硬编码）
      - `REQUEST_USER_INPUT_AVAILABILITY` <- 基于 mode 能力生成
    - V2 在编排末尾执行内容归一化去重（优先级：codex 核心层 > 本地策略层 > 工具层）。
  - 两个变体都不叠加 default 模式的系统层。
- `prompt_mode=claude`：
  - 必选注入 `prompts/claude/main.md`（claude base）。
  - 可选按顺序注入：
    1. `claude_doing_tasks_system`（`prompts/claude/doing-tasks.md`）
    2. `claude_execution_care_system`（`prompts/claude/executing-actions-with-care.md`）
    3. `claude_tool_usage_policy_system`（`prompts/claude/tool-usage-policy.md`）
    4. `claude_tone_style_system`（`prompts/claude/tone-and-style.md`）
    5. `claude_local_policy_system`（`prompts/AGENTS.md`）
    6. `claude_tool_guide_system`（`prompts/ai-tools.md` + legacy fallback）
  - `mode_variant=claude_v1`，并在编排末尾执行内容归一化去重。
  - 不叠加 default/codex 的系统层。

### 可观测字段
- `GET /agent/system-layers` 保持原结构兼容，并新增可选字段：
  - 顶层 `mode_variant`: `default` | `codex_v1` | `codex_v2` | `claude_v1`
  - `layers[].layer_hash`: 每层归一化内容 hash（用于漂移排查）

### 错误语义
- `prompt_mode=codex` 且 codex base 文件缺失或为空时返回：
  - `500 codex_prompt_unavailable`
  - `message=codex prompt is unavailable`
- `prompt_mode=claude` 且 claude base 文件缺失或为空时返回：
  - `500 claude_prompt_unavailable`
  - `message=claude prompt is unavailable`
- `prompt_mode=default` 继续沿用既有系统层错误语义（如 `ai_tool_guide_unavailable`）。
- V2 可选层（local policy/tool guide/模板层）缺失或渲染失败仅 warning + 跳过，不阻塞主请求。

### 回滚语义
- 关闭 `NEXTAI_ENABLE_CODEX_MODE_V2` 可立即回退到 `codex_v1` 行为，无需变更前端协议。

## Provider Tool Routing（2026-02 A 档进阶集）

- 生效范围：provider tool-call 执行路径（`prompt_mode=default|codex|claude`）。
- 手工入口（`biz_params.tool` 与顶层快捷键）仍保持严格 `items` 契约，不放宽格式校验。

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
  - 检查 provider `api_key`、`base_url`、`model_aliases`
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

### 4) 提示词模式不可用（`codex_prompt_unavailable` / `claude_prompt_unavailable`）
- 典型现象：
  - `500 codex_prompt_unavailable`
  - `500 claude_prompt_unavailable`
- 快速排查：
  - 检查对应提示词文件是否存在且非空：
    - `prompts/codex/codex-rs/core/prompt.md`
    - `prompts/claude/main.md`
- 修复动作：
  - 补齐文件后重启网关。

### 5) 重启时请求中断
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

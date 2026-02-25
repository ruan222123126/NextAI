# AI Tool Guide

通过 `POST /agent/process` 触发工具调用。

## 统一格式（必用）

- 除 `request_user_input` 外，tool/function calling 的 `arguments` 必须是对象，统一为 `{"items":[...]}`。
- 单次调用也必须传单元素数组。
- `request_user_input` 使用 `{"questions":[...]}`，不走 `items` 包装。

```json
{
  "items": [{ "...": "..." }]
}
```

## 各工具 `items` 字段

- `view`: `path`(绝对路径), `start`, `end`
- `shell`: `command`, 可选 `cwd`, `timeout_seconds`
- `browser`: `task`, 可选 `timeout_seconds`
- `search`: `query`, 可选 `provider`, `count`, `timeout_seconds`
- `find`: `path`(工作区内路径), `pattern`, 可选 `ignore_case`

## `request_user_input`（Plan 模式）

- 入参格式：`{"questions":[...]}`
- `questions` 数量：1-3
- 每个问题必填字段：`question`
- 可选字段：`id`、`header`（不传会由系统自动补全）
- 可选 `options`：自由数量；每项必填 `label`，可选 `description`
- 问题和选项必须按当前任务动态撰写，禁止机械套用固定模板
- 仅在关键缺失信息会影响执行方案时调用，不要每轮都问。

## `output_plan`（最终计划输出）

- 用途：将最终结构化计划写入系统（供前端计划面板与后续执行流程使用）。
- 推荐入参格式：

```json
{
  "plan": {
    "goal": "目标描述",
    "scope_in": ["范围内项"],
    "scope_out": ["范围外项"],
    "constraints": ["约束"],
    "assumptions": ["假设"],
    "tasks": [
      {
        "id": "task-1",
        "title": "任务标题",
        "description": "任务说明",
        "depends_on": [],
        "status": "pending",
        "deliverables": ["交付物"],
        "verification": ["验证方式"]
      }
    ],
    "acceptance_criteria": ["验收标准"],
    "risks": [
      {
        "id": "risk-1",
        "title": "风险标题",
        "description": "风险说明",
        "mitigation": "缓解策略"
      }
    ],
    "summary_for_execution": "执行摘要"
  }
}
```

- 在回复用户“最终计划”前，必须先调用 `output_plan`。

## 手工请求（推荐）

若手工调用 `POST /agent/process`，使用 `biz_params.tool`：

```json
{
  "biz_params": {
    "tool": {
      "name": "shell",
      "items": [{ "command": "pwd" }]
    }
  }
}
```

## A 档近似路由说明

- `open`：
  - 本地绝对路径：路由到 `view`
  - `http(s)://`：路由到 `browser`，任务模板为“打开 URL 并提取结构化摘要”
- `click` / `screenshot`：
  - A 档为近似能力，路由到 `browser` 执行任务模板
  - 返回结果会标记 `mode=approx`
  - 不保证页面会话状态连续性

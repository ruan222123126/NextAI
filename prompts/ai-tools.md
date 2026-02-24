# AI Tool Guide

通过 `POST /agent/process` 触发工具调用。

## 统一格式（必用）

- tool/function calling 的 `arguments` 必须是对象，统一为 `{"items":[...]}`。
- 单次调用也必须传单元素数组。
- 禁止旧写法：`"view":[...]`、`"edit":[...]`、`"shell":[...]`、`"browser":[...]`、`"search":[...]`、`"find":[...]`。

```json
{
  "items": [{ "...": "..." }]
}
```

## 各工具 `items` 字段

- `view`: `path`(绝对路径), `start`, `end`
- `edit`: `path`(绝对路径), `start`, `end`, `content`
- `shell`: `command`, 可选 `cwd`, `timeout_seconds`
- `browser`: `task`, 可选 `timeout_seconds`
- `search`: `query`, 可选 `provider`, `count`, `timeout_seconds`
- `find`: `path`(工作区内路径), `pattern`, 可选 `ignore_case`

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

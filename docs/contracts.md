# API / CLI 最小契约

## API
- /version, /healthz
- /chats, /chats/{chat_id}, /chats/batch-delete
- /agent/process
- /cron/jobs 系列
- /models 系列
- /envs 系列
- /skills 系列
- /workspace/files, /workspace/files/{file_path}
- /workspace/export, /workspace/import
- /config/channels 系列

## CLI
- copaw app start
- copaw chats list/create/get/delete/send
- copaw cron list/create/update/delete/pause/resume/run/state
- copaw models list/config/active-get/active-set
- copaw env list/set/delete
- copaw skills list/create/enable/disable/delete
- copaw workspace ls/cat/put/rm/export/import
- copaw channels list/types/get/set

## /agent/process 工具调用协议

`POST /agent/process` 支持通过 `biz_params.tool` 直接触发工具执行（无需聊天命令语法解析）。

请求示例：

```json
{
  "input": [
    {
      "role": "user",
      "type": "message",
      "content": [{ "type": "text", "text": "读取 docs/contracts.md" }]
    }
  ],
  "session_id": "s1",
  "user_id": "u1",
  "channel": "console",
  "stream": false,
  "biz_params": {
    "tool": {
      "name": "read_file",
      "input": { "path": "docs/contracts.md" }
    }
  }
}
```

工具定义：

- `read_file`: `{"path":"relative/path.txt"}`
- `create_file`: `{"path":"relative/path.txt","content":"..."}`
- `update_file`: `{"path":"relative/path.txt","content":"...","mode":"overwrite|append"}`
- `shell`: `{"command":"...","cwd":"...","timeout_seconds":20}`

约束：

- 文件路径必须是仓库根目录下相对路径。
- 禁止绝对路径和 `..` 穿越。
- `update_file.mode` 默认 `overwrite`。

错误码（`error.code`）：

- `invalid_tool_input`: 工具输入不合法。
- `tool_forbidden_path`: 路径越界或越权访问。
- `tool_not_found`: 工具不存在，或目标文件不存在。
- `tool_conflict`: 资源冲突（如文件已存在）。
- `tool_error`: 工具内部错误。
- `tool_disabled`: 工具被服务端禁用（如 shell）。

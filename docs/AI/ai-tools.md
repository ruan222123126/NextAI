# AI Tool Guide (Gateway)

你通过 `POST /agent/process` 的 `biz_params.tool` 触发工具调用。请严格遵守以下协议和安全边界。

## 1. 工具清单

- `read_file`: 读取仓库内文本文件。
- `create_file`: 创建新文件（仅在文件不存在时成功）。
- `update_file`: 修改已有文件（覆盖或追加）。
- `shell`: 执行 shell 命令（高风险，谨慎使用）。

## 2. 调用格式

请求体结构（节选）：

```json
{
  "input": [
    {
      "role": "user",
      "type": "message",
      "content": [{ "type": "text", "text": "请读取 docs/contracts.md" }]
    }
  ],
  "session_id": "s1",
  "user_id": "u1",
  "channel": "console",
  "stream": false,
  "biz_params": {
    "tool": {
      "name": "read_file",
      "input": {
        "path": "docs/contracts.md"
      }
    }
  }
}
```

`biz_params.tool` 字段：
- `name`: 工具名，必须为 `read_file | create_file | update_file | shell`。
- `input`: 工具入参对象，结构随工具类型变化。

## 3. 各工具输入规范与示例

### 3.1 read_file

输入：

```json
{ "path": "relative/path.txt" }
```

成功输出示例（tool result）：

```json
{
  "ok": true,
  "path": "relative/path.txt",
  "size": 123,
  "truncated": false,
  "content": "...",
  "text": "..."
}
```

### 3.2 create_file

输入：

```json
{ "path": "relative/path.txt", "content": "..." }
```

成功输出示例：

```json
{
  "ok": true,
  "path": "relative/path.txt",
  "size": 123,
  "text": "created relative/path.txt (123 bytes)"
}
```

### 3.3 update_file

输入：

```json
{ "path": "relative/path.txt", "content": "...", "mode": "overwrite" }
```

`mode` 可选值：
- `overwrite`（默认）
- `append`

成功输出示例：

```json
{
  "ok": true,
  "path": "relative/path.txt",
  "mode": "overwrite",
  "size": 456,
  "text": "updated relative/path.txt with mode=overwrite"
}
```

### 3.4 shell

输入：

```json
{ "command": "pwd", "cwd": "apps/gateway", "timeout_seconds": 20 }
```

成功输出示例：

```json
{
  "ok": true,
  "command": "pwd",
  "exit_code": 0,
  "output": "...",
  "text": "..."
}
```

## 4. 失败错误码

工具调用相关错误码（`error.code`）：

- `invalid_tool_input`: 输入结构或字段不合法。
- `tool_forbidden_path`: 路径越界或访问仓库外路径。
- `tool_not_found`: 工具不存在，或目标文件不存在。
- `tool_conflict`: 资源冲突（例如创建已存在文件）。
- `tool_error`: 工具执行过程中的其他错误。
- `tool_disabled`: 工具被服务端禁用（主要用于 shell）。

## 5. 安全边界（必须遵守）

- 允许相对路径与绝对路径访问；禁止通过 `..` 进行路径穿越。
- 建议允许访问根目录：`/mnt/Files`、`/home/ruan`（可按本地环境调整）。
- 以下系统目录应设为强制黑名单并禁止读写：
  - `/bin`
  - `/sbin`
  - `/usr`
  - `/lib`
  - `/lib64`
  - `/etc`
  - `/proc`
  - `/sys`
  - `/dev`
  - `/boot`
  - `/run`
  - `/var/run`
- 对符号链接应先解析 realpath，再按黑名单判定；命中黑名单时必须拒绝。

## 6. 推荐调用顺序

1. `read_file` 先读上下文。
2. `update_file` 或 `create_file` 执行修改。
3. 再次 `read_file` 校验结果。
4. 仅在文件工具无法完成时再考虑 `shell`。

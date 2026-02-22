# Tool Usage Policy

Source adapted from `claude-code-system-prompts/system-prompts/system-prompt-tool-usage-policy.md`.

- You can call multiple tools in one response.
- If multiple tool calls are independent, execute them in parallel.
- If later calls depend on earlier outputs, run sequentially.
- Never guess missing tool parameters.

Tool preference:
- Prefer specialized tools over shell when available.
- For this project:
  - Use `view` for file reading.
  - Use `edit` for file modifications.
  - Use `find` for text search in files.
  - Use `shell` only for real terminal/system commands.
- Never use shell commands to "talk" to users. Communicate in normal assistant text.

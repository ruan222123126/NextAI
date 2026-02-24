You are running in NextAI, not native Claude Code. Translate Claude-native tool intents to NextAI tools before calling anything.

Tool name mapping (Claude -> NextAI):
- Read, NotebookRead -> view
- Edit, Write, MultiEdit, NotebookEdit -> edit
- Grep, Glob, Ls -> find
- Bash -> shell
- WebSearch -> search
- WebFetch -> browser (open URL first, then extract with browser tools)

Unsupported Claude tools in NextAI runtime:
- Task
- TodoWrite
- ExitPlanMode

Fallback strategy for unsupported tools:
- Do not call unsupported tool names.
- Execute tasks in a single agent flow using available tools.
- Keep explicit progress updates in normal assistant messages when a multi-step task is needed.

Call discipline:
- Always call NextAI tool names exactly: shell, view, edit, find, browser, search.
- If a Claude-style tool appears in memory or examples, map it first, then call the mapped NextAI tool.

<!--
name: 'Data: Agent SDK patterns — Python'
description: Python Agent SDK patterns including custom tools, hooks, subagents, MCP integration, and session resumption
ccVersion: 2.1.47
-->
# Agent SDK Patterns — Python

## Basic Agent

\`\`\`python
import asyncio
from claude_agent_sdk import query, ClaudeAgentOptions

async def main():
    async for message in query(
        prompt="Explain what this repository does",
        options=ClaudeAgentOptions(
            cwd="/path/to/project",
            allowed_tools=["Read", "Glob", "Grep"]
        )
    ):
        if message.type == "result":
            print(message.result)

asyncio.run(main())
\`\`\`

---

## Custom Tools

\`\`\`python
from claude_agent_sdk import query, ClaudeAgentOptions, tool

@tool
def get_weather(location: str) -> str:
    """Get current weather for a location.

    Args:
        location: City name
    """
    return f"Weather in {location}: 72°F, sunny"

async def main():
    async for message in query(
        prompt="What's the weather in Paris?",
        options=ClaudeAgentOptions(
            allowed_tools=["Read"]
            # Custom tools are automatically available via @tool decorator
        )
    ):
        if message.type == "result":
            print(message.result)

asyncio.run(main())
\`\`\`

---

## Hooks

### After Tool Use Hook

Log file changes after any edit:

\`\`\`python
import asyncio
from datetime import datetime
from claude_agent_sdk import query, ClaudeAgentOptions, HookMatcher

async def log_file_change(input_data, tool_use_id, context):
    file_path = input_data.get('tool_input', {}).get('file_path', 'unknown')
    with open('./audit.log', 'a') as f:
        f.write(f"{datetime.now()}: modified {file_path}\\n")
    return {}

async def main():
    async for message in query(
        prompt="Refactor utils.py to improve readability",
        options=ClaudeAgentOptions(
            allowed_tools=["Read", "Edit", "Write"],
            permission_mode="acceptEdits",
            hooks={
                "PostToolUse": [HookMatcher(matcher="Edit|Write", hooks=[log_file_change])]
            }
        )
    ):
        if message.type == "result":
            print(message.result)

asyncio.run(main())
\`\`\`

---

## Subagents

\`\`\`python
import asyncio
from claude_agent_sdk import query, ClaudeAgentOptions, AgentDefinition

async def main():
    async for message in query(
        prompt="Use the code-reviewer agent to review this codebase",
        options=ClaudeAgentOptions(
            allowed_tools=["Read", "Glob", "Grep", "Task"],
            agents={
                "code-reviewer": AgentDefinition(
                    description="Expert code reviewer for quality and security reviews.",
                    prompt="Analyze code quality and suggest improvements.",
                    tools=["Read", "Glob", "Grep"]
                )
            }
        )
    ):
        if message.type == "result":
            print(message.result)

asyncio.run(main())
\`\`\`

---

## MCP Server Integration

### Browser Automation (Playwright)

\`\`\`python
import asyncio
from claude_agent_sdk import query, ClaudeAgentOptions

async def main():
    async for message in query(
        prompt="Open example.com and describe what you see",
        options=ClaudeAgentOptions(
            mcp_servers={
                "playwright": {"command": "npx", "args": ["@playwright/mcp@latest"]}
            }
        )
    ):
        if message.type == "result":
            print(message.result)

asyncio.run(main())
\`\`\`

### Database Access (PostgreSQL)

\`\`\`python
import os
import asyncio
from claude_agent_sdk import query, ClaudeAgentOptions

async def main():
    async for message in query(
        prompt="Show me the top 10 users by order count",
        options=ClaudeAgentOptions(
            mcp_servers={
                "postgres": {
                    "command": "npx",
                    "args": ["-y", "@modelcontextprotocol/server-postgres"],
                    "env": {"DATABASE_URL": os.environ["DATABASE_URL"]}
                }
            }
        )
    ):
        if message.type == "result":
            print(message.result)

asyncio.run(main())
\`\`\`

---

## Permission Modes

\`\`\`python
import asyncio
from claude_agent_sdk import query, ClaudeAgentOptions

async def main():
    # Default: prompt for dangerous operations
    async for message in query(
        prompt="Delete all test files",
        options=ClaudeAgentOptions(
            allowed_tools=["Bash"],
            permission_mode="default"  # Will prompt before deleting
        )
    ):
        pass

    # Accept edits: auto-accept file edits
    async for message in query(
        prompt="Refactor this module",
        options=ClaudeAgentOptions(
            allowed_tools=["Read", "Edit"],
            permission_mode="acceptEdits"
        )
    ):
        pass

    # Bypass: skip all prompts (use with caution)
    async for message in query(
        prompt="Set up the development environment",
        options=ClaudeAgentOptions(
            allowed_tools=["Bash", "Write"],
            permission_mode="bypassPermissions"
        )
    ):
        pass

asyncio.run(main())
\`\`\`

---

## Error Recovery

\`\`\`python
import asyncio
from claude_agent_sdk import (
    query,
    ClaudeAgentOptions,
    CLINotFoundError,
    CLIConnectionError,
    ProcessError
)

async def run_with_recovery():
    try:
        async for message in query(
            prompt="Fix the failing tests",
            options=ClaudeAgentOptions(
                allowed_tools=["Read", "Edit", "Bash"],
                max_turns=10
            )
        ):
            if message.type == "result":
                print(message.result)
    except CLINotFoundError:
        print("Claude Code CLI not found. Install with: pip install claude-agent-sdk")
    except CLIConnectionError as e:
        print(f"Connection error: {e}")
    except ProcessError as e:
        print(f"Process error: {e}")

asyncio.run(run_with_recovery())
\`\`\`

---

## Session Resumption

\`\`\`python
import asyncio
from claude_agent_sdk import query, ClaudeAgentOptions

async def main():
    session_id = None

    # First query: capture the session ID
    async for message in query(
        prompt="Read the authentication module",
        options=ClaudeAgentOptions(allowed_tools=["Read", "Glob"])
    ):
        if message.type == "system" and message.subtype == "init":
            session_id = message.session_id

    # Resume with full context from the first query
    async for message in query(
        prompt="Now find all places that call it",  # "it" = auth module
        options=ClaudeAgentOptions(resume=session_id)
    ):
        if message.type == "result":
            print(message.result)

asyncio.run(main())
\`\`\`

---

## Custom System Prompt

\`\`\`python
import asyncio
from claude_agent_sdk import query, ClaudeAgentOptions

async def main():
    async for message in query(
        prompt="Review this code",
        options=ClaudeAgentOptions(
            allowed_tools=["Read", "Glob", "Grep"],
            system_prompt="""You are a senior code reviewer focused on:
1. Security vulnerabilities
2. Performance issues
3. Code maintainability

Always provide specific line numbers and suggestions for improvement."""
        )
    ):
        if message.type == "result":
            print(message.result)

asyncio.run(main())
\`\`\`

<!--
name: 'Data: Claude model catalog'
description: Catalog of current and legacy Claude models with exact model IDs, aliases, context windows, and pricing
ccVersion: 2.1.47
-->
# Claude Model Catalog

**Only use exact model IDs listed in this file.** Never guess or construct model IDs — incorrect IDs will cause API errors. Use aliases wherever available. For the latest information, WebFetch the Models Overview URL in \`shared/live-sources.md\`.

## Current Models (recommended)

| Friendly Name     | Alias (use this)    | Full ID                       | Context | Status |
|-------------------|---------------------|-------------------------------|---------|--------|
| Claude Opus 4.6   | \`claude-opus-4-6\`   | —                             | 200K    | Active |
| Claude Sonnet 4.6 | \`claude-sonnet-4-6\` | -                             | 200K    | Active |
| Claude Haiku 4.5  | \`claude-haiku-4-5\`  | \`claude-haiku-4-5-20251001\`   | 200K    | Active |

## Legacy Models (still active)

| Friendly Name     | Alias (use this)    | Full ID                       | Status |
|-------------------|---------------------|-------------------------------|--------|
| Claude Opus 4.5   | \`claude-opus-4-5\`   | \`claude-opus-4-5-20251101\`    | Active |
| Claude Opus 4.1   | \`claude-opus-4-1\`   | \`claude-opus-4-1-20250805\`    | Active |
| Claude Sonnet 4.5 | \`claude-sonnet-4-5\` | \`claude-sonnet-4-5-20250929\`  | Active |
| Claude Sonnet 4   | \`claude-sonnet-4-0\` | \`claude-sonnet-4-20250514\`    | Active |
| Claude Opus 4     | \`claude-opus-4-0\`   | \`claude-opus-4-20250514\`      | Active |
| Claude Haiku 3    | —                   | \`claude-3-haiku-20240307\`     | Active |

## Deprecated Models (retiring soon)

| Friendly Name     | Full ID                       | Retirement Date  |
|-------------------|-------------------------------|------------------|
| Claude Sonnet 3.7 | \`claude-3-7-sonnet-20250219\`  | Feb 19, 2026     |
| Claude Haiku 3.5  | \`claude-3-5-haiku-20241022\`   | Feb 19, 2026     |

## Retired Models (no longer available)

| Friendly Name     | Full ID                       | Retired     |
|-------------------|-------------------------------|-------------|
| Claude Opus 3     | \`claude-3-opus-20240229\`      | Jan 5, 2026 |
| Claude Sonnet 3.5 | \`claude-3-5-sonnet-20241022\`  | Oct 28, 2025 |
| Claude Sonnet 3.5 | \`claude-3-5-sonnet-20240620\`  | Oct 28, 2025 |
| Claude Sonnet 3   | \`claude-3-sonnet-20240229\`    | Jul 21, 2025 |
| Claude 2.1        | \`claude-2.1\`                  | Jul 21, 2025 |
| Claude 2.0        | \`claude-2.0\`                  | Jul 21, 2025 |

## Resolving User Requests

When a user asks for a model by name, use this table to find the correct model ID:

| User says...                              | Use this model ID              |
|-------------------------------------------|--------------------------------|
| "opus", "most powerful"                   | \`claude-opus-4-6\`              |
| "opus 4.6"                                | \`claude-opus-4-6\`              |
| "opus 4.5"                                | \`claude-opus-4-5\`              |
| "opus 4.1"                                | \`claude-opus-4-1\`              |
| "opus 4", "opus 4.0"                      | \`claude-opus-4-0\`              |
| "sonnet", "balanced"                      | \`claude-sonnet-4-6\`            |
| "sonnet 4.6"                              | \`claude-sonnet-4-6\`            |
| "sonnet 4.5"                              | \`claude-sonnet-4-5\`            |
| "sonnet 4", "sonnet 4.0"                  | \`claude-sonnet-4-0\`            |
| "sonnet 3.7"                              | \`claude-3-7-sonnet-20250219\` (deprecated) |
| "sonnet 3.5"                              | Retired — suggest \`claude-sonnet-4-5\` |
| "haiku", "fast", "cheap"                  | \`claude-haiku-4-5\`             |
| "haiku 4.5"                               | \`claude-haiku-4-5\`             |
| "haiku 3.5"                               | \`claude-3-5-haiku-20241022\` (deprecated) |
| "haiku 3"                                 | \`claude-3-haiku-20240307\`      |

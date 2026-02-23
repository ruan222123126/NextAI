// @ts-nocheck
interface AgentToolCallPayload {
  name?: string;
  input?: Record<string, unknown>;
}

interface AgentToolResultPayload {
  name?: string;
  summary?: string;
}

interface AgentStreamEvent {
  type?: string;
  tool_call?: AgentToolCallPayload;
  tool_result?: AgentToolResultPayload;
}

interface ViewToolCallNotice {
  summary: string;
  raw: string;
  toolName?: string;
  outputReady?: boolean;
}

export function createChatToolCallHelpers(ctx: any) {
  const { t } = ctx;

  function isToolCallRawNotice(raw: string): boolean {
    const payload = parseToolCallRawEvent(raw);
    return payload?.type === "tool_call";
  }

  function normalizeToolName(value: unknown): string {
    if (typeof value !== "string") {
      return "";
    }
    return value.trim();
  }

  function parseToolNameFromToolCallRaw(raw: string): string {
    const payload = parseToolCallRawEvent(raw);
    if (!payload) {
      return "";
    }
    if (payload.type === "tool_call") {
      return normalizeToolName(payload.tool_call?.name);
    }
    if (payload.type === "tool_result") {
      return normalizeToolName(payload.tool_result?.name);
    }
    return "";
  }

  function parseToolCallRawEvent(raw: string): AgentStreamEvent | null {
    const normalized = raw.trim();
    if (normalized === "") {
      return null;
    }
    try {
      return JSON.parse(normalized) as AgentStreamEvent;
    } catch {
      return null;
    }
  }

  function resolveToolCallSummaryLine(toolCall: ViewToolCallNotice): string {
    const fallback = resolveToolCallFallbackSummary(toolCall);
    if (toolCall.outputReady === false) {
      return fallback;
    }
    const raw = toolCall.raw.trim();
    if (raw === "" || raw === t("chat.toolCallOutputPending") || raw === t("chat.toolCallOutputUnavailable")) {
      return fallback;
    }
    const payload = parseToolCallRawEvent(raw);
    if (payload) {
      if (payload.type === "tool_result") {
        const summary = typeof payload.tool_result?.summary === "string" ? payload.tool_result.summary.trim() : "";
        if (summary !== "") {
          const actionSummary = summarizeToolResultActions(summary);
          if (actionSummary !== "") {
            return actionSummary;
          }
          return truncateToolCallSummary(summary);
        }
      }
      return fallback;
    }
    const actionSummary = summarizeToolResultActions(raw);
    if (actionSummary !== "") {
      return actionSummary;
    }
    return truncateToolCallSummary(firstNonEmptyLine(raw) || fallback);
  }

  function resolveToolCallFallbackSummary(toolCall: ViewToolCallNotice): string {
    const summary = toolCall.summary.trim();
    if (summary !== "") {
      return summary;
    }
    return t("chat.toolCallNotice", { target: resolveToolCallDisplayName(toolCall) });
  }

  function resolveToolCallExpandedDetail(toolCall: ViewToolCallNotice): string {
    const raw = toolCall.raw.trim();
    if (raw === "") {
      return t("chat.toolCallOutputUnavailable");
    }
    if (raw === t("chat.toolCallOutputPending") || raw === t("chat.toolCallOutputUnavailable")) {
      return raw;
    }
    const payload = parseToolCallRawEvent(raw);
    if (!payload) {
      return raw;
    }
    if (payload.type === "tool_result") {
      return formatToolResultOutput(payload.tool_result);
    }
    if (payload.type === "tool_call") {
      return t("chat.toolCallOutputPending");
    }
    return raw;
  }

  function firstNonEmptyLine(text: string): string {
    const lines = text.split(/\r?\n/);
    for (const line of lines) {
      const normalized = line.trim();
      if (normalized !== "") {
        return normalized;
      }
    }
    return "";
  }

  function summarizeToolResultActions(text: string): string {
    const lines = text
      .split(/(?:\r?\n|\\n)+/g)
      .map((line) => line.trim())
      .filter((line) => line !== "");
    if (lines.length === 0) {
      return "";
    }
    let fileCount = 0;
    let listCount = 0;
    for (const line of lines) {
      if (isListBrowseLine(line)) {
        listCount += 1;
        continue;
      }
      if (isFileBrowseLine(line)) {
        fileCount += 1;
      }
    }
    if (fileCount === 0 && listCount === 0) {
      return "";
    }
    const parts: string[] = [];
    if (fileCount > 0) {
      parts.push(t("chat.toolCallSummaryFileCount", { count: fileCount }));
    }
    if (listCount > 0) {
      parts.push(t("chat.toolCallSummaryListCount", { count: listCount }));
    }
    return `${t("chat.toolCallActionBrowse")} ${parts.join("，")}`;
  }

  function isListBrowseLine(line: string): boolean {
    if (line.includes("列表") || line.includes("目录")) {
      return true;
    }
    return /(^|\s)listed files(\s|$)/i.test(line) || /^list files\b/i.test(line) || /^ls(\s|$)/i.test(line);
  }

  function isFileBrowseLine(line: string): boolean {
    if (line.includes("文件")) {
      return true;
    }
    return /^read\s+\S+/i.test(line) || /^view\s+\S+/i.test(line) || /^cat\s+\S+/i.test(line) || /^opened?\s+\S+/i.test(line);
  }

  function truncateToolCallSummary(value: string): string {
    const normalized = value.trim();
    if (normalized.length <= 120) {
      return normalized;
    }
    return `${normalized.slice(0, 117)}...`;
  }

  function resolveToolCallDisplayName(toolCall: ViewToolCallNotice): string {
    const fromNotice = normalizeToolName(toolCall.toolName);
    if (fromNotice !== "") {
      return fromNotice;
    }
    const fromRaw = parseToolNameFromToolCallRaw(toolCall.raw);
    if (fromRaw !== "") {
      return fromRaw;
    }
    return t("chat.toolCallUnknown");
  }

  function formatToolResultOutput(toolResult?: AgentToolResultPayload): string {
    const summary = typeof toolResult?.summary === "string" ? toolResult.summary.trim() : "";
    if (summary !== "") {
      return summary;
    }
    return t("chat.toolCallOutputUnavailable");
  }

  function formatToolCallSummary(toolCall?: AgentToolCallPayload): string {
    const name = typeof toolCall?.name === "string" ? toolCall.name.trim() : "";
    if (name === "shell") {
      const command = summarizeShellCommandForNotice(extractShellCommand(toolCall?.input));
      return command === "" ? t("chat.toolCallShell") : t("chat.toolCallShellCommand", { command });
    }
    if (name === "view") {
      const filePath = extractToolFilePath(toolCall?.input);
      if (filePath !== "") {
        return t("chat.toolCallViewPath", { path: filePath });
      }
      return t("chat.toolCallView");
    }
    if (name === "edit" || name === "exit") {
      const filePath = extractToolFilePath(toolCall?.input);
      if (filePath !== "") {
        return t("chat.toolCallEditPath", { path: filePath });
      }
      return t("chat.toolCallEdit");
    }
    return t("chat.toolCallNotice", { target: name || "tool" });
  }

  function extractToolFilePath(input?: Record<string, unknown>): string {
    if (!input || typeof input !== "object") {
      return "";
    }
    const directPath = input.path;
    if (typeof directPath === "string" && directPath.trim() !== "") {
      return directPath.trim();
    }
    const nested = input.input;
    if (nested && typeof nested === "object" && !Array.isArray(nested)) {
      const nestedPath = extractToolFilePath(nested as Record<string, unknown>);
      if (nestedPath !== "") {
        return nestedPath;
      }
    }
    const items = input.items;
    if (!Array.isArray(items)) {
      return "";
    }
    for (const item of items) {
      if (!item || typeof item !== "object" || Array.isArray(item)) {
        continue;
      }
      const path = (item as { path?: unknown }).path;
      if (typeof path === "string" && path.trim() !== "") {
        return path.trim();
      }
    }
    return "";
  }

  function extractShellCommand(input?: Record<string, unknown>): string {
    if (!input || typeof input !== "object") {
      return "";
    }
    const direct = input.command;
    if (typeof direct === "string" && direct.trim() !== "") {
      return direct.trim();
    }
    const items = input.items;
    if (!Array.isArray(items) || items.length === 0) {
      return "";
    }
    const first = items[0];
    if (!first || typeof first !== "object" || Array.isArray(first)) {
      return "";
    }
    const command = (first as { command?: unknown }).command;
    if (typeof command !== "string") {
      return "";
    }
    return command.trim();
  }

  function summarizeShellCommandForNotice(command: string): string {
    const normalized = command.trim();
    if (normalized === "") {
      return "";
    }
    if (normalized.split(/\r?\n/).length > 1) {
      return t("common.ellipsis");
    }
    return normalized;
  }

  return {
    isToolCallRawNotice,
    normalizeToolName,
    parseToolNameFromToolCallRaw,
    resolveToolCallSummaryLine,
    resolveToolCallExpandedDetail,
    formatToolResultOutput,
    formatToolCallSummary,
  };
}

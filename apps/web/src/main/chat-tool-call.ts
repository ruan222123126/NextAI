import type {
  AgentStreamEvent,
  AgentToolCallPayload,
  AgentToolResultPayload,
  ChatToolCallContext,
  ViewToolCallNotice,
} from "./types.js";

export function createChatToolCallHelpers(ctx: ChatToolCallContext) {
  const { t } = ctx;
  const toolNameAliases: Record<string, string> = {
    read: "view",
    notebookread: "view",
    edit: "edit",
    write: "edit",
    multiedit: "edit",
    notebookedit: "edit",
    ls: "find",
    grep: "find",
    glob: "find",
    bash: "shell",
    websearch: "search",
    webfetch: "browser",
    "functions.spawn_agent": "spawn_agent",
    "functions.send_input": "send_input",
    "functions.resume_agent": "resume_agent",
    "functions.wait": "wait",
    "functions.close_agent": "close_agent",
    "functions.request_user_input": "request_user_input",
    "functions.update_plan": "update_plan",
    "functions.apply_patch": "apply_patch",
  };
  const maxSummaryLineRangeCount = 3;

  function isToolCallRawNotice(raw: string): boolean {
    const payload = parseToolCallRawEvent(raw);
    return payload?.type === "tool_call";
  }

  function normalizeToolName(value: unknown): string {
    if (typeof value !== "string") {
      return "";
    }
    const normalized = value.trim().toLowerCase();
    if (normalized === "") {
      return "";
    }
    return toolNameAliases[normalized] ?? normalized;
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
        const toolName = normalizeToolName(payload.tool_result?.name);
        const summary = typeof payload.tool_result?.summary === "string" ? payload.tool_result.summary.trim() : "";
        if (summary !== "") {
          const actionSummary = summarizeToolResultActions(summary);
          if (actionSummary !== "") {
            return actionSummary;
          }
          if (toolName === "view") {
            return fallback;
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
    const toolName = normalizeToolName(toolCall.toolName) || parseToolNameFromToolCallRaw(raw);
    if (toolName === "view") {
      return fallback;
    }
    return truncateToolCallSummary(firstNonEmptyLine(raw) || fallback);
  }

  function resolveToolCallSummaryMeta(toolCall: ViewToolCallNotice): string {
    if (toolCall.outputReady === false) {
      return "";
    }
    const summaryText = extractToolCallSummaryText(toolCall.raw);
    if (summaryText === "") {
      return "";
    }
    return summarizeBrowseLineRanges(summaryText);
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
    const lines = parseSummaryLines(text);
    if (lines.length === 0) {
      return "";
    }
    const editedPaths: string[] = [];
    const writtenPaths: string[] = [];
    const browseStats = collectBrowseSummaryStats(lines);
    for (const line of lines) {
      const editResult = parseEditResultLine(line);
      if (editResult) {
        const isWriteAction = isWriteResult(editResult);
        if (isWriteAction) {
          pushUniquePath(writtenPaths, editResult.path);
        } else {
          pushUniquePath(editedPaths, editResult.path);
        }
      }
    }
    const { fileCount, listCount } = browseStats;
    if (writtenPaths.length === 1 && editedPaths.length === 0 && fileCount === 0 && listCount === 0) {
      return t("chat.toolCallWritePath", { path: writtenPaths[0] });
    }
    if (writtenPaths.length > 1 && editedPaths.length === 0 && fileCount === 0 && listCount === 0) {
      return `${t("chat.toolCallWrite")} ${t("chat.toolCallSummaryFileCount", { count: writtenPaths.length })}`;
    }
    if (editedPaths.length === 1 && writtenPaths.length === 0 && fileCount === 0 && listCount === 0) {
      return t("chat.toolCallEditPath", { path: editedPaths[0] });
    }
    if (editedPaths.length > 1 && writtenPaths.length === 0 && fileCount === 0 && listCount === 0) {
      return `${t("chat.toolCallEdit")} ${t("chat.toolCallSummaryFileCount", { count: editedPaths.length })}`;
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

  function parseSummaryLines(text: string): string[] {
    return text
      .split(/(?:\r?\n|\\n)+/g)
      .map((line) => line.trim())
      .filter((line) => line !== "");
  }

  function collectBrowseSummaryStats(lines: string[]): {
    fileCount: number;
    listCount: number;
    fileLineRanges: string[];
  } {
    let fileCount = 0;
    let listCount = 0;
    const fileLineRanges: string[] = [];
    for (const line of lines) {
      if (isListBrowseLine(line)) {
        listCount += 1;
        continue;
      }
      if (!isFileBrowseLine(line)) {
        continue;
      }
      fileCount += 1;
      const range = parseBrowseLineRange(line);
      if (range !== "") {
        pushUniquePath(fileLineRanges, range);
      }
    }
    return {
      fileCount,
      listCount,
      fileLineRanges,
    };
  }

  function summarizeBrowseLineRanges(text: string): string {
    const lines = parseSummaryLines(text);
    if (lines.length === 0) {
      return "";
    }
    const { fileCount, fileLineRanges } = collectBrowseSummaryStats(lines);
    if (fileCount === 0 || fileLineRanges.length === 0) {
      return "";
    }
    const displayRanges = fileLineRanges.slice(0, maxSummaryLineRangeCount).join(", ");
    const rangeText = fileLineRanges.length > maxSummaryLineRangeCount
      ? `${displayRanges} ${t("common.ellipsis")}`
      : displayRanges;
    return t("chat.toolCallSummaryLineRange", { range: rangeText });
  }

  function parseBrowseLineRange(line: string): string {
    const match = line.match(/\[(\d+)-(\d+)\]/);
    if (!match) {
      return "";
    }
    const start = Number.parseInt(match[1] ?? "", 10);
    const end = Number.parseInt(match[2] ?? "", 10);
    if (!Number.isFinite(start) || !Number.isFinite(end)) {
      return "";
    }
    return `${start}-${end}`;
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

  function parseEditResultLine(line: string): {
    operation: string;
    path: string;
    startLine: number;
    endLine: number;
    replacedLineCount: number;
  } | null {
    const match = line.match(/^(edit|write|multiedit|notebookedit)\s+(.+)\s+\[(\d+)-(\d+)\]\s+replaced\s+(\d+)\s+line\(s\)\.?$/i);
    if (!match) {
      return null;
    }
    const path = match[2]?.trim() ?? "";
    const startLine = Number.parseInt(match[3] ?? "", 10);
    const endLine = Number.parseInt(match[4] ?? "", 10);
    const replacedLineCount = Number.parseInt(match[5] ?? "", 10);
    if (
      path === ""
      || !Number.isFinite(startLine)
      || !Number.isFinite(endLine)
      || !Number.isFinite(replacedLineCount)
    ) {
      return null;
    }
    return {
      operation: (match[1] ?? "").toLowerCase(),
      path,
      startLine,
      endLine,
      replacedLineCount,
    };
  }

  function isWriteResult(result: {
    operation: string;
    startLine: number;
    endLine: number;
    replacedLineCount: number;
  }): boolean {
    if (result.operation === "write") {
      return true;
    }
    return result.operation === "edit" && result.startLine === 1 && result.endLine === 1 && result.replacedLineCount === 0;
  }

  function pushUniquePath(paths: string[], path: string): void {
    if (!paths.includes(path)) {
      paths.push(path);
    }
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

  function extractToolCallSummaryText(raw: string): string {
    const normalized = raw.trim();
    if (
      normalized === ""
      || normalized === t("chat.toolCallOutputPending")
      || normalized === t("chat.toolCallOutputUnavailable")
    ) {
      return "";
    }
    const payload = parseToolCallRawEvent(normalized);
    if (!payload) {
      return normalized;
    }
    if (payload.type !== "tool_result") {
      return "";
    }
    if (typeof payload.tool_result?.summary !== "string") {
      return "";
    }
    return payload.tool_result.summary.trim();
  }

  function formatToolCallSummary(toolCall?: AgentToolCallPayload): string {
    const name = normalizeToolName(toolCall?.name);
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
    const directFilePath = input.file_path;
    if (typeof directFilePath === "string" && directFilePath.trim() !== "") {
      return directFilePath.trim();
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
    resolveToolCallSummaryMeta,
    resolveToolCallExpandedDetail,
    formatToolResultOutput,
    formatToolCallSummary,
  };
}

// @ts-nocheck
import { createChatToolCallHelpers } from "./chat-tool-call.js";

type PromptMode = "default" | "codex" | "claude";
type I18nKey = string;

interface ChatSpec {
  id: string;
  name: string;
  session_id: string;
  user_id: string;
  channel: string;
  updated_at: string;
  meta?: Record<string, unknown>;
}

interface RuntimeContent {
  type?: string;
  text?: string;
}

interface RuntimeMessage {
  id?: string;
  role?: string;
  content?: RuntimeContent[];
  metadata?: Record<string, unknown>;
}

interface ChatHistoryResponse {
  messages: RuntimeMessage[];
}

interface DeleteResult {
  deleted: boolean;
}

interface WorkspaceUploadResponse {
  uploaded?: boolean;
  path?: string;
  name?: string;
  size?: number;
}

interface ViewMessage {
  id: string;
  role: "user" | "assistant";
  text: string;
  toolCalls: ViewToolCallNotice[];
  textOrder?: number;
  toolOrder?: number;
  timeline: ViewMessageTimelineEntry[];
}

interface ViewToolCallNotice {
  summary: string;
  raw: string;
  step?: number;
  toolName?: string;
  outputReady?: boolean;
  order?: number;
}

interface ViewMessageTimelineEntry {
  type: "text" | "tool_call";
  order: number;
  text?: string;
  toolCall?: ViewToolCallNotice;
}

interface AgentToolCallPayload {
  name?: string;
  input?: Record<string, unknown>;
}

interface AgentToolResultPayload {
  name?: string;
  output?: string;
  input?: Record<string, unknown>;
}

interface AgentStreamEvent {
  type?: string;
  step?: number;
  delta?: string;
  raw?: string;
  tool_call?: AgentToolCallPayload;
  tool_result?: AgentToolResultPayload;
  meta?: {
    code?: string;
    message?: string;
  };
}

export function createChatDomain(ctx: any) {
  const {
    state,
    t,
    setStatus,
    asErrorMessage,
    requestJSON,
    toAbsoluteURL,
    applyTransportDefaultHeaders,
    parseErrorMessage,
    logAgentRawRequest,
    logAgentRawResponse,
    toViewMessage,
    compactTime,
    renderMarkdownToFragment,
    requestWorkspaceFile,
    extractWorkspaceFileText,
    renderComposerTokenEstimate,
    syncControlState,
    appendEmptyItem,
    newSessionID,
    setSearchModalOpen,
    getBootstrapTask,
    hideComposerSlashPanel,
    renderComposerSlashPanel,
    parsePositiveInteger,
    resetComposerFileDragDepth,
    runtimeFlags,
    WEB_CHAT_CHANNEL,
    QQ_CHANNEL,
    TRASH_ICON_SVG,
    PROMPT_TEMPLATE_PREFIX,
    PROMPT_TEMPLATE_NAME_PATTERN,
    PROMPT_TEMPLATE_ARG_KEY_PATTERN,
    PROMPT_TEMPLATE_PLACEHOLDER_PATTERN,
    PROMPT_MODE_META_KEY,
    chatList,
    chatTitle,
    chatSession,
    chatPromptModeSelect,
    searchChatInput,
    searchChatResults,
    messageList,
    thinkingIndicator,
    composerMain,
    messageInput,
    sendButton,
    composerAttachButton,
    composerAttachInput,
  } = ctx;

  let openChatRequestSerial = 0;
  let chatListDigest = "";
  let searchResultsDigest = "";
  let messagesDigest = "";
  let activeStreamAbortController: AbortController | null = null;
  const {
    isToolCallRawNotice,
    normalizeToolName,
    parseToolNameFromToolCallRaw,
    resolveToolCallSummaryLine,
    resolveToolCallExpandedDetail,
    formatToolResultOutput,
    formatToolCallSummary,
  } = createChatToolCallHelpers({ t });

  function computeChatListDigest(chats: ChatSpec[]): string {
    return chats.map((chat) => `${chat.id}:${chat.updated_at}:${chat.name}`).join("|");
  }

  function computeSearchResultsDigest(chats: ChatSpec[], query: string): string {
    return `${query.trim().toLowerCase()}::${computeChatListDigest(chats)}`;
  }

  function collectRenderableMessages(messages: ViewMessage[]): ViewMessage[] {
    return messages.filter((message) => !(message.role === "assistant" && buildOrderedTimeline(message).length === 0));
  }

  function computeMessagesDigest(messages: ViewMessage[]): string {
    return collectRenderableMessages(messages).map((message) => {
      const toolDigest = (message.toolCalls ?? [])
        .map((toolCall) => `${toolCall.order ?? 0}:${toolCall.step ?? 0}:${toolCall.summary}:${toolCall.raw}`)
        .join(";");
      const timelineDigest = (message.timeline ?? [])
        .map((entry) => `${entry.type}:${entry.order}:${entry.text ?? ""}:${entry.toolCall?.summary ?? ""}:${entry.toolCall?.raw ?? ""}`)
        .join(";");
      return `${message.id}:${message.role}:${message.text}:${toolDigest}:${timelineDigest}`;
    }).join("|");
  }

async function reloadChats(options: { includeQQHistory?: boolean } = {}): Promise<void> {
  try {
    const includeQQHistory = options.includeQQHistory ?? true;
    const query = new URLSearchParams({
      channel: WEB_CHAT_CHANNEL,
      user_id: state.userId,
    });
    const chatsRequests: Array<Promise<ChatSpec[]>> = [requestJSON<ChatSpec[]>(`/chats?${query.toString()}`)];
    if (includeQQHistory) {
      const qqQuery = new URLSearchParams({ channel: QQ_CHANNEL });
      chatsRequests.push(requestJSON<ChatSpec[]>(`/chats?${qqQuery.toString()}`));
    }
    const chatsGroups = await Promise.all(chatsRequests);
    const chatsByID = new Map<string, ChatSpec>();
    chatsGroups.flat().forEach((chat) => {
      chatsByID.set(chat.id, chat);
    });
    const nextChats = Array.from(chatsByID.values());
    nextChats.sort((a, b) => {
      const ta = Date.parse(a.updated_at);
      const tb = Date.parse(b.updated_at);
      const va = Number.isFinite(ta) ? ta : 0;
      const vb = Number.isFinite(tb) ? tb : 0;
      if (vb !== va) {
        return vb - va;
      }
      return a.id.localeCompare(b.id);
    });
    const prevDigest = state.chats.map((chat) => `${chat.id}:${chat.updated_at}`).join("|");
    const nextDigest = nextChats.map((chat) => `${chat.id}:${chat.updated_at}`).join("|");
    const chatsChanged = prevDigest !== nextDigest;
    state.chats = nextChats;

    let activeChatCleared = false;
    if (state.activeChatId && !state.chats.some((chat) => chat.id === state.activeChatId)) {
      state.activeChatId = null;
      state.activePromptMode = "default";
      activeChatCleared = true;
    }

    if (!chatsChanged && !activeChatCleared) {
      return;
    }
    renderChatList();
    renderSearchChatResults();
    renderChatHeader();
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

async function openChat(chatID: string): Promise<void> {
  const chat = state.chats.find((item) => item.id === chatID);
  if (!chat) {
    setStatus(t("status.chatNotFound", { chatId: chatID }), "error");
    return;
  }

  const requestSerial = ++openChatRequestSerial;
  state.activeChatId = chat.id;
  state.activeSessionId = chat.session_id;
  state.activePromptMode = resolveChatPromptMode(chat.meta);
  renderChatHeader();
  syncActiveChatSelections();

  try {
    const history = await requestJSON<ChatHistoryResponse>(`/chats/${encodeURIComponent(chat.id)}`);
    if (requestSerial !== openChatRequestSerial || state.activeChatId !== chat.id) {
      return;
    }
    state.messages = history.messages.map(toViewMessage);
    renderMessages({ animate: false });
    setStatus(t("status.loadedMessages", { count: history.messages.length }), "info");
  } catch (error) {
    if (requestSerial !== openChatRequestSerial || state.activeChatId !== chat.id) {
      return;
    }
    setStatus(asErrorMessage(error), "error");
  } finally {
    if (requestSerial === openChatRequestSerial && state.activeChatId === chat.id) {
      renderComposerTokenEstimate();
    }
  }
}

async function deleteChat(chatID: string): Promise<void> {
  const chat = state.chats.find((item) => item.id === chatID);
  if (!chat) {
    setStatus(t("status.chatNotFound", { chatId: chatID }), "error");
    return;
  }

  const confirmed = window.confirm(
    t("chat.deleteConfirm", {
      sessionId: chat.session_id,
    }),
  );
  if (!confirmed) {
    return;
  }

  const wasActive = state.activeChatId === chatID;
  try {
    await requestJSON<DeleteResult>(`/chats/${encodeURIComponent(chatID)}`, {
      method: "DELETE",
    });
    await reloadChats();
    if (wasActive) {
      if (state.chats.length > 0) {
        await openChat(state.chats[0].id);
      } else {
        startDraftSession();
      }
    } else {
      renderChatList();
      renderSearchChatResults();
      renderChatHeader();
    }
    setStatus(
      t("status.chatDeleted", {
        sessionId: chat.session_id,
      }),
      "info",
    );
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function startDraftSession(): void {
  openChatRequestSerial += 1;
  state.activeChatId = null;
  state.activeSessionId = newSessionID();
  state.activePromptMode = "default";
  state.messages = [];
  renderChatHeader();
  renderChatList();
  renderSearchChatResults();
  renderMessages();
  renderComposerTokenEstimate();
}

interface PromptTemplateCommand {
  templateName: string;
  args: Map<string, string>;
}

function parsePromptTemplateCommand(inputText: string): PromptTemplateCommand | null {
  const trimmed = inputText.trim();
  if (!trimmed.startsWith(PROMPT_TEMPLATE_PREFIX)) {
    return null;
  }
  const segments = trimmed.split(/\s+/).filter((segment) => segment !== "");
  const command = segments[0] ?? "";
  const templateName = command.slice(PROMPT_TEMPLATE_PREFIX.length).trim();
  if (templateName === "") {
    throw new Error("prompt template name is required");
  }
  if (!PROMPT_TEMPLATE_NAME_PATTERN.test(templateName)) {
    throw new Error(`invalid prompt template name: ${templateName}`);
  }

  const args = new Map<string, string>();
  for (const segment of segments.slice(1)) {
    const sepIndex = segment.indexOf("=");
    if (sepIndex <= 0) {
      throw new Error(`invalid prompt argument: ${segment} (expected KEY=VALUE)`);
    }
    const key = segment.slice(0, sepIndex).trim();
    const value = segment.slice(sepIndex + 1);
    if (!PROMPT_TEMPLATE_ARG_KEY_PATTERN.test(key)) {
      throw new Error(`invalid prompt argument key: ${key}`);
    }
    args.set(key, value);
  }
  return { templateName, args };
}

async function loadPromptTemplateContent(templateName: string): Promise<string> {
  const candidates = [
    `prompts/${templateName}.md`,
    `prompt/${templateName}.md`,
    `prompts/codex/user-codex/prompts/${templateName}.md`,
  ];
  let lastError: unknown = null;
  for (const path of candidates) {
    try {
      const payload = await requestWorkspaceFile(path);
      const content = extractWorkspaceFileText(payload);
      if (content.trim() === "") {
        throw new Error(`prompt template is empty: ${path}`);
      }
      return content;
    } catch (error) {
      lastError = error;
    }
  }
  throw new Error(`prompt template not found: ${templateName} (${asErrorMessage(lastError)})`);
}

function applyPromptTemplateArgs(templateContent: string, args: Map<string, string>): string {
  if (/\$[1-9]\b/.test(templateContent) || /\$ARGUMENTS\b/.test(templateContent)) {
    throw new Error("positional prompt arguments are not supported yet");
  }
  const placeholderRegex = /\$([A-Za-z_][A-Za-z0-9_]*)/g;
  const requiredKeys = new Set<string>();
  for (const match of templateContent.matchAll(placeholderRegex)) {
    const key = match[1];
    if (key) {
      requiredKeys.add(key);
    }
  }
  const missingKeys = Array.from(requiredKeys).filter((key) => !args.has(key));
  if (missingKeys.length > 0) {
    throw new Error(`missing prompt arguments: ${missingKeys.join(", ")}`);
  }

  return templateContent.replace(PROMPT_TEMPLATE_PLACEHOLDER_PATTERN, (_match, key: string) => args.get(key) ?? "");
}

async function expandPromptTemplateIfNeeded(inputText: string): Promise<string> {
  if (!runtimeFlags.prompt_templates) {
    return inputText;
  }
  const parsed = parsePromptTemplateCommand(inputText);
  if (!parsed) {
    return inputText;
  }
  const templateContent = await loadPromptTemplateContent(parsed.templateName);
  return applyPromptTemplateArgs(templateContent, parsed.args);
}

function setThinkingIndicatorVisible(visible: boolean): void {
  thinkingIndicator.hidden = !visible;
  thinkingIndicator.classList.toggle("is-visible", visible);
  thinkingIndicator.setAttribute("aria-hidden", visible ? "false" : "true");
  if (!visible) {
    if (thinkingIndicator.parentElement) {
      thinkingIndicator.remove();
    }
    return;
  }
  syncThinkingIndicatorPosition();
  messageList.scrollTop = messageList.scrollHeight;
}

function syncThinkingIndicatorPosition(): void {
  if (thinkingIndicator.hidden) {
    return;
  }
  if (thinkingIndicator.parentElement !== messageList || messageList.lastElementChild !== thinkingIndicator) {
    messageList.appendChild(thinkingIndicator);
  }
}

function waitForNextPaint(): Promise<void> {
  return new Promise((resolve) => {
    if (typeof window.requestAnimationFrame === "function") {
      window.requestAnimationFrame(() => resolve());
      return;
    }
    window.setTimeout(resolve, 0);
  });
}

function isAbortError(error: unknown): boolean {
  if (error instanceof DOMException) {
    return error.name === "AbortError";
  }
  if (!error || typeof error !== "object") {
    return false;
  }
  return (error as { name?: unknown }).name === "AbortError";
}

function syncSendButtonState(): void {
  const isSending = state.sending;
  sendButton.classList.toggle("is-sending", isSending);
  const ariaLabelKey: I18nKey = isSending ? "chat.pauseAria" : "chat.sendAria";
  const ariaLabel = t(ariaLabelKey);
  sendButton.setAttribute("aria-label", ariaLabel);
  sendButton.title = ariaLabel;
}

function pauseReply(): void {
  if (!state.sending || !activeStreamAbortController) {
    return;
  }
  activeStreamAbortController.abort();
}

async function sendMessage(): Promise<void> {
  await getBootstrapTask();
  syncControlState();
  if (state.sending) {
    return;
  }

  const draftText = messageInput.value.trim();
  if (draftText === "") {
    setStatus(t("status.inputRequired"), "error");
    return;
  }

  if (state.apiBase === "") {
    setStatus(t("status.controlsRequired"), "error");
    return;
  }
  hideComposerSlashPanel();

  let inputText = draftText;
  try {
    inputText = await expandPromptTemplateIfNeeded(draftText);
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return;
  }

  let bizParams: Record<string, unknown> | undefined;
  try {
    bizParams = parseChatBizParams(inputText);
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return;
  }

  state.sending = true;
  syncSendButtonState();
  const streamAbortController = new AbortController();
  activeStreamAbortController = streamAbortController;
  const assistantID = `assistant-${Date.now()}`;

  state.messages = state.messages.concat(
    {
      id: `user-${Date.now()}`,
      role: "user",
      text: inputText,
      toolCalls: [],
      textOrder: nextMessageOutputOrder(),
      timeline: [],
    },
    {
      id: assistantID,
      role: "assistant",
      text: "",
      toolCalls: [],
      timeline: [],
    },
  );
  const latestUserMessage = state.messages[state.messages.length - 2];
  if (latestUserMessage && latestUserMessage.role === "user" && latestUserMessage.textOrder !== undefined) {
    latestUserMessage.timeline = [
      {
        type: "text",
        order: latestUserMessage.textOrder,
        text: latestUserMessage.text,
      },
    ];
  }
  renderMessages();
  messageInput.value = "";
  renderComposerTokenEstimate();
  setThinkingIndicatorVisible(true);
  await waitForNextPaint();
  setStatus(t("status.streamingReply"), "info");

  try {
    try {
      await streamReply(
        inputText,
        bizParams,
        (delta) => {
          const target = state.messages.find((item) => item.id === assistantID);
          if (!target) {
            return;
          }
          if (delta.trim() !== "") {
            setThinkingIndicatorVisible(false);
          }
          appendAssistantDelta(target, delta);
          renderMessageInPlace(assistantID);
        },
        (event) => {
          handleToolCallEvent(event, assistantID);
        },
        streamAbortController.signal,
      );
    } finally {
      setThinkingIndicatorVisible(false);
    }
    setStatus(t("status.replyCompleted"), "info");

    await reloadChats();
    const matched = state.chats.find(
      (chat) =>
        chat.session_id === state.activeSessionId &&
        chat.channel === WEB_CHAT_CHANNEL &&
        chat.user_id === state.userId,
    );
    if (matched) {
      state.activeChatId = matched.id;
      state.activeSessionId = matched.session_id;
      renderChatHeader();
      renderChatList();
      renderSearchChatResults();
    }
  } catch (error) {
    if (isAbortError(error)) {
      setStatus(t("status.replyPaused"), "info");
    } else {
      const message = asErrorMessage(error);
      fillAssistantErrorMessageIfPending(assistantID, message);
      setStatus(message, "error");
    }
  } finally {
    setThinkingIndicatorVisible(false);
    if (activeStreamAbortController === streamAbortController) {
      activeStreamAbortController = null;
    }
    state.sending = false;
    syncSendButtonState();
    renderComposerTokenEstimate();
  }
}

function isFileDragEvent(event: DragEvent): boolean {
  const types = event.dataTransfer?.types;
  if (!types) {
    return false;
  }
  return Array.from(types).includes("Files");
}

function clearComposerFileDragState(): void {
  resetComposerFileDragDepth();
  composerMain.classList.remove("is-file-drag-over");
}

async function handleComposerAttachmentFiles(files: FileList | null, droppedFilePaths: string[] = []): Promise<void> {
  if (!files || files.length === 0) {
    return;
  }
  try {
    const uploadedPaths = await uploadComposerAttachmentPaths(files, droppedFilePaths);
    const mentionCount = appendComposerAttachmentMentions(uploadedPaths);
    if (mentionCount === 0) {
      throw new Error("uploaded attachment path is empty");
    }
    setStatus(
      t("status.composerAttachmentsAdded", {
        count: mentionCount,
      }),
      "info",
    );
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

async function uploadComposerAttachmentPaths(files: FileList, droppedFilePaths: string[]): Promise<string[]> {
  const mentions: string[] = [];
  const rows = Array.from(files);
  for (let index = 0; index < rows.length; index += 1) {
    const file = rows[index];
    const sourcePath = normalizeAttachmentName(resolveAttachmentPathForMention(file, index, droppedFilePaths));
    const uploadedPath = await uploadWorkspaceAttachment(file, sourcePath);
    const normalizedUploadedPath = normalizeAttachmentName(uploadedPath);
    if (normalizedUploadedPath === "") {
      continue;
    }
    mentions.push(normalizedUploadedPath);
  }
  return mentions;
}

async function uploadWorkspaceAttachment(file: File, sourcePath: string): Promise<string> {
  const formData = new FormData();
  const fileName = normalizeAttachmentName(file.name) || "upload.bin";
  formData.append("file", file, fileName);
  if (sourcePath !== "") {
    formData.append("source_path", sourcePath);
  }
  const payload = await requestJSON<WorkspaceUploadResponse>("/workspace/uploads", {
    method: "POST",
    body: formData,
  });
  const uploadedPath = typeof payload.path === "string" ? normalizeAttachmentName(payload.path) : "";
  if (uploadedPath === "") {
    throw new Error("workspace upload response missing file path");
  }
  return uploadedPath;
}

function appendComposerAttachmentMentions(paths: string[]): number {
  if (paths.length === 0) {
    return 0;
  }
  const mentions: string[] = [];
  for (const path of paths) {
    const normalizedName = normalizeAttachmentName(path);
    if (normalizedName === "") {
      continue;
    }
    mentions.push(`@${normalizedName}`);
  }
  if (mentions.length === 0) {
    return 0;
  }
  const mentionLine = mentions.join(" ");
  const existing = messageInput.value.trimEnd();
  messageInput.value = existing === "" ? mentionLine : `${existing}\n${mentionLine}`;
  messageInput.focus();
  const cursor = messageInput.value.length;
  messageInput.setSelectionRange(cursor, cursor);
  renderComposerTokenEstimate();
  renderComposerSlashPanel();
  return mentions.length;
}

function normalizeAttachmentName(raw: string): string {
  return raw.replace(/[\r\n\t]+/g, " ").trim();
}

function resolveAttachmentPathForMention(file: File, index: number, droppedFilePaths: string[]): string {
  const fromFileObject = extractFilePathFromFileObject(file);
  if (fromFileObject !== "") {
    return fromFileObject;
  }
  const fromDropData = droppedFilePaths[index] ?? "";
  if (fromDropData !== "") {
    return fromDropData;
  }
  return file.name;
}

function extractFilePathFromFileObject(file: File): string {
  const withPath = file as File & { path?: unknown };
  if (typeof withPath.path === "string") {
    const normalized = normalizeAttachmentName(withPath.path);
    if (normalized !== "") {
      return normalized;
    }
  }
  return "";
}

function extractDroppedFilePaths(dataTransfer: DataTransfer | null): string[] {
  if (!dataTransfer) {
    return [];
  }
  const uriListPaths = parseDroppedPathsFromRawText(dataTransfer.getData("text/uri-list"));
  if (uriListPaths.length > 0) {
    return uriListPaths;
  }
  return parseDroppedPathsFromRawText(dataTransfer.getData("text/plain"));
}

function parseDroppedPathsFromRawText(raw: string): string[] {
  const normalizedRaw = raw.trim();
  if (normalizedRaw === "") {
    return [];
  }
  const paths: string[] = [];
  const lines = normalizedRaw.split(/\r?\n/);
  for (const line of lines) {
    const value = line.trim();
    if (value === "" || value.startsWith("#")) {
      continue;
    }
    const localPath = parseDroppedLocalPath(value);
    if (localPath === "") {
      continue;
    }
    paths.push(localPath);
  }
  return paths;
}

function parseDroppedLocalPath(raw: string): string {
  const directPath = normalizePossibleLocalPath(raw);
  if (directPath !== "") {
    return directPath;
  }
  try {
    const parsed = new URL(raw);
    if (parsed.protocol !== "file:") {
      return "";
    }
    const pathname = decodeURIComponent(parsed.pathname ?? "");
    if (pathname === "") {
      return "";
    }
    let resolved = pathname;
    if (/^\/[A-Za-z]:[\\/]/.test(resolved)) {
      resolved = resolved.slice(1);
    }
    if (parsed.hostname && parsed.hostname.toLowerCase() !== "localhost") {
      resolved = `//${parsed.hostname}${resolved}`;
    }
    return normalizeAttachmentName(resolved);
  } catch {
    return "";
  }
}

function normalizePossibleLocalPath(raw: string): string {
  if (raw.startsWith("/") || raw.startsWith("\\\\") || /^[A-Za-z]:[\\/]/.test(raw)) {
    return normalizeAttachmentName(raw);
  }
  return "";
}

async function streamReply(
  userText: string,
  bizParams: Record<string, unknown> | undefined,
  onDelta: (delta: string) => void,
  onEvent?: (event: AgentStreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const payload: Record<string, unknown> = {
    input: [{ role: "user", type: "message", content: [{ type: "text", text: userText }] }],
    session_id: state.activeSessionId,
    user_id: state.userId,
    channel: WEB_CHAT_CHANNEL,
    stream: true,
  };
  payload.biz_params = mergePromptModeBizParams(bizParams, state.activePromptMode);

  const headers = new Headers({
    "content-type": "application/json",
    accept: "text/event-stream,application/json",
  });
  applyTransportDefaultHeaders(headers);
  const requestBody = JSON.stringify(payload);
  logAgentRawRequest(requestBody);

  const response = await fetch(toAbsoluteURL("/agent/process"), {
    method: "POST",
    headers,
    body: requestBody,
    signal,
  });

  if (!response.ok) {
    const fallback = t("error.requestFailed", { status: response.status });
    const raw = await response.text();
    logAgentRawResponse(raw);
    throw new Error(parseErrorMessage(raw, response.status, fallback));
  }
  if (!response.body) {
    throw new Error(t("error.streamUnsupported"));
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let rawOutput = "";
  let doneReceived = false;
  try {
    while (!doneReceived) {
      const chunk = await reader.read();
      if (chunk.done) {
        break;
      }
      const decodedChunk = decoder.decode(chunk.value, { stream: true });
      rawOutput += decodedChunk;
      buffer += decodedChunk.replaceAll("\r", "");
      const result = consumeSSEBuffer(buffer, onDelta, onEvent);
      buffer = result.rest;
      doneReceived = result.done;
    }

    const flushedChunk = decoder.decode();
    rawOutput += flushedChunk;
    buffer += flushedChunk.replaceAll("\r", "");
    if (!doneReceived && buffer.trim() !== "") {
      const result = consumeSSEBuffer(`${buffer}\n\n`, onDelta, onEvent);
      doneReceived = result.done;
    }

    if (!doneReceived) {
      throw new Error(t("error.sseEndedEarly"));
    }
  } finally {
    logAgentRawResponse(rawOutput);
  }
}

function parseChatBizParams(inputText: string): Record<string, unknown> | undefined {
  const trimmed = inputText.trim();
  if (!trimmed.startsWith("/shell")) {
    return undefined;
  }
  const command = trimmed.slice("/shell".length).trim();
  if (command === "") {
    throw new Error("shell command is required after /shell");
  }
  return {
    tool: {
      name: "shell",
      items: [{ command }],
    },
  };
}

function consumeSSEBuffer(
  raw: string,
  onDelta: (delta: string) => void,
  onEvent?: (event: AgentStreamEvent) => void,
): { done: boolean; rest: string } {
  let buffer = raw;
  let done = false;
  while (!done) {
    const boundary = buffer.indexOf("\n\n");
    if (boundary < 0) {
      break;
    }
    const block = buffer.slice(0, boundary);
    buffer = buffer.slice(boundary + 2);
    done = consumeSSEBlock(block, onDelta, onEvent) || done;
  }
  return { done, rest: buffer };
}

function consumeSSEBlock(block: string, onDelta: (delta: string) => void, onEvent?: (event: AgentStreamEvent) => void): boolean {
  if (block.trim() === "") {
    return false;
  }
  const dataLines: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).trimStart());
    }
  }
  if (dataLines.length === 0) {
    return false;
  }
  const data = dataLines.join("\n");
  if (data === "[DONE]") {
    return true;
  }
  let payload: AgentStreamEvent;
  try {
    payload = JSON.parse(data) as AgentStreamEvent;
  } catch {
    onDelta(data);
    return false;
  }
  payload.raw = data;
  if (payload.type === "error") {
    if (onEvent) {
      onEvent(payload);
    }
    const message = typeof payload.meta?.message === "string" ? payload.meta.message.trim() : "";
    const code = typeof payload.meta?.code === "string" ? payload.meta.code.trim() : "";
    if (code !== "" && message !== "") {
      throw new Error(`${code}: ${message}`);
    }
    if (message !== "") {
      throw new Error(message);
    }
    throw new Error(t("error.sseUnexpectedError"));
  }
  if (typeof payload.type === "string" && onEvent) {
    onEvent(payload);
  }
  if (typeof payload.delta === "string") {
    onDelta(payload.delta);
  }
  return false;
}

function renderChatList(options: { force?: boolean } = {}): void {
  const force = options.force === true;
  const nextDigest = computeChatListDigest(state.chats);
  if (!force && chatListDigest === nextDigest) {
    syncActiveChatSelections();
    return;
  }
  chatListDigest = nextDigest;
  chatList.innerHTML = "";
  if (state.chats.length === 0) {
    const li = document.createElement("li");
    li.className = "chat-empty-fill";
    li.setAttribute("aria-hidden", "true");
    chatList.appendChild(li);
    return;
  }

  state.chats.forEach((chat) => {
    const li = document.createElement("li");
    li.className = "chat-list-item";

    const actions = document.createElement("div");
    actions.className = "chat-item-actions";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "chat-item-btn";
    button.dataset.chatId = chat.id;
    if (chat.id === state.activeChatId) {
      button.classList.add("active");
    }
    button.addEventListener("click", () => {
      void openChat(chat.id);
    });

    const title = document.createElement("span");
    title.className = "chat-title";
    title.textContent = chat.name || t("chat.unnamed");

    const meta = document.createElement("span");
    meta.className = "chat-meta";
    meta.textContent = t("chat.meta", {
      updatedAt: compactTime(chat.updated_at),
    });

    button.append(title, meta);
    actions.appendChild(button);

    const deleteLabel = t("chat.delete");
    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.className = "chat-delete-btn";
    deleteButton.setAttribute("aria-label", deleteLabel);
    deleteButton.title = deleteLabel;
    deleteButton.innerHTML = TRASH_ICON_SVG;
    deleteButton.addEventListener("click", async (event) => {
      event.stopPropagation();
      await deleteChat(chat.id);
    });
    actions.appendChild(deleteButton);

    li.appendChild(actions);
    chatList.appendChild(li);
  });
  syncActiveChatSelections();
}

function renderSearchChatResults(options: { force?: boolean } = {}): void {
  const force = options.force === true;
  const nextDigest = computeSearchResultsDigest(state.chats, state.chatSearchQuery);
  if (!force && searchResultsDigest === nextDigest) {
    syncActiveChatSelections();
    return;
  }
  searchResultsDigest = nextDigest;
  searchChatResults.innerHTML = "";
  searchChatInput.value = state.chatSearchQuery;

  if (state.chats.length === 0) {
    appendEmptyItem(searchChatResults, t("search.emptyChats"));
    return;
  }

  const filteredChats = filterChatsForSearch(state.chatSearchQuery);
  if (filteredChats.length === 0) {
    appendEmptyItem(
      searchChatResults,
      t("search.noResults", {
        query: state.chatSearchQuery,
      }),
    );
    return;
  }

  filteredChats.forEach((chat) => {
    const li = document.createElement("li");
    li.className = "search-result-item";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "chat-item-btn search-result-btn";
    button.dataset.chatId = chat.id;
    if (chat.id === state.activeChatId) {
      button.classList.add("active");
    }
    button.addEventListener("click", async () => {
      await openChat(chat.id);
      setSearchModalOpen(false);
    });

    const title = document.createElement("span");
    title.className = "chat-title";
    title.textContent = chat.name || t("chat.unnamed");

    const meta = document.createElement("span");
    meta.className = "chat-meta";
    meta.textContent = t("search.meta", {
      sessionId: chat.session_id,
      channel: chat.channel,
      userId: chat.user_id,
      updatedAt: compactTime(chat.updated_at),
    });

    button.append(title, meta);
    li.appendChild(button);
    searchChatResults.appendChild(li);
  });
  syncActiveChatSelections();
}

function filterChatsForSearch(query: string): ChatSpec[] {
  const normalizedQuery = query.trim().toLowerCase();
  if (normalizedQuery === "") {
    return state.chats;
  }
  return state.chats.filter((chat) => buildChatSearchText(chat).includes(normalizedQuery));
}

function buildChatSearchText(chat: ChatSpec): string {
  return [chat.name, chat.session_id, chat.user_id, chat.channel, resolveChatCronJobID(chat.meta)].join(" ").toLowerCase();
}

function resolveChatCronJobID(meta: Record<string, unknown> | undefined): string {
  if (!meta) {
    return "";
  }
  const raw = meta.cron_job_id;
  if (typeof raw === "string") {
    return raw;
  }
  if (typeof raw === "number") {
    return String(raw);
  }
  return "";
}

function normalizePromptMode(raw: unknown): PromptMode {
  if (typeof raw !== "string") {
    return "default";
  }
  const normalized = raw.trim().toLowerCase();
  if (normalized === "codex") {
    return "codex";
  }
  if (normalized === "claude") {
    return "claude";
  }
  return "default";
}

function resolveChatPromptMode(meta: Record<string, unknown> | undefined): PromptMode {
  return normalizePromptMode(meta?.[PROMPT_MODE_META_KEY]);
}

function mergePromptModeBizParams(
  bizParams: Record<string, unknown> | undefined,
  promptMode: PromptMode,
): Record<string, unknown> {
  const merged: Record<string, unknown> = bizParams ? { ...bizParams } : {};
  merged[PROMPT_MODE_META_KEY] = promptMode;
  return merged;
}

function setActivePromptMode(nextMode: PromptMode, options: { announce?: boolean } = {}): void {
  const normalized = normalizePromptMode(nextMode);
  const changed = state.activePromptMode !== normalized;
  state.activePromptMode = normalized;
  const active = state.chats.find((chat) => chat.id === state.activeChatId);
  if (active) {
    if (!active.meta) {
      active.meta = {};
    }
    active.meta[PROMPT_MODE_META_KEY] = normalized;
  }
  renderChatHeader();
  if (options.announce && changed) {
    let statusKey: I18nKey = "status.promptModeDefaultEnabled";
    if (normalized === "codex") {
      statusKey = "status.promptModeCodexEnabled";
    } else if (normalized === "claude") {
      statusKey = "status.promptModeClaudeEnabled";
    }
    setStatus(t(statusKey), "info");
  }
  renderComposerTokenEstimate();
}

function renderChatHeader(): void {
  const active = state.chats.find((chat) => chat.id === state.activeChatId);
  if (active) {
    state.activePromptMode = resolveChatPromptMode(active.meta);
  }
  chatTitle.textContent = active ? active.name : t("chat.draftTitle");
  const sessionId = state.activeSessionId;
  chatSession.textContent = sessionId;
  chatSession.title = sessionId;
  chatPromptModeSelect.value = state.activePromptMode;
}

function syncActiveChatSelections(): void {
  const activeChatID = state.activeChatId ?? "";
  chatList.querySelectorAll<HTMLButtonElement>(".chat-item-btn[data-chat-id]").forEach((button) => {
    const chatID = button.dataset.chatId ?? "";
    button.classList.toggle("active", chatID !== "" && chatID === activeChatID);
  });
  searchChatResults.querySelectorAll<HTMLButtonElement>(".search-result-btn[data-chat-id]").forEach((button) => {
    const chatID = button.dataset.chatId ?? "";
    button.classList.toggle("active", chatID !== "" && chatID === activeChatID);
  });
}

function renderMessages(options: { animate?: boolean; force?: boolean } = {}): void {
  const animate = options.animate ?? true;
  const force = options.force === true;
  const nextDigest = computeMessagesDigest(state.messages);
  if (!force && messagesDigest === nextDigest) {
    syncThinkingIndicatorPosition();
    return;
  }
  messagesDigest = nextDigest;
  const renderableMessages = collectRenderableMessages(state.messages);
  messageList.innerHTML = "";
  if (renderableMessages.length === 0) {
    const empty = document.createElement("li");
    empty.className = "message-empty-fill";
    empty.setAttribute("aria-hidden", "true");
    messageList.appendChild(empty);
    syncThinkingIndicatorPosition();
    messageList.scrollTop = messageList.scrollHeight;
    return;
  }

  for (const message of renderableMessages) {
    const item = document.createElement("li");
    item.className = `message ${message.role}`;
    if (!animate) {
      item.classList.add("no-anim");
    }
    item.dataset.messageId = message.id;
    renderMessageNode(item, message);
    messageList.appendChild(item);
  }
  syncThinkingIndicatorPosition();
  messageList.scrollTop = messageList.scrollHeight;
}

function renderMessageInPlace(messageID: string): void {
  const target = state.messages.find((item) => item.id === messageID);
  if (!target) {
    return;
  }
  const items = Array.from(messageList.querySelectorAll<HTMLLIElement>(".message"));
  const node = items.find((item) => item.dataset.messageId === messageID);
  if (!node) {
    renderMessages({ force: true });
    return;
  }
  renderMessageNode(node, target);
  messagesDigest = computeMessagesDigest(state.messages);
  syncThinkingIndicatorPosition();
  messageList.scrollTop = messageList.scrollHeight;
}

function nextMessageOutputOrder(): number {
  state.messageOutputOrder += 1;
  return state.messageOutputOrder;
}

function handleToolCallEvent(event: AgentStreamEvent, assistantID: string): void {
  if (event.type === "tool_call") {
    const notice = formatToolCallNotice(event);
    if (!notice) {
      return;
    }
    appendToolCallNoticeToAssistant(assistantID, notice);
    return;
  }
  if (event.type === "tool_result") {
    applyToolResultEvent(event, assistantID);
  }
}

function appendToolCallNoticeToAssistant(assistantID: string, notice: ViewToolCallNotice): void {
  const target = state.messages.find((item) => item.id === assistantID);
  if (!target) {
    return;
  }
  if (notice.summary === "" || notice.raw === "") {
    return;
  }
  const order = nextMessageOutputOrder();
  const noticeWithOrder: ViewToolCallNotice = {
    ...notice,
    order,
  };
  if (target.toolOrder === undefined) {
    target.toolOrder = order;
  }
  target.toolCalls = target.toolCalls.concat(noticeWithOrder);
  target.timeline = target.timeline.concat({
    type: "tool_call",
    order,
    toolCall: noticeWithOrder,
  });
  renderMessageInPlace(assistantID);
}

function appendAssistantDelta(message: ViewMessage, delta: string): void {
  if (delta === "") {
    return;
  }
  if (message.textOrder === undefined) {
    message.textOrder = nextMessageOutputOrder();
  }
  message.text += delta;

  const timeline = message.timeline;
  const last = timeline[timeline.length - 1];
  if (last && last.type === "text") {
    last.text = `${last.text ?? ""}${delta}`;
    return;
  }
  const order = nextMessageOutputOrder();
  timeline.push({
    type: "text",
    order,
    text: delta,
  });
}

function fillAssistantErrorMessageIfPending(assistantID: string, rawMessage: string): void {
  const target = state.messages.find((item) => item.id === assistantID);
  if (!target || target.role !== "assistant" || target.text.trim() !== "") {
    return;
  }
  const message = rawMessage.trim();
  if (message === "") {
    return;
  }
  appendAssistantDelta(target, message);
  renderMessageInPlace(assistantID);
}

function formatToolCallNotice(event: AgentStreamEvent): ViewToolCallNotice | null {
  const raw = formatToolCallRaw(event);
  const toolName = normalizeToolName(event.tool_call?.name) || parseToolNameFromToolCallRaw(raw);
  const detail = formatToolCallDetail(raw, toolName);
  if (detail === "") {
    return null;
  }
  return {
    summary: formatToolCallSummary(event.tool_call),
    raw: detail,
    step: parsePositiveInteger(event.step),
    toolName: toolName === "" ? undefined : toolName,
    outputReady: toolName !== "shell",
  };
}

function formatToolCallDetail(raw: string, toolName: string): string {
  if (toolName === "shell") {
    return t("chat.toolCallOutputPending");
  }
  return raw;
}

function formatToolCallRaw(event: AgentStreamEvent): string {
  const raw = typeof event.raw === "string" ? event.raw.trim() : "";
  if (raw !== "") {
    return raw;
  }
  if (event.tool_call) {
    try {
      return JSON.stringify({
        type: "tool_call",
        step: event.step,
        tool_call: event.tool_call,
      });
    } catch {
      return "";
    }
  }
  return "";
}

function applyToolResultEvent(event: AgentStreamEvent, assistantID: string): void {
  const raw = typeof event.raw === "string" ? event.raw : "";
  const toolName = normalizeToolName(event.tool_result?.name) || parseToolNameFromToolCallRaw(raw);
  if (toolName === "") {
    return;
  }
  const output = formatToolResultOutput(event.tool_result);
  const target = state.messages.find((item) => item.id === assistantID);
  if (!target) {
    return;
  }
  const step = parsePositiveInteger(event.step);
  const notice = findPendingToolCallNotice(target.toolCalls, toolName, step);
  if (notice) {
    notice.raw = output;
    notice.outputReady = true;
    if (normalizeToolName(notice.toolName) === "") {
      notice.toolName = toolName;
    }
    renderMessageInPlace(assistantID);
    return;
  }
  appendToolCallNoticeToAssistant(assistantID, {
    summary: formatToolCallSummary({ name: toolName }),
    raw: output,
    step,
    toolName,
    outputReady: true,
  });
}

function findPendingToolCallNotice(
  notices: ViewToolCallNotice[],
  toolName: string,
  step?: number,
): ViewToolCallNotice | undefined {
  for (let idx = notices.length - 1; idx >= 0; idx -= 1) {
    const item = notices[idx];
    const itemToolName = normalizeToolName(item.toolName) || parseToolNameFromToolCallRaw(item.raw);
    const canConsumeResult = !item.outputReady || isToolCallRawNotice(item.raw);
    if (itemToolName !== toolName || !canConsumeResult) {
      continue;
    }
    if (step !== undefined && item.step !== undefined && item.step !== step) {
      continue;
    }
    return item;
  }
  return undefined;
}

function renderMessageNode(node: HTMLLIElement, message: ViewMessage): void {
  node.innerHTML = "";

  const orderedTimeline = buildOrderedTimeline(message);
  if (orderedTimeline.length === 0) {
    return;
  }

  for (const entry of orderedTimeline) {
    if (entry.type === "text") {
      const textValue = entry.text ?? "";
      if (textValue === "") {
        continue;
      }
      const text = document.createElement("div");
      text.className = "message-text";
      if (message.role === "assistant") {
        text.classList.add("message-markdown");
        text.appendChild(renderMarkdownToFragment(textValue, document));
      } else {
        text.textContent = textValue;
      }
      node.appendChild(text);
      continue;
    }

    const toolCall = entry.toolCall;
    if (!toolCall) {
      continue;
    }
    const toolCallList = document.createElement("div");
    toolCallList.className = "tool-call-list";

    const entryNode = document.createElement("div");
    entryNode.className = "tool-call-entry";

    const row = document.createElement("div");
    row.className = "tool-call-row";

    const summary = document.createElement("span");
    summary.className = "tool-call-summary";
    summary.textContent = resolveToolCallSummaryLine(toolCall);
    row.appendChild(summary);

    const detail = document.createElement("pre");
    detail.className = "tool-call-expand-preview";
    detail.textContent = resolveToolCallExpandedDetail(toolCall);
    detail.hidden = true;

    const toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = "tool-call-toggle";
    toggle.textContent = "▸";
    toggle.setAttribute("aria-label", "toggle tool output");
    toggle.setAttribute("aria-expanded", "false");
    toggle.addEventListener("click", () => {
      const expanded = toggle.getAttribute("aria-expanded") === "true";
      const nextExpanded = !expanded;
      toggle.setAttribute("aria-expanded", nextExpanded ? "true" : "false");
      detail.hidden = !nextExpanded;
      entryNode.classList.toggle("is-expanded", nextExpanded);
    });
    row.appendChild(toggle);

    entryNode.append(row, detail);
    toolCallList.appendChild(entryNode);
    node.appendChild(toolCallList);
  }
}

function buildOrderedTimeline(message: ViewMessage): ViewMessageTimelineEntry[] {
  const fromTimeline = normalizeTimeline(message.timeline);
  if (fromTimeline.length > 0) {
    return fromTimeline;
  }

  const fallback: ViewMessageTimelineEntry[] = [];
  if (message.text !== "") {
    fallback.push({
      type: "text",
      order: message.textOrder ?? Number.MAX_SAFE_INTEGER - 1,
      text: message.text,
    });
  }
  for (const toolCall of message.toolCalls) {
    fallback.push({
      type: "tool_call",
      order: toolCall.order ?? message.toolOrder ?? Number.MAX_SAFE_INTEGER,
      toolCall,
    });
  }
  return normalizeTimeline(fallback);
}

function normalizeTimeline(entries: ViewMessageTimelineEntry[]): ViewMessageTimelineEntry[] {
  const normalized = entries
    .filter((entry) => entry.order > 0)
    .slice()
    .sort((left, right) => left.order - right.order);
  if (normalized.length < 2) {
    return normalized;
  }

  const merged: ViewMessageTimelineEntry[] = [];
  for (const entry of normalized) {
    const last = merged[merged.length - 1];
    if (entry.type === "text" && last && last.type === "text") {
      last.text = `${last.text ?? ""}${entry.text ?? ""}`;
      continue;
    }
    merged.push({ ...entry });
  }
  return merged;
}


  return {
    reloadChats,
    openChat,
    deleteChat,
    startDraftSession,
    sendMessage,
    pauseReply,
    isFileDragEvent,
    clearComposerFileDragState,
    handleComposerAttachmentFiles,
    extractDroppedFilePaths,
    normalizePromptMode,
    setActivePromptMode,
    renderChatHeader,
    renderChatList,
    renderSearchChatResults,
    renderMessages,
    renderMessageInPlace,
    setThinkingIndicatorVisible,
    syncThinkingIndicatorPosition,
    nextMessageOutputOrder,
    normalizeToolName,
    parseToolNameFromToolCallRaw,
    formatToolResultOutput,
    formatToolCallSummary,
    syncSendButtonState,
  };
}

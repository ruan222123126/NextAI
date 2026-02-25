import { createChatToolCallHelpers } from "./chat-tool-call.js";
import type {
  AgentStreamEvent,
  ChatDomainContext,
  ChatHistoryResponse,
  PlanAnswerValue,
  PlanModeState,
  PlanQuestion,
  PlanSpec,
  PlanStateResponse,
  ChatSpec,
  DeleteResult,
  PromptMode,
  ViewMessage,
  ViewMessageTimelineEntry,
  ViewToolCallNotice,
  WebAppTranslate,
  WorkspaceUploadResponse,
} from "./types.js";

type I18nKey = Parameters<WebAppTranslate>[0];

export function createChatDomain(ctx: ChatDomainContext) {
  const {
    state,
    t,
    setStatus,
    asErrorMessage,
    requestJSON,
    openStream,
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
    chatPlanModeSwitch,
    chatPlanStageBadge,
    planModePanel,
    planModeHint,
    planClarifyCounter,
    planQuestionsWrap,
    planClarifyForm,
    planClarifySubmitButton,
    planSpecWrap,
    planGoalValue,
    planScopeInList,
    planScopeOutList,
    planConstraintsList,
    planAssumptionsList,
    planTasksList,
    planAcceptanceList,
    planRisksList,
    planSummaryValue,
    planReviseInput,
    planReviseButton,
    planExecuteButton,
    planDisableButton,
    requestUserInputModal,
    requestUserInputModalTitle,
    requestUserInputModalProgress,
    requestUserInputModalQuestion,
    requestUserInputModalOptions,
    requestUserInputModalCustomInput,
    requestUserInputCancelButton,
    requestUserInputSubmitButton,
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
  const handledRequestUserInputRequests = new Set<string>();
  const handledOutputPlanPromptKeys = new Set<string>();
  let requestUserInputPromptChain: Promise<unknown> = Promise.resolve();
  let outputPlanPromptInFlight = false;
  const STREAM_REPLY_RETRY_LIMIT = 5;
  const DEFAULT_STREAM_REPLY_RETRY_DELAY_MS = 15_000;
  const RETRYABLE_STREAM_ERROR_MESSAGE_MARKERS = ["err_incomplete_chunked_encoding", "incomplete chunked encoding", "failed to fetch", "network request failed", "networkerror", "load failed", "fetch failed"];
  const PLAN_MODE_DEFAULT_MAX_COUNT = 5;
  const OUTPUT_PLAN_EXECUTE_QUESTION_ID = "output_plan_execute";
  const OUTPUT_PLAN_EXECUTE_WAIT_TIMEOUT_MS = 20_000;
  const {
    isToolCallRawNotice,
    normalizeToolName,
    parseToolNameFromToolCallRaw,
    resolveToolCallSummaryLine,
    resolveToolCallSummaryMeta,
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
      resetPlanState();
      activeChatCleared = true;
    }

    if (!chatsChanged && !activeChatCleared) {
      return;
    }
    if (state.activeChatId) {
      const active = state.chats.find((chat) => chat.id === state.activeChatId);
      if (active) {
        syncPlanStateFromChatMeta(active.meta);
      }
    }
    renderChatList();
    renderSearchChatResults();
    renderChatHeader();
    renderPlanPanel();
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
  syncPlanStateFromChatMeta(chat.meta);
  renderChatHeader();
  renderPlanPanel();
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
  resetPlanState();
  state.messages = [];
  renderChatHeader();
  renderPlanPanel();
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
function syncThinkingIndicatorByStreamEvent(event: AgentStreamEvent): void {
  const eventType = typeof event.type === "string" ? event.type : "";
  if (eventType === "step_started" || eventType === "tool_call" || eventType === "tool_result") {
    setThinkingIndicatorVisible(true);
    return;
  }
  if (eventType === "completed" || eventType === "error") {
    setThinkingIndicatorVisible(false);
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
function createAbortError(): Error {
  try {
    return new DOMException("aborted", "AbortError");
  } catch {
    return Object.assign(new Error("aborted"), { name: "AbortError" });
  }
}

class SSEEndedEarlyError extends Error { constructor(message: string) { super(message); this.name = "SSEEndedEarlyError"; } }
function isRetryableStreamError(error: unknown): boolean {
  if (error instanceof SSEEndedEarlyError) {
    return true;
  }
  if (error instanceof TypeError) {
    return true;
  }
  if (error instanceof DOMException && error.name === "NetworkError") {
    return true;
  }
  if (!(error instanceof Error)) {
    return false;
  }
  const normalizedMessage = error.message.trim().toLowerCase();
  if (normalizedMessage === "") {
    return false;
  }
  return RETRYABLE_STREAM_ERROR_MESSAGE_MARKERS.some((marker) => normalizedMessage.includes(marker));
}
function resolveStreamReplyRetryDelayMS(): number {
  const override = (globalThis as { __NEXTAI_STREAM_RETRY_DELAY_MS__?: unknown }).__NEXTAI_STREAM_RETRY_DELAY_MS__;
  const parsedOverride = parsePositiveInteger(override);
  return parsedOverride ?? DEFAULT_STREAM_REPLY_RETRY_DELAY_MS;
}

async function waitWithAbort(ms: number, signal?: AbortSignal): Promise<void> {
  if (ms <= 0) {
    return;
  }
  if (signal?.aborted) {
    throw createAbortError();
  }
  await new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(() => {
      if (signal) {
        signal.removeEventListener("abort", onAbort);
      }
      resolve();
    }, ms);
    function onAbort() {
      window.clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      reject(createAbortError());
    }
    signal?.addEventListener("abort", onAbort, { once: true });
  });
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

function toStringArray(raw: unknown): string[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const out: string[] = [];
  for (const item of raw) {
    if (typeof item !== "string") {
      continue;
    }
    const value = item.trim();
    if (value === "" || out.includes(value)) {
      continue;
    }
    out.push(value);
  }
  return out;
}

function normalizePlanModeState(raw: unknown): PlanModeState {
  const value = typeof raw === "string" ? raw.trim().toLowerCase() : "";
  switch (value) {
    case "off":
    case "planning_intake":
    case "planning_clarify":
    case "planning_ready":
    case "planning_revising":
    case "executing":
    case "done":
    case "aborted":
      return value;
    default:
      return "off";
  }
}

function dedupeQuestionID(base: string, seen: Set<string>): string {
  const seed = base.trim() === "" ? "q" : base.trim();
  if (!seen.has(seed)) {
    return seed;
  }
  let suffix = 2;
  while (true) {
    const candidate = `${seed}_${suffix}`;
    if (!seen.has(candidate)) {
      return candidate;
    }
    suffix += 1;
  }
}

function defaultQuestionID(index: number): string {
  return `q${index}`;
}

function defaultQuestionHeader(index: number): string {
  return `问题 ${index}`;
}

function normalizePlanQuestions(raw: unknown): PlanQuestion[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const out: PlanQuestion[] = [];
  const seenIDs = new Set<string>();
  for (const item of raw) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const row = item as Record<string, unknown>;
    const question = typeof row.question === "string" ? row.question.trim() : "";
    if (question === "") {
      continue;
    }
    const fallbackIndex = out.length + 1;
    const idSeed = typeof row.id === "string" ? row.id.trim() : "";
    const id = dedupeQuestionID(idSeed !== "" ? idSeed : defaultQuestionID(fallbackIndex), seenIDs);
    seenIDs.add(id);
    const header = typeof row.header === "string" ? row.header.trim() : "";
    const normalizedHeader = header !== "" ? header : defaultQuestionHeader(fallbackIndex);
    const options = Array.isArray(row.options)
      ? row.options
        .map((option) => {
          if (!option || typeof option !== "object") {
            return null;
          }
          const optionRow = option as Record<string, unknown>;
          const label = typeof optionRow.label === "string" ? optionRow.label.trim() : "";
          const description = typeof optionRow.description === "string" ? optionRow.description.trim() : "";
          if (label === "") {
            return null;
          }
          return description === "" ? { label } : { label, description };
        })
        .filter((option): option is { label: string; description?: string } => option !== null)
      : [];
    out.push({
      id,
      header: normalizedHeader,
      question,
      options,
    });
  }
  return out;
}

function normalizePlanSpec(raw: unknown): PlanSpec | null {
  if (!raw || typeof raw !== "object") {
    return null;
  }
  const row = raw as Record<string, unknown>;
  const goal = typeof row.goal === "string" ? row.goal.trim() : "";
  if (goal === "") {
    return null;
  }
  const tasks = Array.isArray(row.tasks)
    ? row.tasks
      .map((item) => {
        if (!item || typeof item !== "object") {
          return null;
        }
        const task = item as Record<string, unknown>;
        const id = typeof task.id === "string" ? task.id.trim() : "";
        const title = typeof task.title === "string" ? task.title.trim() : "";
        if (id === "" || title === "") {
          return null;
        }
        const status = typeof task.status === "string" ? task.status.trim().toLowerCase() : "pending";
        return {
          id,
          title,
          description: typeof task.description === "string" ? task.description.trim() : "",
          depends_on: toStringArray(task.depends_on),
          status: (status === "in_progress" || status === "completed" || status === "blocked" ? status : "pending") as PlanSpec["tasks"][number]["status"],
          deliverables: toStringArray(task.deliverables),
          verification: toStringArray(task.verification),
        };
      })
      .filter((item): item is PlanSpec["tasks"][number] => item !== null)
    : [];
  const risks = Array.isArray(row.risks)
    ? row.risks
      .map((item) => {
        if (!item || typeof item !== "object") {
          return null;
        }
        const risk = item as Record<string, unknown>;
        const id = typeof risk.id === "string" ? risk.id.trim() : "";
        const title = typeof risk.title === "string" ? risk.title.trim() : "";
        if (id === "" || title === "") {
          return null;
        }
        return {
          id,
          title,
          description: typeof risk.description === "string" ? risk.description.trim() : "",
          mitigation: typeof risk.mitigation === "string" ? risk.mitigation.trim() : "",
        };
      })
      .filter((item): item is PlanSpec["risks"][number] => item !== null)
    : [];
  return {
    goal,
    scope_in: toStringArray(row.scope_in),
    scope_out: toStringArray(row.scope_out),
    constraints: toStringArray(row.constraints),
    assumptions: toStringArray(row.assumptions),
    tasks,
    acceptance_criteria: toStringArray(row.acceptance_criteria),
    risks,
    summary_for_execution: typeof row.summary_for_execution === "string" ? row.summary_for_execution.trim() : "",
    revision: typeof row.revision === "number" && Number.isFinite(row.revision) ? row.revision : 0,
    updated_at: typeof row.updated_at === "string" ? row.updated_at : "",
  };
}

function resetPlanState(): void {
  state.planModeEnabled = false;
  state.planModeState = "off";
  state.planSpec = null;
  state.planClarifyAskedCount = 0;
  state.planClarifyMaxCount = PLAN_MODE_DEFAULT_MAX_COUNT;
  state.planClarifyUnresolved = [];
  state.planClarifyQuestions = [];
  state.planExecutionSessionId = "";
  state.planSourcePromptVersion = "";
}

function syncPlanStateFromChatMeta(meta: Record<string, unknown> | undefined): void {
  if (!meta) {
    resetPlanState();
    return;
  }
  const enabled = meta.plan_mode_enabled === true;
  state.planModeEnabled = enabled;
  state.planModeState = normalizePlanModeState(meta.plan_mode_state);
  if (!enabled) {
    state.planModeState = "off";
  }
  state.planSpec = normalizePlanSpec(meta.plan_spec);
  state.planClarifyAskedCount = typeof meta.clarify_asked_count === "number" ? Math.max(0, Math.floor(meta.clarify_asked_count)) : 0;
  const parsedMax = typeof meta.clarify_max_count === "number" ? Math.floor(meta.clarify_max_count) : PLAN_MODE_DEFAULT_MAX_COUNT;
  state.planClarifyMaxCount = parsedMax > 0 ? parsedMax : PLAN_MODE_DEFAULT_MAX_COUNT;
  state.planClarifyUnresolved = toStringArray(meta.clarify_unresolved);
  state.planExecutionSessionId = typeof meta.plan_execution_session_id === "string" ? meta.plan_execution_session_id.trim() : "";
  state.planSourcePromptVersion = typeof meta.plan_source_prompt_version === "string" ? meta.plan_source_prompt_version.trim() : "";
  state.planClarifyQuestions = [];
}

function syncActiveChatPlanMetaFromState(): void {
  const active = state.chats.find((chat) => chat.id === state.activeChatId);
  if (!active) {
    return;
  }
  if (!active.meta) {
    active.meta = {};
  }
  active.meta.plan_mode_enabled = state.planModeEnabled;
  active.meta.plan_mode_state = state.planModeState;
  active.meta.clarify_asked_count = state.planClarifyAskedCount;
  active.meta.clarify_max_count = state.planClarifyMaxCount;
  active.meta.clarify_unresolved = [...state.planClarifyUnresolved];
  active.meta.plan_execution_session_id = state.planExecutionSessionId;
  active.meta.plan_source_prompt_version = state.planSourcePromptVersion;
  if (state.planSpec) {
    active.meta.plan_spec = state.planSpec;
  } else {
    delete active.meta.plan_spec;
  }
}

function applyPlanStateResponse(response: PlanStateResponse): void {
  state.planModeEnabled = response.plan_mode_enabled === true;
  state.planModeState = normalizePlanModeState(response.plan_mode_state);
  if (!state.planModeEnabled) {
    state.planModeState = "off";
  }
  state.planSpec = normalizePlanSpec(response.plan_spec ?? null);
  state.planClarifyAskedCount = Math.max(0, Math.floor(response.clarify_asked_count ?? 0));
  const parsedMax = Math.floor(response.clarify_max_count ?? PLAN_MODE_DEFAULT_MAX_COUNT);
  state.planClarifyMaxCount = parsedMax > 0 ? parsedMax : PLAN_MODE_DEFAULT_MAX_COUNT;
  state.planClarifyUnresolved = toStringArray(response.clarify_unresolved);
  state.planExecutionSessionId = typeof response.plan_execution_session_id === "string" ? response.plan_execution_session_id.trim() : "";
  state.planSourcePromptVersion = typeof response.plan_source_prompt_version === "string" ? response.plan_source_prompt_version.trim() : "";
  state.planClarifyQuestions = normalizePlanQuestions(response.questions);
  syncActiveChatPlanMetaFromState();
  renderChatHeader();
  renderPlanPanel();
}

function resolvePlanStageLabel(stateValue: PlanModeState): string {
  switch (stateValue) {
    case "planning_intake":
      return t("chat.planStageIntake");
    case "planning_clarify":
      return t("chat.planStageClarify");
    case "planning_ready":
      return t("chat.planStageReady");
    case "planning_revising":
      return t("chat.planStageRevising");
    case "executing":
      return t("chat.planStageExecuting");
    case "done":
      return t("chat.planStageDone");
    case "aborted":
      return t("chat.planStageAborted");
    default:
      return "";
  }
}

function resolvePlanHintText(stateValue: PlanModeState): string {
  switch (stateValue) {
    case "planning_intake":
      return t("chat.planHintIntake");
    case "planning_clarify":
      return t("chat.planHintClarify");
    case "planning_ready":
      return t("chat.planHintReady");
    case "planning_revising":
      return t("chat.planHintRevising");
    case "executing":
      return t("chat.planHintExecuting");
    case "done":
      return t("chat.planHintDone");
    case "aborted":
      return t("chat.planHintAborted");
    default:
      return t("chat.planHintOff");
  }
}

function renderPlanStringList(list: HTMLUListElement, values: string[], emptyText: string): void {
  list.innerHTML = "";
  if (values.length === 0) {
    const li = document.createElement("li");
    li.className = "plan-empty-item";
    li.textContent = emptyText;
    list.appendChild(li);
    return;
  }
  values.forEach((value) => {
    const li = document.createElement("li");
    li.textContent = value;
    list.appendChild(li);
  });
}

function renderPlanTasksList(spec: PlanSpec | null): void {
  planTasksList.innerHTML = "";
  if (!spec || spec.tasks.length === 0) {
    const li = document.createElement("li");
    li.className = "plan-empty-item";
    li.textContent = t("chat.planEmpty");
    planTasksList.appendChild(li);
    return;
  }
  spec.tasks.forEach((task) => {
    const li = document.createElement("li");
    li.className = "plan-task-item";
    const status = document.createElement("strong");
    status.textContent = `[${task.status}]`;
    const title = document.createElement("span");
    title.textContent = ` ${task.title}`;
    li.append(status, title);
    if (task.description) {
      const desc = document.createElement("p");
      desc.textContent = task.description;
      li.appendChild(desc);
    }
    if (task.depends_on.length > 0) {
      const dep = document.createElement("p");
      dep.className = "plan-task-deps";
      dep.textContent = `${t("chat.planTaskDepends")}: ${task.depends_on.join(", ")}`;
      li.appendChild(dep);
    }
    planTasksList.appendChild(li);
  });
}

function renderPlanRisksList(spec: PlanSpec | null): void {
  planRisksList.innerHTML = "";
  if (!spec || spec.risks.length === 0) {
    const li = document.createElement("li");
    li.className = "plan-empty-item";
    li.textContent = t("chat.planEmpty");
    planRisksList.appendChild(li);
    return;
  }
  spec.risks.forEach((risk) => {
    const li = document.createElement("li");
    const title = document.createElement("strong");
    title.textContent = risk.title;
    li.appendChild(title);
    if (risk.description) {
      const desc = document.createElement("p");
      desc.textContent = risk.description;
      li.appendChild(desc);
    }
    if (risk.mitigation) {
      const mitigation = document.createElement("p");
      mitigation.className = "plan-risk-mitigation";
      mitigation.textContent = `${t("chat.planRiskMitigation")}: ${risk.mitigation}`;
      li.appendChild(mitigation);
    }
    planRisksList.appendChild(li);
  });
}

function renderPlanClarifyForm(): void {
  planClarifyForm.innerHTML = "";
  const shouldRenderQuestions = state.planModeEnabled && state.planModeState === "planning_clarify" && state.planClarifyQuestions.length > 0;
  if (!shouldRenderQuestions) {
    return;
  }
  state.planClarifyQuestions.forEach((question) => {
    const fieldset = document.createElement("fieldset");
    fieldset.className = "plan-question-card";
    fieldset.dataset.questionId = question.id;

    const legend = document.createElement("legend");
    legend.textContent = question.header ? `${question.header} · ${question.question}` : question.question;
    fieldset.appendChild(legend);

    if (Array.isArray(question.options) && question.options.length > 0) {
      question.options.forEach((option) => {
        const label = document.createElement("label");
        label.className = "plan-question-option";
        const radio = document.createElement("input");
        radio.type = "radio";
        radio.name = `plan-option-${question.id}`;
        radio.value = option.label;
        label.appendChild(radio);
        const text = document.createElement("span");
        text.textContent = option.description ? `${option.label} - ${option.description}` : option.label;
        label.appendChild(text);
        fieldset.appendChild(label);
      });
    }

    const freeInput = document.createElement("textarea");
    freeInput.name = `plan-free-${question.id}`;
    freeInput.rows = 2;
    freeInput.placeholder = t("chat.planClarifyFreeInput");
    fieldset.appendChild(freeInput);
    planClarifyForm.appendChild(fieldset);
  });
}

function collectPlanClarifyAnswers(): Record<string, PlanAnswerValue> {
  const answers: Record<string, PlanAnswerValue> = {};
  state.planClarifyQuestions.forEach((question) => {
    const selected = planClarifyForm.querySelector<HTMLInputElement>(`input[name="plan-option-${question.id}"]:checked`);
    const freeInput = planClarifyForm.querySelector<HTMLTextAreaElement>(`textarea[name="plan-free-${question.id}"]`);
    const values: string[] = [];
    if (selected && selected.value.trim() !== "") {
      values.push(selected.value.trim());
    }
    if (freeInput && freeInput.value.trim() !== "") {
      values.push(freeInput.value.trim());
    }
    if (values.length > 0) {
      answers[question.id] = { answers: values };
    }
  });
  return answers;
}

function renderPlanPanel(): void {
  planModePanel.hidden = true;
  planModePanel.classList.remove("is-ready-actions");
  chatPlanModeSwitch.checked = state.planModeEnabled;
  chatPlanStageBadge.hidden = !state.planModeEnabled;
  chatPlanStageBadge.textContent = state.planModeEnabled ? resolvePlanStageLabel(state.planModeState) : "";

  planModeHint.textContent = resolvePlanHintText(state.planModeState);
  planClarifyCounter.hidden = true;
  planClarifyCounter.textContent = t("chat.planClarifyCounter", {
    asked: state.planClarifyAskedCount,
    max: state.planClarifyMaxCount,
  });

  renderPlanClarifyForm();
  planQuestionsWrap.hidden = true;
  planSpecWrap.hidden = true;
  planGoalValue.textContent = state.planSpec?.goal ?? t("chat.planEmpty");
  renderPlanStringList(planScopeInList, state.planSpec?.scope_in ?? [], t("chat.planEmpty"));
  renderPlanStringList(planScopeOutList, state.planSpec?.scope_out ?? [], t("chat.planEmpty"));
  renderPlanStringList(planConstraintsList, state.planSpec?.constraints ?? [], t("chat.planEmpty"));
  renderPlanStringList(planAssumptionsList, state.planSpec?.assumptions ?? [], t("chat.planEmpty"));
  renderPlanTasksList(state.planSpec);
  renderPlanStringList(planAcceptanceList, state.planSpec?.acceptance_criteria ?? [], t("chat.planEmpty"));
  renderPlanRisksList(state.planSpec);
  planSummaryValue.textContent = state.planSpec?.summary_for_execution ?? t("chat.planEmpty");
  planReviseInput.hidden = true;
  planReviseButton.hidden = true;
  planExecuteButton.hidden = true;
  planDisableButton.hidden = true;

  const locked = state.sending;
  chatPlanModeSwitch.disabled = locked;
  planClarifySubmitButton.hidden = true;
  planClarifySubmitButton.disabled = true;
  planReviseButton.disabled = true;
  planExecuteButton.disabled = true;
  planDisableButton.disabled = locked || !state.planModeEnabled;
  updateComposerPlaceholder();
}

function updateComposerPlaceholder(): void {
  messageInput.placeholder = state.planModeEnabled ? t("chat.planInputPlaceholder") : t("chat.inputPlaceholder");
}

async function ensurePlanChatID(): Promise<string> {
  if (state.activeChatId) {
    return state.activeChatId;
  }
  const created = await requestJSON<ChatSpec>("/chats", {
    method: "POST",
    body: {
      name: t("chat.draftTitle"),
      session_id: state.activeSessionId,
      user_id: state.userId,
      channel: WEB_CHAT_CHANNEL,
      meta: {},
    },
  });
  await reloadChats({ includeQQHistory: false });
  await openChat(created.id);
  return created.id;
}

async function togglePlanMode(
  enabled: boolean,
  options: { confirm?: boolean; announce?: boolean } = {},
): Promise<void> {
  await getBootstrapTask();
  syncControlState();
  const announce = options.announce !== false;
  const targetEnabled = enabled;
  try {
    const chatID = await ensurePlanChatID();
    const response = await requestJSON<PlanStateResponse>("/agent/plan/toggle", {
      method: "POST",
      body: {
        chat_id: chatID,
        enabled: targetEnabled,
        confirm: options.confirm === true,
      },
    });
    applyPlanStateResponse(response);
    if (announce) {
      setStatus(targetEnabled ? t("status.planModeEnabled") : t("status.planModeDisabled"), "info");
    }
  } catch (error) {
    const message = asErrorMessage(error);
    if (!targetEnabled && message.includes("plan_toggle_confirmation_required") && options.confirm !== true) {
      const confirmed = window.confirm(t("chat.planDisableConfirm"));
      if (confirmed) {
        await togglePlanMode(false, { ...options, confirm: true });
        return;
      }
      chatPlanModeSwitch.checked = state.planModeEnabled;
      return;
    }
    chatPlanModeSwitch.checked = state.planModeEnabled;
    setStatus(message, "error");
  } finally {
    renderPlanPanel();
  }
}

function toRequestUserInputQuestionsFromPlan(questions: PlanQuestion[]): RequestUserInputQuestion[] {
  const seenIDs = new Set<string>();
  return questions
    .map((question, idx) => {
      const prompt = typeof question.question === "string" ? question.question.trim() : "";
      if (prompt === "") {
        return null;
      }
      const fallbackIndex = idx + 1;
      const idSeed = typeof question.id === "string" ? question.id.trim() : "";
      const id = dedupeQuestionID(idSeed !== "" ? idSeed : defaultQuestionID(fallbackIndex), seenIDs);
      seenIDs.add(id);
      const headerSeed = typeof question.header === "string" ? question.header.trim() : "";
      const header = headerSeed !== "" ? headerSeed : defaultQuestionHeader(fallbackIndex);
      const options = Array.isArray(question.options)
        ? question.options
          .map((option) => {
            const label = typeof option.label === "string" ? option.label.trim() : "";
            const description = typeof option.description === "string" ? option.description.trim() : "";
            if (label === "") {
              return null;
            }
            return description === "" ? { label } : { label, description };
          })
          .filter((option): option is RequestUserInputQuestionOption => option !== null)
        : [];
      return {
        id,
        header,
        question: prompt,
        options,
      };
    })
    .filter((question): question is RequestUserInputQuestion => question !== null);
}

function toPlanClarifyAnswerPayload(
  answers: Record<string, RequestUserInputAnswer>,
): Record<string, PlanAnswerValue> {
  const payload: Record<string, PlanAnswerValue> = {};
  for (const [questionID, answer] of Object.entries(answers)) {
    const id = questionID.trim();
    if (id === "") {
      continue;
    }
    const normalizedAnswers = toStringArray(answer.answers);
    if (normalizedAnswers.length === 0) {
      continue;
    }
    payload[id] = { answers: normalizedAnswers };
  }
  return payload;
}

async function runPlanClarifyFlow(chatID: string, questions: PlanQuestion[]): Promise<PlanStateResponse | null> {
  let pendingQuestions = toRequestUserInputQuestionsFromPlan(questions);
  while (pendingQuestions.length > 0) {
    const answers = await askQuestionsWithModal(pendingQuestions);
    if (answers === null) {
      setStatus(t("status.planClarifyCancelled"), "info");
      return null;
    }
    const response = await requestJSON<PlanStateResponse>("/agent/plan/clarify/answer", {
      method: "POST",
      body: {
        chat_id: chatID,
        answers: toPlanClarifyAnswerPayload(answers),
      },
    });
    applyPlanStateResponse(response);
    if (response.plan_mode_state !== "planning_clarify") {
      return response;
    }
    setStatus(t("status.planClarifyRequested"), "info");
    pendingQuestions = toRequestUserInputQuestionsFromPlan(normalizePlanQuestions(response.questions));
    if (pendingQuestions.length === 0) {
      return response;
    }
  }
  return null;
}

async function compilePlanFromInput(userInput: string): Promise<void> {
  const chatID = await ensurePlanChatID();
  const response = await requestJSON<PlanStateResponse>("/agent/plan/compile", {
    method: "POST",
    body: {
      chat_id: chatID,
      user_input: userInput,
    },
  });
  applyPlanStateResponse(response);
  if (response.plan_mode_state === "planning_clarify") {
    setStatus(t("status.planClarifyRequested"), "info");
    const next = await runPlanClarifyFlow(chatID, normalizePlanQuestions(response.questions));
    if (next && next.plan_mode_state !== "planning_clarify") {
      setStatus(t("status.planReady"), "info");
    }
    return;
  }
  setStatus(t("status.planReady"), "info");
}

async function submitPlanClarifyAnswers(): Promise<void> {
  if (!state.planModeEnabled || state.planModeState !== "planning_clarify") {
    setStatus(t("status.planNotInClarify"), "error");
    return;
  }
  if (state.planClarifyQuestions.length === 0) {
    setStatus(t("status.planClarifyPending"), "info");
    return;
  }
  const chatID = state.activeChatId ?? await ensurePlanChatID();
  const response = await runPlanClarifyFlow(chatID, state.planClarifyQuestions);
  if (response && response.plan_mode_state !== "planning_clarify") {
    setStatus(t("status.planReady"), "info");
  }
}

async function revisePlan(): Promise<void> {
  if (!state.planModeEnabled || state.planModeState !== "planning_ready" || !state.planSpec) {
    setStatus(t("status.planNotReady"), "error");
    return;
  }
  const feedback = planReviseInput.value.trim();
  if (feedback === "") {
    setStatus(t("status.planFeedbackRequired"), "error");
    return;
  }
  const chatID = state.activeChatId ?? await ensurePlanChatID();
  const response = await requestJSON<PlanStateResponse>("/agent/plan/revise", {
    method: "POST",
    body: {
      chat_id: chatID,
      natural_language_feedback: feedback,
    },
  });
  planReviseInput.value = "";
  applyPlanStateResponse(response);
  setStatus(t("status.planRevised"), "info");
}

interface ExecutePlanOptions {
  autoKickoff?: boolean;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms);
  });
}

async function waitForSendingIdle(timeoutMS = OUTPUT_PLAN_EXECUTE_WAIT_TIMEOUT_MS): Promise<void> {
  const startedAt = Date.now();
  while (state.sending) {
    if (Date.now() - startedAt >= timeoutMS) {
      throw new Error("timed out waiting for current stream to finish");
    }
    await sleep(50);
  }
}

async function triggerPlanExecutionKickoff(message: string): Promise<void> {
  const text = message.trim();
  if (text === "") {
    return;
  }
  messageInput.value = text;
  await sendMessage();
}

async function executePlan(options: ExecutePlanOptions = {}): Promise<void> {
  if (!state.planModeEnabled || state.planModeState !== "planning_ready" || !state.planSpec) {
    setStatus(t("status.planNotReady"), "error");
    return;
  }
  const autoKickoff = options.autoKickoff === true;
  const kickoffMessage = autoKickoff ? t("chat.planExecuteAutoKickoffMessage") : "";
  const chatID = state.activeChatId ?? await ensurePlanChatID();
  const response = await requestJSON<{ execution_session_id?: string }>("/agent/plan/execute", {
    method: "POST",
    body: { chat_id: chatID },
  });
  const executionSessionID = typeof response.execution_session_id === "string" ? response.execution_session_id.trim() : "";
  if (executionSessionID === "") {
    throw new Error("plan execute response missing execution_session_id");
  }
  await reloadChats();
  const matched = state.chats.find((chat) => chat.session_id === executionSessionID);
  if (matched) {
    await openChat(matched.id);
  }
  setStatus(t("status.planExecuteStarted", { sessionId: executionSessionID }), "info");
  if (autoKickoff) {
    await triggerPlanExecutionKickoff(kickoffMessage);
  }
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
    handledRequestUserInputRequests.clear();
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
          syncThinkingIndicatorByStreamEvent(event);
          handleToolCallEvent(event, assistantID);
          void maybeHandleRequestUserInputToolCall(event);
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
    handledRequestUserInputRequests.clear();
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

  const requestBody = JSON.stringify(payload);
  const retryDelayMS = resolveStreamReplyRetryDelayMS();
  for (let retry = 0; retry <= STREAM_REPLY_RETRY_LIMIT; retry += 1) {
    let streamProducedOutput = false;
    const onTrackedDelta = (delta: string) => {
      if (delta !== "") {
        streamProducedOutput = true;
      }
      onDelta(delta);
    };
    const onTrackedEvent = (event: AgentStreamEvent) => {
      streamProducedOutput = true;
      if (onEvent) {
        onEvent(event);
      }
    };
    try {
      await streamReplyOnce(requestBody, payload, onTrackedDelta, onTrackedEvent, signal);
      return;
    } catch (error) {
      const hasRetryQuota = retry < STREAM_REPLY_RETRY_LIMIT;
      const shouldRetry = hasRetryQuota && !streamProducedOutput && !isAbortError(error) && isRetryableStreamError(error);
      if (!shouldRetry) {
        throw error;
      }
      const retryAttempt = retry + 1;
      setStatus(
        t("status.streamRetryWaiting", {
          seconds: Math.floor(retryDelayMS / 1000),
          attempt: retryAttempt,
          max: STREAM_REPLY_RETRY_LIMIT,
        }),
        "info",
      );
      await waitWithAbort(retryDelayMS, signal);
      setStatus(t("status.streamingReply"), "info");
    }
  }
}

async function streamReplyOnce(
  requestBody: string,
  payload: Record<string, unknown>,
  onDelta: (delta: string) => void,
  onEvent?: (event: AgentStreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  logAgentRawRequest(requestBody);

  const response = await openStream("/agent/process", {
    method: "POST",
    body: payload,
    accept: "text/event-stream,application/json",
    signal,
  });
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
      throw new SSEEndedEarlyError(t("error.sseEndedEarly"));
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
  void raw;
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
    setStatus(t("status.promptModeDefaultEnabled"), "info");
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
  chatPlanModeSwitch.checked = state.planModeEnabled;
  chatPlanStageBadge.textContent = state.planModeEnabled ? resolvePlanStageLabel(state.planModeState) : "";
  chatPlanStageBadge.hidden = !state.planModeEnabled;
  updateComposerPlaceholder();
  renderPlanPanel();
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
interface RequestUserInputQuestionOption {
  label: string;
  description?: string;
}
interface RequestUserInputQuestion {
  id: string;
  header: string;
  question: string;
  options: RequestUserInputQuestionOption[];
}
interface RequestUserInputAnswer {
  answers: string[];
}

async function maybeHandleRequestUserInputToolCall(event: AgentStreamEvent): Promise<void> {
  if (event.type !== "tool_call") {
    return;
  }
  const toolName = normalizeToolName(event.tool_call?.name);
  if (toolName !== "request_user_input") {
    return;
  }
  const input = (event.tool_call?.input ?? {}) as Record<string, unknown>;
  const requestID = typeof input.request_id === "string" ? input.request_id.trim() : "";
  if (requestID === "" || handledRequestUserInputRequests.has(requestID)) {
    return;
  }
  handledRequestUserInputRequests.add(requestID);

  const questions = parseRequestUserInputQuestions(input.questions);
  const answers = await askQuestionsWithModal(questions);
  if (answers === null) {
    setStatus(t("status.requestUserInputCancelled"), "info");
  }
  try {
    await requestJSON<{ accepted?: boolean; request_id?: string }>("/agent/tool-input-answer", {
      method: "POST",
      body: {
        request_id: requestID,
        session_id: state.activeSessionId,
        user_id: state.userId,
        channel: WEB_CHAT_CHANNEL,
        answers: answers ?? {},
      },
    });
  } catch (error) {
    handledRequestUserInputRequests.delete(requestID);
    setStatus(asErrorMessage(error), "error");
  }
}
function parseRequestUserInputQuestions(raw: unknown): RequestUserInputQuestion[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const out: RequestUserInputQuestion[] = [];
  const seenIDs = new Set<string>();
  for (const item of raw) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const row = item as Record<string, unknown>;
    const question = typeof row.question === "string" ? row.question.trim() : "";
    if (question === "") {
      continue;
    }
    const fallbackIndex = out.length + 1;
    const idSeed = typeof row.id === "string" ? row.id.trim() : "";
    const id = dedupeQuestionID(idSeed !== "" ? idSeed : defaultQuestionID(fallbackIndex), seenIDs);
    seenIDs.add(id);
    const headerSeed = typeof row.header === "string" ? row.header.trim() : "";
    const header = headerSeed !== "" ? headerSeed : defaultQuestionHeader(fallbackIndex);
    const options = parseRequestUserInputQuestionOptions(row.options);
    out.push({
      id,
      header,
      question,
      options,
    });
  }
  return out;
}
function parseRequestUserInputQuestionOptions(raw: unknown): RequestUserInputQuestionOption[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  const out: RequestUserInputQuestionOption[] = [];
  const seenLabels = new Set<string>();
  for (const item of raw) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const row = item as Record<string, unknown>;
    const label = typeof row.label === "string" ? row.label.trim() : "";
    const description = typeof row.description === "string" ? row.description.trim() : "";
    if (label === "") {
      continue;
    }
    const normalizedLabel = label.toLowerCase();
    if (seenLabels.has(normalizedLabel)) {
      continue;
    }
    seenLabels.add(normalizedLabel);
    out.push(description === "" ? { label } : { label, description });
  }
  return out;
}
function setRequestUserInputModalOpen(open: boolean): void {
  requestUserInputModal.classList.toggle("is-hidden", !open);
  requestUserInputModal.setAttribute("aria-hidden", String(!open));
}
function askQuestionsWithModal(
  questions: RequestUserInputQuestion[],
): Promise<Record<string, RequestUserInputAnswer> | null> {
  const task = requestUserInputPromptChain.then(() => askQuestionsWithModalNow(questions));
  requestUserInputPromptChain = task.catch(() => undefined);
  return task;
}
async function askQuestionsWithModalNow(
  questions: RequestUserInputQuestion[],
): Promise<Record<string, RequestUserInputAnswer> | null> {
  const answers: Record<string, RequestUserInputAnswer> = {};
  for (let index = 0; index < questions.length; index += 1) {
    const question = questions[index];
    const picked = await askSingleQuestionWithModal(question, index, questions.length);
    if (picked === null) {
      return null;
    }
    if (picked.length > 0) {
      answers[question.id] = { answers: picked };
    }
  }
  return answers;
}
function askSingleQuestionWithModal(
  question: RequestUserInputQuestion,
  index: number,
  total: number,
): Promise<string[] | null> {
  return new Promise((resolve) => {
    let selectedOption = question.options[0]?.label ?? "";
    const optionButtons: HTMLButtonElement[] = [];
    const current = index + 1;

    const syncOptionSelectionState = (): void => {
      optionButtons.forEach((button) => {
        const isSelected = (button.dataset.optionLabel ?? "") === selectedOption;
        button.classList.toggle("is-selected", isSelected);
        button.setAttribute("aria-pressed", String(isSelected));
      });
    };

    const cleanup = (): void => {
      requestUserInputModal.removeEventListener("click", onModalClick);
      requestUserInputCancelButton.removeEventListener("click", onCancel);
      requestUserInputSubmitButton.removeEventListener("click", onSubmit);
      document.removeEventListener("keydown", onKeyDown);
      requestUserInputModalOptions.innerHTML = "";
      requestUserInputModalCustomInput.value = "";
      setRequestUserInputModalOpen(false);
    };

    const finalize = (value: string[] | null): void => {
      cleanup();
      resolve(value);
    };

    const onModalClick = (event: Event): void => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      if (target.closest("[data-request-user-input-close=\"true\"]")) {
        finalize(null);
      }
    };

    const onCancel = (event: Event): void => {
      event.preventDefault();
      finalize(null);
    };

    const onSubmit = (event: Event): void => {
      event.preventDefault();
      const answers: string[] = [];
      if (selectedOption !== "") {
        answers.push(selectedOption);
      }
      const customInput = requestUserInputModalCustomInput.value.trim();
      if (customInput !== "") {
        answers.push(customInput);
      }
      const normalizedAnswers = toStringArray(answers);
      if (normalizedAnswers.length === 0 && question.options.length > 0) {
        finalize([question.options[0].label]);
        return;
      }
      finalize(normalizedAnswers);
    };

    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") {
        return;
      }
      if (requestUserInputModal.classList.contains("is-hidden")) {
        return;
      }
      event.preventDefault();
      finalize(null);
    };

    requestUserInputModalProgress.textContent = t("chat.userInputProgress", {
      current,
      total,
    });
    requestUserInputModalTitle.textContent = question.header;
    requestUserInputModalQuestion.textContent = question.question;
    requestUserInputModalOptions.innerHTML = "";
    requestUserInputModalCustomInput.value = "";

    question.options.forEach((option) => {
      const row = document.createElement("li");
      const button = document.createElement("button");
      button.type = "button";
      button.className = "request-user-input-modal-option-btn";
      button.dataset.optionLabel = option.label;

      const label = document.createElement("span");
      label.className = "request-user-input-modal-option-label";
      label.textContent = option.label;
      button.appendChild(label);

      if (option.description) {
        const desc = document.createElement("span");
        desc.className = "request-user-input-modal-option-desc";
        desc.textContent = option.description;
        button.appendChild(desc);
      }

      button.addEventListener("click", () => {
        selectedOption = option.label;
        syncOptionSelectionState();
      });

      optionButtons.push(button);
      row.appendChild(button);
      requestUserInputModalOptions.appendChild(row);
    });

    requestUserInputModalOptions.hidden = question.options.length === 0;
    syncOptionSelectionState();

    requestUserInputModal.addEventListener("click", onModalClick);
    requestUserInputCancelButton.addEventListener("click", onCancel);
    requestUserInputSubmitButton.addEventListener("click", onSubmit);
    document.addEventListener("keydown", onKeyDown);

    setRequestUserInputModalOpen(true);
    if (optionButtons.length > 0) {
      optionButtons[0].focus();
    } else {
      requestUserInputModalCustomInput.focus();
    }
  });
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

function normalizePlanStateResponsePayload(raw: unknown): PlanStateResponse | null {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return null;
  }
  const row = raw as Record<string, unknown>;
  if (row.plan_mode_enabled !== true && row.plan_mode_enabled !== false) {
    return null;
  }
  const planSpec = normalizePlanSpec(row.plan_spec);
  return {
    chat_id: typeof row.chat_id === "string" ? row.chat_id.trim() : (state.activeChatId ?? ""),
    plan_mode_enabled: row.plan_mode_enabled === true,
    plan_mode_state: normalizePlanModeState(row.plan_mode_state),
    plan_spec: planSpec ?? undefined,
    clarify_asked_count: typeof row.clarify_asked_count === "number" ? Math.max(0, Math.floor(row.clarify_asked_count)) : 0,
    clarify_max_count: typeof row.clarify_max_count === "number"
      ? Math.max(1, Math.floor(row.clarify_max_count))
      : PLAN_MODE_DEFAULT_MAX_COUNT,
    clarify_unresolved: toStringArray(row.clarify_unresolved),
    plan_execution_session_id: typeof row.plan_execution_session_id === "string" ? row.plan_execution_session_id.trim() : "",
    plan_source_prompt_version: typeof row.plan_source_prompt_version === "string" ? row.plan_source_prompt_version.trim() : "",
    questions: normalizePlanQuestions(row.questions),
  };
}

function parseOutputPlanToolResult(raw: string): PlanStateResponse | null {
  const text = raw.trim();
  if (text === "") {
    return null;
  }
  try {
    const parsed = JSON.parse(text) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return null;
    }
    const root = parsed as Record<string, unknown>;
    const nested = root.plan_state;
    if (nested && typeof nested === "object" && !Array.isArray(nested)) {
      return normalizePlanStateResponsePayload(nested);
    }
    return normalizePlanStateResponsePayload(parsed);
  } catch {
    return null;
  }
}

function buildOutputPlanPromptKey(response: PlanStateResponse): string {
  const chatID = response.chat_id.trim();
  const revision = response.plan_spec?.revision ?? -1;
  const updatedAt = response.plan_spec?.updated_at ?? "";
  const goal = response.plan_spec?.goal ?? "";
  return `${chatID}|${revision}|${updatedAt}|${goal}`;
}

function shouldExecuteOutputPlan(
  answers: Record<string, RequestUserInputAnswer> | null,
  executeOptionLabel: string,
): boolean {
  if (!answers) {
    return false;
  }
  const picked = answers[OUTPUT_PLAN_EXECUTE_QUESTION_ID]?.answers[0] ?? "";
  return picked.trim() === executeOptionLabel;
}

async function maybePromptExecutePlanFromOutputPlan(response: PlanStateResponse): Promise<void> {
  if (!response.plan_mode_enabled || response.plan_mode_state !== "planning_ready" || !response.plan_spec) {
    return;
  }
  const key = buildOutputPlanPromptKey(response);
  if (key !== "" && handledOutputPlanPromptKeys.has(key)) {
    return;
  }
  if (outputPlanPromptInFlight) {
    return;
  }
  if (key !== "") {
    handledOutputPlanPromptKeys.add(key);
  }
  outputPlanPromptInFlight = true;
  try {
    const holdOptionLabel = t("chat.outputPlanExecuteOptionHold");
    const executeOptionLabel = t("chat.outputPlanExecuteOptionExecute");
    const answers = await askQuestionsWithModal([{
      id: OUTPUT_PLAN_EXECUTE_QUESTION_ID,
      header: t("chat.outputPlanExecutePromptHeader"),
      question: t("chat.outputPlanExecutePromptQuestion"),
      options: [
        {
          label: holdOptionLabel,
          description: t("chat.outputPlanExecuteOptionHoldDesc"),
        },
        {
          label: executeOptionLabel,
          description: t("chat.outputPlanExecuteOptionExecuteDesc"),
        },
      ],
    }]);
    if (!shouldExecuteOutputPlan(answers, executeOptionLabel)) {
      setStatus(t("status.outputPlanExecutionDeferred"), "info");
      return;
    }
    await waitForSendingIdle();
    await executePlan({ autoKickoff: true });
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  } finally {
    outputPlanPromptInFlight = false;
  }
}

function maybeApplyOutputPlanToolResult(event: AgentStreamEvent, toolName: string): void {
  if (toolName !== "output_plan") {
    return;
  }
  const output = typeof event.tool_result?.output === "string" ? event.tool_result.output : "";
  const summary = typeof event.tool_result?.summary === "string" ? event.tool_result.summary : "";
  const response = parseOutputPlanToolResult(output) ?? parseOutputPlanToolResult(summary);
  if (!response) {
    return;
  }
  applyPlanStateResponse(response);
  setStatus(t("status.planReady"), "info");
  void maybePromptExecutePlanFromOutputPlan(response);
}

function applyToolResultEvent(event: AgentStreamEvent, assistantID: string): void {
  const raw = typeof event.raw === "string" ? event.raw : "";
  const toolName = normalizeToolName(event.tool_result?.name) || parseToolNameFromToolCallRaw(raw);
  if (toolName === "") {
    return;
  }
  maybeApplyOutputPlanToolResult(event, toolName);
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

    const summaryMetaText = resolveToolCallSummaryMeta(toolCall);
    if (summaryMetaText !== "") {
      const summaryMeta = document.createElement("span");
      summaryMeta.className = "tool-call-summary-meta";
      summaryMeta.textContent = summaryMetaText;
      row.appendChild(summaryMeta);
    }

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
    togglePlanMode,
    submitPlanClarifyAnswers,
    revisePlan,
    executePlan,
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

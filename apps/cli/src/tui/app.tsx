import React, { useCallback, useEffect, useRef, useState } from "react";
import { Box, Static, Text, useApp, useInput, useStdout } from "ink";
import stringWidth from "string-width";
import { appendQuery, fillPath } from "@nextai/sdk-ts";
import { ApiClient, ApiClientError } from "../client/api-client.js";
import { resolveLocale, setLocale, t, type Locale } from "../i18n.js";
import { applyAgentEvent, appendUserMessage, beginAssistantMessage, historyToViewMessages, settleAssistantMessage } from "./state.js";
import { consumeSSEBuffer, parseAgentStreamData } from "./stream.js";
import {
  buildProcessBizParams,
  nextCollaborationMode,
  nextPromptMode,
  normalizeCollaborationMode,
  resolveModesFromMeta,
  type TUICollaborationMode,
  type TUIPromptMode,
} from "./mode.js";
import type { ChatHistoryResponse, ChatSpec, TUIBootstrapOptions, TUIMessage, TUISettings } from "./types.js";

const settingFields = ["apiBase", "apiKey", "userID", "channel", "locale"] as const;
type SettingField = (typeof settingFields)[number];
const settingLabelKeys: Record<SettingField, "tui.settings.api_base" | "tui.settings.api_key" | "tui.settings.user_id" | "tui.settings.channel" | "tui.settings.locale"> = {
  apiBase: "tui.settings.api_base",
  apiKey: "tui.settings.api_key",
  userID: "tui.settings.user_id",
  channel: "tui.settings.channel",
  locale: "tui.settings.locale",
};
const PROMPT_TEMPLATE_PREFIX = "/prompts:";
const PROMPT_TEMPLATE_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const PROMPT_TEMPLATE_ARG_KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;
const PROMPT_TEMPLATE_PLACEHOLDER_PATTERN = /\$([A-Za-z_][A-Za-z0-9_]*)/g;
const promptTemplatesEnabled = parseEnvBool(process.env.NEXTAI_ENABLE_PROMPT_TEMPLATES);

interface TUIAppProps {
  client: ApiClient;
  bootstrap: TUIBootstrapOptions;
}

interface PromptTemplateCommand {
  templateName: string;
  args: Map<string, string>;
}

function parseEnvBool(raw: string | undefined): boolean {
  if (!raw) {
    return false;
  }
  return raw.trim().toLowerCase() === "true";
}

function extractWorkspaceTextPayload(payload: unknown): string {
  if (typeof payload === "string") {
    return payload;
  }
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return "";
  }
  const record = payload as Record<string, unknown>;
  return typeof record.content === "string" ? record.content : "";
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

async function loadPromptTemplateContent(client: ApiClient, templateName: string): Promise<string> {
  const candidates = [`prompts/${templateName}.md`, `prompt/${templateName}.md`];
  let lastError: unknown = null;
  for (const path of candidates) {
    try {
      const payload = await client.workspaceCat(path);
      const content = extractWorkspaceTextPayload(payload);
      if (content.trim() === "") {
        throw new Error(`prompt template is empty: ${path}`);
      }
      return content;
    } catch (error) {
      lastError = error;
    }
  }
  const suffix = lastError ? ` (${errorToMessage(lastError)})` : "";
  throw new Error(`prompt template not found: ${templateName}${suffix}`);
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

async function expandPromptTemplateIfNeeded(client: ApiClient, inputText: string): Promise<string> {
  if (!promptTemplatesEnabled) {
    return inputText;
  }
  const parsed = parsePromptTemplateCommand(inputText);
  if (!parsed) {
    return inputText;
  }
  const templateContent = await loadPromptTemplateContent(client, parsed.templateName);
  return applyPromptTemplateArgs(templateContent, parsed.args);
}

function isBackspaceInput(input: string, key: { backspace: boolean; delete?: boolean; ctrl: boolean }): boolean {
  return key.backspace || Boolean(key.delete) || input === "\u0008" || input === "\u007f" || (key.ctrl && input === "h");
}

function errorToMessage(err: unknown): string {
  if (err instanceof ApiClientError) {
    return `[${err.code}] ${err.message}`;
  }
  if (err instanceof Error) {
    return err.message;
  }
  return String(err);
}

function maskAPIKey(apiKey: string): string {
  if (apiKey.length <= 6) {
    return apiKey === "" ? "" : "*".repeat(apiKey.length);
  }
  return `${apiKey.slice(0, 3)}${"*".repeat(apiKey.length - 6)}${apiKey.slice(-3)}`;
}

function sessionLabel(chat: ChatSpec): string {
  return `${chat.name} (${chat.session_id})`;
}

function clamp(n: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, n));
}

function fitText(input: string, maxLen: number): string {
  if (input.length <= maxLen) {
    return input;
  }
  if (maxLen <= 3) {
    return input.slice(0, maxLen);
  }
  return `${input.slice(0, maxLen - 3)}...`;
}

function calcHistoryVisibleCount(rows: number, historyOpen: boolean, settingsOpen: boolean, sessionsCount: number): number {
  if (!historyOpen) {
    return 0;
  }
  const safeRows = Math.max(rows, 12);
  const reservedRows = settingsOpen ? 14 : 8;
  const limit = Math.max(3, safeRows - reservedRows);
  return clamp(limit, 3, Math.max(3, sessionsCount));
}

function calcWindow(total: number, selected: number, limit: number): { start: number; end: number } {
  if (total <= 0 || limit <= 0 || total <= limit) {
    return { start: 0, end: total };
  }
  const half = Math.floor(limit / 2);
  let start = selected - half;
  if (start < 0) {
    start = 0;
  }
  if (start + limit > total) {
    start = total - limit;
  }
  return {
    start,
    end: start + limit,
  };
}

function promptModeLabelKey(mode: TUIPromptMode): "tui.mode.prompt.default" | "tui.mode.prompt.codex" | "tui.mode.prompt.claude" {
  if (mode === "codex") {
    return "tui.mode.prompt.codex";
  }
  if (mode === "claude") {
    return "tui.mode.prompt.claude";
  }
  return "tui.mode.prompt.default";
}

function collaborationModeLabelKey(
  mode: TUICollaborationMode,
): "tui.mode.collaboration.default" | "tui.mode.collaboration.plan" | "tui.mode.collaboration.execute" | "tui.mode.collaboration.pair_programming" {
  if (mode === "plan") {
    return "tui.mode.collaboration.plan";
  }
  if (mode === "execute") {
    return "tui.mode.collaboration.execute";
  }
  if (mode === "pair_programming") {
    return "tui.mode.collaboration.pair_programming";
  }
  return "tui.mode.collaboration.default";
}

interface ChatRenderLine {
  key: string;
  color: "red" | "cyan" | "white";
  text: string;
}

function wrapByWidth(input: string, width: number): string[] {
  const safeWidth = Math.max(width, 1);
  if (input === "") {
    return [""];
  }
  const parts: string[] = [];
  let current = "";
  let currentWidth = 0;
  for (const ch of input) {
    const chWidth = Math.max(1, stringWidth(ch));
    if (current !== "" && currentWidth + chWidth > safeWidth) {
      parts.push(current);
      current = ch;
      currentWidth = chWidth;
      continue;
    }
    current += ch;
    currentWidth += chWidth;
  }
  if (current !== "") {
    parts.push(current);
  }
  return parts.length > 0 ? parts : [""];
}

function buildChatRenderLines(messages: TUIMessage[], contentWidth: number): ChatRenderLine[] {
  const lines: ChatRenderLine[] = [];
  const safeContentWidth = Math.max(contentWidth, 10);
  for (let i = 0; i < messages.length; i += 1) {
    const msg = messages[i];
    const rolePrefix = msg.role === "user" ? "You: " : "AI: ";
    const color: "red" | "cyan" | "white" = msg.failed ? "red" : msg.role === "user" ? "cyan" : "white";
    const text = msg.pending && msg.text.trim() === "" ? t("tui.message.pending") : msg.text;
    const sourceLines = text.split("\n");

    for (let j = 0; j < sourceLines.length; j += 1) {
      const sourceLine = sourceLines[j] ?? "";
      const firstPrefix = j === 0 ? rolePrefix : "    ";
      const firstWidth = Math.max(4, safeContentWidth - stringWidth(firstPrefix));
      const wrapped = wrapByWidth(sourceLine, firstWidth);

      for (let k = 0; k < wrapped.length; k += 1) {
        const prefix = k === 0 ? firstPrefix : "    ";
        const wrappedPart = wrapped[k] ?? "";
        lines.push({
          key: `${i}-${j}-${k}`,
          color,
          text: `${prefix}${wrappedPart}`,
        });
      }
    }
  }
  return lines;
}

export function TUIApp({ client, bootstrap }: TUIAppProps): React.ReactElement {
  const { exit } = useApp();
  const { stdout } = useStdout();
  const initialized = useRef(false);
  const [sessions, setSessions] = useState<ChatSpec[]>([]);
  const [activeChatID, setActiveChatID] = useState<string>("");
  const [activeSessionID, setActiveSessionID] = useState<string>(bootstrap.sessionID ?? "");
  const [messages, setMessages] = useState<TUIMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [status, setStatus] = useState(t("tui.status.ready"));
  const [errorText, setErrorText] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historySelectionIndex, setHistorySelectionIndex] = useState(0);

  const [settings, setSettings] = useState<TUISettings>({
    apiBase: bootstrap.apiBase,
    apiKey: bootstrap.apiKey,
    userID: bootstrap.userID,
    channel: bootstrap.channel,
    locale: bootstrap.locale,
  });
  const [settingsDraft, setSettingsDraft] = useState<TUISettings>({
    apiBase: bootstrap.apiBase,
    apiKey: bootstrap.apiKey,
    userID: bootstrap.userID,
    channel: bootstrap.channel,
    locale: bootstrap.locale,
  });
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsFieldIndex, setSettingsFieldIndex] = useState(0);
  const [terminalRows, setTerminalRows] = useState<number>(Math.max(stdout?.rows ?? 24, 12));
  const [terminalColumns, setTerminalColumns] = useState<number>(Math.max(stdout?.columns ?? 80, 40));
  const [promptMode, setPromptMode] = useState<TUIPromptMode>("default");
  const [collaborationMode, setCollaborationMode] = useState<TUICollaborationMode>("default");

  const fetchSessions = useCallback(
    async (userID: string, channel: string): Promise<ChatSpec[]> => {
      return client.get<ChatSpec[]>(appendQuery("/chats", {
        user_id: userID.trim() || undefined,
        channel: channel.trim() || undefined,
      }));
    },
    [client],
  );

  const loadHistory = useCallback(
    async (chatID: string): Promise<void> => {
      if (!chatID) {
        setMessages([]);
        return;
      }
      setStatus(t("tui.status.loading_history"));
      const history = await client.get<ChatHistoryResponse>(fillPath("/chats/{chat_id}", { chat_id: chatID }));
      const list = Array.isArray(history?.messages) ? history.messages : [];
      setMessages(historyToViewMessages(list));
      setStatus(t("tui.status.ready"));
    },
    [client],
  );

  const applyChatModes = useCallback((chat: ChatSpec | null | undefined): void => {
    const resolved = resolveModesFromMeta(chat?.meta);
    setPromptMode(resolved.promptMode);
    setCollaborationMode(resolved.collaborationMode);
  }, []);

  const createSession = useCallback(
    async (preferredSessionID?: string): Promise<ChatSpec> => {
      const sessionID = preferredSessionID?.trim() || `s-${Date.now()}`;
      const created = await client.post<ChatSpec>("/chats", {
        name: t("tui.session.new"),
        session_id: sessionID,
        user_id: settings.userID,
        channel: settings.channel,
        meta: {
          prompt_mode: promptMode,
          collaboration_mode: collaborationMode,
        },
      });
      setSessions((prev) => [created, ...prev.filter((item) => item.id !== created.id)]);
      setActiveChatID(created.id);
      setActiveSessionID(created.session_id);
      applyChatModes(created);
      setStatus(t("tui.status.chat_created"));
      setMessages([]);
      return created;
    },
    [client, settings.userID, settings.channel, promptMode, collaborationMode, applyChatModes],
  );

  const refreshSessions = useCallback(async (): Promise<void> => {
    setStatus(t("tui.status.loading_sessions"));
    const list = await fetchSessions(settings.userID, settings.channel);
    setSessions(list);

    if (list.length === 0) {
      setActiveChatID("");
      setActiveSessionID("");
      applyChatModes(null);
      setMessages([]);
      setStatus(t("tui.status.no_sessions"));
      return;
    }

    const byActiveChat = list.find((item) => item.id === activeChatID);
    const bySession = activeSessionID ? list.find((item) => item.session_id === activeSessionID) : null;
    const next = byActiveChat ?? bySession ?? list[0];
    setActiveChatID(next.id);
    setActiveSessionID(next.session_id);
    applyChatModes(next);
    await loadHistory(next.id);
  }, [fetchSessions, settings.userID, settings.channel, activeChatID, activeSessionID, loadHistory, applyChatModes]);

  const openHistoryModal = useCallback(async (): Promise<void> => {
    setStatus(t("tui.status.loading_sessions"));
    const list = await fetchSessions(settings.userID, settings.channel);
    setSessions(list);
    if (list.length === 0) {
      setStatus(t("tui.history.empty"));
      setHistoryOpen(false);
      return;
    }
    const currentIndex = list.findIndex((item) => item.id === activeChatID);
    setHistorySelectionIndex(currentIndex >= 0 ? currentIndex : 0);
    setHistoryOpen(true);
    setStatus(t("tui.history.opened"));
  }, [fetchSessions, settings.userID, settings.channel, activeChatID]);

  const selectHistorySession = useCallback(async (): Promise<void> => {
    if (sessions.length === 0) {
      setHistoryOpen(false);
      return;
    }
    const index = Math.max(0, Math.min(sessions.length - 1, historySelectionIndex));
    const selected = sessions[index];
    if (!selected) {
      setHistoryOpen(false);
      return;
    }
    setActiveChatID(selected.id);
    setActiveSessionID(selected.session_id);
    applyChatModes(selected);
    setHistoryOpen(false);
    await loadHistory(selected.id);
    setStatus(t("tui.status.ready"));
  }, [sessions, historySelectionIndex, loadHistory, applyChatModes]);

  useEffect(() => {
    const onResize = () => {
      setTerminalRows(Math.max(stdout?.rows ?? 24, 12));
      setTerminalColumns(Math.max(stdout?.columns ?? 80, 40));
    };
    onResize();
    stdout.on("resize", onResize);
    return () => {
      stdout.off("resize", onResize);
    };
  }, [stdout]);

  useEffect(() => {
    if (sessions.length === 0) {
      setHistorySelectionIndex(0);
      return;
    }
    if (historySelectionIndex >= sessions.length) {
      setHistorySelectionIndex(sessions.length - 1);
    }
  }, [sessions.length, historySelectionIndex]);

  useEffect(() => {
    if (initialized.current) {
      return;
    }
    initialized.current = true;
    void (async () => {
      try {
        setStatus(t("tui.status.loading_sessions"));
        const list = await fetchSessions(settings.userID, settings.channel);
        setSessions(list);

        // Startup defaults to a brand new conversation.
        // Existing sessions are still accessible via /history.
        if (bootstrap.sessionID) {
          const selected = list.find((item) => item.session_id === bootstrap.sessionID) ?? (await createSession(bootstrap.sessionID));
          setActiveChatID(selected.id);
          setActiveSessionID(selected.session_id);
          applyChatModes(selected);
          await loadHistory(selected.id);
          return;
        }

        await createSession();
      } catch (err) {
        setErrorText(t("tui.error.fetch_failed", { message: errorToMessage(err) }));
        setStatus(t("tui.status.ready"));
      }
    })();
  }, [bootstrap.sessionID, createSession, fetchSessions, loadHistory, settings.channel, settings.userID, applyChatModes]);

  const sendMessage = useCallback(async (): Promise<void> => {
    if (streaming) {
      return;
    }
    const text = draft.trim();
    if (text === "") {
      return;
    }

    let expandedText = text;
    try {
      expandedText = await expandPromptTemplateIfNeeded(client, text);
    } catch (err) {
      setErrorText(t("tui.error.fetch_failed", { message: errorToMessage(err) }));
      setStatus(t("tui.status.ready"));
      return;
    }

    setDraft("");
    setStreaming(true);
    setErrorText("");
    setStatus(t("tui.status.sending"));

    let chatID = activeChatID;
    let sessionID = activeSessionID;
    try {
      if (!chatID || !sessionID) {
        const created = await createSession();
        chatID = created.id;
        sessionID = created.session_id;
      }

      setMessages((prev) => beginAssistantMessage(appendUserMessage(prev, expandedText)));
      const payload = {
        input: [{ role: "user", type: "message", content: [{ type: "text", text: expandedText }] }],
        session_id: sessionID,
        user_id: settings.userID,
        channel: settings.channel,
        stream: true,
        biz_params: buildProcessBizParams(promptMode, collaborationMode),
      };
      const res = await client.openStream("/agent/process", {
        method: "POST",
        body: payload,
        accept: "text/event-stream,application/json",
      });
      if (!res.body) {
        throw new Error("stream unsupported");
      }

      setStatus(t("tui.status.streaming"));
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) {
          break;
        }
        buffer += decoder.decode(value, { stream: true }).replace(/\r/g, "");
        const parsed = consumeSSEBuffer(buffer);
        buffer = parsed.rest;
        for (const rawEvent of parsed.events) {
          const event = parseAgentStreamData(rawEvent);
          if (event === "done" || event === null) {
            continue;
          }
          setMessages((prev) => applyAgentEvent(prev, event));
          if (event.type === "error") {
            const message = typeof event.meta?.message === "string" ? event.meta.message : "stream_error";
            setErrorText(t("tui.error.fetch_failed", { message }));
          }
        }
      }

      setMessages((prev) => settleAssistantMessage(prev));
      if (chatID) {
        await loadHistory(chatID);
      }
      await refreshSessions();
      setStatus(t("tui.status.ready"));
    } catch (err) {
      setMessages((prev) => settleAssistantMessage(prev));
      setErrorText(t("tui.error.fetch_failed", { message: errorToMessage(err) }));
      setStatus(t("tui.status.ready"));
    } finally {
      setStreaming(false);
    }
  }, [
    streaming,
    draft,
    activeChatID,
    activeSessionID,
    createSession,
    settings.userID,
    settings.channel,
    client,
    promptMode,
    collaborationMode,
    loadHistory,
    refreshSessions,
  ]);

  const updateActiveChatModeMeta = useCallback((nextPromptMode: TUIPromptMode, nextCollaborationMode: TUICollaborationMode): void => {
    if (!activeChatID) {
      return;
    }
    setSessions((prev) =>
      prev.map((chat) => {
        if (chat.id !== activeChatID) {
          return chat;
        }
        const baseMeta = chat.meta && typeof chat.meta === "object" && !Array.isArray(chat.meta)
          ? (chat.meta as Record<string, unknown>)
          : {};
        return {
          ...chat,
          meta: {
            ...baseMeta,
            prompt_mode: nextPromptMode,
            collaboration_mode: nextCollaborationMode,
          },
        };
      }),
    );
  }, [activeChatID]);

  const cyclePromptMode = useCallback((): void => {
    const next = nextPromptMode(promptMode);
    setPromptMode(next);
    updateActiveChatModeMeta(next, collaborationMode);
    setStatus(t("tui.status.prompt_mode_changed", { mode: t(promptModeLabelKey(next)) }));
  }, [promptMode, collaborationMode, updateActiveChatModeMeta]);

  const cycleCollaborationMode = useCallback((): void => {
    if (promptMode !== "codex") {
      setStatus(t("tui.status.collaboration_requires_codex"));
      return;
    }
    const next = normalizeCollaborationMode(nextCollaborationMode(collaborationMode));
    setCollaborationMode(next);
    updateActiveChatModeMeta(promptMode, next);
    setStatus(t("tui.status.collaboration_mode_changed", { mode: t(collaborationModeLabelKey(next)) }));
  }, [promptMode, collaborationMode, updateActiveChatModeMeta]);

  const applySettings = useCallback(async (): Promise<void> => {
    const nextLocale = resolveLocale(settingsDraft.locale);
    const nextSettings: TUISettings = {
      apiBase: settingsDraft.apiBase.trim() || settings.apiBase,
      apiKey: settingsDraft.apiKey.trim(),
      userID: settingsDraft.userID.trim() || settings.userID,
      channel: settingsDraft.channel.trim() || settings.channel,
      locale: nextLocale,
    };

    client.setBaseURL(nextSettings.apiBase);
    client.setAPIKey(nextSettings.apiKey);
    setLocale(nextLocale);
    setSettings(nextSettings);
    setSettingsDraft(nextSettings);
    setSettingsOpen(false);
    setStatus(t("tui.status.saved"));
    setErrorText("");

    try {
      const list = await fetchSessions(nextSettings.userID, nextSettings.channel);
      setSessions(list);
      if (list.length === 0) {
        setActiveChatID("");
        setActiveSessionID("");
        applyChatModes(null);
        setMessages([]);
        setStatus(t("tui.status.no_sessions"));
        return;
      }
      const selected = list[0];
      setActiveChatID(selected.id);
      setActiveSessionID(selected.session_id);
      applyChatModes(selected);
      await loadHistory(selected.id);
      setStatus(t("tui.status.ready"));
    } catch (err) {
      setErrorText(t("tui.error.fetch_failed", { message: errorToMessage(err) }));
      setStatus(t("tui.status.ready"));
    }
  }, [settingsDraft, settings, client, fetchSessions, loadHistory, applyChatModes]);

  useInput((input, key) => {
    if (historyOpen) {
      if (key.escape) {
        setHistoryOpen(false);
        setStatus(t("tui.status.ready"));
        return;
      }
      if (key.upArrow) {
        setHistorySelectionIndex((prev) => Math.max(0, prev - 1));
        return;
      }
      if (key.downArrow || key.tab) {
        setHistorySelectionIndex((prev) => Math.min(Math.max(sessions.length - 1, 0), prev + 1));
        return;
      }
      if (key.return) {
        void selectHistorySession().catch((err) => {
          setErrorText(t("tui.error.fetch_failed", { message: errorToMessage(err) }));
          setStatus(t("tui.status.ready"));
        });
      }
      return;
    }

    if (settingsOpen) {
      const field = settingFields[settingsFieldIndex];
      if (key.escape) {
        setSettingsDraft(settings);
        setSettingsOpen(false);
        return;
      }
      if (key.tab || key.downArrow) {
        setSettingsFieldIndex((prev) => (prev + 1) % settingFields.length);
        return;
      }
      if (key.upArrow) {
        setSettingsFieldIndex((prev) => (prev - 1 + settingFields.length) % settingFields.length);
        return;
      }
      if (key.return) {
        void applySettings();
        return;
      }
      if (isBackspaceInput(input, key)) {
        setSettingsDraft((prev) => ({
          ...prev,
          [field]: String(prev[field]).slice(0, -1),
        }));
        return;
      }
      if (field === "locale" && (key.leftArrow || key.rightArrow)) {
        setSettingsDraft((prev) => ({
          ...prev,
          locale: prev.locale === "zh-CN" ? "en-US" : "zh-CN",
        }));
        return;
      }
      if (!key.ctrl && !key.meta && input) {
        setSettingsDraft((prev) => ({
          ...prev,
          [field]: `${String(prev[field])}${input}`,
        }));
      }
      return;
    }

    if (key.ctrl && input === "c") {
      exit();
      return;
    }
    if (key.ctrl && input === "h") {
      void openHistoryModal().catch((err) => {
        setErrorText(t("tui.error.fetch_failed", { message: errorToMessage(err) }));
        setStatus(t("tui.status.ready"));
      });
      return;
    }
    if (key.ctrl && input === "n") {
      void createSession().catch((err) => {
        setErrorText(t("tui.error.fetch_failed", { message: errorToMessage(err) }));
      });
      return;
    }
    if (key.ctrl && input === "r") {
      void refreshSessions().catch((err) => {
        setErrorText(t("tui.error.fetch_failed", { message: errorToMessage(err) }));
      });
      return;
    }
    if (key.ctrl && input === ",") {
      setSettingsDraft(settings);
      setSettingsFieldIndex(0);
      setSettingsOpen(true);
      return;
    }
    if (key.ctrl && input === "p") {
      cyclePromptMode();
      return;
    }
    if (key.ctrl && input === "m") {
      cycleCollaborationMode();
      return;
    }
    if (key.return) {
      void sendMessage();
      return;
    }
    if (isBackspaceInput(input, key)) {
      setDraft((prev) => prev.slice(0, -1));
      return;
    }
    if (!key.ctrl && !key.meta && input) {
      setDraft((prev) => `${prev}${input}`);
    }
  });

  const historyVisibleCount = calcHistoryVisibleCount(terminalRows, historyOpen, settingsOpen, sessions.length);
  const chatContentWidth = Math.max(16, terminalColumns - 2);
  const lastMessage = messages.at(-1);
  const hasLiveMessage = Boolean(lastMessage && lastMessage.role === "assistant" && lastMessage.pending);
  const settledMessages = hasLiveMessage ? messages.slice(0, -1) : messages;
  const liveMessages = hasLiveMessage && lastMessage ? [lastMessage] : [];
  const staticChatLines = buildChatRenderLines(settledMessages, chatContentWidth);
  const liveChatLines = buildChatRenderLines(liveMessages, chatContentWidth).map((line) => ({
    ...line,
    key: `live-${line.key}`,
  }));
  const historyWindow = calcWindow(sessions.length, historySelectionIndex, historyVisibleCount);
  const visibleSessions = sessions.slice(historyWindow.start, historyWindow.end);
  const promptModeLabel = t(promptModeLabelKey(promptMode));
  const collaborationModeLabel = promptMode === "codex" ? t(collaborationModeLabelKey(collaborationMode)) : t("tui.mode.collaboration.na");
  const baseInfo = `${t("tui.settings.user_id")}: ${settings.userID} | ${t("tui.settings.channel")}: ${settings.channel} | ${t("tui.settings.api_base")}: ${settings.apiBase} | ${t("tui.settings.locale")}: ${settings.locale} | ${t("tui.mode.prompt.label")}: ${promptModeLabel} | ${t("tui.mode.collaboration.label")}: ${collaborationModeLabel}`;
  const infoLine = fitText(baseInfo, Math.max(24, terminalColumns - 2));

  return (
    <Box flexDirection="column" paddingX={1}>
      <Text bold>{t("tui.title")}</Text>
      <Text dimColor>{t("tui.shortcuts")}</Text>

      {staticChatLines.length === 0 && liveChatLines.length === 0 ? <Text dimColor>{t("tui.message.empty")}</Text> : null}
      {staticChatLines.length > 0 ? (
        <Static items={staticChatLines}>
          {(line) => (
            <Text key={line.key} color={line.color}>
              {line.text}
            </Text>
          )}
        </Static>
      ) : null}
      {liveChatLines.map((line) => (
        <Text key={line.key} color={line.color}>
          {line.text}
        </Text>
      ))}

      {settingsOpen ? (
        <Box marginTop={1} flexDirection="column" borderStyle="round" borderColor="magenta" paddingX={1}>
          <Text bold>{t("tui.panel.settings")}</Text>
          {settingFields.map((field, index) => {
            const selected = index === settingsFieldIndex;
            const value = settingsDraft[field];
            const renderedValue = field === "apiKey" && !selected ? maskAPIKey(String(value)) : String(value);
            return (
              <Text key={field} color={selected ? "yellow" : undefined}>
                {selected ? "> " : "  "}
                {t(settingLabelKeys[field])}: {renderedValue}
              </Text>
            );
          })}
          <Text dimColor>{t("tui.settings.hint")}</Text>
        </Box>
      ) : null}

      {historyOpen ? (
        <Box marginTop={1} flexDirection="column" borderStyle="double" borderColor="cyan" paddingX={1}>
          <Text bold>{t("tui.history.title")}</Text>
          {sessions.length === 0 ? (
            <Text dimColor>{t("tui.history.empty")}</Text>
          ) : (
            visibleSessions.map((session, index) => {
              const absoluteIndex = historyWindow.start + index;
              const selected = absoluteIndex === historySelectionIndex;
              return (
                <Text key={session.id} color={selected ? "yellow" : undefined}>
                  {selected ? "> " : "  "}
                  {sessionLabel(session)}
                </Text>
              );
            })
          )}
          <Text dimColor>{t("tui.history.hint")}</Text>
        </Box>
      ) : null}

      <Box marginTop={1} borderStyle="round" borderColor={streaming ? "yellow" : "gray"} paddingX={1}>
        <Text>{draft === "" ? `${t("tui.input.placeholder")}` : `> ${draft}`}</Text>
      </Box>

      <Box marginTop={1} flexDirection="column">
        <Text dimColor>{infoLine}</Text>
        <Text color={errorText ? "red" : "green"}>{errorText || status}</Text>
      </Box>
    </Box>
  );
}

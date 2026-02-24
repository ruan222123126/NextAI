import {
  CronWorkflowCanvas,
  createDefaultCronWorkflow,
  validateCronWorkflowSpec,
} from "./cron-workflow.js";
import { DEFAULT_LOCALE, getLocale, isWebMessageKey, setLocale, t } from "./i18n.js";
import { createChatFeature } from "./main/chat-feature.js";
import {
  createComposerSlashController,
  type LocalizedComposerSlashCommand,
} from "./main/composer-slash.js";
import { createCronFeature } from "./main/cron-feature.js";
import { createCustomSelectController } from "./main/custom-select.js";
import { createLogger } from "./main/logging.js";
import { createModelFeature } from "./main/model-feature.js";
import { createTransport } from "./main/transport.js";
import type {
  ActiveModelsInfo,
  AgentStreamEvent,
  AgentToolCallPayload,
  AgentToolResultPayload,
  ChannelsSettingsLevel,
  ChatHistoryResponse,
  ChatSpec,
  ComposerModelOption,
  CronDispatchSpec,
  CronDispatchTarget,
  CronJobSpec,
  CronJobState,
  CronModalMode,
  CronRuntimeSpec,
  CronScheduleSpec,
  CronWorkflowEdge,
  CronWorkflowExecution,
  CronWorkflowNode,
  CronWorkflowNodeExecution,
  CronWorkflowSpec,
  CronWorkflowViewport,
  DeleteResult,
  ModelCapabilities,
  ModelCatalogInfo,
  ModelInfo,
  ModelLimit,
  ModelModalities,
  ModelSlotConfig,
  ModelsSettingsLevel,
  PromptMode,
  ProviderInfo,
  ProviderKVKind,
  ProviderTypeInfo,
  QQAPIEnvironment,
  QQChannelConfig,
  QQTargetType,
  RuntimeContent,
  RuntimeMessage,
  UpsertProviderOptions,
  ViewMessage,
  ViewMessageTimelineEntry,
  ViewToolCallNotice,
  WorkspaceCardKey,
  WorkspaceCodexTreeNode,
  WorkspaceEditorMode,
  WorkspaceFileCatalog,
  WorkspaceFileInfo,
  WorkspaceSettingsLevel,
  WorkspaceTextPayload,
  WorkspaceUploadResponse,
} from "./main/types.js";
import { createWorkspaceFeature } from "./main/workspace-feature.js";
import { renderMarkdownToFragment } from "./markdown.js";

type TabKey = "chat" | "cron";
type SettingsSectionKey = "connection" | "identity" | "display" | "models" | "channels" | "workspace";
type I18nKey = Parameters<typeof t>[0];
type ComposerSlashGroup = "quick" | "template" | "mode";

const TRASH_ICON_SVG = `<svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path fill="currentColor" d="M7 21q-.825 0-1.412-.587T5 19V6H4V4h5V3h6v1h5v2h-1v13q0 .825-.587 1.413T17 21zM17 6H7v13h10zM9 17h2V8H9zm4 0h2V8h-2zM7 6v13z"/></svg>`;

interface PersistedSettings {
  apiBase?: unknown;
  apiKey?: unknown;
  workspaceCardEnabled?: unknown;
}

interface AgentSystemLayerInfo {
  name?: string;
  role?: string;
  source?: string;
  content_preview?: string;
  estimated_tokens?: number;
}

interface AgentSystemLayersResponse {
  version?: string;
  layers?: AgentSystemLayerInfo[];
  estimated_tokens_total?: number;
}

interface RuntimeConfigFeatureFlags {
  prompt_templates?: boolean;
  prompt_context_introspect?: boolean;
}

interface RuntimeConfigResponse {
  features?: RuntimeConfigFeatureFlags;
}

interface SystemPromptTokenScenario {
  promptMode: PromptMode;
  taskCommand: string;
  sessionID: string;
  cacheKey: string;
}

interface ComposerSlashCommandConfig {
  id: string;
  command: string;
  insertText: string;
  titleKey: I18nKey;
  descriptionKey: I18nKey;
  group: ComposerSlashGroup;
  keywords: string[];
}

const DEFAULT_API_BASE = "http://127.0.0.1:8088";
const DEFAULT_API_KEY = "";
const DEFAULT_USER_ID = "demo-user";
const DEFAULT_CHANNEL = "console";
const WEB_CHAT_CHANNEL = DEFAULT_CHANNEL;
const DEFAULT_CRON_JOB_ID = "cron-default";
const CRON_META_SYSTEM_DEFAULT = "system_default";
const QQ_CHANNEL = "qq";
const DEFAULT_QQ_API_BASE = "https://api.sgroup.qq.com";
const QQ_SANDBOX_API_BASE = "https://sandbox.api.sgroup.qq.com";
const DEFAULT_QQ_TOKEN_URL = "https://bots.qq.com/app/getAppAccessToken";
const DEFAULT_QQ_TIMEOUT_SECONDS = 8;
const CHAT_LIVE_REFRESH_INTERVAL_MS = 1500;
const PROVIDER_AUTO_SAVE_DELAY_MS = 900;
const AGENT_RAW_REQUEST_LOG_LABEL = "[NextAI][agent-process][raw-request]";
const AGENT_RAW_RESPONSE_LOG_LABEL = "[NextAI][agent-process][raw-response]";
const SCROLLBAR_ACTIVE_CLASS = "is-scrollbar-scrolling";
const SCROLLBAR_IDLE_HIDE_DELAY_MS = 520;
const DEFAULT_OPENAI_MODEL_IDS = ["gpt-4o-mini", "gpt-4.1-mini"];
const DEFAULT_MODEL_CONTEXT_LIMIT_TOKENS = 128000;
const PROMPT_TEMPLATE_PREFIX = "/prompts:";
const SYSTEM_PROMPT_LAYER_ENDPOINT = "/agent/system-layers";
const SYSTEM_PROMPT_WORKSPACE_FALLBACK_PATHS = ["prompts/AGENTS.md", "prompts/ai-tools.md"] as const;
const SYSTEM_PROMPT_WORKSPACE_PATH_SET = new Set(SYSTEM_PROMPT_WORKSPACE_FALLBACK_PATHS.map((path) => path.toLowerCase()));
const WORKSPACE_CODEX_PREFIX = "prompts/codex/";
const WORKSPACE_CLAUDE_PREFIX = "prompts/claude/";
const WORKSPACE_CARD_KEYS: WorkspaceCardKey[] = ["config", "prompt", "codex", "claude"];
const DEFAULT_WORKSPACE_CARD_ENABLED: Record<WorkspaceCardKey, boolean> = {
  config: true,
  prompt: true,
  codex: true,
  claude: true,
};
const PROMPT_TEMPLATE_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const PROMPT_TEMPLATE_ARG_KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;
const PROMPT_TEMPLATE_PLACEHOLDER_PATTERN = /\$([A-Za-z_][A-Za-z0-9_]*)/g;
const FEATURE_FLAG_PROMPT_TEMPLATES = "nextai.feature.prompt_templates";
const FEATURE_FLAG_PROMPT_CONTEXT_INTROSPECT = "nextai.feature.prompt_context_introspect";
const PROMPT_MODE_META_KEY = "prompt_mode";
const SETTINGS_KEY = "nextai.web.chat.settings";
const LOCALE_KEY = "nextai.web.locale";
const BOOT_READY_ATTRIBUTE = "data-nextai-boot-ready";
const BOOT_READY_EVENT = "nextai:boot-ready";
const BUILTIN_PROVIDER_IDS = new Set(["openai"]);
const COMPOSER_SLASH_COMMANDS: ComposerSlashCommandConfig[] = [
  {
    id: "shell",
    command: "/shell",
    insertText: "/shell ",
    titleKey: "chat.slashShellTitle",
    descriptionKey: "chat.slashShellDesc",
    group: "quick",
    keywords: ["shell", "command", "terminal", "tool", "执行", "命令"],
  },
  {
    id: "prompts-check-fix",
    command: "/prompts:check-fix",
    insertText: "/prompts:check-fix ",
    titleKey: "chat.slashCheckFixTitle",
    descriptionKey: "chat.slashCheckFixDesc",
    group: "template",
    keywords: ["prompts", "check", "fix", "repair", "修复", "检查"],
  },
  {
    id: "prompts-refactor",
    command: "/prompts:refactor",
    insertText: "/prompts:refactor ",
    titleKey: "chat.slashRefactorTitle",
    descriptionKey: "chat.slashRefactorDesc",
    group: "template",
    keywords: ["prompts", "refactor", "cleanup", "重构", "整理"],
  },
  {
    id: "prompts-human-readable",
    command: "/prompts:human-readable",
    insertText: "/prompts:human-readable ",
    titleKey: "chat.slashHumanReadableTitle",
    descriptionKey: "chat.slashHumanReadableDesc",
    group: "template",
    keywords: ["prompts", "human", "readable", "rewrite", "转译", "人话", "易读"],
  },
  {
    id: "new-session",
    command: "/new",
    insertText: "/new",
    titleKey: "chat.slashNewSessionTitle",
    descriptionKey: "chat.slashNewSessionDesc",
    group: "quick",
    keywords: ["new", "session", "chat", "清理", "新会话"],
  },
  {
    id: "review",
    command: "/review",
    insertText: "/review ",
    titleKey: "chat.slashReviewTitle",
    descriptionKey: "chat.slashReviewDesc",
    group: "mode",
    keywords: ["review", "audit", "quality", "代码审查", "风险"],
  },
  {
    id: "compact",
    command: "/compact",
    insertText: "/compact ",
    titleKey: "chat.slashCompactTitle",
    descriptionKey: "chat.slashCompactDesc",
    group: "mode",
    keywords: ["compact", "compress", "summary", "压缩", "总结"],
  },
  {
    id: "memory",
    command: "/memory",
    insertText: "/memory ",
    titleKey: "chat.slashMemoryTitle",
    descriptionKey: "chat.slashMemoryDesc",
    group: "mode",
    keywords: ["memory", "remember", "recall", "记忆", "回忆"],
  },
  {
    id: "plan",
    command: "/plan",
    insertText: "/plan ",
    titleKey: "chat.slashPlanTitle",
    descriptionKey: "chat.slashPlanDesc",
    group: "mode",
    keywords: ["plan", "roadmap", "step", "计划", "拆解"],
  },
  {
    id: "execute",
    command: "/execute",
    insertText: "/execute ",
    titleKey: "chat.slashExecuteTitle",
    descriptionKey: "chat.slashExecuteDesc",
    group: "mode",
    keywords: ["execute", "deliver", "implement", "执行", "落地"],
  },
  {
    id: "pair-programming",
    command: "/pair_programming",
    insertText: "/pair_programming ",
    titleKey: "chat.slashPairProgrammingTitle",
    descriptionKey: "chat.slashPairProgrammingDesc",
    group: "mode",
    keywords: ["pair", "pair programming", "collaborate", "结对", "协作"],
  },
  {
    id: "status",
    command: "/status",
    insertText: "/status",
    titleKey: "chat.slashStatusTitle",
    descriptionKey: "chat.slashStatusDesc",
    group: "quick",
    keywords: ["status", "state", "progress", "状态", "进度"],
  },
];
const TABS: TabKey[] = ["chat", "cron"];
const scrollbarActivityTimers = new WeakMap<HTMLElement, number>();
let chatLiveRefreshTimer: number | null = null;
let chatLiveRefreshInFlight = false;
let activeSettingsSection: SettingsSectionKey = "models";
let systemPromptTokensLoaded = false;
let systemPromptTokensInFlight: Promise<void> | null = null;
let systemPromptTokensInFlightScenarioKey = "";
let systemPromptTokens = 0;
let systemPromptTokensScenarioKey = "";
const runtimeFlags: Required<RuntimeConfigFeatureFlags> = {
  prompt_templates: false,
  prompt_context_introspect: false,
};

const apiBaseInput = mustElement<HTMLInputElement>("api-base");
const apiKeyInput = mustElement<HTMLInputElement>("api-key");
const localeSelect = mustElement<HTMLSelectElement>("locale-select");
const promptContextIntrospectInput = mustElement<HTMLInputElement>("feature-prompt-context-introspect");
const reloadChatsButton = mustElement<HTMLButtonElement>("reload-chats");
const settingsToggleButton = mustElement<HTMLButtonElement>("settings-toggle");
const settingsPopover = mustElement<HTMLElement>("settings-popover");
const settingsPopoverCloseButton = mustElement<HTMLButtonElement>("settings-popover-close");
const chatCronToggleButton = mustElement<HTMLButtonElement>("chat-cron-toggle");
const chatSearchToggleButton = mustElement<HTMLButtonElement>("chat-search-toggle");
const searchModal = mustElement<HTMLElement>("search-modal");
const searchModalCloseButton = mustElement<HTMLButtonElement>("search-modal-close-btn");
const modelsSettingsSection = mustElement<HTMLElement>("settings-section-models");
const channelsSettingsSection = mustElement<HTMLElement>("settings-section-channels");
const workspaceSettingsSection = mustElement<HTMLElement>("settings-section-workspace");
const settingsSectionButtons = Array.from(document.querySelectorAll<HTMLButtonElement>("[data-settings-section]"));
const settingsSectionPanels = Array.from(document.querySelectorAll<HTMLElement>("[data-settings-section-panel]"));
const statusLine = mustElement<HTMLElement>("status-line");

const tabButtons = Array.from(document.querySelectorAll<HTMLButtonElement>(".tab-btn"));

const panelChat = mustElement<HTMLElement>("panel-chat");
const panelCron = mustElement<HTMLElement>("panel-cron");

const newChatButton = mustElement<HTMLButtonElement>("new-chat");
const chatList = mustElement<HTMLUListElement>("chat-list");
const chatTitle = mustElement<HTMLElement>("chat-title");
const chatSession = mustElement<HTMLElement>("chat-session");
const chatPromptModeSelect = mustElement<HTMLSelectElement>("chat-prompt-mode-select");
const searchChatInput = mustElement<HTMLInputElement>("search-chat-input");
const searchChatResults = mustElement<HTMLUListElement>("search-chat-results");
const messageList = mustElement<HTMLUListElement>("message-list");
const thinkingIndicator = mustElement<HTMLElement>("thinking-indicator");
const composerForm = mustElement<HTMLFormElement>("composer");
const composerMain = mustElement<HTMLElement>("composer-main");
const messageInput = mustElement<HTMLTextAreaElement>("message-input");
const composerSlashPanel = mustElement<HTMLElement>("composer-slash-panel");
const composerSlashList = mustElement<HTMLUListElement>("composer-slash-list");
const sendButton = mustElement<HTMLButtonElement>("send-btn");
const composerAttachButton = mustElement<HTMLButtonElement>("composer-attach-btn");
const composerAttachInput = mustElement<HTMLInputElement>("composer-attach-input");
const composerProviderSelect = mustElement<HTMLSelectElement>("composer-provider-select");
const composerModelSelect = mustElement<HTMLSelectElement>("composer-model-select");
const composerTokenEstimate = mustElement<HTMLElement>("composer-token-estimate");

const refreshModelsButton = mustElement<HTMLButtonElement>("refresh-models");
const modelsAddProviderButton = mustElement<HTMLButtonElement>("models-add-provider-btn");
const modelsProviderList = mustElement<HTMLUListElement>("models-provider-list");
const modelsLevel1View = mustElement<HTMLElement>("models-level1-view");
const modelsLevel2View = mustElement<HTMLElement>("models-level2-view");
const modelsEditProviderMeta = mustElement<HTMLElement>("models-edit-provider-meta");
const modelsProviderModalTitle = mustElement<HTMLElement>("models-provider-modal-title");
const modelsProviderForm = mustElement<HTMLFormElement>("models-provider-form");
const modelsProviderTypeSelect = mustElement<HTMLSelectElement>("models-provider-type-select");
const modelsProviderNameInput = mustElement<HTMLInputElement>("models-provider-name-input");
const modelsProviderAPIKeyInput = mustElement<HTMLInputElement>("models-provider-api-key-input");
const modelsProviderAPIKeyVisibilityButton = mustElement<HTMLButtonElement>("models-provider-api-key-visibility-btn");
const modelsProviderBaseURLInput = mustElement<HTMLInputElement>("models-provider-base-url-input");
const modelsProviderBaseURLPreview = mustElement<HTMLElement>("models-provider-base-url-preview");
const modelsProviderTimeoutMSInput = mustElement<HTMLInputElement>("models-provider-timeout-ms-input");
const modelsProviderReasoningEffortField = mustElement<HTMLElement>("models-provider-reasoning-effort-field");
const modelsProviderReasoningEffortSelect = mustElement<HTMLSelectElement>("models-provider-reasoning-effort-select");
const modelsProviderEnabledInput = mustElement<HTMLInputElement>("models-provider-enabled-input");
const modelsProviderStoreField = mustElement<HTMLElement>("models-provider-store-field");
const modelsProviderStoreInput = mustElement<HTMLInputElement>("models-provider-store-input");
const modelsProviderHeadersRows = mustElement<HTMLElement>("models-provider-headers-rows");
const modelsProviderHeadersAddButton = mustElement<HTMLButtonElement>("models-provider-headers-add-btn");
const modelsProviderAliasesRows = mustElement<HTMLElement>("models-provider-aliases-rows");
const modelsProviderAliasesAddButton = mustElement<HTMLButtonElement>("models-provider-aliases-add-btn");
const modelsProviderCustomModelsField = mustElement<HTMLElement>("models-provider-custom-models-field");
const modelsProviderCustomModelsRows = mustElement<HTMLElement>("models-provider-custom-models-rows");
const modelsProviderCustomModelsAddButton = mustElement<HTMLButtonElement>("models-provider-custom-models-add-btn");
const modelsProviderCancelButton = mustElement<HTMLButtonElement>("models-provider-cancel-btn");

const refreshWorkspaceButton = mustElement<HTMLButtonElement>("refresh-workspace");
const workspaceImportOpenButton = mustElement<HTMLButtonElement>("workspace-import-open-btn");
const channelsEntryList = mustElement<HTMLUListElement>("channels-entry-list");
const channelsLevel1View = mustElement<HTMLElement>("channels-level1-view");
const channelsLevel2View = mustElement<HTMLElement>("channels-level2-view");
const qqChannelForm = mustElement<HTMLFormElement>("qq-channel-form");
const qqChannelEnabledInput = mustElement<HTMLInputElement>("qq-channel-enabled");
const qqChannelAppIDInput = mustElement<HTMLInputElement>("qq-channel-app-id");
const qqChannelClientSecretInput = mustElement<HTMLInputElement>("qq-channel-client-secret");
const qqChannelBotPrefixInput = mustElement<HTMLInputElement>("qq-channel-bot-prefix");
const qqChannelTargetTypeSelect = mustElement<HTMLSelectElement>("qq-channel-target-type");
const qqChannelAPIEnvironmentSelect = mustElement<HTMLSelectElement>("qq-channel-api-env");
const qqChannelTimeoutSecondsInput = mustElement<HTMLInputElement>("qq-channel-timeout-seconds");
const workspaceEntryList = mustElement<HTMLUListElement>("workspace-entry-list");
const workspaceLevel1View = mustElement<HTMLElement>("workspace-level1-view");
const workspaceLevel2ConfigView = mustElement<HTMLElement>("workspace-level2-config-view");
const workspaceLevel2PromptView = mustElement<HTMLElement>("workspace-level2-prompt-view");
const workspaceLevel2CodexView = mustElement<HTMLElement>("workspace-level2-codex-view");
const workspaceLevel2ClaudeView = mustElement<HTMLElement>("workspace-level2-claude-view");
const workspaceFilesBody = mustElement<HTMLUListElement>("workspace-files-body");
const workspacePromptsBody = mustElement<HTMLUListElement>("workspace-prompts-body");
const workspaceCodexTreeBody = mustElement<HTMLUListElement>("workspace-codex-tree-body");
const workspaceClaudeBody = mustElement<HTMLUListElement>("workspace-claude-body");
const workspaceEditorModal = mustElement<HTMLElement>("workspace-editor-modal");
const workspaceEditorModalCloseButton = mustElement<HTMLButtonElement>("workspace-editor-modal-close-btn");
const workspaceImportModal = mustElement<HTMLElement>("workspace-import-modal");
const workspaceImportModalCloseButton = mustElement<HTMLButtonElement>("workspace-import-modal-close-btn");
const workspaceEditorForm = mustElement<HTMLFormElement>("workspace-editor-form");
const workspaceFilePathInput = mustElement<HTMLInputElement>("workspace-file-path");
const workspaceFileContentInput = mustElement<HTMLTextAreaElement>("workspace-file-content");
const workspaceSaveFileButton = mustElement<HTMLButtonElement>("workspace-save-file-btn");
const workspaceDeleteFileButton = mustElement<HTMLButtonElement>("workspace-delete-file-btn");
const workspaceImportForm = mustElement<HTMLFormElement>("workspace-import-form");
const workspaceJSONInput = mustElement<HTMLTextAreaElement>("workspace-json");

const refreshCronButton = mustElement<HTMLButtonElement>("refresh-cron");
const cronChatToggleButton = mustElement<HTMLButtonElement>("cron-chat-toggle");
const cronWorkbench = mustElement<HTMLElement>("cron-workbench");
const cronJobsBody = mustElement<HTMLUListElement>("cron-jobs-body");
const cronCreateOpenButton = mustElement<HTMLButtonElement>("cron-create-open-btn");
const cronCreateModal = mustElement<HTMLElement>("cron-create-modal");
const cronCreateModalTitle = mustElement<HTMLElement>("cron-create-modal-title");
const cronCreateModalCloseButton = mustElement<HTMLButtonElement>("cron-create-modal-close-btn");
const cronCreateForm = mustElement<HTMLFormElement>("cron-create-form");
const cronDispatchHint = mustElement<HTMLElement>("cron-dispatch-hint");
const cronIDInput = mustElement<HTMLInputElement>("cron-id");
const cronNameInput = mustElement<HTMLInputElement>("cron-name");
const cronIntervalInput = mustElement<HTMLInputElement>("cron-interval");
const cronSessionIDInput = mustElement<HTMLInputElement>("cron-session-id");
const cronMaxConcurrencyInput = mustElement<HTMLInputElement>("cron-max-concurrency");
const cronTimeoutInput = mustElement<HTMLInputElement>("cron-timeout-seconds");
const cronMisfireInput = mustElement<HTMLInputElement>("cron-misfire-grace");
const cronTaskTypeSelect = mustElement<HTMLSelectElement>("cron-task-type");
const cronTextInput = mustElement<HTMLTextAreaElement>("cron-text");
const cronTextSection = mustElement<HTMLElement>("cron-text-section");
const cronWorkflowSection = mustElement<HTMLElement>("cron-workflow-section");
const cronResetWorkflowButton = mustElement<HTMLButtonElement>("cron-reset-workflow");
const cronWorkflowFullscreenButton = mustElement<HTMLButtonElement>("cron-workflow-fullscreen-btn");
const cronWorkflowViewport = mustElement<HTMLElement>("cron-workflow-viewport");
const cronWorkflowCanvas = mustElement<HTMLElement>("cron-workflow-canvas");
const cronWorkflowEdges = mustElement<SVGSVGElement>("cron-workflow-edges");
const cronWorkflowNodes = mustElement<HTMLElement>("cron-workflow-nodes");
const cronWorkflowNodeEditor = mustElement<HTMLElement>("cron-workflow-node-editor");
const cronWorkflowZoom = mustElement<HTMLElement>("cron-workflow-zoom");
const cronWorkflowExecutionList = mustElement<HTMLUListElement>("cron-workflow-execution-list");
const cronNewSessionButton = mustElement<HTMLButtonElement>("cron-new-session");
const cronSubmitButton = mustElement<HTMLButtonElement>("cron-submit-btn");

const panelByTab: Record<TabKey, HTMLElement> = {
  chat: panelChat,
  cron: panelCron,
};

function resolveLocalizedComposerSlashCommands(): LocalizedComposerSlashCommand[] {
  const resolveGroupLabel = (group: ComposerSlashGroup): string => {
    if (group === "template") {
      return t("chat.slashGroupTemplate");
    }
    if (group === "mode") {
      return t("chat.slashGroupMode");
    }
    return t("chat.slashGroupQuick");
  };
  return COMPOSER_SLASH_COMMANDS.map((command) => ({
    id: command.id,
    command: command.command,
    insertText: command.insertText,
    title: t(command.titleKey),
    description: t(command.descriptionKey),
    groupLabel: resolveGroupLabel(command.group),
    keywords: command.keywords,
  }));
}

const customSelectController = createCustomSelectController({
  translate: (key) => t(key as I18nKey),
});
const {
  initCustomSelects,
  syncCustomSelect,
  syncAllCustomSelects,
} = customSelectController;

const composerSlashController = createComposerSlashController({
  messageInput,
  composerSlashPanel,
  composerSlashList,
  getCommands: () => resolveLocalizedComposerSlashCommands(),
  resolveEmptyText: () => t("chat.slashPanelEmpty"),
  onApplied: () => {
    renderComposerTokenEstimate();
  },
});

const state = {
  apiBase: DEFAULT_API_BASE,
  apiKey: DEFAULT_API_KEY,
  userId: DEFAULT_USER_ID,
  channel: DEFAULT_CHANNEL,
  activeTab: "chat" as TabKey,
  tabLoaded: {
    chat: true,
    models: false,
    channels: false,
    workspace: false,
    cron: false,
  },

  chats: [] as ChatSpec[],
  chatSearchQuery: "",
  activeChatId: null as string | null,
  activeSessionId: newSessionID(),
  activePromptMode: "default" as PromptMode,
  messages: [] as ViewMessage[],
  messageOutputOrder: 0,
  sending: false,

  providers: [] as ProviderInfo[],
  providerTypes: [] as ProviderTypeInfo[],
  modelDefaults: {} as Record<string, string>,
  activeLLM: { provider_id: "", model: "" } as ModelSlotConfig,
  selectedProviderID: "",
  modelsSettingsLevel: "list" as ModelsSettingsLevel,
  channelsSettingsLevel: "list" as ChannelsSettingsLevel,
  workspaceSettingsLevel: "list" as WorkspaceSettingsLevel,
  workspaceCardEnabled: { ...DEFAULT_WORKSPACE_CARD_ENABLED },
  providerAPIKeyVisible: true,
  providerModal: {
    open: false,
    mode: "create" as "create" | "edit",
    editingProviderID: "",
  },
  workspaceFileCatalog: {
    files: [] as WorkspaceFileInfo[],
    configFiles: [] as WorkspaceFileInfo[],
    promptFiles: [] as WorkspaceFileInfo[],
    codexFiles: [] as WorkspaceFileInfo[],
    claudeFiles: [] as WorkspaceFileInfo[],
    codexTree: [] as WorkspaceCodexTreeNode[],
    codexRootFiles: [] as WorkspaceFileInfo[],
    codexFolderPaths: new Set<string>(),
    codexTopLevelFolderPaths: new Set<string>(),
    claudeTree: [] as WorkspaceCodexTreeNode[],
    claudeRootFiles: [] as WorkspaceFileInfo[],
    claudeFolderPaths: new Set<string>(),
    claudeTopLevelFolderPaths: new Set<string>(),
  },
  workspaceCodexExpandedFolders: new Set<string>(),
  workspaceClaudeExpandedFolders: new Set<string>(),
  qqChannelConfig: {
    enabled: false,
    app_id: "",
    client_secret: "",
    bot_prefix: "",
    target_type: "c2c" as QQTargetType,
    target_id: "",
    api_base: DEFAULT_QQ_API_BASE,
    token_url: DEFAULT_QQ_TOKEN_URL,
    timeout_seconds: DEFAULT_QQ_TIMEOUT_SECONDS,
  },
  qqChannelAvailable: true,
  activeWorkspacePath: "",
  activeWorkspaceContent: "",
  activeWorkspaceMode: "json" as WorkspaceEditorMode,
  cronJobs: [] as CronJobSpec[],
  cronStates: {} as Record<string, CronJobState>,
  cronModal: {
    mode: "create" as CronModalMode,
    editingJobID: "",
  },
  cronDraftTaskType: "workflow" as "text" | "workflow",
};

const transport = createTransport({
  getApiBase: () => state.apiBase,
  getApiKey: () => state.apiKey,
  getLocale,
});
const { requestJSON, openStream } = transport;

const logger = createLogger({
  statusLine,
  rawRequestLabel: AGENT_RAW_REQUEST_LOG_LABEL,
  rawResponseLabel: AGENT_RAW_RESPONSE_LOG_LABEL,
  resolveComposerStatus: () => ({
    statusLocal: t("chat.statusLocal"),
    statusFullAccess: t("chat.statusFullAccess"),
  }),
});
const { setStatus, logComposerStatusToConsole, logAgentRawRequest, logAgentRawResponse } = logger;

const workspaceFeature = createWorkspaceFeature({
  state,
  t,
  setStatus,
  setWorkspaceEditorModalOpen,
  setWorkspaceImportModalOpen,
  refreshWorkspaceButton,
  workspaceSettingsSection,
  workspaceImportOpenButton,
  workspaceEditorModal,
  workspaceEditorModalCloseButton,
  workspaceImportModal,
  workspaceImportModalCloseButton,
  workspaceFilesBody,
  workspacePromptsBody,
  workspaceClaudeBody,
  workspaceCodexTreeBody,
  workspaceEditorForm,
  workspaceDeleteFileButton,
  workspaceFilePathInput,
  workspaceImportForm,
  domainContext: {
    state,
    t,
    setStatus,
    syncControlState,
    asWorkspaceErrorMessage,
    requestJSON,
    normalizeWorkspacePathKey,
    isSystemPromptWorkspacePath,
    invalidateSystemPromptTokensCacheAndReload,
    appendEmptyItem,
    setWorkspaceEditorModalOpen,
    setWorkspaceImportModalOpen,
    DEFAULT_WORKSPACE_CARD_ENABLED,
    WORKSPACE_CARD_KEYS,
    WORKSPACE_CODEX_PREFIX,
    WORKSPACE_CLAUDE_PREFIX,
    TRASH_ICON_SVG,
    workspaceEntryList,
    workspaceLevel1View,
    workspaceLevel2ConfigView,
    workspaceLevel2PromptView,
    workspaceLevel2CodexView,
    workspaceLevel2ClaudeView,
    workspaceSettingsSection,
    workspaceFilesBody,
    workspacePromptsBody,
    workspaceCodexTreeBody,
    workspaceClaudeBody,
    workspaceFilePathInput,
    workspaceFileContentInput,
    workspaceSaveFileButton,
    workspaceDeleteFileButton,
    workspaceJSONInput,
  },
});
const {
  setWorkspaceSettingsLevel,
  parseWorkspaceCardEnabled,
  refreshWorkspace,
  renderWorkspacePanel,
  clearWorkspaceSelection,
  isWorkspaceSkillPath,
  normalizeWorkspaceInputPath,
  requestWorkspaceFile,
} = workspaceFeature.actions;

const modelFeature = createModelFeature({
  state,
  refreshModelsButton,
  modelsAddProviderButton,
  modelsProviderForm,
  modelsProviderHeadersAddButton,
  modelsProviderAliasesAddButton,
  modelsProviderTypeSelect,
  modelsProviderBaseURLInput,
  modelsProviderCustomModelsAddButton,
  modelsProviderCancelButton,
  modelsProviderList,
  modelsProviderHeadersRows,
  modelsProviderAliasesRows,
  modelsProviderCustomModelsRows,
  channelsEntryList,
  qqChannelForm,
  qqChannelEnabledInput,
  domainContext: {
    state,
    t,
    setStatus,
    asErrorMessage,
    requestJSON,
    syncControlState,
    syncCustomSelect,
    appendEmptyItem,
    formatModelEntry,
    formatCapabilities,
    formatProviderLabel,
    normalizeProviders,
    normalizeDefaults,
    buildDefaultMapFromProviders,
    normalizeModelSlot,
    parseIntegerInput,
    renderComposerTokenEstimate,
    toRecord,
    TRASH_ICON_SVG,
    QQ_CHANNEL,
    DEFAULT_QQ_API_BASE,
    QQ_SANDBOX_API_BASE,
    DEFAULT_QQ_TOKEN_URL,
    DEFAULT_QQ_TIMEOUT_SECONDS,
    DEFAULT_OPENAI_MODEL_IDS,
    BUILTIN_PROVIDER_IDS,
    PROVIDER_AUTO_SAVE_DELAY_MS,
    composerProviderSelect,
    composerModelSelect,
    modelsSettingsSection,
    channelsSettingsSection,
    channelsEntryList,
    channelsLevel1View,
    channelsLevel2View,
    qqChannelEnabledInput,
    qqChannelAppIDInput,
    qqChannelClientSecretInput,
    qqChannelBotPrefixInput,
    qqChannelTargetTypeSelect,
    qqChannelAPIEnvironmentSelect,
    qqChannelTimeoutSecondsInput,
    modelsProviderAPIKeyInput,
    modelsProviderAPIKeyVisibilityButton,
    modelsProviderBaseURLInput,
    modelsProviderBaseURLPreview,
    modelsLevel1View,
    modelsLevel2View,
    modelsEditProviderMeta,
    modelsProviderList,
    modelsProviderTypeSelect,
    modelsProviderNameInput,
    modelsProviderTimeoutMSInput,
    modelsProviderReasoningEffortField,
    modelsProviderReasoningEffortSelect,
    modelsProviderEnabledInput,
    modelsProviderStoreField,
    modelsProviderStoreInput,
    modelsProviderHeadersRows,
    modelsProviderAliasesRows,
    modelsProviderCustomModelsField,
    modelsProviderCustomModelsRows,
    modelsProviderCustomModelsAddButton,
    modelsProviderModalTitle,
  },
});
const {
  refreshModels,
  syncModelState,
  renderComposerModelSelectors,
  handleComposerProviderSelectChange,
  handleComposerModelSelectChange,
  setActiveModel,
  renderModelsPanel,
  setChannelsSettingsLevel,
  renderChannelsPanel,
  setProviderAPIKeyVisibility,
  renderProviderBaseURLPreview,
  setModelsSettingsLevel,
  renderModelsSettingsLevel,
  syncSelectedProviderID,
  renderProviderTypeOptions,
  resetProviderModalForm,
  openProviderModal,
  closeProviderModal,
  upsertProvider,
  deleteProvider,
  appendProviderKVRow,
  appendCustomModelRow,
  syncProviderCustomModelsField,
  scheduleProviderAutoSave,
  refreshQQChannelConfig,
  saveQQChannelConfig,
  defaultQQChannelConfig,
  normalizeQQChannelConfig,
  resolveQQAPIEnvironment,
} = modelFeature.actions;

const chatFeature = createChatFeature({
  state,
  t,
  setStatus,
  renderComposerTokenEstimate,
  syncControlState,
  getModelActions: () => ({
    handleComposerProviderSelectChange,
    handleComposerModelSelectChange,
  }),
  reloadChatsButton,
  newChatButton,
  chatPromptModeSelect,
  composerForm,
  composerMain,
  messageInput,
  sendButton,
  composerAttachButton,
  composerAttachInput,
  composerProviderSelect,
  composerModelSelect,
  composerSlashPanel,
  composerSlashList,
  composerSlashController,
  domainContext: {
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
    requestWorkspaceFile: (path: string) => requestWorkspaceFile(path),
    extractWorkspaceFileText,
    renderComposerTokenEstimate,
    syncControlState,
    appendEmptyItem,
    newSessionID,
    setSearchModalOpen,
    getBootstrapTask: () => bootstrapTask,
    hideComposerSlashPanel: () => {
      composerSlashController.hide();
    },
    renderComposerSlashPanel: () => {
      composerSlashController.render();
    },
    parsePositiveInteger,
    resetComposerFileDragDepth: () => {},
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
  },
});
const {
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
  hideComposerSlashPanel,
  renderComposerSlashPanel,
} = chatFeature.actions;

const cronFeature = createCronFeature({
  domainContext: {
    state,
    t,
    setStatus,
    asErrorMessage,
    syncControlState,
    syncCustomSelect,
    requestJSON,
    reloadChats,
    compactTime,
    newSessionID,
    createDefaultCronWorkflow,
    validateCronWorkflowSpec,
    CronWorkflowCanvas,
    DEFAULT_CRON_JOB_ID,
    CRON_META_SYSTEM_DEFAULT,
    parseIntegerInput,
    cronCreateOpenButton,
    cronCreateModal,
    cronCreateModalCloseButton,
    cronTaskTypeSelect,
    cronIDInput,
    cronNameInput,
    cronIntervalInput,
    cronSessionIDInput,
    cronMaxConcurrencyInput,
    cronTimeoutInput,
    cronMisfireInput,
    cronTextInput,
    cronResetWorkflowButton,
    cronWorkflowFullscreenButton,
    refreshCronButton,
    cronNewSessionButton,
    cronCreateForm,
    cronJobsBody,
    cronDispatchHint,
    cronCreateModalTitle,
    cronSubmitButton,
    cronTextSection,
    cronWorkflowSection,
    cronWorkflowViewport,
    cronWorkflowCanvas,
    cronWorkflowEdges,
    cronWorkflowNodes,
    cronWorkflowNodeEditor,
    cronWorkflowZoom,
    cronWorkflowExecutionList,
    cronWorkbench,
  },
});
const {
  isCronCreateModalOpen,
  setCronCreateModalOpen,
  supportsNativeCronWorkflowFullscreen,
  isCronWorkflowNativeFullscreen,
  isCronWorkflowFullscreenActive,
  syncCronWorkflowFullscreenUI,
  setCronWorkflowPseudoFullscreen,
  enterCronWorkflowFullscreen,
  exitCronWorkflowFullscreen,
  toggleCronWorkflowFullscreen,
  setCronModalMode,
  initCronWorkflowEditor,
  refreshCronWorkflowLabels,
  syncCronTaskModeUI,
  refreshCronModalTitles,
  renderCronExecutionDetails,
  syncCronDispatchHint,
  ensureCronSessionID,
  openCronEditModal,
  refreshCronJobs,
  renderCronJobs,
  saveCronJob,
  updateCronJobEnabled,
  deleteCronJob,
  runCronJob,
} = cronFeature.actions;

const featureModules = [chatFeature, modelFeature, workspaceFeature, cronFeature];
window.addEventListener("beforeunload", () => {
  for (const feature of featureModules) {
    feature.dispose();
  }
});

setBootReady(false);
const bootstrapTask = bootstrap();

async function bootstrap(): Promise<void> {
  setBootReady(false);
  initLocale();
  restoreSettings();
  await loadRuntimeConfig();
  bindEvents();
  initAutoHideScrollbars();
  initCronWorkflowEditor();
  initCustomSelects();
  setSettingsPopoverOpen(false);
  setSearchModalOpen(false);
  setActiveSettingsSection(activeSettingsSection);
  setWorkspaceEditorModalOpen(false);
  setWorkspaceImportModalOpen(false);
  setCronCreateModalOpen(false);
  applyLocaleToDocument();
  renderTabPanels();
  renderChatHeader();
  renderChatList();
  renderSearchChatResults();
  renderMessages();
  setThinkingIndicatorVisible(false);
  renderComposerModelSelectors();
  renderComposerTokenEstimate();
  void ensureSystemPromptTokensLoaded();
  renderChannelsPanel();
  renderWorkspacePanel();
  syncCronDispatchHint();
  ensureCronSessionID();
  resetProviderModalForm();
  try {
    await syncModelStateOnBoot();
  } finally {
    setBootReady(true);
  }
  ensureChatLiveRefreshLoop();

  setStatus(t("status.loadingChats"), "info");
  await reloadChats();
  if (state.chats.length > 0) {
    await openChat(state.chats[0].id);
    setStatus(t("status.loadedRecentChat"), "info");
    return;
  }
  startDraftSession();
  setStatus(t("status.noChatsDraft"), "info");
}

function setBootReady(ready: boolean): void {
  const root = document.body ?? document.documentElement;
  root.setAttribute(BOOT_READY_ATTRIBUTE, ready ? "true" : "false");
  window.dispatchEvent(
    new CustomEvent(BOOT_READY_EVENT, {
      detail: {
        ready,
      },
    }),
  );
}

function initAutoHideScrollbars(): void {
  document.addEventListener(
    "scroll",
    (event) => {
      const target = resolveScrollEventTarget(event.target);
      if (!target) {
        return;
      }
      markScrollbarScrolling(target);
    },
    { capture: true, passive: true },
  );

  window.addEventListener(
    "scroll",
    () => {
      const root = (document.scrollingElement ?? document.documentElement) as HTMLElement | null;
      if (!root) {
        return;
      }
      markScrollbarScrolling(root);
    },
    { passive: true },
  );
}

function resolveScrollEventTarget(target: EventTarget | null): HTMLElement | null {
  if (target instanceof HTMLElement) {
    return target;
  }
  if (target instanceof Document) {
    return (target.scrollingElement ?? target.documentElement) as HTMLElement | null;
  }
  return null;
}

function markScrollbarScrolling(element: HTMLElement): void {
  element.classList.add(SCROLLBAR_ACTIVE_CLASS);
  const previousTimer = scrollbarActivityTimers.get(element);
  if (typeof previousTimer === "number") {
    window.clearTimeout(previousTimer);
  }
  const timer = window.setTimeout(() => {
    element.classList.remove(SCROLLBAR_ACTIVE_CLASS);
    scrollbarActivityTimers.delete(element);
  }, SCROLLBAR_IDLE_HIDE_DELAY_MS);
  scrollbarActivityTimers.set(element, timer);
}

function parseFeatureFlagValue(raw: string | null): boolean | null {
  if (raw === null) {
    return null;
  }
  const normalized = raw.trim().toLowerCase();
  if (normalized === "") {
    return null;
  }
  if (normalized === "1" || normalized === "true" || normalized === "yes" || normalized === "on") {
    return true;
  }
  if (normalized === "0" || normalized === "false" || normalized === "no" || normalized === "off") {
    return false;
  }
  return null;
}

function resolveClientFeatureFlag(key: string, runtimeValue: boolean): boolean {
  try {
    const queryValue = parseFeatureFlagValue(new URLSearchParams(window.location.search).get(key));
    if (queryValue !== null) {
      return queryValue;
    }
  } catch {
    // ignore query parsing error
  }

  try {
    const persisted = parseFeatureFlagValue(window.localStorage.getItem(key));
    if (persisted !== null) {
      return persisted;
    }
  } catch {
    // ignore localStorage read error
  }
  return runtimeValue;
}

function parseRuntimeFeatureFlag(value: unknown): boolean {
  return typeof value === "boolean" ? value : false;
}

function applyRuntimeFeatureOverrides(features: RuntimeConfigFeatureFlags): void {
  const runtimePromptTemplates = parseRuntimeFeatureFlag(features.prompt_templates);
  const runtimePromptContextIntrospect = parseRuntimeFeatureFlag(features.prompt_context_introspect);
  runtimeFlags.prompt_templates = resolveClientFeatureFlag(FEATURE_FLAG_PROMPT_TEMPLATES, runtimePromptTemplates);
  runtimeFlags.prompt_context_introspect = resolveClientFeatureFlag(
    FEATURE_FLAG_PROMPT_CONTEXT_INTROSPECT,
    runtimePromptContextIntrospect,
  );
  syncFeatureFlagControls();
}

async function loadRuntimeConfig(): Promise<void> {
  try {
    const payload = await requestJSON<RuntimeConfigResponse>("/runtime-config");
    applyRuntimeFeatureOverrides(payload.features ?? {});
  } catch {
    applyRuntimeFeatureOverrides({});
  }
}

function syncFeatureFlagControls(): void {
  promptContextIntrospectInput.checked = runtimeFlags.prompt_context_introspect;
}

function applyPromptContextIntrospectOverride(enabled: boolean, notify = false): void {
  runtimeFlags.prompt_context_introspect = enabled;
  try {
    window.localStorage.setItem(FEATURE_FLAG_PROMPT_CONTEXT_INTROSPECT, String(enabled));
  } catch {
    // ignore localStorage write error
  }
  syncFeatureFlagControls();
  invalidateSystemPromptTokensCacheAndReload();
  renderComposerTokenEstimate();
  if (notify) {
    setStatus(t(enabled ? "status.promptContextIntrospectEnabled" : "status.promptContextIntrospectDisabled"), "info");
  }
}

function normalizeSystemLayerTaskCommand(raw: string): string {
  const command = raw.trim().split(/\s+/)[0]?.toLowerCase() ?? "";
  switch (command) {
    case "/review":
      return "review";
    case "/compact":
      return "compact";
    case "/memory":
      return "memory";
    case "/plan":
      return "plan";
    case "/execute":
      return "execute";
    case "/pair_programming":
    case "/pair-programming":
      return "pair_programming";
    default:
      return "";
  }
}

function resolveSystemPromptTokenScenario(promptMode: PromptMode): SystemPromptTokenScenario {
  const taskCommand = promptMode === "codex" ? normalizeSystemLayerTaskCommand(messageInput.value) : "";
  const sessionID = taskCommand === "memory" ? state.activeSessionId.trim() : "";
  const tokenSourceKey = runtimeFlags.prompt_context_introspect ? "introspect" : "fallback";
  return {
    promptMode,
    taskCommand,
    sessionID,
    cacheKey: `${promptMode}|${taskCommand}|${sessionID}|${tokenSourceKey}`,
  };
}

function renderComposerTokenEstimate(): void {
  const scenario = resolveSystemPromptTokenScenario(state.activePromptMode);
  if (systemPromptTokensLoaded && systemPromptTokensScenarioKey !== scenario.cacheKey) {
    invalidateSystemPromptTokensCacheAndReload();
  }
  if (!systemPromptTokensLoaded && systemPromptTokensInFlight === null) {
    void ensureSystemPromptTokensLoaded();
  }
  composerTokenEstimate.textContent = t("chat.tokensEstimate", {
    used: formatTokensK(estimateCurrentAIContextTokens()),
    total: formatTokensK(resolveActiveModelContextLimitTokens()),
  });
}

function estimateTokenCount(text: string): number {
  const normalized = text.trim();
  if (normalized === "") {
    return 0;
  }

  const cjkRegex = /[\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF\u3040-\u30FF\uAC00-\uD7AF]/g;
  const cjkCount = normalized.match(cjkRegex)?.length ?? 0;
  const remaining = normalized.replace(cjkRegex, " ");
  const chunks = remaining.match(/[^\s]+/g) ?? [];

  let estimate = cjkCount;
  for (const chunk of chunks) {
    const compact = chunk.replace(/\s+/g, "");
    const runeLength = Array.from(compact).length;
    if (runeLength === 0) {
      continue;
    }
    estimate += Math.max(1, Math.ceil(runeLength / 4));
  }
  return estimate;
}

function estimateConversationContextTokens(): number {
  let total = 0;
  for (const message of state.messages) {
    total += estimateTokenCount(message.text ?? "");
  }
  return total;
}

function estimateCurrentAIContextTokens(): number {
  const conversationTokens = estimateConversationContextTokens();
  const draftTokens = estimateTokenCount(messageInput.value);
  return systemPromptTokens + conversationTokens + draftTokens;
}

function resolveActiveModelContextLimitTokens(): number {
  const providerID = state.activeLLM.provider_id.trim();
  const modelID = state.activeLLM.model.trim();
  if (providerID === "" || modelID === "") {
    return DEFAULT_MODEL_CONTEXT_LIMIT_TOKENS;
  }
  const provider = state.providers.find((item) => item.id === providerID);
  if (!provider) {
    return DEFAULT_MODEL_CONTEXT_LIMIT_TOKENS;
  }
  const model = provider.models.find((item) => item.id === modelID);
  const contextLimit = model?.limit?.context;
  if (typeof contextLimit !== "number" || !Number.isFinite(contextLimit) || contextLimit <= 0) {
    return DEFAULT_MODEL_CONTEXT_LIMIT_TOKENS;
  }
  return Math.floor(contextLimit);
}

function formatTokensK(tokens: number): string {
  const normalized = Number.isFinite(tokens) ? Math.max(0, tokens) : 0;
  return `${(normalized / 1000).toFixed(1)}k`;
}

function normalizeWorkspacePathKey(path: string): string {
  return normalizeWorkspaceInputPath(path).toLowerCase();
}

function isSystemPromptWorkspacePath(path: string): boolean {
  return SYSTEM_PROMPT_WORKSPACE_PATH_SET.has(normalizeWorkspacePathKey(path));
}

function extractWorkspaceFileText(payload: unknown): string {
  if (typeof payload === "string") {
    return payload;
  }
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return "";
  }
  const record = payload as Record<string, unknown>;
  return typeof record.content === "string" ? record.content : "";
}

async function loadSystemPromptTokens(scenario: SystemPromptTokenScenario): Promise<number> {
  if (runtimeFlags.prompt_context_introspect) {
    try {
      const query = new URLSearchParams({
        prompt_mode: scenario.promptMode,
      });
      if (scenario.taskCommand !== "") {
        query.set("task_command", scenario.taskCommand);
      }
      if (scenario.sessionID !== "") {
        query.set("session_id", scenario.sessionID);
      }
      const systemLayerEndpoint = `${SYSTEM_PROMPT_LAYER_ENDPOINT}?${query.toString()}`;
      const payload = await requestJSON<AgentSystemLayersResponse>(systemLayerEndpoint);
      const total = payload?.estimated_tokens_total;
      if (typeof total === "number" && Number.isFinite(total) && total >= 0) {
        return Math.floor(total);
      }
    } catch {
      // Fallback to legacy estimation if introspection endpoint is unavailable.
    }
  }

  const tokenLoaders = SYSTEM_PROMPT_WORKSPACE_FALLBACK_PATHS.map(async (path) => {
    try {
      const payload = await requestWorkspaceFile(path);
      return estimateTokenCount(extractWorkspaceFileText(payload));
    } catch {
      return 0;
    }
  });
  const counts = await Promise.all(tokenLoaders);
  return counts.reduce((sum, count) => sum + count, 0);
}

function ensureSystemPromptTokensLoaded(): Promise<void> {
  const targetScenario = resolveSystemPromptTokenScenario(state.activePromptMode);
  if (systemPromptTokensLoaded && systemPromptTokensScenarioKey === targetScenario.cacheKey) {
    return Promise.resolve();
  }
  if (systemPromptTokensInFlight) {
    if (systemPromptTokensInFlightScenarioKey === targetScenario.cacheKey) {
      return systemPromptTokensInFlight;
    }
    return systemPromptTokensInFlight.then(() => ensureSystemPromptTokensLoaded());
  }
  const loadingScenario = targetScenario;
  systemPromptTokensInFlightScenarioKey = loadingScenario.cacheKey;
  systemPromptTokensInFlight = (async () => {
    systemPromptTokens = await loadSystemPromptTokens(loadingScenario);
    systemPromptTokensScenarioKey = loadingScenario.cacheKey;
    systemPromptTokensLoaded = true;
    renderComposerTokenEstimate();
  })().finally(() => {
    systemPromptTokensInFlight = null;
    systemPromptTokensInFlightScenarioKey = "";
  });
  return systemPromptTokensInFlight;
}

function invalidateSystemPromptTokensCacheAndReload(): void {
  systemPromptTokensLoaded = false;
  systemPromptTokens = 0;
  systemPromptTokensScenarioKey = "";
  void ensureSystemPromptTokensLoaded();
}

function ensureChatLiveRefreshLoop(): void {
  if (chatLiveRefreshTimer !== null) {
    return;
  }
  chatLiveRefreshTimer = window.setInterval(() => {
    void refreshActiveChatLive();
  }, CHAT_LIVE_REFRESH_INTERVAL_MS);
}

async function refreshActiveChatLive(): Promise<void> {
  if (chatLiveRefreshInFlight || state.activeTab !== "chat" || state.activeChatId === null || state.sending) {
    return;
  }

  const activeChatID = state.activeChatId;
  const prevUpdatedAt = state.chats.find((chat) => chat.id === activeChatID)?.updated_at ?? "";
  chatLiveRefreshInFlight = true;
  try {
    await reloadChats();
    if (state.activeChatId !== activeChatID) {
      return;
    }
    const latest = state.chats.find((chat) => chat.id === activeChatID);
    if (!latest || latest.updated_at === prevUpdatedAt) {
      return;
    }
    const history = await requestJSON<ChatHistoryResponse>(`/chats/${encodeURIComponent(activeChatID)}`);
    if (state.activeChatId !== activeChatID) {
      return;
    }
    state.messages = history.messages.map(toViewMessage);
    renderMessages({ animate: false });
    renderComposerTokenEstimate();
    renderChatHeader();
    renderChatList();
    renderSearchChatResults();
  } catch {
    // Keep polling silent to avoid interrupting foreground interactions.
  } finally {
    chatLiveRefreshInFlight = false;
  }
}

function bindTabEvents(): void {
  tabButtons.forEach((button) => {
    button.addEventListener("click", () => {
      const tab = button.dataset.tab;
      if (isTabKey(tab)) {
        void switchTab(tab);
      }
    });
  });

  chatCronToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    void switchTab("cron");
  });
  cronChatToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    void switchTab("chat");
  });
}

function bindSearchEvents(): void {
  chatSearchToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    const nextOpen = !isSearchModalOpen();
    setSearchModalOpen(nextOpen);
    if (nextOpen) {
      renderSearchChatResults();
      searchChatInput.focus();
      searchChatInput.select();
    }
  });
  searchModal.addEventListener("click", (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest("[data-search-close=\"true\"]")) {
      setSearchModalOpen(false);
      return;
    }
    event.stopPropagation();
  });
  searchModalCloseButton.addEventListener("click", () => {
    setSearchModalOpen(false);
  });

  searchChatInput.addEventListener("input", () => {
    state.chatSearchQuery = searchChatInput.value.trim();
    renderSearchChatResults();
  });

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isSearchModalOpen()) {
      return;
    }
    setSearchModalOpen(false);
  });
}

function bindSettingsEvents(): void {
  settingsToggleButton.addEventListener("click", (event) => {
    event.stopPropagation();
    setSettingsPopoverOpen(!isSettingsPopoverOpen());
  });
  settingsSectionButtons.forEach((button) => {
    button.addEventListener("click", () => {
      const section = button.dataset.settingsSection;
      if (!isSettingsSectionKey(section)) {
        return;
      }
      setActiveSettingsSection(section);
    });
  });
  settingsPopover.addEventListener("click", (event) => {
    const target = event.target;
    if (target instanceof Element && target.closest("[data-settings-close=\"true\"]")) {
      setSettingsPopoverOpen(false);
      return;
    }
    event.stopPropagation();
  });
  settingsPopoverCloseButton.addEventListener("click", () => {
    setSettingsPopoverOpen(false);
  });
  document.addEventListener("click", (event) => {
    if (!isSettingsPopoverOpen()) {
      return;
    }
    const target = event.target;
    if (!(target instanceof Node)) {
      return;
    }
    if (
      settingsPopover.contains(target)
      || settingsToggleButton.contains(target)
      || workspaceEditorModal.contains(target)
      || workspaceImportModal.contains(target)
    ) {
      return;
    }
    setSettingsPopoverOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isSettingsPopoverOpen()) {
      return;
    }
    if (isWorkspaceEditorModalOpen() || isWorkspaceImportModalOpen()) {
      return;
    }
    setSettingsPopoverOpen(false);
  });
}

function bindConnectionEvents(): void {
  apiBaseInput.addEventListener("change", async () => {
    await handleControlChange(false);
  });
  apiKeyInput.addEventListener("change", async () => {
    await handleControlChange(false);
  });

  localeSelect.addEventListener("change", () => {
    const locale = setLocale(localeSelect.value);
    localeSelect.value = locale;
    localStorage.setItem(LOCALE_KEY, locale);
    applyLocaleToDocument();
    setStatus(
      t("status.localeChanged", {
        localeName: locale === "zh-CN" ? t("locale.zhCN") : t("locale.enUS"),
      }),
      "info",
    );
  });
  promptContextIntrospectInput.addEventListener("change", () => {
    applyPromptContextIntrospectOverride(promptContextIntrospectInput.checked, true);
  });
}

function bindEvents(): void {
  bindTabEvents();
  bindSearchEvents();
  bindSettingsEvents();
  bindConnectionEvents();
  chatFeature.init();
  modelFeature.init();
  workspaceFeature.init();
  cronFeature.init();
}

function isSettingsPopoverOpen(): boolean {
  return !settingsPopover.classList.contains("is-hidden");
}

function isSearchModalOpen(): boolean {
  return !searchModal.classList.contains("is-hidden");
}

function setSearchModalOpen(open: boolean): void {
  searchModal.classList.toggle("is-hidden", !open);
  searchModal.setAttribute("aria-hidden", String(!open));
  chatSearchToggleButton.setAttribute("aria-expanded", String(open));
  document.body.classList.toggle("search-modal-open", open);
}

function isSettingsSectionKey(value: string | undefined): value is SettingsSectionKey {
  return value === "connection" || value === "identity" || value === "display" || value === "models" || value === "channels" || value === "workspace";
}

function setActiveSettingsSection(section: SettingsSectionKey): void {
  activeSettingsSection = section;
  settingsSectionButtons.forEach((button) => {
    const current = button.dataset.settingsSection;
    const active = current === section;
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-selected", String(active));
  });
  settingsSectionPanels.forEach((panel) => {
    const current = panel.dataset.settingsSectionPanel;
    const active = current === section;
    panel.classList.toggle("is-active", active);
    panel.hidden = !active;
  });
  if (section === "models") {
    setModelsSettingsLevel("list");
    if (!state.tabLoaded.models) {
      void refreshModels();
      return;
    }
    renderModelsPanel();
    return;
  }
  if (section === "channels") {
    setChannelsSettingsLevel("list");
    if (!state.tabLoaded.channels) {
      void refreshQQChannelConfig();
      return;
    }
    renderChannelsPanel();
    return;
  }
  if (section === "workspace") {
    setWorkspaceSettingsLevel("list");
    if (!state.tabLoaded.workspace) {
      void refreshWorkspace();
      return;
    }
    renderWorkspacePanel();
  }
}

function setSettingsPopoverOpen(open: boolean): void {
  settingsPopover.classList.toggle("is-hidden", !open);
  settingsPopover.setAttribute("aria-hidden", String(!open));
  settingsToggleButton.setAttribute("aria-expanded", String(open));
  document.body.classList.toggle("settings-open", open);
  if (open) {
    setActiveSettingsSection(activeSettingsSection);
  }
}

function isWorkspaceEditorModalOpen(): boolean {
  return !workspaceEditorModal.classList.contains("is-hidden");
}

function setWorkspaceEditorModalOpen(open: boolean): void {
  workspaceEditorModal.classList.toggle("is-hidden", !open);
  workspaceEditorModal.setAttribute("aria-hidden", String(!open));
  document.body.classList.toggle("workspace-editor-open", open);
}

function isWorkspaceImportModalOpen(): boolean {
  return !workspaceImportModal.classList.contains("is-hidden");
}

function setWorkspaceImportModalOpen(open: boolean): void {
  workspaceImportModal.classList.toggle("is-hidden", !open);
  workspaceImportModal.setAttribute("aria-hidden", String(!open));
  workspaceImportOpenButton.setAttribute("aria-expanded", String(open));
  document.body.classList.toggle("workspace-import-open", open);
}

function initLocale(): void {
  const savedLocale = localStorage.getItem(LOCALE_KEY);
  const locale = setLocale(savedLocale ?? navigator.language ?? DEFAULT_LOCALE);
  localeSelect.value = locale;
  syncCustomSelect(localeSelect);
}

function applyLocaleToDocument(): void {
  document.documentElement.lang = getLocale();
  document.title = t("app.title");

  document.querySelectorAll<HTMLElement>("[data-i18n]").forEach((element) => {
    const key = element.dataset.i18n;
    if (key && isWebMessageKey(key)) {
      element.textContent = t(key);
    }
  });

  document.querySelectorAll<HTMLElement>("[data-i18n-placeholder]").forEach((element) => {
    const key = element.dataset.i18nPlaceholder;
    if (!key || !isWebMessageKey(key)) {
      return;
    }
    if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement) {
      element.placeholder = t(key);
    }
  });

  document.querySelectorAll<HTMLElement>("[data-i18n-aria-label]").forEach((element) => {
    const key = element.dataset.i18nAriaLabel;
    if (key && isWebMessageKey(key)) {
      element.setAttribute("aria-label", t(key));
    }
  });

  renderProviderTypeOptions();
  renderChatHeader();
  renderChatList({ force: true });
  renderSearchChatResults({ force: true });
  renderMessages({ force: true });
  renderComposerModelSelectors();
  renderComposerTokenEstimate();
  renderComposerSlashPanel();
  syncSendButtonState();
  if (state.tabLoaded.models) {
    renderModelsPanel();
  }
  if (state.tabLoaded.channels) {
    renderChannelsPanel();
  }
  if (state.tabLoaded.workspace) {
    renderWorkspacePanel();
  }
  if (state.tabLoaded.cron) {
    renderCronJobs();
    if (state.cronModal.mode === "edit") {
      renderCronExecutionDetails(state.cronStates[state.cronModal.editingJobID]);
    }
  }
  refreshCronWorkflowLabels();
  setCronModalMode(state.cronModal.mode, state.cronModal.editingJobID);
  syncCronWorkflowFullscreenUI();
  syncCronDispatchHint();
  syncAllCustomSelects();
  logComposerStatusToConsole();
}

async function handleControlChange(resetDraft: boolean): Promise<void> {
  syncControlState();
  if (resetDraft) {
    startDraftSession();
    ensureCronSessionID();
  }
  syncCronDispatchHint();
  invalidateResourceTabs();

  await reloadChats();
  if (state.activeTab !== "chat") {
    await loadTabData(state.activeTab, true);
  }
}

async function syncModelStateOnBoot(): Promise<void> {
  try {
    await syncModelState({ autoActivate: true });
  } catch {
    // Keep chat usable even if model catalog is temporarily unavailable.
  }
}

async function switchTab(tab: TabKey): Promise<void> {
  if (state.activeTab === tab) {
    return;
  }
  setSearchModalOpen(false);
  setWorkspaceEditorModalOpen(false);
  setWorkspaceImportModalOpen(false);
  if (tab !== "cron") {
    setCronCreateModalOpen(false);
  }
  state.activeTab = tab;
  renderTabPanels();
  await loadTabData(tab);
}

function renderTabPanels(): void {
  tabButtons.forEach((button) => {
    const tab = button.dataset.tab;
    button.classList.toggle("active", tab === state.activeTab);
  });
  const cronActive = state.activeTab === "cron";
  chatCronToggleButton.classList.toggle("is-active", cronActive);
  chatCronToggleButton.setAttribute("aria-pressed", String(cronActive));

  TABS.forEach((tab) => {
    panelByTab[tab].classList.toggle("is-active", tab === state.activeTab);
  });
}

async function loadTabData(tab: TabKey, force = false): Promise<void> {
  try {
    if (tab === "chat") {
      await reloadChats();
      return;
    }
    if (!force && state.tabLoaded[tab]) {
      return;
    }

    switch (tab) {
      case "cron":
        await refreshCronJobs();
        break;
      default:
        break;
    }
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function restoreSettings(): void {
  const raw = localStorage.getItem(SETTINGS_KEY);
  if (raw) {
    try {
      const parsed = JSON.parse(raw) as PersistedSettings;
      if (typeof parsed.apiBase === "string" && parsed.apiBase.trim() !== "") {
        state.apiBase = parsed.apiBase.trim();
      }
      if (typeof parsed.apiKey === "string") {
        state.apiKey = parsed.apiKey.trim();
      }
      state.workspaceCardEnabled = parseWorkspaceCardEnabled(parsed.workspaceCardEnabled);
    } catch {
      localStorage.removeItem(SETTINGS_KEY);
    }
  }
  state.userId = DEFAULT_USER_ID;
  state.channel = WEB_CHAT_CHANNEL;
  apiBaseInput.value = state.apiBase;
  apiKeyInput.value = state.apiKey;
}

function syncControlState(): void {
  state.apiBase = apiBaseInput.value.trim() || DEFAULT_API_BASE;
  state.apiKey = apiKeyInput.value.trim();
  state.userId = DEFAULT_USER_ID;
  state.channel = WEB_CHAT_CHANNEL;
  localStorage.setItem(
    SETTINGS_KEY,
    JSON.stringify({
      apiBase: state.apiBase,
      apiKey: state.apiKey,
      workspaceCardEnabled: state.workspaceCardEnabled,
    }),
  );
}

function invalidateResourceTabs(): void {
  state.tabLoaded.models = false;
  state.tabLoaded.channels = false;
  state.tabLoaded.workspace = false;
  state.tabLoaded.cron = false;
}

function parseIntegerInput(raw: string, fallback: number, min: number): number {
  const trimmed = raw.trim();
  if (trimmed === "") {
    return fallback;
  }
  const parsed = Number.parseInt(trimmed, 10);
  if (Number.isNaN(parsed)) {
    return fallback;
  }
  if (parsed < min) {
    return min;
  }
  return parsed;
}

function normalizeProviders(providers: ProviderInfo[]): ProviderInfo[] {
  return providers.map((provider) => ({
    ...provider,
    name: provider.name?.trim() || provider.id,
    display_name: provider.display_name?.trim() || provider.name?.trim() || provider.id,
    openai_compatible: provider.openai_compatible ?? false,
    models: Array.isArray(provider.models) ? provider.models : [],
    reasoning_effort: normalizeProviderReasoningEffort(provider.reasoning_effort),
    headers: normalizeProviderHeadersMap(provider.headers),
    timeout_ms: normalizeProviderTimeoutMS(provider.timeout_ms),
    model_aliases: normalizeProviderAliasMap(provider.model_aliases),
    enabled: provider.enabled ?? true,
    store: provider.store === true,
    current_api_key: provider.current_api_key ?? "",
    current_base_url: provider.current_base_url ?? "",
  }));
}

function normalizeProviderHeadersMap(raw: unknown): Record<string, string> | undefined {
  const parsed = toRecord(raw);
  if (!parsed) {
    return undefined;
  }
  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed)) {
    const headerKey = key.trim();
    const headerValue = typeof value === "string" ? value.trim() : "";
    if (headerKey === "" || headerValue === "") {
      continue;
    }
    headers[headerKey] = headerValue;
  }
  if (Object.keys(headers).length === 0) {
    return undefined;
  }
  return headers;
}

function normalizeProviderTimeoutMS(raw: unknown): number | undefined {
  if (typeof raw !== "number" || !Number.isFinite(raw) || raw < 0) {
    return undefined;
  }
  return Math.trunc(raw);
}

function normalizeProviderReasoningEffort(raw: unknown): string | undefined {
  if (typeof raw !== "string") {
    return undefined;
  }
  const value = raw.trim().toLowerCase();
  if (value === "minimal" || value === "low" || value === "medium" || value === "high") {
    return value;
  }
  return undefined;
}

function normalizeProviderAliasMap(raw: unknown): Record<string, string> | undefined {
  const parsed = toRecord(raw);
  if (!parsed) {
    return undefined;
  }
  const aliases: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed)) {
    const alias = key.trim();
    const target = typeof value === "string" ? value.trim() : "";
    if (alias === "" || target === "") {
      continue;
    }
    aliases[alias] = target;
  }
  if (Object.keys(aliases).length === 0) {
    return undefined;
  }
  return aliases;
}

function formatProviderLabel(provider: ProviderInfo): string {
  return provider.display_name?.trim() || provider.name?.trim() || provider.id;
}

function normalizeDefaults(defaults: Record<string, string>, providers: ProviderInfo[]): Record<string, string> {
  const normalized: Record<string, string> = {};
  for (const [providerID, modelID] of Object.entries(defaults ?? {})) {
    if (providerID.trim() === "" || modelID.trim() === "") {
      continue;
    }
    normalized[providerID] = modelID;
  }
  for (const provider of providers) {
    if (normalized[provider.id]) {
      continue;
    }
    if (provider.models.length > 0) {
      normalized[provider.id] = provider.models[0].id;
    }
  }
  return normalized;
}

function buildDefaultMapFromProviders(providers: ProviderInfo[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const provider of providers) {
    if (provider.models.length > 0) {
      out[provider.id] = provider.models[0].id;
    }
  }
  return out;
}

function normalizeModelSlot(raw?: ModelSlotConfig): ModelSlotConfig {
  return {
    provider_id: raw?.provider_id?.trim() ?? "",
    model: raw?.model?.trim() ?? "",
  };
}

function formatModelEntry(model: ModelInfo): string {
  return model.id.trim() || (model.name ?? "").trim();
}

function formatCapabilities(capabilities?: ModelCapabilities): string {
  if (!capabilities) {
    return "";
  }
  const tags: string[] = [];
  if (capabilities.temperature) {
    tags.push("temp");
  }
  if (capabilities.reasoning) {
    tags.push("reason");
  }
  if (capabilities.attachment) {
    tags.push("attach");
  }
  if (capabilities.tool_call) {
    tags.push("tool");
  }
  const input = formatModalities(capabilities.input);
  if (input !== "") {
    tags.push(`in:${input}`);
  }
  const output = formatModalities(capabilities.output);
  if (output !== "") {
    tags.push(`out:${output}`);
  }
  return tags.join("|");
}

function formatModalities(modalities?: ModelModalities): string {
  if (!modalities) {
    return "";
  }
  const out: string[] = [];
  if (modalities.text) {
    out.push("text");
  }
  if (modalities.image) {
    out.push("image");
  }
  if (modalities.audio) {
    out.push("audio");
  }
  if (modalities.video) {
    out.push("video");
  }
  if (modalities.pdf) {
    out.push("pdf");
  }
  return out.join("+");
}

function appendEmptyItem(list: HTMLElement, text: string): void {
  const item = document.createElement("li");
  item.className = "message-empty";
  item.textContent = text;
  list.appendChild(item);
}

function toRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function parsePositiveInteger(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) {
    const n = Math.trunc(value);
    return n > 0 ? n : undefined;
  }
  if (typeof value === "string") {
    const parsed = Number.parseInt(value.trim(), 10);
    if (Number.isFinite(parsed) && parsed > 0) {
      return parsed;
    }
  }
  return undefined;
}

type PersistedToolNoticeParseOptions = {
  hasAssistantText?: boolean;
};

function buildToolCallNoticeFromRaw(raw: string, options: PersistedToolNoticeParseOptions = {}): ViewToolCallNotice | null {
  const normalized = raw.trim();
  if (normalized === "") {
    return null;
  }
  const hasAssistantText = options.hasAssistantText === true;
  let summary = t("chat.toolCallNotice", { target: "tool" });
  let detail = normalized;
  let toolName = "";
  let outputReady = true;
  let step: number | undefined;
  try {
    const payload = JSON.parse(normalized) as AgentStreamEvent;
    step = parsePositiveInteger(payload.step);
    if (payload.type === "tool_call") {
      summary = formatToolCallSummary(payload.tool_call);
      toolName = normalizeToolName(payload.tool_call?.name);
      if (hasAssistantText) {
        detail = t("chat.toolCallOutputUnavailable");
        outputReady = true;
      } else {
        detail = t("chat.toolCallOutputPending");
        outputReady = false;
      }
    } else if (payload.type === "tool_result") {
      toolName = normalizeToolName(payload.tool_result?.name);
      summary = formatToolCallSummary({ name: toolName });
      detail = formatToolResultOutput(payload.tool_result);
      outputReady = true;
    } else {
      summary = formatToolCallSummary(payload.tool_call);
    }
  } catch {
    // ignore invalid raw payload and keep fallback summary
  }
  const resolvedToolName = normalizeToolName(toolName) || parseToolNameFromToolCallRaw(normalized);
  return {
    summary,
    raw: detail,
    step,
    toolName: resolvedToolName === "" ? undefined : resolvedToolName,
    outputReady,
  };
}

function parsePersistedToolCallNotices(
  metadata: Record<string, unknown> | null,
  options: PersistedToolNoticeParseOptions = {},
): ViewToolCallNotice[] {
  if (!metadata) {
    return [];
  }
  const raw = metadata.tool_call_notices;
  if (!Array.isArray(raw)) {
    return [];
  }
  const notices: ViewToolCallNotice[] = [];
  for (const item of raw) {
    if (typeof item === "string") {
      const notice = buildToolCallNoticeFromRaw(item, options);
      if (notice) {
        notices.push(notice);
      }
      continue;
    }
    const obj = toRecord(item);
    if (!obj) {
      continue;
    }
    const rawText = typeof obj.raw === "string" ? obj.raw : "";
    const notice = buildToolCallNoticeFromRaw(rawText, options);
    if (notice) {
      const persistedOrder = parsePositiveInteger(obj.order);
      if (persistedOrder !== undefined) {
        notice.order = persistedOrder;
      }
      notices.push(notice);
    }
  }
  return notices;
}

function toViewMessage(message: RuntimeMessage): ViewMessage {
  const joined = (message.content ?? [])
    .map((item: RuntimeContent) => item.text ?? "")
    .join("")
    .trim();
  const metadata = toRecord(message.metadata);
  const toolCalls = parsePersistedToolCallNotices(metadata, { hasAssistantText: joined !== "" });
  const persistedTextOrder = parsePositiveInteger(metadata?.text_order);
  const persistedToolOrder = parsePositiveInteger(metadata?.tool_order);
  const textOrder = joined === "" ? undefined : (persistedTextOrder ?? nextMessageOutputOrder());
  const orderedToolCalls = withResolvedToolCallOrder(toolCalls, persistedToolOrder);
  const toolOrder = orderedToolCalls.length === 0 ? undefined : orderedToolCalls[0].order;
  const timeline: ViewMessageTimelineEntry[] = [];
  if (joined !== "" && textOrder !== undefined) {
    timeline.push({
      type: "text",
      order: textOrder,
      text: joined,
    });
  }
  for (const toolCall of orderedToolCalls) {
    timeline.push({
      type: "tool_call",
      order: toolCall.order ?? nextMessageOutputOrder(),
      toolCall,
    });
  }
  return {
    id: message.id || `msg-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    role: message.role === "user" ? "user" : "assistant",
    text: joined,
    toolCalls: orderedToolCalls,
    textOrder,
    toolOrder,
    timeline,
  };
}

function withResolvedToolCallOrder(toolCalls: ViewToolCallNotice[], persistedToolOrder?: number): ViewToolCallNotice[] {
  if (toolCalls.length === 0) {
    return [];
  }
  const resolved: ViewToolCallNotice[] = [];
  let cursor = persistedToolOrder;
  for (const toolCall of toolCalls) {
    const parsedOrder = parsePositiveInteger(toolCall.order);
    if (parsedOrder !== undefined) {
      cursor = parsedOrder;
      resolved.push({
        ...toolCall,
        order: parsedOrder,
      });
      continue;
    }
    if (cursor === undefined) {
      cursor = nextMessageOutputOrder();
    } else {
      cursor += 1;
    }
    resolved.push({
      ...toolCall,
      order: cursor,
    });
  }
  return resolved;
}

function asErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function asWorkspaceErrorMessage(error: unknown): string {
  if (!(error instanceof Error)) {
    return asErrorMessage(error);
  }
  const raw = error.message.trim().toLowerCase();
  if (raw === "404 page not found") {
    return t("error.workspaceEndpointMissing", {
      endpoint: "/workspace/files",
      apiBase: state.apiBase,
    });
  }
  return error.message;
}

function compactTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString(getLocale(), { hour12: false });
}

function isTabKey(value: string | undefined): value is TabKey {
  return value === "chat" || value === "cron";
}

function newSessionID(): string {
  const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789";
  const length = 6;
  const bytes = new Uint8Array(length);
  if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") {
    crypto.getRandomValues(bytes);
  } else {
    for (let index = 0; index < bytes.length; index += 1) {
      bytes[index] = Math.floor(Math.random() * 256);
    }
  }
  let id = "";
  for (const value of bytes) {
    id += alphabet[value % alphabet.length];
  }
  return id;
}

function mustElement<T extends Element>(id: string): T {
  const element = document.getElementById(id);
  if (!element) {
    throw new Error(t("error.missingElement", { id }));
  }
  return element as unknown as T;
}

// Shared web domain types. Reuse these in domain modules; do not redeclare locally.

import type {
  AgentStreamEvent as ContractAgentStreamEvent,
  AgentStreamEventMeta as ContractAgentStreamEventMeta,
  AgentToolCallPayload as ContractAgentToolCallPayload,
  AgentToolResultPayload as ContractAgentToolResultPayload,
  ChatHistoryResponse as ContractChatHistoryResponse,
  ChatSpec as ContractChatSpec,
  DeleteResult as ContractDeleteResult,
  RuntimeContent as ContractRuntimeContent,
  RuntimeMessage as ContractRuntimeMessage,
} from "@nextai/sdk-ts";

export type PromptMode = "default" | "codex" | "claude";
export type CollaborationMode = "default" | "plan" | "execute" | "pair_programming";

export type ChatSpec = ContractChatSpec & { updated_at: string };

export type RuntimeContent = ContractRuntimeContent;

export type RuntimeMessage = ContractRuntimeMessage;

export interface ChatHistoryResponse extends Omit<ContractChatHistoryResponse, "messages"> {
  messages: RuntimeMessage[];
}

export interface ViewToolCallNotice {
  summary: string;
  raw: string;
  order?: number;
  step?: number;
  toolName?: string;
  outputReady?: boolean;
}

export interface ViewMessageTimelineEntry {
  type: "text" | "tool_call";
  order: number;
  text?: string;
  toolCall?: ViewToolCallNotice;
}

export interface ViewMessage {
  id: string;
  role: "user" | "assistant";
  text: string;
  toolCalls: ViewToolCallNotice[];
  textOrder?: number;
  toolOrder?: number;
  timeline: ViewMessageTimelineEntry[];
}

export type AgentToolCallPayload = ContractAgentToolCallPayload;

export type AgentToolResultPayload = ContractAgentToolResultPayload;

export type AgentStreamEventMeta = ContractAgentStreamEventMeta;

export type AgentStreamEvent = ContractAgentStreamEvent;

export interface WorkspaceUploadResponse {
  uploaded?: boolean;
  path?: string;
  name?: string;
  size?: number;
}

export type ModelsSettingsLevel = "list" | "edit";
export type ChannelsSettingsLevel = "list" | "edit";
export type ProviderKVKind = "headers" | "aliases";
export type QQTargetType = "c2c" | "group" | "guild";
export type QQAPIEnvironment = "production" | "sandbox";

export interface ModelModalities {
  text: boolean;
  audio: boolean;
  image: boolean;
  video: boolean;
  pdf: boolean;
}

export interface ModelCapabilities {
  temperature: boolean;
  reasoning: boolean;
  attachment: boolean;
  tool_call: boolean;
  input?: ModelModalities;
  output?: ModelModalities;
}

export interface ModelLimit {
  context?: number;
  input?: number;
  output?: number;
}

export interface ModelInfo {
  id: string;
  name: string;
  status?: string;
  alias_of?: string;
  capabilities?: ModelCapabilities;
  limit?: ModelLimit;
}

export interface ProviderInfo {
  id: string;
  name: string;
  display_name: string;
  openai_compatible?: boolean;
  api_key_prefix?: string;
  models: ModelInfo[];
  reasoning_effort?: string;
  store?: boolean;
  headers?: Record<string, string>;
  timeout_ms?: number;
  model_aliases?: Record<string, string>;
  allow_custom_base_url?: boolean;
  enabled?: boolean;
  has_api_key: boolean;
  current_api_key?: string;
  current_base_url?: string;
}

export interface ProviderTypeInfo {
  id: string;
  display_name: string;
}

export interface ComposerModelOption {
  value: string;
  canonical: string;
  label: string;
}

export interface ModelSlotConfig {
  provider_id: string;
  model: string;
}

export interface ModelCatalogInfo {
  providers: ProviderInfo[];
  provider_types?: ProviderTypeInfo[];
  defaults: Record<string, string>;
  active_llm?: ModelSlotConfig;
}

export interface ActiveModelsInfo {
  active_llm?: ModelSlotConfig;
}

export interface UpsertProviderOptions {
  closeAfterSave?: boolean;
  notifyStatus?: boolean;
}

export interface QQChannelConfig {
  enabled: boolean;
  app_id: string;
  client_secret: string;
  bot_prefix: string;
  target_type: QQTargetType;
  target_id: string;
  api_base: string;
  token_url: string;
  timeout_seconds: number;
}

export type CronModalMode = "create" | "edit";
export type CronWorkflowNodeType = "start" | "text_event" | "delay" | "if_event";
export type CronTaskType = "text" | "workflow";

export interface CronScheduleSpec {
  type: string;
  cron: string;
  timezone?: string;
}

export interface CronDispatchTarget {
  user_id: string;
  session_id: string;
}

export interface CronDispatchSpec {
  type?: string;
  channel?: string;
  target: CronDispatchTarget;
  mode?: string;
  meta?: Record<string, unknown>;
}

export interface CronRuntimeSpec {
  max_concurrency?: number;
  timeout_seconds?: number;
  misfire_grace_seconds?: number;
}

export interface CronWorkflowViewport {
  pan_x?: number;
  pan_y?: number;
  zoom?: number;
}

export interface CronWorkflowNode {
  id: string;
  type: CronWorkflowNodeType;
  title?: string;
  x: number;
  y: number;
  text?: string;
  delay_seconds?: number;
  if_condition?: string;
  continue_on_error?: boolean;
}

export interface CronWorkflowEdge {
  id: string;
  source: string;
  target: string;
}

export interface CronWorkflowSpec {
  version: "v1";
  viewport?: CronWorkflowViewport;
  nodes: CronWorkflowNode[];
  edges: CronWorkflowEdge[];
}

export interface CronWorkflowNodeExecution {
  node_id: string;
  node_type: "text_event" | "delay" | "if_event";
  status: "succeeded" | "failed" | "skipped";
  continue_on_error: boolean;
  started_at: string;
  finished_at?: string;
  error?: string;
}

export interface CronWorkflowExecution {
  run_id: string;
  started_at: string;
  finished_at?: string;
  had_failures: boolean;
  nodes: CronWorkflowNodeExecution[];
}

export interface CronJobSpec {
  id: string;
  name: string;
  enabled: boolean;
  schedule: CronScheduleSpec;
  task_type: CronTaskType;
  text?: string;
  workflow?: CronWorkflowSpec;
  dispatch: CronDispatchSpec;
  runtime: CronRuntimeSpec;
  meta?: Record<string, unknown>;
}

export interface CronJobState {
  next_run_at?: string;
  last_run_at?: string;
  last_status?: string;
  last_error?: string;
  paused?: boolean;
  last_result?: string;
  last_workflow?: CronWorkflowExecution;
  last_execution?: CronWorkflowExecution;
}

export type WorkspaceEditorMode = "json" | "text";
export type WorkspaceSettingsLevel = "list" | "config" | "prompt" | "codex" | "claude";
export type WorkspaceCardKey = "config" | "prompt" | "codex" | "claude";

export interface WorkspaceFileInfo {
  path: string;
  kind: "config" | "skill";
  size: number | null;
}

export interface WorkspaceCodexTreeNode {
  name: string;
  path: string;
  folders: WorkspaceCodexTreeNode[];
  files: WorkspaceFileInfo[];
}

export interface WorkspaceFileCatalog {
  files: WorkspaceFileInfo[];
  configFiles: WorkspaceFileInfo[];
  promptFiles: WorkspaceFileInfo[];
  codexFiles: WorkspaceFileInfo[];
  claudeFiles: WorkspaceFileInfo[];
  codexTree: WorkspaceCodexTreeNode[];
  codexRootFiles: WorkspaceFileInfo[];
  codexFolderPaths: Set<string>;
  codexTopLevelFolderPaths: Set<string>;
  claudeTree: WorkspaceCodexTreeNode[];
  claudeRootFiles: WorkspaceFileInfo[];
  claudeFolderPaths: Set<string>;
  claudeTopLevelFolderPaths: Set<string>;
}

export interface WorkspaceTextPayload {
  content: string;
}

export type DeleteResult = ContractDeleteResult;

export type WebAppTone = "neutral" | "info" | "error";

export interface WebAppTabLoadedState {
  chat: boolean;
  models: boolean;
  channels: boolean;
  workspace: boolean;
  cron: boolean;
}

export interface WebAppProviderModalState {
  open: boolean;
  mode: "create" | "edit";
  editingProviderID: string;
}

export interface WebAppCronModalState {
  mode: CronModalMode;
  editingJobID: string;
}

export interface WebAppRuntimeFlags {
  prompt_templates: boolean;
  prompt_context_introspect: boolean;
}

export interface WebAppState {
  apiBase: string;
  apiKey: string;
  userId: string;
  channel: string;
  activeTab: "chat" | "cron";
  tabLoaded: WebAppTabLoadedState;
  chats: ChatSpec[];
  chatSearchQuery: string;
  activeChatId: string | null;
  activeSessionId: string;
  activePromptMode: PromptMode;
  activeCollaborationMode: CollaborationMode;
  messages: ViewMessage[];
  messageOutputOrder: number;
  sending: boolean;
  providers: ProviderInfo[];
  providerTypes: ProviderTypeInfo[];
  modelDefaults: Record<string, string>;
  activeLLM: ModelSlotConfig;
  selectedProviderID: string;
  modelsSettingsLevel: ModelsSettingsLevel;
  channelsSettingsLevel: ChannelsSettingsLevel;
  workspaceSettingsLevel: WorkspaceSettingsLevel;
  workspaceCardEnabled: Record<WorkspaceCardKey, boolean>;
  providerAPIKeyVisible: boolean;
  providerModal: WebAppProviderModalState;
  workspaceFileCatalog: WorkspaceFileCatalog;
  workspaceCodexExpandedFolders: Set<string>;
  workspaceClaudeExpandedFolders: Set<string>;
  qqChannelConfig: QQChannelConfig;
  qqChannelAvailable: boolean;
  activeWorkspacePath: string;
  activeWorkspaceContent: string;
  activeWorkspaceMode: WorkspaceEditorMode;
  cronJobs: CronJobSpec[];
  cronStates: Record<string, CronJobState>;
  cronModal: WebAppCronModalState;
  cronDraftTaskType: "text" | "workflow";
}

export type WebAppTranslate = typeof import("../i18n.js").t;
export type WebAppStatusSetter = (message: string, tone?: WebAppTone) => void;
export type WebAppRequestJSON = <T = unknown>(path: string, options?: WebAppRequestOptions) => Promise<T>;
export type WebAppOpenStream = (path: string, options?: WebAppRequestOptions) => Promise<Response>;

export interface WebAppRequestOptions {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  accept?: string;
}

export interface WebAppCronWorkflowCanvasInstance {
  setWorkflow(workflow: CronWorkflowSpec): void;
  resetToDefault(): void;
  getWorkflow(): CronWorkflowSpec;
  refreshLabels(): void;
}

export interface WebAppCronWorkflowCanvasConstructor {
  new(options: {
    viewport: HTMLElement;
    canvas: HTMLElement;
    edgesLayer: SVGSVGElement;
    nodesLayer: HTMLElement;
    nodeEditor: HTMLElement;
    zoomLabel: HTMLElement;
    onChange?: (workflow: CronWorkflowSpec) => void;
    onStatus?: (message: string, tone: "info" | "error") => void;
  }): WebAppCronWorkflowCanvasInstance;
}

export interface WebAppContext {
  state: WebAppState;
  t: WebAppTranslate;
  setStatus: WebAppStatusSetter;
  asErrorMessage: (error: unknown) => string;
  asWorkspaceErrorMessage: (error: unknown) => string;
  requestJSON: WebAppRequestJSON;
  openStream: WebAppOpenStream;
  logAgentRawRequest: (raw: string) => void;
  logAgentRawResponse: (raw: string) => void;
  toViewMessage: (message: RuntimeMessage) => ViewMessage;
  compactTime: (value: string) => string;
  renderMarkdownToFragment: (markdown: string, doc: Document) => DocumentFragment;
  requestWorkspaceFile: (path: string) => Promise<unknown>;
  extractWorkspaceFileText: (payload: unknown) => string;
  renderComposerTokenEstimate: () => void;
  syncControlState: () => void;
  appendEmptyItem: (list: HTMLElement, text: string) => void;
  newSessionID: () => string;
  setSearchModalOpen: (open: boolean) => void;
  getBootstrapTask: () => Promise<void>;
  parsePositiveInteger: (value: unknown) => number | undefined;
  resetComposerFileDragDepth: () => void;
  runtimeFlags: WebAppRuntimeFlags;
  WEB_CHAT_CHANNEL: string;
  QQ_CHANNEL: string;
  TRASH_ICON_SVG: string;
  PROMPT_TEMPLATE_PREFIX: string;
  PROMPT_TEMPLATE_NAME_PATTERN: RegExp;
  PROMPT_TEMPLATE_ARG_KEY_PATTERN: RegExp;
  PROMPT_TEMPLATE_PLACEHOLDER_PATTERN: RegExp;
  PROMPT_MODE_META_KEY: string;
  chatList: HTMLUListElement;
  chatTitle: HTMLElement;
  chatSession: HTMLElement;
  chatPromptModeSelect: HTMLSelectElement;
  chatCollaborationModeSelect: HTMLSelectElement;
  searchChatInput: HTMLInputElement;
  searchChatResults: HTMLUListElement;
  messageList: HTMLUListElement;
  thinkingIndicator: HTMLElement;
  composerMain: HTMLElement;
  messageInput: HTMLTextAreaElement;
  sendButton: HTMLButtonElement;
  composerAttachButton: HTMLButtonElement;
  composerAttachInput: HTMLInputElement;
  syncCustomSelect: (select: HTMLSelectElement) => void;
  formatModelEntry: (model: ModelInfo) => string;
  formatCapabilities: (capabilities?: ModelCapabilities) => string;
  formatProviderLabel: (provider: ProviderInfo) => string;
  normalizeProviders: (providers: ProviderInfo[]) => ProviderInfo[];
  normalizeDefaults: (defaults: Record<string, string>, providers: ProviderInfo[]) => Record<string, string>;
  buildDefaultMapFromProviders: (providers: ProviderInfo[]) => Record<string, string>;
  normalizeModelSlot: (raw?: ModelSlotConfig) => ModelSlotConfig;
  parseIntegerInput: (raw: string, fallback: number, min: number) => number;
  toRecord: (value: unknown) => Record<string, unknown> | null;
  DEFAULT_QQ_API_BASE: string;
  QQ_SANDBOX_API_BASE: string;
  DEFAULT_QQ_TOKEN_URL: string;
  DEFAULT_QQ_TIMEOUT_SECONDS: number;
  DEFAULT_OPENAI_MODEL_IDS: string[];
  BUILTIN_PROVIDER_IDS: Set<string>;
  PROVIDER_AUTO_SAVE_DELAY_MS: number;
  composerProviderSelect: HTMLSelectElement;
  composerModelSelect: HTMLSelectElement;
  modelsSettingsSection: HTMLElement;
  channelsSettingsSection: HTMLElement;
  channelsEntryList: HTMLUListElement;
  channelsLevel1View: HTMLElement;
  channelsLevel2View: HTMLElement;
  qqChannelEnabledInput: HTMLInputElement;
  qqChannelAppIDInput: HTMLInputElement;
  qqChannelClientSecretInput: HTMLInputElement;
  qqChannelBotPrefixInput: HTMLInputElement;
  qqChannelTargetTypeSelect: HTMLSelectElement;
  qqChannelAPIEnvironmentSelect: HTMLSelectElement;
  qqChannelTimeoutSecondsInput: HTMLInputElement;
  modelsProviderAPIKeyInput: HTMLInputElement;
  modelsProviderAPIKeyVisibilityButton: HTMLButtonElement;
  modelsProviderBaseURLInput: HTMLInputElement;
  modelsProviderBaseURLPreview: HTMLElement;
  modelsLevel1View: HTMLElement;
  modelsLevel2View: HTMLElement;
  modelsEditProviderMeta: HTMLElement;
  modelsProviderList: HTMLUListElement;
  modelsProviderTypeSelect: HTMLSelectElement;
  modelsProviderNameInput: HTMLInputElement;
  modelsProviderTimeoutMSInput: HTMLInputElement;
  modelsProviderReasoningEffortField: HTMLElement;
  modelsProviderReasoningEffortSelect: HTMLSelectElement;
  modelsProviderEnabledInput: HTMLInputElement;
  modelsProviderStoreField: HTMLElement;
  modelsProviderStoreInput: HTMLInputElement;
  modelsProviderHeadersRows: HTMLElement;
  modelsProviderAliasesRows: HTMLElement;
  modelsProviderCustomModelsField: HTMLElement;
  modelsProviderCustomModelsRows: HTMLElement;
  modelsProviderCustomModelsAddButton: HTMLButtonElement;
  modelsProviderModalTitle: HTMLElement;
  normalizeWorkspacePathKey: (path: string) => string;
  isSystemPromptWorkspacePath: (path: string) => boolean;
  invalidateSystemPromptTokensCacheAndReload: () => void;
  setWorkspaceEditorModalOpen: (open: boolean) => void;
  setWorkspaceImportModalOpen: (open: boolean) => void;
  DEFAULT_WORKSPACE_CARD_ENABLED: Record<WorkspaceCardKey, boolean>;
  WORKSPACE_CARD_KEYS: WorkspaceCardKey[];
  WORKSPACE_CODEX_PREFIX: string;
  WORKSPACE_CLAUDE_PREFIX: string;
  workspaceEntryList: HTMLUListElement;
  workspaceLevel1View: HTMLElement;
  workspaceLevel2ConfigView: HTMLElement;
  workspaceLevel2PromptView: HTMLElement;
  workspaceLevel2CodexView: HTMLElement;
  workspaceLevel2ClaudeView: HTMLElement;
  workspaceSettingsSection: HTMLElement;
  workspaceFilesBody: HTMLUListElement;
  workspacePromptsBody: HTMLUListElement;
  workspaceCodexTreeBody: HTMLUListElement;
  workspaceClaudeBody: HTMLUListElement;
  workspaceFilePathInput: HTMLInputElement;
  workspaceFileContentInput: HTMLTextAreaElement;
  workspaceSaveFileButton: HTMLButtonElement;
  workspaceDeleteFileButton: HTMLButtonElement;
  workspaceJSONInput: HTMLTextAreaElement;
  reloadChats: (options?: { includeQQHistory?: boolean }) => Promise<void>;
  createDefaultCronWorkflow: () => CronWorkflowSpec;
  validateCronWorkflowSpec: (spec: CronWorkflowSpec) => string | null;
  CronWorkflowCanvas: WebAppCronWorkflowCanvasConstructor;
  DEFAULT_CRON_JOB_ID: string;
  CRON_META_SYSTEM_DEFAULT: string;
  cronCreateOpenButton: HTMLButtonElement;
  cronCreateModal: HTMLElement;
  cronCreateModalCloseButton: HTMLButtonElement;
  cronTaskTypeSelect: HTMLSelectElement;
  cronIDInput: HTMLInputElement;
  cronNameInput: HTMLInputElement;
  cronIntervalInput: HTMLInputElement;
  cronSessionIDInput: HTMLInputElement;
  cronMaxConcurrencyInput: HTMLInputElement;
  cronTimeoutInput: HTMLInputElement;
  cronMisfireInput: HTMLInputElement;
  cronTextInput: HTMLTextAreaElement;
  cronResetWorkflowButton: HTMLButtonElement;
  cronWorkflowFullscreenButton: HTMLButtonElement;
  refreshCronButton: HTMLButtonElement;
  cronNewSessionButton: HTMLButtonElement;
  cronCreateForm: HTMLFormElement;
  cronJobsBody: HTMLUListElement;
  cronDispatchHint: HTMLElement;
  cronCreateModalTitle: HTMLElement;
  cronSubmitButton: HTMLButtonElement;
  cronTextSection: HTMLElement;
  cronWorkflowSection: HTMLElement;
  cronWorkflowViewport: HTMLElement;
  cronWorkflowCanvas: HTMLElement;
  cronWorkflowEdges: SVGSVGElement;
  cronWorkflowNodes: HTMLElement;
  cronWorkflowNodeEditor: HTMLElement;
  cronWorkflowZoom: HTMLElement;
  cronWorkflowExecutionList: HTMLUListElement;
  cronWorkbench: HTMLElement;
}

export type ChatToolCallContext = Pick<WebAppContext, "t">;

export type ChatDomainContext = Pick<WebAppContext,
  | "state"
  | "t"
  | "setStatus"
  | "asErrorMessage"
  | "requestJSON"
  | "openStream"
  | "logAgentRawRequest"
  | "logAgentRawResponse"
  | "toViewMessage"
  | "compactTime"
  | "renderMarkdownToFragment"
  | "requestWorkspaceFile"
  | "extractWorkspaceFileText"
  | "renderComposerTokenEstimate"
  | "syncControlState"
  | "appendEmptyItem"
  | "newSessionID"
  | "setSearchModalOpen"
  | "getBootstrapTask"
  | "parsePositiveInteger"
  | "resetComposerFileDragDepth"
  | "runtimeFlags"
  | "WEB_CHAT_CHANNEL"
  | "QQ_CHANNEL"
  | "TRASH_ICON_SVG"
  | "PROMPT_TEMPLATE_PREFIX"
  | "PROMPT_TEMPLATE_NAME_PATTERN"
  | "PROMPT_TEMPLATE_ARG_KEY_PATTERN"
  | "PROMPT_TEMPLATE_PLACEHOLDER_PATTERN"
  | "PROMPT_MODE_META_KEY"
  | "chatList"
  | "chatTitle"
  | "chatSession"
  | "chatPromptModeSelect"
  | "chatCollaborationModeSelect"
  | "searchChatInput"
  | "searchChatResults"
  | "messageList"
  | "thinkingIndicator"
  | "composerMain"
  | "messageInput"
  | "sendButton"
  | "composerAttachButton"
  | "composerAttachInput"
>;

export type ModelDomainContext = Pick<WebAppContext,
  | "state"
  | "t"
  | "setStatus"
  | "asErrorMessage"
  | "requestJSON"
  | "syncControlState"
  | "syncCustomSelect"
  | "appendEmptyItem"
  | "formatModelEntry"
  | "formatCapabilities"
  | "formatProviderLabel"
  | "normalizeProviders"
  | "normalizeDefaults"
  | "buildDefaultMapFromProviders"
  | "normalizeModelSlot"
  | "parseIntegerInput"
  | "renderComposerTokenEstimate"
  | "toRecord"
  | "TRASH_ICON_SVG"
  | "QQ_CHANNEL"
  | "DEFAULT_QQ_API_BASE"
  | "QQ_SANDBOX_API_BASE"
  | "DEFAULT_QQ_TOKEN_URL"
  | "DEFAULT_QQ_TIMEOUT_SECONDS"
  | "DEFAULT_OPENAI_MODEL_IDS"
  | "BUILTIN_PROVIDER_IDS"
  | "PROVIDER_AUTO_SAVE_DELAY_MS"
  | "composerProviderSelect"
  | "composerModelSelect"
  | "modelsSettingsSection"
  | "channelsSettingsSection"
  | "channelsEntryList"
  | "channelsLevel1View"
  | "channelsLevel2View"
  | "qqChannelEnabledInput"
  | "qqChannelAppIDInput"
  | "qqChannelClientSecretInput"
  | "qqChannelBotPrefixInput"
  | "qqChannelTargetTypeSelect"
  | "qqChannelAPIEnvironmentSelect"
  | "qqChannelTimeoutSecondsInput"
  | "modelsProviderAPIKeyInput"
  | "modelsProviderAPIKeyVisibilityButton"
  | "modelsProviderBaseURLInput"
  | "modelsProviderBaseURLPreview"
  | "modelsLevel1View"
  | "modelsLevel2View"
  | "modelsEditProviderMeta"
  | "modelsProviderList"
  | "modelsProviderTypeSelect"
  | "modelsProviderNameInput"
  | "modelsProviderTimeoutMSInput"
  | "modelsProviderReasoningEffortField"
  | "modelsProviderReasoningEffortSelect"
  | "modelsProviderEnabledInput"
  | "modelsProviderStoreField"
  | "modelsProviderStoreInput"
  | "modelsProviderHeadersRows"
  | "modelsProviderAliasesRows"
  | "modelsProviderCustomModelsField"
  | "modelsProviderCustomModelsRows"
  | "modelsProviderCustomModelsAddButton"
  | "modelsProviderModalTitle"
>;

export type WorkspaceDomainContext = Pick<WebAppContext,
  | "state"
  | "t"
  | "setStatus"
  | "syncControlState"
  | "asWorkspaceErrorMessage"
  | "requestJSON"
  | "normalizeWorkspacePathKey"
  | "isSystemPromptWorkspacePath"
  | "invalidateSystemPromptTokensCacheAndReload"
  | "appendEmptyItem"
  | "setWorkspaceEditorModalOpen"
  | "setWorkspaceImportModalOpen"
  | "DEFAULT_WORKSPACE_CARD_ENABLED"
  | "WORKSPACE_CARD_KEYS"
  | "WORKSPACE_CODEX_PREFIX"
  | "WORKSPACE_CLAUDE_PREFIX"
  | "TRASH_ICON_SVG"
  | "workspaceEntryList"
  | "workspaceLevel1View"
  | "workspaceLevel2ConfigView"
  | "workspaceLevel2PromptView"
  | "workspaceLevel2CodexView"
  | "workspaceLevel2ClaudeView"
  | "workspaceSettingsSection"
  | "workspaceFilesBody"
  | "workspacePromptsBody"
  | "workspaceCodexTreeBody"
  | "workspaceClaudeBody"
  | "workspaceFilePathInput"
  | "workspaceFileContentInput"
  | "workspaceSaveFileButton"
  | "workspaceDeleteFileButton"
  | "workspaceJSONInput"
>;

export type CronDomainContext = Pick<WebAppContext,
  | "state"
  | "t"
  | "setStatus"
  | "asErrorMessage"
  | "syncControlState"
  | "requestJSON"
  | "reloadChats"
  | "compactTime"
  | "syncCustomSelect"
  | "parseIntegerInput"
  | "newSessionID"
  | "createDefaultCronWorkflow"
  | "validateCronWorkflowSpec"
  | "CronWorkflowCanvas"
  | "DEFAULT_CRON_JOB_ID"
  | "CRON_META_SYSTEM_DEFAULT"
  | "cronCreateOpenButton"
  | "cronCreateModal"
  | "cronCreateModalCloseButton"
  | "cronTaskTypeSelect"
  | "cronIDInput"
  | "cronNameInput"
  | "cronIntervalInput"
  | "cronSessionIDInput"
  | "cronMaxConcurrencyInput"
  | "cronTimeoutInput"
  | "cronMisfireInput"
  | "cronTextInput"
  | "cronResetWorkflowButton"
  | "cronWorkflowFullscreenButton"
  | "refreshCronButton"
  | "cronNewSessionButton"
  | "cronCreateForm"
  | "cronJobsBody"
  | "cronDispatchHint"
  | "cronCreateModalTitle"
  | "cronSubmitButton"
  | "cronTextSection"
  | "cronWorkflowSection"
  | "cronWorkflowViewport"
  | "cronWorkflowCanvas"
  | "cronWorkflowEdges"
  | "cronWorkflowNodes"
  | "cronWorkflowNodeEditor"
  | "cronWorkflowZoom"
  | "cronWorkflowExecutionList"
  | "cronWorkbench"
>;

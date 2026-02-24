import type {
  WorkspaceDomainContext,
  WorkspaceCardKey,
  WorkspaceEditorMode,
  WorkspaceFileCatalog,
  WorkspaceFileInfo,
  WorkspaceSettingsLevel,
  WorkspaceTextPayload,
} from "./types.js";

export function createWorkspaceDomain(ctx: WorkspaceDomainContext) {
  const {
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
    TRASH_ICON_SVG,
    workspaceEntryList,
    workspaceLevel1View,
    workspaceLevel2ConfigView,
    workspaceLevel2PromptView,
    workspaceSettingsSection,
    workspaceFilesBody,
    workspacePromptsBody,
    workspaceFilePathInput,
    workspaceFileContentInput,
    workspaceSaveFileButton,
    workspaceDeleteFileButton,
    workspaceJSONInput,
  } = ctx;

function setWorkspaceSettingsLevel(level: WorkspaceSettingsLevel): void {
  state.workspaceSettingsLevel = level === "config" || level === "prompt" ? level : "list";
  const showList = state.workspaceSettingsLevel === "list";
  workspaceLevel1View.hidden = !showList;
  workspaceLevel2ConfigView.hidden = state.workspaceSettingsLevel !== "config";
  workspaceLevel2PromptView.hidden = state.workspaceSettingsLevel !== "prompt";
  workspaceSettingsSection.classList.toggle("is-level2-active", !showList);
}

function parseWorkspaceCardEnabled(raw: unknown): Record<WorkspaceCardKey, boolean> {
  const next = { ...DEFAULT_WORKSPACE_CARD_ENABLED };
  if (!raw || typeof raw !== "object") {
    return next;
  }
  const source = raw as Record<string, unknown>;
  for (const card of WORKSPACE_CARD_KEYS as WorkspaceCardKey[]) {
    if (typeof source[card] === "boolean") {
      next[card] = source[card] as boolean;
    }
  }
  return next;
}

function isWorkspaceCardKey(value: string | undefined): value is WorkspaceCardKey {
  return value === "config" || value === "prompt";
}

function isWorkspaceCardEnabled(card: WorkspaceCardKey): boolean {
  return state.workspaceCardEnabled[card] !== false;
}

function resolveWorkspaceCardTitle(card: WorkspaceCardKey): string {
  if (card === "config") {
    return t("workspace.configCardTitle");
  }
  return t("workspace.promptCardTitle");
}

function ensureWorkspaceCardEnabled(card: WorkspaceCardKey): boolean {
  if (isWorkspaceCardEnabled(card)) {
    return true;
  }
  setStatus(t("status.workspaceCardBlocked", { card: resolveWorkspaceCardTitle(card) }), "info");
  return false;
}

function setWorkspaceCardEnabled(card: WorkspaceCardKey, enabled: boolean): void {
  if (state.workspaceCardEnabled[card] === enabled) {
    return;
  }
  state.workspaceCardEnabled[card] = enabled;
  if (!enabled && state.workspaceSettingsLevel === card) {
    setWorkspaceSettingsLevel("list");
  }
  syncControlState();
  renderWorkspacePanel();
  setStatus(
    t(enabled ? "status.workspaceCardEnabled" : "status.workspaceCardDisabled", {
      card: resolveWorkspaceCardTitle(card),
    }),
    "info",
  );
}

function appendWorkspaceNavigationCard(
  card: WorkspaceCardKey,
  action: "open-config" | "open-prompt",
  selected: boolean,
  titleText: string,
  descText: string,
  fileCount: number,
): void {
  const enabled = isWorkspaceCardEnabled(card);
  const entry = document.createElement("li");
  entry.className = "models-provider-card-entry workspace-entry-card-entry";

  const button = document.createElement("button");
  button.type = "button";
  button.className = "models-provider-card channels-entry-card workspace-entry-card";
  button.dataset.workspaceAction = action;
  button.disabled = !enabled;
  button.setAttribute("aria-disabled", String(!enabled));
  if (selected) {
    button.classList.add("is-selected");
  }
  if (!enabled) {
    button.classList.add("is-disabled");
  }
  button.setAttribute("aria-pressed", String(selected));

  const title = document.createElement("span");
  title.className = "models-provider-card-title";
  title.textContent = titleText;

  const desc = document.createElement("span");
  desc.className = "models-provider-card-meta";
  desc.textContent = descText;

  const status = document.createElement("span");
  status.className = "models-provider-card-meta workspace-entry-card-status";
  status.textContent = enabled ? t("workspace.cardEnabled") : t("workspace.cardDisabled");

  const countMeta = document.createElement("span");
  countMeta.className = "models-provider-card-meta";
  countMeta.textContent = t("workspace.cardFileCount", { count: fileCount });

  button.append(title, desc, status, countMeta);
  entry.appendChild(button);

  const toggleButton = document.createElement("button");
  toggleButton.type = "button";
  toggleButton.className = "secondary-btn workspace-entry-toggle-btn";
  toggleButton.dataset.workspaceToggleCard = card;
  toggleButton.setAttribute("aria-pressed", String(enabled));
  toggleButton.setAttribute("aria-label", `${enabled ? t("workspace.disableCard") : t("workspace.enableCard")} ${titleText}`);
  toggleButton.textContent = enabled ? t("workspace.disableCard") : t("workspace.enableCard");
  entry.appendChild(toggleButton);

  workspaceEntryList.appendChild(entry);
}

function renderWorkspaceNavigation(configCount: number, promptCount: number): void {
  workspaceEntryList.innerHTML = "";
  appendWorkspaceNavigationCard(
    "config",
    "open-config",
    state.workspaceSettingsLevel === "config",
    t("workspace.configCardTitle"),
    t("workspace.briefGeneric"),
    configCount,
  );
  appendWorkspaceNavigationCard(
    "prompt",
    "open-prompt",
    state.workspaceSettingsLevel === "prompt",
    t("workspace.promptCardTitle"),
    t("workspace.briefAITools"),
    promptCount,
  );
}

async function refreshWorkspace(options: { silent?: boolean } = {}): Promise<void> {
  syncControlState();
  try {
    const catalog = await fetchWorkspaceFileCatalog();
    applyWorkspaceFileCatalog(catalog);
    if (!options.silent) {
      setStatus(t("status.workspaceFilesLoaded", { count: catalog.files.length }), "info");
    }
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

function applyWorkspaceFileCatalog(catalog: WorkspaceFileCatalog): void {
  state.workspaceFileCatalog = catalog;
  syncActiveWorkspaceSelection(catalog.files);
  renderWorkspacePanel();
  state.tabLoaded.workspace = true;
}

function syncActiveWorkspaceSelection(files: WorkspaceFileInfo[]): void {
  if (state.activeWorkspacePath === "") {
    return;
  }
  if (!files.some((file) => file.path === state.activeWorkspacePath)) {
    clearWorkspaceSelection();
  }
}

function renderWorkspacePanel(): void {
  renderWorkspaceFiles();
  renderWorkspaceEditor();
  setWorkspaceSettingsLevel(state.workspaceSettingsLevel);
}

function renderWorkspaceFiles(): void {
  const { configFiles, promptFiles } = state.workspaceFileCatalog;
  renderWorkspaceNavigation(configFiles.length, promptFiles.length);
  renderWorkspaceConfigAndPromptFiles(configFiles, promptFiles);
}

function renderWorkspaceConfigAndPromptFiles(
  configFiles: WorkspaceFileInfo[],
  promptFiles: WorkspaceFileInfo[],
): void {
  renderWorkspaceFileRows(workspaceFilesBody, configFiles, t("workspace.emptyConfig"));
  renderWorkspaceFileRows(workspacePromptsBody, promptFiles, t("workspace.emptyPrompt"));
}

function splitWorkspaceFiles(
  files: WorkspaceFileInfo[],
): { configFiles: WorkspaceFileInfo[]; promptFiles: WorkspaceFileInfo[] } {
  const configFiles: WorkspaceFileInfo[] = [];
  const promptFiles: WorkspaceFileInfo[] = [];
  for (const file of files) {
    if (isWorkspacePromptFile(file)) {
      promptFiles.push(file);
      continue;
    }
    configFiles.push(file);
  }
  return { configFiles, promptFiles };
}

function createWorkspaceFileCatalog(files: WorkspaceFileInfo[]): WorkspaceFileCatalog {
  const groups = splitWorkspaceFiles(files);
  return {
    files,
    configFiles: groups.configFiles,
    promptFiles: groups.promptFiles,
  };
}

function renderWorkspaceFileRows(
  targetBody: HTMLUListElement,
  files: WorkspaceFileInfo[],
  emptyText: string,
): void {
  targetBody.innerHTML = "";
  if (files.length === 0) {
    appendEmptyItem(targetBody, emptyText);
    return;
  }

  files.forEach((file) => {
    targetBody.appendChild(createWorkspaceFileRow(file));
  });
}

function createWorkspaceFileRow(file: WorkspaceFileInfo): HTMLLIElement {
  const entry = document.createElement("li");
  entry.className = "models-provider-card-entry workspace-file-card-entry";
  entry.append(buildWorkspaceFileOpenButton(file), buildWorkspaceFileDeleteButton(file));
  return entry;
}

function buildWorkspaceFileOpenButton(file: WorkspaceFileInfo): HTMLButtonElement {
  const openButton = document.createElement("button");
  openButton.type = "button";
  openButton.className = "models-provider-card workspace-file-open-card";
  openButton.dataset.workspaceOpen = file.path;
  if (file.path === state.activeWorkspacePath) {
    openButton.classList.add("is-selected");
  }
  openButton.setAttribute("aria-pressed", String(file.path === state.activeWorkspacePath));

  const pathTitle = document.createElement("span");
  pathTitle.className = "models-provider-card-title mono workspace-file-card-path";
  pathTitle.textContent = file.path;

  const summaryMeta = document.createElement("span");
  summaryMeta.className = "models-provider-card-meta";
  summaryMeta.textContent = resolveWorkspaceFileSummary(file);

  const sizeMeta = document.createElement("span");
  sizeMeta.className = "models-provider-card-meta";
  sizeMeta.textContent = `${t("workspace.size")}: ${file.size === null ? t("common.none") : String(file.size)}`;

  openButton.append(pathTitle, summaryMeta, sizeMeta);
  return openButton;
}

function buildWorkspaceFileDeleteButton(file: WorkspaceFileInfo): HTMLButtonElement {
  const deleteButton = document.createElement("button");
  deleteButton.type = "button";
  const deleteLabel = t("workspace.deleteFile");
  deleteButton.className = "models-provider-card-delete chat-delete-btn workspace-file-card-delete";
  deleteButton.dataset.workspaceDelete = file.path;
  deleteButton.setAttribute("aria-label", deleteLabel);
  deleteButton.title = deleteLabel;
  deleteButton.innerHTML = TRASH_ICON_SVG;
  deleteButton.disabled = file.kind !== "skill";
  return deleteButton;
}

function resolveWorkspaceFileSummary(file: WorkspaceFileInfo): string {
  const path = normalizeWorkspacePathKey(file.path);
  if (path === "config/envs.json") {
    return t("workspace.briefEnvs");
  }
  if (path === "config/channels.json") {
    return t("workspace.briefChannels");
  }
  if (path === "config/models.json") {
    return t("workspace.briefModels");
  }
  if (path === "config/active-llm.json") {
    return t("workspace.briefActiveLLM");
  }
  if (file.kind === "skill") {
    return t("workspace.briefSkill");
  }
  if (path.startsWith("docs/ai/") || path.startsWith("prompts/") || path.startsWith("prompt/")) {
    return t("workspace.briefAITools");
  }
  return t("workspace.briefGeneric");
}

function isWorkspacePromptFile(file: WorkspaceFileInfo): boolean {
  const path = normalizeWorkspacePathKey(file.path);
  if (file.kind === "skill") {
    return true;
  }
  if (path.startsWith("docs/ai/") && (path.endsWith(".md") || path.endsWith(".markdown"))) {
    return true;
  }
  return path.startsWith("prompts/") || path.startsWith("prompt/");
}

function renderWorkspaceEditor(): void {
  const hasActiveFile = state.activeWorkspacePath !== "";
  const canDelete = hasActiveFile && isWorkspaceSkillPath(state.activeWorkspacePath);
  workspaceFilePathInput.value = state.activeWorkspacePath;
  workspaceFileContentInput.value = state.activeWorkspaceContent;
  workspaceFileContentInput.disabled = !hasActiveFile;
  workspaceSaveFileButton.disabled = !hasActiveFile;
  workspaceDeleteFileButton.disabled = !canDelete;
}

async function openWorkspaceFile(path: string, options: { silent?: boolean } = {}): Promise<void> {
  syncControlState();
  try {
    const payload = await requestWorkspaceFile(path);
    setWorkspaceEditorStateFromPayload(path, payload);
    renderWorkspacePanel();
    setWorkspaceEditorModalOpen(true);
    if (!options.silent) {
      setStatus(t("status.workspaceFileLoaded", { path }), "info");
    }
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

function setWorkspaceEditorStateFromPayload(path: string, payload: unknown): void {
  const prepared = prepareWorkspaceEditorPayload(payload);
  state.activeWorkspacePath = path;
  state.activeWorkspaceContent = prepared.content;
  state.activeWorkspaceMode = prepared.mode;
}

async function saveWorkspaceFile(): Promise<void> {
  syncControlState();
  const draft = collectWorkspaceSaveDraft();
  if (!draft) {
    return;
  }
  try {
    await requestWorkspaceFileUpdate(draft.path, draft.payload);
    setWorkspaceEditorStateFromPayload(draft.path, draft.payload);
    await refreshWorkspace({ silent: true });
    afterWorkspaceFileSaved(draft.path);
    setStatus(t("status.workspaceFileSaved", { path: draft.path }), "info");
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

function collectWorkspaceSaveDraft(): { path: string; payload: unknown } | null {
  const path = normalizeWorkspaceInputPath(workspaceFilePathInput.value);
  if (path === "") {
    setStatus(t("error.workspacePathRequired"), "error");
    return null;
  }
  const payload = resolveWorkspaceEditorPayload(state.activeWorkspaceMode, workspaceFileContentInput.value);
  if (payload === null) {
    setStatus(t("error.workspaceInvalidJSON"), "error");
    return null;
  }
  return { path, payload };
}

function resolveWorkspaceEditorPayload(mode: WorkspaceEditorMode, content: string): unknown | null {
  if (mode === "text") {
    return { content };
  }
  try {
    return JSON.parse(content);
  } catch {
    return null;
  }
}

function afterWorkspaceFileSaved(path: string): void {
  if (isSystemPromptWorkspacePath(path)) {
    invalidateSystemPromptTokensCacheAndReload();
  }
}

async function deleteWorkspaceFile(path: string): Promise<void> {
  syncControlState();
  if (!confirmWorkspaceFileDeletion(path)) {
    return;
  }

  try {
    await requestWorkspaceFileDeletion(path);
    afterWorkspaceFileDeleted(path);
    await refreshWorkspace({ silent: true });
    setStatus(t("status.workspaceFileDeleted", { path }), "info");
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

function confirmWorkspaceFileDeletion(path: string): boolean {
  return window.confirm(t("workspace.deleteFileConfirm", { path }));
}

function afterWorkspaceFileDeleted(path: string): void {
  if (state.activeWorkspacePath === path) {
    clearWorkspaceSelection();
  }
}

async function importWorkspaceJSON(): Promise<void> {
  syncControlState();
  const payload = parseWorkspaceImportInput(workspaceJSONInput.value);
  if (payload === null) {
    return;
  }
  try {
    await requestWorkspaceImport(payload);
    clearWorkspaceSelection();
    await refreshWorkspace({ silent: true });
    setWorkspaceImportModalOpen(false);
    setStatus(t("status.workspaceImportDone"), "info");
  } catch (error) {
    setStatus(asWorkspaceErrorMessage(error), "error");
  }
}

function parseWorkspaceImportInput(raw: string): unknown | null {
  const trimmed = raw.trim();
  if (trimmed === "") {
    setStatus(t("error.workspaceJSONRequired"), "error");
    return null;
  }
  try {
    return JSON.parse(trimmed);
  } catch {
    setStatus(t("error.workspaceInvalidJSON"), "error");
    return null;
  }
}

function buildWorkspaceImportBody(payload: unknown): unknown {
  if (payload && typeof payload === "object" && "mode" in (payload as Record<string, unknown>)) {
    return payload;
  }
  return {
    mode: "replace",
    payload,
  };
}

async function fetchWorkspaceFileCatalog(): Promise<WorkspaceFileCatalog> {
  const raw = await requestWorkspaceFiles();
  return normalizeWorkspaceFileCatalog(raw);
}

async function requestWorkspaceFiles(): Promise<unknown> {
  return requestJSON("/workspace/files");
}

function buildWorkspaceFileRequestPath(path: string): string {
  return `/workspace/files/${encodeURIComponent(path)}`;
}

async function requestWorkspaceFile(path: string): Promise<unknown> {
  return requestJSON(buildWorkspaceFileRequestPath(path));
}

async function requestWorkspaceFileUpdate(path: string, payload: unknown): Promise<void> {
  await requestJSON(buildWorkspaceFileRequestPath(path), {
    method: "PUT",
    body: payload,
  });
}

async function requestWorkspaceFileDeletion(path: string): Promise<void> {
  await requestJSON(buildWorkspaceFileRequestPath(path), {
    method: "DELETE",
  });
}

async function requestWorkspaceImport(payload: unknown): Promise<void> {
  await requestJSON("/workspace/import", {
    method: "POST",
    body: buildWorkspaceImportBody(payload),
  });
}

function normalizeWorkspaceFileCatalog(raw: unknown): WorkspaceFileCatalog {
  const files = normalizeWorkspaceFiles(raw);
  return createWorkspaceFileCatalog(files);
}

function normalizeWorkspaceFiles(raw: unknown): WorkspaceFileInfo[] {
  const rows = collectWorkspaceFileRows(raw);
  const byPath = new Map<string, WorkspaceFileInfo>();
  for (const row of rows) {
    const next = parseWorkspaceFileInfoRow(row);
    if (!next) {
      continue;
    }
    mergeWorkspaceFileInfo(byPath, next);
  }

  return Array.from(byPath.values()).sort((a, b) => a.path.localeCompare(b.path));
}

function collectWorkspaceFileRows(raw: unknown): unknown[] {
  const rows: unknown[] = [];
  if (Array.isArray(raw)) {
    rows.push(...raw);
    return rows;
  }
  if (!raw || typeof raw !== "object") {
    return rows;
  }

  const obj = raw as Record<string, unknown>;
  if (Array.isArray(obj.files)) {
    rows.push(...obj.files);
  } else if (obj.files && typeof obj.files === "object") {
    rows.push(...Object.entries(obj.files as Record<string, unknown>).map(([path, value]) => ({ path, value })));
  }
  if (Array.isArray(obj.items)) {
    rows.push(...obj.items);
  }
  if (rows.length === 0) {
    rows.push(...Object.entries(obj).map(([path, value]) => ({ path, value })));
  }
  return rows;
}

function parseWorkspaceFileInfoRow(row: unknown): WorkspaceFileInfo | null {
  if (typeof row === "string") {
    const path = row.trim();
    if (path === "") {
      return null;
    }
    return {
      path,
      kind: resolveWorkspaceFileKindFromPath(path, "config"),
      size: null,
    };
  }
  if (!row || typeof row !== "object") {
    return null;
  }

  const item = row as Record<string, unknown>;
  const path = resolveWorkspaceFilePathFromRow(item);
  if (path === "") {
    return null;
  }
  return {
    path,
    kind: resolveWorkspaceFileKindFromRow(item, path),
    size: resolveWorkspaceFileSizeFromRow(item),
  };
}

function resolveWorkspaceFilePathFromRow(item: Record<string, unknown>): string {
  if (typeof item.path === "string") {
    return item.path.trim();
  }
  if (typeof item.name === "string") {
    return item.name.trim();
  }
  if (typeof item.file === "string") {
    return item.file.trim();
  }
  return "";
}

function resolveWorkspaceFileSizeFromRow(item: Record<string, unknown>): number | null {
  if (typeof item.size === "number" && Number.isFinite(item.size)) {
    return item.size;
  }
  if (typeof item.bytes === "number" && Number.isFinite(item.bytes)) {
    return item.bytes;
  }
  return null;
}

function resolveWorkspaceFileKindFromRow(item: Record<string, unknown>, path: string): "config" | "skill" {
  const rawKind: "config" | "skill" = item.kind === "skill" ? "skill" : "config";
  return resolveWorkspaceFileKindFromPath(path, rawKind);
}

function resolveWorkspaceFileKindFromPath(path: string, fallback: "config" | "skill"): "config" | "skill" {
  if (fallback === "config" && path.startsWith("skills/") && path.endsWith(".json")) {
    return "skill";
  }
  return fallback;
}

function mergeWorkspaceFileInfo(byPath: Map<string, WorkspaceFileInfo>, next: WorkspaceFileInfo): void {
  const prev = byPath.get(next.path);
  if (!prev || (prev.size === null && next.size !== null)) {
    byPath.set(next.path, next);
  }
}

function clearWorkspaceSelection(): void {
  state.activeWorkspacePath = "";
  state.activeWorkspaceContent = "";
  state.activeWorkspaceMode = "json";
}

function prepareWorkspaceEditorPayload(payload: unknown): { content: string; mode: WorkspaceEditorMode } {
  const textPayload = asWorkspaceTextPayload(payload);
  if (textPayload) {
    return {
      content: textPayload.content,
      mode: "text",
    };
  }
  return {
    content: JSON.stringify(payload, null, 2),
    mode: "json",
  };
}

function asWorkspaceTextPayload(payload: unknown): WorkspaceTextPayload | null {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return null;
  }
  const record = payload as Record<string, unknown>;
  const keys = Object.keys(record);
  if (keys.length !== 1 || keys[0] !== "content") {
    return null;
  }
  return typeof record.content === "string" ? { content: record.content } : null;
}

function normalizeWorkspaceInputPath(path: string): string {
  return path.trim().replace(/^\/+/, "");
}

function isWorkspaceSkillPath(path: string): boolean {
  if (!path.startsWith("skills/") || !path.endsWith(".json")) {
    return false;
  }
  const name = path.slice("skills/".length, path.length - ".json".length).trim();
  return name !== "" && !name.includes("/");
}

  return {
    setWorkspaceSettingsLevel,
    parseWorkspaceCardEnabled,
    isWorkspaceCardKey,
    ensureWorkspaceCardEnabled,
    setWorkspaceCardEnabled,
    refreshWorkspace,
    renderWorkspacePanel,
    openWorkspaceFile,
    saveWorkspaceFile,
    deleteWorkspaceFile,
    importWorkspaceJSON,
    clearWorkspaceSelection,
    isWorkspaceSkillPath,
    normalizeWorkspaceInputPath,
    requestWorkspaceFile,
  };
}

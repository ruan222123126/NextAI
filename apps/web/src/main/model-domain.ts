type ModelsSettingsLevel = "list" | "edit";
type ChannelsSettingsLevel = "list" | "edit";
type ProviderKVKind = "headers" | "aliases";
type QQTargetType = "c2c" | "group" | "guild";
type QQAPIEnvironment = "production" | "sandbox";

interface ModelModalities {
  text: boolean;
  audio: boolean;
  image: boolean;
  video: boolean;
  pdf: boolean;
}

interface ModelCapabilities {
  temperature: boolean;
  reasoning: boolean;
  attachment: boolean;
  tool_call: boolean;
  input?: ModelModalities;
  output?: ModelModalities;
}

interface ModelLimit {
  context?: number;
  input?: number;
  output?: number;
}

interface ModelInfo {
  id: string;
  name: string;
  status?: string;
  alias_of?: string;
  capabilities?: ModelCapabilities;
  limit?: ModelLimit;
}

interface ProviderInfo {
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

interface ProviderTypeInfo {
  id: string;
  display_name: string;
}

interface ComposerModelOption {
  value: string;
  canonical: string;
  label: string;
}

interface ModelSlotConfig {
  provider_id: string;
  model: string;
}

interface ModelCatalogInfo {
  providers: ProviderInfo[];
  provider_types?: ProviderTypeInfo[];
  defaults: Record<string, string>;
  active_llm?: ModelSlotConfig;
}

interface ActiveModelsInfo {
  active_llm?: ModelSlotConfig;
}

interface DeleteResult {
  deleted: boolean;
}

interface UpsertProviderOptions {
  closeAfterSave?: boolean;
  notifyStatus?: boolean;
}

interface QQChannelConfig {
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

export function createModelDomain(ctx: any) {
  const {
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
  } = ctx;

  let syncingComposerModelSelectors = false;
  let providerAutoSaveTimer: number | null = null;
  let providerAutoSaveInFlight = false;
  let providerAutoSaveQueued = false;

async function refreshModels(options: { silent?: boolean } = {}): Promise<void> {
  syncControlState();
  try {
    const result = await syncModelState({ autoActivate: true });
    state.tabLoaded.models = true;
    renderModelsPanel();
    if (!options.silent) {
      setStatus(
        t(result.source === "catalog" ? "status.providersLoadedCatalog" : "status.providersLoadedLegacy", {
          count: result.providers.length,
        }),
        "info",
      );
    }
  } catch (error) {
    if (!options.silent) {
      setStatus(asErrorMessage(error), "error");
    }
  }
}

async function syncModelState(options: { autoActivate: boolean }): Promise<{
  providers: ProviderInfo[];
  providerTypes: ProviderTypeInfo[];
  defaults: Record<string, string>;
  activeLLM: ModelSlotConfig;
  source: "catalog" | "legacy";
}> {
  const result = await loadModelCatalog();
  state.providers = result.providers;
  state.providerTypes = result.providerTypes;
  state.modelDefaults = result.defaults;
  state.activeLLM = result.activeLLM;
  renderComposerModelSelectors();

  if (options.autoActivate) {
    const autoActivated = await maybeAutoActivateModel(result.providers, result.defaults, result.activeLLM);
    if (autoActivated) {
      state.activeLLM = autoActivated;
      renderComposerModelSelectors();
      return {
        ...result,
        activeLLM: autoActivated,
      };
    }
  }

  return result;
}

async function maybeAutoActivateModel(
  providers: ProviderInfo[],
  defaults: Record<string, string>,
  activeLLM: ModelSlotConfig,
): Promise<ModelSlotConfig | null> {
  if (activeLLM.provider_id !== "" && activeLLM.model !== "") {
    return null;
  }
  const candidate = pickAutoActiveModelCandidate(providers, defaults);
  if (!candidate) {
    return null;
  }
  try {
    const out = await requestJSON("/models/active", {
      method: "PUT",
      body: {
        provider_id: candidate.providerID,
        model: candidate.modelID,
      },
    });
    const normalized = normalizeModelSlot(out.active_llm);
    if (normalized.provider_id === "" || normalized.model === "") {
      return null;
    }
    return normalized;
  } catch {
    return null;
  }
}

function pickAutoActiveModelCandidate(
  providers: ProviderInfo[],
  defaults: Record<string, string>,
): { providerID: string; modelID: string } | null {
  let fallback: { providerID: string; modelID: string } | null = null;
  for (const provider of providers) {
    if (provider.enabled === false || provider.has_api_key !== true) {
      continue;
    }
    if (provider.models.length === 0) {
      continue;
    }
    const defaultModel = (defaults[provider.id] ?? "").trim();
    if (defaultModel !== "" && provider.models.some((model) => model.id === defaultModel)) {
      return {
        providerID: provider.id,
        modelID: defaultModel,
      };
    }
    if (!fallback) {
      const firstModel = provider.models[0]?.id?.trim() ?? "";
      if (firstModel !== "") {
        fallback = {
          providerID: provider.id,
          modelID: firstModel,
        };
      }
    }
  }
  return fallback;
}

async function loadModelCatalog(): Promise<{
  providers: ProviderInfo[];
  providerTypes: ProviderTypeInfo[];
  defaults: Record<string, string>;
  activeLLM: ModelSlotConfig;
  source: "catalog" | "legacy";
}> {
  try {
    const catalog = await requestJSON("/models/catalog");
    const providers = normalizeProviders(catalog.providers);
    const providerTypes = normalizeProviderTypes(catalog.provider_types);
    return {
      providers,
      providerTypes,
      defaults: normalizeDefaults(catalog.defaults, providers),
      activeLLM: normalizeModelSlot(catalog.active_llm),
      source: "catalog",
    };
  } catch {
    const providersRaw = await requestJSON("/models");
    const providers = normalizeProviders(providersRaw);
    const activeResult = await requestJSON("/models/active");
    return {
      providers,
      providerTypes: fallbackProviderTypes(providers),
      defaults: buildDefaultMapFromProviders(providers),
      activeLLM: normalizeModelSlot(activeResult.active_llm),
      source: "legacy",
    };
  }
}

function listSelectableProviders(): ProviderInfo[] {
  return state.providers.filter((provider: ProviderInfo) => provider.enabled !== false && provider.models.length > 0);
}

function appendSelectOption(select: HTMLSelectElement, value: string, label: string): void {
  const option = document.createElement("option");
  option.value = value;
  option.textContent = label;
  select.appendChild(option);
}

function resolveComposerProvider(providers: ProviderInfo[]): ProviderInfo | null {
  if (providers.length === 0) {
    return null;
  }
  const active = providers.find((provider) => provider.id === state.activeLLM.provider_id);
  if (active) {
    return active;
  }
  const selected = providers.find((provider) => provider.id === composerProviderSelect.value.trim());
  if (selected) {
    return selected;
  }
  const withDefault = providers.find((provider) => {
    const defaultModel = (state.modelDefaults[provider.id] ?? "").trim();
    return defaultModel !== "" && provider.models.some((model) => model.id === defaultModel);
  });
  return withDefault ?? providers[0];
}

function formatComposerModelLabel(model: ModelInfo): string {
  const modelID = model.id.trim();
  if (modelID !== "") {
    return modelID;
  }
  return (model.name ?? "").trim();
}

function resolveComposerModelCanonicalID(model: ModelInfo): string {
  const modelID = model.id.trim();
  if (modelID === "") {
    return "";
  }
  const aliasOf = (model.alias_of ?? "").trim();
  if (aliasOf !== "" && aliasOf !== modelID) {
    return aliasOf;
  }
  return modelID;
}

function buildComposerModelOptions(provider: ProviderInfo): ComposerModelOption[] {
  const optionsByCanonical = new Map<string, ComposerModelOption>();
  for (const model of provider.models) {
    const modelID = model.id.trim();
    if (modelID === "") {
      continue;
    }
    const canonicalID = resolveComposerModelCanonicalID(model) || modelID;
    const option: ComposerModelOption = {
      value: modelID,
      canonical: canonicalID,
      label: formatComposerModelLabel(model),
    };
    const existing = optionsByCanonical.get(canonicalID);
    if (!existing) {
      optionsByCanonical.set(canonicalID, option);
      continue;
    }
    const existingIsAlias = existing.value !== existing.canonical;
    const currentIsAlias = option.value !== option.canonical;
    if (!existingIsAlias && currentIsAlias) {
      optionsByCanonical.set(canonicalID, option);
    }
  }
  return Array.from(optionsByCanonical.values());
}

function resolveComposerModelValue(options: ComposerModelOption[], requestedModelID: string): string {
  const modelID = requestedModelID.trim();
  if (modelID === "") {
    return "";
  }
  if (options.some((option) => option.value === modelID)) {
    return modelID;
  }
  const byCanonical = options.find((option) => option.canonical === modelID);
  return byCanonical?.value ?? "";
}

function resolveComposerModelID(provider: ProviderInfo, options: ComposerModelOption[]): string {
  const activeModel = state.activeLLM.provider_id === provider.id ? state.activeLLM.model.trim() : "";
  const activeValue = resolveComposerModelValue(options, activeModel);
  if (activeValue !== "") {
    return activeValue;
  }
  const selectedModel = composerModelSelect.value.trim();
  const selectedValue = resolveComposerModelValue(options, selectedModel);
  if (selectedValue !== "") {
    return selectedValue;
  }
  const defaultModel = (state.modelDefaults[provider.id] ?? "").trim();
  const defaultValue = resolveComposerModelValue(options, defaultModel);
  if (defaultValue !== "") {
    return defaultValue;
  }
  return options[0]?.value ?? "";
}

function renderComposerModelOptions(provider: ProviderInfo | null): string {
  composerModelSelect.innerHTML = "";
  if (!provider || provider.models.length === 0) {
    appendSelectOption(composerModelSelect, "", t("models.noModelOption"));
    composerModelSelect.value = "";
    composerModelSelect.disabled = true;
    syncCustomSelect(composerModelSelect);
    return "";
  }

  const options = buildComposerModelOptions(provider);
  if (options.length === 0) {
    appendSelectOption(composerModelSelect, "", t("models.noModelOption"));
    composerModelSelect.value = "";
    composerModelSelect.disabled = true;
    syncCustomSelect(composerModelSelect);
    return "";
  }

  for (const option of options) {
    appendSelectOption(composerModelSelect, option.value, option.label);
  }

  const resolvedModelID = resolveComposerModelID(provider, options);
  composerModelSelect.value = resolvedModelID;
  composerModelSelect.disabled = resolvedModelID === "";
  syncCustomSelect(composerModelSelect);
  return resolvedModelID;
}

function renderComposerModelSelectors(): void {
  const providers = listSelectableProviders();
  syncingComposerModelSelectors = true;
  try {
    composerProviderSelect.innerHTML = "";
    if (providers.length === 0) {
      appendSelectOption(composerProviderSelect, "", t("models.noProviderOption"));
      composerProviderSelect.value = "";
      composerProviderSelect.disabled = true;
      renderComposerModelOptions(null);
      syncCustomSelect(composerProviderSelect);
      return;
    }

    for (const provider of providers) {
      appendSelectOption(composerProviderSelect, provider.id, formatProviderLabel(provider));
    }

    const selectedProvider = resolveComposerProvider(providers);
    if (!selectedProvider) {
      composerProviderSelect.value = "";
      composerProviderSelect.disabled = true;
      renderComposerModelOptions(null);
      syncCustomSelect(composerProviderSelect);
      return;
    }

    composerProviderSelect.value = selectedProvider.id;
    composerProviderSelect.disabled = false;
    syncCustomSelect(composerProviderSelect);
    renderComposerModelOptions(selectedProvider);
  } finally {
    syncingComposerModelSelectors = false;
    renderComposerTokenEstimate();
  }
}

async function handleComposerProviderSelectChange(): Promise<void> {
  if (syncingComposerModelSelectors) {
    return;
  }
  const providers = listSelectableProviders();
  const selectedProvider = providers.find((provider) => provider.id === composerProviderSelect.value.trim()) ?? null;
  syncingComposerModelSelectors = true;
  let selectedModelID = "";
  try {
    selectedModelID = renderComposerModelOptions(selectedProvider);
  } finally {
    syncingComposerModelSelectors = false;
  }
  if (!selectedProvider || selectedModelID === "") {
    return;
  }
  await setActiveModel(selectedProvider.id, selectedModelID);
}

async function handleComposerModelSelectChange(): Promise<void> {
  if (syncingComposerModelSelectors) {
    return;
  }
  const providerID = composerProviderSelect.value.trim();
  const modelID = composerModelSelect.value.trim();
  if (providerID === "" || modelID === "") {
    setStatus(t("error.providerAndModelRequired"), "error");
    return;
  }
  await setActiveModel(providerID, modelID);
}

async function setActiveModel(providerID: string, modelID: string): Promise<boolean> {
  const normalizedProviderID = providerID.trim();
  const normalizedModelID = modelID.trim();
  if (normalizedProviderID === "" || normalizedModelID === "") {
    setStatus(t("error.providerAndModelRequired"), "error");
    return false;
  }
  if (state.activeLLM.provider_id === normalizedProviderID && state.activeLLM.model === normalizedModelID) {
    state.selectedProviderID = normalizedProviderID;
    return true;
  }
  syncControlState();
  try {
    const out = await requestJSON("/models/active", {
      method: "PUT",
      body: {
        provider_id: normalizedProviderID,
        model: normalizedModelID,
      },
    });
    const normalized = normalizeModelSlot(out.active_llm);
    state.activeLLM =
      normalized.provider_id === "" || normalized.model === ""
        ? {
            provider_id: normalizedProviderID,
            model: normalizedModelID,
          }
        : normalized;
    state.selectedProviderID = state.activeLLM.provider_id;
    renderComposerModelSelectors();
    if (state.tabLoaded.models) {
      renderModelsPanel();
    }
    setStatus(
      t("status.activeModelUpdated", {
        providerId: state.activeLLM.provider_id,
        model: state.activeLLM.model,
      }),
      "info",
    );
    return true;
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    renderComposerModelSelectors();
    return false;
  }
}

function renderModelsPanel(): void {
  syncSelectedProviderID();
  renderProviderNavigation();
  renderModelsSettingsLevel();
  renderProviderBaseURLPreview();
  setProviderAPIKeyVisibility(state.providerAPIKeyVisible);
}

function setChannelsSettingsLevel(level: ChannelsSettingsLevel): void {
  state.channelsSettingsLevel = level === "edit" ? "edit" : "list";
  const showEdit = state.channelsSettingsLevel === "edit";
  channelsLevel1View.hidden = showEdit;
  channelsLevel2View.hidden = !showEdit;
  channelsSettingsSection.classList.toggle("is-level2-active", showEdit);
}

function renderChannelNavigation(): void {
  channelsEntryList.innerHTML = "";

  const entry = document.createElement("li");
  entry.className = "models-provider-card-entry";

  const button = document.createElement("button");
  button.type = "button";
  button.className = "models-provider-card channels-entry-card";
  button.dataset.channelAction = "open";
  button.dataset.channelId = QQ_CHANNEL;
  if (state.channelsSettingsLevel === "edit") {
    button.classList.add("is-selected");
  }
  button.setAttribute("aria-pressed", String(state.channelsSettingsLevel === "edit"));

  const title = document.createElement("span");
  title.className = "models-provider-card-title";
  title.textContent = t("workspace.qqChannelTitle");

  const enabledMeta = document.createElement("span");
  enabledMeta.className = "models-provider-card-meta";
  enabledMeta.textContent = t("models.enabledLine", {
    enabled: state.qqChannelConfig.enabled ? t("common.yes") : t("common.no"),
  });

  const environmentMeta = document.createElement("span");
  environmentMeta.className = "models-provider-card-meta";
  const environment = resolveQQAPIEnvironment(state.qqChannelConfig.api_base);
  const environmentLabel = environment === "sandbox"
    ? t("workspace.qqAPIEnvironmentSandbox")
    : t("workspace.qqAPIEnvironmentProduction");
  environmentMeta.textContent = `${t("workspace.qqAPIEnvironment")}: ${environmentLabel}`;

  button.append(title, enabledMeta, environmentMeta);
  entry.appendChild(button);
  channelsEntryList.appendChild(entry);
}

function renderChannelsPanel(): void {
  renderChannelNavigation();
  renderQQChannelConfig();
  setChannelsSettingsLevel(state.channelsSettingsLevel);
}

function setProviderAPIKeyVisibility(visible: boolean): void {
  state.providerAPIKeyVisible = visible;
  modelsProviderAPIKeyInput.type = visible ? "text" : "password";
  modelsProviderAPIKeyVisibilityButton.classList.toggle("is-active", visible);
  modelsProviderAPIKeyVisibilityButton.setAttribute("aria-pressed", String(visible));
}

function renderProviderBaseURLPreview(): void {
  const providerType = resolveProviderTypeForPreview();
  const endpointPath = providerRequestEndpointPath(providerType);
  const base = modelsProviderBaseURLInput.value.trim().replace(/\/+$/g, "");
  modelsProviderBaseURLPreview.textContent = base === "" ? endpointPath : `${base}${endpointPath}`;
}

function setModelsSettingsLevel(level: ModelsSettingsLevel): void {
  const canShowEdit =
    level === "edit" &&
    (state.providerModal.open ||
      (state.providers.some((provider: ProviderInfo) => provider.id === state.selectedProviderID) && state.selectedProviderID !== ""));
  state.modelsSettingsLevel = canShowEdit ? "edit" : "list";
  const showEdit = state.modelsSettingsLevel === "edit";
  modelsLevel1View.hidden = showEdit;
  modelsLevel2View.hidden = !showEdit;
  modelsSettingsSection.classList.toggle("is-level2-active", showEdit);
}

function renderModelsSettingsLevel(): void {
  if (state.providerModal.open && state.providerModal.mode === "create") {
    modelsEditProviderMeta.textContent = t("models.addProvider");
  } else {
    const selected = state.providers.find((provider: ProviderInfo) => provider.id === state.selectedProviderID);
    modelsEditProviderMeta.textContent = selected ? formatProviderLabel(selected) : "";
  }
  setModelsSettingsLevel(state.modelsSettingsLevel);
}

function syncSelectedProviderID(): void {
  if (state.providers.length === 0) {
    state.selectedProviderID = "";
    return;
  }
  if (state.providers.some((provider: ProviderInfo) => provider.id === state.selectedProviderID)) {
    return;
  }
  if (state.activeLLM.provider_id !== "" && state.providers.some((provider: ProviderInfo) => provider.id === state.activeLLM.provider_id)) {
    state.selectedProviderID = state.activeLLM.provider_id;
    return;
  }
  state.selectedProviderID = state.providers[0].id;
}

function renderProviderNavigation(): void {
  modelsProviderList.innerHTML = "";
  if (state.providers.length === 0) {
    appendEmptyItem(modelsProviderList, t("models.emptyProviders"));
    return;
  }

  for (const provider of state.providers) {
    const entry = document.createElement("li");
    entry.className = "models-provider-card-entry";

    const button = document.createElement("button");
    button.type = "button";
    button.className = "models-provider-card";
    if (provider.id === state.selectedProviderID) {
      button.classList.add("is-selected");
    }
    button.dataset.providerAction = "select";
    button.dataset.providerId = provider.id;
    button.setAttribute("aria-pressed", String(provider.id === state.selectedProviderID));

    const title = document.createElement("span");
    title.className = "models-provider-card-title";
    title.textContent = formatProviderLabel(provider);

    const enabledMeta = document.createElement("span");
    enabledMeta.className = "models-provider-card-meta";
    enabledMeta.textContent = t("models.enabledLine", {
      enabled: provider.enabled === false ? t("common.no") : t("common.yes"),
    });

    const keyMeta = document.createElement("span");
    keyMeta.className = "models-provider-card-meta";
    keyMeta.textContent = provider.has_api_key ? t("models.apiKeyConfigured", { value: t("models.apiKeyMasked") }) : t("models.apiKeyUnset");

    const providerTypeMeta = document.createElement("span");
    providerTypeMeta.className = "models-provider-card-meta";
    providerTypeMeta.textContent = t("models.providerTypeLine", {
      providerType: providerTypeDisplayName(resolveProviderType(provider)),
    });

    const deleteButton = document.createElement("button");
    const deleteLabel = t("models.deleteProvider");
    deleteButton.type = "button";
    deleteButton.className = "models-provider-card-delete chat-delete-btn";
    deleteButton.dataset.providerAction = "delete";
    deleteButton.dataset.providerId = provider.id;
    deleteButton.setAttribute("aria-label", deleteLabel);
    deleteButton.title = deleteLabel;
    deleteButton.innerHTML = TRASH_ICON_SVG;

    button.append(title, enabledMeta, keyMeta, providerTypeMeta);
    entry.append(button, deleteButton);
    modelsProviderList.appendChild(entry);
  }
}

function renderProviderTypeOptions(selectedType?: string): void {
  const options = state.providerTypes.length > 0 ? state.providerTypes : fallbackProviderTypes(state.providers);
  if (options.length === 0) {
    modelsProviderTypeSelect.innerHTML = "";
    syncCustomSelect(modelsProviderTypeSelect);
    return;
  }
  const requestedType = normalizeProviderTypeValue(selectedType ?? modelsProviderTypeSelect.value);
  const hasRequestedType = requestedType ? options.some((item: ProviderTypeInfo) => item.id === requestedType) : false;
  const activeType = hasRequestedType ? requestedType : options[0].id;
  modelsProviderTypeSelect.innerHTML = "";
  for (const option of options) {
    const element = document.createElement("option");
    element.value = option.id;
    element.textContent = option.display_name;
    modelsProviderTypeSelect.appendChild(element);
  }
  if (requestedType && !hasRequestedType) {
    const selectedOption = document.createElement("option");
    selectedOption.value = requestedType;
    selectedOption.textContent = requestedType;
    modelsProviderTypeSelect.appendChild(selectedOption);
  }
  modelsProviderTypeSelect.value = activeType;
  syncCustomSelect(modelsProviderTypeSelect);
}

function normalizeProviderTypeValue(value: string): string {
  const normalized = value.trim().toLowerCase();
  if (normalized === "openai-compatible") {
    return normalized;
  }
  if (normalized === "codex-compatible") {
    return normalized;
  }
  if (normalized === "openai") {
    return normalized;
  }
  if (normalized.startsWith("codex-compatible-")) {
    return "codex-compatible";
  }
  if (normalized.startsWith("openai-compatible-")) {
    return "openai-compatible";
  }
  return normalized;
}

function normalizeProviderIDValue(value: string): string {
  return value.trim().toLowerCase();
}

function slugifyProviderID(value: string): string {
  return normalizeProviderIDValue(value)
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function ensureUniqueProviderID(baseProviderID: string): string {
  const base = normalizeProviderIDValue(baseProviderID);
  if (base === "") {
    return "";
  }
  const existing = new Set(
    state.providers
      .map((provider: ProviderInfo) => normalizeProviderIDValue(provider.id))
      .filter((providerID: string) => providerID !== ""),
  );
  if (!existing.has(base)) {
    return base;
  }
  let suffix = 2;
  while (existing.has(`${base}-${suffix}`)) {
    suffix += 1;
  }
  return `${base}-${suffix}`;
}

function resolveProviderIDForUpsert(selectedProviderType: string): string {
  if (state.providerModal.mode === "edit" && state.providerModal.editingProviderID !== "") {
    return state.providerModal.editingProviderID;
  }
  if (selectedProviderType === "") {
    return "";
  }
  if (selectedProviderType === "openai") {
    return ensureUniqueProviderID("openai");
  }
  if (selectedProviderType === "codex-compatible") {
    return ensureUniqueProviderID("codex-compatible");
  }
  const baseProviderID = slugifyProviderID(modelsProviderNameInput.value) || slugifyProviderID(selectedProviderType) || "provider";
  return ensureUniqueProviderID(baseProviderID);
}

function resolveOpenAIDuplicateModelAliases(): Record<string, string> {
  const modelIDs = (state.providers.find((provider: ProviderInfo) => provider.id === "openai")?.models ?? [])
    .map((model: ModelInfo) => model.id.trim())
    .filter((modelID: string) => modelID !== "");
  const source = modelIDs.length > 0 ? modelIDs : DEFAULT_OPENAI_MODEL_IDS;
  const out: Record<string, string> = {};
  for (const modelID of source) {
    out[modelID] = modelID;
  }
  return out;
}

function providerSupportsCustomModels(providerTypeID: string): boolean {
  const normalized = normalizeProviderTypeValue(providerTypeID);
  return normalized !== "";
}

function providerSupportsStore(providerTypeID: string): boolean {
  const normalized = normalizeProviderTypeValue(providerTypeID);
  if (normalized === "codex-compatible" || normalized === "openai-compatible") {
    return true;
  }
  const providerID = normalizeProviderIDValue(providerTypeID);
  if (providerID === "" || providerID === "openai") {
    return false;
  }
  const provider = state.providers.find(
    (item: ProviderInfo) => normalizeProviderIDValue(item.id) === providerID,
  );
  if (!provider) {
    return false;
  }
  if (isCodexCompatibleProviderID(provider.id)) {
    return true;
  }
  return provider.openai_compatible === true;
}

function providerSupportsReasoningEffort(providerTypeID: string): boolean {
  const normalized = normalizeProviderTypeValue(providerTypeID);
  if (normalized === "openai" || normalized === "openai-compatible" || normalized === "codex-compatible") {
    return true;
  }
  const providerID = normalizeProviderIDValue(providerTypeID);
  if (providerID === "") {
    return false;
  }
  if (providerID === "openai" || isCodexCompatibleProviderID(providerID)) {
    return true;
  }
  const provider = state.providers.find(
    (item: ProviderInfo) => normalizeProviderIDValue(item.id) === providerID,
  );
  if (!provider) {
    return false;
  }
  return provider.openai_compatible === true;
}

function syncProviderStoreField(providerTypeID: string): void {
  const enabled = providerSupportsStore(providerTypeID);
  modelsProviderStoreField.hidden = !enabled;
  modelsProviderStoreInput.disabled = !enabled;
  if (!enabled) {
    modelsProviderStoreInput.checked = false;
  }
}

function syncProviderReasoningEffortField(providerTypeID: string): void {
  const enabled = providerSupportsReasoningEffort(providerTypeID);
  modelsProviderReasoningEffortField.hidden = !enabled;
  modelsProviderReasoningEffortSelect.disabled = !enabled;
  if (!enabled) {
    modelsProviderReasoningEffortSelect.value = "";
  }
  syncCustomSelect(modelsProviderReasoningEffortSelect);
}

function syncProviderCustomModelsField(providerTypeID: string): void {
  const enabled = providerSupportsCustomModels(providerTypeID);
  syncProviderStoreField(providerTypeID);
  syncProviderReasoningEffortField(providerTypeID);
  modelsProviderCustomModelsField.hidden = false;
  modelsProviderCustomModelsAddButton.disabled = !enabled;
  for (const input of Array.from(
    modelsProviderCustomModelsRows.querySelectorAll("input[data-custom-model-input=\"true\"]"),
  ) as HTMLInputElement[]) {
    input.disabled = !enabled;
  }
  if (!enabled) {
    resetProviderCustomModelsEditor();
  }
}

function isProviderAutoSaveEnabled(): boolean {
  return state.providerModal.open && state.providerModal.mode === "edit" && state.providerModal.editingProviderID !== "";
}

function resetProviderAutoSaveScheduling(): void {
  if (providerAutoSaveTimer !== null) {
    window.clearTimeout(providerAutoSaveTimer);
    providerAutoSaveTimer = null;
  }
  providerAutoSaveQueued = false;
}

function scheduleProviderAutoSave(): void {
  if (!isProviderAutoSaveEnabled()) {
    return;
  }
  if (providerAutoSaveTimer !== null) {
    window.clearTimeout(providerAutoSaveTimer);
  }
  providerAutoSaveTimer = window.setTimeout(() => {
    providerAutoSaveTimer = null;
    void flushProviderAutoSave();
  }, PROVIDER_AUTO_SAVE_DELAY_MS);
}

async function flushProviderAutoSave(): Promise<void> {
  if (!isProviderAutoSaveEnabled()) {
    return;
  }
  if (providerAutoSaveInFlight) {
    providerAutoSaveQueued = true;
    return;
  }

  providerAutoSaveInFlight = true;
  try {
    await upsertProvider({
      closeAfterSave: false,
      notifyStatus: false,
    });
  } finally {
    providerAutoSaveInFlight = false;
    if (providerAutoSaveQueued) {
      providerAutoSaveQueued = false;
      scheduleProviderAutoSave();
    }
  }
}

function resolveProviderType(provider: ProviderInfo): string {
  if (provider.id === "openai") {
    return "openai";
  }
  if (isCodexCompatibleProviderID(provider.id)) {
    return "codex-compatible";
  }
  if (provider.openai_compatible) {
    return "openai-compatible";
  }
  return provider.id;
}

function fallbackProviderTypes(providers: ProviderInfo[]): ProviderTypeInfo[] {
  const seen = new Set<string>();
  const out: ProviderTypeInfo[] = [];

  const pushType = (id: string, displayName: string): void => {
    const normalized = normalizeProviderTypeValue(id);
    if (normalized === "" || seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    out.push({
      id: normalized,
      display_name: displayName.trim() || providerTypeDisplayName(normalized),
    });
  };

  pushType("openai", t("models.providerTypeOpenAI"));
  pushType("openai-compatible", t("models.providerTypeOpenAICompatible"));
  pushType("codex-compatible", t("models.providerTypeCodexCompatible"));

  for (const provider of providers) {
    if (provider.id === "openai") {
      pushType("openai", t("models.providerTypeOpenAI"));
      continue;
    }
    if (isCodexCompatibleProviderID(provider.id)) {
      pushType("codex-compatible", t("models.providerTypeCodexCompatible"));
      continue;
    }
    if (provider.openai_compatible) {
      pushType("openai-compatible", t("models.providerTypeOpenAICompatible"));
      continue;
    }
    pushType(provider.id, provider.display_name || provider.name || provider.id);
  }

  return out;
}

function normalizeProviderTypes(providerTypesRaw?: ProviderTypeInfo[]): ProviderTypeInfo[] {
  if (!Array.isArray(providerTypesRaw)) {
    return fallbackProviderTypes([]);
  }
  const seen = new Set<string>();
  const out: ProviderTypeInfo[] = [];
  for (const item of providerTypesRaw) {
    const id = normalizeProviderTypeValue(item?.id ?? "");
    if (id === "" || seen.has(id)) {
      continue;
    }
    seen.add(id);
    const displayName = (item?.display_name ?? "").trim() || providerTypeDisplayName(id);
    out.push({
      id,
      display_name: displayName,
    });
  }
  if (out.length === 0) {
    return fallbackProviderTypes([]);
  }
  return out;
}

function providerTypeDisplayName(providerTypeID: string): string {
  if (providerTypeID === "openai") {
    return t("models.providerTypeOpenAI");
  }
  if (providerTypeID === "openai-compatible") {
    return t("models.providerTypeOpenAICompatible");
  }
  if (providerTypeID === "codex-compatible") {
    return t("models.providerTypeCodexCompatible");
  }
  return providerTypeID;
}

function isCodexCompatibleProviderID(providerID: string): boolean {
  const normalized = normalizeProviderIDValue(providerID);
  return normalized === "codex-compatible" || normalized.startsWith("codex-compatible-");
}

function resolveProviderTypeForPreview(): string {
  if (state.providerModal.mode === "edit" && state.providerModal.editingProviderID !== "") {
    const editing = state.providers.find((provider: ProviderInfo) => provider.id === state.providerModal.editingProviderID);
    if (editing) {
      return resolveProviderType(editing);
    }
  }
  return normalizeProviderTypeValue(modelsProviderTypeSelect.value);
}

function providerRequestEndpointPath(providerTypeID: string): string {
  const normalized = normalizeProviderTypeValue(providerTypeID);
  if (normalized === "codex-compatible") {
    return "/responses";
  }
  return "/chat/completions";
}

function resetProviderModalForm(): void {
  renderProviderTypeOptions("openai");
  modelsProviderTypeSelect.disabled = false;
  modelsProviderNameInput.value = "";
  modelsProviderAPIKeyInput.value = "";
  modelsProviderBaseURLInput.value = "";
  modelsProviderTimeoutMSInput.value = "";
  modelsProviderReasoningEffortSelect.value = "";
  modelsProviderEnabledInput.checked = true;
  modelsProviderStoreInput.checked = false;
  resetProviderKVEditor(modelsProviderHeadersRows, "headers");
  resetProviderKVEditor(modelsProviderAliasesRows, "aliases");
  resetProviderCustomModelsEditor();
  syncProviderCustomModelsField("openai");
  setProviderAPIKeyVisibility(true);
  renderProviderBaseURLPreview();
}

function openProviderModal(mode: "create" | "edit", providerID = ""): void {
  resetProviderAutoSaveScheduling();
  state.providerModal.mode = mode;
  state.providerModal.open = true;
  state.providerModal.editingProviderID = providerID;

  if (mode === "create") {
    resetProviderModalForm();
    modelsProviderModalTitle.textContent = t("models.addProviderTitle");
  } else {
    state.selectedProviderID = providerID;
    modelsProviderModalTitle.textContent = t("models.editProviderTitle");
    populateProviderForm(providerID);
  }
  setModelsSettingsLevel("edit");
  renderModelsSettingsLevel();
  if (mode === "create") {
    modelsProviderTypeSelect.focus();
  } else {
    modelsProviderNameInput.focus();
  }
}

function closeProviderModal(): void {
  resetProviderAutoSaveScheduling();
  state.providerModal.open = false;
  state.providerModal.editingProviderID = "";
  setProviderAPIKeyVisibility(true);
  setModelsSettingsLevel("list");
  renderModelsSettingsLevel();
}

function populateProviderForm(providerID: string): void {
  const provider = state.providers.find((item: ProviderInfo) => item.id === providerID);
  if (!provider) {
    setStatus(t("status.providerNotFound", { providerId: providerID }), "error");
    return;
  }
  renderProviderTypeOptions(resolveProviderType(provider));
  modelsProviderTypeSelect.disabled = true;
  modelsProviderNameInput.value = provider.display_name ?? provider.name ?? provider.id;
  modelsProviderAPIKeyInput.value = "";
  modelsProviderBaseURLInput.value = provider.current_base_url ?? "";
  modelsProviderEnabledInput.checked = provider.enabled !== false;
  modelsProviderStoreInput.checked = provider.store === true;
  modelsProviderTimeoutMSInput.value = typeof provider.timeout_ms === "number" ? String(provider.timeout_ms) : "";
  modelsProviderReasoningEffortSelect.value = provider.reasoning_effort ?? "";
  populateProviderHeaderRows(provider);
  populateProviderAliasRows(provider);
  populateProviderCustomModelsRows(provider);
  syncProviderCustomModelsField(resolveProviderType(provider));
  setProviderAPIKeyVisibility(true);
  renderProviderBaseURLPreview();
  setStatus(t("status.providerLoadedForEdit", { providerId: provider.id }), "info");
}

async function upsertProvider(options: UpsertProviderOptions = {}): Promise<boolean> {
  const closeAfterSave = options.closeAfterSave ?? true;
  const notifyStatus = options.notifyStatus ?? true;
  if (closeAfterSave) {
    resetProviderAutoSaveScheduling();
  }
  syncControlState();
  const selectedProviderType = normalizeProviderTypeValue(modelsProviderTypeSelect.value);
  const providerID = resolveProviderIDForUpsert(selectedProviderType);
  if (providerID === "") {
    if (notifyStatus) {
      setStatus(t("error.providerTypeRequired"), "error");
    }
    return false;
  }

  let timeoutMS = 0;
  const timeoutRaw = modelsProviderTimeoutMSInput.value.trim();
  if (timeoutRaw !== "") {
    const parsed = Number.parseInt(timeoutRaw, 10);
    if (Number.isNaN(parsed) || parsed < 0) {
      if (notifyStatus) {
        setStatus(t("error.providerTimeoutInvalid"), "error");
      }
      return false;
    }
    timeoutMS = parsed;
  }

  let headers: Record<string, string> | undefined;
  try {
    headers = collectProviderKVMap(modelsProviderHeadersRows, {
      invalidKey: t("error.invalidProviderHeadersKey"),
      invalidValue: (key) => t("error.invalidProviderHeadersValue", { key }),
    });
  } catch (error) {
    if (notifyStatus) {
      setStatus(asErrorMessage(error), "error");
    }
    return false;
  }

  let aliases: Record<string, string> | undefined;
  try {
    aliases = collectProviderKVMap(modelsProviderAliasesRows, {
      invalidKey: t("error.invalidProviderAliasesKey"),
      invalidValue: (key) => t("error.invalidProviderAliasesValue", { key }),
    });
  } catch (error) {
    if (notifyStatus) {
      setStatus(asErrorMessage(error), "error");
    }
    return false;
  }

  const customModelsEnabled =
    state.providerModal.mode === "create"
      ? providerSupportsCustomModels(selectedProviderType)
      : providerSupportsCustomModels(providerID);
  let customModels: string[] | undefined;
  if (customModelsEnabled) {
    try {
      customModels = collectCustomModelIDs(modelsProviderCustomModelsRows);
    } catch (error) {
      if (notifyStatus) {
        setStatus(asErrorMessage(error), "error");
      }
      return false;
    }
  }

  const payload: Record<string, unknown> = {
    enabled: modelsProviderEnabledInput.checked,
    display_name: modelsProviderNameInput.value.trim(),
  };
  const storeEnabled =
    state.providerModal.mode === "create"
      ? providerSupportsStore(selectedProviderType)
      : providerSupportsStore(providerID);
  if (storeEnabled) {
    payload.store = modelsProviderStoreInput.checked;
  }
  const reasoningEffortEnabled =
    state.providerModal.mode === "create"
      ? providerSupportsReasoningEffort(selectedProviderType)
      : providerSupportsReasoningEffort(providerID);
  if (reasoningEffortEnabled) {
    payload.reasoning_effort = modelsProviderReasoningEffortSelect.value.trim().toLowerCase();
  }
  const apiKey = modelsProviderAPIKeyInput.value.trim();
  if (apiKey !== "") {
    payload.api_key = apiKey;
  }
  const baseURL = modelsProviderBaseURLInput.value.trim();
  if (baseURL !== "") {
    payload.base_url = baseURL;
  }
  payload.timeout_ms = timeoutMS;
  payload.headers = headers ?? {};
  const mergedAliases: Record<string, string> = {};
  if (aliases) {
    Object.assign(mergedAliases, aliases);
  }
  if (customModels) {
    for (const modelID of customModels) {
      if (mergedAliases[modelID] === undefined) {
        mergedAliases[modelID] = modelID;
      }
    }
  }
  if (
    state.providerModal.mode === "create" &&
    selectedProviderType === "openai" &&
    providerID !== "openai" &&
    Object.keys(mergedAliases).length === 0
  ) {
    Object.assign(mergedAliases, resolveOpenAIDuplicateModelAliases());
  }
  payload.model_aliases = mergedAliases;

  try {
    const out = await requestJSON(`/models/${encodeURIComponent(providerID)}/config`, {
      method: "PUT",
      body: payload,
    });
    state.selectedProviderID = out.id ?? providerID;
    if (closeAfterSave) {
      setModelsSettingsLevel("list");
    }
    await refreshModels({ silent: !notifyStatus });
    if (closeAfterSave) {
      closeProviderModal();
      modelsProviderAPIKeyInput.value = "";
    }
    if (notifyStatus) {
      setStatus(
        t(state.providerModal.mode === "create" ? "status.providerCreated" : "status.providerUpdated", {
          providerId: out.id,
        }),
        "info",
      );
    }
    return true;
  } catch (error) {
    if (notifyStatus) {
      setStatus(asErrorMessage(error), "error");
    }
    return false;
  }
}

async function deleteProvider(providerID: string): Promise<void> {
  if (!window.confirm(t("models.deleteProviderConfirm", { providerId: providerID }))) {
    return;
  }
  syncControlState();
  try {
    const out = await requestJSON(`/models/${encodeURIComponent(providerID)}`, {
      method: "DELETE",
    });
    await refreshModels();
    if (state.providerModal.open && state.providerModal.editingProviderID === providerID) {
      closeProviderModal();
    }
    setStatus(t(out.deleted ? "status.providerDeleted" : "status.providerDeleteSkipped", { providerId: providerID }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function resetProviderKVEditor(container: HTMLElement, kind: ProviderKVKind): void {
  container.innerHTML = "";
  appendProviderKVRow(container, kind);
}

function resetProviderCustomModelsEditor(): void {
  modelsProviderCustomModelsRows.innerHTML = "";
  appendCustomModelRow(modelsProviderCustomModelsRows);
}

function collectProviderModelAliases(provider: ProviderInfo): Map<string, string> {
  const aliases = new Map<string, string>();
  for (const [alias, target] of Object.entries(provider.model_aliases ?? {})) {
    const aliasID = alias.trim();
    const targetID = target.trim();
    if (aliasID === "" || targetID === "") {
      continue;
    }
    aliases.set(aliasID, targetID);
  }
  for (const model of provider.models) {
    const alias = model.id.trim();
    if (alias === "" || aliases.has(alias)) {
      continue;
    }
    const target = (model.alias_of ?? "").trim();
    if (target !== "") {
      aliases.set(alias, target);
      continue;
    }
    if (!BUILTIN_PROVIDER_IDS.has(provider.id)) {
      aliases.set(alias, alias);
    }
  }
  return aliases;
}

function populateProviderHeaderRows(provider: ProviderInfo): void {
  const headerEntries = Object.entries(provider.headers ?? {})
    .map(([key, value]) => [key.trim(), value.trim()] as const)
    .filter(([key, value]) => key !== "" && value !== "")
    .sort(([left], [right]) => left.localeCompare(right));

  modelsProviderHeadersRows.innerHTML = "";
  if (headerEntries.length === 0) {
    appendProviderKVRow(modelsProviderHeadersRows, "headers");
    return;
  }
  for (const [key, value] of headerEntries) {
    appendProviderKVRow(modelsProviderHeadersRows, "headers", key, value);
  }
}

function populateProviderAliasRows(provider: ProviderInfo): void {
  const aliases = collectProviderModelAliases(provider);
  const aliasEntries = Array.from(aliases.entries())
    .filter(([alias, target]) => BUILTIN_PROVIDER_IDS.has(provider.id) || alias !== target)
    .sort(([left], [right]) => left.localeCompare(right));

  modelsProviderAliasesRows.innerHTML = "";
  if (aliasEntries.length === 0) {
    appendProviderKVRow(modelsProviderAliasesRows, "aliases");
    return;
  }
  for (const [alias, target] of aliasEntries) {
    appendProviderKVRow(modelsProviderAliasesRows, "aliases", alias, target);
  }
}

function populateProviderCustomModelsRows(provider: ProviderInfo): void {
  resetProviderCustomModelsEditor();

  const customModelIDs = Array.from(collectProviderModelAliases(provider).entries())
    .filter(([alias, target]) => alias === target)
    .map(([alias]) => alias)
    .sort((left, right) => left.localeCompare(right));
  if (customModelIDs.length === 0) {
    return;
  }

  modelsProviderCustomModelsRows.innerHTML = "";
  for (const modelID of customModelIDs) {
    appendCustomModelRow(modelsProviderCustomModelsRows, modelID);
  }
}

function appendProviderKVRow(container: HTMLElement, kind: ProviderKVKind, key = "", value = ""): void {
  const row = document.createElement("div");
  row.className = "kv-row";

  const keyInput = document.createElement("input");
  keyInput.type = "text";
  keyInput.className = "kv-key-input";
  keyInput.value = key;
  keyInput.setAttribute("data-kv-field", "key");
  keyInput.setAttribute("data-i18n-placeholder", "models.kvKeyPlaceholder");
  keyInput.placeholder = t("models.kvKeyPlaceholder");

  const valueInput = document.createElement("input");
  valueInput.type = "text";
  valueInput.className = "kv-value-input";
  valueInput.value = value;
  valueInput.setAttribute("data-kv-field", "value");
  valueInput.setAttribute("data-i18n-placeholder", kind === "headers" ? "models.kvHeaderValuePlaceholder" : "models.kvAliasValuePlaceholder");
  valueInput.placeholder = kind === "headers" ? t("models.kvHeaderValuePlaceholder") : t("models.kvAliasValuePlaceholder");

  const removeButton = document.createElement("button");
  removeButton.type = "button";
  removeButton.className = "secondary-btn";
  removeButton.setAttribute("data-i18n", "models.removeKVRow");
  removeButton.textContent = t("models.removeKVRow");
  removeButton.dataset.kvRemove = "true";

  row.append(keyInput, valueInput, removeButton);
  container.appendChild(row);
}

function appendCustomModelRow(container: HTMLElement, modelID = ""): void {
  const row = document.createElement("div");
  row.className = "custom-model-row";

  const modelInput = document.createElement("input");
  modelInput.type = "text";
  modelInput.className = "custom-model-input";
  modelInput.value = modelID;
  modelInput.setAttribute("data-custom-model-input", "true");
  modelInput.setAttribute("data-i18n-placeholder", "models.customModelPlaceholder");
  modelInput.placeholder = t("models.customModelPlaceholder");

  const removeButton = document.createElement("button");
  removeButton.type = "button";
  removeButton.className = "secondary-btn";
  removeButton.setAttribute("data-i18n", "models.removeKVRow");
  removeButton.textContent = t("models.removeKVRow");
  removeButton.dataset.customModelRemove = "true";

  row.append(modelInput, removeButton);
  container.appendChild(row);
}

function collectCustomModelIDs(container: HTMLElement): string[] | undefined {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const input of Array.from(container.querySelectorAll<HTMLInputElement>("input[data-custom-model-input=\"true\"]"))) {
    const modelID = input.value.trim();
    if (modelID === "" || seen.has(modelID)) {
      continue;
    }
    seen.add(modelID);
    out.push(modelID);
  }
  if (out.length === 0) {
    return undefined;
  }
  return out;
}

function collectProviderKVMap(
  container: HTMLElement,
  messages: {
    invalidKey: string;
    invalidValue: (key: string) => string;
  },
): Record<string, string> | undefined {
  const out: Record<string, string> = {};

  for (const row of Array.from(container.querySelectorAll<HTMLElement>(".kv-row"))) {
    const keyInput = row.querySelector<HTMLInputElement>("input[data-kv-field=\"key\"]");
    const valueInput = row.querySelector<HTMLInputElement>("input[data-kv-field=\"value\"]");
    if (!keyInput || !valueInput) {
      continue;
    }
    const key = keyInput.value.trim();
    const value = valueInput.value.trim();
    if (key === "" && value === "") {
      continue;
    }
    if (key === "") {
      throw new Error(messages.invalidKey);
    }
    if (value === "") {
      throw new Error(messages.invalidValue(key));
    }
    out[key] = value;
  }

  if (Object.keys(out).length === 0) {
    return undefined;
  }
  return out;
}

function defaultQQChannelConfig(): QQChannelConfig {
  return {
    enabled: false,
    app_id: "",
    client_secret: "",
    bot_prefix: "",
    target_type: "c2c",
    target_id: "",
    api_base: DEFAULT_QQ_API_BASE,
    token_url: DEFAULT_QQ_TOKEN_URL,
    timeout_seconds: DEFAULT_QQ_TIMEOUT_SECONDS,
  };
}

function normalizeQQTargetType(raw: unknown): QQTargetType {
  const value = typeof raw === "string" ? raw.trim().toLowerCase() : "";
  if (value === "group") {
    return "group";
  }
  if (value === "guild" || value === "channel" || value === "dm") {
    return "guild";
  }
  return "c2c";
}

function normalizeQQChannelConfig(raw: unknown): QQChannelConfig {
  const fallback = defaultQQChannelConfig();
  const parsed = toRecord(raw);
  if (!parsed) {
    return fallback;
  }
  return {
    enabled: parsed.enabled === true || parsed.enabled === "true" || parsed.enabled === 1,
    app_id: typeof parsed.app_id === "string" ? parsed.app_id.trim() : "",
    client_secret: typeof parsed.client_secret === "string" ? parsed.client_secret.trim() : "",
    bot_prefix: typeof parsed.bot_prefix === "string" ? parsed.bot_prefix : "",
    target_type: normalizeQQTargetType(parsed.target_type),
    target_id: typeof parsed.target_id === "string" ? parsed.target_id.trim() : "",
    api_base: typeof parsed.api_base === "string" && parsed.api_base.trim() !== "" ? parsed.api_base.trim() : fallback.api_base,
    token_url: typeof parsed.token_url === "string" && parsed.token_url.trim() !== "" ? parsed.token_url.trim() : fallback.token_url,
    timeout_seconds: parseIntegerInput(String(parsed.timeout_seconds ?? ""), fallback.timeout_seconds, 1),
  };
}

function normalizeURLForCompare(raw: string): string {
  return raw.trim().toLowerCase().replace(/\/+$/, "");
}

function resolveQQAPIEnvironment(apiBase: string): QQAPIEnvironment {
  const normalized = normalizeURLForCompare(apiBase);
  if (normalized === normalizeURLForCompare(QQ_SANDBOX_API_BASE)) {
    return "sandbox";
  }
  return "production";
}

function resolveQQAPIBase(environment: QQAPIEnvironment): string {
  if (environment === "sandbox") {
    return QQ_SANDBOX_API_BASE;
  }
  return DEFAULT_QQ_API_BASE;
}

function renderQQChannelConfig(): void {
  const cfg = state.qqChannelConfig;
  const available = state.qqChannelAvailable;

  qqChannelEnabledInput.checked = cfg.enabled;
  qqChannelAppIDInput.value = cfg.app_id;
  qqChannelClientSecretInput.value = cfg.client_secret;
  qqChannelBotPrefixInput.value = cfg.bot_prefix;
  qqChannelTargetTypeSelect.value = cfg.target_type;
  qqChannelAPIEnvironmentSelect.value = resolveQQAPIEnvironment(cfg.api_base);
  qqChannelTimeoutSecondsInput.value = String(cfg.timeout_seconds);

  const controls: Array<HTMLInputElement | HTMLSelectElement | HTMLButtonElement> = [
    qqChannelEnabledInput,
    qqChannelAppIDInput,
    qqChannelClientSecretInput,
    qqChannelBotPrefixInput,
    qqChannelTargetTypeSelect,
    qqChannelAPIEnvironmentSelect,
    qqChannelTimeoutSecondsInput,
  ];
  for (const control of controls) {
    control.disabled = !available;
  }
  syncCustomSelect(qqChannelTargetTypeSelect);
  syncCustomSelect(qqChannelAPIEnvironmentSelect);
}

async function refreshQQChannelConfig(options: { silent?: boolean } = {}): Promise<void> {
  try {
    const raw = await requestJSON("/config/channels/qq");
    state.qqChannelConfig = normalizeQQChannelConfig(raw);
    state.qqChannelAvailable = true;
    state.tabLoaded.channels = true;
    renderChannelsPanel();
    if (!options.silent) {
      setStatus(t("status.qqChannelLoaded"), "info");
    }
  } catch (error) {
    state.qqChannelConfig = defaultQQChannelConfig();
    state.qqChannelAvailable = false;
    state.tabLoaded.channels = false;
    renderChannelsPanel();
    if (!options.silent) {
      setStatus(asErrorMessage(error), "error");
    }
  }
}

function collectQQChannelFormConfig(): QQChannelConfig {
  const apiEnvironment: QQAPIEnvironment = qqChannelAPIEnvironmentSelect.value === "sandbox" ? "sandbox" : "production";
  return {
    enabled: qqChannelEnabledInput.checked,
    app_id: qqChannelAppIDInput.value.trim(),
    client_secret: qqChannelClientSecretInput.value.trim(),
    bot_prefix: qqChannelBotPrefixInput.value,
    target_type: normalizeQQTargetType(qqChannelTargetTypeSelect.value),
    target_id: state.qqChannelConfig.target_id,
    api_base: resolveQQAPIBase(apiEnvironment),
    token_url: state.qqChannelConfig.token_url || DEFAULT_QQ_TOKEN_URL,
    timeout_seconds: parseIntegerInput(qqChannelTimeoutSecondsInput.value, DEFAULT_QQ_TIMEOUT_SECONDS, 1),
  };
}

async function saveQQChannelConfig(): Promise<void> {
  syncControlState();
  if (!state.qqChannelAvailable) {
    setStatus(t("error.qqChannelUnavailable"), "error");
    return;
  }
  const payload = collectQQChannelFormConfig();
  try {
    const out = await requestJSON("/config/channels/qq", {
      method: "PUT",
      body: payload,
    });
    state.qqChannelConfig = normalizeQQChannelConfig(out ?? payload);
    state.qqChannelAvailable = true;
    setChannelsSettingsLevel("list");
    renderChannelsPanel();
    setStatus(t("status.qqChannelSaved"), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}


  return {
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
  };
}

type CronModalMode = "create" | "edit";

interface CronScheduleSpec {
  type: string;
  cron: string;
  timezone?: string;
}

interface CronDispatchTarget {
  user_id: string;
  session_id: string;
}

interface CronDispatchSpec {
  type?: string;
  channel?: string;
  target: CronDispatchTarget;
  mode?: string;
  meta?: Record<string, unknown>;
}

interface CronRuntimeSpec {
  max_concurrency?: number;
  timeout_seconds?: number;
  misfire_grace_seconds?: number;
}

interface CronWorkflowViewport {
  pan_x?: number;
  pan_y?: number;
  zoom?: number;
}

interface CronWorkflowNode {
  id: string;
  type: "start" | "text_event" | "delay" | "if_event";
  title?: string;
  x: number;
  y: number;
  text?: string;
  delay_seconds?: number;
  if_condition?: string;
  continue_on_error?: boolean;
}

interface CronWorkflowEdge {
  id: string;
  source: string;
  target: string;
}

interface CronWorkflowSpec {
  version: "v1";
  viewport?: CronWorkflowViewport;
  nodes: CronWorkflowNode[];
  edges: CronWorkflowEdge[];
}

interface CronWorkflowNodeExecution {
  node_id: string;
  node_type: "text_event" | "delay" | "if_event";
  status: "succeeded" | "failed" | "skipped";
  continue_on_error: boolean;
  started_at: string;
  finished_at?: string;
  error?: string;
}

interface CronWorkflowExecution {
  run_id: string;
  started_at: string;
  finished_at?: string;
  had_failures: boolean;
  nodes: CronWorkflowNodeExecution[];
}

interface CronJobSpec {
  id: string;
  name: string;
  enabled: boolean;
  schedule: CronScheduleSpec;
  task_type: "text" | "workflow";
  text?: string;
  workflow?: CronWorkflowSpec;
  dispatch: CronDispatchSpec;
  runtime: CronRuntimeSpec;
  meta?: Record<string, unknown>;
}

interface CronJobState {
  next_run_at?: string;
  last_run_at?: string;
  last_status?: string;
  last_error?: string;
  last_result?: string;
  last_workflow?: CronWorkflowExecution;
  last_execution?: CronWorkflowExecution;
}

interface DeleteResult {
  deleted: boolean;
}

export function createCronDomain(ctx: any) {
  const {
    state,
    t,
    setStatus,
    asErrorMessage,
    syncControlState,
    requestJSON,
    reloadChats,
    compactTime,
    syncCustomSelect,
    parseIntegerInput,
    newSessionID,
    createDefaultCronWorkflow,
    validateCronWorkflowSpec,
    CronWorkflowCanvas,
    DEFAULT_CRON_JOB_ID,
    CRON_META_SYSTEM_DEFAULT,
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
  } = ctx;
  let cronWorkflowEditor: any = null;
  let cronWorkflowPseudoFullscreen = false;

function bindCronEvents(): void {
  cronCreateOpenButton.addEventListener("click", () => {
    setCronModalMode("create");
    syncCronDispatchHint();
    cronIDInput.value = "";
    cronNameInput.value = "";
    cronIntervalInput.value = "60s";
    ensureCronSessionID();
    cronMaxConcurrencyInput.value = "1";
    cronTimeoutInput.value = "30";
    cronMisfireInput.value = "0";
    cronTextInput.value = "";
    state.cronDraftTaskType = "workflow";
    cronTaskTypeSelect.value = "workflow";
    syncCronTaskModeUI();
    cronWorkflowEditor?.setWorkflow(createDefaultCronWorkflow());
    renderCronExecutionDetails(undefined);
    setCronCreateModalOpen(true);
  });
  cronCreateModalCloseButton.addEventListener("click", () => {
    setCronCreateModalOpen(false);
  });
  cronTaskTypeSelect.addEventListener("change", () => {
    const value = cronTaskTypeSelect.value === "text" ? "text" : "workflow";
    state.cronDraftTaskType = value;
    syncCronTaskModeUI();
  });
  cronResetWorkflowButton.addEventListener("click", () => {
    cronWorkflowEditor?.resetToDefault();
  });
  cronWorkflowFullscreenButton.addEventListener("click", () => {
    void toggleCronWorkflowFullscreen();
  });
  document.addEventListener("fullscreenchange", () => {
    if (!isCronWorkflowNativeFullscreen() && cronWorkflowPseudoFullscreen) {
      setCronWorkflowPseudoFullscreen(false);
      return;
    }
    syncCronWorkflowFullscreenUI();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || !isCronCreateModalOpen()) {
      return;
    }
    if (isCronWorkflowFullscreenActive()) {
      event.preventDefault();
      void exitCronWorkflowFullscreen();
      return;
    }
    setCronCreateModalOpen(false);
  });

  refreshCronButton.addEventListener("click", async () => {
    await refreshCronJobs();
  });
  cronNewSessionButton.addEventListener("click", () => {
    cronSessionIDInput.value = newSessionID();
  });
  cronCreateForm.addEventListener("submit", async (event: Event) => {
    event.preventDefault();
    await saveCronJob();
  });
  cronJobsBody.addEventListener("change", async (event: Event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement)) {
      return;
    }
    const jobID = target.dataset.cronToggleEnabled ?? "";
    if (jobID === "") {
      return;
    }
    const enabled = target.checked;
    target.disabled = true;
    const saved = await updateCronJobEnabled(jobID, enabled);
    if (!saved) {
      target.checked = !enabled;
    }
    target.disabled = false;
  });
  cronJobsBody.addEventListener("click", async (event: Event) => {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-cron-run], button[data-cron-edit], button[data-cron-delete]");
    if (!button) {
      return;
    }
    const runJobID = button.dataset.cronRun ?? "";
    if (runJobID !== "") {
      await runCronJob(runJobID);
      return;
    }

    const editJobID = button.dataset.cronEdit ?? "";
    if (editJobID !== "") {
      openCronEditModal(editJobID);
      return;
    }

    const deleteJobID = button.dataset.cronDelete ?? "";
    if (deleteJobID === "") {
      return;
    }
    await deleteCronJob(deleteJobID);
  });
}

function isCronCreateModalOpen(): boolean {
  return !cronCreateModal.classList.contains("is-hidden");
}

function setCronCreateModalOpen(open: boolean): void {
  if (!open && isCronWorkflowFullscreenActive()) {
    void exitCronWorkflowFullscreen();
  }
  cronCreateModal.classList.toggle("is-hidden", !open);
  cronCreateModal.setAttribute("aria-hidden", String(!open));
  cronCreateOpenButton.setAttribute("aria-expanded", String(open));
  cronCreateOpenButton.hidden = open;
  cronWorkbench.dataset.cronView = open ? "editor" : "jobs";
}

function supportsNativeCronWorkflowFullscreen(): boolean {
  const section = cronWorkflowSection as HTMLElement & {
    requestFullscreen?: () => Promise<void>;
  };
  return typeof section.requestFullscreen === "function" && typeof document.exitFullscreen === "function" && document.fullscreenEnabled === true;
}

function isCronWorkflowNativeFullscreen(): boolean {
  return document.fullscreenElement === cronWorkflowSection;
}

function isCronWorkflowFullscreenActive(): boolean {
  return isCronWorkflowNativeFullscreen() || cronWorkflowPseudoFullscreen;
}

function syncCronWorkflowFullscreenUI(): void {
  const active = isCronWorkflowFullscreenActive();
  const label = t(active ? "cron.exitFullscreen" : "cron.enterFullscreen");
  cronWorkflowFullscreenButton.textContent = label;
  cronWorkflowFullscreenButton.setAttribute("aria-label", label);
  cronWorkflowFullscreenButton.title = label;
  cronWorkflowFullscreenButton.setAttribute("aria-pressed", String(active));
}

function setCronWorkflowPseudoFullscreen(active: boolean): void {
  cronWorkflowPseudoFullscreen = active;
  cronWorkflowSection.classList.toggle("is-pseudo-fullscreen", active);
  document.body.classList.toggle("cron-workflow-pseudo-fullscreen", active);
  syncCronWorkflowFullscreenUI();
}

async function enterCronWorkflowFullscreen(): Promise<void> {
  if (supportsNativeCronWorkflowFullscreen()) {
    const section = cronWorkflowSection as HTMLElement & {
      requestFullscreen: () => Promise<void>;
    };
    await section.requestFullscreen();
    return;
  }
  setCronWorkflowPseudoFullscreen(true);
}

async function exitCronWorkflowFullscreen(): Promise<void> {
  if (isCronWorkflowNativeFullscreen() && typeof document.exitFullscreen === "function") {
    await document.exitFullscreen();
    return;
  }
  if (cronWorkflowPseudoFullscreen) {
    setCronWorkflowPseudoFullscreen(false);
  }
}

async function toggleCronWorkflowFullscreen(): Promise<void> {
  if (isCronWorkflowFullscreenActive()) {
    await exitCronWorkflowFullscreen();
  } else {
    await enterCronWorkflowFullscreen();
  }
  syncCronWorkflowFullscreenUI();
}

function setCronModalMode(mode: CronModalMode, editingJobID = ""): void {
  state.cronModal.mode = mode;
  state.cronModal.editingJobID = editingJobID;

  const createMode = mode === "create";
  cronIDInput.readOnly = !createMode;
  refreshCronModalTitles();
  syncCronTaskModeUI();
}

function initCronWorkflowEditor(): void {
  cronWorkflowEditor = new CronWorkflowCanvas({
    viewport: cronWorkflowViewport,
    canvas: cronWorkflowCanvas,
    edgesLayer: cronWorkflowEdges,
    nodesLayer: cronWorkflowNodes,
    nodeEditor: cronWorkflowNodeEditor,
    zoomLabel: cronWorkflowZoom,
    onStatus: (message: string, tone: "neutral" | "info" | "error") => {
      setStatus(message, tone);
    },
  });
}

function syncCronTaskModeUI(): void {
  const mode: "text" | "workflow" = cronTaskTypeSelect.value === "text" ? "text" : "workflow";
  state.cronDraftTaskType = mode;
  if (mode !== "workflow" && isCronWorkflowFullscreenActive()) {
    void exitCronWorkflowFullscreen();
  }
  cronTextSection.classList.toggle("is-hidden", mode !== "text");
  cronWorkflowSection.classList.toggle("is-hidden", mode !== "workflow");
  cronTextSection.setAttribute("aria-hidden", String(mode !== "text"));
  cronWorkflowSection.setAttribute("aria-hidden", String(mode !== "workflow"));
  refreshCronModalTitles();
  syncCustomSelect(cronTaskTypeSelect);
}

function refreshCronModalTitles(): void {
  const createMode = state.cronModal.mode === "create";
  const workflowMode = state.cronDraftTaskType === "workflow";
  const titleKey = createMode
    ? (workflowMode ? "cron.createWorkflowJob" : "cron.createTextJob")
    : (workflowMode ? "cron.updateWorkflowJob" : "cron.updateTextJob");
  const submitKey = createMode ? "cron.submitCreate" : "cron.submitUpdate";
  cronCreateModalTitle.textContent = t(titleKey);
  cronSubmitButton.textContent = t(submitKey);
}

function renderCronExecutionDetails(stateValue: CronJobState | undefined): void {
  cronWorkflowExecutionList.innerHTML = "";
  const execution = stateValue?.last_execution;
  if (!execution || !Array.isArray(execution.nodes) || execution.nodes.length === 0) {
    const empty = document.createElement("li");
    empty.className = "hint";
    empty.textContent = t("cron.executionEmpty");
    cronWorkflowExecutionList.appendChild(empty);
    return;
  }

  execution.nodes.forEach((item: CronWorkflowNodeExecution) => {
    const row = document.createElement("li");
    row.className = "cron-execution-item";
    row.dataset.status = item.status;
    const summary = document.createElement("div");
    summary.className = "cron-execution-summary";
    summary.textContent = t("cron.executionSummary", {
      nodeId: item.node_id,
      nodeType: formatCronWorkflowNodeType(item.node_type),
      status: formatCronWorkflowNodeStatus(item.status),
    });
    row.appendChild(summary);
    if (item.error && item.error.trim() !== "") {
      const err = document.createElement("div");
      err.className = "cron-execution-error";
      err.textContent = item.error;
      row.appendChild(err);
    }
    cronWorkflowExecutionList.appendChild(row);
  });
}

function syncCronDispatchHint(): void {
  cronDispatchHint.textContent = t("workspace.dispatchHint", {
    userId: state.userId,
    channel: state.channel,
  });
}

function ensureCronSessionID(): void {
  if (cronSessionIDInput.value.trim() === "") {
    cronSessionIDInput.value = newSessionID();
  }
}

function openCronEditModal(jobID: string): void {
  const job = state.cronJobs.find((item: CronJobSpec) => item.id === jobID);
  if (!job) {
    setStatus(t("error.cronJobNotFound", { jobId: jobID }), "error");
    return;
  }

  state.cronDraftTaskType = job.task_type === "text" ? "text" : "workflow";
  cronTaskTypeSelect.value = state.cronDraftTaskType;
  setCronModalMode("edit", jobID);

  cronIDInput.value = job.id;
  cronNameInput.value = job.name;
  cronIntervalInput.value = job.schedule.cron ?? "";
  cronSessionIDInput.value = job.dispatch.target.session_id ?? "";
  cronMaxConcurrencyInput.value = String(job.runtime.max_concurrency ?? 1);
  cronTimeoutInput.value = String(job.runtime.timeout_seconds ?? 30);
  cronMisfireInput.value = String(job.runtime.misfire_grace_seconds ?? 0);
  if (state.cronDraftTaskType === "text") {
    cronTextInput.value = job.text ?? "";
  } else {
    const loadedWorkflow = job.workflow ?? createDefaultCronWorkflow();
    const issue = validateCronWorkflowSpec(loadedWorkflow);
    if (issue) {
      setStatus(t("error.cronWorkflowInvalid", { reason: issue }), "error");
      cronWorkflowEditor?.setWorkflow(createDefaultCronWorkflow());
    } else {
      cronWorkflowEditor?.setWorkflow(loadedWorkflow);
    }
  }

  renderCronExecutionDetails(state.cronStates[jobID]);
  syncCronDispatchHint();
  setCronCreateModalOpen(true);
}

async function refreshCronJobs(): Promise<void> {
  syncControlState();
  syncCronDispatchHint();
  ensureCronSessionID();

  try {
    const jobs = await requestJSON("/cron/jobs");
    state.cronJobs = jobs;

    const statePairs = await Promise.all(
      jobs.map(async (job: CronJobSpec) => {
        try {
          const jobState = await requestJSON(`/cron/jobs/${encodeURIComponent(job.id)}/state`);
          return [job.id, jobState] as const;
        } catch {
          return [job.id, null] as const;
        }
      }),
    );

    const stateMap: Record<string, CronJobState> = {};
    for (const [jobID, jobState] of statePairs) {
      if (jobState) {
        stateMap[jobID] = jobState;
      }
    }

    state.cronStates = stateMap;
    state.tabLoaded.cron = true;
    renderCronJobs();
    if (state.cronModal.mode === "edit") {
      renderCronExecutionDetails(state.cronStates[state.cronModal.editingJobID]);
    }
    setStatus(t("status.cronJobsLoaded", { count: jobs.length }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

function renderCronJobs(): void {
  cronJobsBody.innerHTML = "";
  if (state.cronJobs.length === 0) {
    const entry = document.createElement("li");
    entry.className = "cron-job-card-entry";
    const card = document.createElement("article");
    card.className = "cron-job-card cron-job-card-empty";
    card.textContent = t("cron.empty");
    entry.appendChild(card);
    cronJobsBody.appendChild(entry);
    return;
  }

  state.cronJobs.forEach((job: CronJobSpec) => {
    const entry = document.createElement("li");
    entry.className = "cron-job-card-entry";

    const card = document.createElement("article");
    card.className = "cron-job-card";

    const jobState = state.cronStates[job.id];
    const nextRun = jobState?.next_run_at;
    const statusText = formatCronStatus(jobState);

    const head = document.createElement("div");
    head.className = "cron-job-card-head";

    const title = document.createElement("h4");
    title.className = "cron-job-card-title";
    title.textContent = job.name.trim() === "" ? job.id : job.name;

    const enabled = document.createElement("label");
    enabled.className = "cron-job-card-enabled";
    const enabledLabel = document.createElement("span");
    enabledLabel.textContent = t("cron.enabled");
    const enabledToggle = document.createElement("input");
    enabledToggle.type = "checkbox";
    enabledToggle.className = "cron-enabled-toggle-input";
    enabledToggle.checked = job.enabled;
    enabledToggle.dataset.cronToggleEnabled = job.id;
    enabledToggle.setAttribute("aria-label", `${t("cron.enabled")} ${job.id}`);
    enabled.append(enabledLabel, enabledToggle);
    head.append(title, enabled);

    const metaList = document.createElement("ul");
    metaList.className = "detail-list cron-job-card-meta-list";
    metaList.append(
      createCronJobCardMetaItem(t("cron.id"), job.id, { mono: true }),
      createCronJobCardMetaItem(t("cron.type"), job.task_type),
      createCronJobCardMetaItem(t("cron.nextRun"), nextRun ? compactTime(nextRun) : t("common.none")),
      createCronJobCardMetaItem(t("cron.status"), statusText, { title: jobState?.last_error }),
    );

    const actions = document.createElement("div");
    actions.className = "actions-row cron-job-card-actions";

    const runBtn = document.createElement("button");
    runBtn.type = "button";
    runBtn.className = "secondary-btn";
    runBtn.dataset.cronRun = job.id;
    runBtn.textContent = t("cron.run");

    const editBtn = document.createElement("button");
    editBtn.type = "button";
    editBtn.className = "secondary-btn";
    editBtn.dataset.cronEdit = job.id;
    editBtn.textContent = t("cron.edit");

    const deleteBtn = document.createElement("button");
    deleteBtn.type = "button";
    deleteBtn.className = "danger-btn";
    deleteBtn.dataset.cronDelete = job.id;
    deleteBtn.textContent = t("cron.delete");
    if (isDefaultCronJob(job)) {
      deleteBtn.disabled = true;
      deleteBtn.title = t("cron.deleteDisabledDefault");
    }

    actions.append(runBtn, editBtn, deleteBtn);

    card.append(head, metaList, actions);
    entry.appendChild(card);
    cronJobsBody.appendChild(entry);
  });
}

function createCronJobCardMetaItem(
  label: string,
  value: string,
  options: { mono?: boolean; title?: string } = {},
): HTMLLIElement {
  const row = document.createElement("li");
  row.className = "cron-job-card-meta-row";

  const key = document.createElement("span");
  key.className = "cron-job-card-meta-key";
  key.textContent = label;

  const valueSpan = document.createElement("span");
  valueSpan.className = "cron-job-card-meta-value";
  if (options.mono) {
    valueSpan.classList.add("mono");
  }
  valueSpan.textContent = value;
  if (options.title) {
    valueSpan.title = options.title;
  }

  row.append(key, valueSpan);
  return row;
}

function formatCronStatus(stateValue: CronJobState | undefined): string {
  if (!stateValue?.last_status) {
    return t("common.none");
  }
  const normalized = stateValue.last_status.trim().toLowerCase();
  if (normalized === "running") {
    return t("cron.statusRunning");
  }
  if (normalized === "succeeded") {
    return t("cron.statusSucceeded");
  }
  if (normalized === "failed") {
    return t("cron.statusFailed");
  }
  if (normalized === "paused") {
    return t("cron.statusPaused");
  }
  if (normalized === "resumed") {
    return t("cron.statusResumed");
  }
  return stateValue.last_status;
}

function formatCronWorkflowNodeType(value: CronWorkflowNodeExecution["node_type"]): string {
  if (value === "text_event") {
    return t("cron.nodeTypeTextEvent");
  }
  if (value === "delay") {
    return t("cron.nodeTypeDelay");
  }
  if (value === "if_event") {
    return t("cron.nodeTypeIfEvent");
  }
  return value;
}

function formatCronWorkflowNodeStatus(value: CronWorkflowNodeExecution["status"]): string {
  if (value === "succeeded") {
    return t("cron.statusSucceeded");
  }
  if (value === "failed") {
    return t("cron.statusFailed");
  }
  if (value === "skipped") {
    return t("cron.statusSkipped");
  }
  return value;
}

function isDefaultCronJob(job: CronJobSpec): boolean {
  if (job.id === DEFAULT_CRON_JOB_ID) {
    return true;
  }
  const marker = job.meta?.[CRON_META_SYSTEM_DEFAULT];
  return marker === true || marker === "true";
}

async function saveCronJob(): Promise<boolean> {
  syncControlState();

  const id = cronIDInput.value.trim();
  const name = cronNameInput.value.trim();
  const intervalText = cronIntervalInput.value.trim();
  const sessionID = cronSessionIDInput.value.trim();
  const text = cronTextInput.value.trim();
  const taskType: "text" | "workflow" = cronTaskTypeSelect.value === "text" ? "text" : "workflow";
  state.cronDraftTaskType = taskType;

  if (id === "" || name === "") {
    setStatus(t("error.cronIdNameRequired"), "error");
    return false;
  }
  if (intervalText === "") {
    setStatus(t("error.cronScheduleRequired"), "error");
    return false;
  }
  if (sessionID === "") {
    setStatus(t("error.cronSessionRequired"), "error");
    return false;
  }
  if (taskType === "text" && text === "") {
    setStatus(t("error.cronTextRequired"), "error");
    return false;
  }

  let workflowPayload: CronWorkflowSpec | undefined;
  if (taskType === "workflow") {
    if (!cronWorkflowEditor) {
      setStatus(t("error.cronWorkflowEditorMissing"), "error");
      return false;
    }
    workflowPayload = cronWorkflowEditor.getWorkflow();
    const issue = validateCronWorkflowSpec(workflowPayload);
    if (issue) {
      setStatus(t("error.cronWorkflowInvalid", { reason: issue }), "error");
      return false;
    }
  }

  const maxConcurrency = parseIntegerInput(cronMaxConcurrencyInput.value, 1, 1);
  const timeoutSeconds = parseIntegerInput(cronTimeoutInput.value, 30, 1);
  const misfireGraceSeconds = parseIntegerInput(cronMisfireInput.value, 0, 0);
  const editing = state.cronModal.mode === "edit";
  const existingJob = state.cronJobs.find((job: CronJobSpec) => job.id === state.cronModal.editingJobID);

  const payload: CronJobSpec = {
    id,
    name,
    enabled: existingJob?.enabled ?? true,
    schedule: {
      type: existingJob?.schedule.type ?? "interval",
      cron: intervalText,
      timezone: existingJob?.schedule.timezone ?? "",
    },
    task_type: taskType,
    text: taskType === "text" ? text : undefined,
    workflow: taskType === "workflow" ? workflowPayload : undefined,
    dispatch: {
      type: existingJob?.dispatch.type ?? "channel",
      channel: state.channel,
      target: {
        user_id: state.userId,
        session_id: sessionID,
      },
      mode: existingJob?.dispatch.mode ?? "",
      meta: existingJob?.dispatch.meta ?? {},
    },
    runtime: {
      max_concurrency: maxConcurrency,
      timeout_seconds: timeoutSeconds,
      misfire_grace_seconds: misfireGraceSeconds,
    },
    meta: existingJob?.meta ?? {},
  };

  try {
    if (editing) {
      await requestJSON(`/cron/jobs/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: payload,
      });
    } else {
      await requestJSON("/cron/jobs", {
        method: "POST",
        body: payload,
      });
    }
    await refreshCronJobs();
    if (!editing) {
      setCronModalMode("edit", id);
    }
    renderCronExecutionDetails(state.cronStates[id]);
    setStatus(t(editing ? "status.cronUpdated" : "status.cronCreated", { jobId: id }), "info");
    return true;
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return false;
  }
}

async function updateCronJobEnabled(jobID: string, enabled: boolean): Promise<boolean> {
  const existingJob = state.cronJobs.find((job: CronJobSpec) => job.id === jobID);
  if (!existingJob) {
    setStatus(t("error.cronJobNotFound", { jobId: jobID }), "error");
    return false;
  }
  const payload: CronJobSpec = {
    ...existingJob,
    enabled,
    schedule: {
      type: existingJob.schedule.type ?? "interval",
      cron: existingJob.schedule.cron ?? "",
      timezone: existingJob.schedule.timezone ?? "",
    },
    dispatch: {
      type: existingJob.dispatch.type ?? "channel",
      channel: existingJob.dispatch.channel ?? state.channel,
      target: {
        user_id: existingJob.dispatch.target.user_id ?? state.userId,
        session_id: existingJob.dispatch.target.session_id ?? "",
      },
      mode: existingJob.dispatch.mode ?? "",
      meta: existingJob.dispatch.meta ?? {},
    },
    runtime: {
      max_concurrency: existingJob.runtime.max_concurrency ?? 1,
      timeout_seconds: existingJob.runtime.timeout_seconds ?? 30,
      misfire_grace_seconds: existingJob.runtime.misfire_grace_seconds ?? 0,
    },
    meta: existingJob.meta ?? {},
  };

  if (payload.dispatch.target.session_id.trim() === "") {
    setStatus(t("error.cronSessionRequired"), "error");
    return false;
  }

  try {
    await requestJSON(`/cron/jobs/${encodeURIComponent(jobID)}`, {
      method: "PUT",
      body: payload,
    });
    await refreshCronJobs();
    return true;
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
    return false;
  }
}

async function deleteCronJob(jobID: string): Promise<void> {
  syncControlState();
  const job = state.cronJobs.find((item: CronJobSpec) => item.id === jobID);
  if ((job && isDefaultCronJob(job)) || jobID === DEFAULT_CRON_JOB_ID) {
    setStatus(t("error.cronDeleteDefaultProtected", { jobId: jobID }), "error");
    return;
  }
  if (!window.confirm(t("cron.deleteConfirm", { jobId: jobID }))) {
    return;
  }

  try {
    const result = await requestJSON(`/cron/jobs/${encodeURIComponent(jobID)}`, {
      method: "DELETE",
    });
    await refreshCronJobs();
    if (state.cronModal.mode === "edit" && state.cronModal.editingJobID === jobID) {
      setCronCreateModalOpen(false);
      setCronModalMode("create");
      renderCronExecutionDetails(undefined);
    }
    const messageKey = result.deleted ? "status.cronDeleted" : "status.cronDeleteSkipped";
    setStatus(t(messageKey, { jobId: jobID }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}

async function runCronJob(jobID: string): Promise<void> {
  syncControlState();
  try {
    const result = await requestJSON(`/cron/jobs/${encodeURIComponent(jobID)}/run`, {
      method: "POST",
    });
    await refreshCronJobs();
    await reloadChats();
    setStatus(t("status.cronRunRequested", { jobId: jobID, started: String(result.started) }), "info");
  } catch (error) {
    setStatus(asErrorMessage(error), "error");
  }
}


function refreshCronWorkflowLabels(): void {
  cronWorkflowEditor?.refreshLabels();
}

  return {
    bindCronEvents,
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
  };
}

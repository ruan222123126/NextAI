import { createWorkspaceDomain } from "./workspace-domain.js";
import type { FeatureModule } from "./feature-contract.js";
import type {
  WebAppState,
  WebAppStatusSetter,
  WebAppTranslate,
  WorkspaceCardKey,
  WorkspaceDomainContext,
} from "./types.js";

interface WorkspaceFeatureContext {
  domainContext: WorkspaceDomainContext;
  state: WebAppState;
  t: WebAppTranslate;
  setStatus: WebAppStatusSetter;
  setWorkspaceEditorModalOpen: (open: boolean) => void;
  setWorkspaceImportModalOpen: (open: boolean) => void;
  refreshWorkspaceButton: HTMLButtonElement;
  workspaceSettingsSection: HTMLElement;
  workspaceImportOpenButton: HTMLButtonElement;
  workspaceEditorModal: HTMLElement;
  workspaceEditorModalCloseButton: HTMLButtonElement;
  workspaceImportModal: HTMLElement;
  workspaceImportModalCloseButton: HTMLButtonElement;
  workspaceFilesBody: HTMLUListElement;
  workspacePromptsBody: HTMLUListElement;
  workspaceEditorForm: HTMLFormElement;
  workspaceDeleteFileButton: HTMLButtonElement;
  workspaceFilePathInput: HTMLInputElement;
  workspaceImportForm: HTMLFormElement;
}

export function createWorkspaceFeature(ctx: WorkspaceFeatureContext): FeatureModule<any> {
  const {
    domainContext,
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
    workspaceEditorForm,
    workspaceDeleteFileButton,
    workspaceFilePathInput,
    workspaceImportForm,
  } = ctx;

  const domain = createWorkspaceDomain(domainContext);
  const removers: Array<() => void> = [];
  let initialized = false;

  const on = (
    target: EventTarget,
    type: string,
    listener: EventListenerOrEventListenerObject,
    options?: boolean | AddEventListenerOptions,
  ): void => {
    target.addEventListener(type, listener, options);
    removers.push(() => {
      target.removeEventListener(type, listener, options);
    });
  };

  const isWorkspaceEditorModalOpen = (): boolean => !workspaceEditorModal.classList.contains("is-hidden");
  const isWorkspaceImportModalOpen = (): boolean => !workspaceImportModal.classList.contains("is-hidden");

  function openWorkspaceSettingsCard(card: WorkspaceCardKey): void {
    if (!domain.ensureWorkspaceCardEnabled(card)) {
      return;
    }
    domain.setWorkspaceSettingsLevel(card);
    domain.renderWorkspacePanel();
  }

  function handleWorkspaceNavigationClick(target: Element): void {
    const toggleButton = target.closest<HTMLButtonElement>("button[data-workspace-toggle-card]");
    if (toggleButton) {
      const card = toggleButton.dataset.workspaceToggleCard;
      if (!domain.isWorkspaceCardKey(card)) {
        return;
      }
      const enabled = state.workspaceCardEnabled[card] !== false;
      domain.setWorkspaceCardEnabled(card, !enabled);
      return;
    }
    const button = target.closest<HTMLButtonElement>("button[data-workspace-action]");
    if (!button) {
      return;
    }
    const action = button.dataset.workspaceAction;
    if (action === "open-config") {
      openWorkspaceSettingsCard("config");
      return;
    }
    if (action === "open-prompt") {
      openWorkspaceSettingsCard("prompt");
      return;
    }
    if (action === "back") {
      domain.setWorkspaceSettingsLevel("list");
      domain.renderWorkspacePanel();
    }
  }

  async function handleWorkspaceFileListClick(event: Event): Promise<void> {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const openButton = target.closest<HTMLButtonElement>("button[data-workspace-open]");
    if (openButton) {
      const path = openButton.dataset.workspaceOpen ?? "";
      if (path !== "") {
        await domain.openWorkspaceFile(path);
      }
      return;
    }
    const deleteButton = target.closest<HTMLButtonElement>("button[data-workspace-delete]");
    if (!deleteButton) {
      return;
    }
    const path = deleteButton.dataset.workspaceDelete ?? "";
    if (path === "") {
      return;
    }
    await domain.deleteWorkspaceFile(path);
  }

  function init(): void {
    if (initialized) {
      return;
    }
    initialized = true;

    on(refreshWorkspaceButton, "click", async () => {
      await domain.refreshWorkspace();
    });

    on(workspaceSettingsSection, "click", (event: Event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      handleWorkspaceNavigationClick(target);
    });

    on(workspaceImportOpenButton, "click", () => {
      setWorkspaceImportModalOpen(true);
    });

    on(workspaceEditorModal, "click", (event: Event) => {
      const target = event.target;
      if (target instanceof Element && target.closest("[data-workspace-editor-close=\"true\"]")) {
        setWorkspaceEditorModalOpen(false);
        return;
      }
      event.stopPropagation();
    });

    on(workspaceEditorModalCloseButton, "click", () => {
      setWorkspaceEditorModalOpen(false);
    });

    on(workspaceImportModal, "click", (event: Event) => {
      const target = event.target;
      if (target instanceof Element && target.closest("[data-workspace-import-close=\"true\"]")) {
        setWorkspaceImportModalOpen(false);
        return;
      }
      event.stopPropagation();
    });

    on(workspaceImportModalCloseButton, "click", () => {
      setWorkspaceImportModalOpen(false);
    });

    on(workspaceFilesBody, "click", (event: Event) => {
      void handleWorkspaceFileListClick(event);
    });

    on(workspacePromptsBody, "click", (event: Event) => {
      void handleWorkspaceFileListClick(event);
    });

    on(workspaceEditorForm, "submit", async (event: Event) => {
      event.preventDefault();
      await domain.saveWorkspaceFile();
    });

    on(workspaceDeleteFileButton, "click", async () => {
      const path = workspaceFilePathInput.value.trim();
      if (path === "") {
        setStatus(t("error.workspacePathRequired"), "error");
        return;
      }
      await domain.deleteWorkspaceFile(path);
    });

    on(workspaceImportForm, "submit", async (event: Event) => {
      event.preventDefault();
      await domain.importWorkspaceJSON();
    });

    on(document, "keydown", (event: Event) => {
      const keyboardEvent = event as KeyboardEvent;
      if (keyboardEvent.key !== "Escape" || !isWorkspaceEditorModalOpen()) {
        return;
      }
      setWorkspaceEditorModalOpen(false);
    });

    on(document, "keydown", (event: Event) => {
      const keyboardEvent = event as KeyboardEvent;
      if (keyboardEvent.key !== "Escape" || !isWorkspaceImportModalOpen()) {
        return;
      }
      setWorkspaceImportModalOpen(false);
    });
  }

  function dispose(): void {
    if (!initialized) {
      return;
    }
    initialized = false;
    while (removers.length > 0) {
      const remove = removers.pop();
      remove?.();
    }
  }

  return {
    init,
    dispose,
    actions: {
      ...domain,
      setWorkspaceEditorModalOpen,
      setWorkspaceImportModalOpen,
      isWorkspaceEditorModalOpen,
      isWorkspaceImportModalOpen,
    },
  };
}

import { createChatDomain } from "./chat-domain.js";
import type { FeatureModule } from "./feature-contract.js";
import type { ChatDomainContext, WebAppState, WebAppStatusSetter, WebAppTranslate } from "./types.js";

interface ChatModelActions {
  handleComposerProviderSelectChange: () => Promise<void>;
  handleComposerModelSelectChange: () => Promise<void>;
}

interface ChatFeatureContext {
  domainContext: ChatDomainContext;
  state: WebAppState;
  t: WebAppTranslate;
  setStatus: WebAppStatusSetter;
  syncControlState: () => void;
  getModelActions: () => ChatModelActions | null;
  renderComposerTokenEstimate: () => void;
  reloadChatsButton: HTMLButtonElement;
  newChatButton: HTMLButtonElement;
  chatPromptModeSelect: HTMLSelectElement;
  composerForm: HTMLFormElement;
  composerMain: HTMLElement;
  messageInput: HTMLTextAreaElement;
  sendButton: HTMLButtonElement;
  composerAttachButton: HTMLButtonElement;
  composerAttachInput: HTMLInputElement;
  composerProviderSelect: HTMLSelectElement;
  composerModelSelect: HTMLSelectElement;
  composerSlashPanel: HTMLElement;
  composerSlashList: HTMLUListElement;
  composerSlashController: {
    isOpen: () => boolean;
    hide: () => void;
    render: () => void;
    handleKeydown: (event: KeyboardEvent) => boolean;
    handleListClick: (event: Event) => void;
  };
}

export function createChatFeature(ctx: ChatFeatureContext): FeatureModule<any> {
  const {
    domainContext,
    state,
    t,
    setStatus,
    syncControlState,
    getModelActions,
    renderComposerTokenEstimate,
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
  } = ctx;

  const domain = createChatDomain(domainContext);
  const removers: Array<() => void> = [];
  let initialized = false;
  let composerFileDragDepth = 0;

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

  function isComposerSlashPanelOpen(): boolean {
    return composerSlashController.isOpen();
  }

  function hideComposerSlashPanel(): void {
    composerSlashController.hide();
  }

  function renderComposerSlashPanel(): void {
    composerSlashController.render();
  }

  function handleComposerSlashPanelKeydown(event: KeyboardEvent): boolean {
    return composerSlashController.handleKeydown(event);
  }

  function init(): void {
    if (initialized) {
      return;
    }
    initialized = true;

    on(reloadChatsButton, "click", async () => {
      syncControlState();
      setStatus(t("status.refreshingChats"), "info");
      await domain.reloadChats();
      setStatus(t("status.chatsRefreshed"), "info");
    });

    on(newChatButton, "click", () => {
      syncControlState();
      domain.startDraftSession();
      setStatus(t("status.draftReady"), "info");
    });

    on(chatPromptModeSelect, "change", () => {
      domain.setActivePromptMode(domain.normalizePromptMode(chatPromptModeSelect.value), { announce: true });
    });

    on(sendButton, "click", (event: Event) => {
      if (!state.sending) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      domain.pauseReply();
    });

    on(composerForm, "submit", async (event: Event) => {
      event.preventDefault();
      await domain.sendMessage();
    });

    on(messageInput, "keydown", (event: Event) => {
      const keyboardEvent = event as KeyboardEvent;
      if (handleComposerSlashPanelKeydown(keyboardEvent)) {
        return;
      }
      if (
        keyboardEvent.key !== "Enter"
        || keyboardEvent.shiftKey
        || keyboardEvent.ctrlKey
        || keyboardEvent.metaKey
        || keyboardEvent.altKey
        || keyboardEvent.isComposing
      ) {
        return;
      }
      keyboardEvent.preventDefault();
      void domain.sendMessage();
    });

    on(messageInput, "input", () => {
      renderComposerTokenEstimate();
      renderComposerSlashPanel();
    });

    on(messageInput, "focus", () => {
      renderComposerSlashPanel();
    });

    on(composerAttachButton, "click", () => {
      composerAttachInput.click();
    });

    on(composerAttachInput, "change", () => {
      void domain.handleComposerAttachmentFiles(composerAttachInput.files);
      composerAttachInput.value = "";
    });

    on(composerMain, "dragenter", (event: Event) => {
      const dragEvent = event as DragEvent;
      if (!domain.isFileDragEvent(dragEvent)) {
        return;
      }
      dragEvent.preventDefault();
      composerFileDragDepth += 1;
      composerMain.classList.add("is-file-drag-over");
    });

    on(composerMain, "dragover", (event: Event) => {
      const dragEvent = event as DragEvent;
      if (!domain.isFileDragEvent(dragEvent)) {
        return;
      }
      dragEvent.preventDefault();
      if (dragEvent.dataTransfer) {
        dragEvent.dataTransfer.dropEffect = "copy";
      }
      composerMain.classList.add("is-file-drag-over");
    });

    on(composerMain, "dragleave", (event: Event) => {
      if (!composerMain.classList.contains("is-file-drag-over")) {
        return;
      }
      event.preventDefault();
      composerFileDragDepth = Math.max(0, composerFileDragDepth - 1);
      if (composerFileDragDepth === 0) {
        composerMain.classList.remove("is-file-drag-over");
      }
    });

    on(composerMain, "drop", (event: Event) => {
      const dragEvent = event as DragEvent;
      if (!domain.isFileDragEvent(dragEvent)) {
        return;
      }
      dragEvent.preventDefault();
      composerFileDragDepth = 0;
      domain.clearComposerFileDragState();
      void domain.handleComposerAttachmentFiles(
        dragEvent.dataTransfer?.files ?? null,
        domain.extractDroppedFilePaths(dragEvent.dataTransfer ?? null),
      );
    });

    on(window, "drop", () => {
      composerFileDragDepth = 0;
      domain.clearComposerFileDragState();
    });

    on(window, "dragend", () => {
      composerFileDragDepth = 0;
      domain.clearComposerFileDragState();
    });

    on(composerProviderSelect, "change", () => {
      const modelActions = getModelActions();
      void modelActions?.handleComposerProviderSelectChange();
    });

    on(composerModelSelect, "change", () => {
      const modelActions = getModelActions();
      void modelActions?.handleComposerModelSelectChange();
    });

    on(composerSlashPanel, "mousedown", (event: Event) => {
      event.preventDefault();
    });

    on(composerSlashList, "click", (event: Event) => {
      composerSlashController.handleListClick(event);
    });

    on(document, "click", (event: Event) => {
      if (!isComposerSlashPanelOpen()) {
        return;
      }
      const target = event.target;
      if (target instanceof Node && composerMain.contains(target)) {
        return;
      }
      hideComposerSlashPanel();
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
    composerFileDragDepth = 0;
    domain.clearComposerFileDragState();
  }

  return {
    init,
    dispose,
    actions: {
      ...domain,
      isComposerSlashPanelOpen,
      hideComposerSlashPanel,
      renderComposerSlashPanel,
      handleComposerSlashPanelKeydown,
    },
  };
}

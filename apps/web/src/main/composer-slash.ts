export interface LocalizedComposerSlashCommand {
  id: string;
  command: string;
  insertText: string;
  title: string;
  description: string;
  groupLabel: string;
  keywords: string[];
}

interface ComposerSlashControllerOptions {
  messageInput: HTMLTextAreaElement;
  composerSlashPanel: HTMLElement;
  composerSlashList: HTMLUListElement;
  getCommands: () => LocalizedComposerSlashCommand[];
  resolveEmptyText: () => string;
  onApplied?: () => void;
}

export interface ComposerSlashController {
  resolveQuery: (rawInput: string) => string | null;
  resolveCommands: (rawInput?: string) => LocalizedComposerSlashCommand[];
  isOpen: () => boolean;
  hide: () => void;
  render: () => void;
  applyCommand: (command: LocalizedComposerSlashCommand) => void;
  handleKeydown: (event: KeyboardEvent) => boolean;
  handleListClick: (event: Event) => void;
}

export function createComposerSlashController(options: ComposerSlashControllerOptions): ComposerSlashController {
  const {
    messageInput,
    composerSlashPanel,
    composerSlashList,
    getCommands,
    resolveEmptyText,
    onApplied,
  } = options;

  let selectionIndex = 0;

  function resolveQuery(rawInput: string): string | null {
    const normalized = rawInput.replace(/\r/g, "");
    const trimmedLeading = normalized.trimStart();
    if (!trimmedLeading.startsWith("/")) {
      return null;
    }
    const firstLine = trimmedLeading.split("\n")[0] ?? "";
    if (firstLine.includes(" ") || firstLine.includes("\t")) {
      return null;
    }
    return firstLine.slice(1).trim().toLowerCase();
  }

  function resolveCommands(rawInput = messageInput.value): LocalizedComposerSlashCommand[] {
    const query = resolveQuery(rawInput);
    if (query === null) {
      return [];
    }
    const commands = getCommands();
    if (query === "") {
      return commands;
    }
    return commands.filter((command) => {
      if (command.command.slice(1).toLowerCase().startsWith(query)) {
        return true;
      }
      if (command.title.toLowerCase().includes(query)) {
        return true;
      }
      if (command.description.toLowerCase().includes(query)) {
        return true;
      }
      return command.keywords.some((keyword) => keyword.toLowerCase().includes(query));
    });
  }

  function isOpen(): boolean {
    return !composerSlashPanel.classList.contains("is-hidden");
  }

  function hide(): void {
    if (!isOpen()) {
      return;
    }
    composerSlashPanel.classList.add("is-hidden");
    composerSlashPanel.setAttribute("aria-hidden", "true");
    messageInput.setAttribute("aria-expanded", "false");
    composerSlashList.innerHTML = "";
    selectionIndex = 0;
  }

  function render(): void {
    const query = resolveQuery(messageInput.value);
    if (query === null) {
      hide();
      return;
    }
    const commands = resolveCommands();
    composerSlashList.innerHTML = "";
    if (commands.length === 0) {
      const emptyItem = document.createElement("li");
      emptyItem.className = "composer-slash-empty";
      emptyItem.textContent = resolveEmptyText();
      composerSlashList.appendChild(emptyItem);
    } else {
      selectionIndex = Math.max(0, Math.min(selectionIndex, commands.length - 1));
      let previousGroupLabel = "";
      commands.forEach((command, index) => {
        if (command.groupLabel && command.groupLabel !== previousGroupLabel) {
          const groupTitle = document.createElement("li");
          groupTitle.className = "composer-slash-group";
          groupTitle.textContent = command.groupLabel;
          composerSlashList.appendChild(groupTitle);
          previousGroupLabel = command.groupLabel;
        }

        const item = document.createElement("li");
        item.className = "composer-slash-item";
        const option = document.createElement("button");
        option.type = "button";
        option.className = "composer-slash-item-btn";
        if (index === selectionIndex) {
          option.classList.add("is-active");
        }
        option.dataset.composerSlashIndex = String(index);
        option.setAttribute("role", "option");
        option.setAttribute("aria-selected", index === selectionIndex ? "true" : "false");

        const icon = document.createElement("span");
        icon.className = "composer-slash-item-icon";
        icon.setAttribute("aria-hidden", "true");

        const body = document.createElement("span");
        body.className = "composer-slash-item-body";

        const commandText = document.createElement("span");
        commandText.className = "composer-slash-item-command";
        commandText.textContent = command.command;

        const titleText = document.createElement("span");
        titleText.className = "composer-slash-item-title";
        titleText.textContent = command.title;

        const descText = document.createElement("span");
        descText.className = "composer-slash-item-desc";
        descText.textContent = command.description;

        body.append(titleText, descText);
        option.append(icon, body, commandText);
        item.appendChild(option);
        composerSlashList.appendChild(item);
      });
    }
    composerSlashPanel.classList.remove("is-hidden");
    composerSlashPanel.setAttribute("aria-hidden", "false");
    messageInput.setAttribute("aria-expanded", "true");
  }

  function moveSelection(step: number): void {
    const commands = resolveCommands();
    if (commands.length === 0) {
      return;
    }
    const normalizedStep = step < 0 ? -1 : 1;
    const nextIndex = selectionIndex + normalizedStep;
    selectionIndex = (nextIndex + commands.length) % commands.length;
    render();
  }

  function applyCommand(command: LocalizedComposerSlashCommand): void {
    messageInput.value = command.insertText;
    messageInput.focus();
    const cursor = messageInput.value.length;
    messageInput.setSelectionRange(cursor, cursor);
    onApplied?.();
    render();
  }

  function handleKeydown(event: KeyboardEvent): boolean {
    if (!isOpen()) {
      return false;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      hide();
      return true;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      moveSelection(1);
      return true;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      moveSelection(-1);
      return true;
    }
    if (event.key === "Tab") {
      event.preventDefault();
      moveSelection(event.shiftKey ? -1 : 1);
      return true;
    }
    if (event.key !== "Enter") {
      return false;
    }
    const commands = resolveCommands();
    if (commands.length === 0) {
      return false;
    }
    const selected = commands[Math.max(0, Math.min(selectionIndex, commands.length - 1))];
    if (!selected) {
      return false;
    }
    event.preventDefault();
    applyCommand(selected);
    return true;
  }

  function handleListClick(event: Event): void {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const commandButton = target.closest<HTMLButtonElement>("button[data-composer-slash-index]");
    if (!commandButton) {
      return;
    }
    const indexRaw = Number(commandButton.dataset.composerSlashIndex ?? "");
    if (!Number.isInteger(indexRaw)) {
      return;
    }
    const commands = resolveCommands();
    const selected = commands[indexRaw];
    if (!selected) {
      return;
    }
    applyCommand(selected);
  }

  return {
    resolveQuery,
    resolveCommands,
    isOpen,
    hide,
    render,
    applyCommand,
    handleKeydown,
    handleListClick,
  };
}

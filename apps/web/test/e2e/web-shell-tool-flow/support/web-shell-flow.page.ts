type SettingsSection = "models" | "channels" | "workspace";
type PanelName = "chat" | "cron";

export class WebShellFlowPage {
  messageInput(): HTMLTextAreaElement {
    return this.requireByID<HTMLTextAreaElement>("message-input");
  }

  sendMessage(text: string): void {
    const input = this.messageInput();
    input.value = text;
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
  }

  clickNewChat(): void {
    this.requireByID<HTMLButtonElement>("new-chat").click();
  }

  promptModeSelect(): HTMLSelectElement {
    return this.requireByID<HTMLSelectElement>("chat-prompt-mode-select");
  }

  setPromptMode(value: string): void {
    const select = this.promptModeSelect();
    select.value = value;
    select.dispatchEvent(new Event("change", { bubbles: true }));
  }

  assistantMessages(): HTMLLIElement[] {
    return this.queryAll<HTMLLIElement>("#message-list .message.assistant");
  }

  lastAssistantMessage(): HTMLLIElement | null {
    return this.queryOne<HTMLLIElement>("#message-list .message.assistant:last-child");
  }

  lastAssistantSummary(): string {
    return this.lastAssistantMessage()?.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
  }

  clickFirstChatItem(): void {
    this.queryOne<HTMLButtonElement>("#chat-list .chat-item-btn")?.click();
  }

  chatItemButton(chatID: string): HTMLButtonElement | null {
    return this.queryOne<HTMLButtonElement>(`#chat-list .chat-item-btn[data-chat-id="${chatID}"]`);
  }

  chatListText(): string {
    return this.queryOne<HTMLElement>("#chat-list")?.textContent ?? "";
  }

  clickSearchToggle(): void {
    this.requireByID<HTMLButtonElement>("chat-search-toggle").click();
  }

  searchInput(): HTMLInputElement {
    return this.requireByID<HTMLInputElement>("search-chat-input");
  }

  setSearchQuery(value: string): void {
    const input = this.searchInput();
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
  }

  searchResultButtons(): HTMLButtonElement[] {
    return this.queryAll<HTMLButtonElement>("#search-chat-results .search-result-btn");
  }

  clickFirstSearchResult(): void {
    this.searchResultButtons()[0]?.click();
  }

  statusLineText(): string {
    return this.byID<HTMLElement>("status-line")?.textContent ?? "";
  }

  chatTitleText(): string {
    return this.byID<HTMLElement>("chat-title")?.textContent ?? "";
  }

  chatSessionText(): string {
    return this.byID<HTMLElement>("chat-session")?.textContent ?? "";
  }

  thinkingIndicator(): HTMLElement | null {
    return this.byID<HTMLElement>("thinking-indicator");
  }

  openSettings(): void {
    this.requireByID<HTMLButtonElement>("settings-toggle").click();
  }

  openSettingsSection(section: SettingsSection): void {
    const button = this.queryOne<HTMLButtonElement>(`button[data-settings-section="${section}"]`);
    if (!button) {
      throw new Error(`missing settings section button: ${section}`);
    }
    button.click();
  }

  openPanel(panel: PanelName): void {
    const button = this.queryOne<HTMLButtonElement>(`button[data-tab="${panel}"]`);
    if (!button) {
      throw new Error(`missing panel button: ${panel}`);
    }
    button.click();
  }

  isPanelActive(panel: PanelName): boolean {
    return this.byID<HTMLElement>(`panel-${panel}`)?.classList.contains("is-active") === true;
  }

  byID<T extends HTMLElement>(id: string): T | null {
    return document.getElementById(id) as T | null;
  }

  requireByID<T extends HTMLElement>(id: string): T {
    const node = this.byID<T>(id);
    if (!node) {
      throw new Error(`missing element #${id}`);
    }
    return node;
  }

  queryOne<T extends Element>(selector: string): T | null {
    return document.querySelector<T>(selector);
  }

  queryAll<T extends Element>(selector: string): T[] {
    return Array.from(document.querySelectorAll<T>(selector));
  }
}

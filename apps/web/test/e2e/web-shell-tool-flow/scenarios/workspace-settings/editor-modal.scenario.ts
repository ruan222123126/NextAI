import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: workspace 与渠道配置场景 - 编辑器与弹窗", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("工作区文本文件应按原文编辑并以 content 字段保存", async () => {
    const filePath = "prompts/AGENTS.md";
    const rawContent = [
      "# AI Tool Guide",
      "你可以通过 POST /agent/process 触发工具调用。",
      '命令示例: {"shell":[{"command":"pwd"}]}',
    ].join("\n");
    let workspaceFileLoaded = false;
    let workspaceFileSaved = false;
    let savedContent = "";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse({
          files: [{ path: filePath, kind: "config", size: rawContent.length }],
        });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(filePath)}` && method === "GET") {
        workspaceFileLoaded = true;
        return jsonResponse({ content: rawContent });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(filePath)}` && method === "PUT") {
        workspaceFileSaved = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as { content?: string };
        savedContent = payload.content ?? "";
        return jsonResponse({ updated: true });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.openSettings();
    page.openSettingsSection("workspace");

    await waitFor(() => document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${filePath}"]`) !== null, 4000);

    const openButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${filePath}"]`);
    openButton?.click();

    await waitFor(() => workspaceFileLoaded, 4000);
    await waitFor(() => {
      const input = document.getElementById("workspace-file-content") as HTMLTextAreaElement | null;
      return input?.value === rawContent;
    }, 4000);

    const editorInput = document.getElementById("workspace-file-content") as HTMLTextAreaElement;
    expect(editorInput.value).toBe(rawContent);
    expect(editorInput.value.includes("\\n")).toBe(false);

    const newContent = `${rawContent}\n新增一行`;
    editorInput.value = newContent;
    const editorForm = document.getElementById("workspace-editor-form") as HTMLFormElement;
    editorForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => workspaceFileSaved, 4000);
    expect(savedContent).toBe(newContent);
  });



  it("keeps settings popover open when closing workspace editor modal", async () => {
    const filePath = "prompts/AGENTS.md";
    const rawContent = "# AI Tool Guide";
    let workspaceFileLoaded = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse({
          files: [{ path: filePath, kind: "config", size: rawContent.length }],
        });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(filePath)}` && method === "GET") {
        workspaceFileLoaded = true;
        return jsonResponse({ content: rawContent });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.openSettings();
    page.openSettingsSection("workspace");

    await waitFor(() => document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${filePath}"]`) !== null, 4000);
    const openButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${filePath}"]`);
    openButton?.click();

    await waitFor(() => workspaceFileLoaded, 4000);
    await waitFor(() => !((document.getElementById("workspace-editor-modal") as HTMLElement).classList.contains("is-hidden")), 4000);

    const settingsPopover = document.getElementById("settings-popover") as HTMLElement;
    const workspaceEditorModal = document.getElementById("workspace-editor-modal") as HTMLElement;
    const editorCloseButton = document.getElementById("workspace-editor-modal-close-btn") as HTMLButtonElement;
    expect(settingsPopover.classList.contains("is-hidden")).toBe(false);
    expect(workspaceEditorModal.classList.contains("is-hidden")).toBe(false);

    editorCloseButton.click();
    await waitFor(() => workspaceEditorModal.classList.contains("is-hidden"), 4000);
    expect(settingsPopover.classList.contains("is-hidden")).toBe(false);
  });
});

import { describe, expect, it, vi } from "vitest";

import { jsonResponse, mountWebApp, useWebShellFlowFixture, waitFor } from "../../support/test-helpers";
import { WebShellFlowPage } from "../../support/web-shell-flow.page";

describe("web e2e: workspace 与渠道配置场景 - 卡片与目录树", () => {
  useWebShellFlowFixture();
  const page = new WebShellFlowPage();



  it("工作区应新增 codex 提示词卡片并支持文件夹层层展开", async () => {
    const codexFilePaths = [
      "prompts/codex/codex-rs/core/prompt.md",
      "prompts/codex/codex-rs/core/templates/collaboration_mode/default.md",
      "prompts/codex/user-codex/prompts/check-fix.md",
    ];
    const openFilePath = codexFilePaths[0];
    let codexFileLoaded = false;

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
          files: codexFilePaths.map((path) => ({ path, kind: "config", size: path.length })),
        });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(openFilePath)}` && method === "GET") {
        codexFileLoaded = true;
        return jsonResponse({ content: "# codex prompt file" });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.openSettings();
    page.openSettingsSection("workspace");

    await waitFor(() => {
      const button = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-codex"]');
      return Boolean(button && (button.textContent ?? "").includes("3"));
    }, 4000);
    const openCodexButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-codex"]');
    expect(openCodexButton).not.toBeNull();
    expect(openCodexButton?.textContent ?? "").toContain("3");
    openCodexButton?.click();

    await waitFor(() => document.getElementById("workspace-level2-codex-view")?.hasAttribute("hidden") === false, 4000);
    await waitFor(() => document.querySelector('button[data-workspace-folder-toggle="codex-rs"]') !== null, 4000);

    const rootFolderToggle = document.querySelector<HTMLButtonElement>('button[data-workspace-folder-toggle="codex-rs"]');
    expect(rootFolderToggle).not.toBeNull();
    expect(rootFolderToggle?.getAttribute("aria-expanded")).toBe("true");

    const coreFolderToggle = document.querySelector<HTMLButtonElement>('button[data-workspace-folder-toggle="codex-rs/core"]');
    expect(coreFolderToggle).not.toBeNull();
    expect(coreFolderToggle?.getAttribute("aria-expanded")).toBe("false");
    coreFolderToggle?.click();

    await waitFor(
      () => document.querySelector<HTMLButtonElement>('button[data-workspace-folder-toggle="codex-rs/core/templates"]') !== null,
      4000,
    );
    const templatesFolderToggle = document.querySelector<HTMLButtonElement>('button[data-workspace-folder-toggle="codex-rs/core/templates"]');
    expect(templatesFolderToggle).not.toBeNull();
    expect(templatesFolderToggle?.getAttribute("aria-expanded")).toBe("false");

    const openFileButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${openFilePath}"]`);
    expect(openFileButton).not.toBeNull();
    openFileButton?.click();
    await waitFor(() => codexFileLoaded, 4000);
  });



  it("工作区应新增 claude code 提示词卡片并支持文件夹层层展开", async () => {
    const claudeFilePaths = [
      "prompts/claude/claude-code-reverse/tool-adapter-nextai.md",
      "prompts/claude/claude-code-reverse/results/prompts/system-workflow.prompt.md",
      "prompts/claude/claude-code-reverse/results/tools/Read.tool.yaml",
    ];
    const openFilePath = claudeFilePaths[1];
    let claudeFileLoaded = false;

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
          files: claudeFilePaths.map((path) => ({ path, kind: "config", size: path.length })),
        });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(openFilePath)}` && method === "GET") {
        claudeFileLoaded = true;
        return jsonResponse({ content: "# claude code prompt" });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.openSettings();
    page.openSettingsSection("workspace");

    await waitFor(() => {
      const button = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-claude"]');
      return Boolean(button && (button.textContent ?? "").includes("3"));
    }, 4000);
    const openClaudeButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-claude"]');
    expect(openClaudeButton).not.toBeNull();
    expect(openClaudeButton?.textContent ?? "").toContain("3");
    openClaudeButton?.click();

    await waitFor(() => document.getElementById("workspace-level2-claude-view")?.hasAttribute("hidden") === false, 4000);
    await waitFor(() => document.querySelector('button[data-workspace-claude-folder-toggle="claude-code-reverse"]') !== null, 4000);

    const rootFolderToggle = document.querySelector<HTMLButtonElement>(
      'button[data-workspace-claude-folder-toggle="claude-code-reverse"]',
    );
    expect(rootFolderToggle).not.toBeNull();
    expect(rootFolderToggle?.getAttribute("aria-expanded")).toBe("true");

    const resultsFolderToggle = document.querySelector<HTMLButtonElement>(
      'button[data-workspace-claude-folder-toggle="claude-code-reverse/results"]',
    );
    expect(resultsFolderToggle).not.toBeNull();
    expect(resultsFolderToggle?.getAttribute("aria-expanded")).toBe("false");
    resultsFolderToggle?.click();

    await waitFor(
      () => document.querySelector<HTMLButtonElement>('button[data-workspace-claude-folder-toggle="claude-code-reverse/results/prompts"]') !== null,
      4000,
    );
    const promptsFolderToggle = document.querySelector<HTMLButtonElement>(
      'button[data-workspace-claude-folder-toggle="claude-code-reverse/results/prompts"]',
    );
    expect(promptsFolderToggle).not.toBeNull();
    expect(promptsFolderToggle?.getAttribute("aria-expanded")).toBe("false");
    promptsFolderToggle?.click();

    await waitFor(
      () => document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${claudeFilePaths[0]}"]`) !== null,
      4000,
    );
    const rootFileButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${claudeFilePaths[0]}"]`);
    expect(rootFileButton).not.toBeNull();

    const openFileButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${openFilePath}"]`);
    expect(openFileButton).not.toBeNull();
    openFileButton?.click();
    await waitFor(() => claudeFileLoaded, 4000);
  });



  it("工作区卡片应支持启用和禁用", async () => {
    const configPath = "config/channels.json";
    const promptPath = "prompts/ai-tools.md";
    const codexPath = "prompts/codex/user-codex/prompts/check-fix.md";

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
          files: [
            { path: configPath, kind: "config", size: 128 },
            { path: promptPath, kind: "config", size: 256 },
            { path: codexPath, kind: "config", size: 512 },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await mountWebApp();

    page.openSettings();
    page.openSettingsSection("workspace");

    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]') !== null, 4000);

    const disableButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
    expect(disableButton).not.toBeNull();
    expect(disableButton?.textContent ?? "").toContain("禁用");
    disableButton?.click();

    await waitFor(() => {
      const openConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
      return openConfigButton?.disabled === true;
    }, 4000);

    const persistedAfterDisable = window.localStorage.getItem("nextai.web.chat.settings");
    expect(persistedAfterDisable).not.toBeNull();
    const parsedAfterDisable = JSON.parse(String(persistedAfterDisable)) as {
      workspaceCardEnabled?: { config?: boolean };
    };
    expect(parsedAfterDisable.workspaceCardEnabled?.config).toBe(false);

    const disabledOpenConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
    disabledOpenConfigButton?.click();
    expect(document.getElementById("workspace-level2-config-view")?.hasAttribute("hidden")).toBe(true);

    const enableButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
    expect(enableButton).not.toBeNull();
    expect(enableButton?.textContent ?? "").toContain("启用");
    enableButton?.click();

    await waitFor(() => {
      const openConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
      return openConfigButton?.disabled === false;
    }, 4000);
    await waitFor(() => {
      const disableAgainButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
      return (disableAgainButton?.textContent ?? "").includes("禁用");
    }, 4000);
    const persistedAfterEnable = window.localStorage.getItem("nextai.web.chat.settings");
    expect(persistedAfterEnable).not.toBeNull();
    const parsedAfterEnable = JSON.parse(String(persistedAfterEnable)) as {
      workspaceCardEnabled?: { config?: boolean };
    };
    expect(parsedAfterEnable.workspaceCardEnabled?.config).toBe(true);

    const enabledOpenConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
    enabledOpenConfigButton?.click();
    await waitFor(() => document.getElementById("workspace-level2-config-view")?.hasAttribute("hidden") === false, 4000);
  });
});
